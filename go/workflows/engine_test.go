package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/runmeta"
)

type engineRecipe struct {
	Name, Graph, Behavior string
	Status                RunStatus
	Verify                bool
	Cancel, Blocked       bool
	Verification          *string
	Concurrency           *int
	AttemptsUsed          AttemptCount `json:"attempts_used"`
	NodeBefore            NodeStatus   `json:"node_before"`
}
type engineCall struct {
	NodeID     NodeID     `json:"node_id"`
	SpawnIndex SpawnIndex `json:"spawn_index"`
	Inputs     Value      `json:"inputs"`
}

func engineStore(t *testing.T, recipe engineRecipe) (*InMemoryStore, RunID, []NodeID) {
	t.Helper()
	nodes := []Node{{ID: "a", Kind: Agent, OutputSchema: objectSchema()}}
	if recipe.Verify {
		nodes[0].Kind = Verify
	}
	ret := NodeID("a")
	switch recipe.Graph {
	case "diamond", "parallel":
		nodes = []Node{{ID: "a", Kind: Agent, OutputSchema: objectSchema()}, {ID: "b", Kind: Agent, OutputSchema: objectSchema()}}
		if recipe.Graph == "diamond" {
			nodes = append(nodes, Node{ID: "c", Kind: Reduce, Needs: []NodeID{"a", "b"}, OutputSchema: objectSchema()}, Node{ID: "v", Kind: Verify, Needs: []NodeID{"c"}, OutputSchema: objectSchema()})
			ret = "v"
		}
	case "args":
		nodes = []Node{{ID: "args", Kind: Agent, OutputSchema: objectSchema()}, {ID: "a", Kind: Reduce, Needs: []NodeID{"args"}, OutputSchema: objectSchema()}}
	}
	// Source omitted graph-node output schemas default to type:object.
	for i := range nodes {
		if nodes[i].Needs == nil {
			nodes[i].Needs = []NodeID{}
		}
	}
	body, err := json.Marshal(struct {
		Name       string   `json:"name"`
		Revision   Revision `json:"revision"`
		ReturnFrom NodeID   `json:"return_from"`
		Nodes      []Node   `json:"nodes"`
	}{"wf", "base", ret, nodes})
	if err != nil {
		t.Fatal(err)
	}
	d, err := DecodeDefinition(body)
	if err != nil {
		t.Fatal(err)
	}
	s := NewInMemoryStore()
	if _, err = s.RegisterDefinition(d); err != nil {
		t.Fatal(err)
	}
	args, err := jsonvalue.Decode(`{"input":1}`)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateRun(CreateRunInput{DefinitionRevision: "base", SessionID: "s", IdempotencyKey: "k", Args: args, RunContext: runmeta.Snapshot{MessageID: "msg", Origin: "api", Channel: "internal", Authority: runmeta.AuthorityUntrusted, StampedBy: "mini_loop", ApprovedCapabilities: []runmeta.Capability{}}})
	if err != nil {
		t.Fatal(err)
	}
	r.Status = recipe.Status
	if r.Status == "" {
		r.Status = RunQueued
	}
	r.AttemptsUsed = recipe.AttemptsUsed
	s.runs[r.RunID] = r
	if recipe.NodeBefore != "" {
		key := nodeKey{r.RunID, "a"}
		n := s.nodes[key]
		n.Status = recipe.NodeBefore
		if n.Status == NodeFailed {
			msg := "seeded failure"
			n.Error = &msg
		}
		s.nodes[key] = n
	}
	ids := []NodeID{}
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	return s, r.RunID, ids
}
func engineProjection(t *testing.T, s *InMemoryStore, id RunID, nodeIDs []NodeID, calls []engineCall) Value {
	t.Helper()
	r, err := s.GetRun(id)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := s.ListNodes(id)
	if err != nil {
		t.Fatal(err)
	}
	attempts := s.ListAttempts(id)
	labels := map[string]string{string(id): "<run>"}
	for i, a := range attempts {
		labels[string(a.AttemptID)] = "<attempt-" + string(a.NodeID) + ">"
		labels[string(a.AgentID)] = "<agent-" + string(a.NodeID) + ">"
		for _, stamp := range []*float64{attempts[i].StartedAt, attempts[i].HeartbeatAt, attempts[i].EndedAt} {
			if stamp != nil {
				*stamp = 0
			}
		}
	}
	artifacts := []ArtifactSnapshot{}
	for _, nid := range nodeIDs {
		items, err := s.ArtifactsForNode(id, nid)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range items {
			v := a.Snapshot()
			v.CreatedAt = 0
			artifacts = append(artifacts, v)
			labels[string(v.ArtifactID)] = "<artifact-" + string(nid) + ">"
		}
	}
	messages := s.ListOutbox(OutboxFilter{RunID: &id})
	for i, m := range messages {
		labels[string(m.MessageID)] = "<outbox>"
		messages[i].CreatedAt = 0
	}
	slices.SortFunc(calls, func(a, b engineCall) int { return int(a.SpawnIndex - b.SpawnIndex) })
	body, err := json.Marshal(struct {
		Run       WorkflowRun        `json:"run"`
		Nodes     []NodeState        `json:"nodes"`
		Attempts  []NodeAttempt      `json:"attempts"`
		Artifacts []ArtifactSnapshot `json:"artifacts"`
		Outbox    []OutboxSnapshot   `json:"outbox"`
		Calls     []engineCall       `json:"calls"`
	}{runProjection(r), nodes, attempts, artifacts, messages, calls})
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonvalue.Decode(string(body))
	if err != nil {
		t.Fatal(err)
	}
	return normalizeStoreIDs(v, labels)
}
func TestWorkflowEngineMatchesActualPython(t *testing.T) {
	var fixture struct {
		Rows []struct {
			Recipe        engineRecipe
			OutputJSON    string `json:"output_json"`
			Error, Detail string
			RepeatError   string `json:"repeat_error"`
		}
		Constructor []struct {
			Limit         int
			Error, Detail string
		}
	}
	body, err := os.ReadFile("../testdata/python-workflow-engine.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(body, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Rows) != 42 {
		t.Fatalf("engine corpus rows=%d", len(fixture.Rows))
	}
	for _, row := range fixture.Rows {
		t.Run(row.Recipe.Name, func(t *testing.T) {
			s, id, nodeIDs := engineStore(t, row.Recipe)
			calls := []engineCall{}
			var mu sync.Mutex
			started := make(chan struct{})
			runner := WorkflowRunnerFunc(func(ctx context.Context, input AttemptExecution) (*ArtifactSubmission, error) {
				mu.Lock()
				calls = append(calls, engineCall{input.Node.ID, input.Attempt.SpawnIndex, input.Inputs})
				mu.Unlock()
				if row.Recipe.Cancel {
					close(started)
					<-ctx.Done()
					return nil, ctx.Err()
				}
				switch row.Recipe.Behavior {
				case "runtime":
					return nil, &RunnerError{RunnerRuntimeError, "worker failed"}
				case "value-error":
					return nil, &RunnerError{RunnerValueError, "worker failed"}
				case "type-error":
					return nil, &RunnerError{RunnerTypeError, "worker failed"}
				case "nil":
					return nil, nil
				case "wrong-tool":
					s := NewArtifactSubmission(jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "ok", Value: jsonvalue.BoolValue(true)}}), "wrong")
					return &s, nil
				case "bad-value":
					s := ReturnArtifact(jsonvalue.TextValue("wrong"))
					return &s, nil
				}
				var value Value
				if input.Node.Kind == Verify {
					status := "verified"
					if row.Recipe.Verification != nil {
						status = *row.Recipe.Verification
					}
					value = jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "status", Value: jsonvalue.TextValue(status)}})
				} else {
					value = jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "node", Value: jsonvalue.TextValue(string(input.Node.ID))}})
				}
				s := ReturnArtifact(value)
				return &s, nil
			})
			options := EngineOptions{MaxConcurrentAgents: row.Recipe.Concurrency}
			if row.Recipe.Blocked {
				options.AttemptPool, _ = NewAttemptPool(0)
			}
			e, err := NewWorkflowEngine(s, runner, options)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if row.Recipe.Cancel {
				result := make(chan error, 1)
				go func() { _, err := e.Execute(ctx, id); result <- err }()
				if row.Recipe.Blocked {
					for {
						attempts := s.ListAttempts(id)
						if len(attempts) > 0 && attempts[0].Status == AttemptRunning {
							break
						}
						select {
						case <-ctx.Done():
							t.Fatal("permit wait never started")
						case <-time.After(time.Millisecond):
						}
					}
				} else {
					select {
					case <-started:
					case <-ctx.Done():
						t.Fatal("runner never started")
					}
				}
				if _, err := e.Cancel(id, nil); err != nil {
					t.Fatal(err)
				}
				err = <-result
			} else {
				_, err = e.Execute(ctx, id)
			}
			kind, detail := engineError(err)
			if kind != row.Error || detail != row.Detail {
				t.Fatalf("entry error %s %s want %s %s", kind, detail, row.Error, row.Detail)
			}
			compareAttemptProjection(t, engineProjection(t, s, id, nodeIDs, calls), row.OutputJSON)
			golden, err := jsonvalue.Decode(row.OutputJSON)
			if err != nil {
				t.Fatal(err)
			}
			expectedCalls, _ := golden.Lookup("calls")
			expected, _ := expectedCalls.Array()
			slices.SortFunc(calls, func(a, b engineCall) int { return int(a.SpawnIndex - b.SpawnIndex) })
			for i, call := range calls {
				want, _ := expected[i].Lookup("inputs")
				wantBytes, _ := want.MarshalJSON()
				gotBytes, _ := call.Inputs.MarshalJSON()
				if string(gotBytes) != string(wantBytes) {
					t.Fatalf("runner input member order: %s want %s", gotBytes, wantBytes)
				}
			}
			_, err = e.Execute(ctx, id)
			kind, _ = engineError(err)
			if kind != row.RepeatError {
				t.Fatalf("repeat %v want %s", err, row.RepeatError)
			}
			if len(e.locks) != 0 || len(e.inflight) != 0 {
				t.Fatal("engine retained idle execution handles")
			}
		})
	}
	for _, row := range fixture.Constructor {
		_, err := NewWorkflowEngine(NewInMemoryStore(), WorkflowRunnerFunc(func(context.Context, AttemptExecution) (*ArtifactSubmission, error) { return nil, nil }), EngineOptions{MaxConcurrentAgents: &row.Limit})
		kind, detail := engineError(err)
		if kind != row.Error || detail != row.Detail {
			t.Fatalf("constructor %d: %s %s want %s %s", row.Limit, kind, detail, row.Error, row.Detail)
		}
	}
}
func engineError(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	var execution *ExecutionError
	if errors.As(err, &execution) {
		return "WorkflowExecutionError", execution.Detail
	}
	var se *StoreError
	if errors.As(err, &se) {
		return string(se.Kind), se.Detail
	}
	return "NativeError", err.Error()
}

func TestWorkflowEngineSharedPoolAndExecuteLock(t *testing.T) {
	pool, err := NewAttemptPool(1)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan RunID, 4)
	resume := make(chan struct{})
	var mu sync.Mutex
	active, maximum, calls := 0, 0, 0
	runner := WorkflowRunnerFunc(func(ctx context.Context, input AttemptExecution) (*ArtifactSubmission, error) {
		mu.Lock()
		active++
		calls++
		maximum = max(maximum, active)
		mu.Unlock()
		defer func() { mu.Lock(); active--; mu.Unlock() }()
		started <- input.Attempt.RunID
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-resume:
		}
		s := ReturnArtifact(jsonvalue.ObjectValue(nil))
		return &s, nil
	})
	s1, id1, _ := engineStore(t, engineRecipe{})
	s2, id2, _ := engineStore(t, engineRecipe{})
	e1, _ := NewWorkflowEngine(s1, runner, EngineOptions{AttemptPool: pool})
	e2, _ := NewWorkflowEngine(s2, runner, EngineOptions{AttemptPool: pool})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	results := make(chan error, 3)
	go func() { _, err := e1.Execute(ctx, id1); results <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("first runner never started")
	}
	go func() { _, err := e1.Execute(ctx, id1); results <- err }()
	go func() { _, err := e2.Execute(ctx, id2); results <- err }()
	close(resume)
	for i := 0; i < 3; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 || maximum != 1 {
		t.Fatalf("shared permit/execute lock: calls=%d maximum=%d", calls, maximum)
	}
}

func TestWorkflowEngineCancellationWhileWaitingAndRunning(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprint(blocked), func(t *testing.T) {
			poolSize := 1
			if blocked {
				poolSize = 0
			}
			pool, _ := NewAttemptPool(poolSize)
			s, id, _ := engineStore(t, engineRecipe{})
			started := make(chan struct{})
			e, err := NewWorkflowEngine(s, WorkflowRunnerFunc(func(ctx context.Context, input AttemptExecution) (*ArtifactSubmission, error) {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			}), EngineOptions{AttemptPool: pool})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				r, err := e.Execute(ctx, id)
				if err == nil && r.Status != RunCancelled {
					err = fmt.Errorf("cancel result %s", r.Status)
				}
				result <- err
			}()
			if blocked {
				for {
					attempts := s.ListAttempts(id)
					if len(attempts) != 0 && attempts[0].Status == AttemptRunning {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("permit-wait attempt never started")
					case <-time.After(time.Millisecond):
					}
				}
			} else {
				select {
				case <-started:
				case <-ctx.Done():
					t.Fatal("runner never started")
				}
			}
			if _, err := e.Cancel(id, nil); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("cancel did not settle")
			}
			attempts := s.ListAttempts(id)
			if len(attempts) != 1 || attempts[0].Status != AttemptCancelled {
				t.Fatalf("cancelled attempts %+v", attempts)
			}
			if blocked {
				select {
				case <-started:
					t.Fatal("blocked runner invoked")
				default:
				}
			}
		})
	}
}

func TestWorkflowEngineParentDeadlineAndCancelledLockWait(t *testing.T) {
	s, id, _ := engineStore(t, engineRecipe{})
	started := make(chan struct{})
	e, err := NewWorkflowEngine(s, WorkflowRunnerFunc(func(ctx context.Context, input AttemptExecution) (*ArtifactSubmission, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}), EngineOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := e.Execute(ctx, id); result <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("runner never started")
	}
	waitCtx, stopWait := context.WithCancel(context.Background())
	stopWait()
	if _, err := e.Execute(waitCtx, id); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lock wait: %v", err)
	}
	if err := <-result; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("parent deadline: %v", err)
	}
	attempts := s.ListAttempts(id)
	if len(attempts) != 1 || attempts[0].Status != AttemptCancelled {
		t.Fatalf("deadline settlement: %+v", attempts)
	}
	r, _ := s.GetRun(id)
	if r.Status != RunRunning {
		t.Fatalf("service-owned run cancellation: %s", r.Status)
	}
	if len(e.locks) != 0 || len(e.inflight) != 0 {
		t.Fatal("deadline retained engine bookkeeping")
	}
}
