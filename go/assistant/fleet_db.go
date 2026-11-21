package assistant

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

// The fleet's databases hold what must outlive any one node. The registry
// database holds the tenants' and the nodes' rows; the tenants' data lives in
// the vault (see vault), which is the registry database itself or a set of
// shards, each a database of its own. A database is either a SQLite file,
// which suits one host, or PostgreSQL, which every node connects to.
//
// The statements are written once, in the subset both engines accept, with ?
// placeholders. Times are milliseconds in BIGINT columns: PostgreSQL's
// INTEGER is 32 bits.

// fleetDB runs the fleet's statements on either engine.
type fleetDB struct {
	db       *sql.DB
	postgres bool
	bound    sync.Map
	// down makes every statement fail the way an unreachable database does;
	// tests set it.
	down atomic.Bool
}

// statementLimit bounds a statement whose caller set no deadline. A database
// behind a network that drops packets without a word never answers and never
// fails; the deadline is what lets the caller go on. Callers that move a
// tenant's data set their own, by its size (see bounded).
const statementLimit = 20 * time.Second

// timely gives ctx the default deadline when it has none. The deadline's
// resources are released when it passes, for results that are read after the
// statement returns.
func timely(ctx context.Context) context.Context {
	if _, set := ctx.Deadline(); set {
		return ctx
	}
	ctx, cancel := context.WithTimeout(ctx, statementLimit)
	context.AfterFunc(ctx, cancel)
	return ctx
}

// errUnreachable is what a statement returns while a test holds the database
// down. It reads as a timeout, which is what a lost database looks like.
var errUnreachable = fmt.Errorf("the database is not answering: %w", context.DeadlineExceeded)

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
	if d.down.Load() {
		return nil, errUnreachable
	}
	if _, set := ctx.Deadline(); !set {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, statementLimit)
		defer cancel()
	}
	return d.db.ExecContext(ctx, d.bind(query), args...)
}

func (d *fleetDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if d.down.Load() {
		return nil, errUnreachable
	}
	return d.db.QueryContext(timely(ctx), d.bind(query), args...)
}

func (d *fleetDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if d.down.Load() {
		// A row cannot be made with an error of our own; one asked for after
		// its deadline carries the same one.
		expired, cancel := context.WithDeadline(ctx, time.Unix(0, 0))
		defer cancel()
		return d.db.QueryRowContext(expired, d.bind(query), args...)
	}
	return d.db.QueryRowContext(timely(ctx), d.bind(query), args...)
}

func (d *fleetDB) Close() error { return d.db.Close() }

// clock reads the database's own clock. Nodes that judge each other by it
// need not agree about the time themselves. A SQLite file has no clock of its
// own and is used on one host, whose clock it is.
func (d *fleetDB) clock(ctx context.Context) (time.Time, error) {
	if !d.postgres {
		return time.Now(), nil
	}
	var now int64
	err := d.QueryRowContext(ctx, `SELECT CAST(EXTRACT(EPOCH FROM clock_timestamp())*1000 AS BIGINT)`).Scan(&now)
	return fromMillis(now), err
}

// fleetTx is one transaction on a fleet database.
type fleetTx struct {
	tx *sql.Tx
	db *fleetDB
}

// begin starts a transaction. A snapshot transaction sees one state of the
// database throughout and, in PostgreSQL, fails instead of writing over a row
// another transaction changed meanwhile; SQLite, whose transactions here take
// the write lock when they begin, gives both to every transaction.
func (d *fleetDB) begin(ctx context.Context, snapshot bool) (*fleetTx, error) {
	if d.down.Load() {
		return nil, errUnreachable
	}
	var options *sql.TxOptions
	if snapshot && d.postgres {
		options = &sql.TxOptions{Isolation: sql.LevelRepeatableRead}
	}
	// The transaction ends when its context does, so the deadline bounds all
	// of it.
	tx, err := d.db.BeginTx(timely(ctx), options)
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

// affected runs a statement that should change exactly one row and reports
// whether it did.
func (t *fleetTx) affected(ctx context.Context, query string, args ...any) (bool, error) {
	result, err := t.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

// dbTrouble reports whether an error says the database could not be reached
// or could not serve, as opposed to an answer the caller did not want. Only
// trouble counts against a shard's health.
func dbTrouble(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var local *fs.PathError
	if errors.As(err, &local) {
		// The node's own disk, not the database.
		return false
	}
	var answered *pgconn.PgError
	if errors.As(err, &answered) {
		// Connection exceptions, insufficient resources, operator
		// intervention, system errors and internal errors.
		switch answered.Code[:min(2, len(answered.Code))] {
		case "08", "53", "57", "58", "XX":
			return true
		}
		return false
	}
	var network net.Error
	var refused *pgconn.ConnectError
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) ||
		errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.As(err, &network) || errors.As(err, &refused)
}

// conflicted reports whether PostgreSQL ended a transaction because another
// one changed the same rows first: a serialization failure or a deadlock.
// Nothing of the transaction was kept, and the rows are not as it saw them.
func conflicted(err error) bool {
	var answered *pgconn.PgError
	return errors.As(err, &answered) && (answered.Code == "40001" || answered.Code == "40P01")
}

// ---- schema ----

// A database's tables are built by numbered steps, and the database records
// how many of each set it has run. A node runs the steps a database lacks
// when it first connects, and refuses a database that has run more steps than
// the node knows: an older node would write rows a newer one cannot trust.
// {rowid} stands for SQLite's WITHOUT ROWID and {bytes} for the engine's
// byte-string type.

// fleetVersion is what a node of this build writes in its row. The nodes of
// an earlier build write nothing there, which reads as zero.
const fleetVersion = 2

var registrySteps = [][]string{
	{
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
	},
	{
		// Where each tenant's data is, and the copies that must still go.
		`ALTER TABLE tenants ADD COLUMN shard TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE tenants ADD COLUMN archive TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS tenants_shard ON tenants(shard, id)`,
		`ALTER TABLE nodes ADD COLUMN version INTEGER NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS shards(
	name TEXT PRIMARY KEY, state TEXT NOT NULL, tenants BIGINT NOT NULL DEFAULT 0, created BIGINT NOT NULL){rowid}`,
		`CREATE TABLE IF NOT EXISTS garbage(
	tenant_id TEXT NOT NULL, born BIGINT NOT NULL, place TEXT NOT NULL, at BIGINT NOT NULL,
	PRIMARY KEY(tenant_id, born, place)){rowid}`,
		`CREATE INDEX IF NOT EXISTS usage_day ON usage(day)`,
		// Before shards, every tenant's data was in this database.
		`UPDATE vault_heads SET born=(SELECT created_at FROM tenants WHERE tenants.id=vault_heads.tenant_id)
	WHERE born=0 AND EXISTS (SELECT 1 FROM tenants WHERE tenants.id=vault_heads.tenant_id)`,
		`UPDATE tenants SET shard='` + homeShard + `' WHERE shard='' AND id IN (SELECT tenant_id FROM vault_heads)`,
	},
}

var vaultSteps = [][]string{
	{
		`CREATE TABLE IF NOT EXISTS vault_heads(
	tenant_id TEXT PRIMARY KEY, seq BIGINT NOT NULL, node TEXT NOT NULL, at BIGINT NOT NULL,
	bytes BIGINT NOT NULL){rowid}`,
		`CREATE TABLE IF NOT EXISTS vault_files(
	tenant_id TEXT NOT NULL, path TEXT NOT NULL, size BIGINT NOT NULL, seq BIGINT NOT NULL,
	PRIMARY KEY(tenant_id, path)){rowid}`,
		`CREATE TABLE IF NOT EXISTS vault_pages(
	tenant_id TEXT NOT NULL, path TEXT NOT NULL, idx INTEGER NOT NULL, data {bytes} NOT NULL,
	PRIMARY KEY(tenant_id, path, idx)){rowid}`,
	},
	{
		// born tells one tenant's data from that of a deleted tenant with the
		// same name; mark names the checkpoint a node's copy matches; sum is
		// the file's SHA-256.
		`ALTER TABLE vault_heads ADD COLUMN born BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE vault_heads ADD COLUMN mark TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE vault_files ADD COLUMN sum TEXT NOT NULL DEFAULT ''`,
	},
}

// schemaLock is the PostgreSQL advisory lock nodes take while they change the
// tables, so two that start together do not collide in the catalogue. It is
// held by the transaction, which a connection pooler passes through.
const schemaLock = 0x6d696e69_6c6f6f70

var errNewerSchema = errors.New("the database was written by a newer version of the fleet; upgrade this node")

// migrate runs the steps of one set that the database has not run yet.
func (d *fleetDB) migrate(ctx context.Context, set string, steps [][]string) (err error) {
	meta := `CREATE TABLE IF NOT EXISTS fleet_meta(name TEXT PRIMARY KEY, value BIGINT NOT NULL)`
	if !d.postgres {
		// Outside the transaction, which takes SQLite's write lock at once.
		if _, err := d.ExecContext(ctx, meta); err != nil {
			return err
		}
	}
	tx, err := d.begin(ctx, false)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()
	engine := strings.NewReplacer("{rowid}", " WITHOUT ROWID", "{bytes}", "BLOB")
	if d.postgres {
		engine = strings.NewReplacer("{rowid}", "", "{bytes}", "BYTEA")
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(?)`, int64(schemaLock)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, meta); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO fleet_meta(name,value) VALUES(?,0) ON CONFLICT(name) DO NOTHING`, set); err != nil {
		return err
	}
	var done int
	if err = tx.QueryRowContext(ctx, `SELECT value FROM fleet_meta WHERE name=?`, set).Scan(&done); err != nil {
		return err
	}
	if done > len(steps) {
		return errNewerSchema
	}
	for _, step := range steps[done:] {
		for _, statement := range step {
			if _, err = tx.ExecContext(ctx, engine.Replace(statement)); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE fleet_meta SET value=? WHERE name=?`, len(steps), set); err != nil {
		return err
	}
	return tx.Commit()
}

// databaseAt says where one database is: a PostgreSQL URL, or a SQLite file
// in a directory.
type databaseAt struct {
	url string
	dir string
}

// defaultConns is how many connections a node keeps to one PostgreSQL
// database. A node's statements are short, so the pool only has to cover the
// tenants that checkpoint at once and the fleet's own loops; every node's
// pools together must fit the server's max_connections.
const defaultConns = 8

// openFleetDB opens one of the fleet's databases and builds the tables it
// lacks: the vault's always, the registry's when it is the registry.
func openFleetDB(ctx context.Context, at databaseAt, registry bool, conns int) (*fleetDB, error) {
	var database *fleetDB
	if at.url == "" {
		db, err := openFleetFile(ctx, filepath.Join(at.dir, "fleet.db"))
		if err != nil {
			return nil, err
		}
		database = &fleetDB{db: db}
	} else {
		db, err := openPostgres(at.url, conns)
		if err != nil {
			return nil, err
		}
		database = &fleetDB{db: db, postgres: true}
	}
	// A database that cannot be reached should fail the caller, not hang it.
	prepare, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	err := database.migrate(prepare, "vault", vaultSteps)
	if err == nil && registry {
		err = database.migrate(prepare, "registry", registrySteps)
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("prepare the fleet database: %w", err), database.Close())
	}
	return database, nil
}

// openFleetFile opens a SQLite file whose transactions take the write lock
// when they begin, so one that reads before it writes never finds that
// another process wrote in between.
func openFleetFile(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", (&url.URL{Scheme: "file", Path: absolute, RawQuery: "_txlock=immediate"}).String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=NORMAL", "PRAGMA busy_timeout=5000"} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return nil, errors.Join(err, db.Close())
		}
	}
	return db, nil
}

// errDatabaseURL never echoes the address: it holds a password.
var errDatabaseURL = errors.New("a database URL must look like postgres://user:password@host:5432/name")

// checkDatabaseURL accepts what the driver accepts in URL form, which
// includes several hosts for a database with standbys:
// postgres://user:password@a:5432,b:5432/name?target_session_attrs=read-write.
func checkDatabaseURL(address string) error {
	_, rest, ok := strings.Cut(address, "://")
	if !ok || (!strings.HasPrefix(address, "postgres://") && !strings.HasPrefix(address, "postgresql://")) {
		return errDatabaseURL
	}
	// The hosts are what is between the credentials and the path. The
	// driver would fill in a default for none, from the node's environment.
	hosts, _, _ := strings.Cut(rest, "/")
	hosts, _, _ = strings.Cut(hosts, "?")
	if at := strings.LastIndex(hosts, "@"); at >= 0 {
		hosts = hosts[at+1:]
	}
	if _, err := pgx.ParseConfig(address); err != nil || hosts == "" {
		return errDatabaseURL
	}
	return nil
}

func openPostgres(address string, conns int) (*sql.DB, error) {
	if err := checkDatabaseURL(address); err != nil {
		return nil, err
	}
	config, err := pgx.ParseConfig(address)
	if err != nil {
		return nil, errDatabaseURL
	}
	if config.ConnectTimeout == 0 {
		// A host that does not answer must not hold a tenant's open.
		config.ConnectTimeout = 5 * time.Second
	}
	if config.RuntimeParams["application_name"] == "" {
		config.RuntimeParams["application_name"] = "miniloop-fleet"
	}
	if conns <= 0 {
		conns = defaultConns
	}
	db := stdlib.OpenDB(*config)
	db.SetMaxOpenConns(conns)
	db.SetMaxIdleConns(max(1, conns/4))
	db.SetConnMaxIdleTime(5 * time.Minute)
	return db, nil
}
