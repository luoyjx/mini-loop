package sqlitestate

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

type restartProvider func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error)

func (f restartProvider) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	return f(ctx, request)
}

type restartRecipe struct {
	Name           string                `json:"name"`
	Initial        []protocol.Message    `json:"initial"`
	Restored       []protocol.Message    `json:"restored"`
	Epoch          agent.TranscriptEpoch `json:"epoch"`
	Status         agent.SessionStatus   `json:"status"`
	Owner          agent.OwnerID         `json:"owner"`
	CreatedAt      float64               `json:"created_at"`
	RunCount       int                   `json:"run_count"`
	Todos          []protocol.TodoItem   `json:"live_todos"`
	Steering       []string              `json:"live_steering"`
	Repaired       []string              `json:"repaired"`
	Confirmed      bool                  `json:"confirmed"`
	SecondMessages int                   `json:"second_messages"`
}

func restartManager(t *testing.T, root string, store *Store, provider agent.Provider) *agent.SessionManager {
	t.Helper()
	manager, err := agent.NewSessionManager(agent.ManagerConfig{
		WorkspaceRoot: root,
		Services:      agent.ManagerServices{Provider: provider, StateStore: store},
		Defaults:      agent.SessionDefaults{PermissionMode: agent.ModeAuto, MaxRounds: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopRestartManager(t, manager) })
	return manager
}

func stopRestartManager(t *testing.T, manager *agent.SessionManager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

func restoreRestartSession(t *testing.T, manager *agent.SessionManager) *agent.ManagedSession {
	t.Helper()
	sessions, err := manager.RestoreSessions(context.Background())
	if err != nil || len(sessions) != 1 {
		t.Fatal("restore", sessions, err)
	}
	return sessions[0]
}

func sameRestartMessages(a, b []protocol.Message) bool {
	return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b)
}

// Replay the actual Python manager corpus through native SQLite, not a test
// StateStore. Every restart closes the database before constructing its successor.
func TestManagerNativeSQLiteRestartMatchesSourceRecipes(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-state-restore.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []restartRecipe `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 7 {
		t.Fatal("source restore corpus", err, len(fixture.Cases))
	}
	for _, recipe := range fixture.Cases {
		t.Run(recipe.Name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			path := filepath.Join(root, "state.db")
			store := openStore(t, path)
			system := "recorded system"
			row := agent.SessionRecord{SessionID: "saved", Workspace: filepath.Join(root, "saved-workspace"), System: &system, CreatedAt: 10, RunCount: 4, Status: agent.StatusRunning, Owner: "tenant", WorkspaceBound: true, Todos: recipe.Todos, PendingSteering: recipe.Steering}
			if err := store.UpsertSession(ctx, row); err != nil {
				t.Fatal(err)
			}
			if _, err := store.AppendMessages(ctx, row.SessionID, recipe.Initial, 2); err != nil {
				t.Fatal(err)
			}
			event, err := agent.DecodeStoredEvent([]byte(`{"type":"status","status":"running","seq":9,"session":"saved","transcript_epoch":2,"ts":10}`))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.AppendEvent(ctx, row.SessionID, event); err != nil {
				t.Fatal(err)
			}
			if recipe.Name == "tools" {
				if err := store.WriteApproval(ctx, agent.ApprovalRecord{ApprovalID: "pending", SessionID: row.SessionID, ToolUseID: "parked", ToolName: protocol.ToolBash, Status: agent.ApprovalPending, Kind: agent.ApprovalPermission}); err != nil {
					t.Fatal(err)
				}
			}
			if recipe.Name == "foreign" {
				if ok, err := store.AcquireLease(ctx, row.SessionID, "foreign", time.Hour); err != nil || !ok {
					t.Fatal(ok, err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openStore(t, path)
			calls := 0
			provider := restartProvider(func(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
				calls++
				if err := request.Validate(); err != nil || request.System == nil || !strings.Contains(*request.System, system) {
					t.Fatal("restored model request", err)
				}
				stored, err := store.LoadMessages(ctx, row.SessionID, nil)
				if err != nil || len(stored) < len(recipe.Restored)+1 {
					t.Fatal("provider preceded durable transcript flush", len(stored), err)
				}
				return protocol.ModelReply{ID: "reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: request.Model, Content: []protocol.Block{protocol.NewTextBlock("finished")}, StopReason: protocol.StopEndTurn}, nil
			})
			manager := restartManager(t, root, store, provider)
			session := restoreRestartSession(t, manager)
			info, state := session.Info(), session.PersistenceStatus()
			if info.ID != row.SessionID || session.Owner() != recipe.Owner || info.Status != recipe.Status || info.Busy || info.PermissionMode != agent.ModeInteractive || info.CreatedAt != recipe.CreatedAt || info.RunCount != recipe.RunCount || !info.WorkspaceBound || !reflect.DeepEqual(info.Todos, recipe.Todos) || info.PendingSteering != len(recipe.Steering) {
				t.Fatal("restored identity/metadata", info)
			}
			if !state.Restored || state.LeaseConfirmed != recipe.Confirmed || state.RestorePending == recipe.Confirmed {
				t.Fatal(state)
			}
			if _, err := os.Stat(row.Workspace); err != nil {
				t.Fatal("bound workspace not recreated", err)
			}
			if _, err := manager.Get("foreign", row.SessionID); !errors.Is(err, agent.ErrSessionNotFound) {
				t.Fatal("owner admission", err)
			}
			if again, err := manager.RestoreSessions(ctx); err != nil || len(again) != 0 {
				t.Fatal("duplicate restore", again, err)
			}
			if recipe.Name == "foreign" {
				if !sameRestartMessages(session.Messages(), recipe.Initial) {
					t.Fatal("foreign lease allowed repair")
				}
				if _, err := session.Run(ctx, "blocked"); !errors.Is(err, agent.ErrSessionLeaseLost) || calls != 0 {
					t.Fatal("foreign lease admitted model", calls, err)
				}
				if err := store.ReleaseLease(ctx, row.SessionID, "foreign"); err != nil {
					t.Fatal(err)
				}
			} else {
				if !sameRestartMessages(session.Messages(), recipe.Restored) || !reflect.DeepEqual(state.RepairedToolUses, recipe.Repaired) {
					t.Fatal("repair differs from Source", session.Messages(), state)
				}
				epoch, err := store.TranscriptEpoch(ctx, row.SessionID)
				if err != nil || epoch != recipe.Epoch {
					t.Fatal("repair epoch", epoch, err)
				}
				if recipe.Name == "tools" {
					approvals, err := store.ReadApprovals(ctx, row.SessionID, nil)
					if err != nil || len(approvals) != 1 || approvals[0].Status != agent.ApprovalExpired || approvals[0].ResolvedAt == nil {
						t.Fatal("approval not expired", approvals, err)
					}
				}
				stopRestartManager(t, manager)
				if holder, _, err := store.LeaseHolder(ctx, row.SessionID); err != nil || holder != "" {
					t.Fatal("stop retained lease", holder, err)
				}
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				store = openStore(t, path)
				manager = restartManager(t, root, store, provider)
				session = restoreRestartSession(t, manager)
				if len(session.Messages()) != recipe.SecondMessages || !reflect.DeepEqual(session.Info().Todos, recipe.Todos) || session.Info().PendingSteering != len(recipe.Steering) {
					t.Fatal("second restart lost repair/metadata", session.Info())
				}
			}
			if _, err := session.Run(ctx, "continue"); err != nil || calls != 1 {
				t.Fatal("first resumed request", calls, err)
			}
			events, err := session.CatchUpEvents(ctx, 9)
			if err != nil || len(events) == 0 {
				t.Fatal("native event catch-up", len(events), err)
			}
			for i, event := range events {
				if event.SessionID != row.SessionID || event.Sequence <= 9 || i > 0 && event.Sequence <= events[i-1].Sequence {
					t.Fatal("stored event sequence/scope", event)
				}
			}
			stopRestartManager(t, manager)
		})
	}
}

func TestManagerCreatedSessionSurvivesNativeDatabaseReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "state.db")
	store := openStore(t, path)
	var requests []protocol.ModelRequest
	provider := restartProvider(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		requests = append(requests, request.Clone())
		return protocol.ModelReply{ID: "reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: request.Model, Content: []protocol.Block{protocol.NewTextBlock("finished")}, StopReason: protocol.StopEndTurn}, nil
	})
	manager := restartManager(t, root, store, provider)
	session, err := manager.Create(ctx, agent.CreateSessionRequest{Owner: "tenant"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Run(ctx, "first question"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Steer("remember after restart"); err != nil {
		t.Fatal(err)
	}
	before := session.Info()
	history := session.Messages()
	stopRestartManager(t, manager)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, path)
	manager = restartManager(t, root, store, provider)
	session = restoreRestartSession(t, manager)
	if session.ID() != before.ID || session.Owner() != "tenant" || session.Info().CreatedAt != before.CreatedAt || session.Info().RunCount != 1 || session.Info().PendingSteering != 1 || !sameRestartMessages(session.Messages(), history) {
		t.Fatal("lost manager-created durable session", before, session.Info())
	}
	snapshot, err := session.ReadTranscript(ctx, agent.TranscriptSelection{})
	if err != nil || !sameRestartMessages(snapshot.Messages, history) {
		t.Fatal("native managed transcript read", snapshot, err)
	}
	if _, err := session.Run(ctx, "second question"); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || len(requests[1].Messages) <= len(history) || !sameRestartMessages(requests[1].Messages[:len(history)], history) {
		t.Fatal("provider did not receive durable request prefix", requests)
	}
	encoded, err := json.Marshal(requests[1].Messages)
	if err != nil || !strings.Contains(string(encoded), "remember after restart") {
		t.Fatal("provider did not receive durable steering", string(encoded), err)
	}
	if state := session.PersistenceStatus(); state.Error != nil || !state.LeaseConfirmed || state.RestorePending {
		t.Fatal("native persistence degraded", state)
	}
	stopRestartManager(t, manager)
}
