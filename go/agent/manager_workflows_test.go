package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
)

type managerWorkflowCall struct {
	Rounds    int  `json:"rounds"`
	Workspace bool `json:"workspace_bound"`
	ModelPool bool `json:"model_pool_shared"`
	ToolPool  bool `json:"tool_pool_shared"`
}
type managerWorkflowProjection struct {
	Name          string                `json:"name"`
	Error         string                `json:"error"`
	Detail        string                `json:"detail"`
	Enabled       bool                  `json:"enabled"`
	Tools         []protocol.ToolName   `json:"tools"`
	JournalShared bool                  `json:"journal_shared"`
	PoolShared    bool                  `json:"pool_shared"`
	PoolCapacity  int                   `json:"pool_capacity"`
	Status        workflows.RunStatus   `json:"status"`
	Reason        *string               `json:"reason"`
	Calls         []managerWorkflowCall `json:"worker_calls"`
	Deleted       bool                  `json:"delete_returned"`
	Before        bool                  `json:"exists_while_draining"`
	After         bool                  `json:"exists_after"`
	ChildTools    []protocol.ToolName   `json:"child_tools"`
	ForkShared    bool                  `json:"fork_shared"`
}

func workflowNames(session *ManagedSession) []protocol.ToolName {
	names := []protocol.ToolName{}
	for _, name := range session.core.gate.catalog.Names() {
		if name == protocol.ToolWorkflow || name == protocol.ToolWorkflowStatus || name == protocol.ToolWorkflowCancel {
			names = append(names, name)
		}
	}
	return names
}

func TestManagerWorkflowsMatchActualPython(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-manager-workflows.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Rows []managerWorkflowProjection }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, expected := range fixture.Rows {
		t.Run(expected.Name, func(t *testing.T) {
			ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			config := managerTestConfig(t.TempDir(), &FakeProvider{})
			config.Defaults.Model = "manager-model"
			config.Services.WorkflowTools = expected.Name != "disabled"
			config.Services.TeamTools = true
			if expected.Name == "custom-caps" {
				caps := workflows.DefinitionCaps{MaxConcurrentAgents: 1, MaxAgents: 2, MaxRounds: 3, WallTimeSeconds: 10}
				config.Services.WorkflowCaps = &caps
			}
			var manager *SessionManager
			var parent *ManagedSession
			started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			defer releaseOnce.Do(func() { close(release) })
			calls := []managerWorkflowCall{}
			var callMu sync.Mutex
			factory := WorkflowWorkerFactory(func(worker WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
				return workflows.WorkflowRunnerFunc(func(workerCtx context.Context, _ workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
					callMu.Lock()
					calls = append(calls, managerWorkflowCall{*worker.MaxRounds, worker.Workspace == parent.core.workspace, worker.ModelLimiter == manager.config.Services.ModelLimiter, worker.ToolLimiter == manager.config.Services.ToolLimiter})
					callMu.Unlock()
					close(started)
					if expected.Name == "delete-running" || expected.Name == "delete-preserve" || expected.Name == "stop-running" {
						<-workerCtx.Done()
						close(cancelled)
						<-release
						return nil, workerCtx.Err()
					}
					result := workflows.NewArtifactSubmission(jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "ok", Value: jsonvalue.BoolValue(true)}}), "return_artifact")
					return &result, nil
				}), nil
			})
			if expected.Name == "injected" || expected.Name == "same-pool" || expected.Name == "conflicting-pool" {
				journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
				pool, _ := workflows.NewAttemptPool(2)
				config.Services.WorkflowService, err = NewWorkflowService(WorkflowServiceConfig{Caps: workflows.DefaultDefinitionCaps(), Journal: journal, ResolveParent: func(id SessionID) (WorkflowParent, bool) {
					session, err := manager.Get("owner", id)
					if err != nil {
						return WorkflowParent{}, false
					}
					return WorkflowParent{Owner: session.Owner(), Workspace: session.core.workspace}, true
				}, AttemptPool: pool, WorkerFactory: factory})
				if err != nil {
					t.Fatal(err)
				}
				defer config.Services.WorkflowService.Close(ctx)
				if expected.Name == "same-pool" {
					config.Services.WorkflowAttemptPool = pool
				}
				if expected.Name == "conflicting-pool" {
					config.Services.WorkflowAttemptPool, _ = workflows.NewAttemptPool(2)
				}
			}
			manager, err = NewSessionManager(config)
			if expected.Error != "" {
				if err == nil || err.Error() != expected.Detail {
					t.Fatal("constructor refusal", err, expected.Detail)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer manager.Stop(ctx)
			parent = createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
			service := manager.Workflows()
			if service != nil && config.Services.WorkflowService == nil {
				service.config.WorkerFactory = factory
			}
			actual := managerWorkflowProjection{Name: expected.Name, Tools: workflowNames(parent), Before: true, After: true, Calls: []managerWorkflowCall{}, ChildTools: []protocol.ToolName{}}
			actual.Enabled = service != nil
			if service != nil {
				actual.JournalShared = service.config.Journal == manager.config.Services.ActionJournal
				actual.PoolShared = service.config.AttemptPool == manager.config.Services.WorkflowAttemptPool
				actual.PoolCapacity = service.config.AttemptPool.Capacity()
			}
			if expected.Name == "fork" {
				child, err := manager.Fork(ctx, "owner", parent.ID())
				if err != nil {
					t.Fatal(err)
				}
				actual.ChildTools = workflowNames(child)
				actual.ForkShared = child.core.runtime.workflows == service
			} else if expected.Name == "teammate" {
				child := spawnMember(t, manager, parent, "peer")
				actual.ChildTools = workflowNames(child)
			} else if expected.Name != "disabled" && expected.Name != "delete-idle" {
				request := nativeWorkflowRequest(t, "action")
				request.SessionID = parent.ID()
				request.LaunchTurn = 1
				request.Input.Definition, err = jsonvalue.Decode(`{"name":"wf","return_from":"a","nodes":[{"id":"a","kind":"agent"}],"budget":{"max_concurrent_agents":1,"max_agents":1,"max_rounds":2,"wall_time_seconds":1}}`)
				if err != nil {
					t.Fatal(err)
				}
				request.ActionInput = nil
				launch, err := service.Launch(ctx, request)
				if err != nil {
					t.Fatal(err)
				}
				if expected.Name == "delete-running" || expected.Name == "delete-preserve" || expected.Name == "stop-running" {
					select {
					case <-started:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					var stopDone chan error
					if expected.Name == "stop-running" {
						stopDone = make(chan error, 1)
						go func() { stopDone <- manager.Stop(ctx) }()
					} else {
						actual.Deleted, err = manager.Delete("owner", parent.ID(), DeleteSessionOptions{PreserveWorkspace: expected.Name == "delete-preserve"})
						if err != nil {
							t.Fatal(err)
						}
					}
					select {
					case <-cancelled:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
					_, err = os.Stat(parent.core.workspace)
					actual.Before = err == nil
					releaseOnce.Do(func() { close(release) })
					if stopDone != nil {
						if err := <-stopDone; err != nil {
							t.Fatal(err)
						}
					} else if err := manager.WaitCleanup(ctx); err != nil {
						t.Fatal(err)
					}
					_, err = os.Stat(parent.core.workspace)
					actual.After = err == nil
				}
				run, err := service.Wait(ctx, launch.RunID)
				if err != nil {
					t.Fatal(err)
				}
				actual.Status, actual.Reason = run.Status, run.CancelReason
			}
			if expected.Name == "delete-idle" {
				actual.Deleted, err = manager.Delete("owner", parent.ID(), DeleteSessionOptions{})
				if err != nil {
					t.Fatal(err)
				}
				if err := manager.WaitCleanup(ctx); err != nil {
					t.Fatal(err)
				}
				_, err = os.Stat(parent.core.workspace)
				actual.After = err == nil
			}
			if err := manager.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			callMu.Lock()
			actual.Calls = append(actual.Calls, calls...)
			callMu.Unlock()
			got, _ := json.Marshal(actual)
			want, _ := json.Marshal(expected)
			if string(got) != string(want) {
				t.Fatalf("actual %s\nsource %s", got, want)
			}
		})
	}
}

func TestManagerWorkflowDefaultsCaptureCapsAndUseFreshWorkers(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	provider := stateProviderFunc(func(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		if len(request.Messages) == 1 {
			return fakeReply([]protocol.Block{protocol.NewToolUse("a", protocol.ReturnArtifactToolInput(jsonvalue.ObjectValue(nil)))}, protocol.StopToolUse), nil
		}
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	})
	caps := workflows.DefaultDefinitionCaps()
	config := managerTestConfig(t.TempDir(), provider)
	config.Services.WorkflowTools = true
	config.Services.WorkflowCaps = &caps
	manager := makeManager(t, config)
	parent := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	caps.MaxAgents = 0
	request := nativeWorkflowRequest(t, "action")
	request.SessionID = parent.ID()
	launched, err := manager.Workflows().Launch(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	run, err := manager.Workflows().Wait(ctx, launched.RunID)
	if err != nil || run.Status != workflows.RunCompleted {
		t.Fatal(run, err)
	}
	worker := manager.Workflows().config.Worker
	if worker.ModelLimiter != manager.config.Services.ModelLimiter || worker.ToolLimiter != manager.config.Services.ToolLimiter || worker.Skills != manager.config.Services.Skills {
		t.Fatal("worker dependencies were detached from shared manager services")
	}
	// Owned runtime guards hide stale cached results after deletion.
	authority := parent.core.runtime.binding
	authority.RunContext = request.Context
	call := ToolCall{ID: "status", Input: protocol.WorkflowStatusToolInput(protocol.WorkflowReferenceInput{RunID: run.RunID})}
	first, err := parent.core.gate.Dispatch(ctx, authority, call)
	if err != nil || first.IsError() {
		t.Fatal(first, err)
	}
	if _, err := manager.Delete("foreign", parent.ID(), DeleteSessionOptions{}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal(err)
	}
	if _, err := manager.Delete("owner", parent.ID(), DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := manager.WaitCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	cached, err := parent.core.gate.Dispatch(ctx, authority, call)
	if err != nil || !cached.Denied || cached.Replayed {
		t.Fatal("deleted session replay", cached, err)
	}
}

func TestManagerWorkflowDeleteJoinsTerminalPublication(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var manager *SessionManager
	journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	service, err := NewWorkflowService(WorkflowServiceConfig{Caps: workflows.DefaultDefinitionCaps(), Journal: journal, ResolveParent: func(id SessionID) (WorkflowParent, bool) {
		session, err := manager.Get("owner", id)
		if err != nil {
			return WorkflowParent{}, false
		}
		return WorkflowParent{Owner: "owner", Workspace: session.core.workspace, Events: WorkflowEventSinkFunc(func(_ context.Context, event WorkflowEvent) error {
			if event.Kind == WorkflowFailed {
				close(entered)
				<-release
			}
			return nil
		})}, true
	}, WorkerFactory: func(WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
		return workflows.WorkflowRunnerFunc(func(context.Context, workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
			return nil, &workflows.RunnerError{Kind: workflows.RunnerRuntimeError, Detail: "failed"}
		}), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	config.Services.WorkflowService = service
	manager = makeManager(t, config)
	parent := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	request := nativeWorkflowRequest(t, "action")
	request.SessionID = parent.ID()
	launch, err := service.Launch(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if service.HasActive(workflows.SessionID(parent.ID())) {
		t.Fatal("expected terminal state before publication finishes")
	}
	if _, err := manager.Delete("owner", parent.ID(), DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := manager.WaitCleanup(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(parent.core.workspace); err != nil {
		t.Fatal("workspace removed during terminal publication", err)
	}
	releaseOnce.Do(func() { close(release) })
	if err := manager.WaitCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(parent.core.workspace); !os.IsNotExist(err) {
		t.Fatal("joined scratch not reclaimed", err)
	}
	run, err := service.Wait(ctx, launch.RunID)
	if err != nil || run.Status != workflows.RunFailed {
		t.Fatal(run, err)
	}
}

func TestManagerWorkflowConfigurationRefusalsPrecedeWorkspaceAllocation(t *testing.T) {
	for _, name := range []string{"caps", "duration", "pool"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "new")
			config := managerTestConfig(root, &FakeProvider{})
			config.Services.WorkflowTools = true
			switch name {
			case "caps":
				config.Services.WorkflowCaps = &workflows.DefinitionCaps{}
			case "duration":
				caps := workflows.DefaultDefinitionCaps()
				caps.WallTimeSeconds = 1e20
				config.Services.WorkflowCaps = &caps
			case "pool":
				config.Services.WorkflowAttemptPool = &workflows.AttemptPool{}
			}
			if _, err := NewSessionManager(config); err == nil {
				t.Fatal("invalid workflow configuration accepted")
			}
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatal("invalid configuration allocated workspace", err)
			}
		})
	}
}

type workflowBlockingJournal struct {
	ActionJournal
	entered, release chan struct{}
	once             sync.Once
}

func (journal *workflowBlockingJournal) Begin(ctx context.Context, request ActionRequest) (ActionRecord, error) {
	if request.ActionID == "blocked" {
		journal.once.Do(func() { close(journal.entered) })
		select {
		case <-journal.release:
		case <-ctx.Done():
			return ActionRecord{}, ctx.Err()
		}
	}
	return journal.ActionJournal.Begin(ctx, request)
}

func TestManagerWorkflowDeleteAccountsForLaunchAlreadyInAdmission(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	base, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	journal := &workflowBlockingJournal{ActionJournal: base, entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(journal.release) })
	var parent *ManagedSession
	service, err := NewWorkflowService(WorkflowServiceConfig{Caps: workflows.DefaultDefinitionCaps(), Journal: journal, ResolveParent: func(SessionID) (WorkflowParent, bool) {
		return WorkflowParent{Owner: "owner", Workspace: parent.core.workspace}, true
	}, WorkerFactory: func(WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
		return workflows.WorkflowRunnerFunc(func(context.Context, workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
			result := workflows.NewArtifactSubmission(jsonvalue.ObjectValue(nil), "return_artifact")
			return &result, nil
		}), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	config.Services.WorkflowService = service
	manager := makeManager(t, config)
	parent = createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	request := nativeWorkflowRequest(t, "blocked")
	request.SessionID = parent.ID()
	request.parent = parent
	type launchOutcome struct {
		result WorkflowLaunchResult
		err    error
	}
	launched := make(chan launchOutcome, 1)
	go func() { result, err := service.Launch(ctx, request); launched <- launchOutcome{result, err} }()
	select {
	case <-journal.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := manager.Delete("owner", parent.ID(), DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(parent.core.workspace); err != nil {
		t.Fatal("in-admission launch lost its workspace", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := manager.WaitCleanup(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(journal.release) })
	result := <-launched
	if result.err != nil {
		t.Fatal(result.err)
	}
	if err := manager.WaitCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(parent.core.workspace); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	run, err := service.Wait(ctx, result.result.RunID)
	if err != nil || !run.Terminal() || service.HasActive(workflows.SessionID(parent.ID())) {
		t.Fatal(run, err)
	}
	request.ActionID = "late"
	if _, err := service.Launch(ctx, request); err == nil {
		t.Fatal("injected resolver bypassed the closed managed parent")
	}
}

func TestManagerWorkflowStopCallerTimeoutLeavesOwnedDrain(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	config.Services.WorkflowTools = true
	manager := makeManager(t, config)
	parent := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	service := manager.Workflows()
	service.config.WorkerFactory = func(WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
		return workflows.WorkflowRunnerFunc(func(workerCtx context.Context, _ workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
			close(entered)
			<-workerCtx.Done()
			close(cancelled)
			<-release
			return nil, workerCtx.Err()
		}), nil
	}
	request := nativeWorkflowRequest(t, "action")
	request.SessionID = parent.ID()
	launched, err := service.Launch(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	caller, cancel := context.WithCancel(ctx)
	cancel()
	if err := manager.Stop(caller); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if manager.State() != ManagerStopping {
		t.Fatal(manager.State())
	}
	if _, err := manager.Create(ctx, CreateSessionRequest{Owner: "owner"}); !errors.Is(err, ErrManagerStopped) {
		t.Fatal(err)
	}
	if _, err := os.Stat(parent.core.workspace); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(release) })
	if err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	run, err := service.Wait(ctx, launched.RunID)
	if err != nil || run.Status != workflows.RunCancelled || run.CancelReason == nil || *run.CancelReason != "service shutdown" {
		t.Fatal(run, err)
	}
	if len(service.sessionTasks(workflows.SessionID(parent.ID()))) != 0 {
		t.Fatal("shutdown left an owned task")
	}
}

func TestManagerWorkflowCleanupFailurePinsSharedWorkspaceUntilShutdown(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	shared := t.TempDir()
	config := managerTestConfig(t.TempDir(), &FakeProvider{})
	config.Services.WorkflowTools = true
	config.WorkspaceFactory = workspaceFactoryFunc(func(context.Context, SessionID) (string, error) { return shared, nil })
	manager := makeManager(t, config)
	first := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	second := createManaged(t, manager, CreateSessionRequest{Owner: "owner"})
	failure := make(chan error, 1)
	failure <- errors.New("cancel failed")
	if manager.finishWorkflowDeletion(first, failure, true) {
		t.Fatal("failed cancellation declared cleanup safe")
	}
	if _, err := manager.Delete("owner", first.ID(), DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Delete("owner", second.ID(), DeleteSessionOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := manager.WaitCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(shared); err != nil {
		t.Fatal("another cleanup removed a failure-pinned workspace", err)
	}
	if len(manager.CleanupErrors()) != 1 {
		t.Fatal(manager.CleanupErrors())
	}
	if err := manager.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(shared); !os.IsNotExist(err) {
		t.Fatal("terminal drained retention was not retried", err)
	}
}
