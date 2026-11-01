package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// plainModel answers every turn with fixed text and no tool call, like a
// model that ignores the report contract.
type plainModel struct{ text string }

func (m plainModel) Complete(context.Context, protocol.ModelRequest) (protocol.ModelReply, error) {
	return protocol.ModelReply{ID: "msg_plain", Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: ScriptedModelID,
		Content: []protocol.Block{protocol.NewTextBlock(m.text)}, StopReason: protocol.StopEndTurn}, nil
}

func ledgerHas(t *testing.T, app *App, kind LedgerKind, fragment string) bool {
	t.Helper()
	for _, entry := range ledgerKinds(t, app, kind) {
		if strings.Contains(entry.Detail, fragment) {
			return true
		}
	}
	return false
}

func TestBatchClaimKeepsOneThreadAndOneTrustClass(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, filepath.Join(t.TempDir(), "assistant.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now()
	enqueue := func(kind TriggerKind, thread ThreadID) int64 {
		id, err := enqueueTrigger(ctx, store.db, Trigger{Kind: kind, Thread: thread, Ref: "r", Summary: string(kind)}, now)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	mailA := enqueue(TriggerEmail, "t-a")
	wakeA := enqueue(TriggerTaskWakeup, "t-a")
	mailB := enqueue(TriggerEmail, "t-b")
	chatWake := enqueue(TriggerTaskWakeup, ChatThread)
	first, err := store.ChatFromUser(ctx, "第一条", now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.ChatFromUser(ctx, "第二条", now)
	if err != nil {
		t.Fatal(err)
	}
	ids := func(batch []Trigger) []int64 {
		out := []int64{}
		for _, trigger := range batch {
			out = append(out, trigger.ID)
		}
		return out
	}
	// Chat goes first and batches only with chat: the task check queued on
	// the chat thread runs untrusted, so it waits for its own turn.
	if batch, err := store.ClaimBatch(ctx, now, false); err != nil || fmt.Sprint(ids(batch)) != fmt.Sprint([]int64{first, second}) {
		t.Fatalf("chat batch %v %v", ids(batch), err)
	}
	if batch, _ := store.ClaimBatch(ctx, now, false); fmt.Sprint(ids(batch)) != fmt.Sprint([]int64{mailA, wakeA}) {
		t.Fatalf("one thread's mail and task check run together: %v", ids(batch))
	}
	if batch, _ := store.ClaimBatch(ctx, now, false); fmt.Sprint(ids(batch)) != fmt.Sprint([]int64{mailB}) {
		t.Fatalf("another thread runs alone: %v", ids(batch))
	}
	if batch, _ := store.ClaimBatch(ctx, now, false); len(batch) != 0 {
		t.Fatalf("the chat thread is still running, its task check must wait: %v", ids(batch))
	}
	for _, id := range []int64{first, second} {
		if err := store.FinishTrigger(ctx, id, TriggerDone, "", "", now); err != nil {
			t.Fatal(err)
		}
	}
	if batch, _ := store.ClaimBatch(ctx, now, false); fmt.Sprint(ids(batch)) != fmt.Sprint([]int64{chatWake}) || batch[0].Attempts != 1 {
		t.Fatalf("chat-thread task check: %+v", batch)
	}
}

func TestQueuedMailOfOneThreadIsHandledInOneTurn(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	before, _ := app.Store.Channel(ctx, 200)
	last := deliver(t, app, PresetManagerBurst)
	triggers := settle(t, app.Store)
	follower := triggerByID(t, triggers, last)
	lead := triggerByID(t, triggers, last-1)
	if lead.Status != TriggerDone || follower.Status != TriggerDone || lead.SessionID == "" || follower.SessionID != lead.SessionID {
		t.Fatalf("both mails settle in one turn: lead %+v follower %+v", lead, follower)
	}
	if !strings.Contains(follower.Decision, fmt.Sprintf("并入触发器 #%d", lead.ID)) || !strings.Contains(lead.Decision, "合并了 2 个触发器") {
		t.Fatalf("decisions should name the batch: %q / %q", lead.Decision, follower.Decision)
	}
	proposals, err := app.Store.ProposalsForTrigger(ctx, lead.ID)
	if err != nil || len(proposals) != 1 || !strings.Contains(proposals[0].Body, "报价单") || !strings.Contains(proposals[0].Body, "客户方案") {
		t.Fatalf("one reply covers both mails: %+v %v", proposals, err)
	}
	if others, _ := app.Store.ProposalsForTrigger(ctx, follower.ID); len(others) != 0 {
		t.Fatalf("the follower drafts nothing of its own: %+v", others)
	}
	after, _ := app.Store.Channel(ctx, 200)
	replies := 0
	for _, message := range after[len(before):] {
		if message.Role == RoleAssistant && message.ProposalID == nil {
			replies++
			if !strings.Contains(message.Text, "连发 2 封") {
				t.Fatalf("the report should cover the batch: %q", message.Text)
			}
		}
	}
	if replies != 1 {
		t.Fatalf("one turn posts one report, got %d", replies)
	}
	if !ledgerHas(t, app, LedgerRoute, "合并同一 thread 积压的 2 个触发器") {
		t.Fatal("the route ledger row should record the merge")
	}
	if tasks, _ := app.Store.ActiveTasks(ctx, lead.Thread); len(tasks) != 1 {
		t.Fatalf("one follow-up task for the batch: %+v", tasks)
	}
}

func TestWorkerQuestionWaitsForTheUserAndTheAnswerReturnsToItsThread(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	asked := deliver(t, app, PresetQuestion)
	trigger := triggerByID(t, settle(t, app.Store), asked)
	if trigger.Status != TriggerDone || !strings.Contains(trigger.Decision, "向用户提问") {
		t.Fatalf("question trigger %+v", trigger)
	}
	if !channelHas(t, app, "❓ Dave 问") {
		t.Fatal("the question should reach the channel")
	}
	refs := attentionRefs(t, app)
	var ref string
	for candidate, kind := range refs {
		if kind == AttentionQuestion {
			ref = candidate
		}
	}
	id, ok := questionID(ref)
	if !ok {
		t.Fatalf("the question should wait on the attention list: %v", refs)
	}
	if proposals, _ := app.Store.ProposalsForTrigger(ctx, asked); len(proposals) != 0 {
		t.Fatalf("nothing is drafted before the user decides: %+v", proposals)
	}

	server := httptest.NewServer(app.Handler())
	defer server.Close()
	post := func(path, body string) int {
		request, _ := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		return response.StatusCode
	}
	if code := post(fmt.Sprintf("/api/questions/%d/answer", id), `{"text":""}`); code != http.StatusBadRequest {
		t.Fatalf("empty answer: %d", code)
	}
	if code := post(fmt.Sprintf("/api/questions/%d/answer", id), `{"text":"周三上午"}`); code != http.StatusAccepted {
		t.Fatalf("answer: %d", code)
	}
	if code := post(fmt.Sprintf("/api/questions/%d/answer", id), `{"text":"周二"}`); code != http.StatusConflict {
		t.Fatalf("a question is answered once: %d", code)
	}
	triggers := settle(t, app.Store)
	var answer Trigger
	for _, candidate := range triggers {
		if candidate.Kind == TriggerAnswer {
			answer = candidate
		}
	}
	if answer.Thread != trigger.Thread || answer.SessionID != trigger.SessionID || answer.Status != TriggerDone {
		t.Fatalf("the answer goes back to the asking worker: asked %+v answer %+v", trigger, answer)
	}
	drafts, err := app.Store.ProposalsForTrigger(ctx, answer.ID)
	if err != nil || len(drafts) != 1 || drafts[0].To != ColleagueAddress || !strings.Contains(drafts[0].Body, "周三上午") {
		t.Fatalf("the answer becomes a reply draft: %+v %v", drafts, err)
	}
	if _, waiting := attentionRefs(t, app)[ref]; waiting {
		t.Fatal("an answered question leaves the attention list")
	}
	question, _, _ := app.Store.Question(ctx, id)
	if question.Status != QuestionAnswered || question.Answer != "周三上午" || question.AnsweredAt == nil {
		t.Fatalf("question %+v", question)
	}
}

func TestQuestionCanBeDismissedAndStillAnswered(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	deliver(t, app, PresetQuestion)
	settle(t, app.Store)
	question, ok, err := app.Store.Question(ctx, 1)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := app.Store.Dismiss(ctx, QuestionRef(question.ID), "test", clock.Now()); err != nil {
		t.Fatal(err)
	}
	if _, waiting := attentionRefs(t, app)[QuestionRef(question.ID)]; waiting {
		t.Fatal("a dismissed question leaves the list")
	}
	if _, _, err := app.Store.AnswerQuestion(ctx, question.ID, "周二下午", "test", clock.Now()); err != nil {
		t.Fatal(err)
	}
	settle(t, app.Store)
	if err := app.Store.Dismiss(ctx, QuestionRef(question.ID), "test", clock.Now()); err == nil {
		t.Fatal("an answered question is no longer dismissable")
	}
}

func TestTurnWithoutAReportIsDeliveredAsNotify(t *testing.T) {
	clock := newClock()
	app, _ := launchApp(t, t.TempDir(), clock, appOptions{provider: plainModel{"这封邮件需要你看一下"}})
	id := deliver(t, app, PresetFriend)
	if got := triggerByID(t, settle(t, app.Store), id); got.Status != TriggerDone {
		t.Fatalf("trigger %+v", got)
	}
	if !channelHas(t, app, "这封邮件需要你看一下") {
		t.Fatal("the final text is delivered when the worker does not report")
	}
	if !ledgerHas(t, app, LedgerReport, "没有调用 report") {
		t.Fatal("the ledger should note the missing report")
	}
}

func TestReportedQuietTurnKeepsItsFinalTextInTheLedger(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	id := deliver(t, app, PresetFriend)
	if got := triggerByID(t, settle(t, app.Store), id); got.Status != TriggerQuiet || !strings.Contains(got.Decision, "不打扰用户") {
		t.Fatalf("trigger %+v", got)
	}
	if !ledgerHas(t, app, LedgerReport, "worker 汇报 quiet") {
		t.Fatal("the report should reach the ledger")
	}
}

func TestReportInputValidation(t *testing.T) {
	cases := []struct {
		input reportInput
		ok    bool
	}{
		{reportInput{Outcome: ReportNotify, Message: "结论"}, true},
		{reportInput{Outcome: ReportNotify}, false},
		{reportInput{Outcome: ReportAsk, Question: "选哪天？"}, true},
		{reportInput{Outcome: ReportAsk, Message: "背景"}, false},
		{reportInput{Outcome: ReportQuiet, Reason: "闲聊"}, true},
		{reportInput{Outcome: ReportQuiet}, false},
		{reportInput{Outcome: "shout", Message: "x"}, false},
		{reportInput{Outcome: ReportNotify, Message: strings.Repeat("长", maxReportMessage+1)}, false},
	}
	for _, c := range cases {
		if err := c.input.Validate(); (err == nil) != c.ok {
			t.Errorf("%+v: err=%v, want ok=%v", c.input, err, c.ok)
		}
	}
	data, _ := json.Marshal(Report{Outcome: ReportQuiet, Reason: "r"})
	if strings.Contains(string(data), "question") {
		t.Fatalf("empty fields are omitted: %s", data)
	}
}
