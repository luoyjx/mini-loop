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
	// Shard names the database that holds the tenant's data, when the fleet
	// keeps tenants in a database; empty until the tenant is first opened and
	// while it is archived.
	Shard string `json:"shard,omitempty"`

	tokenHash string
	// archive names the tenant's object in the archive store while it is
	// archived there.
	archive string
}

// holds reports whether place is where the tenant's data is.
func (r TenantRecord) holds(place string) bool {
	return place == r.Shard || (r.State == TenantCold && r.archive != "" && place == archiveAt(r.archive))
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

// The registry is the part of the fleet's database every node reads. A row
// holds what the fleet needs while the tenant itself is closed: the credential
// hash, when to wake it, when its owner was last here, and where its data is.
// active marks a tenant some node opened, so a crash can be told from a clean
// sleep.
type registry struct{ db *fleetDB }

func (r *registry) Close() error { return r.db.Close() }

const tenantColumns = `id,token_hash,state,created_at,last_seen,next_wake,active,activations,shard,archive`

func scanTenant(row interface{ Scan(...any) error }) (TenantRecord, error) {
	var record TenantRecord
	var id, state string
	var created, seen, wake int64
	if err := row.Scan(&id, &record.tokenHash, &state, &created, &seen, &wake, &record.Active, &record.Activations, &record.Shard, &record.archive); err != nil {
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
	result, err := r.db.ExecContext(ctx, `INSERT INTO tenants(id,token_hash,state,created_at) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, string(id), tokenHash, string(TenantNew), millis(now))
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
// it, and reports whether the tenant row went. The tenant's data is in other
// places, which no transaction here reaches: each is written down as a
// leftover in the same transaction and returned, so that it goes even if this
// node stops before it has removed it.
func (r *registry) remove(ctx context.Context, id TenantID, node string, alive, now time.Time) (bool, []leftover, error) {
	tx, err := r.db.begin(ctx, false)
	if err != nil {
		return false, nil, err
	}
	fail := func(err error) (bool, []leftover, error) { return false, nil, errors.Join(err, tx.Rollback()) }
	var shard, archive string
	var born int64
	err = tx.QueryRowContext(ctx, `DELETE FROM tenants WHERE id=? AND NOT EXISTS (
		SELECT 1 FROM placements p JOIN nodes n ON n.id=p.node WHERE p.tenant_id=? AND p.node<>? AND n.beat>=?)
		RETURNING shard, created_at, archive`, string(id), string(id), node, millis(alive)).Scan(&shard, &born, &archive)
	removed := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fail(err)
	}
	// Usage a winding-down turn recorded after the tenant went goes too.
	if _, err := tx.ExecContext(ctx, `DELETE FROM usage WHERE tenant_id=? AND NOT EXISTS (SELECT 1 FROM tenants WHERE id=?)`, string(id), string(id)); err != nil {
		return fail(err)
	}
	left := []leftover{}
	if removed && shard != "" {
		left = append(left, leftover{id: id, born: born, place: shard})
		if _, err := tx.ExecContext(ctx, `UPDATE shards SET tenants=tenants-1 WHERE name=?`, shard); err != nil {
			return fail(err)
		}
	}
	if removed && archive != "" {
		left = append(left, leftover{id: id, born: born, place: archiveAt(archive)})
	}
	for _, l := range left {
		if err := discard(ctx, tx, l, now); err != nil {
			return fail(err)
		}
	}
	return removed, left, tx.Commit()
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
	tx, err := r.db.begin(ctx, false)
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
	if _, err := tx.ExecContext(ctx, `UPDATE tenants SET active=0, next_wake=?, last_seen=CASE WHEN last_seen>? THEN last_seen ELSE ? END WHERE id=?`, wake, unixOrZero(seen), unixOrZero(seen), string(id)); err != nil {
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
	_, err := r.db.ExecContext(ctx, `UPDATE tenants SET last_seen=CASE WHEN last_seen>? THEN last_seen ELSE ? END WHERE id=?`, millis(at), millis(at), string(id))
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

// freezable lists sleeping tenants with data in a shard whose owner was last
// seen before cutoff and that are not due to wake before soon. A tenant that
// is about to run would only be read back.
func (r *registry) freezable(ctx context.Context, cutoff, soon time.Time, limit int) ([]TenantID, error) {
	return r.ids(ctx, `SELECT id FROM tenants WHERE state=? AND active=0 AND last_seen<? AND shard<>'' AND (next_wake=0 OR next_wake>?)
		ORDER BY last_seen LIMIT ?`, string(TenantWarm), millis(cutoff), millis(soon), limit)
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

func (r *registry) addUsage(ctx context.Context, id TenantID, day string, calls, input, output int) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO usage(tenant_id,day,calls,input_tokens,output_tokens) VALUES(?,?,?,?,?)
		ON CONFLICT(tenant_id,day) DO UPDATE SET calls=usage.calls+excluded.calls, input_tokens=usage.input_tokens+excluded.input_tokens, output_tokens=usage.output_tokens+excluded.output_tokens`, string(id), day, calls, input, output)
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

// pruneUsage drops the usage rows of days before day, oldest first and one
// day to a statement: a fleet that turns retention on late has years of rows,
// and one statement for all of them would hold its locks for as long. What a
// pass leaves, the next takes.
func (r *registry) pruneUsage(ctx context.Context, day string) error {
	for range 16 {
		var oldest sql.NullString
		if err := r.db.QueryRowContext(ctx, `SELECT MIN(day) FROM usage`).Scan(&oldest); err != nil {
			return err
		}
		if !oldest.Valid || oldest.String >= day {
			return nil
		}
		// Every tenant's row for one day: more than the default allows.
		drop, cancel := context.WithTimeout(ctx, 5*time.Minute)
		_, err := r.db.ExecContext(drop, `DELETE FROM usage WHERE day=?`, oldest.String)
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}

// usageTotal sums every tenant's use on one day.
func (r *registry) usageTotal(ctx context.Context, day string) (TenantUsage, error) {
	usage := TenantUsage{Day: day}
	err := r.db.QueryRowContext(ctx, `SELECT CAST(COALESCE(SUM(calls),0) AS BIGINT),CAST(COALESCE(SUM(input_tokens),0) AS BIGINT),CAST(COALESCE(SUM(output_tokens),0) AS BIGINT) FROM usage WHERE day=?`, day).Scan(&usage.Calls, &usage.InputTokens, &usage.OutputTokens)
	return usage, err
}
