package agent

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestTranscriptSearchActualSourceModelAndEpochRewrite(t *testing.T) {
	var fixture struct {
		Managers []struct {
			Rewrite      bool
			Output       string
			CurrentEpoch TranscriptEpoch `json:"current_epoch"`
			Names        [][]protocol.ToolName
			Results      []string
		}
	}
	data, err := os.ReadFile("../testdata/python-transcript-search-runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, row := range fixture.Managers {
		t.Run(map[bool]string{false: "current", true: "rewritten"}[row.Rewrite], func(t *testing.T) {
			var names [][]protocol.ToolName
			var results []string
			provider := stateProviderFunc(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
				catalog := []protocol.ToolName{}
				for _, schema := range request.Tools {
					catalog = append(catalog, schema.Name)
				}
				names = append(names, catalog)
				for _, message := range request.Messages {
					blocks, _ := message.Content.Blocks()
					for _, block := range blocks {
						if result, ok := block.ToolResult(); ok && result.ToolUseID == "search" {
							results = append(results, result.Content)
							return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
						}
					}
				}
				last, _ := request.Messages[len(request.Messages)-1].Content.Plain()
				if last == "search history" {
					input := protocol.TranscriptSearchToolInput(protocol.TranscriptSearchInput{Query: "needle"})
					// Source Fake ToolUseBlock omits caller; preserve that wire fact.
					return fakeReply([]protocol.Block{protocol.NewToolUseWithoutCaller("search", input)}, protocol.StopToolUse), nil
				}
				return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
			})
			store := newRuntimeStateStore()
			config := managerTestConfig(t.TempDir(), provider)
			config.Services.StateStore, config.Services.SessionQueryTools = store, true
			manager := makeManager(t, config)
			own := createManaged(t, manager, CreateSessionRequest{Owner: "alice", PermissionMode: ModeReadonly})
			foreign := createManaged(t, manager, CreateSessionRequest{Owner: "bob", PermissionMode: ModeReadonly})
			if _, err := foreign.Run(context.Background(), "foreign needle"); err != nil {
				t.Fatal(err)
			}
			if row.Rewrite {
				if _, err := own.Run(context.Background(), "own old needle"); err != nil {
					t.Fatal(err)
				}
				// The same idle live-transcript rewrite as the Source compaction
				// recipe. Persistence guards/event capture determine the new epoch.
				own.core.mu.Lock()
				own.core.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("summary")}}
				own.core.mu.Unlock()
			}
			names = nil
			output, err := own.Run(context.Background(), "search history")
			if err != nil || output != row.Output {
				t.Fatal(output, err)
			}
			current, err := store.TranscriptEpoch(context.Background(), own.ID())
			if err != nil || current != row.CurrentEpoch || !reflect.DeepEqual(names, row.Names) || !reflect.DeepEqual(results, row.Results) {
				t.Fatal("Source runtime differs", current, names, results, err)
			}
		})
	}
}
