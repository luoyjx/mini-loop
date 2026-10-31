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
// still goes through the session's real tool gate.
type ScriptedModel struct {
	calls atomic.Uint64
	Now   func() time.Time
}

const ScriptedModelID = "scripted-offline"

var headerPattern = regexp.MustCompile(`\[trigger id=(\d+) kind=(\S+) thread=(\S+) ref=(\S*)\]`)

type scriptedTurn struct {
	kind    TriggerKind
	ref     string
	text    string
	results map[protocol.ToolName][]string
}

type plannedCall struct {
	name  protocol.ToolName
	input string
}

type plan struct {
	calls []plannedCall
	final string
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
		turn.kind, turn.ref = TriggerKind(text[match[4]:match[5]]), text[match[8]:match[9]]
		turn.text = strings.TrimSpace(text[match[1]:])
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
	switch turn.kind {
	case TriggerEmail:
		return planEmail(turn)
	case TriggerInvite:
		return planInvite(turn)
	case TriggerRoutine:
		return m.planBrief(turn)
	case TriggerChat:
		return m.planChat(turn)
	}
	return plan{final: "（离线脚本模型不认识这个触发器）"}
}

var suspiciousWords = []string{"转发", "forward", "密码", "password", "汇款", "礼品卡", "gift card", "验证码"}

func planEmail(turn scriptedTurn) plan {
	read, haveMail := turn.last(ToolMailRead)
	recalled, haveRecall := turn.last(protocol.ToolRecall)
	if !haveMail || !haveRecall {
		return plan{calls: []plannedCall{{ToolMailRead, jsonInput(mailReadInput{ID: turn.ref})}, {protocol.ToolRecall, `{"query":"经理"}`}}}
	}
	var mail mailDetail
	if err := json.Unmarshal([]byte(read), &mail); err != nil {
		return plan{final: "读取邮件失败：" + truncateRunes(read, 80)}
	}
	manager := strings.Contains(strings.ToLower(recalled), strings.ToLower(mail.From))
	content := strings.ToLower(mail.Subject + " " + mail.Body)
	if !manager {
		for _, word := range suspiciousWords {
			if strings.Contains(content, strings.ToLower(word)) {
				return plan{final: fmt.Sprintf("⚠️ 可疑邮件：%s <%s> 要求「%s」，含「%s」类指令。我没有执行，也没起草回复，建议忽略或上报。", mail.FromName, mail.From, mail.Subject, word)}
			}
		}
		return plan{final: fmt.Sprintf("%s 普通来信（%s：%s），不打扰用户，记入晨报。", QuietMarker, mail.FromName, mail.Subject)}
	}
	if receipt, done := turn.last(ToolProposeEmail); done {
		return plan{final: fmt.Sprintf("%s（你的经理）来信：「%s」。我起草了回复，%s", mail.FromName, mail.Subject, proposalNumber(receipt))}
	}
	subject := mail.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	body := fmt.Sprintf("%s 你好，\n\n收到，关于「%s」我今天下班前给你答复。\n\n（由助理起草，经我确认后发出）", mail.FromName, mail.Subject)
	return plan{calls: []plannedCall{{ToolProposeEmail, jsonInput(proposeEmailInput{To: mail.From, Subject: subject, Body: body, Reason: "经理来信，需要当天回复"})}}}
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
	if !haveCalendar || !haveInbox || !haveRecall {
		return plan{calls: []plannedCall{{ToolCalendarList, `{"days":1}`}, {ToolMailList, `{"folder":"inbox","limit":30}`}, {protocol.ToolRecall, `{"query":"经理"}`}}}
	}
	var events []eventView
	var mails []mailSummary
	_ = json.Unmarshal([]byte(listed), &events)
	_ = json.Unmarshal([]byte(inbox), &mails)
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
	b.WriteString("。")
	return plan{final: b.String()}
}

func (m *ScriptedModel) planChat(turn scriptedTurn) plan {
	text := strings.TrimSpace(strings.TrimPrefix(turn.text, "用户在信道里说："))
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
	return plan{final: "我是离线脚本模型，只听得懂：记住 …／今天日程／收件箱／晨报／我的经理是谁。设置 ANTHROPIC_API_KEY 并加 -model anthropic 可换成真实模型。"}
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
