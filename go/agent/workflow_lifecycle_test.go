package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
)

func nativeWorkflowRequest(t *testing.T, action ActionID) WorkflowLaunchRequest {
	t.Helper()
	definition, err := jsonvalue.Decode(`{"name":"wf","return_from":"a","nodes":[{"id":"a","kind":"agent"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	input := protocol.WorkflowInput{Definition: definition, Args: jsonvalue.ObjectValue(nil)}
	return WorkflowLaunchRequest{SessionID: "s", Input: input, ActionInput: &input, Context: workflowLaunch(t), ActionID: action, ToolUseID: "u", LaunchTurn: 2}
}
func TestWorkflowServiceConcurrentReplayAndCancelJoin(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	started := make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	factory := func(c WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
		return workflows.WorkflowRunnerFunc(func(ctx context.Context, e workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
			calls.Add(1)
			once.Do(func() { close(started) })
			<-ctx.Done()
			return nil, ctx.Err()
		}), nil
	}
	journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	parent := WorkflowParent{Owner: "owner", Workspace: t.TempDir()}
	service, err := NewWorkflowService(WorkflowServiceConfig{Caps: workflows.DefaultDefinitionCaps(), Journal: journal, ResolveParent: func(SessionID) (WorkflowParent, bool) { return parent, true }, WorkerFactory: factory})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close(ctx)
	request := nativeWorkflowRequest(t, "action")
	var wg sync.WaitGroup
	results := make(chan WorkflowLaunchResult, 24)
	failures := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := service.Launch(ctx, request)
			if err != nil {
				failures <- err
			} else {
				results <- result
			}
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var id workflows.RunID
	for result := range results {
		if id == "" {
			id = result.RunID
		} else if id != result.RunID {
			t.Fatal("replay launched another run")
		}
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if calls.Load() != 1 || !service.HasActive("s") {
		t.Fatal("duplicate worker or lost active state", calls.Load())
	}
	foreign := workflows.SessionID("foreign")
	if _, err := service.Cancel(ctx, id, &foreign, "foreign"); err == nil {
		t.Fatal("foreign session cancelled workflow")
	}
	wg = sync.WaitGroup{}
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.Cancel(ctx, id, nil, "requested"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	run, err := service.Wait(ctx, id)
	if err != nil || run.Status != workflows.RunCancelled || service.HasActive("s") {
		t.Fatal(run, err)
	}
	if len(service.store.ListOutbox(workflows.OutboxFilter{RunID: &id})) != 1 {
		t.Fatal("duplicate terminal notice")
	}
	service.mu.Lock()
	tasks, live := len(service.tasks), len(service.live)
	service.mu.Unlock()
	if tasks != 0 || live != 0 {
		t.Fatal("joined task retained live authority", tasks, live)
	}
}

func TestWorkflowServiceCloseKeepsOwnershipAfterCallerTimeout(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	factory := func(c WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
		return workflows.WorkflowRunnerFunc(func(ctx context.Context, e workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
			close(started)
			<-ctx.Done()
			close(cancelled)
			<-release
			return nil, ctx.Err()
		}), nil
	}
	journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	root := t.TempDir()
	service, err := NewWorkflowService(WorkflowServiceConfig{Caps: workflows.DefaultDefinitionCaps(), Journal: journal, ResolveParent: func(SessionID) (WorkflowParent, bool) {
		return WorkflowParent{Owner: "owner", Workspace: root}, true
	}, WorkerFactory: factory})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Launch(ctx, nativeWorkflowRequest(t, "action"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cancelledCaller, cancelCaller := context.WithCancel(ctx)
	cancelCaller()
	if err := service.Close(cancelledCaller); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := service.Launch(ctx, nativeWorkflowRequest(t, "later")); err == nil {
		t.Fatal("launch entered draining service")
	}
	service.mu.Lock()
	owned := len(service.tasks)
	service.mu.Unlock()
	if owned != 1 {
		t.Fatal("close released ownership before join")
	}
	releaseOnce.Do(func() { close(release) })
	if err := service.Close(ctx); err != nil {
		t.Fatal(err)
	}
	run, err := service.Wait(ctx, result.RunID)
	if err != nil || run.Status != workflows.RunCancelled || workflowReason(run.CancelReason, "") != "service shutdown" {
		t.Fatal(run, err)
	}
	service.mu.Lock()
	owned, live := len(service.tasks), len(service.live)
	service.mu.Unlock()
	if owned != 0 || live != 0 {
		t.Fatal("close did not drain task authority")
	}
}

func TestWorkflowServicePinsTerminalGraphUntilPublication(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	observed, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	root := t.TempDir()
	events := WorkflowEventSinkFunc(func(_ context.Context, e WorkflowEvent) error {
		if e.Kind == WorkflowFailed {
			once.Do(func() { close(observed) })
			<-release
		}
		return nil
	})
	factory := func(WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
		return workflows.WorkflowRunnerFunc(func(context.Context, workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
			return nil, &workflows.RunnerError{Kind: workflows.RunnerRuntimeError, Detail: "fault"}
		}), nil
	}
	journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	service, err := NewWorkflowService(WorkflowServiceConfig{Caps: workflows.DefaultDefinitionCaps(), Journal: journal, ResolveParent: func(SessionID) (WorkflowParent, bool) {
		return WorkflowParent{Owner: "owner", Workspace: root, Events: events}, true
	}, WorkerFactory: factory})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		releaseOnce.Do(func() { close(release) })
		if err := service.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	result, err := service.Launch(ctx, nativeWorkflowRequest(t, "action"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-observed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	keep := workflows.TerminalRunLimit(0)
	if ids := service.PruneTerminalRuns(&keep); len(ids) != 0 {
		t.Fatal("active terminal graph evicted", ids)
	}
	run, err := service.Get(result.RunID)
	if err != nil || run.Status != workflows.RunFailed {
		t.Fatal(run, err)
	}
	releaseOnce.Do(func() { close(release) })
	if _, err := service.Wait(ctx, result.RunID); err != nil {
		t.Fatal(err)
	}
	batch, err := service.Views().PrepareNotifications("s", 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Views().AcknowledgeNotifications(batch); err != nil {
		t.Fatal(err)
	}
	removed := service.PruneTerminalRuns(&keep)
	if len(removed) != 1 || removed[0] != result.RunID {
		t.Fatal("drained graph retained", removed)
	}
	service.mu.Lock()
	live, terminal, queued := len(service.live), len(service.terminalEvents), len(service.resultEvents)
	service.mu.Unlock()
	if live != 0 || terminal != 0 || queued != 0 {
		t.Fatal("pruned bookkeeping retained")
	}
}

func TestWorkflowServiceUsesFreshNativeWorkers(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	provider := stateProviderFunc(func(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
		if len(request.Messages) == 1 {
			return fakeReply([]protocol.Block{protocol.NewToolUse("a", protocol.ReturnArtifactToolInput(jsonvalue.ObjectValue(nil)))}, protocol.StopToolUse), nil
		}
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	})
	journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	root := t.TempDir()
	parent := WorkflowParent{Owner: "owner", Workspace: root}
	service, err := NewWorkflowService(WorkflowServiceConfig{Caps: workflows.DefaultDefinitionCaps(), Journal: journal, ResolveParent: func(SessionID) (WorkflowParent, bool) { return parent, true }, Worker: WorkflowRunnerConfig{Provider: provider, Owner: "ignored", Workspace: "ignored"}})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close(ctx)
	request := nativeWorkflowRequest(t, "action")
	request.Input.Definition, err = jsonvalue.Decode(`{"name":"wf","return_from":"b","nodes":[{"id":"a","kind":"agent"},{"id":"b","kind":"reduce","needs":["a"]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	request.ActionInput = &request.Input
	result, err := service.Launch(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Wait(ctx, result.RunID)
	if err != nil || run.Status != workflows.RunCompleted || run.AttemptsUsed != 2 {
		t.Fatal(run, err)
	}
	for _, attempt := range service.store.ListAttempts(run.RunID) {
		if attempt.Status != workflows.AttemptSucceeded {
			t.Fatal(attempt)
		}
	}
}

func TestWorkflowServiceObservabilityIsBoundedAndDetached(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	root := t.TempDir()
	journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	factory := func(config WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
		return workflows.WorkflowRunnerFunc(func(ctx context.Context, e workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
			for i := 0; i < 120; i++ {
				if err := config.EventSink.OnEvent(ctx, SessionEventRecord{Event: SessionEvent{kind: EventToolUse, toolUse: ToolUseEvent{Name: protocol.ToolCompress, Input: protocol.CompressToolInput(), ID: "u"}}}); err != nil {
					return nil, err
				}
			}
			value := workflows.NewArtifactSubmission(jsonvalue.ObjectValue(nil), "return_artifact")
			return &value, nil
		}), nil
	}
	sink := WorkflowEventSinkFunc(func(context.Context, WorkflowEvent) error {
		return workflowServiceError(WorkflowRuntimeError, strings.Repeat("界", 600))
	})
	service, err := NewWorkflowService(WorkflowServiceConfig{Caps: workflows.DefaultDefinitionCaps(), Journal: journal, ResolveParent: func(SessionID) (WorkflowParent, bool) {
		return WorkflowParent{Owner: "owner", Workspace: root, Events: sink}, true
	}, WorkerFactory: factory})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close(ctx)
	result, err := service.Launch(ctx, nativeWorkflowRequest(t, "action"))
	if err != nil {
		t.Fatal(err)
	}
	run, err := service.Wait(ctx, result.RunID)
	if err != nil || run.Status != workflows.RunCompleted {
		t.Fatal(run, err)
	}
	observations := service.ObservabilityErrors()
	if len(observations) != 100 {
		t.Fatal(len(observations))
	}
	for _, observation := range observations {
		if utf8.RuneCountInString(observation.Error) != 500 {
			t.Fatal("unbounded diagnostic")
		}
	}
	observations[0].Error = "mutated"
	if service.ObservabilityErrors()[0].Error == "mutated" {
		t.Fatal("diagnostic aliases service")
	}
}

func TestWorkflowServiceQueuedReplayRequiresTrustedLiveContext(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	root := t.TempDir()
	journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	factory := func(config WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
		return workflows.WorkflowRunnerFunc(func(ctx context.Context, e workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
			foreign := e.Attempt
			foreign.RunID = "foreign"
			if _, err := config.ResolveContext(ctx, foreign); err == nil {
				return nil, errors.New("foreign live context lookup admitted")
			}
			live, err := config.ResolveContext(ctx, e.Attempt)
			if err != nil {
				return nil, err
			}
			if live.Authority() != AuthorityExplicitHuman || !live.Allows(CapabilityWorkflowLaunch) {
				return nil, errors.New("live context was not retained")
			}
			live.approved = nil
			retained, err := config.ResolveContext(ctx, e.Attempt)
			if err != nil || !retained.Allows(CapabilityWorkflowLaunch) {
				return nil, errors.New("context aliases caller")
			}
			result := workflows.NewArtifactSubmission(jsonvalue.ObjectValue(nil), "return_artifact")
			return &result, nil
		}), nil
	}
	service, err := NewWorkflowService(WorkflowServiceConfig{Caps: workflows.DefaultDefinitionCaps(), Journal: journal, ResolveParent: func(SessionID) (WorkflowParent, bool) { return WorkflowParent{Owner: "owner", Workspace: root}, true }, WorkerFactory: factory})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close(ctx)
	request := nativeWorkflowRequest(t, "action")
	admitted, err := service.admission.Admit(request.Input.Definition)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.RegisterDefinition(admitted.Definition()); err != nil {
		t.Fatal(err)
	}
	// Explicit fixture seed: a recorded context exists, but no live binding/task.
	run, err := service.store.CreateRun(workflows.CreateRunInput{DefinitionRevision: admitted.Definition().Revision(), SessionID: "s", IdempotencyKey: "action", Args: request.Input.Args, RunContext: request.Context.Snapshot(), PolicySnapshotHash: admitted.PolicySnapshotHash()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := journal.Begin(ctx, ActionRequest{ActionID: "action", SessionID: "s", MessageID: request.Context.MessageID(), ToolUseID: "u", Input: protocol.WorkflowToolInput(request.Input)}); err != nil {
		t.Fatal(err)
	}
	if _, err := journal.AttachWorkflow(ctx, "action", run.RunID); err != nil {
		t.Fatal(err)
	}
	different := request
	different.Context = different.Context.clone()
	different.Context.actorID = stringActorPointer("different")
	if _, err := service.Launch(ctx, different); err == nil {
		t.Fatal("snapshot minted a mismatched live context")
	}
	service.mu.Lock()
	tasks := len(service.tasks)
	service.mu.Unlock()
	if tasks != 0 {
		t.Fatal("refused replay started a worker")
	}
	result, err := service.Launch(ctx, request)
	if err != nil || !result.Reused || result.RunID != run.RunID {
		t.Fatal(result, err)
	}
	terminal, err := service.Wait(ctx, run.RunID)
	if err != nil || terminal.Status != workflows.RunCompleted {
		t.Fatal(terminal, err)
	}
}
func stringActorPointer(text string) *ActorID { value := ActorID(text); return &value }

func TestWorkflowServiceCancelSessionAfterParentDeletion(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	root := t.TempDir()
	journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	var deleted atomic.Bool
	started := make(chan struct{})
	var once sync.Once
	factory := func(WorkflowRunnerConfig) (workflows.WorkflowRunner, error) {
		return workflows.WorkflowRunnerFunc(func(ctx context.Context, e workflows.AttemptExecution) (*workflows.ArtifactSubmission, error) {
			once.Do(func() { close(started) })
			<-ctx.Done()
			return nil, ctx.Err()
		}), nil
	}
	service, err := NewWorkflowService(WorkflowServiceConfig{Caps: workflows.DefaultDefinitionCaps(), Journal: journal, ResolveParent: func(SessionID) (WorkflowParent, bool) {
		return WorkflowParent{Owner: "owner", Workspace: root}, !deleted.Load()
	}, WorkerFactory: factory})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close(ctx)
	result, err := service.Launch(ctx, nativeWorkflowRequest(t, "action"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	deleted.Store(true)
	if err := service.CancelSession(ctx, "s"); err != nil {
		t.Fatal(err)
	}
	run, err := service.Get(result.RunID)
	if err != nil || run.Status != workflows.RunCancelled || workflowReason(run.CancelReason, "") != "parent session deleted" || service.HasActive("s") {
		t.Fatal(run, err)
	}
	if len(service.store.ListOutbox(workflows.OutboxFilter{RunID: &run.RunID})) != 1 {
		t.Fatal("deleted parent lost terminal notice")
	}
}

func TestWorkflowServiceConstructorAdmission(t *testing.T) {
	journal, _ := NewInMemoryActionJournal(DefaultResultsRetained)
	valid := WorkflowServiceConfig{Caps: workflows.DefaultDefinitionCaps(), Journal: journal, ResolveParent: func(SessionID) (WorkflowParent, bool) { return WorkflowParent{}, false }, WorkerFactory: func(WorkflowRunnerConfig) (workflows.WorkflowRunner, error) { return nil, nil }}
	for _, patch := range []func(*WorkflowServiceConfig){
		func(c *WorkflowServiceConfig) { c.Caps = workflows.DefinitionCaps{} },
		func(c *WorkflowServiceConfig) { c.Caps.WallTimeSeconds = 1e20 },
		func(c *WorkflowServiceConfig) { c.Journal = nil },
		func(c *WorkflowServiceConfig) { c.ResolveParent = nil },
		func(c *WorkflowServiceConfig) { c.WorkerFactory = nil },
	} {
		config := valid
		patch(&config)
		if _, err := NewWorkflowService(config); err == nil {
			t.Fatal("invalid constructor admitted")
		}
	}
}
