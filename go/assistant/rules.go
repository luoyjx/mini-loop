package assistant

import "strings"

// Verdict is the fast-path (deterministic) filter outcome. Only Candidate
// reaches a model: an explicit request goes straight to the worker, and an
// inferred candidate first passes the triage, the semantic slow path. The
// worker may still report quiet.
type Verdict string

const (
	VerdictIgnore    Verdict = "ignore"
	VerdictCandidate Verdict = "candidate"
)

// Grade is the admission grade of a candidate. Mail from a known contact
// addressed to the user is an explicit request; mail where only one of the two
// holds (a known contact copied the user, or a stranger wrote directly) is an
// inferred candidate. Every non-mail trigger is explicit: the user, the
// scheduler or an invite addressed to the user started it.
type Grade string

const (
	GradeExplicit Grade = "explicit"
	GradeInferred Grade = "inferred"
)

type RuleDecision struct {
	Verdict Verdict
	Rule    string
	Grade   Grade
}

// Rules are the program-level admission policy: cheap, explainable and
// editable without touching prompts.
type Rules struct {
	// BulkSenders are address fragments treated as bulk mail.
	BulkSenders []string
}

func DefaultRules() Rules {
	return Rules{BulkSenders: []string{"noreply", "no-reply", "newsletter", "digest@", "promo"}}
}

// Classify applies the fast path. known reports whether the mail's sender is
// a contact; it is ignored for other trigger kinds.
func (rules Rules) Classify(trigger Trigger, mail *Email, known bool) RuleDecision {
	explicit := func(rule string) RuleDecision { return RuleDecision{VerdictCandidate, rule, GradeExplicit} }
	switch trigger.Kind {
	case TriggerChat:
		return explicit("用户在信道里直接发话")
	case TriggerRoutine:
		return explicit("定时 routine")
	case TriggerInvite:
		return explicit("会议邀约：后台提前核对日程并起草回复")
	case TriggerTaskWakeup:
		return explicit("任务到了检查时间")
	case TriggerUpcoming:
		return explicit("日程临近：提前准备提醒")
	case TriggerAnswer:
		return explicit("用户回答了 worker 的提问")
	case TriggerJobDone:
		return explicit("后台准备任务有了结果")
	}
	if mail == nil {
		return RuleDecision{VerdictIgnore, "触发器引用的邮件不存在", ""}
	}
	if mail.Category == CategoryNewsletter {
		return RuleDecision{VerdictIgnore, "批量邮件（newsletter），静默归档，留给晨报统计", ""}
	}
	from := strings.ToLower(mail.From)
	for _, fragment := range rules.BulkSenders {
		if strings.Contains(from, fragment) {
			return RuleDecision{VerdictIgnore, "发件人命中批量邮件规则「" + fragment + "」", ""}
		}
	}
	direct := addressed(mail.To, OwnerAddress)
	switch {
	case known && direct:
		return explicit("已知联系人直接写给你：明确请求")
	case known:
		return RuleDecision{VerdictCandidate, "已知联系人抄送你：推断候选", GradeInferred}
	case direct:
		return RuleDecision{VerdictCandidate, "陌生发件人直接写给你：推断候选", GradeInferred}
	}
	return RuleDecision{VerdictIgnore, "陌生发件人、你只在抄送里：只归档", ""}
}
