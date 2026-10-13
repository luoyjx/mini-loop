package sqlitestate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type extensionSQLInput struct {
	Name string `json:"name"`
}

func (extensionSQLInput) Validate() error { return nil }

type extensionSQLCodec struct{}

func (extensionSQLCodec) DecodeExtensionInput(_ context.Context, input protocol.ToolInput) (extensionSQLInput, error) {
	data, err := input.MarshalJSON()
	if err != nil {
		return extensionSQLInput{}, err
	}
	var value extensionSQLInput
	err = json.Unmarshal(data, &value)
	return value, err
}

type extensionSQLHandler struct{ calls *int }

func (handler extensionSQLHandler) ExecuteExtensionTool(_ context.Context, authority agent.ToolAuthority, input extensionSQLInput) (string, error) {
	*handler.calls++
	return fmt.Sprintf("%s:Hello, %s!", authority.OwnerID, input.Name), nil
}

func TestExtensionSchemaNativeSQLiteReopenAndOwnedRestore(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-extension-schemas.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name       string
			SchemaJSON string `json:"schema_json"`
			Sessions   []struct {
				SnapshotJSON string `json:"snapshot_json"`
				Fingerprint  string
			}
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 16 {
		t.Fatal(err)
	}
	for _, row := range fixture.Cases {
		t.Run(row.Name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			path := filepath.Join(root, "state.db")
			calls, requests := 0, 0
			schema, err := protocol.ExtensionToolSchema("greet", "Unicode schema 描述", []byte(row.SchemaJSON))
			if err != nil {
				t.Fatal(err)
			}
			definition, err := agent.NewTypedExtensionTool(schema, agent.ToolTraits{Risk: agent.RiskRead, Readonly: true}, extensionSQLCodec{}, extensionSQLHandler{&calls})
			if err != nil {
				t.Fatal(err)
			}
			catalog, err := agent.NewToolCatalog(definition)
			if err != nil {
				t.Fatal(err)
			}
			provider := restartProvider(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
				requests++
				canonical, err := protocol.PythonJSON(request.Tools, false, true)
				if err != nil || canonical != row.Sessions[0].SnapshotJSON {
					t.Fatal("restored model catalogue changed", canonical, err)
				}
				reply := protocol.ModelReply{ID: "reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: request.Model, StopReason: protocol.StopEndTurn, Content: []protocol.Block{protocol.NewTextBlock("done")}}
				if requests%2 != 0 {
					input, err := protocol.DecodeModelToolInput("greet", []byte(`{"name":"World"}`))
					if err != nil {
						return protocol.ModelReply{}, err
					}
					reply.Content = []protocol.Block{protocol.NewToolUse("custom", input)}
					reply.StopReason = protocol.StopToolUse
				}
				return reply, nil
			})
			makeManager := func(store *Store) *agent.SessionManager {
				manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(root, "ws"), Defaults: agent.SessionDefaults{Model: "model", MaxTokens: 64, MaxRounds: 2, PermissionMode: agent.ModeAuto}, Services: agent.ManagerServices{StateStore: store, Provider: provider, Tools: catalog}})
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
			if _, err := session.Run(ctx, "greet"); err != nil {
				t.Fatal(err)
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
			found := false
			for _, record := range events {
				if event, ok := record.Event.ToolCatalog(); ok {
					canonical, err := protocol.PythonJSON(event.Schemas, false, true)
					if err != nil || canonical != row.Sessions[0].SnapshotJSON || event.Fingerprint != row.Sessions[0].Fingerprint {
						t.Fatal("SQLite schema event lost metadata", canonical, err)
					}
					found = true
				}
			}
			if !found {
				t.Fatal("missing stored catalogue")
			}
			manager = makeManager(store)
			restored, err := manager.RestoreSessions(ctx)
			if err != nil || len(restored) != 1 || restored[0].Owner() != "alice" || restored[0].ID() != id {
				t.Fatal("owned restore failed", restored, err)
			}
			if _, err := manager.Get("bob", id); err == nil {
				t.Fatal("foreign owner acquired restored extension")
			}
			if restored[0].Info().PermissionMode != agent.ModeInteractive {
				t.Fatal("restore reinstated old auto authority")
			}
			if _, err := restored[0].Run(ctx, "greet again"); err != nil {
				t.Fatal(err)
			}
			if calls != 2 || requests != 4 {
				t.Fatal("restored extension did not execute one fresh typed call", calls, requests)
			}
			if state := restored[0].PersistenceStatus(); state.Error != nil || !state.Restored || !state.LeaseConfirmed {
				t.Fatal(state)
			}
		})
	}
}
