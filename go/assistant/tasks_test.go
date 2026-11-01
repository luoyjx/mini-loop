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

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func taskOnThread(t *testing.T, app *App, thread ThreadID) Task {
	t.Helper()
	tasks, err := app.Store.Tasks(context.Background(), 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		if task.Thread == thread {
			return task
		}
	}
	t.Fatalf("no task on thread %s: %+v", thread, tasks)
	return Task{}
}

func lastChannel(t *testing.T, app *App) string {
	t.Helper()
	channel, err := app.Store.Channel(context.Background(), 200)
	if err != nil || len(channel) == 0 {
		t.Fatal(err)
	}
	return channel[len(channel)-1].Text
}

func channelHas(t *testing.T, app *App, fragment string) bool {
	t.Helper()
	channel, err := app.Store.Channel(context.Background(), 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, message := range channel {
		if strings.Contains(message.Text, fragment) {
			return true
		}
	}
	return false
}

// The manager's mail becomes a draft plus a follow-up task. Approving the
// draft sends it; that outcome brings the task's check forward, the same
// subscribed worker wakes, finds the reply in the sent folder and closes the
// task without waiting for its scheduled hour.
func TestTaskFollowsAReplyUntilItIsSent(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	origin := deliver(t, app, PresetManager)
	triggers := settle(t, app.Store)
	worker := triggerByID(t, triggers, origin)
	task := taskOnThread(t, app, worker.Thread)
	if task.Status != TaskActive || task.WakeAt == nil || !task.WakeAt.Equal(clock.Now().Add(time.Hour)) || task.SessionID != worker.SessionID || task.MaxChecks != defaultMaxChecks {
		t.Fatalf("follow-up task %+v", task)
	}
	if !channelHas(t, app, "建了任务 #") {
		t.Fatal("the worker's report should mention the follow-up task")
	}
	drafts, _ := app.Store.ProposalsForTrigger(ctx, origin)
	if _, err := app.Store.DecideProposal(ctx, drafts[0].ID, true, "test", clock.Now()); err != nil {
		t.Fatal(err)
	}
	waitProposal(t, app, drafts[0].ID, ProposalSent)
	waitFor(t, "the task to close", func() bool {
		current, _, err := app.Store.Task(ctx, task.ID)
		return err == nil && current.Status == TaskDone
	})
	triggers = settle(t, app.Store)
	done, _, _ := app.Store.Task(ctx, task.ID)
	if done.Checks != 1 || !strings.Contains(done.Result, "已发出") || done.ClosedBy != "worker" || done.WakeAt != nil {
		t.Fatalf("closed task %+v", done)
	}
	wakeup := triggerByID(t, triggers, done.LastTrigger)
	if wakeup.Kind != TriggerTaskWakeup || wakeup.SessionID != worker.SessionID || !strings.Contains(wakeup.Summary, "已经发出") {
		t.Fatalf("the wakeup should reach the subscribed worker with the reason: %+v", wakeup)
	}
	if !channelHas(t, app, "✅ 任务 #") {
		t.Fatal("the worker should report the completed task")
	}
	brought := false
	for _, entry := range ledgerKinds(t, app, LedgerTask) {
		brought = brought || strings.Contains(entry.Detail, "提前检查：提案 #")
	}
	if !brought {
		t.Fatal("the ledger should record why the check moved forward")
	}
}

// Without an approval the worker keeps checking on its own schedule. After
// defaultMaxChecks wakeups the scheduler hands the task to the user instead
// of waking it again.
func TestTaskChecksAgainUntilItsBudgetRunsOut(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	origin := deliver(t, app, PresetManager)
	task := taskOnThread(t, app, triggerByID(t, settle(t, app.Store), origin).Thread)
	for check := 1; check <= defaultMaxChecks; check++ {
		clock.Set(clock.Now().Add(61 * time.Minute))
		app.Scheduler.DueTasks(ctx)
		settle(t, app.Store)
		current, _, _ := app.Store.Task(ctx, task.ID)
		if current.Status != TaskActive || current.Checks != check || !strings.Contains(current.Note, "还没发出") || current.WakeReason != "worker 约定" {
			t.Fatalf("check %d: %+v", check, current)
		}
		if current.WakeAt == nil || !current.WakeAt.Equal(clock.Now().Add(30*time.Minute)) {
			t.Fatalf("check %d should schedule the next one in 30 minutes: %v", check, current.WakeAt)
		}
	}
	if !channelHas(t, app, "⏳ 任务 #") {
		t.Fatal("pending checks should remind the user")
	}
	clock.Set(clock.Now().Add(31 * time.Minute))
	if woken := app.Scheduler.DueTasks(ctx); woken != 0 {
		t.Fatalf("an exhausted task must not wake again, woke %d", woken)
	}
	stalled, _, _ := app.Store.Task(ctx, task.ID)
	if stalled.Status != TaskStalled || stalled.ClosedBy != "scheduler" {
		t.Fatalf("stalled task %+v", stalled)
	}
	if text := lastChannel(t, app); !strings.Contains(text, "交给你处理") {
		t.Fatalf("the user should be told: %q", text)
	}
}

// A chat reminder is a task on the chat thread that fires once at its time.
func TestChatReminderFiresAtItsTime(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	if _, err := app.Store.ChatFromUser(ctx, "10 分钟后提醒我 给 Bob 回电话", clock.Now()); err != nil {
		t.Fatal(err)
	}
	app.Router.Wake()
	settle(t, app.Store)
	task := taskOnThread(t, app, ChatThread)
	if task.Title != "提醒：给 Bob 回电话" || !task.WakeAt.Equal(clock.Now().Add(10*time.Minute)) {
		t.Fatalf("reminder task %+v", task)
	}
	if text := lastChannel(t, app); !strings.Contains(text, "任务 #") {
		t.Fatalf("the reply should confirm the task: %q", text)
	}
	if app.Scheduler.DueTasks(ctx) != 0 {
		t.Fatal("a reminder must not fire early")
	}
	clock.Set(clock.Now().Add(10 * time.Minute))
	app.Scheduler.DueTasks(ctx)
	settle(t, app.Store)
	if done, _, _ := app.Store.Task(ctx, task.ID); done.Status != TaskDone {
		t.Fatalf("reminder after firing %+v", done)
	}
	if text := lastChannel(t, app); !strings.Contains(text, "⏰ 提醒：给 Bob 回电话") {
		t.Fatalf("reminder text %q", text)
	}
}

// A meeting booked in the outside world is seen through calendar_list and
// reminded once, with the organizer's mail and related tasks.
func TestUpcomingEventIsRemindedOnce(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	soon, err := app.Connector.AddEvent(ctx, SimEvent{InMinutes: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Connector.AddEvent(ctx, SimEvent{Title: "下午茶", InMinutes: 40}); err != nil {
		t.Fatal(err)
	}
	// The scheduler loop's first tick may race this call; either one claims.
	app.Scheduler.Upcoming(ctx)
	if again := app.Scheduler.Upcoming(ctx); again != 0 {
		t.Fatalf("an event is reminded once, fired %d more", again)
	}
	triggers := settle(t, app.Store)
	if len(triggers) != 1 || triggers[0].Kind != TriggerUpcoming || triggers[0].Ref != soon.ID || triggers[0].Status != TriggerDone || triggers[0].Thread != EventThread(soon) {
		t.Fatalf("only the meeting inside the lead time is reminded: %+v", triggers)
	}
	text := lastChannel(t, app)
	for _, want := range []string{"⏰ 10 分钟后", "客户方案电话会", "Alice（你的经理）", "Q4 规划"} {
		if !strings.Contains(text, want) {
			t.Fatalf("reminder %q lacks %q", text, want)
		}
	}
	clock.Set(clock.Now().Add(11 * time.Minute))
	if fired := app.Scheduler.Upcoming(ctx); fired != 0 {
		t.Fatalf("a started event is not reminded, fired %d", fired)
	}
}

func TestTaskStoreClaimsChecksExactlyOnce(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, t.TempDir()+"/assistant.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := newClock().Now()
	draft := Task{Title: "跟进", Thread: "t-1", SessionID: "s", TriggerID: 1, Check: "回复发出了吗"}
	task, created, err := store.CreateTask(ctx, draft, now.Add(time.Minute), "worker", now)
	if err != nil || !created {
		t.Fatal(created, err)
	}
	if again, created, err := store.CreateTask(ctx, draft, now.Add(time.Hour), "worker", now); err != nil || created || again.ID != task.ID {
		t.Fatalf("a re-run must reuse task #%d: %+v %v %v", task.ID, again, created, err)
	}
	if woken, _, _ := store.ClaimDueTasks(ctx, now, time.Minute); woken != 0 {
		t.Fatal("a task is not due before its check")
	}
	later := now.Add(10 * time.Minute)
	if woken, _, err := store.ClaimDueTasks(ctx, later, time.Minute); err != nil || woken != 1 {
		t.Fatal(woken, err)
	}
	claimed, _, _ := store.Task(ctx, task.ID)
	if claimed.Checks != 1 || claimed.WakeAt == nil || !claimed.WakeAt.Equal(later.Add(taskFallback)) || !strings.Contains(claimed.WakeReason, "兜底") {
		t.Fatalf("a claim must count the check and set the fallback: %+v", claimed)
	}
	wakeups, _ := store.Triggers(ctx, 10)
	if len(wakeups) != 1 || wakeups[0].Kind != TriggerTaskWakeup || wakeups[0].Ref != TaskRef(task.ID) || !strings.Contains(wakeups[0].Summary, "补上错过的检查") {
		t.Fatalf("a check 9 minutes late is a catch-up: %+v", wakeups)
	}
	if _, err := store.WakeTask(ctx, task.ID, "用户点了立即检查", "me", later); err != nil {
		t.Fatal(err)
	}
	if woken, _, _ := store.ClaimDueTasks(ctx, later, time.Minute); woken != 0 {
		t.Fatal("a task whose wakeup is still queued must not wake twice")
	}
	if err := store.FinishTrigger(ctx, wakeups[0].ID, TriggerDone, "", "", later); err != nil {
		t.Fatal(err)
	}
	if woken, _, _ := store.ClaimDueTasks(ctx, later, time.Minute); woken != 1 {
		t.Fatal("after the previous wakeup settles the task wakes again")
	}
	if _, err := store.CloseTask(ctx, task.ID, TaskCancelled, "用户取消", "me", later); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CloseTask(ctx, task.ID, TaskDone, "late", "worker", later); !errors.Is(err, ErrTaskClosed) {
		t.Fatalf("a closed task cannot close again: %v", err)
	}
	for i := 0; i < maxActiveTasks; i++ {
		if _, _, err := store.CreateTask(ctx, Task{Title: "t", Thread: "t-2", TriggerID: int64(100 + i), Check: "c"}, later.Add(time.Hour), "worker", later); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, _, err := store.CreateTask(ctx, Task{Title: "one more", Thread: "t-2", TriggerID: 999, Check: "c"}, later.Add(time.Hour), "worker", later); !errors.Is(err, ErrTooManyTasks) {
		t.Fatalf("active tasks are capped: %v", err)
	}
}

type fakeBindings map[agent.SessionID]turnBinding

func (b fakeBindings) binding(id agent.SessionID) (turnBinding, bool) {
	bound, ok := b[id]
	return bound, ok
}

// A turn driven by one thread's external mail cannot close or postpone
// another thread's task; the user's own chat turn can.
func TestWorkersOnlyChangeTasksOnTheirThread(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, t.TempDir()+"/assistant.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	clock := newClock()
	task, _, err := store.CreateTask(ctx, Task{Title: "跟进 Alice", Thread: "t-alice", SessionID: "alice", TriggerID: 1, Check: "c"}, clock.Now().Add(time.Hour), "worker", clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	tools := taskTool{store, fakeBindings{"phish": {TriggerID: 2, Thread: "t-phish"}, "chat": {TriggerID: 3, Thread: ChatThread}}, clock.Now}
	untrusted, _ := agent.DefaultRunContext()
	human, _ := agent.ExplicitHumanRunContext(agent.HumanRunConfig{})
	phish := agent.ToolAuthority{SessionID: "phish", OwnerID: Owner, Mode: agent.ModeInteractive, RunContext: untrusted}
	if _, err := (taskCompleteTool{tools}).ExecuteExtensionTool(ctx, phish, taskCompleteInput{TaskID: task.ID, Result: "done"}); err == nil || !strings.Contains(err.Error(), "only change tasks on thread t-phish") {
		t.Fatalf("a foreign thread must not close the task: %v", err)
	}
	if _, err := (taskScheduleTool{tools}).ExecuteExtensionTool(ctx, phish, taskScheduleInput{TaskID: task.ID, InMinutes: 10080}); err == nil {
		t.Fatal("a foreign thread must not postpone the task")
	}
	chat := agent.ToolAuthority{SessionID: "chat", OwnerID: Owner, Mode: agent.ModeInteractive, RunContext: human}
	if _, err := (taskScheduleTool{tools}).ExecuteExtensionTool(ctx, chat, taskScheduleInput{TaskID: task.ID, InMinutes: 5, Note: "用户让我 5 分钟后再看"}); err != nil {
		t.Fatalf("the user's chat turn may change any task: %v", err)
	}
	if err := (taskCreateInput{Title: "x", Check: "y", InMinutes: 0}).Validate(); err == nil {
		t.Fatal("a check needs a delay of at least one minute")
	}
	if err := (taskCreateInput{Title: strings.Repeat("长", maxTaskTitle+1), Check: "y", InMinutes: 5}).Validate(); err == nil {
		t.Fatal("titles are bounded")
	}
}

// concurrencyProbe wraps the scripted model and records how many model calls
// overlap overall and per thread.
type concurrencyProbe struct {
	inner *ScriptedModel
	delay time.Duration

	mu        sync.Mutex
	inflight  int
	max       int
	perThread map[ThreadID]int
	threadMax int
}

func (p *concurrencyProbe) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	turn, _ := readTurn(request.Messages)
	p.mu.Lock()
	p.inflight++
	p.perThread[turn.thread]++
	p.max = max(p.max, p.inflight)
	p.threadMax = max(p.threadMax, p.perThread[turn.thread])
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.inflight--
		p.perThread[turn.thread]--
		p.mu.Unlock()
	}()
	select {
	case <-time.After(p.delay):
	case <-ctx.Done():
		return protocol.ModelReply{}, ctx.Err()
	}
	return p.inner.Complete(ctx, request)
}

// Events on different threads run on parallel workers up to the configured
// capacity; events on one thread run one at a time, in order.
func TestDifferentThreadsRunInParallelAndOneThreadInOrder(t *testing.T) {
	clock := newClock()
	probe := &concurrencyProbe{inner: &ScriptedModel{Now: clock.Now}, delay: 150 * time.Millisecond, perThread: map[ThreadID]int{}}
	app, _ := launchApp(t, t.TempDir(), clock, appOptions{provider: probe, workers: 3})
	ctx := context.Background()
	for _, preset := range []Preset{PresetManager, PresetPhishing, PresetFriend, PresetManagerFollow, PresetManagerFollow} {
		if _, err := app.Connector.DeliverPreset(ctx, preset); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := app.Poller.PollOnce(ctx); err != nil {
		t.Fatal(err)
	}
	app.Router.Wake()
	triggers := settle(t, app.Store)
	if len(triggers) != 5 {
		t.Fatalf("triggers %+v", triggers)
	}
	probe.mu.Lock()
	overall, perThread := probe.max, probe.threadMax
	probe.mu.Unlock()
	if overall != 3 {
		t.Fatalf("four threads on three workers should overlap three turns, saw %d", overall)
	}
	if perThread != 1 {
		t.Fatalf("one thread must never run two turns at once, saw %d", perThread)
	}
}

func TestTaskAndEventEndpoints(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	task, _, err := app.Store.CreateTask(ctx, Task{Title: "跟进", Thread: "t-x", SessionID: "s", TriggerID: 1, Check: "c"}, clock.Now().Add(time.Hour), "worker", clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Handler()
	var body string
	call := func(path, payload string) int {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
		request.Host = "127.0.0.1:8790"
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		body = recorder.Body.String()
		return recorder.Code
	}
	path := "/api/tasks/" + strings.TrimPrefix(TaskRef(task.ID), "task-")
	if code := call(path+"/wake", `{}`); code != http.StatusOK || !strings.Contains(body, "用户点了立即检查") {
		t.Fatalf("wake: %d %s", code, body)
	}
	if code := call(path+"/cancel", `{}`); code != http.StatusOK {
		t.Fatalf("cancel: %d", code)
	}
	if code := call(path+"/cancel", `{}`); code != http.StatusConflict {
		t.Fatalf("second cancel: %d", code)
	}
	if code := call(path+"/finish", `{}`); code != http.StatusNotFound {
		t.Fatalf("unknown action: %d", code)
	}
	if code := call("/api/simulate/event", `{"in_minutes":5}`); code != http.StatusAccepted {
		t.Fatalf("book event: %d", code)
	}
	if code := call("/api/simulate/event", `{"in_minutes":0,"minutes":1}`); code != http.StatusBadRequest {
		t.Fatalf("invalid event: %d", code)
	}
}
