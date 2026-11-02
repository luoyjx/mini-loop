package assistant

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
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

// startApp runs a daemon whose connector reaches the World through go/mcp's
// in-process client: the same tool names, JSON arguments and result text as
// the stdio child, without a process boundary.
func startApp(t *testing.T, dir string, clock *fixedClock) (*App, func()) {
	t.Helper()
	return startAppWith(t, dir, clock, nil)
}

func startAppWith(t *testing.T, dir string, clock *fixedClock, wrap func(mcpCaller) mcpCaller) (*App, func()) {
	t.Helper()
	return launchApp(t, dir, clock, appOptions{wrap: wrap})
}

// appOptions overrides the test daemon's MCP caller, model, worker count,
// embedder or, when clock is nil, its time source.
type appOptions struct {
	wrap     func(mcpCaller) mcpCaller
	provider agent.Provider
	workers  int
	now      func() time.Time
	embedder Embedder
}

func launchApp(t *testing.T, dir string, clock *fixedClock, options appOptions) (*App, func()) {
	t.Helper()
	ctx := context.Background()
	wrap := options.wrap
	now := options.now
	if clock != nil {
		now = clock.Now
	}
	var provider agent.Provider = &ScriptedModel{Now: now}
	if options.provider != nil {
		provider = options.provider
	}
	workers := options.workers
	if workers == 0 {
		workers = 2
	}
	world, err := OpenWorld(ctx, filepath.Join(dir, "mock-mcp", "world.db"), now)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewInProcessMock(world)
	if err != nil {
		t.Fatal(err)
	}
	var caller mcpCaller = client
	if wrap != nil {
		caller = wrap(caller)
	}
	app, err := NewApp(ctx, Config{DataDir: dir, Connector: NewConnector(caller, "in-process"), Provider: provider, Model: ScriptedModelID, Now: now, Workers: workers, SchedulerTick: time.Hour, PollInterval: time.Hour, LeaseTTL: 2 * time.Second, Embedder: options.embedder})
	if err != nil {
		t.Fatal(errors.Join(err, world.Close()))
	}
	runCtx, cancel := context.WithCancel(ctx)
	app.Start(runCtx)
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if err := errors.Join(app.Close(shutdown), world.Close()); err != nil {
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

// deliver drops a preset into the external inbox through the simulator tool,
// polls once like the daemon would and returns the resulting trigger ID.
func deliver(t *testing.T, app *App, preset Preset) int64 {
	t.Helper()
	ctx := context.Background()
	mail, err := app.Connector.DeliverPreset(ctx, preset)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Poller.PollOnce(ctx); err != nil {
		t.Fatal(err)
	}
	triggers, err := app.Store.Triggers(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, trigger := range triggers {
		if trigger.Ref == mail.ID {
			return trigger.ID
		}
	}
	t.Fatalf("mail %s produced no trigger", mail.ID)
	return 0
}

// waitProposal waits until the outbox moves a proposal to want.
func waitProposal(t *testing.T, app *App, id int64, want ProposalStatus) Proposal {
	t.Helper()
	app.Outbox.Wake()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		p, ok, err := app.Store.Proposal(context.Background(), id)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		if p.Status == want {
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	p, _, _ := app.Store.Proposal(context.Background(), id)
	t.Fatalf("proposal #%d is %s (%s), want %s", id, p.Status, p.LastError, want)
	return Proposal{}
}

func TestRulesKeepBulkMailAwayFromTheModel(t *testing.T) {
	rules := DefaultRules()
	cases := []struct {
		name    string
		trigger Trigger
		mail    *Email
		known   bool
		want    Verdict
		grade   Grade
	}{
		{"newsletter", Trigger{Kind: TriggerEmail}, &Email{From: "a@b.example", To: OwnerAddress, Category: CategoryNewsletter}, true, VerdictIgnore, ""},
		{"noreply sender", Trigger{Kind: TriggerEmail}, &Email{From: "NoReply@shop.example", To: OwnerAddress, Category: CategoryPerson}, true, VerdictIgnore, ""},
		{"known, addressed", Trigger{Kind: TriggerEmail}, &Email{From: "bob@friends.example", To: OwnerAddress, Category: CategoryPerson}, true, VerdictCandidate, GradeExplicit},
		{"known, copied", Trigger{Kind: TriggerEmail}, &Email{From: ManagerAddress, To: TeamAddress, Cc: "Me@Acme.example", Category: CategoryPerson}, true, VerdictCandidate, GradeInferred},
		{"stranger, addressed", Trigger{Kind: TriggerEmail}, &Email{From: "x@y.example", To: "other@acme.example, " + OwnerAddress, Category: CategoryPerson}, false, VerdictCandidate, GradeInferred},
		{"stranger, copied", Trigger{Kind: TriggerEmail}, &Email{From: "x@y.example", To: TeamAddress, Cc: OwnerAddress, Category: CategoryPerson}, false, VerdictIgnore, ""},
		{"missing mail", Trigger{Kind: TriggerEmail}, nil, true, VerdictIgnore, ""},
		{"invite", Trigger{Kind: TriggerInvite}, nil, false, VerdictCandidate, GradeExplicit},
		{"chat", Trigger{Kind: TriggerChat}, nil, false, VerdictCandidate, GradeExplicit},
	}
	for _, c := range cases {
		if got := rules.Classify(c.trigger, c.mail, c.known); got.Verdict != c.want || got.Grade != c.grade {
			t.Errorf("%s: verdict %s grade %q, want %s %q", c.name, got.Verdict, got.Grade, c.want, c.grade)
		}
	}
}

func TestProactiveFlowDraftsButNeverSendsWithoutApproval(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	// The phishing sender is a stranger, so its mail is an inferred
	// candidate; auto mode lets the triage hand it to a worker.
	if err := app.Store.SetCandidateMode(ctx, CandidateAuto, "test", clock.Now()); err != nil {
		t.Fatal(err)
	}
	managerID := deliver(t, app, PresetManager)
	inviteID := deliver(t, app, PresetInviteConflict)
	newsletterID := deliver(t, app, PresetNewsletter)
	phishingID := deliver(t, app, PresetPhishing)
	triggers := settle(t, app.Store)

	if got := triggerByID(t, triggers, newsletterID).Status; got != TriggerIgnored {
		t.Fatalf("newsletter status %s, want ignored by the fast path", got)
	}
	if got := triggerByID(t, triggers, phishingID); got.Status != TriggerDone || got.SessionID == "" {
		t.Fatalf("phishing trigger %+v", got)
	}
	sent, err := app.Connector.ListMail(ctx, FolderSent, 10)
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
	if err != nil || decided.Status != ProposalApproved {
		t.Fatalf("approve: %+v %v", decided, err)
	}
	if _, err := app.Store.DecideProposal(ctx, reply[0].ID, true, "test", clock.Now()); !errors.Is(err, ErrProposalDecided) {
		t.Fatalf("second approval must be refused, got %v", err)
	}
	if _, err := app.Store.DecideProposal(ctx, invite[0].ID, false, "test", clock.Now()); err != nil {
		t.Fatal(err)
	}
	if done := waitProposal(t, app, reply[0].ID, ProposalSent); done.Receipt == "" || done.Attempts != 1 {
		t.Fatalf("sent proposal should carry the MCP receipt: %+v", done)
	}
	sent, _ = app.Connector.ListMail(ctx, FolderSent, 10)
	if len(sent) != 1 || sent[0].To != ManagerAddress {
		t.Fatalf("exactly the approved mail is sent: %+v", sent)
	}
	if contacts, _ := app.Store.Contacts(ctx, 20); len(contacts) == 0 || contacts[0].Address != ManagerAddress || contacts[0].Interactions != 1 || contacts[0].LastAt == nil {
		t.Fatalf("the send counts as an interaction with the manager: %+v", contacts)
	}
	event, _, _ := app.Connector.Event(ctx, invite[0].EventID)
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
	id := deliver(t, app, PresetFriend)
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
	if fired := app.Scheduler.Due(ctx); fired != 0 {
		t.Fatalf("06:30 is before the 07:00 brief, fired %d", fired)
	}
	stop()
	// The daemon was down at 07:00; the next start catches up once.
	clock.Set(time.Date(2026, 11, 2, 9, 15, 0, 0, time.Local))
	app, _ = startApp(t, dir, clock)
	// The scheduler loop's first pass races this call; either one claims it.
	app.Scheduler.Due(ctx)
	if fired := app.Scheduler.Due(ctx); fired != 0 {
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

// A laptop asleep at 07:00 resumes with tickers that never fired; the first
// tick after wake claims the slot once and labels it a catch-up.
func TestRoutineCatchesUpAfterSleepWithoutRestart(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	if fired := app.Scheduler.Due(ctx); fired != 0 {
		t.Fatalf("fired %d before 07:00", fired)
	}
	clock.Set(time.Date(2026, 11, 2, 9, 40, 0, 0, time.Local))
	app.Scheduler.Due(ctx)
	if fired := app.Scheduler.Due(ctx); fired != 0 {
		t.Fatalf("the woken slot must fire once, fired %d more", fired)
	}
	if triggers := settle(t, app.Store); len(triggers) != 1 || !strings.Contains(triggers[0].Summary, "补跑") {
		t.Fatalf("catch-up trigger %+v", triggers)
	}
}

func TestRestartRoutesFollowUpToRestoredWorker(t *testing.T) {
	clock := newClock()
	dir := t.TempDir()
	app, stop := startApp(t, dir, clock)
	first := deliver(t, app, PresetManagerFollow)
	worker := triggerByID(t, settle(t, app.Store), first).SessionID
	if worker == "" {
		t.Fatal("first follow-up has no worker")
	}
	stop()

	app, _ = startApp(t, dir, clock)
	second := deliver(t, app, PresetManagerFollow)
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
	// The second follow-up saw the thread's task in its prompt and extended it.
	tasks, _ := app.Store.ActiveTasks(context.Background(), "t-q4-plan")
	if len(tasks) != 1 || !strings.Contains(tasks[0].Note, "又来信") || tasks[0].SessionID != worker {
		t.Fatalf("one follow-up task per thread, extended by the restored worker: %+v", tasks)
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
	mails, _, err := presetMail(PresetManager, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	inbound := []InboundMail{{Seq: 7, Mail: mails[0]}}
	if created, err := store.IngestMail(ctx, inbound, 7, now); err != nil || created != 1 {
		t.Fatal(created, err)
	}
	// A crash before the cursor commit re-polls the same page: no new trigger.
	if created, err := store.IngestMail(ctx, inbound, 7, now); err != nil || created != 0 {
		t.Fatalf("re-polled mail must not enqueue again: %d %v", created, err)
	}
	batch, err := store.ClaimBatch(ctx, now, false)
	if err != nil || len(batch) != 1 {
		t.Fatal(batch, err)
	}
	claimed := batch[0]
	if again, _ := store.ClaimBatch(ctx, now, false); len(again) != 0 {
		t.Fatal("a running trigger must not be claimed twice")
	}
	if count, err := store.RequeueInterrupted(ctx, now); err != nil || count != 1 {
		t.Fatalf("requeue %d %v", count, err)
	}
	if reclaimed, _ := store.ClaimBatch(ctx, now, false); len(reclaimed) != 1 || reclaimed[0].ID != claimed.ID || reclaimed[0].Attempts != 2 {
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
