package sqlitestate

import (
	"context"
	"database/sql"
	"errors"

	"github.com/luoyjx/mini-loop/go/agent"
)

func (s *Store) WriteApproval(ctx context.Context, record agent.ApprovalRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	if record.ToolUseIDNull && record.ToolUseID != "" {
		return errors.New("NULL approval tool-use ID also contains a value")
	}
	if err := validateText(string(record.ApprovalID), string(record.SessionID), record.ToolUseID, string(record.ToolName), record.Rule, record.Message, record.InputPreview, string(record.Status), string(record.Kind)); err != nil {
		return err
	}
	if record.Answer != nil {
		if err := validateText(*record.Answer); err != nil {
			return err
		}
	}
	toolUse := sql.NullString{String: record.ToolUseID, Valid: !record.ToolUseIDNull}
	_, err := s.conn.ExecContext(ctx, `INSERT INTO approvals
 (approval_id,session_id,tool_use_id,tool_name,rule,message,input_preview,status,created_at,resolved_at,kind,answer)
 VALUES (?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(approval_id) DO UPDATE SET
 status=excluded.status,resolved_at=excluded.resolved_at,answer=excluded.answer`,
		record.ApprovalID, record.SessionID, toolUse, record.ToolName, record.Rule, record.Message, record.InputPreview, record.Status, record.CreatedAt, record.ResolvedAt, record.Kind, record.Answer)
	return err
}

func (s *Store) ReadApprovals(ctx context.Context, session agent.SessionID, status *agent.ApprovalStatus) ([]agent.ApprovalRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if err := validateText(string(session)); err != nil {
		return nil, err
	}
	query := `SELECT approval_id,session_id,tool_use_id,tool_name,rule,message,input_preview,status,created_at,resolved_at,kind,answer FROM approvals WHERE session_id=?`
	var rows *sql.Rows
	var err error
	if status == nil {
		rows, err = s.conn.QueryContext(ctx, query+" ORDER BY created_at", session)
	} else {
		if err := validateText(string(*status)); err != nil {
			return nil, err
		}
		rows, err = s.conn.QueryContext(ctx, query+" AND status=? ORDER BY created_at", session, *status)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []agent.ApprovalRecord{}
	for rows.Next() {
		var record agent.ApprovalRecord
		var toolUse sql.NullString
		if err := rows.Scan(&record.ApprovalID, &record.SessionID, &toolUse, &record.ToolName, &record.Rule, &record.Message, &record.InputPreview, &record.Status, &record.CreatedAt, &record.ResolvedAt, &record.Kind, &record.Answer); err != nil {
			return nil, err
		}
		record.ToolUseID, record.ToolUseIDNull = toolUse.String, !toolUse.Valid
		result = append(result, record)
	}
	return result, rows.Err()
}
