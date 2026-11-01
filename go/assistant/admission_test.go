package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// apiCall sends one request to the daemon's page API.
func apiCall(t *testing.T, app *App, method, path, payload string) (int, string) {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(payload))
	request.Host = "127.0.0.1:8790"
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.String()
}

// waitJob waits until a preparation job reaches want.
func waitJob(t *testing.T, app *App, id int64, want JobStatus) Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		job, found, err := app.Store.Job(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if found && job.Status == want {
			return job
		}
		time.Sleep(20 * time.Millisecond)
	}
	job, _, _ := app.Store.Job(context.Background(), id)
	t.Fatalf("job #%d is %s, want %s", id, job.Status, want)
	return Job{}
}

func countChannel(t *testing.T, app *App, fragment string) int {
	t.Helper()
	channel, err := app.Store.Channel(context.Background(), 500)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, message := range channel {
		if strings.Contains(message.Text, fragment) {
			count++
		}
	}
	return count
}

// In observe mode, the default, mail the admission grade marks as an inferred
// candidate passes the triage and never reaches a worker on its own: the user
// hands it over or ignores it on the page.
func TestTriageObservesInferredCandidatesUntilTheUserHandsThemOver(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	if mode, err := app.Store.CandidateMode(ctx); err != nil || mode != CandidateObserve {
		t.Fatalf("candidate mode defaults to observe: %s %v", mode, err)
	}
	cc := deliver(t, app, PresetCc)
	stranger := deliver(t, app, PresetStranger)
	phishing := deliver(t, app, PresetPhishing)
	triggers := settle(t, app.Store)
	for _, c := range []struct {
		id       int64
		status   TriggerStatus
		fragment string
	}{
		{cc, TriggerObserved, "用户只在抄送里"},
		{stranger, TriggerIgnored, "推广或合作邀约"},
		{phishing, TriggerObserved, "观察模式只记录"},
	} {
		if got := triggerByID(t, triggers, c.id); got.Status != c.status || !strings.Contains(got.Decision, c.fragment) || got.SessionID != "" {
			t.Errorf("trigger #%d: %s %q, want %s with %q and no worker turn", c.id, got.Status, got.Decision, c.status, c.fragment)
		}
	}
	if countChannel(t, app, "可疑邮件") != 0 {
		t.Fatal("observe mode must not run the worker")
	}
	if stats := app.Router.Stats(); stats.Triaged != 3 || stats.TriageHandle != 1 || stats.TriageObserve != 1 || stats.TriageIgnore != 1 || stats.Observed != 2 || stats.Turns != 0 {
		t.Fatalf("router stats %+v", stats)
	}
	if !ledgerHas(t, app, LedgerTriage, "分诊 handle：含可疑指令") || !ledgerHas(t, app, LedgerTriage, "陌生发件人直接写给你") {
		t.Fatal("the triage ledger rows should carry the verdict and the grade")
	}
	candidates, err := app.Store.Candidates(ctx, 10)
	if err != nil || len(candidates) != 2 || candidates[0].ID != phishing || candidates[1].ID != cc {
		t.Fatalf("candidates: %+v %v", candidates, err)
	}

	// The user hands the warning-worthy mail to a worker; the router skips
	// the triage this time.
	if code, body := apiCall(t, app, http.MethodPost, "/api/candidates/"+strconv.FormatInt(phishing, 10)+"/handle", `{}`); code != http.StatusOK || !strings.Contains(body, `"admitted":true`) {
		t.Fatalf("handle: %d %s", code, body)
	}
	triggers = settle(t, app.Store)
	if got := triggerByID(t, triggers, phishing); got.Status != TriggerDone || got.SessionID == "" {
		t.Fatalf("handed-over candidate: %+v", got)
	}
	if countChannel(t, app, "可疑邮件") != 1 || app.Router.Stats().Triaged != 3 {
		t.Fatalf("the worker warns once without another triage: %+v", app.Router.Stats())
	}
	if code, _ := apiCall(t, app, http.MethodPost, "/api/candidates/"+strconv.FormatInt(cc, 10)+"/ignore", `{}`); code != http.StatusOK {
		t.Fatalf("ignore: %d", code)
	}
	if got := triggerStatus(t, app, cc); got.Status != TriggerIgnored || got.Decision != "用户忽略这个候选" {
		t.Fatalf("ignored candidate: %+v", got)
	}
	for _, c := range []struct {
		path string
		code int
	}{
		{"/api/candidates/" + strconv.FormatInt(cc, 10) + "/handle", http.StatusConflict},
		{"/api/candidates/9999/handle", http.StatusNotFound},
		{"/api/candidates/" + strconv.FormatInt(cc, 10) + "/maybe", http.StatusNotFound},
	} {
		if code, body := apiCall(t, app, http.MethodPost, c.path, `{}`); code != c.code {
			t.Errorf("%s: %d %s, want %d", c.path, code, body, c.code)
		}
	}

	// Auto mode lets a handle verdict run the worker directly.
	if code, _ := apiCall(t, app, http.MethodPost, "/api/candidates/mode", `{"mode":"loud"}`); code != http.StatusBadRequest {
		t.Fatalf("unknown mode: %d", code)
	}
	if code, _ := apiCall(t, app, http.MethodPost, "/api/candidates/mode", `{"mode":"auto"}`); code != http.StatusOK {
		t.Fatalf("auto mode: %d", code)
	}
	if !channelHas(t, app, "推断候选改为自动模式") {
		t.Fatal("the mode switch should be visible in the channel")
	}
	again := deliver(t, app, PresetPhishing)
	if got := triggerByID(t, settle(t, app.Store), again); got.Status != TriggerDone || got.SessionID == "" || countChannel(t, app, "可疑邮件") != 2 {
		t.Fatalf("auto mode runs the worker: %+v", got)
	}
	_, body := apiCall(t, app, http.MethodGet, "/api/state", "")
	var state StateView
	if err := json.Unmarshal([]byte(body), &state); err != nil {
		t.Fatal(err)
	}
	if state.CandidateMode != CandidateAuto || len(state.Candidates) != 0 || state.Router.Triaged != 4 || len(state.Contacts) < len(seedContacts) {
		t.Fatalf("state view: mode %s candidates %d router %+v contacts %d", state.CandidateMode, len(state.Candidates), state.Router, len(state.Contacts))
	}
}

// triageOutage fails every triage request and passes worker turns through.
type triageOutage struct{ next agent.Provider }

func (m triageOutage) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if request.Purpose == protocol.PurposeDecision {
		return protocol.ModelReply{}, errors.New("triage model unavailable")
	}
	return m.next.Complete(ctx, request)
}

// A failed triage records the candidate: an unclear mail becomes neither
// work nor silence while the model is down.
func TestFailedTriageRecordsTheCandidate(t *testing.T) {
	clock := newClock()
	app, _ := launchApp(t, t.TempDir(), clock, appOptions{provider: triageOutage{&ScriptedModel{Now: clock.Now}}})
	ctx := context.Background()
	if err := app.Store.SetCandidateMode(ctx, CandidateAuto, "test", clock.Now()); err != nil {
		t.Fatal(err)
	}
	phishing := deliver(t, app, PresetPhishing)
	manager := deliver(t, app, PresetManager)
	triggers := settle(t, app.Store)
	if got := triggerByID(t, triggers, phishing); got.Status != TriggerObserved || !strings.Contains(got.Decision, "分诊失败") {
		t.Fatalf("failed triage: %+v", got)
	}
	if got := triggerByID(t, triggers, manager); got.Status != TriggerDone {
		t.Fatalf("an explicit request needs no triage: %+v", got)
	}
	if stats := app.Router.Stats(); stats.TriageFailed != 1 || stats.Explicit != 1 {
		t.Fatalf("router stats %+v", stats)
	}
}

// A bill from a known vendor starts a background preparation job; its result
// comes back to the bill's thread as a job.done trigger and only then reaches
// the user.
func TestBillStartsAPreparationJobWhoseResultReturnsToItsThread(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	bill := deliver(t, app, PresetBill)
	origin := triggerByID(t, settle(t, app.Store), bill)
	if origin.Status != TriggerQuiet || !strings.Contains(origin.Decision, "核对完再汇报") {
		t.Fatalf("the bill turn stays quiet until the check is done: %+v", origin)
	}
	jobs, err := app.Store.Jobs(ctx, 10)
	if err != nil || len(jobs) != 1 || jobs[0].Thread != origin.Thread || jobs[0].TriggerID != bill || jobs[0].SessionID != origin.SessionID {
		t.Fatalf("one job on the bill's thread: %+v %v", jobs, err)
	}
	job := waitJob(t, app, jobs[0].ID, JobDone)
	if !strings.Contains(job.Result, "比上一期多 ¥200") || !strings.Contains(job.Result, "m-seed-4") || job.FinishedAt == nil {
		t.Fatalf("job result should compare with last month's bill: %+v", job)
	}
	triggers := settle(t, app.Store)
	var done Trigger
	for _, trigger := range triggers {
		if trigger.Kind == TriggerJobDone && trigger.Ref == JobRef(job.ID) {
			done = trigger
		}
	}
	if done.ID == 0 || done.Thread != origin.Thread || done.Status != TriggerDone {
		t.Fatalf("job.done trigger: %+v", done)
	}
	if text := lastChannel(t, app); !strings.Contains(text, "后台核对完成") || !strings.Contains(text, "¥200") {
		t.Fatalf("the worker passes the result on: %q", text)
	}
	if !ledgerHas(t, app, LedgerJob, "开始排队") || !ledgerHas(t, app, LedgerJob, "完成：") {
		t.Fatal("the job's lifecycle should be in the ledger")
	}
	// The preparation session holds only the read tools.
	ledger, _ := app.Store.Ledger(ctx, 500)
	prep := ""
	for _, entry := range ledger {
		if entry.Kind == LedgerTool && entry.TriggerID == bill && entry.SessionID != origin.SessionID && entry.SessionID != "" {
			prep = entry.SessionID
			if !strings.Contains(entry.Detail, string(ToolMailList)) {
				t.Fatalf("preparation should only read: %+v", entry)
			}
		}
	}
	if prep == "" {
		t.Fatal("the preparation session's tool calls should be bound to the bill's trigger")
	}
}

// The user stops a job from the page; a cancelled job wakes nobody, and a
// job a crashed process left running is queued again on restart.
func TestJobsCanBeStoppedAndSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	clock := newClock()
	app, stop := startApp(t, dir, clock)
	ctx := context.Background()
	if _, err := app.SetPaused(ctx, true); err != nil {
		t.Fatal(err)
	}
	queued, created, err := app.Store.CreateJob(ctx, Job{Thread: "t-bill-x", TriggerID: 1, Goal: "核对账单"}, clock.Now())
	if err != nil || !created {
		t.Fatalf("create: %+v %v", queued, err)
	}
	if again, created, err := app.Store.CreateJob(ctx, Job{Thread: "t-bill-x", TriggerID: 1, Goal: " 核对账单 "}, clock.Now()); err != nil || created || again.ID != queued.ID {
		t.Fatalf("the same goal from the same trigger is one job: %+v %v %v", again, created, err)
	}
	if code, body := apiCall(t, app, http.MethodPost, "/api/jobs/"+strconv.FormatInt(queued.ID, 10)+"/stop", `{}`); code != http.StatusOK || !strings.Contains(body, `"status":"cancelled"`) {
		t.Fatalf("stop: %d %s", code, body)
	}
	if code, _ := apiCall(t, app, http.MethodPost, "/api/jobs/"+strconv.FormatInt(queued.ID, 10)+"/stop", `{}`); code != http.StatusConflict {
		t.Fatalf("stopping twice: %d", code)
	}
	if code, _ := apiCall(t, app, http.MethodPost, "/api/jobs/9999/stop", `{}`); code != http.StatusNotFound {
		t.Fatalf("unknown job: %d", code)
	}
	if trigger, err := app.Store.FinishJob(ctx, queued.ID, JobDone, "late result", clock.Now()); err != nil || trigger != 0 {
		t.Fatalf("a cancelled job wakes nobody: %d %v", trigger, err)
	}
	for i := 0; i < maxOpenJobs; i++ {
		if _, _, err := app.Store.CreateJob(ctx, Job{Thread: "t-many", TriggerID: 2, Goal: "目标 " + strconv.Itoa(i)}, clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := app.Store.CreateJob(ctx, Job{Thread: "t-many", TriggerID: 2, Goal: "one more"}, clock.Now()); !errors.Is(err, ErrTooManyJobs) {
		t.Fatalf("open jobs are capped, got %v", err)
	}
	for i := 0; i < maxOpenJobs; i++ {
		jobs, _ := app.Store.Jobs(ctx, 20)
		for _, job := range jobs {
			if job.Status == JobQueued && job.Thread == "t-many" {
				if _, err := app.Store.CancelJob(ctx, job.ID, "test", clock.Now()); err != nil {
					t.Fatal(err)
				}
			}
		}
	}

	// A job left running by a crash is queued again and runs after resume.
	crashed, _, err := app.Store.CreateJob(ctx, Job{Thread: "t-bill-y", TriggerID: 3, Goal: "核对账单"}, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if claimed, ok, err := app.Store.ClaimJob(ctx, clock.Now()); err != nil || !ok || claimed.ID != crashed.ID {
		t.Fatalf("claim: %+v %v %v", claimed, ok, err)
	}
	stop()
	app, _ = startApp(t, dir, clock)
	if job, _, _ := app.Store.Job(ctx, crashed.ID); job.Status != JobQueued {
		t.Fatalf("restart requeues the running job: %+v", job)
	}
	if _, err := app.SetPaused(ctx, false); err != nil {
		t.Fatal(err)
	}
	if job := waitJob(t, app, crashed.ID, JobDone); !strings.Contains(job.Result, "m-seed-4") {
		t.Fatalf("requeued job result: %+v", job)
	}
	settle(t, app.Store)
	if !channelHas(t, app, "后台核对完成") {
		t.Fatal("the requeued job's result should reach its thread")
	}
}

// The watchdog flags a task whose checks keep saying the same thing, once
// per streak, and clears the flag when a check makes progress.
func TestWatchdogFlagsATaskWhoseChecksStopMakingProgress(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	origin := deliver(t, app, PresetManager)
	task := taskOnThread(t, app, triggerByID(t, settle(t, app.Store), origin).Thread)
	ref := ProgressRef(task.ID)
	for check := 1; check <= watchdogRepeat; check++ {
		clock.Set(clock.Now().Add(61 * time.Minute))
		app.Scheduler.DueTasks(ctx)
		settle(t, app.Store)
		if flagged, err := app.Store.Watchdog(ctx, clock.Now()); err != nil || (flagged != 0) != (check == watchdogRepeat) {
			t.Fatalf("check %d: flagged %d %v", check, flagged, err)
		}
	}
	if kind := attentionRefs(t, app)[ref]; kind != AttentionNoProgress {
		t.Fatalf("the stuck task should be on the attention list, got %q", kind)
	}
	if !channelHas(t, app, "看门狗：任务 #") || !ledgerHas(t, app, LedgerWatchdog, "没有实质进展") {
		t.Fatal("the flag should reach the channel and the ledger")
	}
	if flagged, _ := app.Store.Watchdog(ctx, clock.Now()); flagged != 0 || countChannel(t, app, "看门狗：任务 #") != 1 {
		t.Fatal("one streak is flagged once")
	}
	if code, _ := apiCall(t, app, http.MethodPost, "/api/attention/"+ref+"/dismiss", `{}`); code != http.StatusOK {
		t.Fatalf("dismiss: %d", code)
	}
	if _, listed := attentionRefs(t, app)[ref]; listed {
		t.Fatal("a dismissed flag leaves the list")
	}

	// Progress clears the flag; a new streak raises it again.
	if _, err := app.Store.db.ExecContext(ctx, `UPDATE tasks SET note=? WHERE id=?`, "报价单已补齐", task.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < watchdogRepeat; i++ {
		if err := app.Store.RecordTaskCheck(ctx, task.ID, 0, clock.Now()); err != nil {
			t.Fatal(err)
		}
		flagged, err := app.Store.Watchdog(ctx, clock.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, held, _ := app.Store.watchdogFlag(ctx, task.ID); held != (i == watchdogRepeat-1) || (flagged == 1) != held {
			t.Fatalf("after progress check %d: flag %v, flagged %d", i+1, held, flagged)
		}
	}
	if kind := attentionRefs(t, app)[ref]; kind != AttentionNoProgress {
		t.Fatal("a new streak resurfaces the dismissed flag")
	}
	// A closed task leaves the list; Tick runs the watchdog with the
	// scheduler.
	if code, _ := apiCall(t, app, http.MethodPost, "/api/tasks/"+strconv.FormatInt(task.ID, 10)+"/cancel", `{}`); code != http.StatusOK {
		t.Fatalf("cancel task: %d", code)
	}
	app.Scheduler.Tick(ctx)
	if _, listed := attentionRefs(t, app)[ref]; listed {
		t.Fatal("a closed task is no longer watched")
	}
}

func TestKnownContactsAndLearnedRecipients(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	for _, address := range []string{ManagerAddress, " Bob@Friends.example ", VendorAddress} {
		if known, err := app.Store.KnownContact(ctx, address); err != nil || !known {
			t.Fatalf("%s should be a seeded contact: %v", address, err)
		}
	}
	if known, _ := app.Store.KnownContact(ctx, "growth@leadgen-partners.example"); known {
		t.Fatal("strangers are not contacts")
	}
	if err := noteContact(ctx, app.Store.db, "New@Example.com", "New", "outbox", clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := noteContact(ctx, app.Store.db, "not an address", "", "outbox", clock.Now()); err != nil {
		t.Fatal(err)
	}
	contacts, err := app.Store.Contacts(ctx, 20)
	if err != nil || len(contacts) != len(seedContacts)+1 || contacts[0].Address != "new@example.com" || contacts[0].Source != "outbox" || contacts[0].Interactions != 1 {
		t.Fatalf("a recipient becomes a contact: %+v %v", contacts, err)
	}
	if err := app.Store.seedContacts(ctx, clock.Now()); err != nil {
		t.Fatal(err)
	}
	if again, _ := app.Store.Contacts(ctx, 20); len(again) != len(contacts) {
		t.Fatal("contacts are seeded once")
	}
}
