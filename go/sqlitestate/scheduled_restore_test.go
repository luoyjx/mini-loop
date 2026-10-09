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
	"github.com/luoyjx/mini-loop/go/workspace"
)

type scheduledRecipe struct {
	Name           string               `json:"name"`
	Initial        []protocol.Message   `json:"initial"`
	Messages       []protocol.Message   `json:"messages"`
	Owner          agent.OwnerID        `json:"owner"`
	Bound          bool                 `json:"bound"`
	WorkspaceKind  string               `json:"workspace_kind"`
	FactoryCalls   []string             `json:"factory_calls"`
	Mode           agent.PermissionMode `json:"mode"`
	RunCount       int                  `json:"run_count"`
	Status         agent.SessionStatus  `json:"status"`
	Confirmed      bool                 `json:"confirmed"`
	SavedCount     int                  `json:"saved_before_count"`
	SavedSystem    *string              `json:"saved_before_system"`
	SavedWorkspace *string              `json:"saved_before_workspace_kind"`
	Repaired       []string             `json:"repaired"`
	ModelCalls     int                  `json:"model_calls"`
	RunError       *string              `json:"run_error"`
	RunResult      *string              `json:"run_result"`
}

type scheduledWorkspaceFactory func(context.Context, agent.SessionID) (string, error)

func (f scheduledWorkspaceFactory) WorkspaceFor(ctx context.Context, id agent.SessionID) (string, error) {
	return f(ctx, id)
}

func sameScheduledSystem(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func TestScheduledNativeSQLiteRestoreMatchesSourceRecipes(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-scheduled-restore.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []scheduledRecipe `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Cases) != 7 {
		t.Fatal("Source scheduled corpus", err, len(fixture.Cases))
	}
	for _, recipe := range fixture.Cases {
		t.Run(recipe.Name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			path := filepath.Join(root, "state.db")
			store := openStore(t, path)
			var row agent.SessionRecord
			if len(recipe.Initial) > 0 {
				system := "saved custom system"
				row = agent.SessionRecord{SessionID: "stable", Workspace: filepath.Join(root, "recorded"), System: &system, CreatedAt: 20, RunCount: 3, Status: agent.StatusRunning, Owner: "alice", WorkspaceBound: recipe.Bound, Todos: []protocol.TodoItem{{Content: "todo", Status: "pending", ActiveForm: "doing"}}, PendingSteering: []string{"queued"}}
				row.Workspace, err = workspace.ResolvePath(row.Workspace)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.UpsertSession(ctx, row); err != nil {
					t.Fatal(err)
				}
				if _, err := store.AppendMessages(ctx, row.SessionID, recipe.Initial, 3); err != nil {
					t.Fatal(err)
				}
			}
			if recipe.Name == "foreign-bound" {
				if ok, err := store.AcquireLease(ctx, "stable", "foreign", time.Hour); err != nil || !ok {
					t.Fatal(ok, err)
				}
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openStore(t, path)
			fresh, err := workspace.ResolvePath(filepath.Join(root, "factory"))
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			factoryCalls := []string{}
			config := agent.ManagerConfig{
				WorkspaceRoot: root,
				Defaults:      agent.SessionDefaults{PermissionMode: agent.ModeAuto, MaxRounds: 2},
				WorkspaceFactory: scheduledWorkspaceFactory(func(_ context.Context, id agent.SessionID) (string, error) {
					factoryCalls = append(factoryCalls, string(id))
					return fresh, nil
				}),
				Services: agent.ManagerServices{StateStore: store, SystemBuilder: agent.FixedSystem("current scheduled builder"), Provider: restartProvider(func(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
					calls++
					if err := request.Validate(); err != nil || request.System == nil || !strings.Contains(*request.System, "current scheduled builder") || strings.Contains(*request.System, "saved custom system") {
						t.Fatal("scheduled request/system", request.System, err)
					}
					if recipe.Name != "missing-null" {
						count, err := store.MessageCount(ctx, "stable", nil)
						if err != nil || count < len(recipe.Messages)+1 {
							t.Fatal("scheduled provider preceded native flush", count, err)
						}
					}
					return protocol.ModelReply{ID: "reply", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: request.Model, Content: []protocol.Block{protocol.NewTextBlock("scheduled complete")}, StopReason: protocol.StopEndTurn}, nil
				})},
			}
			if recipe.Name == "missing-null" {
				config.Services.StateStore = nil
			}
			manager, err := agent.NewSessionManager(config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { stopRestartManager(t, manager) })
			session, err := manager.RestoreScheduledSession(ctx, "stable")
			if err != nil {
				t.Fatal(err)
			}
			if again, err := manager.RestoreScheduledSession(ctx, "stable"); err != nil || again != session {
				t.Fatal("scheduled handle not reused", err)
			}
			info := session.Info()
			kind := "factory"
			if info.Workspace == row.Workspace {
				kind = "recorded"
			}
			if info.ID != "stable" || session.Owner() != recipe.Owner || info.WorkspaceBound != recipe.Bound || kind != recipe.WorkspaceKind || info.PermissionMode != recipe.Mode || info.RunCount != recipe.RunCount || info.Status != recipe.Status || info.Busy || !reflect.DeepEqual(factoryCalls, recipe.FactoryCalls) || !sameRestartMessages(session.Messages(), recipe.Messages) {
				t.Fatal("scheduled projection", info, kind, factoryCalls, session.Messages())
			}
			state := session.PersistenceStatus()
			if state.LeaseConfirmed != recipe.Confirmed || len(state.RepairedToolUses) != len(recipe.Repaired) {
				t.Fatal(state)
			}
			rows, err := store.LoadSessions(ctx)
			if err != nil || len(rows) != recipe.SavedCount {
				t.Fatal("missing row created before claim", rows, err)
			}
			if len(rows) > 0 {
				kind := "factory"
				if rows[0].Workspace == row.Workspace {
					kind = "recorded"
				}
				if !sameScheduledSystem(rows[0].System, recipe.SavedSystem) || recipe.SavedWorkspace == nil || kind != *recipe.SavedWorkspace || !reflect.DeepEqual(rows[0].Todos, row.Todos) || !reflect.DeepEqual(rows[0].PendingSteering, row.PendingSteering) {
					t.Fatal("saved scheduled projection/metadata", rows)
				}
			}
			result, err := session.Run(ctx, "[Scheduled cron fixture] continue")
			if calls != recipe.ModelCalls || recipe.RunError != nil && !errors.Is(err, agent.ErrSessionLeaseLost) || recipe.RunError == nil && (err != nil || recipe.RunResult == nil || result != *recipe.RunResult) {
				t.Fatal("resumed scheduled request", calls, result, err)
			}
			rows, err = store.LoadSessions(ctx)
			if err != nil || recipe.Name == "missing-sql" && len(rows) != 0 {
				t.Fatal("missing-row refusal wrote state", rows, err)
			}
			if recipe.Name == "scratch-clean" || recipe.Name == "scratch-crash" {
				if rows[0].Workspace != fresh || rows[0].System != nil {
					t.Fatal("growth did not publish selected scratch", rows)
				}
			}
			stopRestartManager(t, manager)
		})
	}
}
