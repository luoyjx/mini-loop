package assistant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// pausedKey is the meta row of the pause switch: the pause time in Unix
// milliseconds, or "" while running.
const pausedKey = "paused_since"

// failedTriggerWindow is how long a failed trigger stays on the attention
// list. Failed proposals and handed-back tasks stay until the user acts.
const failedTriggerWindow = 24 * time.Hour

var (
	// ErrNotActionable answers an action on an item that is not, or no longer,
	// in the state the action needs (409 on the page).
	ErrNotActionable = errors.New("item is not in a state that allows this action")
	// ErrUnknownRef answers a ref that names no item (404 on the page).
	ErrUnknownRef = errors.New("unknown item")
)

const (
	pauseText  = "已暂停处理：邮件照常收录，但不派工、不发信、不跑定时检查；正在进行的回合会跑完。"
	resumeText = "已恢复处理：暂停期间积压的事件、到期的检查和今天错过的 routine 现在补上。"
)

// Paused reports whether the user paused processing, and since when. While
// paused the router claims nothing, the outbox sends nothing and the
// scheduler claims no routine, task check or reminder; the poller still
// records mail and the page still records decisions.
func (s *Store) Paused() (time.Time, bool) {
	at := s.paused.Load()
	if at == 0 {
		return time.Time{}, false
	}
	return fromMillis(at), true
}

// SetPaused records a pause or resume with its ledger entry and channel line
// in one transaction, and reports whether the state changed. The pause holds
// across restarts until the user resumes.
func (s *Store) SetPaused(ctx context.Context, paused bool, actor string, now time.Time) (bool, error) {
	changed := false
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var current string
		err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, pausedKey).Scan(&current)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if (current != "") == paused {
			return nil
		}
		value, detail, text := "", "恢复处理", resumeText
		if paused {
			value, detail, text = strconv.FormatInt(millis(now), 10), "暂停处理", pauseText
		}
		if err := s.setMeta(ctx, tx, pausedKey, value); err != nil {
			return err
		}
		if err := appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerControl, Actor: actor, Detail: detail}); err != nil {
			return err
		}
		if _, err := postChannel(ctx, tx, ChannelMessage{Role: RoleSystem, Text: text}, now); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil || !changed {
		return false, err
	}
	if paused {
		s.paused.Store(millis(now))
	} else {
		s.paused.Store(0)
	}
	return true, nil
}

// AttentionKind names what an item waits for. The kinds are disjoint: each
// item is one object in one state.
type AttentionKind string

const (
	// AttentionApproval: a draft waits for approve or deny.
	AttentionApproval AttentionKind = "approval"
	// AttentionQuestion: a worker asked the user one question.
	AttentionQuestion AttentionKind = "question"
	// AttentionSendFailed: an approved proposal finally failed to go out.
	AttentionSendFailed AttentionKind = "send_failed"
	// AttentionTaskHandoff: a task used up its automatic checks.
	AttentionTaskHandoff AttentionKind = "task_handoff"
	// AttentionTriggerFailed: an event could not be processed (last 24 hours).
	AttentionTriggerFailed AttentionKind = "trigger_failed"
	// AttentionProblem: a background component failed three times in a row.
	AttentionProblem AttentionKind = "problem"
)

// AttentionItem is one thing that waits for the user. Ref names its object
// ("proposal-3", "question-4", "task-2", "trigger-9", "health-outbox"), so
// the page offers that object's actions.
type AttentionItem struct {
	Kind   AttentionKind `json:"kind"`
	Ref    string        `json:"ref"`
	Title  string        `json:"title"`
	Detail string        `json:"detail"`
	Since  time.Time     `json:"since"`
}

// ProposalRef and TriggerRef name a proposal and a trigger on the attention
// list; TaskRef names a task.
func ProposalRef(id int64) string { return fmt.Sprintf("proposal-%d", id) }
func TriggerRef(id int64) string  { return fmt.Sprintf("trigger-%d", id) }

func splitRef(ref string) (string, int64, bool) {
	kind, number, ok := strings.Cut(ref, "-")
	id, err := strconv.ParseInt(number, 10, 64)
	if !ok || err != nil || id <= 0 {
		return "", 0, false
	}
	return kind, id, true
}

var proposalKindLabel = map[ProposalKind]string{ProposalSendEmail: "发邮件", ProposalRespondInvite: "回复邀约"}

// Attention lists what waits for the user, in kind order and oldest first
// within a kind: drafts to approve, workers' open questions, approved
// proposals that finally failed, tasks handed back after their last automatic
// check, and triggers that failed in the last 24 hours. A dismissed item leaves the list; retrying or
// resuming its object clears the dismissal. App adds component problems.
func (s *Store) Attention(ctx context.Context, now time.Time) ([]AttentionItem, error) {
	items := []AttentionItem{}
	collect := func(query string, args []any, scan func(*sql.Rows) (AttentionItem, error)) error {
		rows, err := s.db.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item, err := scan(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return rows.Err()
	}
	steps := []func() error{
		func() error {
			return collect(`SELECT id,kind,recipient,subject,reason,created_at FROM proposals WHERE status=? ORDER BY id`,
				[]any{string(ProposalPending)}, func(rows *sql.Rows) (AttentionItem, error) {
					var id, created int64
					var kind, to, subject, reason string
					err := rows.Scan(&id, &kind, &to, &subject, &reason, &created)
					detail := proposalKindLabel[ProposalKind(kind)] + " · 发给 " + to
					if reason != "" {
						detail += "；" + truncateRunes(reason, 80)
					}
					return AttentionItem{Kind: AttentionApproval, Ref: ProposalRef(id), Title: fmt.Sprintf("提案 #%d 待批准：%s", id, subject), Detail: detail, Since: fromMillis(created)}, err
				})
		},
		func() error {
			return collect(`SELECT id,trigger_id,thread,text,created_at FROM questions q WHERE status=?
				AND NOT EXISTS (SELECT 1 FROM dismissals d WHERE d.ref='question-'||q.id) ORDER BY id`,
				[]any{string(QuestionOpen)}, func(rows *sql.Rows) (AttentionItem, error) {
					var id, trigger, created int64
					var thread, text string
					err := rows.Scan(&id, &trigger, &thread, &text, &created)
					return AttentionItem{Kind: AttentionQuestion, Ref: QuestionRef(id), Title: "助理问你：" + text, Detail: fmt.Sprintf("来自触发器 #%d · thread %s；回答会交回这个 thread 的 worker", trigger, thread), Since: fromMillis(created)}, err
				})
		},
		func() error {
			return collect(`SELECT id,recipient,subject,last_error,settled_at,COALESCE(decided_at,created_at) FROM proposals p WHERE status=?
				AND NOT EXISTS (SELECT 1 FROM dismissals d WHERE d.ref='proposal-'||p.id) ORDER BY id`,
				[]any{string(ProposalFailed)}, func(rows *sql.Rows) (AttentionItem, error) {
					var id, settled, decided int64
					var to, subject, last string
					err := rows.Scan(&id, &to, &subject, &last, &settled, &decided)
					if settled == 0 {
						settled = decided
					}
					return AttentionItem{Kind: AttentionSendFailed, Ref: ProposalRef(id), Title: fmt.Sprintf("提案 #%d 没发出去：%s", id, subject), Detail: "发给 " + to + "；最近一次失败：" + truncateRunes(last, 120), Since: fromMillis(settled)}, err
				})
		},
		func() error {
			return collect(`SELECT id,title,result,check_note,updated_at FROM tasks t WHERE status=?
				AND NOT EXISTS (SELECT 1 FROM dismissals d WHERE d.ref='task-'||t.id) ORDER BY updated_at,id`,
				[]any{string(TaskStalled)}, func(rows *sql.Rows) (AttentionItem, error) {
					var id, updated int64
					var title, result, check string
					err := rows.Scan(&id, &title, &result, &check, &updated)
					return AttentionItem{Kind: AttentionTaskHandoff, Ref: TaskRef(id), Title: fmt.Sprintf("任务 #%d 转交给你：%s", id, title), Detail: result + "；要核对：" + truncateRunes(check, 80), Since: fromMillis(updated)}, err
				})
		},
		func() error {
			return collect(`SELECT id,summary,decision,updated_at FROM triggers t WHERE status=? AND updated_at>=?
				AND NOT EXISTS (SELECT 1 FROM dismissals d WHERE d.ref='trigger-'||t.id) ORDER BY id`,
				[]any{string(TriggerFailed), millis(now.Add(-failedTriggerWindow))}, func(rows *sql.Rows) (AttentionItem, error) {
					var id, updated int64
					var summary, decision string
					err := rows.Scan(&id, &summary, &decision, &updated)
					return AttentionItem{Kind: AttentionTriggerFailed, Ref: TriggerRef(id), Title: fmt.Sprintf("触发器 #%d 处理失败：%s", id, truncateRunes(summary, 60)), Detail: truncateRunes(decision, 160), Since: fromMillis(updated)}, err
				})
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// HealthItems turns component problems into attention items.
func HealthItems(problems []HealthProblem) []AttentionItem {
	items := make([]AttentionItem, 0, len(problems))
	for _, problem := range problems {
		items = append(items, AttentionItem{Kind: AttentionProblem, Ref: "health-" + string(problem.Component),
			Title:  fmt.Sprintf("%s 连续失败 %d 次", healthLabels[problem.Component], problem.Failures),
			Detail: "最近一次：" + problem.LastError + "；恢复后自动消失", Since: problem.Since})
	}
	return items
}

// Dismiss takes a failed proposal, an open question, a handed-back task or a
// failed trigger off the attention list without acting on it; a dismissed
// question can still be answered. Pending drafts and component problems
// cannot be dismissed: a draft needs a decision, and a problem clears itself
// once the component works again.
func (s *Store) Dismiss(ctx context.Context, ref, actor string, now time.Time) error {
	kind, id, ok := splitRef(ref)
	if !ok {
		return ErrUnknownRef
	}
	var query, want string
	switch kind {
	case "proposal":
		query, want = `SELECT status,trigger_id,thread FROM proposals WHERE id=?`, string(ProposalFailed)
	case "task":
		query, want = `SELECT status,last_trigger,thread FROM tasks WHERE id=?`, string(TaskStalled)
	case "trigger":
		query, want = `SELECT status,id,thread FROM triggers WHERE id=?`, string(TriggerFailed)
	case "question":
		query, want = `SELECT status,trigger_id,thread FROM questions WHERE id=?`, string(QuestionOpen)
	default:
		return ErrUnknownRef
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		var status, thread string
		var trigger int64
		err := tx.QueryRowContext(ctx, query, id).Scan(&status, &trigger, &thread)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUnknownRef
		}
		if err != nil {
			return err
		}
		if status != want {
			return fmt.Errorf("%s is %s: %w", ref, status, ErrNotActionable)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO dismissals(ref,actor,at) VALUES(?,?,?) ON CONFLICT(ref) DO NOTHING`, ref, actor, millis(now)); err != nil {
			return err
		}
		return appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerControl, Actor: actor, TriggerID: trigger, Thread: ThreadID(thread), Detail: "知道了：" + ref + " 移出待处理"})
	})
}

func undismiss(ctx context.Context, tx *sql.Tx, ref string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM dismissals WHERE ref=?`, ref)
	return err
}

// RetryProposal sends a finally failed proposal again: failed → approved with
// a fresh attempt budget under the same idempotency key, so a send that did
// reach the server before the failure still converges on one effect.
func (s *Store) RetryProposal(ctx context.Context, id int64, actor string, now time.Time) (Proposal, error) {
	var retried Proposal
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		p, err := scanProposal(tx.QueryRowContext(ctx, `SELECT `+proposalColumns+` FROM proposals WHERE id=?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUnknownRef
		}
		if err != nil {
			return err
		}
		if p.Status != ProposalFailed {
			return fmt.Errorf("proposal #%d is %s: %w", id, p.Status, ErrNotActionable)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE proposals SET status=?, attempts=0, not_before=?, settled_at=0 WHERE id=? AND status=?`, string(ProposalApproved), millis(now), id, string(ProposalFailed)); err != nil {
			return err
		}
		if err := undismiss(ctx, tx, ProposalRef(id)); err != nil {
			return err
		}
		p.Status, p.Attempts = ProposalApproved, 0
		retried = p
		return appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerDecision, Actor: actor, TriggerID: p.TriggerID, Thread: p.Thread, SessionID: p.SessionID, ActionID: p.ActionID, Detail: fmt.Sprintf("重试发送提案 #%d（同一幂等键 %s）", p.ID, IdempotencyKey(p.ID))})
	})
	return retried, err
}

// ReprocessTrigger queues a failed trigger again with a fresh attempt budget.
// A re-run converges with what the failed run left behind: drafts and tasks
// dedupe per trigger.
func (s *Store) ReprocessTrigger(ctx context.Context, id int64, actor string, now time.Time) (Trigger, error) {
	var queued Trigger
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		trigger, err := scanTrigger(tx.QueryRowContext(ctx, `SELECT `+triggerColumns+` FROM triggers WHERE id=?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUnknownRef
		}
		if err != nil {
			return err
		}
		if trigger.Status != TriggerFailed {
			return fmt.Errorf("trigger #%d is %s: %w", id, trigger.Status, ErrNotActionable)
		}
		decision := "用户要求重新处理"
		if _, err := tx.ExecContext(ctx, `UPDATE triggers SET status=?, attempts=0, not_before=?, decision=?, updated_at=? WHERE id=? AND status=?`, string(TriggerPending), millis(now), decision, millis(now), id, string(TriggerFailed)); err != nil {
			return err
		}
		if err := undismiss(ctx, tx, TriggerRef(id)); err != nil {
			return err
		}
		trigger.Status, trigger.Attempts, trigger.Decision, trigger.UpdatedAt = TriggerPending, 0, decision, now
		queued = trigger
		return appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerControl, Actor: actor, TriggerID: id, Thread: trigger.Thread, Detail: fmt.Sprintf("重新处理触发器 #%d", id)})
	})
	return queued, err
}
