package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
	secretpkg "github.com/luoyjx/mini-loop/go/secrets"
)

type lifecycleSink struct {
	mu      sync.Mutex
	records []SessionEventRecord
	fault   bool
	session *ManagedSession
}

func (sink *lifecycleSink) OnEvent(_ context.Context, record SessionEventRecord) error {
	if sink.session != nil {
		sink.session.Info()
		sink.session.Events()
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.records = append(sink.records, record)
	if sink.fault {
		if record.Sequence%2 == 0 {
			panic(runtimeCanary)
		}
		return errors.New(runtimeCanary)
	}
	return nil
}
func TestSubscriptionBoundsReplayAndEphemeralMatchPython(t *testing.T) {
	fixture := readLifecycleContracts(t).Bus
	bus := &sessionEvents{sessionID: "bus", epoch: 1}
	live := bus.subscribe(false)
	for i := 0; i < 2305; i++ {
		bus.append(SessionEvent{kind: EventStatus, status: StatusEvent{Status: StatusIdle}})
	}
	for i := 0; i < 5; i++ {
		bus.append(SessionEvent{kind: EventAssistantDelta, delta: AssistantDeltaEvent{Text: "piece"}})
	}
	live.Close()
	records := []SessionEventRecord{}
	ephemeral := 0
	for record := range live.Events() {
		records = append(records, record)
		if delta, ok := record.Event.AssistantDelta(); ok {
			ephemeral++
			if delta.Text != "piece" {
				t.Fatal(delta)
			}
		}
	}
	replay := bus.subscribe(true)
	replay.Close()
	backlog := []SessionEventRecord{}
	for record := range replay.Events() {
		backlog = append(backlog, record)
		if record.Event.Ephemeral() {
			t.Fatal("ephemeral replay")
		}
	}
	if len(records) != fixture.LiveCount || int(records[0].Sequence) != fixture.LiveFirst || int(records[len(records)-1].Sequence) != fixture.LiveLast || len(backlog) != fixture.ReplayCount || int(backlog[0].Sequence) != fixture.ReplayFirst || int(backlog[len(backlog)-1].Sequence) != fixture.ReplayLast || ephemeral != fixture.EphemeralLive || EventBacklog != fixture.Backlog || SubscriberQueueMax != fixture.Queue {
		t.Fatal("bus contract drift")
	}
	if bus.subscriberCount() != 0 {
		t.Fatal("subscription leak")
	}
	live.Close()
}
func TestConcurrentPublicationDetachedReplayAndSinkFaults(t *testing.T) {
	sink := &lifecycleSink{fault: true}
	bus := &sessionEvents{secrets: runtimeSecrets(), sessionID: SessionID(runtimeCanary), epoch: 1, sink: sink}
	live := bus.subscribe(false)
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := 0; i < 40; i++ {
				bus.append(SessionEvent{kind: EventCancelled, cancelled: CancelledEvent{runtimeCanary, []string{runtimeCanary}}})
			}
		}()
	}
	workers.Wait()
	live.Close()
	next := EventSequence(1)
	for record := range live.Events() {
		if record.Sequence != next || record.SessionID != SessionID(secretpkg.Mask) || record.Timestamp <= 0 {
			t.Fatal("framing/order", record)
		}
		next++
		v, _ := record.Event.Cancelled()
		if v.Reason != secretpkg.Mask || v.RepairedToolUses[0] != secretpkg.Mask {
			t.Fatal("masking")
		}
		v.RepairedToolUses[0] = "changed"
	}
	if next != 321 || strings.Contains(bus.sinkProblem(), runtimeCanary) || bus.sinkProblem() == "" {
		t.Fatal(next, bus.sinkProblem())
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for i, record := range sink.records {
		if int(record.Sequence) != i+1 {
			t.Fatal("sink unordered")
		}
	}
	replay := bus.subscribe(true)
	replay.Close()
	for record := range replay.Events() {
		v, _ := record.Event.Cancelled()
		v.RepairedToolUses[0] = "mutated"
	}
	for _, record := range bus.snapshot() {
		v, _ := record.Event.Cancelled()
		if v.RepairedToolUses[0] != secretpkg.Mask {
			t.Fatal("backlog aliased")
		}
	}
}
func TestTranscriptEpochTracksReplacementNotAppendOrEphemeral(t *testing.T) {
	history := []protocol.Message{{Role: protocol.RoleUser, Content: protocol.PlainContent("first")}}
	bus := &sessionEvents{epoch: 1, history: func() []protocol.Message { return append([]protocol.Message(nil), history...) }}
	emit := func() { bus.append(SessionEvent{kind: EventStatus, status: StatusEvent{Status: StatusIdle}}) }
	emit()
	history = append(history, protocol.Message{Role: protocol.RoleAssistant, Content: protocol.BlockContent(protocol.NewTextBlock("reply"))})
	emit()
	history[0].Content = protocol.PlainContent("first") // same text, new immutable storage
	bus.append(SessionEvent{kind: EventAssistantDelta, delta: AssistantDeltaEvent{Text: "piece"}})
	emit()
	history = history[:1]
	emit()
	records := bus.snapshot()
	expected := []int{1, 1, 2, 3}
	for i, record := range records {
		if record.TranscriptEpoch != expected[i] {
			t.Fatal(record)
		}
	}
}

type parkedProvider struct {
	started chan struct{}
	mu      sync.Mutex
	calls   int
}

func (p *parkedProvider) Complete(ctx context.Context, r protocol.ModelRequest) (protocol.ModelReply, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if call == 1 {
		close(p.started)
		<-ctx.Done()
		return protocol.ModelReply{}, ctx.Err()
	}
	reply := fakeReply([]protocol.Block{protocol.NewTextBlock("next")}, protocol.StopEndTurn)
	return reply, nil
}
func managedForTest(t *testing.T, p Provider) *ManagedSession {
	t.Helper()
	config := runtimeConfig(t.TempDir(), p)
	config.Mode = ModeAuto
	session, err := NewManagedSession(config)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

// Done is evaluated only after Run's first admission check. The probe makes
// queue placement deterministic without sleeps or exposing runtime test hooks.
type admissionProbeContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (ctx *admissionProbeContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.waiting) })
	return ctx.Context.Done()
}

func TestManagedCancelTargetsActiveTurnAndQueueRemainsUsable(t *testing.T) {
	p := &parkedProvider{started: make(chan struct{})}
	session := managedForTest(t, p)
	first := make(chan error, 1)
	go func() { _, err := session.Run(context.Background(), "first"); first <- err }()
	<-p.started
	queuedCtx, cancelQueue := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	go func() { _, err := session.Run(queuedCtx, "abandoned"); queued <- err }()
	cancelQueue()
	if err := <-queued; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	second := make(chan error, 1)
	probe := &admissionProbeContext{Context: context.Background(), waiting: make(chan struct{})}
	go func() {
		output, err := session.Run(probe, "second")
		if err == nil && output != "next" {
			err = errors.New("wrong second output")
		}
		second <- err
	}()
	<-probe.waiting
	if ok, err := session.Cancel(context.Background(), "operator"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	info := session.Info()
	if info.RunCount != 2 || info.Status != StatusIdle || info.Busy {
		t.Fatal(info)
	}
	if len(eventRecordsOfKind(session.Events(), EventCancelled)) != 1 {
		t.Fatal("wrong cancellation owner")
	}
	if err := protocol.ValidateTranscript(session.Messages()); err != nil {
		t.Fatal(err)
	}
}
func TestManagedStopAdmissionRechecksQueuedCallers(t *testing.T) {
	p := &parkedProvider{started: make(chan struct{})}
	session := managedForTest(t, p)
	first := make(chan error, 1)
	go func() { _, err := session.Run(context.Background(), "first"); first <- err }()
	<-p.started
	second := make(chan error, 1)
	probe := &admissionProbeContext{Context: context.Background(), waiting: make(chan struct{})}
	go func() { _, err := session.Run(probe, "queued"); second <- err }()
	<-probe.waiting
	session.StopAccepting("closing")
	if ok, err := session.Cancel(context.Background(), "stop"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := <-second; err == nil || !strings.Contains(err.Error(), "closing") {
		t.Fatal(err)
	}
	if _, err := session.Run(context.Background(), "late"); err == nil {
		t.Fatal("admitted after close")
	}
	if session.Info().RunCount != 1 {
		t.Fatal(session.Info())
	}
}
func TestManagedInfoApprovalAndCancellationDoNotBlockOnLoopLock(t *testing.T) {
	broker, err := NewApprovalBroker(ApprovalBrokerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	config := runtimeConfig(t.TempDir(), &approvalModel{})
	config.Approvals = broker
	session, err := NewManagedSession(config)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := session.Run(context.Background(), "write"); done <- err }()
	waitBrokerPending(t, broker, session.ID())
	infoDone := make(chan SessionInfo, 1)
	go func() { infoDone <- session.Info() }()
	select {
	case info := <-infoDone:
		if info.Activity != ActivityAwaitingApproval || !info.Busy {
			t.Fatal(info)
		}
	case <-time.After(time.Second):
		t.Fatal("Info waited on run lock")
	}
	if ok, err := session.Cancel(context.Background(), "stop approval"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(broker.List(session.ID())) != 0 {
		t.Fatal("pending approval survived cancel")
	}
}

type panicSecondBash struct{ calls int }

func (b *panicSecondBash) ExecuteBash(context.Context, protocol.BashInput) (string, error) {
	b.calls++
	if b.calls == 2 {
		panic(runtimeCanary)
	}
	return "completed first", nil
}
func TestManagedPanicRepairsOnlyUnfinishedCallsAndReleasesAdmission(t *testing.T) {
	provider := resourceProvider{tools: []protocol.Block{protocol.NewBashUse("a", "echo a"), protocol.NewBashUse("b", "echo b"), protocol.NewBashUse("c", "echo c")}}
	config := runtimeConfig(t.TempDir(), provider)
	config.Mode = ModeAuto
	config.Bash = &panicSecondBash{}
	config.Secrets = runtimeSecrets()
	session, err := NewManagedSession(config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = session.Run(context.Background(), "go"); err == nil || strings.Contains(err.Error(), runtimeCanary) {
		t.Fatal(err)
	}
	if info := session.Info(); info.Status != StatusError || info.Busy {
		t.Fatal(info)
	}
	messages := session.Messages()
	if err := protocol.ValidateTranscript(messages); err != nil {
		t.Fatal(err)
	}
	blocks, _ := messages[len(messages)-1].Content.Blocks()
	first, _ := blocks[0].ToolResult()
	second, _ := blocks[1].ToolResult()
	third, _ := blocks[2].ToolResult()
	if first.Content != "completed first" || second.Content != unknownToolResult || third.Content != unknownToolResult {
		t.Fatal(first, second, third)
	}
	if _, err := session.Run(context.Background(), "again"); err != nil {
		t.Fatal("admission not released", err)
	}
}
func TestManagedSinkCanInspectInfoAndFaultDoesNotAbortTurn(t *testing.T) {
	sink := &lifecycleSink{fault: true}
	config := runtimeConfig(t.TempDir(), &lifecycleProvider{name: "refusal"})
	config.EventSink = sink
	config.Secrets = runtimeSecrets()
	session, err := NewManagedSession(config)
	if err != nil {
		t.Fatal(err)
	}
	sink.session = session
	done := make(chan error, 1)
	go func() { _, err := session.Run(context.Background(), "go"); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("sink inspect deadlock")
	}
	if session.Info().SinkError == nil {
		t.Fatal("sink fault hidden")
	}
}

func TestManagedLifecycleMaskingKeepsRawExecutionAndHistory(t *testing.T) {
	executor, provider, sink := &secretBash{}, &secretModel{}, &lifecycleSink{}
	config := runtimeConfig(t.TempDir(), provider)
	config.Mode = ModeAuto
	config.Secrets = runtimeSecrets()
	config.Bash = executor
	config.EventSink = sink
	config.SystemBuilder = FixedSystem("stable " + runtimeCanary)
	session, err := NewManagedSession(config)
	if err != nil {
		t.Fatal(err)
	}
	subscription := session.Subscribe(false)
	if _, err = session.Run(context.Background(), "work "+runtimeCanary); err != nil {
		t.Fatal(err)
	}
	subscription.Close()
	if !strings.Contains(executor.command, runtimeCanary) || !provider.sawRawCall || !provider.sawMaskedResult {
		t.Fatal("raw/recorded boundary changed")
	}
	inspect := func(record SessionEventRecord) {
		t.Helper()
		_, err := protocol.MaskedPythonJSON(projectLifecycle(record), func(value string) string {
			if strings.Contains(value, runtimeCanary) {
				t.Errorf("record leaked %q", value)
			}
			return value
		}, false, false)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, record := range session.Events() {
		inspect(record)
	}
	for record := range subscription.Events() {
		inspect(record)
	}
	for _, record := range sink.records {
		inspect(record)
	}
	prompt, _ := session.Messages()[0].Content.Plain()
	if !strings.Contains(prompt, runtimeCanary) {
		t.Fatal("mask rewrote live user prompt")
	}
}

func TestReconciliationCallbackPrecedesUnknownRetry(t *testing.T) {
	ctx := context.Background()
	journal, _ := NewStoredActionJournal(newTestActionStore())
	handler := &gateHandler{}
	definition, _ := NewToolDefinition(protocol.ToolBash, ToolTraits{Risk: RiskExec}, handler)
	verified := false
	definition = definition.WithVerifier(verifierFunc(func(context.Context, ToolAuthority, ToolCall) (EffectVerdict, error) {
		verified = true
		return EffectNotApplied, nil
	}))
	catalog, _ := NewToolCatalog(definition)
	gate, _ := NewJournaledToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{}, journal)
	authority := gateTestAuthority(ModeAuto)
	authority.RunContext = actionContext(t)
	call := gateTestCall("echo x")
	id, _ := ToolActionID("s", authority.RunContext, call)
	journal.Begin(ctx, ActionRequest{id, "s", "m", call.ID, call.Input})
	journal.Finish(ctx, ActionSettlement{ActionID: id, Status: ActionUnknown})
	notified := false
	outcome, err := gate.dispatch(ctx, authority, call, func(event ActionReconciliation) {
		if !verified || handler.calls != 0 || event.ActionID != id || event.Verdict != EffectNotApplied || !event.Verifiable {
			t.Fatal("reconcile boundary", event, handler.calls)
		}
		notified = true
	})
	if err != nil || !notified || handler.calls != 1 || outcome.Replayed {
		t.Fatal(outcome, err, notified, handler.calls)
	}
}

func TestCoreQueuedTurnReportsAndCancelsBeforeMutation(t *testing.T) {
	p := &parkedProvider{started: make(chan struct{})}
	config := runtimeConfig(t.TempDir(), p)
	session, err := NewRuntimeSession(config)
	if err != nil {
		t.Fatal(err)
	}
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := session.Run(firstCtx, "first"); first <- err }()
	<-p.started
	queuedCtx, cancelQueued := context.WithCancel(context.Background())
	probe := &admissionProbeContext{Context: queuedCtx, waiting: make(chan struct{})}
	queued := make(chan error, 1)
	go func() { _, err := session.Run(probe, "queued"); queued <- err }()
	<-probe.waiting
	cancelQueued()
	if err := <-queued; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	records := eventRecordsOfKind(session.Events(), EventTurnQueued)
	if len(records) != 1 || records[0].Scope.RunContext.MessageID() == "" {
		t.Fatal("missing queue notification/provenance", records)
	}
	cancelFirst()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(session.Messages()) != 1 {
		t.Fatal("queued cancellation mutated transcript")
	}
	if output, err := session.Run(context.Background(), "next"); err != nil || output != "next" {
		t.Fatal(output, err)
	}
}

type terminalSink struct{ started, release chan struct{} }

func (sink terminalSink) OnEvent(_ context.Context, record SessionEventRecord) error {
	if record.Event.Kind() == EventDone {
		close(sink.started)
		<-sink.release
	}
	return nil
}
func TestManagedTerminalCommitRejectsLateCancel(t *testing.T) {
	sink := terminalSink{make(chan struct{}), make(chan struct{})}
	config := runtimeConfig(t.TempDir(), &lifecycleProvider{name: "refusal"})
	config.EventSink = sink
	session, err := NewManagedSession(config)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := session.Run(context.Background(), "go"); done <- err }()
	<-sink.started
	if cancelled, err := session.Cancel(context.Background(), "late"); cancelled || err != nil {
		t.Fatal(cancelled, err)
	}
	close(sink.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(eventRecordsOfKind(session.Events(), EventCancelled)) != 0 {
		t.Fatal("completed turn was cancelled")
	}
}
