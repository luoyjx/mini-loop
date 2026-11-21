package assistant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// A turn used to end in several transactions: the channel lines, each
// trigger's status, the task-check snapshots and the ledger rows. A crash
// between them left a line posted while its trigger was still running, so the
// restart ran the turn again and posted the line twice, and a snapshot written
// twice looked like a stalled check to the watchdog. Settle commits a turn's
// whole outcome in one transaction, the way an engine commits a state change
// together with the work it enqueues. After a crash the outcome is either all
// there or not there at all, and then the triggers are still running and the
// restart requeues them.

// errSettled reports that a trigger of the batch is no longer running, so
// another path already settled it. Nothing is written.
var errSettled = errors.New("trigger already settled")

// Settlement is everything one turn's end writes.
type Settlement struct {
	// Batch is the turn's triggers, the lead first.
	Batch  []Trigger
	Status TriggerStatus
	// Decision is the lead's decision. Followers name the turn they joined.
	// With a Question it is replaced by the question's number.
	Decision string
	Session  string
	// Messages go to the channel, in order: the report, then approval cards.
	Messages []ChannelMessage
	// Question is the worker's question. Its messages are posted only when
	// the question is new, so a lead trigger asks once.
	Question *Question
	// Checks are tasks the turn checked, each snapshotted once per trigger.
	Checks []TaskCheck
	Ledger []LedgerEntry
}

// TaskCheck names a task and the trigger whose turn checked it.
type TaskCheck struct {
	Task    int64
	Trigger int64
}

// Settle commits a turn's outcome. It returns the question the turn asked,
// if any.
func (s *Store) Settle(ctx context.Context, settlement Settlement, now time.Time) (Question, error) {
	if len(settlement.Batch) == 0 {
		return Question{}, errors.New("settle: empty batch")
	}
	var asked Question
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		for _, entry := range settlement.Ledger {
			if entry.At.IsZero() {
				entry.At = now
			}
			if err := appendLedger(ctx, tx, entry); err != nil {
				return err
			}
		}
		decision := settlement.Decision
		if settlement.Question != nil {
			question, _, err := askQuestion(ctx, tx, *settlement.Question, settlement.Messages, now)
			if err != nil {
				return err
			}
			asked = question
			decision = fmt.Sprintf("向用户提问 #%d，等待回答", question.ID)
		} else {
			for _, message := range settlement.Messages {
				if _, err := postChannel(ctx, tx, message, now); err != nil {
					return err
				}
			}
		}
		for _, check := range settlement.Checks {
			if _, err := tx.ExecContext(ctx, `INSERT INTO task_checks(task_id,trigger_id,check_note,note,status,at)
				SELECT id,?,check_note,note,status,? FROM tasks WHERE id=?
				AND NOT EXISTS (SELECT 1 FROM task_checks WHERE task_id=? AND trigger_id=?)`,
				check.Trigger, millis(now), check.Task, check.Task, check.Trigger); err != nil {
				return err
			}
		}
		lead := settlement.Batch[0]
		for i, trigger := range settlement.Batch {
			text := decision
			switch {
			case i > 0:
				text = fmt.Sprintf("并入触发器 #%d 的回合：%s", lead.ID, decision)
			case len(settlement.Batch) > 1:
				text = fmt.Sprintf("%s（这一回合合并了 %d 个触发器）", decision, len(settlement.Batch))
			}
			if err := settleTrigger(ctx, tx, trigger.ID, settlement.Status, text, &settlement.Session, now); err != nil {
				return err
			}
		}
		return nil
	})
	return asked, err
}

// FailBatch fails every trigger of a batch with the same cause and tells the
// user, in one transaction. Each trigger keeps its own session.
func (s *Store) FailBatch(ctx context.Context, batch []Trigger, cause string, notice ChannelMessage, now time.Time) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		for _, trigger := range batch {
			if err := settleTrigger(ctx, tx, trigger.ID, TriggerFailed, cause, nil, now); err != nil {
				return err
			}
		}
		_, err := postChannel(ctx, tx, notice, now)
		return err
	})
}

// settleTrigger ends one running trigger. A nil session keeps the trigger's.
func settleTrigger(ctx context.Context, tx *sql.Tx, id int64, status TriggerStatus, decision string, session *string, now time.Time) error {
	var result sql.Result
	var err error
	if session == nil {
		result, err = tx.ExecContext(ctx, `UPDATE triggers SET status=?, decision=?, updated_at=? WHERE id=? AND status=?`,
			string(status), decision, millis(now), id, string(TriggerRunning))
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE triggers SET status=?, decision=?, session_id=?, updated_at=? WHERE id=? AND status=?`,
			string(status), decision, *session, millis(now), id, string(TriggerRunning))
	}
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("trigger #%d: %w", id, errSettled)
	}
	return nil
}
