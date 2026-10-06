package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/decisions"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
)

// Raw JSON fields are confined to the external source-fixture boundary.
type decisionRuntimeFixture struct {
	System, Secret string
	Schemas        []protocol.ToolSchema
	Metadata       struct {
		Risk         ToolRisk
		Readonly     bool
		ParallelSafe bool `json:"parallel_safe"`
		Capabilities []Capability
	}
	LLM []struct {
		Name, Error           string
		ErrorType             decisions.ErrorKind `json:"error_type"`
		Input                 decisions.Request
		Reply                 protocol.ModelReply
		Result                json.RawMessage
		Requests              []protocol.ModelRequest
		ParentUnchanged       bool               `json:"parent_unchanged"`
		ParentContextRestored bool               `json:"parent_context_restored"`
		EventTypes            []SessionEventKind `json:"event_types"`
		ModelScopes           []struct {
			Agent           string
			Depth           int
			Purpose         protocol.RequestPurpose
			ParentMessageID MessageID `json:"parent_message_id"`
			HasChildMessage bool      `json:"has_child_message"`
		} `json:"model_scopes"`
	}
	Tools []struct {
		Name           string
		Mode           PermissionMode
		Input          decisions.Request
		Calls          []decisions.Request
		Events         []json.RawMessage
		Output         string
		Failed, Denied bool
	}
}

func readDecisionRuntimeFixture(t *testing.T) decisionRuntimeFixture {
	t.Helper()
	b, err := os.ReadFile("../testdata/python-decision-runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	var f decisionRuntimeFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

type decisionBackendFunc func(context.Context, decisions.Request) (decisions.Result, error)

func (f decisionBackendFunc) Evaluate(ctx context.Context, r decisions.Request) (decisions.Result, error) {
	return f(ctx, r)
}
func (f decisionBackendFunc) Model() string { return "configured-decision" }

func compareDecisionValues(t *testing.T, got, want decisions.Value, path string) {
	t.Helper()
	if a, ok := got.Number(); ok {
		b, yes := want.Number()
		if !yes {
			t.Fatalf("%s: numeric kind differs", path)
		}
		if strings.HasSuffix(path, ".confidence") || strings.HasSuffix(path, ".score") {
			x, _ := strconv.ParseFloat(a, 64)
			y, _ := strconv.ParseFloat(b, 64)
			if math.Abs(x-y) > 1e-12 {
				t.Fatalf("%s: %s != %s", path, a, b)
			}
		} else if a != b {
			t.Fatalf("%s: numeric spelling %s != %s", path, a, b)
		}
		return
	}
	if a, ok := got.Object(); ok {
		b, yes := want.Object()
		if !yes || len(a) != len(b) {
			t.Fatalf("%s: object shape differs", path)
		}
		for k, v := range a {
			expected, exists := b[k]
			if !exists {
				t.Fatalf("%s: unexpected key %q", path, k)
			}
			compareDecisionValues(t, v, expected, path+"."+k)
		}
		return
	}
	if a, ok := got.Array(); ok {
		b, yes := want.Array()
		if !yes || len(a) != len(b) {
			t.Fatalf("%s: array differs", path)
		}
		for i := range a {
			compareDecisionValues(t, a[i], b[i], path+"["+strconv.Itoa(i)+"]")
		}
		return
	}
	a, _ := got.MarshalJSON()
	b, _ := want.MarshalJSON()
	if string(a) != string(b) {
		t.Fatalf("%s: %s != %s", path, a, b)
	}
}
func compareDecisionJSON(t *testing.T, got, want []byte) {
	t.Helper()
	a, err := decisions.DecodeValue(got)
	if err != nil {
		t.Fatal(err)
	}
	b, err := decisions.DecodeValue(want)
	if err != nil {
		t.Fatal(err)
	}
	compareDecisionValues(t, a, b, "$")
}

func TestDecisionLLMMatchesActualPythonAndIsolatesParent(t *testing.T) {
	f := readDecisionRuntimeFixture(t)
	if f.System != decisions.LLMSystem {
		t.Fatal("system prompt drift")
	}
	for _, row := range f.LLM {
		t.Run(row.Name, func(t *testing.T) {
			requests := []protocol.ModelRequest{}
			provider := &decisionStreamProbe{complete: func(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
				requests = append(requests, request.Clone())
				if row.Name == "provider-fault" {
					return protocol.ModelReply{}, errors.New("private upstream credential")
				}
				return row.Reply.Clone(), nil
			}}
			cfg := runtimeConfig(t.TempDir(), provider)
			cfg.Model, cfg.Label, cfg.Depth = "configured-model", "parent", 2
			cache := &countingCachePolicy{}
			cfg.CachePolicy = cache
			s, err := NewRuntimeSession(cfg)
			if err != nil {
				t.Fatal(err)
			}
			s.messages = []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("unrelated private history")}}
			s.recoveryModel, s.lastModelSpan, s.lastStreamID, s.streamedText = "previous-parent-model", "parent-span", "parent-stream", "stream"
			before, meter := s.Messages(), s.TokenMeter()
			run, _ := DefaultRunContext()
			run.messageID = "parent-message"
			backend, err := NewLLMDecisionProvider(s, run, DecisionLLMConfig{})
			if err != nil {
				t.Fatal(err)
			}
			result, err := backend.Evaluate(context.Background(), row.Input)
			if row.Error != "" {
				var contract *decisions.Error
				if !errors.As(err, &contract) || contract.Kind() != row.ErrorType {
					t.Fatalf("error %v != %s %s", err, row.ErrorType, row.Error)
				}
				// The native JSON boundary also rejects escaped unpaired surrogates;
				// Python reaches the later root-shape refusal for this one recipe.
				if row.Name != "surrogate" && err.Error() != row.Error {
					t.Fatalf("%v != %s", err, row.Error)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				got, err := result.MarshalJSON()
				if err != nil {
					t.Fatal(err)
				}
				compareDecisionJSON(t, got, row.Result)
			}
			if !row.ParentUnchanged || !row.ParentContextRestored || !reflect.DeepEqual(before, s.Messages()) || !reflect.DeepEqual(meter, s.TokenMeter()) || s.recoveryModel != "previous-parent-model" || s.lastModelSpan != "parent-span" || s.lastStreamID != "parent-stream" || s.streamedText != "stream" || len(cache.purposes) != 0 || provider.streams != 0 {
				t.Fatal("parent state/cache/stream was changed")
			}
			if len(requests) != len(row.Requests) {
				t.Fatalf("calls %d != %d", len(requests), len(row.Requests))
			}
			for i, got := range requests {
				want := row.Requests[i]
				if got.Purpose != protocol.PurposeDecision || got.Model != want.Model || got.MaxTokens != want.MaxTokens || got.System == nil || *got.System != *want.System || got.Tools == nil || len(got.Tools) != 0 || len(got.Messages) != 1 || got.Messages[0].Role != protocol.RoleUser || !reflect.DeepEqual(got.Cache, protocol.CacheAnnotations{}) {
					t.Fatal("side-query request drift", got)
				}
				a, _ := got.Messages[0].Content.Plain()
				b, _ := want.Messages[0].Content.Plain()
				compareDecisionJSON(t, []byte(a), []byte(b))
			}
			kinds := []SessionEventKind{}
			starts := 0
			for _, record := range s.Events() {
				kinds = append(kinds, record.Event.Kind())
				if start, ok := record.Event.ModelStart(); ok {
					want := row.ModelScopes[starts]
					starts++
					context := record.Scope.RunContext.Snapshot()
					if record.Scope.Label != want.Agent || record.Scope.Depth != want.Depth || start.Purpose != want.Purpose || context.ParentMessageID == nil || *context.ParentMessageID != want.ParentMessageID || context.MessageID == run.MessageID() || context.Authority != AuthorityPeerAgent || context.ActorID != nil || len(context.ApprovedCapabilities) != 0 {
						t.Fatal("child provenance drift", record.Scope)
					}
				}
			}
			if starts != len(row.ModelScopes) || !slices.Equal(kinds, row.EventTypes) {
				t.Fatalf("events %v != %v", kinds, row.EventTypes)
			}
		})
	}
}

type decisionStreamProbe struct {
	complete stateProviderFunc
	streams  int
}

func (p *decisionStreamProbe) Complete(ctx context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	return p.complete(ctx, r)
}
func (p *decisionStreamProbe) CompleteStream(context.Context, protocol.ModelRequest, func(protocol.StreamDelta) error) (protocol.ModelReply, error) {
	p.streams++
	return protocol.ModelReply{}, errors.New("unexpected stream")
}

func decisionEventProjection(t *testing.T, record SessionEventRecord) []byte {
	t.Helper()
	b, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	allowed := []string{"type", "purpose", "model", "tool_count", "message_count", "status", "served_model", "usage", "prompt_tokens", "provider", "probability_source", "question_count", "error_type"}
	for k := range fields {
		if !slices.Contains(allowed, k) {
			delete(fields, k)
		}
	}
	b, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func TestDecisionToolMatchesPythonGateMaskAndMetadata(t *testing.T) {
	f := readDecisionRuntimeFixture(t)
	if !reflect.DeepEqual(f.Schemas, []protocol.ToolSchema{protocol.DecisionSchema()}) {
		t.Fatal("schema drift")
	}
	for _, row := range f.Tools {
		t.Run(row.Name, func(t *testing.T) {
			calls := []decisions.Request{}
			cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
			cfg.Mode, cfg.DecisionTools = row.Mode, true
			minimum := 1
			mask := secrets.New(secrets.Config{MinLength: &minimum})
			mask.RegisterValue("canary", f.Secret)
			if row.Name == "mask-invalid" {
				mask.RegisterValue("type", "noul")
			}
			cfg.Secrets = mask
			cfg.DecisionProvider = decisionBackendFunc(func(ctx context.Context, r decisions.Request) (decisions.Result, error) {
				calls = append(calls, r.Clone())
				if row.Name == "provider-fault" {
					return decisions.Result{}, errors.New("private upstream credential")
				}
				p := .9
				if row.Name == "bad-answer" {
					p = 9
				}
				usage := &decisions.TokenUsage{InputTokens: 13, OutputTokens: 3}
				if row.Name == "unavailable-usage" {
					usage = nil
				}
				return decisions.NewResult(r, "custom", "served-custom", decisions.LLMEstimate, map[string]decisions.Answer{"q": decisions.NoulResult(decisions.NoulAnswer{Probability: p})}, usage)
			})
			s, err := NewRuntimeSession(cfg)
			if err != nil {
				t.Fatal(err)
			}
			d, _ := s.gate.catalog.Lookup(protocol.ToolDecision)
			if d.Risk() != f.Metadata.Risk || d.Readonly() != f.Metadata.Readonly || d.ParallelSafe() != f.Metadata.ParallelSafe || len(d.Capabilities()) != len(f.Metadata.Capabilities) || d.ExecutionMode(ToolCall{}) != ExecutionExclusive {
				t.Fatal("traits drift")
			}
			run, _ := DefaultRunContext()
			outcome, err := s.dispatchTool(context.Background(), run, protocol.ToolUseBlock{ID: "decision-1", Name: protocol.ToolDecision, Input: protocol.DecisionToolInput(row.Input)})
			if err != nil {
				t.Fatal(err)
			}
			if outcome.Failed != row.Failed || outcome.Denied != row.Denied {
				t.Fatalf("outcome %+v", outcome)
			}
			if row.Failed || row.Denied {
				if outcome.Output != row.Output {
					t.Fatalf("%s != %s", outcome.Output, row.Output)
				}
			} else {
				compareDecisionJSON(t, []byte(outcome.Output), []byte(row.Output))
			}
			if len(calls) != len(row.Calls) {
				t.Fatalf("backend calls %d != %d", len(calls), len(row.Calls))
			}
			for i, call := range calls {
				got, _ := call.MarshalJSON()
				want, _ := row.Calls[i].MarshalJSON()
				compareDecisionJSON(t, got, want)
			}
			events := []SessionEventRecord{}
			for _, r := range s.Events() {
				switch r.Event.Kind() {
				case EventModelStart, EventModelEnd, EventDecisionCompleted, EventDecisionFailed:
					events = append(events, r)
				}
				got, err := json.Marshal(r)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(got), f.Secret) || strings.Contains(string(got), "private upstream") {
					t.Fatal("raw data in event")
				}
				if row.Name != "mask-invalid" { // Masked enum tokens intentionally cease to be executable input.
					decoded, err := DecodeStoredEvent(got)
					if err != nil {
						t.Fatal(err)
					}
					again, err := json.Marshal(decoded)
					if err != nil {
						t.Fatal(err)
					}
					compareDecisionJSON(t, got, again)
				}
			}
			if len(events) != len(row.Events) {
				t.Fatalf("events %d != %d", len(events), len(row.Events))
			}
			for i, record := range events {
				compareDecisionJSON(t, decisionEventProjection(t, record), row.Events[i])
			}
		})
	}
}

func TestDecisionDefaultsAndConstructorValidation(t *testing.T) {
	cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
	cfg.DecisionProvider = decisionBackendFunc(func(context.Context, decisions.Request) (decisions.Result, error) {
		t.Fatal("provider called during construction")
		return decisions.Result{}, nil
	})
	s, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.gate.catalog.Lookup(protocol.ToolDecision); ok {
		t.Fatal("backend alone enabled tool")
	}
	for _, options := range []DecisionLLMConfig{{Timeout: -1}, {MaxOutputTokens: -1}} {
		cfg.DecisionLLM = options
		if _, err := NewRuntimeSession(cfg); err == nil {
			t.Fatal("negative budget accepted")
		}
		if _, err := NewLLMDecisionProvider(s, RunContext{}, options); err == nil {
			t.Fatal("negative provider budget accepted")
		}
	}
	if _, err := NewLLMDecisionProvider(nil, RunContext{}, DecisionLLMConfig{}); err == nil {
		t.Fatal("nil parent accepted")
	}
	if _, err := (*LLMDecisionProvider)(nil).Evaluate(context.Background(), decisions.Request{}); err == nil {
		t.Fatal("nil provider accepted")
	}
}

func TestDecisionLimiterDeadlineCancellationAndConcurrency(t *testing.T) {
	f := readDecisionRuntimeFixture(t)
	row := f.LLM[0]
	pool, _ := NewConcurrencyLimiter(1)
	lease, _ := pool.Acquire(context.Background())
	var calls atomic.Int32
	cfg := runtimeConfig(t.TempDir(), stateProviderFunc(func(ctx context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
		calls.Add(1)
		return row.Reply.Clone(), nil
	}))
	cfg.ModelLimiter = pool
	s, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := NewLLMDecisionProvider(s, RunContext{}, DecisionLLMConfig{Timeout: 20 * time.Millisecond})
	_, err = p.Evaluate(context.Background(), row.Input)
	var contract *decisions.Error
	if !errors.As(err, &contract) || contract.Kind() != decisions.LLMTimeoutError || calls.Load() != 0 {
		t.Fatal("deadline did not cover limiter wait", err)
	}
	lease.Release()
	entered := make(chan struct{}, 1)
	p.provider = stateProviderFunc(func(ctx context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return protocol.ModelReply{}, ctx.Err()
	})
	p.config.Timeout = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := p.Evaluate(ctx, row.Input); done <- err }()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(pool.slots) != 0 {
		t.Fatal("permit leaked")
	}
	var active, maximum atomic.Int32
	p.provider = stateProviderFunc(func(ctx context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
		n := active.Add(1)
		for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
		}
		defer active.Add(-1)
		time.Sleep(time.Millisecond)
		return row.Reply.Clone(), nil
	})
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if _, err := p.Evaluate(context.Background(), row.Input); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	if maximum.Load() != 1 || len(pool.slots) != 0 {
		t.Fatal("shared limit drift")
	}
}

type decisionOnlyRole struct{}

func (decisionOnlyRole) Select(_ AgentRole, parent *ToolCatalog) (*ToolCatalog, error) {
	d, ok := parent.Lookup(protocol.ToolDecision)
	if !ok {
		return nil, errors.New("missing decision")
	}
	return NewToolCatalog(d)
}
func TestDecisionSelectedChildAndManagerWiring(t *testing.T) {
	row := readDecisionRuntimeFixture(t).Tools[0]
	var calls int
	backend := decisionBackendFunc(func(ctx context.Context, r decisions.Request) (decisions.Result, error) {
		calls++
		return decisions.NewResult(r, "custom", "served-custom", decisions.LLMEstimate, map[string]decisions.Answer{"q": decisions.NoulResult(decisions.NoulAnswer{Probability: .9})}, nil)
	})
	provider := stateProviderFunc(func(ctx context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
		if len(r.Messages) == 1 {
			return fakeReply([]protocol.Block{protocol.NewToolUse("child-decision", protocol.DecisionToolInput(row.Input))}, protocol.StopToolUse), nil
		}
		return fakeReply([]protocol.Block{protocol.NewTextBlock("child complete")}, protocol.StopEndTurn), nil
	})
	cfg := runtimeConfig(t.TempDir(), provider)
	cfg.DecisionTools, cfg.DecisionProvider = true, backend
	cfg.Approver = approverFunc(func(context.Context, ApprovalRequest) (bool, error) { return true, nil })
	s, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := s.rolePolicy.Select(RoleWorker, s.gate.catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := selected.Lookup(protocol.ToolDecision); ok {
		t.Fatal("default child received decision without capabilities")
	}
	s.rolePolicy = decisionOnlyRole{}
	if out, err := s.Delegate(context.Background(), "judge explicit data", RoleWorker); err != nil || out != "child complete" || calls != 1 {
		t.Fatal(out, err, calls)
	}
	completed := false
	for _, r := range s.Events() {
		if _, ok := r.Event.DecisionCompleted(); ok {
			completed = true
			if r.Scope.Depth != 1 || !strings.HasSuffix(r.Scope.Label, ">worker") {
				t.Fatal("child handler retained parent scope")
			}
		}
	}
	if !completed {
		t.Fatal("selected child did not emit completion")
	}
	manager, err := NewSessionManager(ManagerConfig{WorkspaceRoot: t.TempDir(), Services: ManagerServices{Provider: provider, DecisionTools: true, DecisionProvider: backend, DecisionLLM: DecisionLLMConfig{MaxOutputTokens: 123}}})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop(context.Background())
	managed, err := manager.Create(context.Background(), CreateSessionRequest{Owner: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := managed.core.gate.catalog.Lookup(protocol.ToolDecision); !ok || managed.core.decisionProvider == nil || managed.core.decisionLLM.MaxOutputTokens != 123 || managed.core.modelLimiter != manager.config.Services.ModelLimiter {
		t.Fatal("manager lost decision dependencies")
	}
	if _, err := NewSessionManager(ManagerConfig{Services: ManagerServices{Provider: provider, DecisionLLM: DecisionLLMConfig{Timeout: -1}}}); err == nil {
		t.Fatal("manager admitted invalid decision budgets")
	}
}

type decisionRecording struct{ records [][]byte }

func (w *decisionRecording) Start(TrajectoryStart) (TrajectoryID, error) {
	return "decision-trace", nil
}
func (w *decisionRecording) Append(_ TrajectoryID, r TrajectoryRecord) error {
	b, err := json.Marshal(r)
	if err == nil {
		w.records = append(w.records, b)
	}
	return err
}
func (w *decisionRecording) Finish(TrajectoryID, TrajectoryFinish) error { return nil }
func (w *decisionRecording) Count(SessionID) (int, error)                { return 0, nil }

func TestDecisionRealToolTurnsAndPrivateRecording(t *testing.T) {
	f := readDecisionRuntimeFixture(t)
	for _, custom := range []bool{true, false} {
		t.Run(fmt.Sprint(custom), func(t *testing.T) {
			row := f.LLM[0]
			q, _ := decisions.NewNoul(decisions.StringValue("Ready?"))
			request, _ := decisions.NewRequest(decisions.ObjectValue(map[string]decisions.Value{f.Secret: decisions.StringValue(f.Secret)}), map[string]decisions.Question{"q": q})
			stage := 0
			sideQueries := 0
			provider := stateProviderFunc(func(ctx context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
				if r.Purpose == protocol.PurposeDecision {
					sideQueries++
					reply := row.Reply.Clone()
					reply.Content = []protocol.Block{protocol.NewTextBlock(`{"distributions":{"q":{"true":0.9,"false":0.1}}}`)}
					return reply, nil
				}
				stage++
				if stage == 1 {
					reply := fakeReply([]protocol.Block{protocol.NewToolUse("decision-turn", protocol.DecisionToolInput(request))}, protocol.StopToolUse)
					reply.Usage.InputTokens = 10
					return reply, nil
				}
				blocks, _ := r.Messages[len(r.Messages)-1].Content.Blocks()
				result, ok := blocks[0].ToolResult()
				if !ok {
					t.Fatal("missing paired result")
				}
				if _, err := decisions.DecodeResult(request, []byte(result.Content)); err != nil {
					t.Fatal(err)
				}
				reply := fakeReply([]protocol.Block{protocol.NewTextBlock("judgment recorded")}, protocol.StopEndTurn)
				reply.Usage.InputTokens = 10
				return reply, nil
			})
			trace := &decisionRecording{}
			mask := secrets.New(secrets.Config{})
			mask.RegisterValue("canary", f.Secret)
			cfg := runtimeConfig(t.TempDir(), provider)
			cfg.Mode, cfg.DecisionTools, cfg.Secrets, cfg.Trajectories = ModeAuto, true, mask, trace
			if custom {
				cfg.DecisionProvider = decisionBackendFunc(func(ctx context.Context, r decisions.Request) (decisions.Result, error) {
					return decisions.NewResult(r, "custom", "served-custom", decisions.LLMEstimate, map[string]decisions.Answer{"q": decisions.NoulResult(decisions.NoulAnswer{Probability: .9})}, nil)
				})
			}
			s, err := NewManagedSession(cfg)
			if err != nil {
				t.Fatal(err)
			}
			out, err := s.Run(context.Background(), "evaluate")
			if err != nil || out != "judgment recorded" {
				t.Fatal(out, err)
			}
			if stage != 2 || sideQueries != map[bool]int{true: 0, false: 1}[custom] || s.core.TokenMeter().Observations != 2 {
				t.Fatal("parent model/meter was contaminated", stage, sideQueries, s.core.TokenMeter())
			}
			found := false
			for _, raw := range trace.records {
				if strings.Contains(string(raw), f.Secret) || strings.Contains(string(raw), `clé-\"private\"`) {
					t.Fatal("secret in private record")
				}
				var record struct {
					Type       SessionEventKind
					Purpose    protocol.RequestPurpose
					ModelInput json.RawMessage `json:"model_input"`
				}
				if err := json.Unmarshal(raw, &record); err != nil {
					t.Fatal(err)
				}
				if record.Type == EventModelStart && record.Purpose == protocol.PurposeDecision {
					found = true
					if len(record.ModelInput) == 0 {
						t.Fatal("missing full decision model input")
					}
					if custom {
						masked, err := decisions.DecodeRequest(record.ModelInput)
						if err != nil {
							t.Fatal(err)
						}
						state, _ := masked.State().Object()
						if _, ok := state[secrets.Mask]; !ok {
							t.Fatal("custom recording did not mask member names")
						}
					}
				}
			}
			if !found {
				t.Fatal("no decision private span")
			}
			for _, record := range s.Events() {
				raw, err := json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(raw), "model_input") {
					t.Fatal("private input leaked into live SSE projection")
				}
			}
		})
	}
}

func TestDecisionGateRewriteJournalReplayAndCancellation(t *testing.T) {
	criteria := map[string]decisions.Value{}
	probabilities := map[string]float64{}
	for i := 0; i < 255; i++ {
		key := fmt.Sprintf("criterion-%03d", i)
		criteria[key] = decisions.NullValue()
		probabilities[key] = 1.0 / 255
	}
	question, _ := decisions.NewChoice(decisions.StringValue("Choose?"), criteria)
	questions := map[string]decisions.Question{}
	for i := 0; i < 8; i++ {
		questions[fmt.Sprint(i)] = question
	}
	request, err := decisions.NewRequest(decisions.StringValue("final rewritten evidence"), questions)
	if err != nil {
		t.Fatal(err)
	}
	answers := map[string]decisions.Answer{}
	for key := range questions {
		answers[key] = decisions.ChoiceResult(decisions.ChoiceAnswer{Choice: "criterion-000", Confidence: 0, Probabilities: probabilities})
	}
	result, err := decisions.NewResult(request, "custom", "served-custom", decisions.LLMEstimate, answers, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := result.MarshalJSON()
	if len(encoded) <= 4000 {
		t.Fatal("large-result probe too small")
	}
	journal, _ := NewInMemoryActionJournal(10)
	calls := 0
	order := []string{}
	cfg := runtimeConfig(t.TempDir(), &FakeProvider{})
	cfg.Mode, cfg.DecisionTools, cfg.ActionJournal = ModeAuto, true, journal
	cfg.DecisionProvider = decisionBackendFunc(func(ctx context.Context, r decisions.Request) (decisions.Result, error) {
		calls++
		order = append(order, "execute")
		a, _ := r.MarshalJSON()
		b, _ := request.MarshalJSON()
		compareDecisionJSON(t, a, b)
		return result, nil
	})
	cfg.Hooks = GateHooks{
		Before: []BeforeHook{beforeHookFunc(func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
			order = append(order, "before")
			return RewriteToolCall(protocol.DecisionToolInput(request)), nil
		})},
		Guards: []GuardHook{guardHookFunc(func(ctx context.Context, a ToolAuthority, c ToolCall) (string, bool, error) {
			order = append(order, "guard")
			r, _ := c.Input.Decision()
			if len(r.Questions()) != 8 {
				t.Fatal("guard saw original input")
			}
			return "", false, nil
		})},
		After: []AfterHook{afterHookFunc(func(ctx context.Context, a ToolAuthority, c ToolCall, out string) (string, error) {
			order = append(order, "after")
			return out, nil
		})},
		Observers: []ResultObserver{observerFunc(func(ctx context.Context, a ToolAuthority, c ToolCall, out ToolOutcome) error {
			order = append(order, "observer")
			if out.Denied {
				return nil
			}
			r, ok, err := journal.Get(ctx, a.ActionID)
			if err != nil || !ok || r.Status != ActionCompleted || r.Result == nil || *r.Result != out.Output {
				t.Fatal("observer preceded exact result settlement", err)
			}
			return nil
		})},
	}
	s, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	input := protocol.DecisionToolInput(readDecisionRuntimeFixture(t).Tools[0].Input)
	run, _ := DefaultRunContext()
	use := protocol.ToolUseBlock{ID: "large", Name: protocol.ToolDecision, Input: input}
	out, err := s.dispatchTool(context.Background(), run, use)
	if err != nil || out.Failed || out.Output != string(encoded) || calls != 1 {
		t.Fatal("large result was truncated", err)
	}
	if !slices.Equal(order, []string{"before", "guard", "execute", "after", "observer"}) {
		t.Fatal(order)
	}
	before := len(s.Events())
	out, err = s.dispatchTool(context.Background(), run, use)
	if err != nil || !out.Replayed || out.Output != string(encoded) || calls != 1 {
		t.Fatal("exact replay failed", err)
	}
	for _, r := range s.Events()[before:] {
		if r.Event.Kind() == EventDecisionCompleted || r.Event.Kind() == EventModelStart {
			t.Fatal("replay repeated provider metadata")
		}
	}
	s.mode = ModeReadonly
	out, err = s.dispatchTool(context.Background(), run, use)
	if err != nil || !out.Denied || out.Replayed || calls != 1 {
		t.Fatal("replay bypassed current permission", out, err)
	}
	// A cancelled custom query settles through the same journal and releases its
	// shared model permit; it emits cancelled model telemetry without completion.
	entered := make(chan struct{}, 1)
	pool, _ := NewConcurrencyLimiter(1)
	cfg.Hooks = GateHooks{}
	cfg.ModelLimiter = pool
	cfg.DecisionProvider = decisionBackendFunc(func(ctx context.Context, r decisions.Request) (decisions.Result, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return decisions.Result{}, ctx.Err()
	})
	s, err = NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := s.dispatchTool(ctx, run, protocol.ToolUseBlock{ID: "cancel", Name: protocol.ToolDecision, Input: input})
		done <- err
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(pool.slots) != 0 {
		t.Fatal("custom permit leaked")
	}
	ended := false
	for _, r := range s.Events() {
		if e, ok := r.Event.DecisionModelEnd(); ok {
			ended = e.Status == ModelCancelled
		}
		if r.Event.Kind() == EventDecisionCompleted || r.Event.Kind() == EventDecisionFailed {
			t.Fatal("cancellation became decision failure/completion")
		}
	}
	if !ended {
		t.Fatal("missing cancelled custom span")
	}
}

func TestDecisionRecoveryFallbackAndProviderPanicStayIsolated(t *testing.T) {
	row := readDecisionRuntimeFixture(t).LLM[0]
	waits := &recoveryTestWaiter{}
	retries := 3
	recovery, err := NewDefaultRecovery(RecoveryConfig{FallbackModel: "fallback-model", MaxRetries: &retries, Waiter: waits, Jitter: func() float64 { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	requests := []protocol.ModelRequest{}
	cfg := runtimeConfig(t.TempDir(), stateProviderFunc(func(ctx context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
		calls++
		requests = append(requests, r.Clone())
		if calls <= 3 {
			return protocol.ModelReply{}, &recoveryFixtureError{protocol.ModelFailure{Kind: protocol.ModelFailureOverloaded, Class: "APIStatusError", Status: 529, Message: "overloaded"}}
		}
		reply := row.Reply.Clone()
		reply.Model = "served-fallback"
		return reply, nil
	}))
	cfg.Recovery = recovery
	cfg.Model = "configured-model"
	s, err := NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	s.recoveryModel = "previous-parent-model"
	backend, err := NewLLMDecisionProvider(s, RunContext{}, DecisionLLMConfig{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := backend.Evaluate(context.Background(), row.Input)
	if err != nil || result.Model() != "served-fallback" || calls != 4 || s.recoveryModel != "previous-parent-model" {
		t.Fatal(result.Model(), err, calls)
	}
	for i, r := range requests {
		expected := "configured-model"
		if i == 3 {
			expected = "fallback-model"
		}
		if r.Model != expected {
			t.Fatal("parent recovery override reached query", r.Model)
		}
		a, _ := json.Marshal(r.Messages)
		b, _ := json.Marshal(requests[0].Messages)
		if string(a) != string(b) || len(r.Tools) != 0 || r.MaxTokens != 4096 {
			t.Fatal("retry changed authoritative query")
		}
	}
	backend.provider = stateProviderFunc(func(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
		panic("private panic value")
	})
	_, err = backend.Evaluate(context.Background(), row.Input)
	var contract *decisions.Error
	if !errors.As(err, &contract) || contract.Kind() != decisions.LLMProviderError || strings.Contains(err.Error(), "private") {
		t.Fatal("provider panic escaped", err)
	}
	cfg.DecisionTools, cfg.Mode = true, ModeAuto
	cfg.DecisionProvider = decisionBackendFunc(func(context.Context, decisions.Request) (decisions.Result, error) { panic("private panic value") })
	s, err = NewRuntimeSession(cfg)
	if err != nil {
		t.Fatal(err)
	}
	run, _ := DefaultRunContext()
	out, err := s.dispatchTool(context.Background(), run, protocol.ToolUseBlock{ID: "panic", Name: protocol.ToolDecision, Input: protocol.DecisionToolInput(row.Input)})
	if err != nil || !out.Failed || out.Output != "Error: Decision failed: RuntimeError" {
		t.Fatal(out, err)
	}
}
