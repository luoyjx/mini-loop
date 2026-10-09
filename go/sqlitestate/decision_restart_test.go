package sqlitestate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/decisions"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type restartDecisionProvider func(context.Context, decisions.Request) (decisions.Result, error)

func (provider restartDecisionProvider) Evaluate(ctx context.Context, request decisions.Request) (decisions.Result, error) {
	return provider(ctx, request)
}

func TestDecisionNativeSQLiteReopenMatchesSource(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-decision-replay.json")
	if err != nil {
		t.Fatal(err)
	}
	// Raw JSON is confined to the Source fixture boundary.
	var corpus struct {
		Request, Baseline json.RawMessage
		Cases             []struct {
			Backing         string
			Maximum         bool
			Bytes, Calls    int
			CanonicalSHA256 string `json:"canonical_sha256"`
			ReplayExact     bool   `json:"replay_exact"`
		}
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, row := range corpus.Cases {
		if row.Backing != "sqlite-reopen" {
			continue
		}
		name := "baseline"
		if row.Maximum {
			name = "maximum"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			request, err := decisions.DecodeRequest(corpus.Request)
			if err != nil {
				t.Fatal(err)
			}
			value, err := decisions.DecodeValue(corpus.Baseline)
			if err != nil {
				t.Fatal(err)
			}
			base, err := value.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if row.Maximum {
				value = value.MapStrings(func(text string) string {
					if text == "test-model" {
						return text + strings.Repeat("m", decisions.MaxResponseBytes-len(base))
					}
					return text
				})
			}
			raw, err := value.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			result, err := decisions.DecodeResult(request, raw)
			if err != nil {
				t.Fatal(err)
			}
			calls, modelCalls := 0, 0
			backend := restartDecisionProvider(func(_ context.Context, got decisions.Request) (decisions.Result, error) {
				if !reflect.DeepEqual(got, request) {
					t.Error("decision request changed")
				}
				calls++
				return result, nil
			})
			provider := restartProvider(func(_ context.Context, req protocol.ModelRequest) (protocol.ModelReply, error) {
				modelCalls++
				reply := protocol.ModelReply{ID: "reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: req.Model, StopReason: protocol.StopEndTurn}
				if modelCalls%2 == 1 {
					reply.StopReason = protocol.StopToolUse
					reply.Content = []protocol.Block{protocol.NewToolUse("same-decision", protocol.DecisionToolInput(request))}
				} else {
					reply.Content = []protocol.Block{protocol.NewTextBlock("done")}
				}
				return reply, nil
			})
			root := t.TempDir()
			path := filepath.Join(root, "state.db")
			store := openStore(t, path)
			newManager := func() *agent.SessionManager {
				manager, err := agent.NewSessionManager(agent.ManagerConfig{
					WorkspaceRoot: root,
					Defaults:      agent.SessionDefaults{PermissionMode: agent.ModeAuto, MaxRounds: 2},
					Services: agent.ManagerServices{
						StateStore: store, Provider: provider, DecisionTools: true, DecisionProvider: backend,
						// Keep the archival payload intact; compaction has separate restart proof.
						Compactor: agent.InMemoryCompactor{TokenThreshold: 10000000, MaxMessages: 1000},
					},
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { stopRestartManager(t, manager) })
				return manager
			}
			manager := newManager()
			session, err := manager.Create(ctx, agent.CreateSessionRequest{Owner: "tenant"})
			if err != nil {
				t.Fatal(err)
			}
			run, err := agent.DefaultRunContext()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := session.RunWithContext(ctx, "decide", run); err != nil {
				t.Fatal(err)
			}
			call := agent.ToolCall{ID: "same-decision", Input: protocol.DecisionToolInput(request)}
			id, err := agent.ToolActionID(session.ID(), run, call)
			if err != nil {
				t.Fatal(err)
			}
			first, found, err := store.ReadAction(ctx, id)
			if err != nil || !found || first.Status != agent.ActionCompleted || first.Result == nil || len(*first.Result) != row.Bytes {
				t.Fatal("native result retention", found, err)
			}
			decoded, err := decisions.DecodeValue([]byte(*first.Result))
			if err != nil {
				t.Fatal(err)
			}
			canonical, err := decoded.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(canonical)
			if hex.EncodeToString(digest[:]) != row.CanonicalSHA256 {
				t.Fatal("Source semantic result digest differs")
			}
			history := session.Messages()
			stopRestartManager(t, manager)
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openStore(t, path)
			manager = newManager()
			session = restoreRestartSession(t, manager)
			if !sameRestartMessages(session.Messages(), history) {
				t.Fatal("reopen changed archived decision transcript")
			}
			if session.Info().PermissionMode != agent.ModeInteractive {
				t.Fatal("restore retained historical auto authority")
			}
			// The current operator explicitly admits auto mode; restoration grants none.
			if _, err := session.ChangePermissionMode(agent.ModeAuto); err != nil {
				t.Fatal(err)
			}
			if _, err := session.RunWithContext(ctx, "retry same identity", run); err != nil {
				t.Fatal(err)
			}
			messages := session.Messages()
			blocks, _ := messages[len(messages)-2].Content.Blocks()
			if len(blocks) != 1 {
				t.Fatal("replayed result shape", blocks)
			}
			replayed, ok := blocks[0].ToolResult()
			if !ok || replayed.IsError || replayed.Content != *first.Result || !row.ReplayExact || calls != row.Calls {
				t.Fatal("native reopen changed bytes or repeated backend", calls)
			}
			if _, err := session.ChangePermissionMode(agent.ModeReadonly); err != nil {
				t.Fatal(err)
			}
			if _, err := session.RunWithContext(ctx, "retry while readonly", run); err != nil {
				t.Fatal(err)
			}
			messages = session.Messages()
			blocks, _ = messages[len(messages)-2].Content.Blocks()
			if len(blocks) != 1 {
				t.Fatal("denied result shape", blocks)
			}
			denied, ok := blocks[0].ToolResult()
			if !ok || !strings.Contains(denied.Content, "Permission denied: this session is read-only") || denied.Content == *first.Result || calls != row.Calls {
				t.Fatal("stored replay bypassed current permission", calls)
			}
			retained, found, err := store.ReadAction(ctx, id)
			if err != nil || !found || !reflect.DeepEqual(retained, first) {
				t.Fatal("retry or denial changed settled action", found, err)
			}
			if err := protocol.ValidateTranscript(session.Messages()); err != nil {
				t.Fatal(err)
			}
			if status := session.PersistenceStatus(); status.Error != nil || !status.LeaseConfirmed || status.RestorePending {
				t.Fatal("native persistence degraded", status)
			}
		})
	}
}
