package assistant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Task limits. A worker picks each check time within [minTaskDelay,
// maxTaskDelay]; a claimed check pushes the next one taskFallback out until
// the worker schedules its own; a task wakes at most defaultMaxChecks times.
const (
	maxActiveTasks   = 20
	defaultMaxChecks = 6
	taskFallback     = 30 * time.Minute
	minTaskDelay     = time.Minute
	maxTaskDelay     = 7 * 24 * time.Hour
	maxTaskTitle     = 80
	maxTaskText      = 300
)

var (
	ErrTaskClosed   = errors.New("task is no longer active")
	ErrTooManyTasks = fmt.Errorf("at most %d active tasks", maxActiveTasks)
)

// TaskRef is a task.wakeup trigger's Ref.
func TaskRef(id int64) string { return fmt.Sprintf("task-%d", id) }

// taskID parses a TaskRef.
func taskID(ref string) (int64, bool) {
	var id int64
	if _, err := fmt.Sscanf(ref, "task-%d", &id); err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

const taskColumns = `id,title,thread,session_id,trigger_id,status,check_note,note,result,wake_at,wake_reason,checks,max_checks,last_trigger,closed_by,created_at,updated_at`

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var task Task
	var thread, status string
	var wake, created, updated int64
	if err := row.Scan(&task.ID, &task.Title, &thread, &task.SessionID, &task.TriggerID, &status, &task.Check, &task.Note, &task.Result, &wake, &task.WakeReason, &task.Checks, &task.MaxChecks, &task.LastTrigger, &task.ClosedBy, &created, &updated); err != nil {
		return Task{}, err
	}
	task.Thread, task.Status, task.CreatedAt, task.UpdatedAt = ThreadID(thread), TaskStatus(status), fromMillis(created), fromMillis(updated)
	if wake > 0 {
		at := fromMillis(wake)
		task.WakeAt = &at
	}
	return task, nil
}

func queryTasks(ctx context.Context, q queryer, query string, args ...any) ([]Task, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+taskColumns+` FROM tasks `+query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Task{}
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, rows.Err()
}

func activeTask(ctx context.Context, tx *sql.Tx, id int64) (Task, error) {
	task, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, fmt.Errorf("task #%d not found", id)
	}
	if err != nil {
		return Task{}, err
	}
	if task.Status != TaskActive {
		return task, fmt.Errorf("task #%d is %s: %w", id, task.Status, ErrTaskClosed)
	}
	return task, nil
}

func taskLedger(task Task, actor, detail string, now time.Time) LedgerEntry {
	return LedgerEntry{At: now, Kind: LedgerTask, Actor: actor, TriggerID: task.LastTrigger, Thread: task.Thread, SessionID: task.SessionID, Detail: fmt.Sprintf("任务 #%d %s", task.ID, detail)}
}

// CreateTask is idempotent per (origin trigger, title): a re-run after a
// crash returns the existing task with created=false.
func (s *Store) CreateTask(ctx context.Context, task Task, wakeAt time.Time, actor string, now time.Time) (Task, bool, error) {
	dedupe := fmt.Sprintf("%d|%s", task.TriggerID, strings.ToLower(strings.TrimSpace(task.Title)))
	var result Task
	created := false
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		existing, err := scanTask(tx.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE dedupe=?`, dedupe))
		if err == nil {
			result = existing
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE status=?`, string(TaskActive)).Scan(&active); err != nil {
			return err
		}
		if active >= maxActiveTasks {
			return ErrTooManyTasks
		}
		inserted, err := tx.ExecContext(ctx, `INSERT INTO tasks(title,thread,session_id,trigger_id,status,check_note,note,wake_at,wake_reason,max_checks,last_trigger,dedupe,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			task.Title, string(task.Thread), task.SessionID, task.TriggerID, string(TaskActive), task.Check, task.Note, millis(wakeAt), "worker 约定的首次检查", defaultMaxChecks, task.TriggerID, dedupe, millis(now), millis(now))
		if err != nil {
			return err
		}
		if task.ID, err = inserted.LastInsertId(); err != nil {
			return err
		}
		task.Status, task.WakeAt, task.WakeReason, task.MaxChecks, task.LastTrigger, task.CreatedAt, task.UpdatedAt = TaskActive, &wakeAt, "worker 约定的首次检查", defaultMaxChecks, task.TriggerID, now, now
		result, created = task, true
		return appendLedger(ctx, tx, taskLedger(task, actor, fmt.Sprintf("创建：%s；首次检查 %s", task.Title, wakeAt.Format("01-02 15:04")), now))
	})
	return result, created, err
}

// ScheduleTask sets an active task's next check, and optionally what to check
// and a progress note.
func (s *Store) ScheduleTask(ctx context.Context, id int64, wakeAt time.Time, check, note, actor string, now time.Time) (Task, error) {
	var result Task
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		task, err := activeTask(ctx, tx, id)
		if err != nil {
			return err
		}
		if strings.TrimSpace(check) != "" {
			task.Check = check
		}
		if strings.TrimSpace(note) != "" {
			task.Note = note
		}
		task.WakeAt, task.WakeReason, task.UpdatedAt = &wakeAt, "worker 约定", now
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET wake_at=?, wake_reason=?, check_note=?, note=?, updated_at=? WHERE id=?`, millis(wakeAt), task.WakeReason, task.Check, task.Note, millis(now), id); err != nil {
			return err
		}
		result = task
		detail := "下次检查 " + wakeAt.Format("01-02 15:04")
		if task.Note != "" {
			detail += "；进展：" + truncateRunes(task.Note, 80)
		}
		return appendLedger(ctx, tx, taskLedger(task, actor, detail, now))
	})
	return result, err
}

// CloseTask ends an active task as done (a worker), cancelled (the user) or
// stalled (the scheduler).
func (s *Store) CloseTask(ctx context.Context, id int64, status TaskStatus, result, actor string, now time.Time) (Task, error) {
	var closed Task
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		task, err := activeTask(ctx, tx, id)
		if err != nil {
			return err
		}
		closed, err = closeTask(ctx, tx, task, status, result, actor, now)
		return err
	})
	return closed, err
}

func closeTask(ctx context.Context, tx *sql.Tx, task Task, status TaskStatus, result, actor string, now time.Time) (Task, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET status=?, result=?, closed_by=?, wake_at=0, updated_at=? WHERE id=? AND status=?`, string(status), result, actor, millis(now), task.ID, string(TaskActive)); err != nil {
		return Task{}, err
	}
	task.Status, task.Result, task.ClosedBy, task.WakeAt, task.UpdatedAt = status, result, actor, nil, now
	verb := map[TaskStatus]string{TaskDone: "完成", TaskCancelled: "取消", TaskStalled: "转交用户"}[status]
	return task, appendLedger(ctx, tx, taskLedger(task, actor, verb+"："+truncateRunes(result, 120), now))
}

// WakeTask moves an active task's next check to now. The page uses it for
// "check now"; the reason reaches the worker's prompt.
func (s *Store) WakeTask(ctx context.Context, id int64, reason, actor string, now time.Time) (Task, error) {
	var result Task
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		task, err := activeTask(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET wake_at=?, wake_reason=?, updated_at=? WHERE id=?`, millis(now), reason, millis(now), id); err != nil {
			return err
		}
		task.WakeAt, task.WakeReason, task.UpdatedAt = &now, reason, now
		result = task
		return appendLedger(ctx, tx, taskLedger(task, actor, "提前检查："+reason, now))
	})
	return result, err
}

// bringForward pulls the next check of every active task on thread to now.
// An outbox outcome or a decision on that thread is news the task's worker
// should see without waiting for its scheduled check.
func bringForward(ctx context.Context, tx *sql.Tx, thread ThreadID, reason, actor string, now time.Time) error {
	tasks, err := queryTasks(ctx, tx, `WHERE thread=? AND status=? AND wake_at>?`, string(thread), string(TaskActive), millis(now))
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if _, err := tx.ExecContext(ctx, `UPDATE tasks SET wake_at=?, wake_reason=?, updated_at=? WHERE id=?`, millis(now), reason, millis(now), task.ID); err != nil {
			return err
		}
		if err := appendLedger(ctx, tx, taskLedger(task, actor, "提前检查："+reason, now)); err != nil {
			return err
		}
	}
	return nil
}

// ClaimDueTasks turns every due task into one task.wakeup trigger, in the same
// transaction that counts the check and sets the fallback check. A task whose
// previous wakeup is still queued or running waits for it. A task out of
// checks is handed to the user instead. A check more than late after its time
// (the daemon was down or the Mac asleep) is labelled a catch-up.
func (s *Store) ClaimDueTasks(ctx context.Context, now time.Time, late time.Duration) (woken, stalled int, err error) {
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		due, err := queryTasks(ctx, tx, `WHERE status=? AND wake_at>0 AND wake_at<=?
			AND NOT EXISTS (SELECT 1 FROM triggers t WHERE t.id=tasks.last_trigger AND t.kind=? AND t.status IN (?,?))
			ORDER BY wake_at, id`, string(TaskActive), millis(now), string(TriggerTaskWakeup), string(TriggerPending), string(TriggerRunning))
		if err != nil {
			return err
		}
		for _, task := range due {
			if task.Checks >= task.MaxChecks {
				result := fmt.Sprintf("自动检查 %d 次仍未完成，交给你处理", task.Checks)
				if _, err := closeTask(ctx, tx, task, TaskStalled, result, "scheduler", now); err != nil {
					return err
				}
				if _, err := postChannel(ctx, tx, ChannelMessage{Role: RoleSystem, Text: fmt.Sprintf("任务 #%d「%s」%s。", task.ID, task.Title, result), TriggerID: task.LastTrigger}, now); err != nil {
					return err
				}
				stalled++
				continue
			}
			summary := fmt.Sprintf("任务 #%d 第 %d/%d 次检查（%s）：%s", task.ID, task.Checks+1, task.MaxChecks, task.WakeReason, task.Title)
			if task.WakeAt != nil && now.Sub(*task.WakeAt) > late {
				summary += "，补上错过的检查"
			}
			id, err := enqueueTrigger(ctx, tx, Trigger{Kind: TriggerTaskWakeup, Thread: task.Thread, Ref: TaskRef(task.ID), Summary: summary}, now)
			if err != nil {
				return err
			}
			fallback := now.Add(taskFallback)
			if _, err := tx.ExecContext(ctx, `UPDATE tasks SET checks=checks+1, last_trigger=?, wake_at=?, wake_reason=?, updated_at=? WHERE id=?`, id, millis(fallback), "兜底检查：上次检查没有约定下一次", millis(now), task.ID); err != nil {
				return err
			}
			woken++
		}
		return nil
	})
	return woken, stalled, err
}

func (s *Store) Task(ctx context.Context, id int64) (Task, bool, error) {
	task, err := scanTask(s.db.QueryRowContext(ctx, `SELECT `+taskColumns+` FROM tasks WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, false, nil
	}
	return task, err == nil, err
}

// Tasks lists active tasks by next check, then the most recently closed.
func (s *Store) Tasks(ctx context.Context, limit int) ([]Task, error) {
	return queryTasks(ctx, s.db, `ORDER BY status<>?, CASE WHEN status=? THEN wake_at ELSE -updated_at END, id DESC LIMIT ?`, string(TaskActive), string(TaskActive), limit)
}

// ActiveTasks lists active tasks, on one thread or (thread "") all of them.
func (s *Store) ActiveTasks(ctx context.Context, thread ThreadID) ([]Task, error) {
	if thread == "" {
		return queryTasks(ctx, s.db, `WHERE status=? ORDER BY wake_at, id LIMIT ?`, string(TaskActive), maxActiveTasks)
	}
	return queryTasks(ctx, s.db, `WHERE status=? AND thread=? ORDER BY wake_at, id`, string(TaskActive), string(thread))
}

// ---- event reminders ----

// EventThread is the thread an event's work belongs to: its invite thread
// when it came from one, otherwise a thread of its own.
func EventThread(event CalendarEvent) ThreadID {
	if event.Thread != "" {
		return event.Thread
	}
	return ThreadID("event:" + event.ID)
}

// ClaimReminder records one reminder per (event, start time) and enqueues its
// trigger in the same transaction. A rescheduled event is reminded again.
func (s *Store) ClaimReminder(ctx context.Context, event CalendarEvent, now time.Time) (int64, bool, error) {
	var triggerID int64
	claimed := false
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO reminders(event_id,start_at,trigger_id,created_at) VALUES(?,?,0,?)`, event.ID, millis(event.Start), millis(now))
		if err != nil {
			return err
		}
		if affected, err := result.RowsAffected(); err != nil || affected == 0 {
			return err
		}
		minutes := int(event.Start.Sub(now).Round(time.Minute) / time.Minute)
		summary := fmt.Sprintf("%s 开始：%s（还有 %d 分钟）", event.Start.Format("15:04"), truncateRunes(event.Title, 60), minutes)
		triggerID, err = enqueueTrigger(ctx, tx, Trigger{Kind: TriggerUpcoming, Thread: EventThread(event), Ref: event.ID, Summary: summary}, now)
		if err != nil {
			return err
		}
		claimed = true
		_, err = tx.ExecContext(ctx, `UPDATE reminders SET trigger_id=? WHERE event_id=? AND start_at=?`, triggerID, event.ID, millis(event.Start))
		return err
	})
	return triggerID, claimed, err
}

// CountTriggers counts triggers in one status (the page shows running ones).
func (s *Store) CountTriggers(ctx context.Context, status TriggerStatus) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM triggers WHERE status=?`, string(status)).Scan(&count)
	return count, err
}
