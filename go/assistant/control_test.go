package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// gatedModel holds every background turn until gate closes; the user's chat
// passes straight through.
type gatedModel struct {
	inner agent.Provider
	gate  chan struct{}
}

func (m *gatedModel) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if turn, ok := readTurn(request.Messages); ok && turn.thread != ChatThread {
		select {
		case <-m.gate:
		case <-ctx.Done():
			return protocol.ModelReply{}, ctx.Err()
		}
	}
	return m.inner.Complete(ctx, request)
}

// modelFailure is a provider error with the classification the agent's
// recovery and the router read.
type modelFailure struct{ failure protocol.ModelFailure }

func (e *modelFailure) Error() string                          { return e.failure.Message }
func (e *modelFailure) RecoveryFailure() protocol.ModelFailure { return e.failure }

type failingModel struct{ err error }

func (m failingModel) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	return protocol.ModelReply{}, m.err
}

func triggerStatus(t *testing.T, app *App, id int64) Trigger {
	t.Helper()
	triggers, err := app.Store.Triggers(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	return triggerByID(t, triggers, id)
}

func attentionRefs(t *testing.T, app *App) map[string]AttentionKind {
	t.Helper()
	items, err := app.Attention(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]AttentionKind{}
	for _, item := range items {
		refs[item.Ref] = item.Kind
	}
	return refs
}

func TestSecondDaemonOnTheSameDataDirIsRefused(t *testing.T) {
	clock := newClock()
	dir := t.TempDir()
	_, stop := startApp(t, dir, clock)
	world, err := OpenWorld(context.Background(), filepath.Join(t.TempDir(), "world.db"), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer world.Close()
	open := func() (*App, error) {
		client, err := NewInProcessMock(world)
		if err != nil {
			t.Fatal(err)
		}
		return NewApp(context.Background(), Config{DataDir: dir, Connector: NewConnector(client, "in-process"), Provider: &ScriptedModel{Now: clock.Now}, Model: ScriptedModelID, Now: clock.Now})
	}
	if _, err := open(); !errors.Is(err, ErrDataDirBusy) {
		t.Fatalf("second daemon: %v", err)
	}
	stop()
	app, err := open()
	if err != nil {
		t.Fatalf("the lock must be released on Close: %v", err)
	}
	if err := app.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Pausing stops dispatch, sending and the clock but keeps recording mail and
// chat; the pause survives a restart, and resuming catches everything up.
func TestPauseHoldsProcessingAcrossARestartAndResumeCatchesUp(t *testing.T) {
	clock := newClock()
	dir := t.TempDir()
	ctx := context.Background()
	app, stop := startApp(t, dir, clock)
	if changed, err := app.SetPaused(ctx, true); err != nil || !changed {
		t.Fatal(changed, err)
	}
	if changed, _ := app.SetPaused(ctx, true); changed {
		t.Fatal("pausing twice must not change anything")
	}
	mail := deliver(t, app, PresetManager)
	chat, err := app.Store.ChatFromUser(ctx, "今天日程", clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := app.Store.CreateTask(ctx, Task{Title: "跟进", Thread: "t-paused", SessionID: "s", TriggerID: mail, Check: "c"}, clock.Now(), "test", clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	draft, _, err := app.Store.CreateProposal(ctx, Proposal{Kind: ProposalSendEmail, TriggerID: mail, Thread: "t-paused", To: "x@acme.example", Subject: "s", Body: "b"}, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Store.DecideProposal(ctx, draft.ID, true, "test", clock.Now()); err != nil {
		t.Fatal(err)
	}
	app.Router.Wake()
	if app.Scheduler.Tick(ctx) != 0 || app.Outbox.Step(ctx) {
		t.Fatal("a paused daemon must not claim checks or send")
	}
	time.Sleep(150 * time.Millisecond)
	if status := triggerStatus(t, app, mail).Status; status != TriggerPending {
		t.Fatalf("paused mail trigger is %s", status)
	}
	stop()

	app, _ = startApp(t, dir, clock)
	if _, paused := app.Store.Paused(); !paused {
		t.Fatal("the pause must survive a restart")
	}
	app.Router.Wake()
	time.Sleep(150 * time.Millisecond)
	if status := triggerStatus(t, app, chat).Status; status != TriggerPending {
		t.Fatalf("paused chat trigger is %s after restart", status)
	}
	if changed, err := app.SetPaused(ctx, false); err != nil || !changed {
		t.Fatal(changed, err)
	}
	waitProposal(t, app, draft.ID, ProposalSent)
	waitFor(t, "the due task check", func() bool {
		current, _, _ := app.Store.Task(ctx, task.ID)
		return current.Checks == 1
	})
	triggers := settle(t, app.Store)
	if triggerByID(t, triggers, mail).Status != TriggerDone || triggerByID(t, triggers, chat).Status != TriggerDone {
		t.Fatalf("backlog after resume: %+v", triggers)
	}
	if !channelHas(t, app, pauseText) || !channelHas(t, app, resumeText) {
		t.Fatal("pause and resume must be told on the channel")
	}
	if got := len(ledgerKinds(t, app, LedgerControl)); got != 2 {
		t.Fatalf("control ledger entries: %d", got)
	}
}

// With every background slot busy, the user's message still runs at once on
// the reserved slot; the queued mail waits for a background slot.
func TestUserChatRunsOnTheReservedSlotWhileWorkersAreBusy(t *testing.T) {
	clock := newClock()
	model := &gatedModel{inner: &ScriptedModel{Now: clock.Now}, gate: make(chan struct{})}
	app, _ := launchApp(t, t.TempDir(), clock, appOptions{provider: model, workers: 1})
	ctx := context.Background()
	first := deliver(t, app, PresetManager)
	second := deliver(t, app, PresetFriend)
	app.Router.Wake()
	waitFor(t, "one background turn", func() bool {
		running, err := app.Store.CountTriggers(ctx, TriggerRunning)
		return err == nil && running == 1
	})
	chat, err := app.Store.ChatFromUser(ctx, "今天日程", clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	app.Router.Wake()
	waitFor(t, "the chat on the reserved slot", func() bool { return triggerStatus(t, app, chat).Status == TriggerDone })
	if status := triggerStatus(t, app, second).Status; status != TriggerPending {
		t.Fatalf("the reserved slot ran background work: trigger %d is %s", second, status)
	}
	if status := triggerStatus(t, app, first).Status; status != TriggerRunning {
		t.Fatalf("first mail is %s", status)
	}
	close(model.gate)
	settle(t, app.Store)
}

func TestChatIsClaimedFirstAndTheReservedSlotTakesOnlyChat(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, filepath.Join(t.TempDir(), "assistant.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now()
	if _, err := enqueueTrigger(ctx, store.db, Trigger{Kind: TriggerEmail, Thread: "t-a", Ref: "m1", Summary: "mail"}, now); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := store.ClaimTrigger(ctx, now, true); ok {
		t.Fatal("the reserved slot claimed mail")
	}
	chat, err := store.ChatFromUser(ctx, "hi", now)
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimTrigger(ctx, now, false)
	if err != nil || !ok || claimed.ID != chat {
		t.Fatalf("chat must go first, claimed %+v %v", claimed, err)
	}
}

func TestPermanentFailuresAreNotRetried(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"mcp refusal", &ToolError{Tool: "mail_read", Message: "no such mail"}, true},
		{"bad credentials", &modelFailure{protocol.ModelFailure{Kind: protocol.ModelFailureStatus, Status: 401}}, true},
		{"invalid request", &modelFailure{protocol.ModelFailure{Kind: protocol.ModelFailureStatus, Status: 400}}, true},
		{"rate limit", &modelFailure{protocol.ModelFailure{Kind: protocol.ModelFailureStatus, Status: 429}}, false},
		{"request timeout", &modelFailure{protocol.ModelFailure{Kind: protocol.ModelFailureStatus, Status: 408}}, false},
		{"overloaded", &modelFailure{protocol.ModelFailure{Kind: protocol.ModelFailureOverloaded, Status: 529}}, false},
		{"connection", &modelFailure{protocol.ModelFailure{Kind: protocol.ModelFailureConnection}}, false},
		{"storage", errors.New("database is locked"), false},
	}
	for _, c := range cases {
		if got := permanent(errors.Join(errors.New("worker s"), c.err)); got != c.want {
			t.Errorf("%s: permanent=%v", c.name, got)
		}
	}

	clock := newClock()
	failure := &modelFailure{protocol.ModelFailure{Kind: protocol.ModelFailureStatus, Status: 401, Message: "invalid x-api-key (simulated)"}}
	app, _ := launchApp(t, t.TempDir(), clock, appOptions{provider: failingModel{failure}})
	id := deliver(t, app, PresetManager)
	app.Router.Wake()
	triggers := settle(t, app.Store)
	trigger := triggerByID(t, triggers, id)
	if trigger.Status != TriggerFailed || trigger.Attempts != 1 {
		t.Fatalf("a rejected request must fail on its first attempt: %+v", trigger)
	}
	if attentionRefs(t, app)[TriggerRef(id)] != AttentionTriggerFailed {
		t.Fatal("the failed trigger must wait for the user")
	}
}

func TestAttentionListsWhatWaitsForTheUser(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	now := clock.Now()

	mail := deliver(t, app, PresetManager)
	settle(t, app.Store)
	drafts, err := app.Store.ProposalsForTrigger(ctx, mail)
	if err != nil || len(drafts) != 1 {
		t.Fatalf("drafts %+v %v", drafts, err)
	}
	draft := drafts[0]
	// Paused, the outbox and router leave the objects below where the test
	// puts them.
	if _, err := app.SetPaused(ctx, true); err != nil {
		t.Fatal(err)
	}
	if attentionRefs(t, app)[ProposalRef(draft.ID)] != AttentionApproval {
		t.Fatal("a pending draft waits for approval")
	}
	if err := app.Store.Dismiss(ctx, ProposalRef(draft.ID), "me", now); !errors.Is(err, ErrNotActionable) {
		t.Fatalf("a pending draft needs a decision, not a dismissal: %v", err)
	}

	// An approved proposal that finally fails waits until it is retried or
	// dismissed; a retry clears the dismissal so a new failure shows again.
	approved, err := app.Store.DecideProposal(ctx, draft.ID, true, "me", now)
	if err != nil {
		t.Fatal(err)
	}
	fail := func() {
		if err := app.Store.FailProposalAttempt(ctx, approved, &ToolError{Tool: MCPMailSend, Message: "mailbox full"}, true, now, "test", now); err != nil {
			t.Fatal(err)
		}
	}
	fail()
	if attentionRefs(t, app)[ProposalRef(draft.ID)] != AttentionSendFailed {
		t.Fatal("a failed send waits for the user")
	}
	if err := app.Store.Dismiss(ctx, ProposalRef(draft.ID), "me", now); err != nil {
		t.Fatal(err)
	}
	if _, listed := attentionRefs(t, app)[ProposalRef(draft.ID)]; listed {
		t.Fatal("a dismissed failure leaves the list")
	}
	retried, err := app.Store.RetryProposal(ctx, draft.ID, "me", now)
	if err != nil || retried.Status != ProposalApproved || retried.Attempts != 0 {
		t.Fatalf("retry %+v %v", retried, err)
	}
	approved = retried
	fail()
	if attentionRefs(t, app)[ProposalRef(draft.ID)] != AttentionSendFailed {
		t.Fatal("a retried proposal that fails again must show again")
	}

	// A task that ran out of checks waits until the user resumes it.
	task, _, err := app.Store.CreateTask(ctx, Task{Title: "等回复", Thread: "t-wait", SessionID: "s", TriggerID: mail, Check: "c"}, now.Add(time.Hour), "test", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Store.Dismiss(ctx, TaskRef(task.ID), "me", now); !errors.Is(err, ErrNotActionable) {
		t.Fatalf("an active task cannot be dismissed: %v", err)
	}
	if _, err := app.Store.CloseTask(ctx, task.ID, TaskStalled, "自动检查 6 次仍未完成，交给你处理", "scheduler", now); err != nil {
		t.Fatal(err)
	}
	if attentionRefs(t, app)[TaskRef(task.ID)] != AttentionTaskHandoff {
		t.Fatal("a handed-back task waits for the user")
	}
	resumed, err := app.Store.ResumeTask(ctx, task.ID, "me", now)
	if err != nil || resumed.Status != TaskActive || resumed.Checks != 0 || resumed.WakeAt == nil || !resumed.WakeAt.Equal(now) {
		t.Fatalf("resume %+v %v", resumed, err)
	}
	if _, listed := attentionRefs(t, app)[TaskRef(task.ID)]; listed {
		t.Fatal("a resumed task leaves the list")
	}

	// A failed trigger waits for 24 hours.
	failed, err := enqueueTrigger(ctx, app.Store.db, Trigger{Kind: TriggerEmail, Thread: "t-broken", Ref: "m-missing", Summary: "坏掉的邮件"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Store.FinishTrigger(ctx, failed, TriggerFailed, "mail_read: refused", "", now); err != nil {
		t.Fatal(err)
	}
	if attentionRefs(t, app)[TriggerRef(failed)] != AttentionTriggerFailed {
		t.Fatal("a failed trigger waits for the user")
	}
	later, err := app.Store.Attention(ctx, now.Add(25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range later {
		if item.Ref == TriggerRef(failed) {
			t.Fatal("a failed trigger leaves the list after 24 hours")
		}
	}

	// A component is a problem only after three failures in a row.
	for i := 0; i < healthThreshold-1; i++ {
		app.Health.Record(HealthOutbox, errors.New("broken pipe"), now)
	}
	if _, listed := attentionRefs(t, app)["health-outbox"]; listed {
		t.Fatal("two failures are not a problem yet")
	}
	app.Health.Record(HealthOutbox, errors.New("broken pipe"), now)
	if attentionRefs(t, app)["health-outbox"] != AttentionProblem {
		t.Fatal("three failures in a row are a problem")
	}
	app.Health.Record(HealthOutbox, nil, now)
	if _, listed := attentionRefs(t, app)["health-outbox"]; listed {
		t.Fatal("a success clears the problem")
	}

	// The page reads the list and acts on it.
	handler := app.Handler()
	call := func(method, path, payload string) (int, string) {
		request := httptest.NewRequest(method, path, strings.NewReader(payload))
		request.Host = "127.0.0.1:8790"
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder.Code, recorder.Body.String()
	}
	if code, body := call(http.MethodPost, "/api/triggers/"+strings.TrimPrefix(TriggerRef(failed), "trigger-")+"/retry", `{}`); code != http.StatusOK || !strings.Contains(body, "用户要求重新处理") {
		t.Fatalf("retry trigger: %d %s", code, body)
	}
	if code, _ := call(http.MethodPost, "/api/attention/"+TriggerRef(failed)+"/dismiss", `{}`); code != http.StatusConflict {
		t.Fatalf("dismiss a queued trigger: %d", code)
	}
	if code, _ := call(http.MethodPost, "/api/attention/nothing-here/dismiss", `{}`); code != http.StatusNotFound {
		t.Fatalf("dismiss unknown ref: %d", code)
	}
	if code, _ := call(http.MethodPost, "/api/proposals/"+strings.TrimPrefix(ProposalRef(draft.ID), "proposal-")+"/retry", `{}`); code != http.StatusOK {
		t.Fatalf("retry proposal: %d", code)
	}
	if code, _ := call(http.MethodPost, "/api/pause", `{"paused":true}`); code != http.StatusOK {
		t.Fatalf("pause: %d", code)
	}
	code, body := call(http.MethodGet, "/api/state", "")
	var view StateView
	if code != http.StatusOK || json.Unmarshal([]byte(body), &view) != nil {
		t.Fatalf("state: %d", code)
	}
	if view.Paused == nil || view.Workers.Reserved != reservedSlots {
		t.Fatalf("state view: paused=%v workers=%+v", view.Paused, view.Workers)
	}
	if code, _ := call(http.MethodPost, "/api/pause", `{"paused":false}`); code != http.StatusOK {
		t.Fatalf("resume: %d", code)
	}
}

// loopingModel keeps calling calendar_list with a different range each round
// and never answers, so the turn runs out of rounds.
type loopingModel struct {
	mu    sync.Mutex
	calls int
}

func (m *loopingModel) Complete(_ context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	m.mu.Lock()
	m.calls++
	n := m.calls
	m.mu.Unlock()
	input, err := protocol.DecodeModelToolInput(ToolCalendarList, []byte(fmt.Sprintf(`{"days":%d}`, n%7+1)))
	if err != nil {
		return protocol.ModelReply{}, err
	}
	reply := protocol.ModelReply{ID: fmt.Sprintf("msg_loop_%d", n), Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: ScriptedModelID}
	if request.Purpose != "" && request.Purpose != protocol.PurposeAgentTurn {
		reply.Content, reply.StopReason = []protocol.Block{protocol.NewTextBlock("summary")}, protocol.StopEndTurn
		return reply, nil
	}
	reply.Content, reply.StopReason = []protocol.Block{protocol.NewToolUse(fmt.Sprintf("toolu_loop_%d", n), input)}, protocol.StopToolUse
	return reply, nil
}

// The session reports a failed model call as "[Error] …" final text. The
// router treats it as a failed turn: an outage is retried later, and nothing
// reaches the channel as if the worker had answered.
func TestModelOutageIsRetriedInsteadOfPostedAsAnAnswer(t *testing.T) {
	clock := newClock()
	failure := &modelFailure{protocol.ModelFailure{Kind: protocol.ModelFailureStatus, Status: 500, Message: "internal server error (simulated)"}}
	app, _ := launchApp(t, t.TempDir(), clock, appOptions{provider: failingModel{failure}})
	id := deliver(t, app, PresetManager)
	app.Router.Wake()
	waitFor(t, "the retry to be scheduled", func() bool {
		trigger := triggerStatus(t, app, id)
		return trigger.Status == TriggerPending && trigger.Attempts == 1 && strings.Contains(trigger.Decision, "重试")
	})
	if channelHas(t, app, "[Error]") {
		t.Fatal("a model failure reached the channel as an answer")
	}
}

func TestTurnThatRunsOutOfRoundsFailsWithoutRetry(t *testing.T) {
	clock := newClock()
	app, _ := launchApp(t, t.TempDir(), clock, appOptions{provider: &loopingModel{}})
	id := deliver(t, app, PresetManager)
	app.Router.Wake()
	trigger := triggerByID(t, settle(t, app.Store), id)
	if trigger.Status != TriggerFailed || trigger.Attempts != 1 || !strings.Contains(trigger.Decision, "max_rounds") {
		t.Fatalf("a turn out of rounds must fail once: %+v", trigger)
	}
	if channelHas(t, app, "Partial output") {
		t.Fatal("the stop text reached the channel as an answer")
	}
}
