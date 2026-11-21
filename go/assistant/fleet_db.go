package assistant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// The fleet's database holds what must outlive any one node: the registry of
// tenants and nodes and, when the fleet keeps tenants in it (see vault), the
// tenants' data. It is either fleet.db in the data directory, which suits one
// host, or a PostgreSQL database that every node connects to, which lets the
// nodes run on separate machines with nothing durable on their own disks.
//
// The statements are written once, in the subset both engines accept, with ?
// placeholders. Times are milliseconds in BIGINT columns: PostgreSQL's
// INTEGER is 32 bits.

// fleetDB runs the fleet's statements on either engine.
type fleetDB struct {
	db       *sql.DB
	postgres bool
	bound    sync.Map
}

// Engine names the database for the operator's view.
func (d *fleetDB) Engine() string {
	if d.postgres {
		return "postgres"
	}
	return "sqlite"
}

// bind rewrites ? placeholders to PostgreSQL's numbered ones. No statement
// here has a question mark inside a literal.
func (d *fleetDB) bind(query string) string {
	if !d.postgres {
		return query
	}
	if bound, ok := d.bound.Load(query); ok {
		return bound.(string)
	}
	var b strings.Builder
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	d.bound.Store(query, b.String())
	return b.String()
}

func (d *fleetDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.db.ExecContext(ctx, d.bind(query), args...)
}

func (d *fleetDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.db.QueryContext(ctx, d.bind(query), args...)
}

func (d *fleetDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.db.QueryRowContext(ctx, d.bind(query), args...)
}

func (d *fleetDB) Close() error { return d.db.Close() }

// fleetTx is one transaction on the fleet's database.
type fleetTx struct {
	tx *sql.Tx
	db *fleetDB
}

// begin starts a transaction. A snapshot transaction sees one state of the
// database throughout and, in PostgreSQL, fails instead of writing over a row
// another transaction changed meanwhile; SQLite, with its single writer,
// gives both to every transaction.
func (d *fleetDB) begin(ctx context.Context, snapshot bool) (*fleetTx, error) {
	var options *sql.TxOptions
	if snapshot && d.postgres {
		options = &sql.TxOptions{Isolation: sql.LevelRepeatableRead}
	}
	tx, err := d.db.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &fleetTx{tx: tx, db: d}, nil
}

func (t *fleetTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, t.db.bind(query), args...)
}

func (t *fleetTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, t.db.bind(query), args...)
}

func (t *fleetTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, t.db.bind(query), args...)
}

func (t *fleetTx) Commit() error   { return t.tx.Commit() }
func (t *fleetTx) Rollback() error { return t.tx.Rollback() }

// fleetSchema is the registry and the vault. {rowid} stands for SQLite's
// WITHOUT ROWID and {bytes} for the engine's byte-string type.
var fleetSchema = []string{
	`CREATE TABLE IF NOT EXISTS tenants(
	id TEXT PRIMARY KEY, token_hash TEXT NOT NULL, state TEXT NOT NULL,
	created_at BIGINT NOT NULL, last_seen BIGINT NOT NULL DEFAULT 0,
	next_wake BIGINT NOT NULL DEFAULT 0, active INTEGER NOT NULL DEFAULT 0,
	activations INTEGER NOT NULL DEFAULT 0){rowid}`,
	`CREATE INDEX IF NOT EXISTS tenants_wake ON tenants(next_wake) WHERE next_wake>0`,
	`CREATE INDEX IF NOT EXISTS tenants_idle ON tenants(last_seen) WHERE state='warm'`,
	`CREATE TABLE IF NOT EXISTS usage(
	tenant_id TEXT NOT NULL, day TEXT NOT NULL, calls INTEGER NOT NULL DEFAULT 0,
	input_tokens BIGINT NOT NULL DEFAULT 0, output_tokens BIGINT NOT NULL DEFAULT 0,
	PRIMARY KEY(tenant_id, day)){rowid}`,
	`CREATE INDEX IF NOT EXISTS tenants_active ON tenants(id) WHERE active=1`,
	`CREATE TABLE IF NOT EXISTS nodes(
	id TEXT PRIMARY KEY, addr TEXT NOT NULL, started BIGINT NOT NULL, beat BIGINT NOT NULL,
	open INTEGER NOT NULL DEFAULT 0, max_active INTEGER NOT NULL DEFAULT 0){rowid}`,
	`CREATE TABLE IF NOT EXISTS placements(
	tenant_id TEXT PRIMARY KEY, node TEXT NOT NULL, since BIGINT NOT NULL){rowid}`,
	`CREATE INDEX IF NOT EXISTS placements_node ON placements(node)`,
	`CREATE TABLE IF NOT EXISTS vault_heads(
	tenant_id TEXT PRIMARY KEY, seq BIGINT NOT NULL, node TEXT NOT NULL, at BIGINT NOT NULL,
	bytes BIGINT NOT NULL){rowid}`,
	`CREATE TABLE IF NOT EXISTS vault_files(
	tenant_id TEXT NOT NULL, path TEXT NOT NULL, size BIGINT NOT NULL, seq BIGINT NOT NULL,
	PRIMARY KEY(tenant_id, path)){rowid}`,
	`CREATE TABLE IF NOT EXISTS vault_pages(
	tenant_id TEXT NOT NULL, path TEXT NOT NULL, idx INTEGER NOT NULL, data {bytes} NOT NULL,
	PRIMARY KEY(tenant_id, path, idx)){rowid}`,
}

// schemaLock is the PostgreSQL advisory lock nodes take while they create the
// tables, so two that start together do not collide in the catalogue.
const schemaLock = 0x6d696e69_6c6f6f70

// openFleetDB opens fleet.db in dir, or the PostgreSQL database at address,
// and creates the tables that are missing.
func openFleetDB(ctx context.Context, dir, address string) (*fleetDB, error) {
	if address == "" {
		statements := make([]string, len(fleetSchema))
		engine := strings.NewReplacer("{rowid}", " WITHOUT ROWID", "{bytes}", "BLOB")
		for i, statement := range fleetSchema {
			statements[i] = engine.Replace(statement)
		}
		db, err := openSQLite(ctx, filepath.Join(dir, "fleet.db"), strings.Join(statements, ";\n"))
		if err != nil {
			return nil, err
		}
		return &fleetDB{db: db}, nil
	}
	if err := checkDatabaseURL(address); err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", address)
	if err != nil {
		return nil, errors.New("the database URL could not be parsed")
	}
	// A node's statements are short; the pool only has to cover the tenants
	// that checkpoint at once and the fleet's own loops.
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)
	// A database that cannot be reached should stop the node from starting,
	// not hang it.
	prepare, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := createPostgresSchema(prepare, db); err != nil {
		return nil, errors.Join(fmt.Errorf("prepare the fleet database: %w", err), db.Close())
	}
	return &fleetDB{db: db, postgres: true}, nil
}

func checkDatabaseURL(address string) error {
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" {
		// The address holds a password: never echo it.
		return errors.New("the database URL must look like postgres://user:password@host:5432/name")
	}
	return nil
}

func createPostgresSchema(ctx context.Context, db *sql.DB) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, int64(schemaLock)); err != nil {
		return err
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, int64(schemaLock))
	engine := strings.NewReplacer("{rowid}", "", "{bytes}", "BYTEA")
	for _, statement := range fleetSchema {
		if _, err := conn.ExecContext(ctx, engine.Replace(statement)); err != nil {
			return err
		}
	}
	return nil
}
