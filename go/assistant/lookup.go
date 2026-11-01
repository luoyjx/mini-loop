package assistant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Chain recall: lookup reads one local record by its ref and names the refs
// one hop away, so a worker can follow a piece of work across triggers,
// threads, worker sessions, drafts, tasks, questions and preparation jobs
// instead of searching for it. It only reads assistant.db; summaries may
// quote external mail and are data.
const (
	maxLookupRef    = 120
	maxLookupOutput = 1600
	lookupRows      = 8
	threadPrefix    = "thread:"
	sessionPrefix   = "session:"
)

// LookupRefs describes the refs lookup accepts, for the tool description and
// errors.
const LookupRefs = "trigger-<id>、task-<id>、proposal-<id>、question-<id>、job-<id>、thread:<thread>、session:<worker session>"

type lookupResult struct {
	lines []string
	next  []string
	seen  map[string]bool
	self  string
}

func newLookup(self string) *lookupResult {
	return &lookupResult{seen: map[string]bool{self: true}, self: self}
}

func (r *lookupResult) line(format string, args ...any) {
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
}

// hop names a ref one step away; empty and repeated refs are skipped.
func (r *lookupResult) hop(ref string) {
	if ref == "" || ref == threadPrefix || ref == sessionPrefix || r.seen[ref] {
		return
	}
	if at := strings.LastIndex(ref, "-"); at > 0 && ref[at+1:] == "0" && !strings.Contains(ref, ":") {
		return
	}
	r.seen[ref] = true
	r.next = append(r.next, ref)
}

func (r *lookupResult) String() string {
	next := "无"
	if len(r.next) > 0 {
		next = strings.Join(r.next, " · ")
	}
	return truncateRunes(strings.Join(r.lines, "\n")+"\n下一跳："+next, maxLookupOutput)
}

func refID(ref, prefix string) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimPrefix(ref, prefix), 10, 64)
	return id, strings.HasPrefix(ref, prefix) && err == nil && id > 0
}

// Lookup resolves one ref. An unknown or malformed ref is ErrUnknownRef.
func (s *Store) Lookup(ctx context.Context, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	result := newLookup(ref)
	var err error
	switch {
	case strings.HasPrefix(ref, threadPrefix) && len(ref) > len(threadPrefix):
		err = s.lookupThread(ctx, ThreadID(strings.TrimPrefix(ref, threadPrefix)), result)
	case strings.HasPrefix(ref, sessionPrefix) && len(ref) > len(sessionPrefix):
		err = s.lookupSession(ctx, strings.TrimPrefix(ref, sessionPrefix), result)
	default:
		err = s.lookupRecord(ctx, ref, result)
	}
	if err != nil {
		return "", err
	}
	return result.String(), nil
}

func (s *Store) lookupRecord(ctx context.Context, ref string, r *lookupResult) error {
	if id, ok := refID(ref, "trigger-"); ok {
		trigger, err := scanTrigger(s.db.QueryRowContext(ctx, `SELECT `+triggerColumns+` FROM triggers WHERE id=?`, id))
		if err != nil {
			return lookupMissing(ref, err)
		}
		return s.describeTrigger(ctx, trigger, r)
	}
	if id, ok := refID(ref, "task-"); ok {
		task, found, err := s.Task(ctx, id)
		if err != nil || !found {
			return lookupMissing(ref, err)
		}
		r.line("%s · 任务「%s」· %s · thread %s", ref, task.Title, task.Status, task.Thread)
		r.line("要核对：%s", truncateRunes(task.Check, 120))
		r.line("进展：%s · 已检查 %d/%d 次", firstNonEmpty(truncateRunes(task.Note, 120), "无"), task.Checks, task.MaxChecks)
		if task.Result != "" {
			r.line("结果：%s（%s）", truncateRunes(task.Result, 120), task.ClosedBy)
		}
		r.hop(TriggerRef(task.TriggerID))
		r.hop(TriggerRef(task.LastTrigger))
		r.hop(threadPrefix + string(task.Thread))
		r.hop(sessionPrefix + task.SessionID)
		return nil
	}
	if id, ok := refID(ref, "proposal-"); ok {
		p, found, err := s.Proposal(ctx, id)
		if err != nil || !found {
			return lookupMissing(ref, err)
		}
		r.line("%s · %s → %s · %s · thread %s", ref, p.Kind, p.To, p.Status, p.Thread)
		r.line("主题：%s", truncateRunes(p.Subject, 120))
		if p.DecidedBy != "" {
			r.line("决定：%s", p.DecidedBy)
		}
		if p.Receipt != "" {
			r.line("回执：%s", p.Receipt)
		}
		r.hop(TriggerRef(p.TriggerID))
		r.hop(threadPrefix + string(p.Thread))
		r.hop(sessionPrefix + p.SessionID)
		return nil
	}
	if id, ok := refID(ref, "question-"); ok {
		q, found, err := s.Question(ctx, id)
		if err != nil || !found {
			return lookupMissing(ref, err)
		}
		r.line("%s · %s · thread %s", ref, q.Status, q.Thread)
		r.line("提问：%s", truncateRunes(q.Text, 160))
		if q.Answer != "" {
			r.line("用户回答：%s", truncateRunes(q.Answer, 160))
		}
		r.hop(TriggerRef(q.TriggerID))
		r.hop(threadPrefix + string(q.Thread))
		r.hop(sessionPrefix + q.SessionID)
		return nil
	}
	if id, ok := refID(ref, "job-"); ok {
		job, found, err := s.Job(ctx, id)
		if err != nil || !found {
			return lookupMissing(ref, err)
		}
		r.line("%s · 后台准备「%s」· %s · thread %s", ref, truncateRunes(job.Goal, 80), job.Status, job.Thread)
		if job.Result != "" {
			r.line("产出（可能引用外部内容）：%s", truncateRunes(job.Result, 300))
		}
		r.hop(TriggerRef(job.TriggerID))
		r.hop(threadPrefix + string(job.Thread))
		r.hop(sessionPrefix + job.SessionID)
		return nil
	}
	return fmt.Errorf("ref %q: use %s: %w", ref, LookupRefs, ErrUnknownRef)
}

func lookupMissing(ref string, err error) error {
	if err == nil || errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%s not found: %w", ref, ErrUnknownRef)
	}
	return err
}

// describeTrigger writes one trigger and the records it started.
func (s *Store) describeTrigger(ctx context.Context, trigger Trigger, r *lookupResult) error {
	r.line("%s · %s · %s · thread %s · %s", TriggerRef(trigger.ID), trigger.Kind, trigger.Status, trigger.Thread, trigger.CreatedAt.Format("01-02 15:04"))
	r.line("摘要（可能引用外部内容）：%s", truncateRunes(trigger.Summary, 160))
	if trigger.Decision != "" {
		r.line("决定：%s", truncateRunes(trigger.Decision, 160))
	}
	switch trigger.Kind {
	case TriggerTaskWakeup, TriggerAnswer, TriggerJobDone:
		r.hop(trigger.Ref)
	}
	r.hop(threadPrefix + string(trigger.Thread))
	r.hop(sessionPrefix + trigger.SessionID)
	refs, err := s.refsStartedBy(ctx, `trigger_id=?`, trigger.ID)
	for _, ref := range refs {
		r.hop(ref)
	}
	return err
}

// refsStartedBy lists the drafts, tasks, questions and jobs matching a
// condition on their trigger_id, thread or session_id column.
func (s *Store) refsStartedBy(ctx context.Context, where string, arg any) ([]string, error) {
	sources := []struct {
		table string
		ref   func(int64) string
	}{
		{"proposals", ProposalRef}, {"tasks", TaskRef}, {"questions", QuestionRef}, {"jobs", JobRef},
	}
	refs := []string{}
	for _, source := range sources {
		rows, err := s.db.QueryContext(ctx, `SELECT id FROM `+source.table+` WHERE `+where+` ORDER BY id DESC LIMIT ?`, arg, lookupRows)
		if err != nil {
			return refs, err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return refs, err
			}
			refs = append(refs, source.ref(id))
		}
		if err := rows.Close(); err != nil {
			return refs, err
		}
	}
	return refs, nil
}

func (s *Store) triggersWhere(ctx context.Context, where string, arg any) ([]Trigger, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+triggerColumns+` FROM triggers WHERE `+where+` ORDER BY id DESC LIMIT ?`, arg, lookupRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Trigger{}
	for rows.Next() {
		trigger, err := scanTrigger(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, trigger)
	}
	return result, rows.Err()
}

// lookupThread lists a thread's newest triggers and what they started.
func (s *Store) lookupThread(ctx context.Context, thread ThreadID, r *lookupResult) error {
	triggers, err := s.triggersWhere(ctx, `thread=?`, string(thread))
	if err != nil {
		return err
	}
	if len(triggers) == 0 {
		return fmt.Errorf("thread %s not found: %w", thread, ErrUnknownRef)
	}
	session, subscribed, err := s.Subscription(ctx, thread)
	if err != nil {
		return err
	}
	r.line("thread %s · 最近 %d 个触发器（新的在前；摘要可能引用外部内容）：", thread, len(triggers))
	for _, trigger := range triggers {
		r.line("- %s %s %s：%s", TriggerRef(trigger.ID), trigger.Kind, trigger.Status, truncateRunes(trigger.Summary, 60))
		r.hop(TriggerRef(trigger.ID))
	}
	if subscribed {
		r.line("订阅的 worker：%s", session)
		r.hop(sessionPrefix + session)
	}
	refs, err := s.refsStartedBy(ctx, `thread=?`, string(thread))
	for _, ref := range refs {
		r.hop(ref)
	}
	return err
}

// lookupSession lists the triggers a worker session handled and what it
// started.
func (s *Store) lookupSession(ctx context.Context, session string, r *lookupResult) error {
	triggers, err := s.triggersWhere(ctx, `session_id=?`, session)
	if err != nil {
		return err
	}
	refs, err := s.refsStartedBy(ctx, `session_id=?`, session)
	if err != nil {
		return err
	}
	if len(triggers) == 0 && len(refs) == 0 {
		return fmt.Errorf("session %s not found: %w", session, ErrUnknownRef)
	}
	r.line("worker session %s · 处理过 %d 个触发器（新的在前；摘要可能引用外部内容）：", session, len(triggers))
	for _, trigger := range triggers {
		r.line("- %s %s · thread %s：%s", TriggerRef(trigger.ID), trigger.Kind, trigger.Thread, truncateRunes(trigger.Summary, 60))
		r.hop(TriggerRef(trigger.ID))
		r.hop(threadPrefix + string(trigger.Thread))
	}
	for _, ref := range refs {
		r.hop(ref)
	}
	return nil
}
