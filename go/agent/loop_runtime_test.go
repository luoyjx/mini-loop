package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/secrets"
	"github.com/luoyjx/mini-loop/go/workspace"
)

func repeatedSession(t *testing.T, provider *loopingProvider, hooks GateHooks, max int) *Session {
	t.Helper()
	catalog := gateTestCatalog(t, &gateHandler{output: "same"})
	gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), hooks)
	if err != nil {
		t.Fatal(err)
	}
	session, err := NewSessionWithGate("s", "owner", provider, gate, ModeAuto, "", max)
	if err != nil {
		t.Fatal(err)
	}
	return session
}
func TestStuckWindowBoundAndUserTurnReset(t *testing.T) {
	provider := &loopingProvider{}
	session := repeatedSession(t, provider, GateHooks{}, 40)
	session.stuckDetector = NullStuckDetector{}
	if _, err := session.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if len(session.recentSteps) != StuckWindow {
		t.Fatal("evidence window unbounded")
	}
	provider = &loopingProvider{}
	session = repeatedSession(t, provider, GateHooks{}, 12)
	thresholds := DefaultStuckThresholds()
	thresholds.MaxNudges = 0
	session.stuckDetector, _ = NewStuckDetector(thresholds)
	for _, prompt := range []string{"first", "second"} {
		before := provider.calls
		if _, err := session.Run(context.Background(), prompt); err != nil {
			t.Fatal(err)
		}
		if provider.calls-before != 4 {
			t.Fatal("new user intent inherited old stuck evidence")
		}
	}
}
func TestStuckComparesFinalRewrittenInputs(t *testing.T) {
	provider := &loopingProvider{varyingCommands: true}
	hooks := GateHooks{Before: []BeforeHook{beforeHookFunc(func(context.Context, ToolAuthority, ToolCall) (BeforeDecision, error) {
		return RewriteToolCall(protocol.BashToolInput(protocol.BashInput{Command: "printf normalized"})), nil
	})}}
	session := repeatedSession(t, provider, hooks, 12)
	_, err := session.Run(context.Background(), "go")
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 8 {
		t.Fatal("step identity used model arguments before rewrite")
	}
	want, _ := InputStepHash(protocol.BashToolInput(protocol.BashInput{Command: "printf normalized"}))
	for _, step := range session.recentSteps {
		if step.InputHash != want {
			t.Fatal("wrong rewritten hash")
		}
	}
}

type observingDetector struct {
	states  []StuckState
	failure error
}

func (detector *observingDetector) MaxNudges() int { return 0 }
func (detector *observingDetector) Inspect(state StuckState) (*StuckSignal, error) {
	detector.states = append(detector.states, state)
	return nil, detector.failure
}
func TestFailedDetectorPreservesAnsweredBatch(t *testing.T) {
	session := repeatedSession(t, &loopingProvider{}, GateHooks{}, 12)
	detector := &observingDetector{failure: errors.New("inspection failed")}
	session.stuckDetector = detector
	_, err := session.Run(context.Background(), "go")
	if !errors.Is(err, detector.failure) {
		t.Fatal("detector failure hidden")
	}
	if err := protocol.ValidateTranscript(session.Messages()); err != nil {
		t.Fatal("inspection failure orphaned answered tools")
	}
	detector.states[0].Steps[0].Name = protocol.ToolGlob
	if session.recentSteps[0].Name != protocol.ToolBash {
		t.Fatal("custom detector mutated session evidence")
	}
}
func TestStuckEventsAreMaskedAndDetached(t *testing.T) {
	registry := secrets.New(secrets.Config{})
	registry.RegisterValue("TOKEN", "event-credential")
	name := protocol.ToolName("event-credential")
	events := &sessionEvents{secrets: registry}
	signal := StuckSignal{Pattern: "event-credential", Detail: "event-credential", Tool: &name, Advice: "event-credential"}
	events.append(SessionEvent{kind: EventStuck, stuck: StuckEvent{signal, true, 1}})
	name = "changed"
	record := events.snapshot()[0]
	event, _ := record.Event.Stuck()
	value := event.Signal()
	if string(value.Pattern) != secrets.Mask || value.Detail != secrets.Mask || value.Advice != secrets.Mask || value.Tool == nil || string(*value.Tool) != secrets.Mask {
		t.Fatal("stuck event leaked credential")
	}
	*value.Tool = "mutated"
	again, _ := events.snapshot()[0].Event.Stuck()
	if string(*again.Signal().Tool) != secrets.Mask {
		t.Fatal("event snapshot aliases tool field")
	}
}
func TestActualProviderRequestsCarryCacheButHistoryDoesNot(t *testing.T) {
	provider := &loopingProvider{}
	session := repeatedSession(t, provider, GateHooks{}, 12)
	if _, err := session.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	for i, request := range provider.requests {
		if request.Cache.System == nil {
			t.Fatal("default cache not applied to provider")
		}
		if i > 0 && len(request.Cache.Messages) == 0 {
			t.Fatal("tool result cache breakpoint absent")
		}
		if len(request.Cache.Messages)+boolCount(request.Cache.System != nil) > 4 {
			t.Fatal("default exceeded budget")
		}
	}
	history, _ := json.Marshal(session.Messages())
	if strings.Contains(string(history), "cache_control") {
		t.Fatal("cache annotation entered live history")
	}
	session.cachePolicy = NullCachePolicy{}
	provider.requests = nil
	if _, err := session.Run(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	for _, request := range provider.requests {
		if request.Cache.System != nil || len(request.Cache.Messages) != 0 {
			t.Fatal("Null cache policy did not reach provider")
		}
	}
}
func TestDefaultCacheAndDetectorZeroValuesAndInvalidConfigs(t *testing.T) {
	for _, config := range []CacheConfig{{MaxBreakpoints: 0, Stride: 15}, {MaxBreakpoints: 4, Stride: 0}, {MaxBreakpoints: 4, Stride: 21}} {
		if _, err := NewCachePolicy(config); err == nil {
			t.Fatal("invalid cache config accepted")
		}
	}
	request := protocol.ModelRequest{Model: DefaultModel, MaxTokens: 8000, Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("go")}}, Purpose: protocol.PurposeAgentTurn}
	defaults, _ := NewDefaultCachePolicy().Annotate(request)
	zero, _ := (DefaultCachePolicy{}).Annotate(request)
	if !reflect.DeepEqual(defaults, zero) {
		t.Fatal("zero cache policy differs from defaults")
	}
	signal, err := (DefaultStuckDetector{}).Inspect(StuckState{RoundsWithoutTools: 3})
	if err != nil || signal == nil || signal.Pattern != StuckMonologue || (DefaultStuckDetector{}).MaxNudges() != 1 {
		t.Fatal("zero detector differs from defaults")
	}
}

func TestCacheAndStuckPoliciesAndStopHooksReachFreshChildren(t *testing.T) {
	provider := &loopingProvider{monologue: true}
	session, err := NewRuntimeSession(RuntimeConfig{ID: "parent", Owner: "owner", Provider: provider, Bash: echoExecutor{}, Workspace: t.TempDir(), Mode: ModeAuto, MaxRounds: 1, SubagentMaxRounds: 12, CachePolicy: NullCachePolicy{}, StuckDetector: NullStuckDetector{}, StopHooks: []StopHook{resumeStopHook{}}})
	if err != nil {
		t.Fatal(err)
	}
	output, err := session.Delegate(context.Background(), "child", RoleGeneralPurpose)
	if err != nil {
		t.Fatal(err)
	}
	if provider.calls != 12 || !strings.HasPrefix(output, "[stopped after 12 rounds") {
		t.Fatal("child did not inherit stop/detector policy")
	}
	for _, request := range provider.requests {
		if request.Cache.System != nil || len(request.Cache.Messages) != 0 {
			t.Fatal("child did not inherit null cache policy")
		}
	}
	for _, record := range session.Events() {
		if _, ok := record.Event.Stuck(); ok {
			t.Fatal("null detector produced child stuck events")
		}
	}
	if len(session.recentSteps) != 0 || session.stuckNudges != 0 {
		t.Fatal("child evidence entered parent detector state")
	}
}
func TestFakeProviderRejectsMoreThanFourCacheBreakpoints(t *testing.T) {
	system := "stable"
	blocks := make([]protocol.Block, 9)
	for i := range blocks {
		blocks[i] = protocol.NewTextBlock("text")
	}
	messages := []protocol.Message{{Role: protocol.RoleUser, Content: protocol.BlockContent(blocks...)}}
	request := protocol.ModelRequest{Model: DefaultModel, MaxTokens: 100, System: &system, Messages: messages, Purpose: protocol.PurposeAgentTurn}
	policy, err := NewCachePolicy(CacheConfig{MaxBreakpoints: 6, Stride: 1})
	if err != nil {
		t.Fatal(err)
	}
	request, err = policy.Annotate(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Cache.Messages)+boolCount(request.Cache.System != nil) <= CacheMaxBreakpoints {
		t.Fatal("test did not exceed the supported breakpoint count")
	}
	if _, err := (&FakeProvider{}).Complete(context.Background(), request); err == nil {
		t.Fatal("fake silently accepted unsupported breakpoint count")
	}
}

type countingCachePolicy struct{ purposes []protocol.RequestPurpose }

func (policy *countingCachePolicy) Annotate(request protocol.ModelRequest) (protocol.ModelRequest, error) {
	policy.purposes = append(policy.purposes, request.Purpose)
	return NewDefaultCachePolicy().Annotate(request)
}
func TestCachePolicyAppliesToSummaryRequest(t *testing.T) {
	files, err := workspace.NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	policy := &countingCachePolicy{}
	value := CompactionContext{Messages: []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("prompt")}}, Files: files, Provider: &FakeProvider{}, Model: DefaultModel, CachePolicy: policy}
	if _, err := NewDefaultCompactor().Compact(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(policy.purposes, []protocol.RequestPurpose{protocol.PurposeCompaction}) {
		t.Fatal("summary bypassed cache policy")
	}
}
