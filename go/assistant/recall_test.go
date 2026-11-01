package assistant

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// promptRecorder keeps the trigger prompts a worker turn was given.
type promptRecorder struct {
	next  agent.Provider
	mu    sync.Mutex
	seen  []string
	tools []protocol.ToolName
}

func (m *promptRecorder) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if request.Purpose == "" || request.Purpose == protocol.PurposeAgentTurn {
		m.mu.Lock()
		m.tools = m.tools[:0]
		for _, tool := range request.Tools {
			m.tools = append(m.tools, tool.Name)
		}
		m.mu.Unlock()
		for _, message := range request.Messages {
			if text, ok := messageText(message); ok && strings.HasPrefix(text, "[trigger ") {
				m.mu.Lock()
				m.seen = append(m.seen, text)
				m.mu.Unlock()
			}
		}
	}
	return m.next.Complete(ctx, request)
}

// lastTools lists the tools of the newest worker turn request.
func (m *promptRecorder) lastTools() []protocol.ToolName {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]protocol.ToolName(nil), m.tools...)
}

func (m *promptRecorder) last(fragment string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.seen) - 1; i >= 0; i-- {
		if strings.Contains(m.seen[i], fragment) {
			return m.seen[i]
		}
	}
	return ""
}

// lookup follows a piece of work from its trigger to the draft, task, thread
// and worker session it touched, and back.
func TestLookupFollowsAPieceOfWorkAcrossRecords(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	ctx := context.Background()
	delivered := deliver(t, app, PresetManager)
	origin := triggerByID(t, settle(t, app.Store), delivered)
	proposals, err := app.Store.ProposalsForTrigger(ctx, origin.ID)
	if err != nil || len(proposals) != 1 {
		t.Fatalf("one draft: %+v %v", proposals, err)
	}
	task := taskOnThread(t, app, origin.Thread)
	thread, session := threadPrefix+string(origin.Thread), sessionPrefix+origin.SessionID
	for _, c := range []struct {
		ref  string
		want []string
	}{
		{TriggerRef(origin.ID), []string{"email.received", "下一跳：", ProposalRef(proposals[0].ID), TaskRef(task.ID), thread, session}},
		{TaskRef(task.ID), []string{task.Title, "要核对：", TriggerRef(origin.ID), thread}},
		{ProposalRef(proposals[0].ID), []string{"send_email", ManagerAddress, TriggerRef(origin.ID)}},
		{thread, []string{TriggerRef(origin.ID), "订阅的 worker", session, TaskRef(task.ID)}},
		{session, []string{TriggerRef(origin.ID), thread, ProposalRef(proposals[0].ID)}},
	} {
		out, err := app.Store.Lookup(ctx, c.ref)
		if err != nil {
			t.Fatalf("%s: %v", c.ref, err)
		}
		for _, want := range c.want {
			if !strings.Contains(out, want) {
				t.Errorf("%s should mention %q:\n%s", c.ref, want, out)
			}
		}
		if strings.Contains(strings.SplitN(out, "下一跳：", 2)[1], c.ref) {
			t.Errorf("%s names itself as a next hop:\n%s", c.ref, out)
		}
	}
	for _, ref := range []string{"trigger-9999", "nonsense", "thread:", "thread:nope", "session:nope", "task-0"} {
		if _, err := app.Store.Lookup(ctx, ref); !errors.Is(err, ErrUnknownRef) {
			t.Errorf("%q: want ErrUnknownRef, got %v", ref, err)
		}
	}

	// The user asks in the channel; the worker answers with the lookup tool.
	if _, err := app.Store.ChatFromUser(ctx, "追溯 "+TriggerRef(origin.ID), clock.Now()); err != nil {
		t.Fatal(err)
	}
	app.Router.Wake()
	settle(t, app.Store)
	if text := lastChannel(t, app); !strings.Contains(text, "下一跳：") || !strings.Contains(text, TaskRef(task.ID)) {
		t.Fatalf("the reply should carry the lookup: %q", text)
	}
	if !ledgerHas(t, app, LedgerTool, "lookup（allowed") {
		t.Fatal("the lookup call should be in the ledger")
	}
	if ToolRisk(ToolLookup) != string(agent.RiskRead) {
		t.Fatal("lookup is a read")
	}
	if err := (lookupInput{}).Validate(); err == nil {
		t.Fatal("an empty ref is rejected")
	}
}

// The nightly routine learns recipients of sent mail, removes duplicate
// memories, records the meeting window, remembers the preferences the user
// stated today and refreshes the profile every worker prompt shows.
func TestNightlyConsolidationBuildsTheProfile(t *testing.T) {
	clock := newClock()
	recorder := &promptRecorder{next: &ScriptedModel{Now: clock.Now}}
	app, _ := launchApp(t, t.TempDir(), clock, appOptions{provider: recorder})
	ctx := context.Background()
	routines, err := app.Store.Routines(ctx)
	if err != nil || len(routines) != 2 {
		t.Fatalf("the nightly routine is seeded: %+v %v", routines, err)
	}
	if profile, _ := app.Store.Profile(ctx); len(profile) != 3 || profile[0].Value != "Alice <"+ManagerAddress+">" {
		t.Fatalf("start-up builds the profile lines that need no calendar: %+v", profile)
	}
	if _, err := app.Connector.SendMail(ctx, OutgoingMail{Key: "manual-1", Thread: "t-partner", To: "Carol@Partner.example", Subject: "合同", Body: "附件是合同。"}); err != nil {
		t.Fatal(err)
	}
	for _, input := range []memory.Input{
		{Name: "copy-a", Type: memory.Feedback, Description: "开会偏好（重复）", Body: "日程冲突的邀约婉拒并建议改期；newsletter 不用提醒。", Origin: memory.AutoExtracted},
		{Name: "copy-b", Type: memory.Feedback, Description: "开会偏好（标点不同）", Body: "日程冲突的邀约 婉拒并建议改期, Newsletter 不用提醒", Origin: memory.AutoExtracted},
		{Name: "a-chat-note", Type: memory.User, Description: "用户在信道里说的", Body: "日程冲突的邀约婉拒并建议改期，newsletter 不用提醒", Origin: memory.Explicit},
	} {
		if _, err := app.Memory.Write(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	for _, line := range []string{"以后周五下午尽量不安排会", "记住 我喜欢早上处理邮件"} {
		if _, err := app.Store.ChatFromUser(ctx, line, clock.Now()); err != nil {
			t.Fatal(err)
		}
		app.Router.Wake()
		settle(t, app.Store)
	}
	if !channelHas(t, app, "今晚 23:30 的夜间整理会把它记下来") {
		t.Fatal("a stated preference is acknowledged")
	}

	run := func() Trigger {
		t.Helper()
		clock.Set(clock.Now().Add(time.Minute))
		id, err := app.Scheduler.RunNow(ctx, NightlyRoutine.ID)
		if err != nil {
			t.Fatal(err)
		}
		return triggerByID(t, settle(t, app.Store), id)
	}
	nightly := run()
	if nightly.Status != TriggerQuiet || !strings.Contains(nightly.Decision, "记下 1 条偏好") {
		t.Fatalf("nightly turn: %+v", nightly)
	}
	if !ledgerHas(t, app, LedgerMemory, "学到联系人 1 个，去掉重复记忆 3 条") {
		t.Fatal("the consolidation should be in the ledger")
	}
	if prompt := recorder.last("守护进程已经完成的整理"); !strings.Contains(prompt, "carol@partner.example") || !strings.Contains(prompt, "以后周五下午尽量不安排会") || !strings.Contains(prompt, "<chat>") {
		t.Fatalf("the nightly prompt carries the consolidation and today's chat:\n%s", prompt)
	}
	if known, _ := app.Store.KnownContact(ctx, "carol@partner.example"); !known {
		t.Fatal("a recipient of sent mail becomes a contact")
	}
	records, err := app.Memory.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	copies, preference := 0, false
	for _, record := range records {
		if dedupeKey(record.Body) == dedupeKey("日程冲突的邀约婉拒并建议改期newsletter不用提醒") {
			copies++
			if record.Origin != memory.Explicit || record.Name != "meeting-preference" {
				t.Fatalf("the explicit memory is kept: %+v", record)
			}
		}
		if strings.TrimSpace(record.Body) == "以后周五下午尽量不安排会" && record.Type == memory.Feedback {
			preference = true
		}
	}
	if copies != 1 || !preference {
		t.Fatalf("duplicates removed and the preference remembered: %+v", records)
	}
	profile, err := app.Store.Profile(ctx)
	if err != nil || len(profile) != 4 {
		t.Fatalf("profile: %+v %v", profile, err)
	}
	if profile[2].Key != ProfileMeetings || !strings.HasPrefix(profile[2].Value, "通常在 ") || !strings.Contains(profile[3].Value, "以后周五下午尽量不安排会") {
		t.Fatalf("the profile has the meeting window and the new preference: %+v", profile)
	}

	// A re-run converges, and the next worker turn sees the profile.
	again := run()
	if again.Status != TriggerQuiet || !ledgerHas(t, app, LedgerMemory, "学到联系人 0 个，去掉重复记忆 0 条") {
		t.Fatalf("re-run: %+v", again)
	}
	if after, _ := app.Memory.List(ctx); len(after) != len(records) {
		t.Fatalf("a re-run remembers nothing twice: %d → %d", len(records), len(after))
	}
	if _, err := app.Store.ChatFromUser(ctx, "今天日程", clock.Now()); err != nil {
		t.Fatal(err)
	}
	app.Router.Wake()
	settle(t, app.Store)
	if prompt := recorder.last("今天日程"); !strings.Contains(prompt, "用户画像") || !strings.Contains(prompt, "会议时间：通常在") || !strings.Contains(prompt, "经理：Alice") {
		t.Fatalf("worker prompts carry the profile:\n%s", prompt)
	}
}
