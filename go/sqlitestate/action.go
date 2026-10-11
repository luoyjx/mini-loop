package sqlitestate

import (
	"context"
	"database/sql"
	"errors"

	"github.com/luoyjx/mini-loop/go/agent"
)

var _ agent.StateStore = (*Store)(nil)

func (s *Store) ReadAction(ctx context.Context, id agent.ActionID) (agent.ActionRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return agent.ActionRecord{}, false, err
	}
	if err := validateText(string(id)); err != nil {
		return agent.ActionRecord{}, false, err
	}
	var record agent.ActionRecord
	err := s.conn.QueryRowContext(ctx, `SELECT action_id,session_id,message_id,tool_use_id,tool_name,input_hash,status,result,workflow_run_id,created_at,completed_at FROM actions WHERE action_id=?`, id).Scan(
		&record.ActionID, &record.SessionID, &record.MessageID, &record.ToolUseID, &record.ToolName, &record.InputHash, &record.Status, &record.Result, &record.WorkflowRunID, &record.CreatedAt, &record.CompletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return agent.ActionRecord{}, false, nil
	}
	if err != nil {
		return agent.ActionRecord{}, false, err
	}
	return record, true, nil
}

func (s *Store) WriteAction(ctx context.Context, record agent.ActionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	if err := validateText(string(record.ActionID), string(record.SessionID), string(record.MessageID), record.ToolUseID, string(record.ToolName), string(record.InputHash), string(record.Status)); err != nil {
		return err
	}
	if record.Result != nil {
		if err := validateText(*record.Result); err != nil {
			return err
		}
	}
	if record.WorkflowRunID != nil {
		if err := validateText(string(*record.WorkflowRunID)); err != nil {
			return err
		}
	}
	_, err := s.conn.ExecContext(ctx, `INSERT INTO actions
 (action_id,session_id,message_id,tool_use_id,tool_name,input_hash,status,result,workflow_run_id,created_at,completed_at)
 VALUES (?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(action_id) DO UPDATE SET
 status=excluded.status,result=excluded.result,workflow_run_id=excluded.workflow_run_id,completed_at=excluded.completed_at`,
		record.ActionID, record.SessionID, record.MessageID, record.ToolUseID, record.ToolName, record.InputHash, record.Status, record.Result, record.WorkflowRunID, record.CreatedAt, record.CompletedAt)
	return err
}

// MarkInflightUnknown is an explicit recovery policy. Opening the database never
// runs it; it does not establish that a foreign process or external effect stopped.
func (s *Store) MarkInflightUnknown(ctx context.Context, session *agent.SessionID) ([]agent.ActionID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if session != nil {
		if err := validateText(string(*session)); err != nil {
			return nil, err
		}
	}
	ids := []agent.ActionID{}
	err := s.withImmediate(ctx, func() error {
		query := "SELECT action_id FROM actions WHERE status='started'"
		var rows *sql.Rows
		var err error
		if session == nil {
			rows, err = s.conn.QueryContext(ctx, query)
		} else {
			rows, err = s.conn.QueryContext(ctx, query+" AND session_id=?", *session)
		}
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id agent.ActionID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := s.conn.ExecContext(ctx, "UPDATE actions SET status='unknown' WHERE action_id=?", id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}
