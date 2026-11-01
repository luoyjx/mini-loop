package assistant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// The watchdog judges whether a task's checks make progress. Each finished
// check is recorded with what the task then said it would check and its
// progress note. The agent's own stuck detector, which catches a turn that
// repeats one tool call, reads those records as steps: three checks in a row
// with the same check and note, or six that alternate between two states,
// mean the task is going nowhere. The fallback check and the six-check budget
// only keep a task from being forgotten or waking forever.
const (
	watchdogRepeat    = 3
	watchdogAlternate = 6
	taskCheckStep     = protocol.ToolName("task_check")
)

// ProgressRef names a task the watchdog flagged on the attention list.
func ProgressRef(task int64) string { return fmt.Sprintf("progress-%d", task) }

func watchdogDetector() agent.DefaultStuckDetector {
	detector, _ := agent.NewStuckDetector(agent.StuckThresholds{RepeatActionResult: watchdogRepeat, RepeatActionError: watchdogRepeat, Alternating: watchdogAlternate, Monologue: watchdogRepeat, UnproductiveTool: 0, MaxNudges: 0})
	return detector
}

// RecordTaskCheck snapshots a task after a turn that checked it.
func (s *Store) RecordTaskCheck(ctx context.Context, task, trigger int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO task_checks(task_id,trigger_id,check_note,note,status,at) SELECT id,?,check_note,note,status,? FROM tasks WHERE id=?`, trigger, millis(now), task)
	return err
}

type taskCheck struct {
	check, note string
}

func (s *Store) taskChecks(ctx context.Context, task int64) ([]taskCheck, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT check_note,note FROM (SELECT id,check_note,note FROM task_checks WHERE task_id=? ORDER BY id DESC LIMIT ?) ORDER BY id`, task, agent.StuckWindow)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []taskCheck{}
	for rows.Next() {
		var check taskCheck
		if err := rows.Scan(&check.check, &check.note); err != nil {
			return nil, err
		}
		result = append(result, check)
	}
	return result, rows.Err()
}

var watchdogText = map[agent.StuckPattern]string{
	agent.StuckRepeatResult: fmt.Sprintf("最近 %d 次检查要核对的内容和进展都没变", watchdogRepeat),
	agent.StuckAlternating:  fmt.Sprintf("最近 %d 次检查在两种状态之间来回", watchdogAlternate),
}

// Watchdog inspects every active task and flags the ones whose checks stopped
// making progress, once per streak: a ledger row, a channel line and an
// attention item. A flag clears when a later check makes progress or the task
// closes.
func (s *Store) Watchdog(ctx context.Context, now time.Time) (int, error) {
	tasks, err := s.ActiveTasks(ctx, "")
	if err != nil {
		return 0, err
	}
	detector := watchdogDetector()
	flagged := 0
	for _, task := range tasks {
		checks, err := s.taskChecks(ctx, task.ID)
		if err != nil {
			return flagged, err
		}
		steps := make([]agent.ToolStep, 0, len(checks))
		for _, check := range checks {
			input, err := agent.OutputStepHash(check.check)
			if err != nil {
				return flagged, err
			}
			output, err := agent.OutputStepHash(check.note)
			if err != nil {
				return flagged, err
			}
			steps = append(steps, agent.ToolStep{Name: taskCheckStep, InputHash: input, OutputHash: output})
		}
		signal, err := detector.Inspect(agent.StuckState{Steps: steps})
		if err != nil {
			return flagged, err
		}
		if signal == nil {
			if _, err := s.db.ExecContext(ctx, `DELETE FROM watchdog WHERE task_id=?`, task.ID); err != nil {
				return flagged, err
			}
			continue
		}
		raised, err := s.flagTask(ctx, task, *signal, now)
		if err != nil {
			return flagged, err
		}
		if raised {
			flagged++
		}
	}
	return flagged, nil
}

func (s *Store) flagTask(ctx context.Context, task Task, signal agent.StuckSignal, now time.Time) (bool, error) {
	detail := watchdogText[signal.Pattern]
	if detail == "" {
		detail = string(signal.Pattern)
	}
	raised := false
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO watchdog(task_id,pattern,detail,at) VALUES(?,?,?,?)`, task.ID, string(signal.Pattern), detail, millis(now))
		if err != nil {
			return err
		}
		if affected, err := result.RowsAffected(); err != nil || affected == 0 {
			return err
		}
		raised = true
		if err := undismiss(ctx, tx, ProgressRef(task.ID)); err != nil {
			return err
		}
		if err := appendLedger(ctx, tx, LedgerEntry{At: now, Kind: LedgerWatchdog, Actor: "watchdog", TriggerID: task.LastTrigger, Thread: task.Thread, SessionID: task.SessionID, Detail: fmt.Sprintf("任务 #%d 没有实质进展（%s，模式 %s）", task.ID, detail, signal.Pattern)}); err != nil {
			return err
		}
		_, err = postChannel(ctx, tx, ChannelMessage{Role: RoleSystem, Text: fmt.Sprintf("看门狗：任务 #%d「%s」%s，可能卡住了。", task.ID, task.Title, detail), TriggerID: task.LastTrigger}, now)
		return err
	})
	return raised, err
}

func (s *Store) watchdogFlag(ctx context.Context, task int64) (string, bool, error) {
	var detail string
	err := s.db.QueryRowContext(ctx, `SELECT detail FROM watchdog WHERE task_id=?`, task).Scan(&detail)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return detail, err == nil, err
}
