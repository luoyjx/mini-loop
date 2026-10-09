package sqlitestate

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"math/big"
)

func (s *Store) migrate(ctx context.Context, path string) error {
	if _, err := s.conn.ExecContext(ctx, schemaSQL); err != nil {
		return err
	}
	var version schemaValue
	err := s.conn.QueryRowContext(ctx, "SELECT version FROM schema_version").Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.conn.ExecContext(ctx, "INSERT INTO schema_version (version) VALUES (?)", CurrentSchema)
	} else if err == nil {
		if version.integer.Cmp(big.NewInt(int64(CurrentSchema))) > 0 {
			return &SchemaError{Path: path, Found: version.integer.String()}
		}
		// Every negative version follows the same additive branches. Preserve
		// unbounded positive identities in refusal errors before narrowing.
		found := SchemaVersion(math.MinInt64)
		if version.integer.IsInt64() {
			found = SchemaVersion(version.integer.Int64())
		}
		if found < CurrentSchema {
			err = s.upgrade(ctx, found)
		}
	}
	if err != nil {
		return err
	}
	for _, statement := range []string{
		"CREATE INDEX IF NOT EXISTS messages_epoch ON messages (session_id, epoch, ordinal)",
		"CREATE INDEX IF NOT EXISTS actions_session ON actions (session_id, status)",
	} {
		if _, err := s.conn.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

type tableName string

const (
	sessionsTable  tableName = "sessions"
	approvalsTable tableName = "approvals"
	messagesTable  tableName = "messages"
)

func (s *Store) columns(ctx context.Context, table tableName) (map[string]bool, error) {
	rows, err := s.conn.QueryContext(ctx, "PRAGMA table_info("+string(table)+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var index, required, primary int
		var name, kind string
		var defaultValue sql.NullString
		if err := rows.Scan(&index, &name, &kind, &required, &defaultValue, &primary); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

type columnAddition struct{ name, statement string }

// addMissing accepts only internal fixed SQL, never an identifier supplied by a
// database row. Column inventories determine presence without generating DDL.
func (s *Store) addMissing(ctx context.Context, table tableName, requireTable bool, additions ...columnAddition) error {
	columns, err := s.columns(ctx, table)
	if err != nil {
		return err
	}
	if requireTable && len(columns) == 0 {
		return nil
	}
	for _, addition := range additions {
		if !columns[addition.name] {
			if _, err := s.conn.ExecContext(ctx, addition.statement); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) upgrade(ctx context.Context, found SchemaVersion) error {
	if err := s.addMissing(ctx, sessionsTable, true,
		columnAddition{"owner", "ALTER TABLE sessions ADD COLUMN owner TEXT NOT NULL DEFAULT 'anonymous'"}); err != nil {
		return err
	}
	if err := s.addMissing(ctx, approvalsTable, true,
		columnAddition{"kind", "ALTER TABLE approvals ADD COLUMN kind TEXT NOT NULL DEFAULT 'approval'"},
		columnAddition{"answer", "ALTER TABLE approvals ADD COLUMN answer TEXT"}); err != nil {
		return err
	}
	if found < 5 {
		if err := s.addMissing(ctx, sessionsTable, false,
			columnAddition{"lease_owner", "ALTER TABLE sessions ADD COLUMN lease_owner TEXT"},
			columnAddition{"lease_until", "ALTER TABLE sessions ADD COLUMN lease_until REAL NOT NULL DEFAULT 0"}); err != nil {
			return err
		}
	}
	if found < 4 {
		if _, err := s.conn.ExecContext(ctx, actionSchemaSQL); err != nil {
			return err
		}
	}
	if found < 3 {
		if err := s.addMissing(ctx, sessionsTable, false,
			columnAddition{"todos", "ALTER TABLE sessions ADD COLUMN todos TEXT NOT NULL DEFAULT '[]'"}); err != nil {
			return err
		}
	}
	if err := s.addMissing(ctx, sessionsTable, false,
		columnAddition{"pending_steering", "ALTER TABLE sessions ADD COLUMN pending_steering TEXT NOT NULL DEFAULT '[]'"},
		columnAddition{"workspace_bound", "ALTER TABLE sessions ADD COLUMN workspace_bound INTEGER NOT NULL DEFAULT 0"}); err != nil {
		return err
	}
	if found < 2 {
		if err := s.addMissing(ctx, messagesTable, false,
			columnAddition{"epoch", "ALTER TABLE messages ADD COLUMN epoch INTEGER NOT NULL DEFAULT 1"}); err != nil {
			return err
		}
	}
	_, err := s.conn.ExecContext(ctx, "UPDATE schema_version SET version = ?", CurrentSchema)
	return err
}

const actionSchemaSQL = `CREATE TABLE IF NOT EXISTS actions (
 action_id TEXT PRIMARY KEY,
 session_id TEXT NOT NULL,
 message_id TEXT NOT NULL,
 tool_use_id TEXT NOT NULL,
 tool_name TEXT NOT NULL,
 input_hash TEXT NOT NULL,
 status TEXT NOT NULL,
 result TEXT,
 workflow_run_id TEXT,
 created_at REAL NOT NULL,
 completed_at REAL
);`
