package assistant

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fixedClock is a settable clock shared by the app and the scripted model.
type fixedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fixedClock) Set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

func newClock() *fixedClock {
	return &fixedClock{now: time.Date(2026, 11, 2, 6, 30, 0, 0, time.Local)}
}

func startApp(t *testing.T, dir string, clock *fixedClock) (*App, func()) {
	t.Helper()
	app, err := NewApp(context.Background(), Config{DataDir: dir, Provider: &ScriptedModel{Now: clock.Now}, Model: ScriptedModelID, Now: clock.Now, Workers: 2, SchedulerTick: time.Hour, LeaseTTL: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	app.Start(ctx)
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if err := app.Close(shutdown); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(stop)
	return app, stop
}

// settle waits until no trigger is pending or running.
func settle(t *testing.T, store *Store) []Trigger {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		triggers, err := store.Triggers(context.Background(), 100)
		if err != nil {
			t.Fatal(err)
		}
		busy := false
		for _, trigger := range triggers {
			busy = busy || trigger.Status == TriggerPending || trigger.Status == TriggerRunning
		}
		if !busy {
			return triggers
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("triggers did not settle")
	return nil
}

func triggerByID(t *testing.T, triggers []Trigger, id int64) Trigger {
	t.Helper()
	for _, trigger := range triggers {
		if trigger.ID == id {
			return trigger
		}
	}
	t.Fatalf("trigger %d missing", id)
	return Trigger{}
}

func deliver(t *testing.T, app *App, preset Preset, now time.Time) int64 {
	t.Helper()
	id, err := app.Store.Deliver(context.Background(), preset, now)
	if err != nil {
		t.Fatal(err)
	}
	app.Router.Wake()
	return id
}

func TestRulesKeepBulkMailAwayFromTheModel(t *testing.T) {
	rules := DefaultRules()
	cases := []struct {
		name    string
		trigger Trigger
		mail    *Email
		want    Verdict
	}{
		{"newsletter", Trigger{Kind: TriggerEmail}, &Email{From: "a@b.example", Category: CategoryNewsletter}, VerdictIgnore},
		{"noreply sender", Trigger{Kind: TriggerEmail}, &Email{From: "NoReply@shop.example", Category: CategoryPerson}, VerdictIgnore},
		{"person", Trigger{Kind: TriggerEmail}, &Email{From: "bob@friends.example", Category: CategoryPerson}, VerdictCandidate},
		{"missing mail", Trigger{Kind: TriggerEmail}, nil, VerdictIgnore},
		{"invite", Trigger{Kind: TriggerInvite}, nil, VerdictCandidate},
		{"chat", Trigger{Kind: TriggerChat}, nil, VerdictCandidate},
	}
	for _, c := range cases {
		if got := rules.Classify(c.trigger, c.mail).Verdict; got != c.want {
			t.Errorf("%s: verdict %s, want %s", c.name, got, c.want)
		}
	}
}

func TestProactiveFlowDraftsButNeverSendsWithoutApproval(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	now := clock.Now()
	managerID := deliver(t, app, PresetManager, now)
	inviteID := deliver(t, app, PresetInviteConflict, now.Add(time.Millisecond))
	newsletterID := deliver(t, app, PresetNewsletter, now.Add(2*time.Millisecond))
	phishingID := deliver(t, app, PresetPhishing, now.Add(3*time.Millisecond))
	triggers := settle(t, app.Store)

	if got := triggerByID(t, triggers, newsletterID).Status; got != TriggerIgnored {
		t.Fatalf("newsletter status %s, want ignored by the fast path", got)
	}
	if got := triggerByID(t, triggers, phishingID); got.Status != TriggerDone || got.SessionID == "" {
		t.Fatalf("phishing trigger %+v", got)
	}
	sent, err := app.Store.ListMail(ctx, FolderSent, 10)
	if err != nil || len(sent) != 0 {
		t.Fatalf("nothing may be sent before approval: %v %v", sent, err)
	}
	reply, err := app.Store.ProposalsForTrigger(ctx, managerID)
	if err != nil || len(reply) != 1 || reply[0].Kind != ProposalSendEmail || reply[0].To != ManagerAddress || reply[0].Status != ProposalPending || reply[0].ActionID == "" {
		t.Fatalf("manager reply proposal %+v %v", reply, err)
	}
	invite, err := app.Store.ProposalsForTrigger(ctx, inviteID)
	if err != nil || len(invite) != 1 || invite[0].Response != InviteDecline || !strings.Contains(invite[0].Body, "1:1 with Alice") {
		t.Fatalf("conflicting invite should be declined with a reason: %+v %v", invite, err)
	}
	phishing, err := app.Store.ProposalsForTrigger(ctx, phishingID)
	if err != nil || len(phishing) != 0 {
		t.Fatalf("phishing mail must not produce a proposal: %+v %v", phishing, err)
	}
	channel, err := app.Store.Channel(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	cards, warned := 0, false
	for _, message := range channel {
		if message.ProposalID != nil {
			cards++
		}
		warned = warned || strings.Contains(message.Text, "可疑邮件")
	}
	if cards != 2 || !warned {
		t.Fatalf("channel should carry two approval cards and a phishing warning: cards=%d warned=%v", cards, warned)
	}

	decided, err := app.Store.DecideProposal(ctx, reply[0].ID, true, "test", clock.Now())
	if err != nil || decided.Status != ProposalSent {
		t.Fatalf("approve: %+v %v", decided, err)
	}
	if _, err := app.Store.DecideProposal(ctx, reply[0].ID, true, "test", clock.Now()); !errors.Is(err, ErrProposalDecided) {
		t.Fatalf("second approval must be refused, got %v", err)
	}
	if _, err := app.Store.DecideProposal(ctx, invite[0].ID, false, "test", clock.Now()); err != nil {
		t.Fatal(err)
	}
	sent, _ = app.Store.ListMail(ctx, FolderSent, 10)
	if len(sent) != 1 || sent[0].To != ManagerAddress {
		t.Fatalf("exactly the approved mail is sent: %+v", sent)
	}
	event, _, _ := app.Store.Event(ctx, invite[0].EventID)
	if event.Status != EventInvited {
		t.Fatalf("a denied decline leaves the invite untouched, got %s", event.Status)
	}
	ledger, _ := app.Store.Ledger(ctx, 500)
	var effect, decision bool
	for _, entry := range ledger {
		if entry.Kind == LedgerEffect && entry.ActionID == reply[0].ActionID {
			effect = true
		}
		if entry.Kind == LedgerDecision && entry.Actor == "test" && entry.TriggerID == managerID {
			decision = true
		}
	}
	if !effect || !decision {
		t.Fatal("ledger must tie the effect to the gate action and the approving actor")
	}
}

func TestQuietWorkersStayOffTheChannel(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	before, _ := app.Store.Channel(context.Background(), 100)
	id := deliver(t, app, PresetFriend, clock.Now())
	if got := triggerByID(t, settle(t, app.Store), id).Status; got != TriggerQuiet {
		t.Fatalf("friend chatter status %s, want quiet", got)
	}
	after, _ := app.Store.Channel(context.Background(), 100)
	if len(after) != len(before) {
		t.Fatal("a quiet worker must not post to the channel")
	}
}

func TestRoutineSlotFiresOnceAndCatchesUpAfterDowntime(t *testing.T) {
	clock := newClock()
	dir := t.TempDir()
	app, stop := startApp(t, dir, clock)
	ctx := context.Background()
	if fired := app.Scheduler.Due(ctx, false); fired != 0 {
		t.Fatalf("06:30 is before the 07:00 brief, fired %d", fired)
	}
	stop()
	// The daemon was down at 07:00; the next start catches up once.
	clock.Set(time.Date(2026, 11, 2, 9, 15, 0, 0, time.Local))
	app, _ = startApp(t, dir, clock)
	if fired := app.Scheduler.Due(ctx, true); fired != 1 {
		t.Fatalf("missed slot should catch up once, fired %d", fired)
	}
	if fired := app.Scheduler.Due(ctx, false); fired != 0 {
		t.Fatalf("a claimed slot must not fire again, fired %d", fired)
	}
	triggers := settle(t, app.Store)
	if len(triggers) != 1 || triggers[0].Kind != TriggerRoutine || !strings.Contains(triggers[0].Summary, "补跑") || triggers[0].Status != TriggerDone {
		t.Fatalf("routine trigger %+v", triggers)
	}
	channel, _ := app.Store.Channel(ctx, 100)
	if last := channel[len(channel)-1].Text; !strings.Contains(last, "早上好") || !strings.Contains(last, "Q4 规划") {
		t.Fatalf("brief should summarise the agenda and the manager's mail: %q", last)
	}
}

func TestRestartRoutesFollowUpToRestoredWorker(t *testing.T) {
	clock := newClock()
	dir := t.TempDir()
	app, stop := startApp(t, dir, clock)
	first := deliver(t, app, PresetManagerFollow, clock.Now())
	worker := triggerByID(t, settle(t, app.Store), first).SessionID
	if worker == "" {
		t.Fatal("first follow-up has no worker")
	}
	stop()

	app, _ = startApp(t, dir, clock)
	second := deliver(t, app, PresetManagerFollow, clock.Now().Add(time.Second))
	if got := triggerByID(t, settle(t, app.Store), second); got.SessionID != worker || got.Status != TriggerDone {
		t.Fatalf("follow-up after restart went to %q (%s), want subscribed worker %q", got.SessionID, got.Status, worker)
	}
	ledger, _ := app.Store.Ledger(context.Background(), 200)
	restored := false
	for _, entry := range ledger {
		restored = restored || (entry.Kind == LedgerRoute && entry.TriggerID == second && strings.Contains(entry.Detail, "从持久状态恢复"))
	}
	if !restored {
		t.Fatal("route should record that the worker was restored from durable state")
	}
}

func TestInterruptedTriggersRequeueAndProposalsDedupe(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, t.TempDir()+"/assistant.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := newClock().Now()
	if _, err := store.Deliver(ctx, PresetManager, now); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := store.ClaimTrigger(ctx, now)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, again, _ := store.ClaimTrigger(ctx, now); again {
		t.Fatal("a running trigger must not be claimed twice")
	}
	if count, err := store.RequeueInterrupted(ctx, now); err != nil || count != 1 {
		t.Fatalf("requeue %d %v", count, err)
	}
	if reclaimed, ok, _ := store.ClaimTrigger(ctx, now); !ok || reclaimed.ID != claimed.ID || reclaimed.Attempts != 2 {
		t.Fatalf("reclaimed %+v", reclaimed)
	}
	draft := Proposal{Kind: ProposalSendEmail, TriggerID: claimed.ID, Thread: claimed.Thread, SessionID: "s", ActionID: "a", To: ManagerAddress, Subject: "Re", Body: "ok"}
	first, created, err := store.CreateProposal(ctx, draft, now)
	if err != nil || !created {
		t.Fatal(created, err)
	}
	draft.To = strings.ToUpper(draft.To)
	second, created, err := store.CreateProposal(ctx, draft, now)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("re-run should reuse proposal #%d, got #%d created=%v err=%v", first.ID, second.ID, created, err)
	}
}

func TestHandlerRefusesForeignHostsAndFormPosts(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	handler := app.Handler()
	cases := []struct {
		name, method, host, path, contentType string
		want                                  int
	}{
		{"page", http.MethodGet, "127.0.0.1:8790", "/", "", http.StatusOK},
		{"rebinding host", http.MethodGet, "evil.example:8790", "/api/state", "", http.StatusForbidden},
		{"form post", http.MethodPost, "localhost:8790", "/api/chat", "application/x-www-form-urlencoded", http.StatusForbidden},
		{"unknown decision", http.MethodPost, "localhost:8790", "/api/proposals/1/launch", "application/json", http.StatusNotFound},
		{"unknown preset", http.MethodPost, "[::1]:8790", "/api/simulate", "application/json", http.StatusBadRequest},
	}
	for _, c := range cases {
		body := strings.NewReader(`{"preset":"nope"}`)
		request := httptest.NewRequest(c.method, c.path, body)
		request.Host = c.host
		if c.contentType != "" {
			request.Header.Set("Content-Type", c.contentType)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != c.want {
			t.Errorf("%s: status %d, want %d (%s)", c.name, recorder.Code, c.want, recorder.Body.String())
		}
	}
}
