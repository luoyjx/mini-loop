package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/decisions"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
)

type decisionSinkFixture struct {
	Secret  string
	Request decisions.Request
	Escaped struct {
		BackendRequest decisions.Request `json:"backend_request"`
		OutputModel    string            `json:"output_model"`
	}
	Cancellation struct {
		Propagated        bool
		DecisionCompleted bool          `json:"decision_completed"`
		DecisionFailed    bool          `json:"decision_failed"`
		ModelStatus       []ModelStatus `json:"model_status"`
		ActionStatus      ActionStatus  `json:"action_status"`
		Permit            int
	}
}

func decisionSinksFixture(t *testing.T) decisionSinkFixture {
	t.Helper()
	b, e := os.ReadFile("../testdata/python-decision-sinks.json")
	if e != nil {
		t.Fatal(e)
	}
	var f decisionSinkFixture
	if e = json.Unmarshal(b, &f); e != nil {
		t.Fatal(e)
	}
	return f
}

type decisionSinkRecorder struct {
	mu      sync.Mutex
	records []SessionEventRecord
}

func (s *decisionSinkRecorder) OnEvent(_ context.Context, r SessionEventRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, r.clone())
	return nil
}
func decisionValueContains(value decisions.Value, secret string, depth int) bool {
	if depth > 32 {
		return false
	}
	if text, ok := value.Text(); ok {
		if strings.Contains(text, secret) {
			return true
		}
		if nested, e := decisions.DecodeValue([]byte(text)); e == nil {
			return decisionValueContains(nested, secret, depth+1)
		}
	}
	if members, ok := value.Object(); ok {
		for key, v := range members {
			if strings.Contains(key, secret) || decisionValueContains(v, secret, depth+1) {
				return true
			}
		}
	}
	if items, ok := value.Array(); ok {
		for _, v := range items {
			if decisionValueContains(v, secret, depth+1) {
				return true
			}
		}
	}
	return false
}
func assertDecisionSink(t *testing.T, name string, b []byte, secret string) {
	t.Helper()
	value, e := decisions.DecodeValue(b)
	if e != nil {
		t.Fatal(name, e)
	}
	if decisionValueContains(value, secret, 0) {
		t.Fatal(name, "contains decoded secret")
	}
}
func TestDecisionEscapedResultMaskedBeforeEveryManagedSink(t *testing.T) {
	f := decisionSinksFixture(t)
	if f.Escaped.OutputModel != f.Secret {
		t.Fatal("source negative recipe no longer reproduces")
	}
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("canary", f.Secret)
	journal, _ := NewInMemoryActionJournal(10)
	store := newRuntimeStateStore()
	trace := &decisionRecording{}
	sink := &decisionSinkRecorder{}
	stage := 0
	provider := stateProviderFunc(func(_ context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
		stage++
		if stage == 1 {
			return fakeReply([]protocol.Block{protocol.NewToolUse("mask", protocol.DecisionToolInput(f.Request))}, protocol.StopToolUse), nil
		}
		blocks, _ := r.Messages[len(r.Messages)-1].Content.Blocks()
		out, ok := blocks[0].ToolResult()
		if !ok {
			t.Fatal("missing result")
		}
		value, e := decisions.DecodeValue([]byte(out.Content))
		if e != nil {
			t.Fatal(e)
		}
		members, _ := value.Object()
		model, _ := members["model"].Text()
		if model != secrets.Mask {
			t.Fatal("escaped model not masked")
		}
		return fakeReply([]protocol.Block{protocol.NewTextBlock("done")}, protocol.StopEndTurn), nil
	})
	cfg := runtimeConfig(t.TempDir(), provider)
	cfg.Mode = ModeAuto
	cfg.DecisionTools = true
	cfg.Secrets = registry
	cfg.ActionJournal = journal
	cfg.StateStore = store
	cfg.Trajectories = trace
	cfg.EventSink = sink
	cfg.DecisionProvider = decisionBackendFunc(func(_ context.Context, r decisions.Request) (decisions.Result, error) {
		a, _ := r.MarshalJSON()
		b, _ := f.Escaped.BackendRequest.MarshalJSON()
		compareDecisionJSON(t, a, b)
		return decisions.NewResult(r, "custom", f.Secret, decisions.LLMEstimate, map[string]decisions.Answer{"q": decisions.NoulResult(decisions.NoulAnswer{Probability: .9})}, nil)
	})
	session, e := NewManagedSession(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if out, e := session.Run(context.Background(), f.Secret); e != nil || out != "done" {
		t.Fatal(out, e)
	}
	for name, value := range map[string][]SessionEventRecord{"live/SSE": session.core.Events(), "event sink": sink.records, "state events": store.events[session.core.id]} {
		b, e := json.Marshal(value)
		if e != nil {
			t.Fatal(e)
		}
		assertDecisionSink(t, name, b, f.Secret)
	}
	b, e := json.Marshal(journal.records)
	if e != nil {
		t.Fatal(e)
	}
	assertDecisionSink(t, "action journal", b, f.Secret)
	b, e = json.Marshal(store.messages)
	if e != nil {
		t.Fatal(e)
	}
	assertDecisionSink(t, "stored transcript", b, f.Secret)
	for _, b := range trace.records {
		assertDecisionSink(t, "private trajectory", b, f.Secret)
	}
}
func TestDecisionCancellationMatchesSourceJournalAndPermit(t *testing.T) {
	f := decisionSinksFixture(t)
	pool, _ := NewConcurrencyLimiter(1)
	journal, _ := NewInMemoryActionJournal(10)
	entered := make(chan struct{})
	cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
	cfg.Mode = ModeAuto
	cfg.DecisionTools = true
	cfg.ModelLimiter = pool
	cfg.ActionJournal = journal
	cfg.DecisionProvider = decisionBackendFunc(func(ctx context.Context, r decisions.Request) (decisions.Result, error) {
		close(entered)
		<-ctx.Done()
		return decisions.Result{}, ctx.Err()
	})
	session, e := NewRuntimeSession(cfg)
	if e != nil {
		t.Fatal(e)
	}
	run, _ := DefaultRunContext()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	use := protocol.ToolUseBlock{ID: "cancel", Name: protocol.ToolDecision, Input: protocol.DecisionToolInput(f.Request)}
	done := make(chan error, 1)
	go func() { _, e := session.dispatchTool(ctx, run, use); done <- e }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("backend not reached")
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) || !f.Cancellation.Propagated {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation stuck")
	}
	completed, failed := false, false
	var statuses []ModelStatus
	for _, record := range session.Events() {
		if end, ok := record.Event.DecisionModelEnd(); ok {
			statuses = append(statuses, end.Status)
		}
		completed = completed || record.Event.Kind() == EventDecisionCompleted
		failed = failed || record.Event.Kind() == EventDecisionFailed
	}
	if completed != f.Cancellation.DecisionCompleted || failed != f.Cancellation.DecisionFailed || len(statuses) != 1 || statuses[0] != f.Cancellation.ModelStatus[0] {
		t.Fatal("cancellation metadata differs")
	}
	if len(journal.records) != 1 {
		t.Fatal("cancelled action record missing")
	}
	for _, record := range journal.records {
		if record.Status != f.Cancellation.ActionStatus {
			t.Fatal("action not cancelled", record.Status)
		}
	}
	acquire, cancelAcquire := context.WithTimeout(context.Background(), time.Second)
	defer cancelAcquire()
	lease, e := pool.Acquire(acquire)
	if e != nil || f.Cancellation.Permit != 1 {
		t.Fatal("permit leaked", e)
	}
	lease.Release()
}
