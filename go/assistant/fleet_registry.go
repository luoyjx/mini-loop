package assistant

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"time"
)

// TenantID names one owner's assistant inside a fleet. It appears in URLs and
// directory names, so it is restricted to a safe alphabet.
type TenantID string

var tenantIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,39}$`)

func (id TenantID) Valid() bool { return tenantIDPattern.MatchString(string(id)) }

// TenantState is where a tenant's data lives.
type TenantState string

const (
	// TenantNew is registered and has no files: nothing is created until
	// the owner's first request.
	TenantNew TenantState = "new"
	// TenantWarm has its data directory on disk.
	TenantWarm TenantState = "warm"
	// TenantCold was compressed into one archive after a long absence and
	// is unpacked on the owner's next request.
	TenantCold TenantState = "cold"
)

// TenantRecord is the fleet's row for one tenant. Times are nil when unset.
type TenantRecord struct {
	ID          TenantID    `json:"id"`
	State       TenantState `json:"state"`
	Active      bool        `json:"active"`
	CreatedAt   time.Time   `json:"created_at"`
	LastSeen    *time.Time  `json:"last_seen,omitempty"`
	NextWake    *time.Time  `json:"next_wake,omitempty"`
	Activations int         `json:"activations"`

	tokenHash string
}

// TenantUsage is one tenant's model use on one local day.
type TenantUsage struct {
	Day          string `json:"day"`
	Calls        int    `json:"calls"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

var (
	ErrTenantExists  = errors.New("tenant already exists")
	ErrTenantUnknown = errors.New("tenant not found")
)

// The registry is the fleet's only shared database. A row holds what the
// fleet needs while the tenant itself is closed: the credential hash, when to
// wake it, when its owner was last here, and where its data is. active marks a
// tenant this process opened, so a crash can be told from a clean sleep.
const registrySchema = `
CREATE TABLE IF NOT EXISTS tenants(
	id TEXT PRIMARY KEY, token_hash TEXT NOT NULL, state TEXT NOT NULL,
	created_at INTEGER NOT NULL, last_seen INTEGER NOT NULL DEFAULT 0,
	next_wake INTEGER NOT NULL DEFAULT 0, active INTEGER NOT NULL DEFAULT 0,
	activations INTEGER NOT NULL DEFAULT 0) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS tenants_wake ON tenants(next_wake) WHERE next_wake>0;
CREATE INDEX IF NOT EXISTS tenants_idle ON tenants(last_seen) WHERE state='warm';
CREATE TABLE IF NOT EXISTS usage(
	tenant_id TEXT NOT NULL, day TEXT NOT NULL, calls INTEGER NOT NULL DEFAULT 0,
	input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY(tenant_id, day)) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS tenants_active ON tenants(id) WHERE active=1;
CREATE TABLE IF NOT EXISTS nodes(
	id TEXT PRIMARY KEY, addr TEXT NOT NULL, started INTEGER NOT NULL, beat INTEGER NOT NULL,
	open INTEGER NOT NULL DEFAULT 0, max_active INTEGER NOT NULL DEFAULT 0) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS placements(
	tenant_id TEXT PRIMARY KEY, node TEXT NOT NULL, since INTEGER NOT NULL) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS placements_node ON placements(node);
`

type registry struct{ db *sql.DB }

func openRegistry(ctx context.Context, path string) (*registry, error) {
	db, err := openSQLite(ctx, path, registrySchema)
	if err != nil {
		return nil, err
	}
	return &registry{db}, nil
}

func (r *registry) Close() error { return r.db.Close() }

const tenantColumns = `id,token_hash,state,created_at,last_seen,next_wake,active,activations`

func scanTenant(row interface{ Scan(...any) error }) (TenantRecord, error) {
	var record TenantRecord
	var id, state string
	var created, seen, wake int64
	if err := row.Scan(&id, &record.tokenHash, &state, &created, &seen, &wake, &record.Active, &record.Activations); err != nil {
		return TenantRecord{}, err
	}
	record.ID, record.State, record.CreatedAt = TenantID(id), TenantState(state), fromMillis(created)
	if seen > 0 {
		at := fromMillis(seen)
		record.LastSeen = &at
	}
	if wake > 0 {
		at := fromMillis(wake)
		record.NextWake = &at
	}
	return record, nil
}

func (r *registry) create(ctx context.Context, id TenantID, tokenHash string, now time.Time) error {
	result, err := r.db.ExecContext(ctx, `INSERT OR IGNORE INTO tenants(id,token_hash,state,created_at) VALUES(?,?,?,?)`, string(id), tokenHash, string(TenantNew), millis(now))
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil || affected == 0 {
		return errors.Join(err, ErrTenantExists)
	}
	return nil
}

func (r *registry) get(ctx context.Context, id TenantID) (TenantRecord, bool, error) {
	record, err := scanTenant(r.db.QueryRowContext(ctx, `SELECT `+tenantColumns+` FROM tenants WHERE id=?`, string(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return TenantRecord{}, false, nil
	}
	return record, err == nil, err
}

// remove deletes a tenant's rows unless a live node other than node holds
// it, and reports whether the tenant row went.
func (r *registry) remove(ctx context.Context, id TenantID, node string, alive time.Time) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM tenants WHERE id=? AND NOT EXISTS (
		SELECT 1 FROM placements p JOIN nodes n ON n.id=p.node WHERE p.tenant_id=? AND p.node<>? AND n.beat>=?)`,
		string(id), string(id), node, millis(alive))
	if err != nil {
		return false, errors.Join(err, tx.Rollback())
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage WHERE tenant_id=? AND NOT EXISTS (SELECT 1 FROM tenants WHERE id=?)`, string(id), string(id)); err != nil {
		return false, errors.Join(err, tx.Rollback())
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return false, errors.Join(err, tx.Rollback())
	}
	return removed == 1, tx.Commit()
}

// list pages through the tenants in ID order, after the given ID.
func (r *registry) list(ctx context.Context, after TenantID, limit int) ([]TenantRecord, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+tenantColumns+` FROM tenants WHERE id>? ORDER BY id LIMIT ?`, string(after), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []TenantRecord{}
	for rows.Next() {
		record, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *registry) counts(ctx context.Context) (map[TenantState]int, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM tenants GROUP BY state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[TenantState]int{}
	for rows.Next() {
		var state string
		var count int
		if err := rows.Scan(&state, &count); err != nil {
			return nil, err
		}
		counts[TenantState(state)] = count
	}
	return counts, rows.Err()
}

// activated marks a tenant open in this process. Its wake time is cleared:
// while it runs, its own scheduler keeps time.
func (r *registry) activated(ctx context.Context, id TenantID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE tenants SET active=1, next_wake=0, state=?, activations=activations+1 WHERE id=?`, string(TenantWarm), string(id))
	return err
}

// hibernated records a clean sleep: it gives up the node's placement and
// writes when to wake the tenant (zero for never) and when its owner was last
// here. It reports false, and writes nothing, when the node no longer holds
// the tenant: the others took it for dead and one of them owns the row now.
func (r *registry) hibernated(ctx context.Context, id TenantID, node string, next, seen time.Time) (bool, error) {
	wake := int64(0)
	if !next.IsZero() {
		wake = millis(next)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM placements WHERE tenant_id=? AND node=?`, string(id), node)
	if err != nil {
		return false, errors.Join(err, tx.Rollback())
	}
	held, err := result.RowsAffected()
	if err != nil || held != 1 {
		return false, errors.Join(err, tx.Rollback())
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tenants SET active=0, next_wake=?, last_seen=MAX(last_seen,?) WHERE id=?`, wake, unixOrZero(seen), string(id)); err != nil {
		return false, errors.Join(err, tx.Rollback())
	}
	return true, tx.Commit()
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return millis(t)
}

func (r *registry) seen(ctx context.Context, id TenantID, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE tenants SET last_seen=MAX(last_seen,?) WHERE id=?`, millis(at), string(id))
	return err
}

// due lists sleeping tenants whose wake time has come, earliest first. An
// owner last seen before cutoff is dormant and is not woken.
func (r *registry) due(ctx context.Context, now, cutoff time.Time, limit int) ([]TenantID, error) {
	return r.ids(ctx, `SELECT id FROM tenants WHERE next_wake>0 AND next_wake<=? AND active=0 AND last_seen>=? ORDER BY next_wake LIMIT ?`, millis(now), unixOrZero(cutoff), limit)
}

// nudge makes a sleeping tenant due at now unless it is due already. A tenant
// that was never opened, or whose owner was last seen before cutoff, is left
// alone. It reports whether the tenant is due.
func (r *registry) nudge(ctx context.Context, id TenantID, now, cutoff time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE tenants SET next_wake=CASE WHEN next_wake BETWEEN 1 AND ? THEN next_wake ELSE ? END
		WHERE id=? AND active=0 AND state<>? AND last_seen>=?`, millis(now), millis(now), string(id), string(TenantNew), unixOrZero(cutoff))
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

// postpone moves a sleeping tenant's wake time, for one that failed to open.
func (r *registry) postpone(ctx context.Context, id TenantID, next time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE tenants SET next_wake=? WHERE id=? AND active=0`, millis(next), string(id))
	return err
}

// retire clears the wake time of every sleeping tenant whose owner was last
// seen before cutoff, and reports how many it cleared. They leave the wake
// index, so absent owners cost the waker nothing.
func (r *registry) retire(ctx context.Context, cutoff time.Time) (int, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE tenants SET next_wake=0 WHERE next_wake>0 AND active=0 AND last_seen<?`, millis(cutoff))
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	return int(affected), err
}

// archivable lists sleeping tenants with a data directory whose owner was
// last seen before cutoff.
func (r *registry) archivable(ctx context.Context, cutoff time.Time, limit int) ([]TenantID, error) {
	return r.ids(ctx, `SELECT id FROM tenants WHERE state=? AND active=0 AND last_seen<? ORDER BY last_seen LIMIT ?`, string(TenantWarm), millis(cutoff), limit)
}

// archived marks a tenant cold unless it was opened meanwhile.
func (r *registry) archived(ctx context.Context, id TenantID) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE tenants SET state=? WHERE id=? AND active=0 AND state=?`, string(TenantCold), string(id), string(TenantWarm))
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

func (r *registry) ids(ctx context.Context, query string, args ...any) ([]TenantID, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []TenantID{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, TenantID(id))
	}
	return ids, rows.Err()
}

func (r *registry) addUsage(ctx context.Context, id TenantID, day string, input, output int) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO usage(tenant_id,day,calls,input_tokens,output_tokens) VALUES(?,?,1,?,?)
		ON CONFLICT(tenant_id,day) DO UPDATE SET calls=calls+1, input_tokens=input_tokens+excluded.input_tokens, output_tokens=output_tokens+excluded.output_tokens`, string(id), day, input, output)
	return err
}

func (r *registry) usage(ctx context.Context, id TenantID, day string) (TenantUsage, error) {
	usage := TenantUsage{Day: day}
	err := r.db.QueryRowContext(ctx, `SELECT calls,input_tokens,output_tokens FROM usage WHERE tenant_id=? AND day=?`, string(id), day).Scan(&usage.Calls, &usage.InputTokens, &usage.OutputTokens)
	if errors.Is(err, sql.ErrNoRows) {
		return usage, nil
	}
	return usage, err
}

// usageTotal sums every tenant's use on one day.
func (r *registry) usageTotal(ctx context.Context, day string) (TenantUsage, error) {
	usage := TenantUsage{Day: day}
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(calls),0),COALESCE(SUM(input_tokens),0),COALESCE(SUM(output_tokens),0) FROM usage WHERE day=?`, day).Scan(&usage.Calls, &usage.InputTokens, &usage.OutputTokens)
	return usage, err
}
