package assistant

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// A fleet with many tenants keeps their data in several databases, the
// shards, so that no one database has to hold or serve all of it. The
// registry database stays small: one row per tenant, which names the tenant's
// shard. Every statement about a tenant's data touches that one shard, so a
// shard that is slow or lost holds up only its own tenants, and a node starts
// and serves the others without it.
//
//   - A tenant gets its shard when it is first opened: the open shard with
//     the fewest tenants. A shard that is new to the fleet takes no tenants
//     until an operator opens it, by which time every node knows its address.
//   - A tenant moves between shards while it sleeps. Emptying a shard
//     ("draining") moves its tenants to the open ones.
//   - The node that writes a tenant's data is named in the tenant's head row
//     in its shard. A node becomes that writer when it opens the tenant, and a
//     checkpoint from any other node is refused. The registry's placement says
//     who may open the tenant; the head says who may write it, in the same
//     database and the same transaction as the write.
//
// The registry and a shard are separate databases and no transaction spans
// them. What is done in two steps is made safe by writing the intent in the
// registry first: a copy of a tenant's data that must go (the shard a tenant
// was moved from, the archive of a tenant that was read back, everything of a
// deleted tenant) is a row in the registry's garbage table until it is gone.

const (
	// homeShard is the registry database itself, where tenants' data lived
	// before the fleet had shards and still lives when it has none.
	homeShard = "home"
	// coldPlace starts the place of a leftover in the archive store; no
	// shard's name starts so.
	coldPlace = "~cold"
	// shardTroubles is how many statements in a row a shard may fail to
	// answer before the node stops asking it for shardCooldown.
	shardTroubles = 3
	shardCooldown = 5 * time.Second
)

var shardNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,30}$`)

// ShardState says what a shard is for.
type ShardState string

const (
	// ShardOpen takes new tenants.
	ShardOpen ShardState = "open"
	// ShardClosed keeps the tenants it has and takes no new ones.
	ShardClosed ShardState = "closed"
	// ShardDraining is closed, and its sleeping tenants are moved away.
	ShardDraining ShardState = "draining"
	// ShardRetired is empty for good; nodes need not know its address.
	ShardRetired ShardState = "retired"
)

// ShardInfo is the operator's view of one shard.
type ShardInfo struct {
	Name    string     `json:"name"`
	State   ShardState `json:"state"`
	Tenants int64      `json:"tenants"`
	// Configured says this node has the shard's address; Down that the shard
	// did not answer this node's last statements.
	Configured bool `json:"configured"`
	Down       bool `json:"down"`
}

var (
	// ErrShardUnknown means the registry has no shard of that name.
	ErrShardUnknown = errors.New("shard not found")
	// ErrShardInUse refuses a change that would strand tenants.
	ErrShardInUse = errors.New("the shard still holds tenants or copies that must go")
	// errNoShard means no shard takes new tenants.
	errNoShard = errors.New("no shard takes new tenants")
	// errShardDown is the fast answer while a shard is not asked.
	errShardDown = errors.New("the tenant's shard is not answering")
)

// leftover is a copy of a tenant's data that must go: in a shard, or one
// object in the archive store (archiveAt). born tells the tenant from a later
// one with the same name.
type leftover struct {
	id    TenantID
	born  int64
	place string
}

// archiveAt is the place of the archive a tenant was given under mark. Every
// archiving writes an object of its own, so that removing an old one, however
// late, cannot remove the tenant's archive.
func archiveAt(mark string) string { return coldPlace + "/" + mark }

// archiveMark is the mark of the archive a place names, if it names one.
func archiveMark(place string) (string, bool) { return strings.CutPrefix(place, coldPlace+"/") }

// heldBy is the condition every statement that says where a tenant's data is
// carries: the node still holds the tenant. A node that was taken for dead in
// the middle of a move or an archiving has lost the placement, and what it
// then believes about the tenant may be out of date.
const heldBy = ` AND EXISTS (SELECT 1 FROM placements WHERE placements.tenant_id=tenants.id AND placements.node=?)`

func discard(ctx context.Context, tx *fleetTx, l leftover, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO garbage(tenant_id,born,place,at) VALUES(?,?,?,?) ON CONFLICT(tenant_id,born,place) DO NOTHING`,
		string(l.id), l.born, l.place, millis(now))
	return err
}

// ---- the catalogue, in the registry ----

// enroll enters the shards this node knows. A fleet's first shards are open.
// A shard that joins later is closed: the nodes that have not been told its
// address yet could not open a tenant placed in it.
func (r *registry) enroll(ctx context.Context, names []string, now time.Time) (err error) {
	tx, err := r.db.begin(ctx, false)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()
	var known int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM shards`).Scan(&known); err != nil {
		return err
	}
	fresh, home := ShardClosed, ShardClosed
	if known == 0 {
		fresh = ShardOpen
		if len(names) == 0 {
			home = ShardOpen
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO shards(name,state,tenants,created) VALUES(?,?,(SELECT COUNT(*) FROM tenants WHERE shard=?),?)
		ON CONFLICT(name) DO NOTHING`, homeShard, string(home), homeShard, millis(now)); err != nil {
		return err
	}
	for _, name := range names {
		if _, err = tx.ExecContext(ctx, `INSERT INTO shards(name,state,tenants,created) VALUES(?,?,0,?) ON CONFLICT(name) DO NOTHING`, name, string(fresh), millis(now)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *registry) shards(ctx context.Context) ([]ShardInfo, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT name,state,tenants FROM shards ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	shards := []ShardInfo{}
	for rows.Next() {
		var shard ShardInfo
		var state string
		if err := rows.Scan(&shard.Name, &state, &shard.Tenants); err != nil {
			return nil, err
		}
		shard.State = ShardState(state)
		shards = append(shards, shard)
	}
	return shards, rows.Err()
}

// setShard changes what a shard is for. A shard is retired only once nothing
// of any tenant is left in it, and stays retired; the fleet always keeps one
// shard open.
func (r *registry) setShard(ctx context.Context, name string, state ShardState) (err error) {
	tx, err := r.db.begin(ctx, false)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()
	var current string
	var tenants int64
	// The write comes first and takes the row.
	if known, err := tx.affected(ctx, `UPDATE shards SET state=state WHERE name=?`, name); err != nil || !known {
		return errors.Join(err, ErrShardUnknown)
	}
	if err = tx.QueryRowContext(ctx, `SELECT state,tenants FROM shards WHERE name=?`, name).Scan(&current, &tenants); err != nil {
		return err
	}
	switch {
	case ShardState(current) == state:
		return tx.Commit()
	case ShardState(current) == ShardRetired:
		return fmt.Errorf("shard %s is retired for good; enrol its database under a new name", name)
	case state == ShardRetired:
		var pending, assigned int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM garbage WHERE place=?`, name).Scan(&pending); err != nil {
			return err
		}
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenants WHERE shard=?`, name).Scan(&assigned); err != nil {
			return err
		}
		if pending > 0 || assigned > 0 {
			return fmt.Errorf("%w: %d tenants and %d copies in %s", ErrShardInUse, assigned, pending, name)
		}
	}
	if ShardState(current) == ShardOpen {
		var others int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM shards WHERE state=? AND name<>?`, string(ShardOpen), name).Scan(&others); err != nil {
			return err
		}
		if others == 0 {
			return fmt.Errorf("shard %s is the only one that takes new tenants; open another first", name)
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE shards SET state=? WHERE name=?`, string(state), name); err != nil {
		return err
	}
	return tx.Commit()
}

// assign gives a tenant that has none a shard: the open one with the fewest
// tenants. The caller holds the tenant's placement.
func (r *registry) assign(ctx context.Context, id TenantID) (shard string, err error) {
	tx, err := r.db.begin(ctx, false)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()
	var born int64
	if err = tx.QueryRowContext(ctx, `SELECT shard,created_at FROM tenants WHERE id=?`, string(id)).Scan(&shard, &born); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrTenantUnknown
		}
		return "", err
	}
	if shard != "" {
		return shard, tx.Commit()
	}
	var legacy int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM vault_heads WHERE tenant_id=? AND born=0`, string(id)).Scan(&legacy); err != nil {
		return "", err
	}
	if legacy > 0 {
		// A node from before shards wrote the tenant's data here.
		shard = homeShard
	} else if err = tx.QueryRowContext(ctx, `SELECT name FROM shards WHERE state=? ORDER BY tenants, name LIMIT 1`, string(ShardOpen)).Scan(&shard); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = errNoShard
		}
		return "", err
	}
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`UPDATE tenants SET shard=? WHERE id=?`, []any{shard, string(id)}},
		{`UPDATE shards SET tenants=tenants+1 WHERE name=?`, []any{shard}},
		// What an earlier stay left there is in the tenant's place again;
		// the caller empties it before it writes.
		{`DELETE FROM garbage WHERE tenant_id=? AND born=? AND place=?`, []any{string(id), born, shard}},
	} {
		if _, err = tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return "", err
		}
	}
	return shard, tx.Commit()
}

// target is the open shard with the fewest tenants, other than except.
func (r *registry) target(ctx context.Context, except string) (string, error) {
	var name string
	err := r.db.QueryRowContext(ctx, `SELECT name FROM shards WHERE state=? AND name<>? ORDER BY tenants, name LIMIT 1`, string(ShardOpen), except).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errNoShard
	}
	return name, err
}

// relocate runs the registry's half of a step that changes where a tenant's
// data is: update must change the tenant's row, or nothing is written and it
// reports false.
func (r *registry) relocate(ctx context.Context, update string, args []any, then func(tx *fleetTx) error) (bool, error) {
	tx, err := r.db.begin(ctx, false)
	if err != nil {
		return false, err
	}
	changed, err := tx.affected(ctx, update, args...)
	if err == nil && changed {
		err = then(tx)
	}
	if err != nil || !changed {
		return false, errors.Join(err, tx.Rollback())
	}
	return true, tx.Commit()
}

// moved records that a tenant's data is in another shard now, and that the
// copy in the old one must go. It reports false when node no longer holds the
// tenant.
func (r *registry) moved(ctx context.Context, id TenantID, born int64, node, from, to string, now time.Time) (bool, error) {
	return r.relocate(ctx, `UPDATE tenants SET shard=? WHERE id=? AND shard=? AND created_at=?`+heldBy, []any{to, string(id), from, born, node}, func(tx *fleetTx) error {
		for _, statement := range []struct {
			query string
			args  []any
		}{
			{`UPDATE shards SET tenants=tenants-1 WHERE name=?`, []any{from}},
			{`UPDATE shards SET tenants=tenants+1 WHERE name=?`, []any{to}},
			{`DELETE FROM garbage WHERE tenant_id=? AND born=? AND place=?`, []any{string(id), born, to}},
		} {
			if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
				return err
			}
		}
		return discard(ctx, tx, leftover{id, born, from}, now)
	})
}

// frozen records that a sleeping tenant's data is in the archive store now,
// as the object written under mark, and that the copy in its shard must go.
// It reports false when the tenant was opened meanwhile or node no longer
// holds it.
func (r *registry) frozen(ctx context.Context, id TenantID, born int64, node, shard, mark string, now time.Time) (bool, error) {
	return r.relocate(ctx, `UPDATE tenants SET state=?, shard='', archive=? WHERE id=? AND active=0 AND state=? AND shard=? AND created_at=?`+heldBy,
		[]any{string(TenantCold), mark, string(id), string(TenantWarm), shard, born, node}, func(tx *fleetTx) error {
			if _, err := tx.ExecContext(ctx, `UPDATE shards SET tenants=tenants-1 WHERE name=?`, shard); err != nil {
				return err
			}
			// The object is the tenant's data now, not a copy to remove.
			if _, err := tx.ExecContext(ctx, `DELETE FROM garbage WHERE tenant_id=? AND born=? AND place=?`, string(id), born, archiveAt(mark)); err != nil {
				return err
			}
			return discard(ctx, tx, leftover{id, born, shard}, now)
		})
}

// thawed records that the data of a tenant archived under mark is in its
// shard again, and that the archive must go. It reports false when node no
// longer holds the tenant.
func (r *registry) thawed(ctx context.Context, id TenantID, born int64, node, mark string, now time.Time) (bool, error) {
	return r.relocate(ctx, `UPDATE tenants SET state=?, archive='' WHERE id=? AND state=? AND archive=? AND created_at=?`+heldBy,
		[]any{string(TenantWarm), string(id), string(TenantCold), mark, born, node}, func(tx *fleetTx) error {
			return discard(ctx, tx, leftover{id, born, archiveAt(mark)}, now)
		})
}

// intend writes down a copy that is about to be made and must go unless the
// step that makes it completes.
func (r *registry) intend(ctx context.Context, l leftover, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO garbage(tenant_id,born,place,at) VALUES(?,?,?,?) ON CONFLICT(tenant_id,born,place) DO NOTHING`,
		string(l.id), l.born, l.place, millis(now))
	return err
}

func (r *registry) leftovers(ctx context.Context, limit int) ([]leftover, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT tenant_id,born,place FROM garbage ORDER BY at, tenant_id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	left := []leftover{}
	for rows.Next() {
		var l leftover
		var id string
		if err := rows.Scan(&id, &l.born, &l.place); err != nil {
			return nil, err
		}
		l.id = TenantID(id)
		left = append(left, l)
	}
	return left, rows.Err()
}

func (r *registry) cleaned(ctx context.Context, l leftover) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM garbage WHERE tenant_id=? AND born=? AND place=?`, string(l.id), l.born, l.place)
	return err
}

func (r *registry) leftoverCount(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM garbage`).Scan(&count)
	return count, err
}

// movable pages through a shard's sleeping tenants in ID order.
func (r *registry) movable(ctx context.Context, shard string, after TenantID, limit int) ([]TenantID, error) {
	return r.ids(ctx, `SELECT id FROM tenants WHERE shard=? AND id>? AND active=0 ORDER BY id LIMIT ?`, shard, string(after), limit)
}

// outdated counts the live nodes of a build before version.
func (r *registry) outdated(ctx context.Context, alive time.Time, version int) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE beat>=? AND version<?`, millis(alive), version).Scan(&count)
	return count, err
}

// advertised counts the live nodes that other nodes can reach, which are the
// nodes of a cluster.
func (r *registry) advertised(ctx context.Context, alive time.Time) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE beat>=? AND addr<>''`, millis(alive)).Scan(&count)
	return count, err
}

// ---- one shard ----

// shard is this node's connection to one of the vault's databases. A shard
// other than home is connected to when it is first needed, so a node starts
// while one of them is away.
type shard struct {
	name     string
	dial     func(context.Context) (*fleetDB, error)
	cooldown time.Duration

	mu        sync.Mutex
	db        *fleetDB
	troubles  int
	downUntil time.Time
}

func (s *shard) use(ctx context.Context) (*fleetDB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.troubles >= shardTroubles && time.Now().Before(s.downUntil) {
		return nil, fmt.Errorf("shard %s: %w", s.name, errShardDown)
	}
	if s.db == nil {
		db, err := s.dial(ctx)
		if err != nil {
			s.noteLocked(err)
			return nil, fmt.Errorf("shard %s: %w", s.name, err)
		}
		s.db = db
	}
	return s.db, nil
}

func (s *shard) noteLocked(err error) {
	if !dbTrouble(err) {
		// It answered, whatever it said.
		s.troubles = 0
		return
	}
	s.troubles++
	if s.troubles >= shardTroubles {
		// After the cooldown the next statement is let through; if it fails
		// the shard is left alone again.
		s.downUntil = time.Now().Add(s.cooldown)
	}
}

// do runs fn on the shard and counts its result towards the shard's health.
// While the shard is down it answers at once, so the fleet's loops and the
// tenants of other shards do not wait behind it.
func (s *shard) do(ctx context.Context, fn func(*fleetDB) error) error {
	db, err := s.use(ctx)
	if err != nil {
		return err
	}
	err = fn(db)
	s.mu.Lock()
	s.noteLocked(err)
	s.mu.Unlock()
	return err
}

func (s *shard) isDown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.troubles >= shardTroubles && time.Now().Before(s.downUntil)
}

// probe asks the shard a trivial question, to find which side of a copy
// between two shards failed.
func (s *shard) probe(ctx context.Context) {
	_ = s.do(ctx, func(db *fleetDB) error {
		var one int
		return db.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
	})
}

// held is the checkpoint a shard holds of a tenant: its number, its name and
// the size of the tenant's files.
type held struct {
	seq   int64
	mark  string
	bytes int64
}

// take makes node the one writer of a tenant's data in this shard and returns
// the checkpoint the shard holds. Its first statement takes the head's row,
// so a checkpoint in flight either commits before the node reads the head or
// is refused after. What the shard holds under the tenant's name but not of
// this tenant (born differs) is the data of a deleted one and is removed;
// reset removes whatever is there, for a tenant whose data is about to be
// written anew.
//
// Removing data that refers to objects in the archive store takes two
// transactions: the first makes node the writer, so nothing comes to refer
// to more objects after it, and returns the objects referred to, which the
// caller writes down before it calls again (see vault.take); noted are those
// written down already.
func take(ctx context.Context, db *fleetDB, id TenantID, node string, born int64, reset bool, now time.Time, noted map[string]bool) (head held, unnoted []string, owner int64, err error) {
	tx, err := db.begin(ctx, false)
	if err != nil {
		return held{}, nil, 0, err
	}
	defer func() {
		if err != nil {
			head, err = held{}, errors.Join(err, tx.Rollback())
		}
	}()
	tenant := string(id)
	if _, err = tx.ExecContext(ctx, `INSERT INTO vault_heads(tenant_id,seq,node,at,bytes,born,mark) VALUES(?,0,?,?,0,?,'')
		ON CONFLICT(tenant_id) DO UPDATE SET node=excluded.node`, tenant, node, millis(now), born); err != nil {
		return head, nil, 0, err
	}
	var stored int64
	if err = tx.QueryRowContext(ctx, `SELECT seq,born,mark,bytes FROM vault_heads WHERE tenant_id=?`, tenant).Scan(&head.seq, &stored, &head.mark, &head.bytes); err != nil {
		return head, nil, 0, err
	}
	switch {
	case reset || (stored != 0 && stored != born):
		owner = stored
		if owner == 0 {
			owner = born
		}
		tokens, err := fileObjects(ctx, tx, id, owner)
		if err != nil {
			return head, nil, 0, err
		}
		for _, token := range tokens {
			if !noted[token] {
				unnoted = append(unnoted, token)
			}
		}
		if len(unnoted) > 0 {
			return head, unnoted, owner, tx.Commit()
		}
		for _, table := range []string{"vault_files", "vault_pages"} {
			if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE tenant_id=?`, tenant); err != nil {
				return head, nil, 0, err
			}
		}
		head = held{}
		if _, err = tx.ExecContext(ctx, `UPDATE vault_heads SET seq=0, bytes=0, born=?, mark='' WHERE tenant_id=?`, born, tenant); err != nil {
			return head, nil, 0, err
		}
	case stored == 0 || (head.seq > 0 && head.mark == ""):
		// Written by a node from before these columns, which removed a
		// tenant's data with the tenant: it is this tenant's.
		if head.seq > 0 && head.mark == "" {
			head.mark = randomHex(8)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE vault_heads SET born=?, mark=? WHERE tenant_id=?`, born, head.mark, tenant); err != nil {
			return head, nil, 0, err
		}
	}
	return head, nil, 0, tx.Commit()
}

// purge removes one tenant's data from a shard while node is its writer
// there, and nothing of a later tenant with the same name. Data another node
// has taken since is refused with errFenced: that node opened the tenant
// there or is moving it there.
func purge(ctx context.Context, db *fleetDB, id TenantID, born int64, node string) error {
	tx, err := db.begin(ctx, false)
	if err != nil {
		return err
	}
	found, err := tx.affected(ctx, `DELETE FROM vault_heads WHERE tenant_id=? AND (born=? OR born=0) AND node=?`, string(id), born, node)
	switch {
	case err != nil:
	case found:
		for _, table := range []string{"vault_files", "vault_pages"} {
			if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE tenant_id=?`, string(id)); err != nil {
				break
			}
		}
	default:
		var writer string
		err = tx.QueryRowContext(ctx, `SELECT node FROM vault_heads WHERE tenant_id=? AND (born=? OR born=0)`, string(id), born).Scan(&writer)
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		} else if err == nil {
			err = fmt.Errorf("%w: node %s has taken the data of tenant %s", errFenced, writer, id)
		}
	}
	if err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

// storedFile is one of a tenant's files as a shard holds it. A large file
// keeps its bulk as the object base names in the archive store, of baseSize
// bytes whose SHA-256 is baseSum, and its pages are then only the overlay
// pages that differ from it (see fleet_files.go).
type storedFile struct {
	path     string
	size     int64
	sum      string
	base     string
	baseSize int64
	baseSum  string
	overlay  int64
}

// errCorrupt means what a shard holds for a file is not the file that was
// stored: pages are missing or their content does not match its checksum.
var errCorrupt = errors.New("the stored pages are not the file that was stored")

// storedFiles lists a tenant's files inside a transaction, in path order,
// with their total size.
func storedFiles(ctx context.Context, q queryer, id TenantID) ([]storedFile, int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT path,size,sum,base,base_size,base_sum,overlay FROM vault_files WHERE tenant_id=? ORDER BY path`, string(id))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	files := []storedFile{}
	var total int64
	for rows.Next() {
		var file storedFile
		if err := rows.Scan(&file.path, &file.size, &file.sum, &file.base, &file.baseSize, &file.baseSum, &file.overlay); err != nil {
			return nil, 0, err
		}
		if !safeRel(file.path) || file.size < 0 {
			return nil, 0, fmt.Errorf("stored file %q is not inside the tenant's directory", file.path)
		}
		if file.base != "" && !tokenPattern.MatchString(file.base) {
			return nil, 0, fmt.Errorf("%w: %s names no object the fleet writes", errCorrupt, file.path)
		}
		files = append(files, file)
		total += file.size
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if total > maxArchiveBytes {
		return nil, 0, errors.New("the tenant is larger than a tenant may be")
	}
	return files, total, nil
}

// storedPages hands one stored file's pages to write in order. It fails with
// errCorrupt unless the pages are contiguous, every page but the last is
// full, they hold the file's size and they match its checksum.
func storedPages(ctx context.Context, q queryer, id TenantID, file storedFile, write func(idx int, data []byte) error) error {
	rows, err := q.QueryContext(ctx, `SELECT idx,data FROM vault_pages WHERE tenant_id=? AND path=? ORDER BY idx`, string(id), file.path)
	if err != nil {
		return err
	}
	defer rows.Close()
	var written int64
	var digest hash.Hash = sha256.New()
	for want := 0; rows.Next(); want++ {
		var idx int
		var data []byte
		if err := rows.Scan(&idx, &data); err != nil {
			return err
		}
		if idx != want || len(data) == 0 || len(data) > vaultPage || written%vaultPage != 0 {
			return fmt.Errorf("%w: %s page %d", errCorrupt, file.path, idx)
		}
		digest.Write(data)
		if err := write(idx, data); err != nil {
			return err
		}
		written += int64(len(data))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if written != file.size {
		return fmt.Errorf("%w: %s holds %d bytes of %d", errCorrupt, file.path, written, file.size)
	}
	// A file stored before checksums has none.
	if file.sum != "" && hex.EncodeToString(digest.Sum(nil)) != file.sum {
		return fmt.Errorf("%w: %s does not match its checksum", errCorrupt, file.path)
	}
	return nil
}

// insertPages writes pages of one file in a single statement.
func insertPages(ctx context.Context, tx *fleetTx, id TenantID, path string, pages []vaultPageData) error {
	if len(pages) == 0 {
		return nil
	}
	query := strings.Builder{}
	query.WriteString(`INSERT INTO vault_pages(tenant_id,path,idx,data) VALUES `)
	args := make([]any, 0, 4*len(pages))
	for i, page := range pages {
		if i > 0 {
			query.WriteByte(',')
		}
		query.WriteString(`(?,?,?,?)`)
		args = append(args, string(id), path, page.idx, page.data)
	}
	query.WriteString(` ON CONFLICT(tenant_id,path,idx) DO UPDATE SET data=excluded.data`)
	_, err := tx.ExecContext(ctx, query.String(), args...)
	return err
}

// ---- the vault's shards ----

// shard returns the node's connection to a shard by name.
func (v *vault) shard(name string) (*shard, error) {
	if s, ok := v.shards[name]; ok {
		return s, nil
	}
	return nil, fmt.Errorf("shard %q is not configured on this node", name)
}

// close closes the connections to the shards other than home, which is the
// registry's.
func (v *vault) close() error {
	var err error
	for _, s := range v.shards {
		s.mu.Lock()
		if s.dial != nil && s.db != nil {
			err = errors.Join(err, s.db.Close())
			s.db = nil
		}
		s.mu.Unlock()
	}
	return err
}

// bounded gives a statement set that carries size bytes a deadline: time to
// notice a database that stopped answering, and a floor of one mebibyte a
// second.
func bounded(ctx context.Context, size int64) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 20*time.Second+time.Duration(size>>20)*time.Second)
}

// move copies a sleeping tenant's data to another shard and makes that the
// tenant's shard. The caller holds the tenant's placement. Each step leaves a
// state the next run completes or undoes: until the registry names the new
// shard the copy there is a leftover, and from then on the old one is.
func (v *vault) move(ctx context.Context, record TenantRecord, node string, to *shard) error {
	from, err := v.shard(record.Shard)
	if err != nil {
		return err
	}
	id, born := record.ID, millis(record.CreatedAt)
	if err := v.registry.intend(ctx, leftover{id, born, to.name}, v.now()); err != nil {
		return err
	}
	head, err := v.take(ctx, from, id, node, born, false)
	if err != nil {
		return err
	}
	// Both sides of the copy get the time the tenant's size takes.
	ctx, cancel := bounded(ctx, 2*head.bytes)
	defer cancel()
	if _, err := v.take(ctx, to, id, node, born, true); err != nil {
		return err
	}
	source, err := from.use(ctx)
	if err != nil {
		return err
	}
	target, err := to.use(ctx)
	if err != nil {
		return err
	}
	if err := copyTenant(ctx, source, target, id, node, head, v.now()); err != nil {
		if dbTrouble(err) {
			from.probe(ctx)
			to.probe(ctx)
		}
		return err
	}
	v.at("move")
	moved, err := v.registry.moved(ctx, id, born, node, from.name, to.name, v.now())
	if err != nil || !moved {
		return errors.Join(err, fmt.Errorf("tenant %s left shard %s or this node while it was being moved", id, from.name))
	}
	v.counters.moves.Add(1)
	v.at("moved")
	// This node took the old copy when the move began, and removes it only
	// if no node took it since.
	return v.clean(ctx, leftover{id, born, from.name}, node)
}

// copyTenant writes what source holds of a tenant to target, where node has
// just taken the tenant's emptied head, in one transaction on each side. The
// caller's deadline bounds both.
func copyTenant(ctx context.Context, source, target *fleetDB, id TenantID, node string, head held, now time.Time) (err error) {
	seq, mark := head.seq, head.mark
	read, err := source.begin(ctx, true)
	if err != nil {
		return err
	}
	defer read.Rollback()
	var stored int64
	if err := read.QueryRowContext(ctx, `SELECT seq FROM vault_heads WHERE tenant_id=? AND node=?`, string(id), node).Scan(&stored); err != nil || stored != seq {
		return errors.Join(err, errFenced)
	}
	files, total, err := storedFiles(ctx, read, id)
	if err != nil {
		return err
	}
	write, err := target.begin(ctx, false)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, write.Rollback())
		}
	}()
	if ours, err := write.affected(ctx, `UPDATE vault_heads SET seq=?, mark=?, at=?, bytes=? WHERE tenant_id=? AND node=? AND seq=0`, seq, mark, millis(now), total, string(id), node); err != nil || !ours {
		return errors.Join(err, errFenced)
	}
	for _, file := range files {
		if _, err = write.ExecContext(ctx, `INSERT INTO vault_files(tenant_id,path,size,seq,sum,base,base_size,base_sum,overlay) VALUES(?,?,?,?,?,?,?,?,?)`,
			string(id), file.path, file.size, seq, file.sum, file.base, file.baseSize, file.baseSum, file.overlay); err != nil {
			return err
		}
		// A large file's object stays where it is: both copies refer to it,
		// and the old one's going does not take it along (see vault.letGo).
		pages := storedPages
		if file.base != "" {
			pages = overlayPages
		}
		batch := make([]vaultPageData, 0, vaultBatch)
		err = pages(ctx, read, id, file, func(idx int, data []byte) error {
			batch = append(batch, vaultPageData{idx: idx, data: slices.Clone(data)})
			if len(batch) < vaultBatch {
				return nil
			}
			err := insertPages(ctx, write, id, file.path, batch)
			batch = batch[:0]
			return err
		})
		if err == nil {
			err = insertPages(ctx, write, id, file.path, batch)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", file.path, err)
		}
	}
	return write.Commit()
}

// at marks a point between two steps of a change to where a tenant's data
// is. Tests hold the node up there.
func (v *vault) at(step string) {
	if v.pause != nil {
		v.pause(step)
	}
}

// seize makes node the writer of the copy a leftover names in a shard. A
// node that takes the copy afterwards, to open the tenant there or to move it
// there, becomes its writer in turn, and clean then leaves the copy alone.
func (v *vault) seize(ctx context.Context, l leftover, node string) error {
	if _, archived := archiveMark(l.place); archived {
		// An archive is never written twice under one name.
		return nil
	}
	s, err := v.shard(l.place)
	if err != nil {
		return err
	}
	return s.do(ctx, func(db *fleetDB) error {
		_, err := db.ExecContext(ctx, `UPDATE vault_heads SET node=? WHERE tenant_id=? AND (born=? OR born=0)`, node, string(l.id), l.born)
		return err
	})
}

// discard removes a leftover nothing can take any more: the tenant is gone.
func (v *vault) discard(ctx context.Context, l leftover, node string) error {
	if err := v.seize(ctx, l, node); err != nil {
		return err
	}
	return v.clean(ctx, l, node)
}

// clean removes one leftover and its row. In a shard the copy goes only
// while node is its writer. A copy in a shard this node has no address for,
// or in an archive store it does not have, stays written down.
func (v *vault) clean(ctx context.Context, l leftover, node string) error {
	if mark, archived := archiveMark(l.place); archived {
		if v.cold == nil {
			return errors.New("this node has no archive store")
		}
		if err := v.cold.remove(ctx, coldKey(l.id, l.born, mark)); err != nil {
			return err
		}
	} else {
		s, err := v.shard(l.place)
		if err != nil {
			return err
		}
		// The objects of the copy's large files are written down before
		// the rows that refer to them go.
		var tokens []string
		err = s.do(ctx, func(db *fleetDB) (err error) {
			tokens, err = fileObjects(ctx, db, l.id, l.born)
			return err
		})
		if err == nil {
			err = v.note(ctx, l.id, l.born, tokens)
		}
		if err != nil {
			return err
		}
		err = s.do(ctx, func(db *fleetDB) error {
			// Removing a tenant's pages takes the time its size takes.
			var size int64
			if err := db.QueryRowContext(ctx, `SELECT bytes FROM vault_heads WHERE tenant_id=?`, string(l.id)).Scan(&size); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			ctx, cancel := bounded(ctx, size)
			defer cancel()
			return purge(ctx, db, l.id, l.born, node)
		})
		if err != nil {
			return err
		}
		v.counters.purges.Add(1)
		v.at("purged")
		if err := v.registry.cleaned(ctx, l); err != nil {
			return err
		}
		v.letGo(ctx, l, tokens)
		return nil
	}
	v.counters.purges.Add(1)
	return v.registry.cleaned(ctx, l)
}

// ---- the fleet's side ----

// reserve takes the slot of a tenant that is not open here, so that a request
// for it waits until release. It reports false when the tenant is open or the
// fleet is closing.
func (f *Fleet) reserve(id TenantID) (release func(), ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, open := f.tenants[id]; open || f.closed {
		return nil, false
	}
	hold := &tenant{id: id, closing: true, gone: make(chan struct{})}
	f.tenants[id] = hold
	return func() {
		f.mu.Lock()
		delete(f.tenants, id)
		f.mu.Unlock()
		close(hold.gone)
	}, true
}

// hold reserves a sleeping tenant and takes its placement, so that no node
// opens, moves or archives it until release. The tenant's row read after that
// is the truth about where its data is.
func (f *Fleet) hold(ctx context.Context, id TenantID) (release func(), err error) {
	free, ok := f.reserve(id)
	if !ok {
		return nil, errMoving
	}
	if err := f.place(ctx, id); err != nil {
		free()
		return nil, err
	}
	return func() {
		if err := f.registry.unplace(context.WithoutCancel(ctx), id, f.node); err != nil {
			fmt.Fprintf(f.log, "fleet: release %s: %v\n", id, err)
		}
		free()
	}, nil
}

// Shards lists the vault's shards with this node's view of each. A fleet
// that keeps its tenants in the data directory has none.
func (f *Fleet) Shards(ctx context.Context) ([]ShardInfo, error) {
	if f.vault == nil {
		return []ShardInfo{}, nil
	}
	shards, err := f.registry.shards(ctx)
	for i := range shards {
		if s, ok := f.vault.shards[shards[i].Name]; ok {
			shards[i].Configured, shards[i].Down = true, s.isDown()
		}
	}
	return shards, err
}

// SetShard changes what a shard is for: open takes new tenants, closed keeps
// what it has, draining moves its tenants to the open shards, and retired is
// for one that is empty and will not be used again.
func (f *Fleet) SetShard(ctx context.Context, name string, state ShardState) error {
	if f.vault == nil {
		return ErrShardUnknown
	}
	switch state {
	case ShardOpen, ShardClosed, ShardDraining, ShardRetired:
	default:
		return fmt.Errorf("a shard is %s, %s, %s or %s", ShardOpen, ShardClosed, ShardDraining, ShardRetired)
	}
	return f.registry.setShard(ctx, name, state)
}

// drainBatch is how many tenants one pass moves out of each draining shard.
const drainBatch = 8

// drain moves sleeping tenants out of the shards that are being emptied. It
// walks each shard in ID order and remembers where it was, so tenants that
// cannot move yet (open somewhere) do not keep the others waiting.
func (f *Fleet) drain(ctx context.Context) {
	shards, err := f.registry.shards(ctx)
	if err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(f.log, "fleet: list shards: %v\n", err)
		}
		return
	}
	for _, info := range shards {
		if info.State != ShardDraining || ctx.Err() != nil {
			continue
		}
		after := f.drainAt[info.Name]
		ids, err := f.registry.movable(ctx, info.Name, after, drainBatch)
		if err != nil {
			fmt.Fprintf(f.log, "fleet: list the tenants of shard %s: %v\n", info.Name, err)
			continue
		}
		if len(ids) == 0 {
			delete(f.drainAt, info.Name)
			continue
		}
		f.drainAt[info.Name] = ids[len(ids)-1]
		for _, id := range ids {
			if err := f.relocate(ctx, id, info.Name); err != nil && ctx.Err() == nil {
				var away *elsewhere
				if errors.As(err, &away) || errors.Is(err, errMoving) || errors.Is(err, ErrTenantUnknown) {
					// Open somewhere, or gone: a later pass finds it.
					continue
				}
				fmt.Fprintf(f.log, "fleet: move %s out of shard %s: %v\n", id, info.Name, err)
				if errors.Is(err, errNoShard) || errors.Is(err, errShardDown) {
					break
				}
			}
		}
	}
}

// relocate moves one sleeping tenant out of a shard to the open shard with
// the fewest tenants.
func (f *Fleet) relocate(ctx context.Context, id TenantID, from string) error {
	release, err := f.hold(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	record, ok, err := f.registry.get(ctx, id)
	if err != nil || !ok || record.Shard != from {
		// Archived or moved since it was listed.
		return err
	}
	name, err := f.registry.target(ctx, from)
	if err != nil {
		return err
	}
	to, err := f.vault.shard(name)
	if err != nil {
		return err
	}
	return f.vault.move(ctx, record, f.node, to)
}

// collect removes the copies that a step which changed where a tenant's data
// is left behind and did not get to remove itself.
func (f *Fleet) collect(ctx context.Context) {
	left, err := f.registry.leftovers(ctx, 64)
	if err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(f.log, "fleet: list leftover copies: %v\n", err)
		}
		return
	}
	// One line per place and pass, not one per tenant, while a place is away.
	// Every archive and every large file's object has a place of its own in
	// the one archive store.
	failed := map[string]error{}
	for _, l := range left {
		if ctx.Err() != nil {
			return
		}
		where := l.place
		_, archived := archiveMark(where)
		if _, object := fileToken(where); archived || object {
			where = "the archive store"
		}
		if _, skip := failed[where]; skip {
			continue
		}
		if err := f.clean(ctx, l); err != nil {
			var away *elsewhere
			if errors.As(err, &away) || errors.Is(err, errMoving) || errors.Is(err, errFenced) {
				continue
			}
			failed[where] = err
		}
	}
	places := make([]string, 0, len(failed))
	for place := range failed {
		places = append(places, place)
	}
	sort.Strings(places)
	for _, place := range places {
		fmt.Fprintf(f.log, "fleet: remove leftover copies in %s: %v\n", place, failed[place])
	}
}

// clean removes one leftover. A tenant that still exists is held meanwhile:
// a move or an archive in flight holds it too, and then the copy is not a
// leftover yet. If the tenant has come back to the place since, the copy is
// its data and only the row goes.
//
// Holding the tenant is not enough by itself: a node that is held up for
// longer than the others wait loses the tenant without noticing. So the node
// makes itself the copy's writer and looks again where the tenant is before
// it removes anything. Whoever took the tenant to the place before that is
// seen in the registry, and whoever takes it there after is the copy's writer
// and the removal is refused.
func (f *Fleet) clean(ctx context.Context, l leftover) error {
	if token, object := fileToken(l.place); object {
		return f.cleanFile(ctx, l, token)
	}
	live := func() (bool, error) {
		record, ok, err := f.registry.get(ctx, l.id)
		return ok && millis(record.CreatedAt) == l.born && record.holds(l.place), err
	}
	record, ok, err := f.registry.get(ctx, l.id)
	if err != nil {
		return err
	}
	if !ok || millis(record.CreatedAt) != l.born {
		return f.vault.discard(ctx, l, f.node)
	}
	release, err := f.hold(ctx, l.id)
	if errors.Is(err, ErrTenantUnknown) {
		return f.vault.discard(ctx, l, f.node)
	}
	if err != nil {
		return err
	}
	defer release()
	for _, seize := range []bool{true, false} {
		if stays, err := live(); err != nil {
			return err
		} else if stays {
			return f.registry.cleaned(ctx, l)
		}
		if seize {
			f.vault.at("seize")
			if err := f.vault.seize(ctx, l, f.node); err != nil {
				return err
			}
		}
	}
	f.vault.at("clean")
	return f.vault.clean(ctx, l, f.node)
}
