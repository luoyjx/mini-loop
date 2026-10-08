package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workflows"
)

type WorkflowServiceErrorKind string

const (
	WorkflowRuntimeError    WorkflowServiceErrorKind = "RuntimeError"
	WorkflowPermissionError WorkflowServiceErrorKind = "PermissionError"
	WorkflowValueError      WorkflowServiceErrorKind = "ValueError"
	WorkflowLookupError     WorkflowServiceErrorKind = "LookupError"
)

type WorkflowServiceError struct {
	Kind   WorkflowServiceErrorKind
	Detail string
}

func (e *WorkflowServiceError) Error() string { return e.Detail }
func workflowServiceError(kind WorkflowServiceErrorKind, detail string) error {
	return &WorkflowServiceError{kind, detail}
}

type WorkflowParent struct {
	Owner     OwnerID
	Workspace string
	Events    WorkflowEventSink
}
type WorkflowParentResolver func(SessionID) (WorkflowParent, bool)
type WorkflowWorkerFactory func(WorkflowRunnerConfig) (workflows.WorkflowRunner, error)
type WorkflowServiceConfig struct {
	Caps          workflows.DefinitionCaps
	Journal       ActionJournal
	ResolveParent WorkflowParentResolver
	Store         *workflows.InMemoryStore
	AttemptPool   *workflows.AttemptPool
	Worker        WorkflowRunnerConfig
	WorkerFactory WorkflowWorkerFactory
}
type WorkflowLaunchRequest struct {
	parent     *ManagedSession
	SessionID  SessionID
	Input      protocol.WorkflowInput
	Context    RunContext
	ActionID   ActionID
	ToolUseID  string
	LaunchTurn workflows.ParentTurn
	// Nil selects source service fallback: normalized definition plus args.
	// Tool adapters supply the original concrete input instead.
	ActionInput *protocol.WorkflowInput
}
type WorkflowLaunchStatus string

const WorkflowAsyncLaunched WorkflowLaunchStatus = "async_launched"

type WorkflowLaunchResult struct {
	Status        WorkflowLaunchStatus `json:"status"`
	RunID         workflows.RunID      `json:"run_id"`
	Name          string               `json:"workflow_name"`
	Revision      workflows.Revision   `json:"definition_revision"`
	EstimatedSize string               `json:"estimated_size"`
	Reused        bool                 `json:"reused"`
	Error         *string              `json:"error"`
}
type WorkflowObservationError struct {
	RunID workflows.RunID   `json:"run_id"`
	Kind  WorkflowEventKind `json:"kind"`
	Error string            `json:"error"`
}
type workflowLiveLaunch struct {
	context RunContext
	owner   OwnerID
}
type workflowServiceTask struct {
	start, done chan struct{}
	err         error
}
type workflowDefinitionInfo struct {
	Name        string                 `json:"name"`
	Revision    workflows.Revision     `json:"revision"`
	Budget      workflows.BudgetPolicy `json:"budget"`
	Policy      workflows.ToolPolicy   `json:"policy"`
	Nodes       []workflows.Node       `json:"nodes"`
	InputSchema workflows.Value        `json:"input_schema"`
}

func definitionInfo(definition workflows.Definition) (workflowDefinitionInfo, error) {
	data, err := definition.MarshalJSON()
	if err != nil {
		return workflowDefinitionInfo{}, err
	}
	var info workflowDefinitionInfo
	err = json.Unmarshal(data, &info)
	return info, err
}

// WorkflowService owns process-local tasks and trusted live launch contexts.
// Saved run snapshots are inert. Parent resolution and worker factories are
// operator seams; this constructor does not install a manager or model tool.
type WorkflowService struct {
	resolveEvents                workflowEventResolver
	config                       WorkflowServiceConfig
	admission                    *workflows.DefinitionAdmission
	store                        *workflows.InMemoryStore
	views                        *workflows.ServiceViews
	engine                       *workflows.WorkflowEngine
	admissionMu                  sync.Mutex
	mu                           sync.Mutex
	closed                       bool
	shutdownDone                 chan struct{}
	shutdownErr                  error
	tasks                        map[workflows.RunID]*workflowServiceTask
	live                         map[workflows.RunID]workflowLiveLaunch
	terminalEvents, resultEvents map[workflows.RunID]bool
	observationErrors            []WorkflowObservationError
}

func NewWorkflowService(config WorkflowServiceConfig) (*WorkflowService, error) {
	admission, err := workflows.NewDefinitionAdmission(config.Caps)
	if err != nil {
		return nil, err
	}
	if !validWorkflowWallTime(config.Caps.WallTimeSeconds) {
		return nil, errors.New("workflow wall-time policy exceeds Go duration range")
	}
	if config.Journal == nil || config.ResolveParent == nil {
		return nil, errors.New("workflow service requires journal and parent resolver")
	}
	if config.WorkerFactory == nil {
		if config.Worker.Provider == nil {
			return nil, errors.New("workflow service requires a provider")
		}
		config.WorkerFactory = func(c WorkflowRunnerConfig) (workflows.WorkflowRunner, error) { return NewFreshWorkflowRunner(c) }
	}
	if config.Store == nil {
		config.Store = workflows.NewInMemoryStore()
	}
	if config.AttemptPool == nil {
		config.AttemptPool, err = workflows.NewAttemptPool(config.Caps.MaxConcurrentAgents)
		if err != nil {
			return nil, err
		}
	}
	// Policy-owned values override caller workspace/authority/round/event fields.
	config.Worker.MaxRounds = nil
	config.Worker.ResolveContext = nil
	config.Worker.EventSink = nil
	s := &WorkflowService{config: config, admission: admission, store: config.Store, tasks: map[workflows.RunID]*workflowServiceTask{}, live: map[workflows.RunID]workflowLiveLaunch{}, terminalEvents: map[workflows.RunID]bool{}, resultEvents: map[workflows.RunID]bool{}, shutdownDone: make(chan struct{})}
	s.views, err = workflows.NewServiceViews(s.store)
	if err != nil {
		return nil, err
	}
	limit := config.Caps.MaxConcurrentAgents
	s.engine, err = workflows.NewWorkflowEngine(s.store, workflows.WorkflowRunnerFunc(s.runAttempt), workflows.EngineOptions{MaxConcurrentAgents: &limit, AttemptPool: config.AttemptPool})
	if err != nil {
		return nil, err
	}
	return s, nil
}

func validWorkflowWallTime(seconds float64) bool {
	return seconds >= 1/float64(time.Second) && seconds < float64(math.MaxInt64)/float64(time.Second)
}

func (s *WorkflowService) sessionTasks(session workflows.SessionID) []*workflowServiceTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks := []*workflowServiceTask{}
	for id, task := range s.tasks {
		if run, err := s.store.GetRun(id); err == nil && run.SessionID == session {
			tasks = append(tasks, task)
		}
	}
	return tasks
}
func (s *WorkflowService) Store() *workflows.InMemoryStore { return s.store }
func (s *WorkflowService) Views() *workflows.ServiceViews  { return s.views }
func (s *WorkflowService) Get(id workflows.RunID) (workflows.WorkflowRun, error) {
	return s.store.GetRun(id)
}
func (s *WorkflowService) Status(id workflows.RunID, session *workflows.SessionID) (workflows.RunStatusView, error) {
	return s.views.Status(id, session)
}
func (s *WorkflowService) Summaries(session workflows.SessionID) ([]workflows.RunSummary, error) {
	return s.views.Summaries(session)
}

func (s *WorkflowService) Launch(ctx context.Context, request WorkflowLaunchRequest) (WorkflowLaunchResult, error) {
	s.admissionMu.Lock()
	locked := true
	defer func() {
		if locked {
			s.admissionMu.Unlock()
		}
	}()
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return WorkflowLaunchResult{}, workflowServiceError(WorkflowRuntimeError, "workflow service is closed")
	}
	if request.parent != nil {
		request.parent.mu.Lock()
		err := request.parent.admissionError()
		request.parent.mu.Unlock()
		if err != nil {
			return WorkflowLaunchResult{}, err
		}
	}
	if request.Context.Authority() != AuthorityExplicitHuman || request.Context.Validate() != nil {
		return WorkflowLaunchResult{}, workflowServiceError(WorkflowPermissionError, "Workflow launch requires an explicit_human trusted local context")
	}
	if !request.Context.Allows(CapabilityWorkflowLaunch) {
		return WorkflowLaunchResult{}, workflowServiceError(WorkflowPermissionError, "Workflow launch requires a per-message workflow.launch approval")
	}
	if request.ActionID == "" {
		return WorkflowLaunchResult{}, workflowServiceError(WorkflowValueError, "action_id is required")
	}
	parent, found := s.config.ResolveParent(request.SessionID)
	if !found {
		return WorkflowLaunchResult{}, workflowServiceError(WorkflowLookupError, fmt.Sprintf("session %s not found", request.SessionID))
	}
	if parent.Owner == "" {
		return WorkflowLaunchResult{}, workflowServiceError(WorkflowPermissionError, "workflow parent requires a trusted owner binding")
	}
	admitted, err := s.admission.Admit(request.Input.Definition)
	if err != nil {
		return WorkflowLaunchResult{}, err
	}
	info, err := definitionInfo(admitted.Definition())
	if err != nil {
		return WorkflowLaunchResult{}, err
	}
	if info.Policy.OriginAuthorityRequired != workflows.AuthorityRequirement(request.Context.Authority()) {
		return WorkflowLaunchResult{}, workflowServiceError(WorkflowPermissionError, "workflow definition authority policy does not match the run context")
	}
	if request.Input.Args.Kind() != jsonvalue.Object {
		return WorkflowLaunchResult{}, workflowServiceError(WorkflowValueError, "workflow args must be an object")
	}
	if err := workflows.ValidateValue(info.InputSchema, request.Input.Args); err != nil {
		return WorkflowLaunchResult{}, err
	}
	input := protocol.WorkflowInput{Definition: admitted.Definition().Data(), Args: request.Input.Args}
	if request.ActionInput != nil {
		input = *request.ActionInput
	}
	use := request.ToolUseID
	if use == "" {
		use = string(request.ActionID)
	}
	action, err := s.config.Journal.Begin(ctx, ActionRequest{ActionID: request.ActionID, SessionID: request.SessionID, MessageID: request.Context.MessageID(), ToolUseID: use, Input: protocol.WorkflowToolInput(input)})
	if err != nil {
		return WorkflowLaunchResult{}, err
	}
	if action.WorkflowRunID != nil {
		run, err := s.store.GetRun(*action.WorkflowRunID)
		if err != nil {
			return WorkflowLaunchResult{}, err
		}
		if run.SessionID != workflows.SessionID(request.SessionID) {
			return WorkflowLaunchResult{}, workflowServiceError(WorkflowPermissionError, "workflow run belongs to a different session")
		}
		definition, err := s.store.GetDefinition(run.DefinitionRevision)
		if err != nil {
			return WorkflowLaunchResult{}, err
		}
		info, err = definitionInfo(definition)
		if err != nil {
			return WorkflowLaunchResult{}, err
		}
		if run.Status == workflows.RunQueued {
			if err := s.bindReplay(run, request.Context, parent.Owner); err != nil {
				return WorkflowLaunchResult{}, err
			}
			task := s.ensureTask(run)
			if task != nil {
				close(task.start)
			}
		}
		return workflowLaunchResult(run, info, true), nil
	}
	definition, err := s.store.RegisterDefinition(admitted.Definition())
	if err != nil {
		return WorkflowLaunchResult{}, err
	}
	actionID := workflows.LaunchActionID(request.ActionID)
	run, err := s.store.CreateRun(workflows.CreateRunInput{DefinitionRevision: definition.Revision(), SessionID: workflows.SessionID(request.SessionID), RunContext: request.Context.Snapshot(), IdempotencyKey: workflows.IdempotencyKey(request.ActionID), Args: request.Input.Args, LaunchActionID: &actionID, PolicySnapshotHash: admitted.PolicySnapshotHash()})
	if err != nil {
		return WorkflowLaunchResult{}, err
	}
	if _, err := s.config.Journal.AttachWorkflow(ctx, request.ActionID, run.RunID); err != nil {
		return WorkflowLaunchResult{}, err
	}
	s.mu.Lock()
	if _, exists := s.live[run.RunID]; !exists {
		s.live[run.RunID] = workflowLiveLaunch{request.Context.clone(), parent.Owner}
	}
	s.mu.Unlock()
	if err := s.views.RecordLaunchTurn(run.RunID, request.LaunchTurn); err != nil {
		return WorkflowLaunchResult{}, err
	}
	s.PruneTerminalRuns(nil)
	task := s.ensureTask(run)
	s.admissionMu.Unlock()
	locked = false
	// The registered task cannot execute before planned/approval publication.
	// Close sees the task even while a cooperative observer is publishing them.
	if task != nil {
		defer close(task.start)
	}
	s.emit(ctx, run, WorkflowPlanned, nil, workflowEventPayload{definitionHash: definition.Hash(), nodeCount: len(info.Nodes), size: info.Budget.SizeGuideline})
	s.emit(ctx, run, WorkflowDecisionRecorded, nil, workflowEventPayload{actor: request.Context.Snapshot().ActorID, authority: request.Context.Authority()})
	return workflowLaunchResult(run, info, false), nil
}

// PruneTerminalRuns excludes in-flight task graphs until their terminal effects
// are published; a terminal state alone does not mean its goroutine has joined.
func (s *WorkflowService) PruneTerminalRuns(limit *workflows.TerminalRunLimit) []workflows.RunID {
	s.mu.Lock()
	defer s.mu.Unlock()
	pinned := make([]workflows.RunID, 0, len(s.tasks))
	for id := range s.tasks {
		pinned = append(pinned, id)
	}
	removed := s.views.PruneTerminalRunsExcept(limit, pinned)
	for _, id := range removed {
		delete(s.live, id)
		delete(s.terminalEvents, id)
		delete(s.resultEvents, id)
	}
	return removed
}
func workflowLaunchResult(run workflows.WorkflowRun, info workflowDefinitionInfo, reused bool) WorkflowLaunchResult {
	status := WorkflowAsyncLaunched
	if reused && run.Terminal() {
		status = WorkflowLaunchStatus(strings.ToLower(string(run.Status)))
	}
	return WorkflowLaunchResult{Status: status, RunID: run.RunID, Name: info.Name, Revision: info.Revision, EstimatedSize: info.Budget.SizeGuideline, Reused: reused}
}
func (s *WorkflowService) bindReplay(run workflows.WorkflowRun, live RunContext, owner OwnerID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.live[run.RunID]; exists {
		return nil
	}
	saved, err := json.Marshal(run.RunContext)
	if err != nil {
		return err
	}
	fresh, err := json.Marshal(live.Snapshot())
	if err != nil {
		return err
	}
	if string(saved) != string(fresh) {
		return workflowServiceError(WorkflowPermissionError, "queued workflow replay requires its original trusted live context")
	}
	s.live[run.RunID] = workflowLiveLaunch{live.clone(), owner}
	return nil
}

// ensureTask returns only a newly registered task, whose start channel the caller owns.
func (s *WorkflowService) ensureTask(run workflows.WorkflowRun) *workflowServiceTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.tasks[run.RunID]; exists || run.Status != workflows.RunQueued {
		return nil
	}
	task := &workflowServiceTask{start: make(chan struct{}), done: make(chan struct{})}
	s.tasks[run.RunID] = task
	go func() {
		<-task.start
		_, task.err = s.execute(run.RunID)
		s.mu.Lock()
		if current, err := s.store.GetRun(run.RunID); err == nil && current.Terminal() {
			delete(s.live, run.RunID)
		}
		close(task.done)
		if s.tasks[run.RunID] == task {
			delete(s.tasks, run.RunID)
		}
		s.mu.Unlock()
	}()
	return task
}
func (s *WorkflowService) Wait(ctx context.Context, id workflows.RunID) (workflows.WorkflowRun, error) {
	s.mu.Lock()
	task := s.tasks[id]
	s.mu.Unlock()
	if task != nil {
		select {
		case <-ctx.Done():
			return workflows.WorkflowRun{}, ctx.Err()
		case <-task.done:
			if task.err != nil {
				return workflows.WorkflowRun{}, task.err
			}
		}
	}
	return s.store.GetRun(id)
}
func (s *WorkflowService) Cancel(ctx context.Context, id workflows.RunID, session *workflows.SessionID, reason string) (workflows.WorkflowRun, error) {
	run, err := s.store.GetRun(id)
	if err != nil {
		return workflows.WorkflowRun{}, err
	}
	if session != nil && run.SessionID != *session {
		return workflows.WorkflowRun{}, &workflows.StoreError{Kind: workflows.StoreNotFound, Detail: fmt.Sprintf("run %s not found", id)}
	}
	cancelled, err := s.engine.Cancel(id, &reason)
	if err != nil {
		return workflows.WorkflowRun{}, err
	}
	s.mu.Lock()
	task := s.tasks[id]
	s.mu.Unlock()
	if task != nil {
		select {
		case <-ctx.Done():
			return workflows.WorkflowRun{}, ctx.Err()
		case <-task.done:
		}
	}
	result, err := s.store.GetRun(id)
	if err != nil {
		return workflows.WorkflowRun{}, err
	}
	if task == nil && !run.Terminal() && result.Status == workflows.RunCancelled {
		s.emitTerminal(context.Background(), result, WorkflowCancelled, workflowEventPayload{reason: workflowReason(result.CancelReason, reason)})
		if err := s.enqueueTerminal(result); err != nil {
			return workflows.WorkflowRun{}, err
		}
	}
	if result.Terminal() {
		return result, nil
	}
	return cancelled, nil
}
func (s *WorkflowService) HasActive(session workflows.SessionID) bool {
	for _, run := range s.store.ListRuns(&session) {
		if !run.Terminal() {
			return true
		}
	}
	return false
}
func (s *WorkflowService) CancelSession(ctx context.Context, session workflows.SessionID) error {
	// Serialize the deletion snapshot with admission: a launch that already
	// resolved its parent must register its task before cleanup can pass.
	s.admissionMu.Lock()
	runs := s.store.ListRuns(&session)
	s.admissionMu.Unlock()
	var first error
	for _, run := range runs {
		if !run.Terminal() {
			_, err := s.Cancel(ctx, run.RunID, &session, "parent session deleted")
			if err != nil && first == nil {
				first = err
			}
		}
	}
	// Terminal state can precede observer/outbox publication. Join those tasks too.
	for _, task := range s.sessionTasks(session) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-task.done:
			if task.err != nil && first == nil {
				first = task.err
			}
		}
	}
	return first
}
func (s *WorkflowService) Close(ctx context.Context) error {
	s.admissionMu.Lock()
	s.mu.Lock()
	start := !s.closed
	s.closed = true
	s.mu.Unlock()
	s.admissionMu.Unlock()
	if start {
		go func() {
			var first error
			for _, run := range s.store.ListRuns(nil) {
				if !run.Terminal() {
					_, err := s.Cancel(context.Background(), run.RunID, nil, "service shutdown")
					if err != nil && first == nil {
						first = err
					}
				}
			}
			s.mu.Lock()
			tasks := make([]*workflowServiceTask, 0, len(s.tasks))
			for _, task := range s.tasks {
				tasks = append(tasks, task)
			}
			s.mu.Unlock()
			for _, task := range tasks {
				<-task.done
			}
			s.mu.Lock()
			clear(s.live)
			s.shutdownErr = first
			close(s.shutdownDone)
			s.mu.Unlock()
		}()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.shutdownDone:
		s.mu.Lock()
		err := s.shutdownErr
		s.mu.Unlock()
		return err
	}
}
func workflowReason(value *string, fallback string) string {
	if value != nil && *value != "" {
		return *value
	}
	return fallback
}
func workflowErrorDetail(err error) string {
	var service *WorkflowServiceError
	var runner *workflows.RunnerError
	var execution *workflows.ExecutionError
	var store *workflows.StoreError
	switch {
	case errors.As(err, &service):
		return string(service.Kind) + ": " + service.Detail
	case errors.As(err, &runner):
		return string(runner.Kind) + ": " + runner.Detail
	case errors.As(err, &execution):
		return "RuntimeError: " + execution.Detail
	case errors.As(err, &store):
		return string(store.Kind) + ": " + store.Detail
	case errors.Is(err, context.Canceled):
		return "CancelledError: "
	case errors.Is(err, context.DeadlineExceeded):
		return "TimeoutError: "
	default:
		return "NativeRunnerError: " + err.Error()
	}
}
