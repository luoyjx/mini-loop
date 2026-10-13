package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type publicHelperCall struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []protocol.Message `json:"messages"`
}

type publicHelperFixture struct {
	Constants struct {
		Unknown    string `json:"unknown_result"`
		Trajectory string `json:"trajectory_schema"`
	} `json:"constants"`
	Memory []struct {
		Name, Operation, Reply, Query string
		Limit, Threshold              *int
		Tools, Auto, Empty, Fault     bool
		SendFault                     bool `json:"send_fault"`
		ListFault                     bool `json:"list_fault"`
		FallbackFault                 bool `json:"fallback_fault"`
		Before, After, Foreign        []memory.Record
		Selected                      []memory.Record
		Prepared                      string
		Count                         int
		Error                         *string
		Calls                         []publicHelperCall
		Purposes                      []protocol.RequestPurpose
		Loads                         []agent.MemoryEvent
		MessagesUnchanged             bool `json:"messages_unchanged"`
	}
	Budget []struct {
		Name          string
		MaxBytes      *int `json:"max_bytes"`
		PreviewChars  *int `json:"preview_chars"`
		WriteFault    bool `json:"write_fault"`
		Before, After []protocol.Message
		Count         int
		Error         *string
		Files         map[string]string
	}
}

func readPublicHelpers(t *testing.T) publicHelperFixture {
	t.Helper()
	raw, err := os.ReadFile("../testdata/python-public-helpers.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture publicHelperFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Memory) != 41 || len(fixture.Budget) != 20 {
		t.Fatal("public helper coverage changed", len(fixture.Memory), len(fixture.Budget))
	}
	return fixture
}

func TestPublicMemoryHelpersSourceContracts(t *testing.T) {
	fixture := readPublicHelpers(t)
	if agent.UnknownResult != fixture.Constants.Unknown || agent.TrajectorySchema != fixture.Constants.Trajectory {
		t.Fatal("public constants differ from Source")
	}
	for _, row := range fixture.Memory {
		t.Run(row.Name, func(t *testing.T) {
			ctx := context.Background()
			memoryRoot := filepath.Join(t.TempDir(), "memory")
			store, err := memory.NewStore(ctx, memoryRoot, nil)
			if err != nil {
				t.Fatal(err)
			}
			scoped, err := memory.Bind(store, "alice")
			if err != nil {
				t.Fatal(err)
			}
			for _, record := range append(append([]memory.Record(nil), row.Before...), row.Foreign...) {
				if _, err := store.Write(ctx, memory.OwnerID(record.Owner), memory.Input{Name: record.Name, Type: record.Type, Description: record.Description, Body: record.Body, Origin: record.Origin}); err != nil {
					t.Fatal(err)
				}
			}
			calls := []publicHelperCall{}
			purposes := []protocol.RequestPurpose{}
			loads := []agent.MemoryEvent{}
			query := agent.MemoryQueryContext{Store: scoped, Model: "claude-sonnet-4-5-20250929"}
			if len(row.Calls) > 0 {
				query.Model = row.Calls[0].Model
			}
			broken := false
			breakStore := func() {
				if err := os.Rename(memoryRoot, memoryRoot+".saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(memoryRoot, []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
				broken = true
			}
			if row.ListFault {
				breakStore()
			}
			query.Create = func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
				if row.FallbackFault {
					breakStore()
				}
				calls = append(calls, publicHelperCall{request.Model, request.MaxTokens, request.Messages})
				purposes = append(purposes, request.Purpose)
				if row.Fault {
					return protocol.ModelReply{}, errors.New("model fault")
				}
				return protocol.ModelReply{Content: []protocol.Block{protocol.NewTextBlock(row.Reply)}, StopReason: protocol.StopEndTurn}, nil
			}
			var count int
			prepared := ""
			selected := []memory.Record{}
			switch row.Operation {
			case "select":
				limit := agent.DefaultMemorySelectionLimit
				if row.Limit != nil {
					limit = *row.Limit
				}
				selected, err = agent.SelectRelevantMemories(ctx, query, row.Query, limit)
			case "prepare":
				prepared, err = agent.PrepareMemoryContext(ctx, agent.MemoryContext{Query: query, Enabled: row.Auto && row.Tools, Send: func(_ context.Context, event agent.MemoryEvent) error {
					loads = append(loads, event)
					if row.SendFault {
						return errors.New("load sender fault")
					}
					return nil
				}}, row.Query)
			case "consolidate":
				threshold := agent.DefaultMemoryConsolidationThreshold
				if row.Threshold != nil {
					threshold = *row.Threshold
				}
				count, err = agent.ConsolidateMemories(ctx, query, threshold)
			default:
				t.Fatal("unconsumed operation", row.Operation)
			}
			if broken {
				if restoreErr := os.Remove(memoryRoot); restoreErr != nil {
					t.Fatal(restoreErr)
				}
				if restoreErr := os.Rename(memoryRoot+".saved", memoryRoot); restoreErr != nil {
					t.Fatal(restoreErr)
				}
			}
			if (err == nil) != (row.Error == nil) {
				t.Fatal("error boundary differs", err, row.Error)
			}
			if err != nil && !row.ListFault && !row.FallbackFault && err.Error() != *row.Error {
				t.Fatal("error differs", err, *row.Error)
			}
			if count != row.Count || prepared != row.Prepared {
				t.Fatal("helper output differs", count, row.Count, prepared, row.Prepared)
			}
			if len(selected) == 0 {
				selected = []memory.Record{}
			}
			if !reflect.DeepEqual(selected, row.Selected) || !reflect.DeepEqual(calls, row.Calls) || !reflect.DeepEqual(purposes, row.Purposes) || !reflect.DeepEqual(loads, row.Loads) {
				t.Fatal("selection/request/event differs", selected, row.Selected, calls, row.Calls, purposes, row.Purposes, loads, row.Loads)
			}
			after, err := scoped.List(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(after) == 0 {
				after = []memory.Record{}
			}
			foreignOwner := memory.OwnerID("bob")
			foreign, err := store.List(ctx, &foreignOwner)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, row.After) || !reflect.DeepEqual(foreign, row.Foreign) || !row.MessagesUnchanged {
				t.Fatal("store, owner isolation or Source transcript differs", after, row.After, foreign, row.Foreign)
			}
		})
	}
}

func TestPublicToolResultBudgetSourceContracts(t *testing.T) {
	fixture := readPublicHelpers(t)
	for _, row := range fixture.Budget {
		t.Run(row.Name, func(t *testing.T) {
			root := t.TempDir()
			files, err := workspace.NewFiles(root)
			if err != nil {
				t.Fatal(err)
			}
			registry := secrets.New(secrets.Config{})
			registry.RegisterValue("test-secret", "secret-value")
			options := agent.DefaultToolResultBudgetOptions()
			if row.MaxBytes != nil {
				options.MaxBytes = *row.MaxBytes
			}
			if row.PreviewChars != nil {
				options.PreviewChars = *row.PreviewChars
			}
			original, err := json.Marshal(row.Before)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			clockCalls := 0
			result, err := agent.ToolResultBudget(ctx, agent.ToolResultBudgetContext{Messages: row.Before, Files: files, Secrets: registry, Now: func() int64 {
				clockCalls++
				if row.WriteFault && clockCalls == 2 {
					cancel()
				}
				return 123456
			}}, options)
			if (err == nil) != (row.Error == nil) || result.Persisted != row.Count {
				t.Fatal("budget error/count differs", err, result.Persisted, row.Count)
			}
			after, encodeErr := json.Marshal(result.Messages)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			var normalized []protocol.Message
			if err := json.Unmarshal([]byte(strings.ReplaceAll(string(after), files.Root(), "<workspace>")), &normalized); err != nil {
				t.Fatal(err)
			}
			if len(normalized) == 0 {
				normalized = []protocol.Message{}
			}
			if !reflect.DeepEqual(normalized, row.After) {
				t.Fatal("budget transcript differs", string(after), row.After)
			}
			unchanged, _ := json.Marshal(row.Before)
			if string(unchanged) != string(original) {
				t.Fatal("budget mutated caller messages")
			}
			paths, err := filepath.Glob(filepath.Join(root, ".task_outputs", "tool-results", "*.txt"))
			if err != nil {
				t.Fatal(err)
			}
			stored := map[string]string{}
			for _, path := range paths {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				stored[filepath.Base(path)] = string(data)
			}
			if !reflect.DeepEqual(stored, row.Files) {
				t.Fatal("budget disk effects/masking differ", stored, row.Files)
			}
		})
	}
}

func TestPublicMemoryHelpersAuthorityAndCallbackBoundaries(t *testing.T) {
	ctx := context.Background()
	store, err := memory.NewStore(ctx, filepath.Join(t.TempDir(), "memory"), nil)
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := memory.Bind(store, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scoped.Write(ctx, memory.Input{Name: "alpha", Type: memory.Project, Description: "alpha facts", Body: "alpha body", Origin: memory.Explicit}); err != nil {
		t.Fatal(err)
	}
	for _, cause := range []error{agent.ErrStateTranscript, agent.ErrSessionLeaseLost} {
		query := agent.MemoryQueryContext{Store: scoped, Model: "fixture", Create: func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
			return protocol.ModelReply{}, cause
		}}
		if _, err := agent.SelectRelevantMemories(ctx, query, "alpha", 5); !errors.Is(err, cause) {
			t.Fatal("selection hid authority loss", err, cause)
		}
		if _, err := agent.ConsolidateMemories(ctx, query, 0); !errors.Is(err, cause) {
			t.Fatal("consolidation hid authority loss", err, cause)
		}
	}
	query := agent.MemoryQueryContext{Store: scoped, Model: "fixture", Create: func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
		panic("ordinary callback fault")
	}}
	selected, err := agent.SelectRelevantMemories(ctx, query, "alpha", 5)
	if err != nil || len(selected) != 1 || selected[0].Name != "alpha" {
		t.Fatal("ordinary selection panic did not fall back", selected, err)
	}
	if count, err := agent.ConsolidateMemories(ctx, query, 0); err != nil || count != 0 {
		t.Fatal("ordinary consolidation panic escaped", count, err)
	}
	for _, operation := range []string{"select", "consolidate"} {
		cancelCtx, cancel := context.WithCancel(ctx)
		query.Create = func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
			cancel()
			return protocol.ModelReply{}, context.Canceled
		}
		if operation == "select" {
			_, err = agent.SelectRelevantMemories(cancelCtx, query, "alpha", 5)
		} else {
			_, err = agent.ConsolidateMemories(cancelCtx, query, 0)
		}
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation escaped its error boundary", operation, err)
		}
	}
	if text, err := agent.PrepareMemoryContext(ctx, agent.MemoryContext{}, "unchanged"); err != nil || text != "unchanged" {
		t.Fatal("disabled preparation acquired dependencies", text, err)
	}
}

func TestPublicSelectionZeroSliceDoesNotFallBack(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "memory")
	store, err := memory.NewStore(ctx, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := memory.Bind(store, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scoped.Write(ctx, memory.Input{Name: "alpha", Body: "alpha"}); err != nil {
		t.Fatal(err)
	}
	query := agent.MemoryQueryContext{Store: scoped, Model: "fixture", Create: func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(root, []byte("not a directory"), 0600); err != nil {
			t.Fatal(err)
		}
		return protocol.ModelReply{Content: []protocol.Block{protocol.NewTextBlock("[0]")}}, nil
	}}
	selected, err := agent.SelectRelevantMemories(ctx, query, "alpha", 0)
	if err != nil || len(selected) != 0 {
		t.Fatal("valid indices followed by zero slice used failing lexical search", selected, err)
	}
}

func TestPublicBudgetPreservesWorkspaceBoundary(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	files, err := workspace.NewFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".task_outputs")); err != nil {
		t.Fatal(err)
	}
	messages := []protocol.Message{{Role: protocol.RoleUser, Content: protocol.BlockContent(protocol.NewToolResult("id", "result", false))}}
	result, err := agent.ToolResultBudget(context.Background(), agent.ToolResultBudgetContext{Messages: messages, Files: files}, agent.ToolResultBudgetOptions{})
	if err == nil || result.Persisted != 0 || !reflect.DeepEqual(result.Messages, messages) {
		t.Fatal("budget widened workspace authority", result, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("outside files created", entries, err)
	}
}
