package sqlitestate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func TestMCPMappingKeysNativeSQLiteReopenAndCurrentPermission(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-mcp-mapping-key-journal.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name, Mode string
			Input      string `json:"input_json"`
			Batch      bool
			Error      *struct{ Kind, Message string }
			Calls      []jsonvalue.Value
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	selected := 0
	for _, row := range fixture.Cases {
		if row.Mode != "auto" || (row.Name != "auto/integer-key/batch" && row.Name != "auto/nan-key/batch" && row.Name != "auto/text-then-integer/batch" && row.Name != "auto/array-key/batch" && row.Name != "auto/keyword-text/batch" && row.Name != "auto/nan-root/batch" && row.Name != "auto/integer-key-nan-value/batch") {
			continue
		}
		selected++
		t.Run(row.Name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			path := filepath.Join(root, "state.db")
			calls, requests := 0, 0
			registry := &agent.MCPRegistry{}
			registered, err := registry.Register(ctx, mcpRootSQLPeer{&calls}, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			provider := restartProvider(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
				requests++
				reply := protocol.ModelReply{ID: "reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: request.Model, StopReason: protocol.StopEndTurn, Content: []protocol.Block{protocol.NewTextBlock("done")}}
				if requests > 1 {
					return reply, nil
				}
				blocks := []protocol.Block{}
				if row.Batch {
					first, err := protocol.DecodeModelToolInput("mcp__peer__echo", []byte(`{"first":1}`))
					if err != nil {
						return protocol.ModelReply{}, err
					}
					blocks = append(blocks, protocol.NewToolUse("first", first))
				}
				input, err := protocol.DecodeModelToolInput("mcp__peer__echo", []byte(row.Input))
				if err != nil {
					return protocol.ModelReply{}, err
				}
				blocks = append(blocks, protocol.NewToolUse("model", input))
				reply.Content = blocks
				reply.StopReason = protocol.StopToolUse
				return reply, nil
			})
			makeManager := func(store *Store) *agent.SessionManager {
				manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(root, "ws"), Defaults: agent.SessionDefaults{Model: "model", MaxTokens: 64, MaxRounds: 2, PermissionMode: agent.ModeAuto}, Services: agent.ManagerServices{StateStore: store, Provider: provider, Tools: registered.Catalog}})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { stopRestartManager(t, manager) })
				return manager
			}
			store := openStore(t, path)
			manager := makeManager(store)
			session, err := manager.Create(ctx, agent.CreateSessionRequest{Owner: "alice"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = session.Run(ctx, "MCP")
			if row.Error != nil {
				var failure interface {
					error
					SourceClass() string
				}
				if !errors.As(err, &failure) || failure.SourceClass() != row.Error.Kind {
					t.Fatal("batch failure lost", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if calls != len(row.Calls) {
				t.Fatal("side effects before entire batch prepared", calls, row.Calls)
			}
			if fault := session.PersistenceStatus().Error; fault != nil {
				t.Fatal("mapping event persistence failed", *fault)
			}
			id := session.ID()
			stopRestartManager(t, manager)
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openStore(t, path)
			events, err := store.LoadEvents(ctx, id, 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			expectedUse := row.Name != "auto/array-key/batch" && row.Name != "auto/nan-root/batch"
			foundUse := false
			for _, event := range events {
				use, ok := event.Event.ToolUse()
				if !ok || use.ID != "model" {
					continue
				}
				foundUse = true
				observation, err := use.Input.Observation()
				if err != nil {
					t.Fatal(err)
				}
				raw, err := observation.LegacyJSON()
				if err != nil {
					t.Fatal(err)
				}
				value, err := jsonvalue.Decode(string(raw))
				if err != nil || value.Kind() != jsonvalue.Object {
					t.Fatal("recorded mapping event is not an object", string(raw), err)
				}
			}
			if foundUse != expectedUse {
				t.Fatal("stored use-event stage differs", foundUse, expectedUse)
			}
			messages, err := store.LoadMessages(ctx, id, nil)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, message := range messages {
				if message.Role != protocol.RoleAssistant {
					continue
				}
				blocks, _ := message.Content.Blocks()
				for _, block := range blocks {
					use, ok := block.ToolUse()
					if !ok || use.ID != "model" {
						continue
					}
					observation, err := use.Input.Observation()
					if err != nil {
						t.Fatal(err)
					}
					raw, err := observation.LegacyJSON()
					if err != nil {
						t.Fatal(err)
					}
					value, err := jsonvalue.Decode(string(raw))
					want, _ := jsonvalue.Decode(row.Input)
					gotLegacy, _ := value.MarshalLegacyUTF8()
					wantLegacy, _ := want.MarshalLegacyUTF8()
					if err != nil || string(gotLegacy) != string(wantLegacy) {
						t.Fatal("SQLite normalized original MCP root", string(raw), err)
					}
					found = true
				}
			}
			if !found {
				t.Fatal("original model root missing after SQLite reopen")
			}
			manager = makeManager(store)
			restored, err := manager.RestoreSessions(ctx)
			if err != nil || len(restored) != 1 || restored[0].Owner() != "alice" {
				t.Fatal(restored, err)
			}
			if _, err := manager.Get("bob", id); err == nil {
				t.Fatal("foreign owner got MCP history")
			}
			if restored[0].Info().PermissionMode != agent.ModeInteractive {
				t.Fatal("old auto mode restored")
			}
			before := calls
			if _, err := restored[0].Run(ctx, "continue"); err != nil {
				t.Fatal(err)
			}
			if calls != before || !restored[0].PersistenceStatus().LeaseConfirmed {
				t.Fatal("restore replayed an external effect or lacked fresh lease")
			}
		})
	}
	if selected != 7 {
		t.Fatal(selected)
	}
}
