package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type executionClassifierFunc func(ToolCall) (ExecutionMode, error)

func (f executionClassifierFunc) ClassifyExecution(c ToolCall) (ExecutionMode, error) { return f(c) }

type scheduledHandlerFunc func(context.Context, ToolAuthority, protocol.ToolInput) (string, error)

func (f scheduledHandlerFunc) ExecuteTool(c context.Context, a ToolAuthority, i protocol.ToolInput) (string, error) {
	return f(c, a, i)
}

func scheduledSession(t *testing.T, calls []protocol.Block, handler ToolHandler, classifier ExecutionClassifier) *Session {
	t.Helper()
	definition, err := NewToolDefinition(protocol.ToolBash, ToolTraits{Risk: RiskExec, ParallelSafe: true}, handler)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := NewToolCatalog(definition.WithExecutionClassifier(classifier))
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{})
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewSessionWithGate("schedule", "owner", resourceProvider{tools: calls}, gate, ModeAuto, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	s.stuckDetector = NullStuckDetector{}
	return s
}
func receiveSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("synchronization timed out")
	}
}
func receiveRun(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("run timed out")
		return nil
	}
}

func TestParallelGroupsOverlapKeepOrderAndRespectBarrier(t *testing.T) {
	first := make(chan struct{})
	tail := make(chan struct{})
	var mu sync.Mutex
	var log []string
	handler := scheduledHandlerFunc(func(ctx context.Context, _ ToolAuthority, input protocol.ToolInput) (string, error) {
		value, _ := input.Bash()
		name := value.Command
		switch name {
		case "a":
			select {
			case <-first:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		case "b":
		case "d":
			select {
			case <-tail:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		case "e":
		}
		mu.Lock()
		log = append(log, name)
		mu.Unlock()
		if name == "b" {
			close(first)
		}
		if name == "e" {
			close(tail)
		}
		return name, nil
	})
	classifier := executionClassifierFunc(func(call ToolCall) (ExecutionMode, error) {
		value, _ := call.Input.Bash()
		if value.Command == "c" {
			return ExecutionExclusive, nil
		}
		return ExecutionParallel, nil
	})
	calls := []protocol.Block{}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		calls = append(calls, protocol.NewBashUse(name, name))
	}
	s := scheduledSession(t, calls, handler, classifier)
	if _, err := s.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	compareSchedulingResults(t, s, log)
	if !reflect.DeepEqual(log, []string{"b", "a", "c", "e", "d"}) {
		t.Fatalf("completion/barrier order: %v", log)
	}
	var results []string
	for _, message := range s.Messages() {
		blocks, _ := message.Content.Blocks()
		for _, block := range blocks {
			if result, ok := block.ToolResult(); ok {
				results = append(results, result.ToolUseID)
			}
		}
	}
	if !reflect.DeepEqual(results, []string{"a", "b", "c", "d", "e"}) {
		t.Fatalf("result order: %v", results)
	}
	for i, step := range s.recentSteps {
		hash, _ := OutputStepHash(results[i])
		if step.OutputHash != hash {
			t.Fatal("stuck ledger followed completion order")
		}
	}
	uses := eventRecordsOfKind(s.Events(), EventToolUse)
	for i, record := range uses {
		use, _ := record.Event.ToolUse()
		if use.ID != results[i] {
			t.Fatal("tool telemetry starts out of model order")
		}
	}
}

func TestExecutionClassifierFailureAndFreshBarrierState(t *testing.T) {
	calls := []protocol.Block{protocol.NewBashUse("first", "barrier"), protocol.NewBashUse("second", "after")}
	var changed atomic.Bool
	classifier := executionClassifierFunc(func(call ToolCall) (ExecutionMode, error) {
		if call.ID == "first" {
			return ExecutionExclusive, nil
		}
		if !changed.Load() {
			t.Error("classifier ran before earlier barrier")
		}
		return ExecutionParallel, nil
	})
	s := scheduledSession(t, calls, scheduledHandlerFunc(func(context.Context, ToolAuthority, protocol.ToolInput) (string, error) {
		changed.Store(true)
		return "ok", nil
	}), classifier)
	if _, err := s.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []executionClassifierFunc{
		func(ToolCall) (ExecutionMode, error) { return "bad", nil }, func(ToolCall) (ExecutionMode, error) { return ExecutionParallel, errors.New("broken") }, func(ToolCall) (ExecutionMode, error) { panic("private payload") },
	} {
		definition := s.gate.catalog.ordered[0].WithExecutionClassifier(f)
		if definition.ExecutionMode(gateTestCall("x")) != ExecutionExclusive {
			t.Fatal("broken classifier was parallel")
		}
	}
}

func TestParallelCancellationJoinsWorkersKeepsCompletedAndStopsBarrier(t *testing.T) {
	blocked := make(chan struct{})
	exited := make(chan struct{})
	var later atomic.Bool
	handler := scheduledHandlerFunc(func(ctx context.Context, _ ToolAuthority, input protocol.ToolInput) (string, error) {
		value, _ := input.Bash()
		switch value.Command {
		case "done":
			return "recorded", nil
		case "wait":
			close(blocked)
			<-ctx.Done()
			close(exited)
			return "", ctx.Err()
		default:
			later.Store(true)
			return "later", nil
		}
	})
	classifier := executionClassifierFunc(func(call ToolCall) (ExecutionMode, error) {
		if call.ID == "later" {
			return ExecutionExclusive, nil
		}
		return ExecutionParallel, nil
	})
	s := scheduledSession(t, []protocol.Block{protocol.NewBashUse("done", "done"), protocol.NewBashUse("wait", "wait"), protocol.NewBashUse("later", "later")}, handler, classifier)
	sub := s.Subscribe(false)
	defer sub.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.Run(ctx, "go"); done <- err }()
	receiveSignal(t, blocked)
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	awaiting := true
	for awaiting {
		select {
		case record := <-sub.Events():
			if result, ok := record.Event.ToolResult(); ok && result.ID == "done" {
				awaiting = false
			}
		case <-timer.C:
			t.Fatal("completed sibling was not recorded")
		}
	}
	cancel()
	if err := receiveRun(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	receiveSignal(t, exited)
	if later.Load() {
		t.Fatal("barrier ran after cancellation")
	}
	messages := s.Messages()
	if err := protocol.ValidateTranscript(messages); err != nil {
		t.Fatal(err)
	}
	blocks, _ := messages[len(messages)-1].Content.Blocks()
	for i, block := range blocks {
		result, _ := block.ToolResult()
		want := unknownToolResult
		if i == 0 {
			want = "recorded"
		}
		if result.Content != want {
			t.Fatalf("repair: %+v", result)
		}
	}
	if _, err := s.Run(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
}

func TestParallelFailureAndPanicReleaseSharedCapacity(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(map[bool]string{false: "handler error", true: "panic"}[panics], func(t *testing.T) {
			limiter, _ := NewConcurrencyLimiter(1)
			s := scheduledSession(t, []protocol.Block{protocol.NewBashUse("bad", "bad"), protocol.NewBashUse("good", "good")}, scheduledHandlerFunc(func(_ context.Context, _ ToolAuthority, input protocol.ToolInput) (string, error) {
				v, _ := input.Bash()
				if v.Command == "bad" {
					if panics {
						panic("sensitive")
					}
					return "", errors.New("broken")
				}
				return "good", nil
			}), nil)
			s.toolLimiter = limiter
			_, err := s.Run(context.Background(), "go")
			if panics {
				if err == nil || strings.Contains(err.Error(), "sensitive") {
					t.Fatalf("panic error: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if err := protocol.ValidateTranscript(s.Messages()); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			lease, err := limiter.Acquire(ctx)
			if err != nil {
				t.Fatal("capacity leaked", err)
			}
			lease.Release()
			lease.Release()
		})
	}
}

type pooledProvider struct {
	entered         chan struct{}
	release         chan struct{}
	active, maximum atomic.Int32
}

func (p *pooledProvider) Complete(ctx context.Context, _ protocol.ModelRequest) (protocol.ModelReply, error) {
	n := p.active.Add(1)
	defer p.active.Add(-1)
	for {
		old := p.maximum.Load()
		if n <= old || p.maximum.CompareAndSwap(old, n) {
			break
		}
	}
	p.entered <- struct{}{}
	select {
	case <-p.release:
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	case <-ctx.Done():
		return protocol.ModelReply{}, ctx.Err()
	}
}

func TestSharedModelAndToolPoolsCancelWaitingAndBypassExclusive(t *testing.T) {
	modelPool, _ := NewConcurrencyLimiter(1)
	provider := &pooledProvider{entered: make(chan struct{}, 2), release: make(chan struct{})}
	first, _ := NewSession("one", "owner", provider, echoExecutor{}, 1)
	second, _ := NewSession("two", "owner", provider, echoExecutor{}, 1)
	first.modelLimiter = modelPool
	second.modelLimiter = modelPool
	done := make(chan error, 1)
	go func() { _, err := first.Run(context.Background(), "one"); done <- err }()
	receiveSignal(t, provider.entered)
	sub := second.Subscribe(false)
	defer sub.Close()
	ctx, cancel := context.WithCancel(context.Background())
	waitingModel := make(chan error, 1)
	go func() { _, err := second.Run(ctx, "two"); waitingModel <- err }()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	awaiting := true
	for awaiting {
		select {
		case record := <-sub.Events():
			if _, ok := record.Event.ModelStart(); ok {
				awaiting = false
			}
		case <-timer.C:
			t.Fatal("waiting model did not start telemetry")
		}
	}
	cancel()
	if err := receiveRun(t, waitingModel); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if provider.maximum.Load() != 1 {
		t.Fatal("waiting model entered provider")
	}
	close(provider.release)
	if err := receiveRun(t, done); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Run(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}
	if provider.maximum.Load() != 1 {
		t.Fatal("shared model cap exceeded")
	}
	toolPool, _ := NewConcurrencyLimiter(1)
	lease, _ := toolPool.Acquire(context.Background())
	defer lease.Release()
	s := scheduledSession(t, []protocol.Block{protocol.NewBashUse("exclusive", "exclusive")}, scheduledHandlerFunc(func(context.Context, ToolAuthority, protocol.ToolInput) (string, error) { return "ok", nil }), executionClassifierFunc(func(ToolCall) (ExecutionMode, error) { return ExecutionExclusive, nil }))
	s.toolLimiter = toolPool
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if _, err := s.Run(ctx2, "go"); err != nil {
		t.Fatal("exclusive tool consumed parallel pool", err)
	}
	canceled, cancel3 := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() { _, err := toolPool.Acquire(canceled); waiting <- err }()
	cancel3()
	if err := receiveRun(t, waiting); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestToolPoolSharedAcrossSessionsAndCancelledAdmissionRepairsAll(t *testing.T) {
	pool, _ := NewConcurrencyLimiter(1)
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	var active, maximum atomic.Int32
	handler := scheduledHandlerFunc(func(ctx context.Context, _ ToolAuthority, _ protocol.ToolInput) (string, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if n <= old || maximum.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		select {
		case <-release:
			return "done", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	calls := []protocol.Block{protocol.NewBashUse("one", "one"), protocol.NewBashUse("two", "two")}
	first := scheduledSession(t, calls, handler, nil)
	second := scheduledSession(t, calls, handler, nil)
	first.toolLimiter, second.toolLimiter = pool, pool
	done := make(chan error, 2)
	go func() { _, err := first.Run(context.Background(), "one"); done <- err }()
	receiveSignal(t, entered)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiting := make(chan error, 1)
	go func() { _, err := second.Run(ctx, "two"); waiting <- err }()
	// Wait until the second transcript contains its batch, before admission.
	sub := second.Subscribe(true)
	defer sub.Close()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	awaiting := true
	for awaiting {
		select {
		case record := <-sub.Events():
			if _, ok := record.Event.ModelEnd(); ok {
				awaiting = false
			}
		case <-timer.C:
			t.Fatal("second model did not finish")
		}
	}
	cancel()
	if err := receiveRun(t, waiting); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := protocol.ValidateTranscript(second.Messages()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := receiveRun(t, done); err != nil {
		t.Fatal(err)
	}
	go func() { _, err := second.Run(context.Background(), "again"); done <- err }()
	if err := receiveRun(t, done); err != nil {
		t.Fatal(err)
	}
	if maximum.Load() != 1 {
		t.Fatalf("shared tool cap exceeded: %d", maximum.Load())
	}
}

func TestParallelFailureCauseSurvivesEarlierSiblingCancellation(t *testing.T) {
	entered := make(chan struct{})
	exited := make(chan struct{})
	s := scheduledSession(t, []protocol.Block{protocol.NewBashUse("waiting", "waiting"), protocol.NewBashUse("panic", "panic")}, scheduledHandlerFunc(func(ctx context.Context, _ ToolAuthority, input protocol.ToolInput) (string, error) {
		value, _ := input.Bash()
		if value.Command == "waiting" {
			close(entered)
			<-ctx.Done()
			close(exited)
			return "", ctx.Err()
		}
		<-entered
		panic("private")
	}), nil)
	_, err := s.Run(context.Background(), "go")
	if err == nil || !strings.Contains(err.Error(), "panicked") || strings.Contains(err.Error(), "private") {
		t.Fatalf("failure cause replaced: %v", err)
	}
	receiveSignal(t, exited)
	if err = protocol.ValidateTranscript(s.Messages()); err != nil {
		t.Fatal(err)
	}
}
