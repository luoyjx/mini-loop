package sqlitestate

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

var _ agent.SessionStore = (*Store)(nil)

func (s *Store) UpsertSession(ctx context.Context, record agent.SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	if err := validateText(string(record.SessionID), record.Workspace, string(record.Status), string(record.Owner)); err != nil {
		return err
	}
	if record.System != nil {
		if err := validateText(*record.System); err != nil {
			return err
		}
	}
	todos := make([]jsonvalue.Value, 0, len(record.Todos))
	for _, item := range record.Todos {
		todos = append(todos, jsonvalue.ObjectValue([]jsonvalue.Field{
			{Name: "content", Value: jsonvalue.TextValue(item.Content)},
			{Name: "status", Value: jsonvalue.TextValue(string(item.Status))},
			{Name: "activeForm", Value: jsonvalue.TextValue(item.ActiveForm)},
		}))
	}
	todoJSON, err := jsonvalue.ArrayValue(todos).MarshalUTF8Spaced()
	if err != nil {
		return err
	}
	steering := make([]jsonvalue.Value, 0, len(record.PendingSteering))
	for _, item := range record.PendingSteering {
		steering = append(steering, jsonvalue.TextValue(item))
	}
	steeringJSON, err := jsonvalue.ArrayValue(steering).MarshalUTF8Spaced()
	if err != nil {
		return err
	}
	var system sql.NullString
	if record.System != nil {
		system = sql.NullString{String: *record.System, Valid: true}
	}
	_, err = s.conn.ExecContext(ctx, `INSERT INTO sessions
 (session_id, workspace, system, created_at, run_count, status, todos, owner, pending_steering, workspace_bound)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
 ON CONFLICT(session_id) DO UPDATE SET
 workspace=excluded.workspace, system=excluded.system, run_count=excluded.run_count,
 status=excluded.status, todos=excluded.todos, owner=excluded.owner,
 pending_steering=excluded.pending_steering, workspace_bound=excluded.workspace_bound`,
		record.SessionID, record.Workspace, system, record.CreatedAt, record.RunCount,
		record.Status, string(todoJSON), record.Owner, string(steeringJSON), record.WorkspaceBound)
	return err
}

// LoadSessions returns detached inert metadata. Event cursors are read from the
// event table after session rows have been fetched, matching the Source lock scope.
func (s *Store) LoadSessions(ctx context.Context) ([]agent.SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	columns, err := s.columns(ctx, sessionsTable)
	if err != nil {
		return nil, err
	}
	owner, steering, bound := "'anonymous'", "'[]'", "0"
	if columns["owner"] {
		owner = "owner"
	}
	if columns["pending_steering"] {
		steering = "pending_steering"
	}
	if columns["workspace_bound"] {
		bound = "workspace_bound"
	}
	rows, err := s.conn.QueryContext(ctx, "SELECT session_id, workspace, system, created_at, run_count, status, todos, "+owner+", "+steering+", "+bound+" FROM sessions ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []agent.SessionRecord{}
	for rows.Next() {
		var record agent.SessionRecord
		var system, todos, pending sql.NullString
		var workspaceBound sqliteTruth
		if err := rows.Scan(&record.SessionID, &record.Workspace, &system, &record.CreatedAt, &record.RunCount, &record.Status, &todos, &record.Owner, &pending, &workspaceBound); err != nil {
			return nil, err
		}
		if system.Valid {
			record.System = &system.String
		}
		record.WorkspaceBound = bool(workspaceBound)
		record.Todos, err = decodeTodos(todos.String)
		if err != nil {
			return nil, fmt.Errorf("session %q todos: %w", record.SessionID, err)
		}
		record.PendingSteering, err = decodeSteering(pending.String)
		if err != nil {
			return nil, fmt.Errorf("session %q steering: %w", record.SessionID, err)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range result {
		if err := s.conn.QueryRowContext(ctx, "SELECT COALESCE(MAX(ordinal), 0) FROM events WHERE session_id=?", result[i].SessionID).Scan(&result[i].EventCursor); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// DeleteSession atomically removes operational rows and leaves the action and
// approval reconciliation trail intact. It does not reclaim workspace files.
func (s *Store) DeleteSession(ctx context.Context, id agent.SessionID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	if err := validateText(string(id)); err != nil {
		return err
	}
	return s.withImmediate(ctx, func() error {
		for _, statement := range []string{
			"DELETE FROM messages WHERE session_id=?",
			"DELETE FROM events WHERE session_id=?",
			"DELETE FROM sessions WHERE session_id=?",
		} {
			if _, err := s.conn.ExecContext(ctx, statement, id); err != nil {
				return err
			}
		}
		return nil
	})
}
