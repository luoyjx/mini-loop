package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type AttemptExecution struct {
	Attempt NodeAttempt
	Node    Node
	Inputs  Value
}

// WorkflowRunner owns execution, not launch authority. Inputs and schema values
// are immutable; attempts/nodes are detached. Nil submission is a missing result.
type WorkflowRunner interface {
	Run(context.Context, AttemptExecution) (*ArtifactSubmission, error)
}
type WorkflowRunnerFunc func(context.Context, AttemptExecution) (*ArtifactSubmission, error)

func (f WorkflowRunnerFunc) Run(ctx context.Context, input AttemptExecution) (*ArtifactSubmission, error) {
	return f(ctx, input)
}

type RunnerErrorKind string

const (
	RunnerRuntimeError   RunnerErrorKind = "RuntimeError"
	RunnerValueError     RunnerErrorKind = "ValueError"
	RunnerTypeError      RunnerErrorKind = "TypeError"
	RunnerTimeoutError   RunnerErrorKind = "TimeoutError"
	RunnerAttributeError RunnerErrorKind = "AttributeError"
)

type RunnerError struct {
	Kind   RunnerErrorKind
	Detail string
}

func (e *RunnerError) Error() string { return e.Detail }

type ExecutionError struct{ Detail string }

func (e *ExecutionError) Error() string { return e.Detail }

func executionErrorReason(err error) string {
	var runner *RunnerError
	if errors.As(err, &runner) {
		switch runner.Kind {
		case RunnerRuntimeError, RunnerValueError, RunnerTypeError, RunnerTimeoutError, RunnerAttributeError:
			return string(runner.Kind) + ": " + runner.Detail
		}
	}
	var validation *ValidationError
	if errors.As(err, &validation) {
		return string(validation.Kind) + ": " + validation.Detail
	}
	var store *StoreError
	if errors.As(err, &store) {
		return string(store.Kind) + ": " + store.Detail
	}
	return "NativeRunnerError: " + err.Error()
}

type AttemptPool struct{ tokens chan struct{} }
type AttemptPermit struct {
	pool *AttemptPool
	once sync.Once
}

func NewAttemptPool(size int) (*AttemptPool, error) {
	if size < 0 {
		return nil, storeError(StoreValueFailure, "Semaphore initial value must be >= 0")
	}
	p := &AttemptPool{tokens: make(chan struct{}, size)}
	for i := 0; i < size; i++ {
		p.tokens <- struct{}{}
	}
	return p, nil
}
func (p *AttemptPool) Acquire(ctx context.Context) (*AttemptPermit, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.tokens:
		return &AttemptPermit{pool: p}, nil
	}
}
func (p *AttemptPermit) Release() { p.once.Do(func() { p.pool.tokens <- struct{}{} }) }

type EngineOptions struct {
	MaxConcurrentAgents *int
	AttemptPool         *AttemptPool
}
type executeLock struct {
	token chan struct{}
	refs  int
}
type inflightBatch struct {
	cancel context.CancelFunc
}
type WorkflowEngine struct {
	store         *InMemoryStore
	runner        WorkflowRunner
	maxConcurrent int
	pool          *AttemptPool
	mu            sync.Mutex
	locks         map[RunID]*executeLock
	inflight      map[RunID]*inflightBatch
}

func NewWorkflowEngine(store *InMemoryStore, runner WorkflowRunner, options EngineOptions) (*WorkflowEngine, error) {
	limit := HardMaxConcurrentAgents
	if options.MaxConcurrentAgents != nil {
		limit = *options.MaxConcurrentAgents
	}
	if limit <= 0 || limit > HardMaxConcurrentAgents {
		return nil, storeError(StoreValueFailure, fmt.Sprintf("max_concurrent_agents must be between 1 and %d", HardMaxConcurrentAgents))
	}
	if store == nil || runner == nil {
		return nil, &ExecutionError{"workflow store and runner are required"}
	}
	pool := options.AttemptPool
	if pool == nil {
		pool, _ = NewAttemptPool(limit)
	}
	return &WorkflowEngine{store: store, runner: runner, maxConcurrent: limit, pool: pool, locks: map[RunID]*executeLock{}, inflight: map[RunID]*inflightBatch{}}, nil
}
func (e *WorkflowEngine) runLock(ctx context.Context, id RunID) (func(), error) {
	e.mu.Lock()
	lock := e.locks[id]
	if lock == nil {
		lock = &executeLock{token: make(chan struct{}, 1)}
		e.locks[id] = lock
	}
	lock.refs++
	e.mu.Unlock()
	cleanup := func() {
		e.mu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(e.locks, id)
		}
		e.mu.Unlock()
	}
	select {
	case <-ctx.Done():
		cleanup()
		return nil, ctx.Err()
	case lock.token <- struct{}{}:
	}
	return func() { <-lock.token; cleanup() }, nil
}

func (e *WorkflowEngine) Cancel(id RunID, reason *string) (WorkflowRun, error) {
	// Serialize cancellation admission with claim publication and task tracking.
	e.mu.Lock()
	defer e.mu.Unlock()
	for {
		r, err := e.store.GetRun(id)
		if err != nil {
			return WorkflowRun{}, err
		}
		_, err = e.store.RequestCancel(id, r.Version, reason)
		if isVersionConflict(err) {
			continue
		}
		if err != nil {
			return WorkflowRun{}, err
		}
		break
	}
	if batch := e.inflight[id]; batch != nil {
		batch.cancel()
	}
	if _, err := e.store.CancelClaimedAttempts(id, nil); err != nil {
		return WorkflowRun{}, err
	}
	r, err := e.store.GetRun(id)
	if err != nil {
		return WorkflowRun{}, err
	}
	if r.Status == RunCancelling && len(r.ActiveNodeIDs) == 0 {
		return e.store.FinishCancellation(id)
	}
	return r, nil
}
func isVersionConflict(err error) bool {
	var se *StoreError
	return errors.As(err, &se) && se.Kind == StoreVersionConflict
}

func (e *WorkflowEngine) Execute(ctx context.Context, id RunID) (WorkflowRun, error) {
	release, err := e.runLock(ctx, id)
	if err != nil {
		return WorkflowRun{}, err
	}
	defer release()
	r, err := e.store.GetRun(id)
	if err != nil {
		return WorkflowRun{}, err
	}
	d, err := e.store.GetDefinition(r.DefinitionRevision)
	if err != nil {
		return WorkflowRun{}, err
	}
	if err = ValidateDefinition(d); err != nil {
		return WorkflowRun{}, err
	}
	var view definitionWire
	data, err := json.Marshal(d)
	if err != nil {
		return WorkflowRun{}, err
	}
	if err = decodeStrict(data, &view); err != nil {
		return WorkflowRun{}, err
	}
	if r.Status == RunQueued {
		r, err = e.store.TransitionRun(id, r.Version, RunRunning, nil)
		if err != nil {
			return WorkflowRun{}, err
		}
	}
	if r.Terminal() {
		return r, nil
	}
	if r.Status != RunRunning && r.Status != RunCancelling {
		return WorkflowRun{}, &ExecutionError{fmt.Sprintf("run cannot execute from %s", r.Status)}
	}
	for {
		if err := ctx.Err(); err != nil {
			return WorkflowRun{}, err
		}
		r, err = e.store.GetRun(id)
		if err != nil {
			return WorkflowRun{}, err
		}
		nodes, err := e.store.ListNodes(id)
		if err != nil {
			return WorkflowRun{}, err
		}
		states := map[NodeID]NodeState{}
		active, complete := false, true
		failed := []string{}
		for _, n := range nodes {
			states[n.NodeID] = n
			active = active || n.Status == NodeRunning
			complete = complete && n.Status.SatisfiesDependency()
			if n.Status == NodeFailed {
				detail := "failed"
				if n.Error != nil && *n.Error != "" {
					detail = *n.Error
				}
				failed = append(failed, string(n.NodeID)+": "+detail)
			}
		}
		if r.Status == RunCancelling {
			if !active {
				return e.store.FinishCancellation(id)
			}
			runtime.Gosched()
			continue
		}
		if r.Terminal() {
			return r, nil
		}
		if len(failed) != 0 {
			return e.store.FailRun(id, "workflow node failed: "+strings.Join(failed, "; "))
		}
		if complete {
			final := states[view.ReturnFrom]
			if len(final.ResultArtifactIDs) == 0 {
				return e.store.FailRun(id, "return node produced no artifact")
			}
			return e.store.FinalizeRun(id, r.Version, final.ResultArtifactIDs[len(final.ResultArtifactIDs)-1])
		}
		runnable := []Node{}
		for _, n := range view.Nodes {
			ready := states[n.ID].Status == NodePending
			for _, dep := range n.Needs {
				ready = ready && states[dep].Status.SatisfiesDependency()
			}
			if ready {
				runnable = append(runnable, n)
			}
		}
		if len(runnable) == 0 {
			return e.store.FailRun(id, "workflow graph is deadlocked")
		}
		limit := min(e.maxConcurrent, view.Budget.MaxConcurrentAgents, view.Budget.MaxAgents-int(r.AttemptsUsed), len(runnable))
		if limit <= 0 {
			return e.store.FailRun(id, "workflow attempt budget exhausted")
		}
		selected := runnable[:limit]
		prior := map[NodeID]NodeAttempt{}
		for _, a := range e.store.ListAttempts(id) {
			prior[a.NodeID] = a
		}
		claims := []AttemptClaim{}
		for i, n := range selected {
			raw, err := workflowID20("")
			if err != nil {
				return WorkflowRun{}, err
			}
			claim := AttemptClaim{NodeID: n.ID, AgentID: AgentID("wfagent_" + string(n.ID) + "_" + raw[:10]), SpawnIndex: SpawnIndex(int(r.AttemptsUsed) + i)}
			if n.Kind == Verify && len(n.Needs) != 0 {
				if a, ok := prior[n.Needs[0]]; ok {
					parent := a.AgentID
					claim.ParentAgentID = &parent
				}
			}
			claims = append(claims, claim)
		}
		e.mu.Lock()
		attempts, claimErr := e.store.ClaimNodes(id, claims, r.Version)
		if claimErr != nil {
			e.mu.Unlock()
			if isVersionConflict(claimErr) {
				continue
			}
			return WorkflowRun{}, claimErr
		}
		batchCtx, cancel := context.WithCancel(ctx)
		batch := &inflightBatch{cancel: cancel}
		e.inflight[id] = batch
		e.mu.Unlock()
		var wg sync.WaitGroup
		for i, a := range attempts {
			wg.Add(1)
			go func(a NodeAttempt, n Node) { defer wg.Done(); e.executeAttempt(batchCtx, a, n) }(a, selected[i])
		}
		wg.Wait()
		cancel()
		e.mu.Lock()
		delete(e.inflight, id)
		e.mu.Unlock()
		if ctx.Err() != nil {
			if _, err := e.store.CancelClaimedAttempts(id, nil); err != nil {
				return WorkflowRun{}, err
			}
			return WorkflowRun{}, ctx.Err()
		}
	}
}

func cloneWorkflowNode(n Node) Node {
	n.Needs = slices.Clone(n.Needs)
	n.ItemsFrom = recordPointer(n.ItemsFrom)
	if n.MaxRounds != nil {
		value := *n.MaxRounds
		n.MaxRounds = &value
	}
	return n
}
func (e *WorkflowEngine) inputsFor(a NodeAttempt, n Node) (Value, error) {
	r, err := e.store.GetRun(a.RunID)
	if err != nil {
		return Value{}, err
	}
	fields := []jsonvalue.Field{{Name: "args", Value: r.Args}}
	for _, dep := range n.Needs {
		artifacts, err := e.store.ArtifactsForNode(a.RunID, dep)
		if err != nil {
			return Value{}, err
		}
		var value Value
		if len(artifacts) == 1 {
			value = artifacts[0].Snapshot().Value
		} else {
			items := []Value{}
			for _, artifact := range artifacts {
				items = append(items, artifact.Snapshot().Value)
			}
			value = jsonvalue.ArrayValue(items)
		}
		fields = append(fields, jsonvalue.Field{Name: string(dep), Value: value})
	}
	return jsonvalue.ObjectValue(fields), nil
}
func (e *WorkflowEngine) executeAttempt(ctx context.Context, claimed NodeAttempt, node Node) {
	if ctx.Err() != nil {
		return
	}
	a, err := e.store.StartAttempt(claimed.AttemptID, claimed.Version)
	if err != nil {
		return
	}
	submission, err := e.runAttempt(ctx, a, node)
	verification := NotApplicable
	if err == nil && node.Kind == Verify && submission == nil {
		err = &RunnerError{RunnerAttributeError, "'NoneType' object has no attribute 'value'"}
	}
	if err == nil && node.Kind == Verify && submission != nil {
		verification = VerificationFromValue(submission.Value())
	}
	var artifact Artifact
	if err == nil {
		artifact, err = ArtifactFromSubmission(submission, ArtifactBinding{Run: a.RunID, Node: a.NodeID, Attempt: a.AttemptID, Schema: node.OutputSchema, Verification: verification})
	}
	input := CommitAttemptInput{AttemptID: a.AttemptID, ExpectedVersion: a.Version, AttemptStatus: AttemptSucceeded, NodeStatus: NodeSucceeded, Artifact: &artifact, Verification: &verification}
	if err == nil {
		if verification == Unverified {
			input.NodeStatus = NodeUnverified
		}
	} else if errors.Is(err, context.Canceled) || (errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil) {
		detail := "cancelled"
		input.AttemptStatus = AttemptCancelled
		input.NodeStatus = NodeCancelled
		input.Artifact = nil
		input.Verification = nil
		input.Error = &detail
	} else {
		reason := executionErrorReason(err)
		input.AttemptStatus = AttemptFailed
		input.NodeStatus = NodeFailed
		input.Artifact = nil
		input.Verification = nil
		input.Error = &reason
		if node.Kind == Verify {
			claim := node.ID
			if len(node.Needs) != 0 {
				claim = node.Needs[0]
			}
			value := jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "status", Value: jsonvalue.TextValue("unverified")}, {Name: "claim_id", Value: jsonvalue.TextValue(string(claim))}, {Name: "evidence", Value: jsonvalue.ArrayValue(nil)}, {Name: "reason", Value: jsonvalue.TextValue(reason)}})
			valid := false
			artifact, err = NewArtifact(ArtifactInput{Run: a.RunID, Node: a.NodeID, Attempt: a.AttemptID, Value: value, Schema: node.OutputSchema, Verification: Unverified, SchemaValid: &valid})
			if err != nil {
				return
			}
			v := Unverified
			input.Artifact = &artifact
			input.Verification = &v
			input.NodeStatus = NodeUnverified
		}
	}
	// Source gathers attempt exceptions and derives the next run outcome from state.
	_, _ = e.store.CommitAttempt(input)
}
func (e *WorkflowEngine) runAttempt(ctx context.Context, a NodeAttempt, node Node) (*ArtifactSubmission, error) {
	permit, err := e.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer permit.Release()
	inputs, err := e.inputsFor(a, node)
	if err != nil {
		return nil, err
	}
	return e.runner.Run(ctx, AttemptExecution{Attempt: a.Clone(), Node: cloneWorkflowNode(node), Inputs: inputs})
}
