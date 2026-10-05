package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/cron"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type scheduledRestoreRecipe struct {
	Name             string             `json:"name"`
	Initial          []protocol.Message `json:"initial"`
	Messages         []protocol.Message `json:"messages"`
	Owner            OwnerID            `json:"owner"`
	Bound            bool               `json:"bound"`
	WorkspaceKind    string             `json:"workspace_kind"`
	FactoryCalls     []string           `json:"factory_calls"`
	System           *string            `json:"system"`
	Mode             PermissionMode     `json:"mode"`
	RunCount         int                `json:"run_count"`
	Status           SessionStatus      `json:"status"`
	Confirmed        bool               `json:"confirmed"`
	SavedBeforeCount int                `json:"saved_before_count"`
	SavedSystem      *string            `json:"saved_before_system"`
	SavedWorkspace   *string            `json:"saved_before_workspace_kind"`
	Repaired         []string           `json:"repaired"`
	ModelCalls       int                `json:"model_calls"`
	RunError         *string            `json:"run_error"`
	RunResult        *string            `json:"run_result"`
}

func scheduledRestoreRecipes(t *testing.T) []scheduledRestoreRecipe {
	t.Helper()
	b, err := os.ReadFile("../testdata/python-scheduled-restore.json")
	if err != nil {
		t.Fatal(err)
	}
	var x struct {
		Cases []scheduledRestoreRecipe `json:"cases"`
	}
	if err = json.Unmarshal(b, &x); err != nil || len(x.Cases) != 7 {
		t.Fatal(err)
	}
	return x.Cases
}
func seedScheduled(t *testing.T, store *runtimeStateStore, initial []protocol.Message, bound bool) SessionRecord {
	t.Helper()
	system := "saved custom system"
	row := SessionRecord{SessionID: "stable", Workspace: filepath.Join(t.TempDir(), "recorded"), System: &system, CreatedAt: 20, RunCount: 3, Status: StatusRunning, Owner: "alice", WorkspaceBound: bound, Todos: []protocol.TodoItem{{Content: "todo", Status: "pending", ActiveForm: "doing"}}, PendingSteering: []string{"queued"}}
	row.Workspace, _ = workspace.ResolvePath(row.Workspace)
	if err := store.UpsertSession(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendMessages(context.Background(), row.SessionID, initial, 3); err != nil {
		t.Fatal(err)
	}
	return row
}
func TestScheduledRestoreActualPythonRecipe(t *testing.T) {
	for _, recipe := range scheduledRestoreRecipes(t) {
		t.Run(recipe.Name, func(t *testing.T) {
			store := newRuntimeStateStore()
			var row SessionRecord
			if len(recipe.Initial) > 0 {
				row = seedScheduled(t, store, recipe.Initial, recipe.Bound)
			}
			if recipe.Name == "foreign-bound" {
				store.holders["stable"] = "foreign"
			}
			calls := 0
			factoryCalls := []string{}
			fresh, _ := workspace.ResolvePath(filepath.Join(t.TempDir(), "factory"))
			provider := stateProviderFunc(func(_ context.Context, req protocol.ModelRequest) (protocol.ModelReply, error) {
				calls++
				if err := req.Validate(); err != nil {
					t.Fatal("invalid resumed request", err)
				}
				if req.System == nil || !strings.Contains(*req.System, "current scheduled builder") || strings.Contains(*req.System, "saved custom system") {
					t.Fatal("scheduled system contract", req.System)
				}
				if recipe.Name != "missing-null" {
					count, _ := store.MessageCount(context.Background(), "stable", nil)
					if count < len(recipe.Messages)+1 {
						t.Fatal("request before flush", count)
					}
				}
				reply := stateFinal()
				reply.Content = []protocol.Block{protocol.NewTextBlock("scheduled complete")}
				return reply, nil
			})
			config := managerTestConfig(t.TempDir(), provider)
			config.Services.StateStore = store
			if recipe.Name == "missing-null" {
				config.Services.StateStore = nil
			}
			config.Services.SystemBuilder = FixedSystem("current scheduled builder")
			defaultSystem := "new-session default"
			config.Defaults.System = &defaultSystem
			config.WorkspaceFactory = workspaceFactoryFunc(func(_ context.Context, id SessionID) (string, error) {
				factoryCalls = append(factoryCalls, string(id))
				return fresh, nil
			})
			manager := makeManager(t, config)
			session, err := manager.RestoreScheduledSession(context.Background(), "stable")
			if err != nil {
				t.Fatal(err)
			}
			again, err := manager.RestoreScheduledSession(context.Background(), "stable")
			if err != nil || session != again {
				t.Fatal("did not reuse handle", err)
			}
			info := session.Info()
			kind := "factory"
			if info.Workspace == row.Workspace {
				kind = "recorded"
			}
			if info.ID != "stable" || session.Owner() != recipe.Owner || info.WorkspaceBound != recipe.Bound || kind != recipe.WorkspaceKind || info.PermissionMode != recipe.Mode || info.RunCount != recipe.RunCount || info.Status != recipe.Status || info.Busy || !reflect.DeepEqual(factoryCalls, recipe.FactoryCalls) || !sameSystem(session.core.explicitSystem, recipe.System) {
				t.Fatal(info, kind, factoryCalls)
			}
			if _, err = os.Stat(info.Workspace); err != nil {
				t.Fatal(err)
			}
			equalRestoreMessages(t, session.Messages(), recipe.Messages)
			status := session.PersistenceStatus()
			if status.LeaseConfirmed != recipe.Confirmed || len(status.RepairedToolUses) != len(recipe.Repaired) {
				t.Fatal(status)
			}
			rows, _ := store.LoadSessions(context.Background())
			if len(rows) != recipe.SavedBeforeCount {
				t.Fatal("missing record created before claim", rows)
			}
			if len(rows) > 0 {
				k := "factory"
				if rows[0].Workspace == row.Workspace {
					k = "recorded"
				}
				if !sameSystem(rows[0].System, recipe.SavedSystem) || recipe.SavedWorkspace == nil || k != *recipe.SavedWorkspace {
					t.Fatal(rows, k)
				}
				if !reflect.DeepEqual(rows[0].Todos, row.Todos) || !reflect.DeepEqual(rows[0].PendingSteering, row.PendingSteering) {
					t.Fatal("repair erased metadata")
				}
				if _, err = manager.Get("other", session.ID()); !errors.Is(err, ErrSessionNotFound) {
					t.Fatal("lost owner", err)
				}
			}
			result, err := session.Run(context.Background(), "[Scheduled cron fixture] continue")
			if calls != recipe.ModelCalls {
				t.Fatal(calls, recipe.ModelCalls)
			}
			if recipe.RunError != nil {
				if !errors.Is(err, ErrSessionLeaseLost) {
					t.Fatal(err)
				}
			} else {
				if err != nil || recipe.RunResult == nil || result != *recipe.RunResult {
					t.Fatal(result, err)
				}
			}
			if recipe.Name == "missing-sql" {
				rows, _ = store.LoadSessions(context.Background())
				if len(rows) != 0 {
					t.Fatal("missing-row refusal wrote state")
				}
			}
			if recipe.Name == "scratch-clean" || recipe.Name == "scratch-crash" {
				rows, _ = store.LoadSessions(context.Background())
				if rows[0].Workspace != fresh || rows[0].System != nil {
					t.Fatal("growth did not publish selected scratch", rows)
				}
			}
		})
	}
}
func TestScheduledRestoreLiveBypassesStorageAndClosedManagerRefuses(t *testing.T) {
	store := newRuntimeStateStore()
	cfg := managerTestConfig(t.TempDir(), cronDoneProvider{})
	cfg.Services.StateStore = store
	manager := makeManager(t, cfg)
	system := "live system"
	live := createManaged(t, manager, CreateSessionRequest{Owner: "alice", System: &system, PermissionMode: ModeAuto})
	store.faults["sessions"] = errors.New("read unavailable")
	got, err := manager.RestoreScheduledSession(context.Background(), live.ID())
	if err != nil || got != live || got.Info().PermissionMode != ModeAuto || got.core.explicitSystem == nil {
		t.Fatal(got, err)
	}
	if _, err = manager.RestoreScheduledSession(context.Background(), ""); !errors.Is(err, ErrStateRestore) {
		t.Fatal(err)
	}
	if _, err = manager.RestoreScheduledSession(context.Background(), "missing"); err == nil {
		t.Fatal("ignored read failure")
	}
	if err = manager.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.RestoreScheduledSession(context.Background(), live.ID()); !errors.Is(err, ErrManagerStopped) {
		t.Fatal(err)
	}
}
func TestScheduledRestorePendingScratchRetriesOwnProjection(t *testing.T) {
	store := newRuntimeStateStore()
	row := seedScheduled(t, store, []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("crash")}}, false)
	store.holders[row.SessionID] = "foreign"
	fresh, _ := workspace.ResolvePath(filepath.Join(t.TempDir(), "factory"))
	cfg := managerTestConfig(t.TempDir(), cronDoneProvider{})
	cfg.Services.StateStore = store
	cfg.WorkspaceFactory = workspaceFactoryFunc(func(context.Context, SessionID) (string, error) { return fresh, nil })
	manager := makeManager(t, cfg)
	session, err := manager.RestoreScheduledSession(context.Background(), row.SessionID)
	if err != nil || !session.PersistenceStatus().RestorePending {
		t.Fatal(err)
	}
	store.mu.Lock()
	delete(store.holders, row.SessionID)
	store.faults["renew"] = errors.New("renew failed after projection")
	store.mu.Unlock()
	if _, err = session.Run(context.Background(), "continue"); err == nil {
		t.Fatal("failed repair admitted")
	}
	store.mu.Lock()
	saved := store.rows[row.SessionID]
	delete(store.faults, "renew")
	store.mu.Unlock()
	if saved.Workspace != fresh || saved.System != nil {
		t.Fatal("partial write not present", saved)
	}
	// Projection identity advances with a successful upsert even if renewal then
	// faults; a retry must load its own row without mistaking it for foreign state.
	if _, err = session.Run(context.Background(), "continue"); err != nil {
		t.Fatal("own projection rejected on retry", err)
	}
	if session.Info().PendingSteering != 0 || session.PersistenceStatus().RestorePending {
		t.Fatal(session.Info())
	}
}
func TestManagedCronLazyRestorationRequiresNewArmAndFreshAuthority(t *testing.T) {
	store := newRuntimeStateStore()
	row := seedScheduled(t, store, []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("crash")}}, true)
	root := t.TempDir()
	operator, err := cron.New(cron.Config{DurablePath: filepath.Join(root, ".cron.json"), Resolver: cron.ResolverFunc(func(cron.SessionID) (cron.Runner, error) { return nil, nil })})
	if err != nil {
		t.Fatal(err)
	}
	job, err := operator.Schedule(cron.Request{Session: cron.SessionID(row.SessionID), Cron: "30 12 5 10 *", Prompt: "continue", Recurring: boolPointer(false)})
	if err != nil {
		t.Fatal(err)
	}
	var snapshots []RunContextSnapshot
	var mu sync.Mutex
	calls := 0
	cfg := managerTestConfig(root, stateProviderFunc(func(_ context.Context, req protocol.ModelRequest) (protocol.ModelReply, error) {
		calls++
		if err := req.Validate(); err != nil {
			t.Fatal(err)
		}
		return stateFinal(), nil
	}))
	cfg.Services.StateStore = store
	cfg.Services.UserPromptHooks = []UserPromptHook{promptHookFunc(func(_ context.Context, v TurnContext, _ string) (*string, error) {
		mu.Lock()
		snapshots = append(snapshots, v.Authority.RunContext.Snapshot())
		mu.Unlock()
		return nil, nil
	})}
	manager := makeManager(t, cfg)
	if _, err = manager.Get(row.Owner, row.SessionID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("restored eagerly", err)
	}
	manager.cron.Tick(cronTime())
	waitCron(t, manager)
	if calls != 0 || manager.cron.Armed(job.Job.ID) {
		t.Fatal("job activated on boot")
	}
	if text := manager.cron.Arm(job.Job.ID, nil); !strings.Contains(text, "Armed") {
		t.Fatal(text)
	}
	manager.cron.Tick(cronTime())
	waitCron(t, manager)
	session, err := manager.Get(row.Owner, row.SessionID)
	if err != nil || calls != 1 || session.Info().RunCount != 4 {
		t.Fatal(err, calls)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(snapshots) != 1 || snapshots[0].Authority != AuthorityUntrusted || snapshots[0].ActorID != nil || len(snapshots[0].ApprovedCapabilities) != 0 {
		t.Fatal(snapshots)
	}
	if len(manager.cron.Jobs()) != 0 {
		t.Fatal("one-shot retained")
	}
}

type cancellableRestoreInventory struct {
	*runtimeStateStore
	entered chan struct{}
}

func (s *cancellableRestoreInventory) LoadSessions(ctx context.Context) ([]SessionRecord, error) {
	close(s.entered)
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestManagedCronStopCancelsPendingRestoreInventory(t *testing.T) {
	backing := newRuntimeStateStore()
	store := &cancellableRestoreInventory{backing, make(chan struct{})}
	cfg := managerTestConfig(t.TempDir(), cronDoneProvider{})
	cfg.Services.StateStore = store
	manager := makeManager(t, cfg)
	_, err := manager.cron.Schedule(cron.Request{Session: "missing", Cron: "30 12 5 10 *", Prompt: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	manager.cron.Tick(cronTime())
	<-store.entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = manager.Stop(ctx); err != nil {
		t.Fatal("stop failed to cancel restore read", err)
	}
	if len(manager.List("anonymous")) != 0 {
		t.Fatal("late publication")
	}
}
func TestManagedCronReportsMissingStoredRowWithoutCreatingIt(t *testing.T) {
	store := newRuntimeStateStore()
	cfg := managerTestConfig(t.TempDir(), stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
		t.Fatal("missing row reached model")
		return stateFinal(), nil
	}))
	cfg.Services.StateStore = store
	manager := makeManager(t, cfg)
	_, err := manager.cron.Schedule(cron.Request{Session: "missing", Cron: "30 12 5 10 *", Prompt: "continue", Recurring: boolPointer(false)})
	if err != nil {
		t.Fatal(err)
	}
	manager.cron.Tick(cronTime())
	waitCron(t, manager)
	rows, _ := store.LoadSessions(context.Background())
	if len(rows) != 0 || len(manager.cron.Jobs()) != 0 {
		t.Fatal(rows)
	}
	problems := manager.cron.Problems().Summary()
	if len(problems) != 1 || !strings.Contains(problems[0], "session_lease_lost") {
		t.Fatal("lost occurrence not reported", problems)
	}
}
