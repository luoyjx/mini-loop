package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/luoyjx/mini-loop/go/protocol"
)

// ScriptedModel is a deterministic offline stand-in for an LLM so the demo
// runs without credentials. It only chooses tool calls and wording from the
// prompt header and the tool results already in the transcript; every effect
// still goes through the session's real tool gate. A background turn ends
// with a report call, as the worker contract asks.
type ScriptedModel struct {
	calls atomic.Uint64
	Now   func() time.Time
}

const ScriptedModelID = "scripted-offline"

var headerPattern = regexp.MustCompile(`\[trigger id=(\d+) kind=(\S+) thread=(\S+) ref=(\S*)\]`)

type scriptedTurn struct {
	kind    TriggerKind
	thread  ThreadID
	ref     string
	text    string
	open    []int64
	events  []scriptedEvent
	results map[protocol.ToolName][]string
}

// scriptedEvent is one event of a batched prompt.
type scriptedEvent struct {
	kind TriggerKind
	ref  string
}

var (
	openTaskPattern = regexp.MustCompile(`(?m)^- 任务 #(\d+) `)
	eventPattern    = regexp.MustCompile(`\[event id=\d+ kind=(\S+) ref=(\S*)\]`)
)

// refs lists the refs of one kind in the batch, oldest first; a single-event
// turn has only its own.
func (turn scriptedTurn) refs(kind TriggerKind) []string {
	if len(turn.events) == 0 {
		return []string{turn.ref}
	}
	refs := []string{}
	for _, event := range turn.events {
		if event.kind == kind {
			refs = append(refs, event.ref)
		}
	}
	return refs
}

type plannedCall struct {
	name  protocol.ToolName
	input string
}

// plan is the next step: tool calls, or the final text. A background turn's
// final text becomes its report: notify by default, quiet when quiet is set,
// ask with the question when ask is set.
type plan struct {
	calls []plannedCall
	final string
	quiet bool
	ask   string
}

func (m *ScriptedModel) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	if err := ctx.Err(); err != nil {
		return protocol.ModelReply{}, err
	}
	n := m.calls.Add(1)
	reply := protocol.ModelReply{ID: fmt.Sprintf("msg_scripted_%d", n), Type: protocol.ReplyMessage, Role: protocol.RoleAssistant, Model: ScriptedModelID}
	if request.Purpose != "" && request.Purpose != protocol.PurposeAgentTurn {
		reply.Content, reply.StopReason = []protocol.Block{protocol.NewTextBlock("（离线脚本模型：此请求只返回占位摘要）")}, protocol.StopEndTurn
		return reply, nil
	}
	turn, ok := readTurn(request.Messages)
	var next plan
	if !ok {
		next = plan{final: "（离线脚本模型没有找到触发器头，未执行任何操作）"}
	} else {
		next = m.plan(turn)
	}
	if len(next.calls) == 0 {
		reply.Content, reply.StopReason = []protocol.Block{protocol.NewTextBlock(next.final)}, protocol.StopEndTurn
		return reply, nil
	}
	reply.Content = make([]protocol.Block, 0, len(next.calls))
	for i, call := range next.calls {
		input, err := protocol.DecodeModelToolInput(call.name, []byte(call.input))
		if err != nil {
			return protocol.ModelReply{}, err
		}
		reply.Content = append(reply.Content, protocol.NewToolUse(fmt.Sprintf("toolu_scripted_%d_%d", n, i), input))
	}
	reply.StopReason = protocol.StopToolUse
	return reply, nil
}

func messageText(message protocol.Message) (string, bool) {
	if plain, ok := message.Content.Plain(); ok {
		return plain, true
	}
	blocks, _ := message.Content.Blocks()
	var b strings.Builder
	for _, block := range blocks {
		if block.Kind() == protocol.BlockToolResult {
			return "", false
		}
		if text, ok := block.Text(); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String(), b.Len() > 0
}

// readTurn finds the newest trigger prompt and the tool results after it.
func readTurn(messages []protocol.Message) (scriptedTurn, bool) {
	start := -1
	var turn scriptedTurn
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != protocol.RoleUser {
			continue
		}
		text, ok := messageText(messages[i])
		if !ok {
			continue
		}
		match := headerPattern.FindStringSubmatchIndex(text)
		if match == nil {
			continue
		}
		turn.kind, turn.thread, turn.ref = TriggerKind(text[match[4]:match[5]]), ThreadID(text[match[6]:match[7]]), text[match[8]:match[9]]
		turn.text = strings.TrimSpace(text[match[1]:])
		for _, open := range openTaskPattern.FindAllStringSubmatch(turn.text, -1) {
			var id int64
			if _, err := fmt.Sscan(open[1], &id); err == nil {
				turn.open = append(turn.open, id)
			}
		}
		for _, event := range eventPattern.FindAllStringSubmatch(turn.text, -1) {
			turn.events = append(turn.events, scriptedEvent{kind: TriggerKind(event[1]), ref: event[2]})
		}
		start = i
		break
	}
	if start < 0 {
		return scriptedTurn{}, false
	}
	names := map[string]protocol.ToolName{}
	turn.results = map[protocol.ToolName][]string{}
	for _, message := range messages[start+1:] {
		blocks, _ := message.Content.Blocks()
		for _, block := range blocks {
			if use, ok := block.ToolUse(); ok {
				names[use.ID] = use.Name
			}
			if result, ok := block.ToolResult(); ok {
				name := names[result.ToolUseID]
				turn.results[name] = append(turn.results[name], result.Content)
			}
		}
	}
	return turn, true
}

func (turn scriptedTurn) last(name protocol.ToolName) (string, bool) {
	values := turn.results[name]
	if len(values) == 0 {
		return "", false
	}
	return values[len(values)-1], true
}

func jsonInput(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func (m *ScriptedModel) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *ScriptedModel) plan(turn scriptedTurn) plan {
	next := m.planKind(turn)
	if turn.kind == TriggerChat || len(next.calls) > 0 {
		return next
	}
	return reportPlan(turn, next)
}

// reportPlan turns a background turn's conclusion into one report call, then
// ends the turn; the final text then only reaches the ledger.
func reportPlan(turn scriptedTurn, next plan) plan {
	if _, done := turn.last(ToolReport); done {
		return plan{final: next.final}
	}
	input := reportInput{Outcome: ReportNotify, Message: truncateRunes(next.final, maxReportMessage)}
	switch {
	case next.ask != "":
		input = reportInput{Outcome: ReportAsk, Message: truncateRunes(next.final, maxReportMessage), Question: truncateRunes(next.ask, maxReportQuestion)}
	case next.quiet:
		input = reportInput{Outcome: ReportQuiet, Reason: truncateRunes(next.final, maxReportReason)}
	}
	return plan{calls: []plannedCall{{ToolReport, jsonInput(input)}}}
}

func (m *ScriptedModel) planKind(turn scriptedTurn) plan {
	switch turn.kind {
	case TriggerEmail:
		return planEmail(turn)
	case TriggerInvite:
		return planInvite(turn)
	case TriggerRoutine:
		return m.planBrief(turn)
	case TriggerChat:
		return m.planChat(turn)
	case TriggerTaskWakeup:
		return m.planTaskWakeup(turn)
	case TriggerUpcoming:
		return m.planUpcoming(turn)
	case TriggerAnswer:
		return planAnswer(turn)
	}
	return plan{final: "（离线脚本模型不认识这个触发器）"}
}

var suspiciousWords = []string{"转发", "forward", "密码", "password", "汇款", "礼品卡", "gift card", "验证码"}

// planEmail reads every mail of the batch and answers the newest: a manager's
// mail gets one reply draft and a follow-up task, a suspicious mail a warning,
// a colleague's either-or question goes to the user, and anything else stays
// quiet.
func planEmail(turn scriptedTurn) plan {
	refs := turn.refs(TriggerEmail)
	reads := turn.results[ToolMailRead]
	recalled, haveRecall := turn.last(protocol.ToolRecall)
	if len(reads) < len(refs) || !haveRecall {
		calls := make([]plannedCall, 0, len(refs)+1)
		for _, ref := range refs {
			calls = append(calls, plannedCall{ToolMailRead, jsonInput(mailReadInput{ID: ref})})
		}
		return plan{calls: append(calls, plannedCall{protocol.ToolRecall, `{"query":"经理"}`})}
	}
	mails := make([]mailDetail, 0, len(refs))
	for _, read := range reads[len(reads)-len(refs):] {
		var mail mailDetail
		if err := json.Unmarshal([]byte(read), &mail); err != nil {
			return plan{final: "读取邮件失败：" + truncateRunes(read, 80)}
		}
		mails = append(mails, mail)
	}
	mail := mails[len(mails)-1]
	subjects := make([]string, 0, len(mails))
	for _, each := range mails {
		subjects = append(subjects, "「"+each.Subject+"」")
	}
	manager := strings.Contains(strings.ToLower(recalled), strings.ToLower(mail.From))
	content := strings.ToLower(mail.Subject + " " + mail.Body)
	if !manager {
		for _, word := range suspiciousWords {
			if strings.Contains(content, strings.ToLower(word)) {
				return plan{final: fmt.Sprintf("⚠️ 可疑邮件：%s <%s> 要求「%s」，含「%s」类指令。我没有执行，也没起草回复，建议忽略或上报。", mail.FromName, mail.From, mail.Subject, word)}
			}
		}
		if strings.Contains(mail.Body, "还是") && strings.ContainsAny(mail.Body, "？?") {
			return plan{final: fmt.Sprintf("%s 来信「%s」，要你拿主意。", mail.FromName, mail.Subject), ask: fmt.Sprintf("%s 问：%s 你怎么定？", mail.FromName, truncateRunes(mail.Body, 80))}
		}
		return plan{quiet: true, final: fmt.Sprintf("普通来信（%s：%s），不打扰用户，记入晨报。", mail.FromName, strings.Join(subjects, ""))}
	}
	// The manager expects a same-day answer: draft one reply to the newest
	// mail and track it as a task (or extend the thread's existing task)
	// until it goes out.
	drafted, haveDraft := turn.last(ToolProposeEmail)
	tracked, haveTask := turn.last(ToolTaskCreate)
	if !haveTask {
		tracked, haveTask = turn.last(ToolTaskSchedule)
	}
	if haveDraft && haveTask {
		if len(mails) > 1 {
			return plan{final: fmt.Sprintf("%s（你的经理）连发 %d 封：%s。我合并起草了一封回复，%s%s", mail.FromName, len(mails), strings.Join(subjects, ""), proposalNumber(drafted), taskSentence(tracked))}
		}
		return plan{final: fmt.Sprintf("%s（你的经理）来信：「%s」。我起草了回复，%s%s", mail.FromName, mail.Subject, proposalNumber(drafted), taskSentence(tracked))}
	}
	var calls []plannedCall
	if !haveDraft {
		subject := mail.Subject
		if !strings.HasPrefix(strings.ToLower(subject), "re:") {
			subject = "Re: " + subject
		}
		about := "「" + mail.Subject + "」"
		if len(mails) > 1 {
			about = strings.Join(subjects, "和")
		}
		body := fmt.Sprintf("%s 你好，\n\n收到，关于%s我今天下班前给你答复。\n\n（由助理起草，经我确认后发出）", mail.FromName, about)
		calls = append(calls, plannedCall{ToolProposeEmail, jsonInput(proposeEmailInput{To: mail.From, Subject: subject, Body: body, Reason: "经理来信，需要当天回复"})})
	}
	if !haveTask {
		if len(turn.open) > 0 {
			calls = append(calls, plannedCall{ToolTaskSchedule, jsonInput(taskScheduleInput{TaskID: turn.open[0], InMinutes: 60, Note: truncateRunes(mail.FromName+" 又来信："+mail.Subject+"；新的回复草稿待批准", maxTaskText-1)})})
		} else {
			calls = append(calls, plannedCall{ToolTaskCreate, jsonInput(taskCreateInput{Title: truncateRunes("跟进：回复 "+mail.FromName+"「"+mail.Subject+"」", maxTaskTitle-1), Check: "已发送邮件里有没有这个 thread 发给 " + mail.From + " 的回复", InMinutes: 60, Note: "回复草稿已起草，等用户批准"})})
		}
	}
	return plan{calls: calls}
}

var answerPattern = regexp.MustCompile(`(?m)^回答：(.*)$`)

// planAnswer drafts a reply to the thread's newest inbox mail with the user's
// answer.
func planAnswer(turn scriptedTurn) plan {
	inbox, haveInbox := turn.last(ToolMailList)
	if !haveInbox {
		return plan{calls: []plannedCall{{ToolMailList, `{"folder":"inbox","limit":30}`}}}
	}
	answer := ""
	if match := answerPattern.FindStringSubmatch(turn.text); match != nil {
		answer = strings.TrimSpace(match[1])
	}
	var mails []mailSummary
	_ = json.Unmarshal([]byte(inbox), &mails)
	var original *mailSummary
	for i := range mails {
		if mails[i].Thread == turn.thread {
			original = &mails[i]
			break
		}
	}
	if original == nil {
		return plan{final: "收到你的回答「" + answer + "」。这个 thread 没有可回复的邮件，我先记下。"}
	}
	if drafted, done := turn.last(ToolProposeEmail); done {
		return plan{final: fmt.Sprintf("按你的回答「%s」起草了给 %s 的回复，%s", answer, original.FromName, proposalNumber(drafted))}
	}
	subject := original.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	body := fmt.Sprintf("%s 你好，\n\n关于「%s」：%s。\n\n（由助理起草，经我确认后发出）", original.FromName, original.Subject, answer)
	return plan{calls: []plannedCall{{ToolProposeEmail, jsonInput(proposeEmailInput{To: original.From, Subject: subject, Body: body, Reason: "按你对提问的回答起草"})}}}
}

var taskNumberPattern = regexp.MustCompile(`任务 #(\d+)`)

func taskSentence(receipt string) string {
	match := taskNumberPattern.FindStringSubmatch(receipt)
	switch {
	case match == nil:
		return ""
	case strings.HasPrefix(receipt, "已创建") || strings.Contains(receipt, "已存在"):
		return "建了任务 #" + match[1] + " 跟进，1 小时后检查回复有没有发出。"
	default:
		return "任务 #" + match[1] + " 继续跟进，1 小时后再查。"
	}
}

func proposalNumber(receipt string) string {
	if match := regexp.MustCompile(`#(\d+)`).FindStringSubmatch(receipt); match != nil {
		return "提案 #" + match[1] + " 等你批准后发出。"
	}
	return "等你批准后发出。"
}

func planInvite(turn scriptedTurn) plan {
	read, haveMail := turn.last(ToolMailRead)
	listed, haveCalendar := turn.last(ToolCalendarList)
	if !haveMail || !haveCalendar {
		return plan{calls: []plannedCall{{ToolMailRead, jsonInput(mailReadInput{ID: turn.ref})}, {ToolCalendarList, `{"days":7}`}}}
	}
	var mail mailDetail
	var events []eventView
	if json.Unmarshal([]byte(read), &mail) != nil || json.Unmarshal([]byte(listed), &events) != nil {
		return plan{final: "读取邀约或日程失败，没有起草回复。"}
	}
	var target *eventView
	for i := range events {
		if events[i].ID == mail.EventID {
			target = &events[i]
		}
	}
	if target == nil {
		return plan{final: fmt.Sprintf("%s 发来邀约「%s」，但日历里找不到对应日程，请你手动看一下。", mail.FromName, mail.Subject)}
	}
	start, _ := time.Parse(time.RFC3339, target.Start)
	end, _ := time.Parse(time.RFC3339, target.End)
	var conflict *eventView
	for i := range events {
		event := events[i]
		if event.ID == target.ID || event.Status != EventConfirmed {
			continue
		}
		otherStart, _ := time.Parse(time.RFC3339, event.Start)
		otherEnd, _ := time.Parse(time.RFC3339, event.End)
		if otherStart.Before(end) && start.Before(otherEnd) {
			conflict = &events[i]
			break
		}
	}
	when := start.Local().Format("01-02 15:04")
	if receipt, done := turn.last(ToolProposeInvite); done {
		if conflict != nil {
			return plan{final: fmt.Sprintf("%s 邀请你参加「%s」（%s），与「%s」冲突。我起草了婉拒并建议改期，%s", mail.FromName, target.Title, when, conflict.Title, proposalNumber(receipt))}
		}
		return plan{final: fmt.Sprintf("%s 邀请你参加「%s」（%s），日程空闲。我起草了接受回复，%s", mail.FromName, target.Title, when, proposalNumber(receipt))}
	}
	input := proposeInviteInput{EventID: target.ID, Response: InviteAccept, Note: "好的，我会准时参加。", Reason: "日程空闲"}
	if conflict != nil {
		otherEnd, _ := time.Parse(time.RFC3339, conflict.End)
		input.Response = InviteDecline
		input.Note = fmt.Sprintf("抱歉，这个时间我已有「%s」。能否改到 %s 之后？", conflict.Title, otherEnd.Local().Format("15:04"))
		input.Reason = "与「" + conflict.Title + "」冲突"
	}
	return plan{calls: []plannedCall{{ToolProposeInvite, jsonInput(input)}}}
}

func (m *ScriptedModel) planBrief(turn scriptedTurn) plan {
	listed, haveCalendar := turn.last(ToolCalendarList)
	inbox, haveInbox := turn.last(ToolMailList)
	recalled, haveRecall := turn.last(protocol.ToolRecall)
	open, haveTasks := turn.last(ToolTaskList)
	if !haveCalendar || !haveInbox || !haveRecall || !haveTasks {
		return plan{calls: []plannedCall{{ToolCalendarList, `{"days":1}`}, {ToolMailList, `{"folder":"inbox","limit":30}`}, {protocol.ToolRecall, `{"query":"经理"}`}, {ToolTaskList, `{}`}}}
	}
	var events []eventView
	var mails []mailSummary
	var tasks []taskView
	_ = json.Unmarshal([]byte(listed), &events)
	_ = json.Unmarshal([]byte(inbox), &mails)
	_ = json.Unmarshal([]byte(open), &tasks)
	now := m.now()
	var b strings.Builder
	b.WriteString("早上好！")
	agenda := []string{}
	invited := 0
	for _, event := range events {
		start, _ := time.Parse(time.RFC3339, event.Start)
		switch event.Status {
		case EventConfirmed:
			agenda = append(agenda, start.Local().Format("15:04")+" "+event.Title)
		case EventInvited:
			invited++
		}
	}
	if len(agenda) == 0 {
		b.WriteString("今天没有日程")
	} else {
		fmt.Fprintf(&b, "今天 %d 个日程：%s", len(agenda), strings.Join(agenda, "；"))
	}
	if invited > 0 {
		fmt.Fprintf(&b, "（另有 %d 个邀约待回复）", invited)
	}
	recent, bulk := 0, 0
	fromManager := []string{}
	for _, mail := range mails {
		received, _ := time.Parse(time.RFC3339, mail.ReceivedAt)
		if now.Sub(received) > 24*time.Hour {
			continue
		}
		recent++
		if mail.Category == CategoryNewsletter {
			bulk++
		}
		if strings.Contains(strings.ToLower(recalled), strings.ToLower(mail.From)) {
			fromManager = append(fromManager, "「"+mail.Subject+"」")
		}
	}
	fmt.Fprintf(&b, "。近 24 小时 %d 封邮件", recent)
	if len(fromManager) > 0 {
		fmt.Fprintf(&b, "，经理来信 %d 封：%s", len(fromManager), strings.Join(fromManager, "、"))
	}
	if bulk > 0 {
		fmt.Fprintf(&b, "，newsletter %d 封已静默归档", bulk)
	}
	if len(tasks) > 0 {
		fmt.Fprintf(&b, "；进行中任务 %d 个", len(tasks))
	}
	b.WriteString("。")
	return plan{final: b.String()}
}

var taskReasonPattern = regexp.MustCompile(`任务到点检查：([^\n]*)`)

// planTaskWakeup checks a task by its own convention: a reminder fires once;
// a reply follow-up is done when the sent folder holds mail on the task's
// thread since the task began, closed when the user denied the draft, and
// otherwise checked again in 30 minutes.
func (m *ScriptedModel) planTaskWakeup(turn scriptedTurn) plan {
	listed, haveList := turn.last(ToolTaskList)
	sent, haveSent := turn.last(ToolMailList)
	if !haveList || !haveSent {
		return plan{calls: []plannedCall{{ToolTaskList, `{}`}, {ToolMailList, `{"folder":"sent","limit":30}`}}}
	}
	id, _ := taskID(turn.ref)
	var tasks []taskView
	var mails []mailSummary
	_ = json.Unmarshal([]byte(listed), &tasks)
	_ = json.Unmarshal([]byte(sent), &mails)
	var task *taskView
	for i := range tasks {
		if tasks[i].ID == id {
			task = &tasks[i]
		}
	}
	if task == nil {
		return plan{quiet: true, final: fmt.Sprintf("任务 #%d 已不在进行中。", id)}
	}
	reason := ""
	if match := taskReasonPattern.FindStringSubmatch(turn.text); match != nil {
		reason = match[1]
	}
	created, _ := time.Parse(time.RFC3339, task.CreatedAt)
	var reply *mailSummary
	for i := range mails {
		received, _ := time.Parse(time.RFC3339, mails[i].ReceivedAt)
		if mails[i].Thread == turn.thread && !received.Before(created) {
			reply = &mails[i]
			break
		}
	}
	var call plannedCall
	var final string
	switch {
	case strings.HasPrefix(task.Title, "提醒"):
		what := strings.TrimLeft(strings.TrimPrefix(task.Title, "提醒"), "：: ")
		ago := int(m.now().Sub(created).Round(time.Minute) / time.Minute)
		call = plannedCall{ToolTaskComplete, jsonInput(taskCompleteInput{TaskID: id, Result: "已在信道提醒用户"})}
		final = fmt.Sprintf("⏰ 提醒：%s（任务 #%d，你 %d 分钟前让我提醒的）", what, id, ago)
	case reply != nil:
		call = plannedCall{ToolTaskComplete, jsonInput(taskCompleteInput{TaskID: id, Result: fmt.Sprintf("回复「%s」已发出（%s）", reply.Subject, reply.ID)})}
		final = fmt.Sprintf("✅ 任务 #%d 完成：回复「%s」已经发出，这件事不用再跟了。", id, reply.Subject)
	case strings.Contains(reason, "拒绝"):
		call = plannedCall{ToolTaskComplete, jsonInput(taskCompleteInput{TaskID: id, Result: "用户拒绝了回复草稿，不再跟进"})}
		final = fmt.Sprintf("任务 #%d 关闭：你拒绝了回复草稿，我不再跟进。要换个说法回复的话跟我说。", id)
	case strings.Contains(reason, "发送失败"):
		call = plannedCall{ToolTaskSchedule, jsonInput(taskScheduleInput{TaskID: id, InMinutes: 30, Note: "回复发送失败，等用户处理"})}
		final = fmt.Sprintf("⚠️ 任务 #%d：回复没能发出，30 分钟后我再看一次。", id)
	default:
		call = plannedCall{ToolTaskSchedule, jsonInput(taskScheduleInput{TaskID: id, InMinutes: 30, Note: fmt.Sprintf("第 %d 次检查：回复还没发出", task.Checks)})}
		final = fmt.Sprintf("⏳ 任务 #%d「%s」还没完成：回复还没发出，草稿在信道里等你批准。30 分钟后再看。", id, task.Title)
	}
	if _, done := turn.last(call.name); done {
		return plan{final: final}
	}
	return plan{calls: []plannedCall{call}}
}

// planUpcoming prepares a short heads-up: time, organizer, the organizer's
// recent mail and active tasks that concern this event.
func (m *ScriptedModel) planUpcoming(turn scriptedTurn) plan {
	listed, haveCalendar := turn.last(ToolCalendarList)
	inbox, haveInbox := turn.last(ToolMailList)
	open, haveTasks := turn.last(ToolTaskList)
	recalled, haveRecall := turn.last(protocol.ToolRecall)
	if !haveCalendar || !haveInbox || !haveTasks || !haveRecall {
		return plan{calls: []plannedCall{{ToolCalendarList, `{"days":2}`}, {ToolMailList, `{"folder":"inbox","limit":30}`}, {ToolTaskList, `{}`}, {protocol.ToolRecall, `{"query":"经理"}`}}}
	}
	var events []eventView
	var mails []mailSummary
	var tasks []taskView
	_ = json.Unmarshal([]byte(listed), &events)
	_ = json.Unmarshal([]byte(inbox), &mails)
	_ = json.Unmarshal([]byte(open), &tasks)
	var event *eventView
	for i := range events {
		if events[i].ID == turn.ref {
			event = &events[i]
		}
	}
	if event == nil {
		return plan{quiet: true, final: "日程已不在日历里。"}
	}
	start, _ := time.Parse(time.RFC3339, event.Start)
	minutes := int(start.Sub(m.now()).Round(time.Minute) / time.Minute)
	var b strings.Builder
	fmt.Fprintf(&b, "⏰ %d 分钟后（%s）：%s。", minutes, start.Local().Format("15:04"), event.Title)
	name, subjects := "", []string{}
	if !strings.EqualFold(event.Organizer, OwnerAddress) {
		for _, mail := range mails {
			if strings.EqualFold(mail.From, event.Organizer) {
				name = mail.FromName
				if len(subjects) < 2 {
					subjects = append(subjects, "「"+mail.Subject+"」")
				}
			}
		}
		who := firstNonEmpty(name, event.Organizer)
		if strings.Contains(strings.ToLower(recalled), strings.ToLower(event.Organizer)) {
			who += "（你的经理）"
		}
		b.WriteString("组织者 " + who)
		if len(subjects) > 0 {
			b.WriteString("；最近来信 " + strings.Join(subjects, "、"))
		}
		b.WriteString("。")
	}
	related := []string{}
	for _, task := range tasks {
		if task.Thread == turn.thread || (name != "" && strings.Contains(task.Title, name)) {
			related = append(related, fmt.Sprintf("#%d %s", task.ID, task.Title))
		}
	}
	if len(related) > 0 {
		b.WriteString("相关的进行中任务：" + strings.Join(related, "；") + "。")
	}
	return plan{final: b.String()}
}

var reminderPattern = regexp.MustCompile(`(\d+)\s*(分钟|小时)(以?后)?`)

// parseReminder reads "30 分钟后提醒我 X" in any order; 30 minutes by default.
func parseReminder(text string) (int, string) {
	minutes := 30
	if match := reminderPattern.FindStringSubmatch(text); match != nil {
		if _, err := fmt.Sscan(match[1], &minutes); err == nil && match[2] == "小时" {
			minutes *= 60
		}
		text = strings.Replace(text, match[0], " ", 1)
	}
	what := strings.Trim(strings.Replace(text, "提醒我", " ", 1), " ，,。：:")
	return minutes, strings.Join(strings.Fields(what), " ")
}

func (m *ScriptedModel) planChat(turn scriptedTurn) plan {
	// A batch of chat messages is answered by its newest message.
	text := turn.text
	if at := strings.LastIndex(text, "用户在信道里说："); at >= 0 {
		text = text[at+len("用户在信道里说："):]
	}
	text = strings.TrimSpace(text)
	lower := strings.ToLower(text)
	has := func(words ...string) bool {
		for _, word := range words {
			if strings.Contains(lower, word) {
				return true
			}
		}
		return false
	}
	switch {
	case strings.HasPrefix(text, "记住"):
		fact := strings.TrimSpace(strings.TrimLeft(strings.TrimPrefix(text, "记住"), "：:，, "))
		if _, done := turn.last(protocol.ToolRemember); done {
			return plan{final: "好的，记住了：" + truncateRunes(fact, 60)}
		}
		if fact == "" {
			return plan{final: "要我记住什么？例如：记住 周三下午不开会"}
		}
		name := fmt.Sprintf("chat-note-%d", m.now().UnixMilli())
		return plan{calls: []plannedCall{{protocol.ToolRemember, jsonInput(struct {
			Name        string `json:"name"`
			Content     string `json:"content"`
			Description string `json:"description"`
			Type        string `json:"type"`
		}{name, fact, truncateRunes(fact, 40), "user"})}}}
	case strings.Contains(text, "提醒我"):
		if receipt, done := turn.last(ToolTaskCreate); done {
			return plan{final: "好的，" + strings.TrimPrefix(receipt, "已")}
		}
		minutes, what := parseReminder(text)
		if what == "" {
			return plan{final: "要我提醒什么？例如：30 分钟后提醒我 给 Bob 回电话"}
		}
		if minutes < 1 || minutes > int(maxTaskDelay/time.Minute) {
			return plan{final: "提醒时间要在 1 分钟到 7 天之间。"}
		}
		return plan{calls: []plannedCall{{ToolTaskCreate, jsonInput(taskCreateInput{Title: truncateRunes("提醒："+what, maxTaskTitle-1), Check: truncateRunes("到点在信道提醒用户："+what, maxTaskText-1), InMinutes: minutes})}}}
	case has("任务", "待办", "跟进"):
		listed, done := turn.last(ToolTaskList)
		if !done {
			return plan{calls: []plannedCall{{ToolTaskList, `{}`}}}
		}
		var tasks []taskView
		_ = json.Unmarshal([]byte(listed), &tasks)
		if len(tasks) == 0 {
			return plan{final: "现在没有进行中的任务。"}
		}
		lines := []string{}
		for _, task := range tasks {
			next, _ := time.Parse(time.RFC3339, task.NextCheck)
			lines = append(lines, fmt.Sprintf("#%d %s（下次检查 %s，已查 %d/%d 次）", task.ID, task.Title, next.Local().Format("15:04"), task.Checks, task.MaxChecks))
		}
		return plan{final: fmt.Sprintf("进行中的任务 %d 个：%s", len(tasks), strings.Join(lines, "；"))}
	case has("晨报", "简报", "brief"):
		return m.planBrief(turn)
	case has("日程", "会议", "calendar"):
		listed, done := turn.last(ToolCalendarList)
		if !done {
			return plan{calls: []plannedCall{{ToolCalendarList, `{"days":2}`}}}
		}
		var events []eventView
		_ = json.Unmarshal([]byte(listed), &events)
		lines := []string{}
		for _, event := range events {
			start, _ := time.Parse(time.RFC3339, event.Start)
			lines = append(lines, fmt.Sprintf("%s %s（%s）", start.Local().Format("01-02 15:04"), event.Title, event.Status))
		}
		if len(lines) == 0 {
			return plan{final: "今明两天没有日程。"}
		}
		return plan{final: "今明两天：" + strings.Join(lines, "；")}
	case has("邮件", "收件箱", "inbox", "mail"):
		listed, done := turn.last(ToolMailList)
		if !done {
			return plan{calls: []plannedCall{{ToolMailList, `{"folder":"inbox","limit":5}`}}}
		}
		var mails []mailSummary
		_ = json.Unmarshal([]byte(listed), &mails)
		lines := []string{}
		for _, mail := range mails {
			lines = append(lines, mail.FromName+"："+mail.Subject)
		}
		return plan{final: fmt.Sprintf("最近 %d 封：%s", len(lines), strings.Join(lines, "；"))}
	case has("记得", "我的经理", "偏好", "回忆", "recall"):
		recalled, done := turn.last(protocol.ToolRecall)
		if !done {
			return plan{calls: []plannedCall{{protocol.ToolRecall, jsonInput(struct {
				Query string `json:"query"`
			}{recallQuery(text)})}}}
		}
		return plan{final: "我记得：" + truncateRunes(plainMemory(recalled), 160)}
	}
	return plan{final: "我是离线脚本模型，只听得懂：记住 …／N 分钟后提醒我 …／进行中的任务／今天日程／收件箱／晨报／我的经理是谁。设置 ANTHROPIC_API_KEY 并加 -model anthropic 可换成真实模型。"}
}

// recallQuery picks a topical keyword: memory search counts substrings, so a
// whole question rarely matches while its topic word does.
func recallQuery(text string) string {
	for _, word := range []string{"经理", "偏好", "会议", "开会", "newsletter"} {
		if strings.Contains(text, word) {
			return word
		}
	}
	return text
}

var memoryTag = regexp.MustCompile(`<[^>]+>`)

// plainMemory drops the recall tool's record framing for a channel reply.
func plainMemory(text string) string {
	return strings.Join(strings.Fields(memoryTag.ReplaceAllString(text, " ")), " ")
}
