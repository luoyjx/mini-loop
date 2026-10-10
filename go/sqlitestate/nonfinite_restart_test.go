package sqlitestate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/provider"
)

func TestNonfiniteManagedSQLiteRestoreAndCatchupRemainUntrusted(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-provider-nonfinite-stream.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Store, Name string
			Events      string `json:"tool_events_json"`
			Restore     *struct {
				Owner          string
				Mode           string `json:"permission_mode"`
				ResumeRequests int    `json:"resume_transport_requests"`
			}
		}
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	matched := 0
	for _, row := range fixture.Cases {
		if row.Store != "sqlite" {
			continue
		}
		matched++
		t.Run(row.Name, func(t *testing.T) {
			if row.Restore == nil {
				t.Fatal("Source restore capture missing")
			}
			values, err := jsonvalue.Decode(row.Events)
			if err != nil {
				t.Fatal(err)
			}
			events, _ := values.Array()
			input, _ := events[0].Lookup("input")
			raw, err := input.MarshalLegacyUTF8()
			if err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"id":"message","type":"message","role":"assistant","model":"model","content":[{"type":"tool_use","id":"call","name":"bash","input":%s}],"stop_reason":"tool_use","usage":{"input_tokens":1,"output_tokens":0}}`, raw)
			}))
			defer server.Close()
			model, err := provider.New(provider.Config{APIKey: "fixture-key", BaseURL: server.URL})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			root := t.TempDir()
			path := filepath.Join(root, "state.db")
			store := openStore(t, path)
			newManager := func() *agent.SessionManager {
				manager, err := agent.NewSessionManager(agent.ManagerConfig{WorkspaceRoot: filepath.Join(root, "ws"), Defaults: agent.SessionDefaults{Model: "model", PermissionMode: agent.ModeAuto, MaxRounds: 3}, Services: agent.ManagerServices{StateStore: store, Provider: model}})
				if err != nil {
					t.Fatal(err)
				}
				return manager
			}
			manager := newManager()
			session, err := manager.Create(ctx, agent.CreateSessionRequest{Owner: agent.OwnerID(row.Restore.Owner)})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = session.Run(ctx, "hello"); err != nil {
				t.Fatal(err)
			}
			before, err := protocol.ObservedMessagesPythonJSON(session.Messages(), false, true)
			if err != nil {
				t.Fatal(err)
			}
			if len(session.Messages()) != 4 {
				t.Fatal("initial Source transcript shape changed", len(session.Messages()))
			}
			stopRestartManager(t, manager)
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openStore(t, path)
			manager = newManager()
			session = restoreRestartSession(t, manager)
			defer stopRestartManager(t, manager)
			after, err := protocol.ObservedMessagesPythonJSON(session.Messages(), false, true)
			if err != nil || before != after {
				t.Fatal("restore changed observations", before, after, err)
			}
			if string(session.Owner()) != row.Restore.Owner || string(session.Info().PermissionMode) != row.Restore.Mode {
				t.Fatal(session.Owner(), session.Info().PermissionMode, row.Restore)
			}
			caught, err := session.CatchUpEvents(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}
			uses := 0
			for _, record := range caught {
				if record.Scope.RunContext.Authority() != agent.AuthorityUntrusted {
					t.Fatal("catch-up reconstructed live authority")
				}
				if use, ok := record.Event.ToolUse(); ok {
					uses++
					observation, err := use.Input.Observation()
					if err != nil {
						t.Fatal(err)
					}
					bytes, err := observation.LegacyJSON()
					if err != nil || string(bytes) != string(raw) {
						t.Fatal(string(bytes), string(raw), err)
					}
					if _, exists, err := store.ReadAction(ctx, use.ActionID); err != nil || exists {
						t.Fatal("restore created action", exists, err)
					}
				}
			}
			if uses != 1 {
				t.Fatal("historical tool event lost", uses)
			}
			count := requests.Load()
			if _, err = session.Run(ctx, "resume"); err != nil {
				t.Fatal(err)
			}
			if requests.Load()-count != int32(row.Restore.ResumeRequests) {
				t.Fatal("restored request reached transport")
			}
			if len(session.Messages()) != 6 {
				t.Fatal("resumed Source transcript shape changed", len(session.Messages()))
			}
			if status := session.PersistenceStatus(); status.Error != nil || !status.LeaseConfirmed {
				t.Fatal(status)
			}
		})
	}
	if matched != 2 {
		t.Fatal("Source SQLite restore inventory changed", matched)
	}
}
