// Package sqlitestate opens and upgrades the durable conversation database.
// Record operations and agent.StateStore composition are separate port slices.
package sqlitestate

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/mattn/go-sqlite3"
)

// SchemaVersion is the durable file format version, independent of Go releases.
type SchemaVersion int64

const CurrentSchema SchemaVersion = 7

//go:embed schema.sql
var schemaSQL string

// SchemaError names a database that this implementation refuses to open.
type SchemaError struct {
	Path  string
	Found string
	Cause error
}

func (e *SchemaError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s is not readable as a SQLite database (%v). Likely a corrupt or foreign file at the state path -- a half-written database from a killed process, or something else entirely. Move it aside to start fresh; rows inside it are not recoverable through this store.", e.Path, e.Cause)
	}
	return fmt.Sprintf("%s was written by schema v%s; this build understands v%d. Refusing to open it rather than silently dropping fields.", e.Path, e.Found, CurrentSchema)
}

func (e *SchemaError) Unwrap() error { return e.Cause }

// Store owns a single pinned connection: connection-local PRAGMAs and later
// transactions cannot migrate onto an unconfigured connection from a pool.
type Store struct {
	mu   sync.Mutex
	db   *sql.DB
	conn *sql.Conn
}

// Open creates parent directories and applies the Source schema-v7 migration.
// An open failure can leave additive DDL applied, as in Python; migrations are
// not a fleet transaction. Successful callers own Close. No Manager is activated.
func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	dsn := path
	if path != ":memory:" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		// Treat '?' and '#' as filename bytes, never driver configuration.
		dsn = (&url.URL{Scheme: "file", Path: absolute}).String()
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		return nil, classifyOpenError(path, err)
	}
	store := &Store{db: db, conn: conn}
	for _, pragma := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=NORMAL", "PRAGMA foreign_keys=ON"} {
		if _, err = conn.ExecContext(ctx, pragma); err != nil {
			break
		}
	}
	if err == nil {
		err = store.migrate(ctx, path)
	}
	if err != nil {
		return nil, errors.Join(classifyOpenError(path, err), store.Close())
	}
	return store, nil
}

func classifyOpenError(path string, err error) error {
	var databaseError sqlite3.Error
	if errors.As(err, &databaseError) && (databaseError.Code == sqlite3.ErrNotADB || databaseError.Code == sqlite3.ErrCorrupt) {
		return &SchemaError{Path: path, Cause: err}
	}
	return err
}

// Close is idempotent and releases both the pinned connection and the pool.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil
	}
	err := errors.Join(s.conn.Close(), s.db.Close())
	s.conn, s.db = nil, nil
	return err
}
