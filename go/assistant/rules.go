package assistant

import "strings"

// Verdict is the fast-path (deterministic) filter outcome. Only Candidate
// reaches a model; the semantic slow path then runs inside the worker turn,
// which may still answer [QUIET].
type Verdict string

const (
	VerdictIgnore    Verdict = "ignore"
	VerdictCandidate Verdict = "candidate"
)

type RuleDecision struct {
	Verdict Verdict
	Rule    string
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

func (rules Rules) Classify(trigger Trigger, mail *Email) RuleDecision {
	switch trigger.Kind {
	case TriggerChat:
		return RuleDecision{VerdictCandidate, "用户在信道里直接发话"}
	case TriggerRoutine:
		return RuleDecision{VerdictCandidate, "定时 routine"}
	case TriggerInvite:
		return RuleDecision{VerdictCandidate, "会议邀约：后台提前核对日程并起草回复"}
	}
	if mail == nil {
		return RuleDecision{VerdictIgnore, "触发器引用的邮件不存在"}
	}
	if mail.Category == CategoryNewsletter {
		return RuleDecision{VerdictIgnore, "批量邮件（newsletter），静默归档，留给晨报统计"}
	}
	from := strings.ToLower(mail.From)
	for _, fragment := range rules.BulkSenders {
		if strings.Contains(from, fragment) {
			return RuleDecision{VerdictIgnore, "发件人命中批量邮件规则「" + fragment + "」"}
		}
	}
	return RuleDecision{VerdictCandidate, "个人来信，交给 worker 做语义判断"}
}
