package assistant

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
)

// The vault keeps every tenant's files in the fleet's database, so a node's
// disk is only a cache: any node can open any tenant, and a node that is lost
// takes nothing with it. A tenant is still an unchanged single-owner App on a
// directory of SQLite files. While it is open, the node copies what changed
// to the vault as checkpoints:
//
//   - A checkpoint is one transaction that replaces the changed 4 KiB pages
//     of the tenant's files and advances the tenant's sequence number. The
//     vault therefore always holds one complete state, never half of one.
//   - The databases are read through SQLite's backup interface from read
//     snapshots that are checked to be of the same instant, so the queue, the
//     sessions and the mock world agree with each other in every checkpoint.
//   - A checkpoint is refused unless the node still holds the tenant's
//     placement and the sequence number is the one it last wrote. A node the
//     others took for dead cannot write over the tenant's new holder.
//
// What is in the vault is what survives a node. The fleet checkpoints an open
// tenant every Checkpoint interval, so a crash loses at most that much of a
// running turn, and before anything leaves the node that cannot be taken
// back: the answer to an owner's write and an approved effect (see barrier).
// Nothing the outside saw is ever rolled back.

const (
	vaultPage  = 4096
	vaultDir   = ".vault"
	vaultBatch = 128
	// cutAttempts bounds how often a checkpoint retries reading the
	// databases at one instant before it gives way to the next one.
	cutAttempts = 8
)

var (
	// errFenced means the vault refused a checkpoint: another node holds the
	// tenant, or wrote it since this node read it.
	errFenced = errors.New("another node holds the tenant")
	// errCacheBroken means the node's copy can no longer be compared with the
	// vault, so the tenant must be closed and read from the vault again.
	errCacheBroken = errors.New("the node's copy of the tenant is unusable")
)

type vaultCounters struct {
	checkpoints, pages, bytes, pulls, pulledBytes, reuses atomic.Int64
}

type vault struct {
	db       *fleetDB
	now      func() time.Time
	counters vaultCounters
}

// vaultHead is the note a node leaves in its copy of a tenant: which
// checkpoint the copy matches and whether it was closed cleanly. Born is the
// tenant's creation time, so the copy of a deleted tenant is never taken for
// a new tenant with the same name.
type vaultHead struct {
	Seq   int64 `json:"seq"`
	Born  int64 `json:"born"`
	Clean bool  `json:"clean"`
}

func headPath(dir string) string { return filepath.Join(dir, vaultDir, "head.json") }

func readHead(dir string) (vaultHead, bool) {
	var head vaultHead
	data, err := os.ReadFile(headPath(dir))
	if err != nil || json.Unmarshal(data, &head) != nil {
		return vaultHead{}, false
	}
	return head, true
}

func writeHead(dir string, head vaultHead) error {
	data, err := json.Marshal(head)
	if err != nil {
		return err
	}
	return replaceFile(headPath(dir), bytes.NewReader(data))
}

// replaceFile writes path through a temporary file and a rename.
func replaceFile(path string, content io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp := path + ".tmp"
	file, err := os.OpenFile(temp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(file, content)
	if err = errors.Join(err, file.Close()); err != nil {
		return errors.Join(err, os.Remove(temp))
	}
	return os.Rename(temp, path)
}

// replica is one open tenant's link to the vault.
type replica struct {
	vault *vault
	id    TenantID
	node  string
	dir   string
	born  int64

	mu  sync.Mutex
	seq int64
	// lost is set once the vault refused this node; nothing more is written.
	lost bool
	// known is every file the vault holds for the tenant; stamps remembers
	// the plain files as they were when last compared.
	known   map[string]bool
	stamps  map[string]fileStamp
	sources map[string]*vaultSource
	// between runs after each database's snapshot is taken; tests use it to
	// commit at the worst moment.
	between func()
}

type fileStamp struct {
	size    int64
	modTime time.Time
}

// vaultSource is the node's own view of one of the tenant's databases.
// witness stays open while the tenant is and never holds a transaction: its
// data version tells whether anybody committed since it was last asked. snap
// is opened for one checkpoint, to read the snapshot of a database that
// changed.
type vaultSource struct {
	path          string
	snap, witness *sqlite3.SQLiteConn
	// shipped is witness's data version at the snapshot the vault holds;
	// fresh means the vault's copy has not been compared yet.
	shipped int64
	fresh   bool
}

func (s *vaultSource) close() error {
	err := s.release()
	return errors.Join(err, s.witness.Close())
}

// release ends the snapshot, if one is open.
func (s *vaultSource) release() error {
	if s.snap == nil {
		return nil
	}
	snap := s.snap
	s.snap = nil
	_, _ = snap.Exec("ROLLBACK", nil)
	return snap.Close()
}

// attach makes the node's copy of a tenant match the vault and returns the
// link that keeps the vault up to date. It reports whether the copy had to be
// read from the vault. The caller holds the tenant's placement.
func (v *vault) attach(ctx context.Context, id TenantID, node, dir string, born time.Time) (*replica, bool, error) {
	r := &replica{vault: v, id: id, node: node, dir: dir, born: millis(born), known: map[string]bool{}, stamps: map[string]fileStamp{}, sources: map[string]*vaultSource{}}
	var remote int64
	err := v.db.QueryRowContext(ctx, `SELECT seq FROM vault_heads WHERE tenant_id=?`, string(id)).Scan(&remote)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	local, ok := readHead(dir)
	pulled := false
	if ok && local.Clean && local.Born == r.born && local.Seq == remote && remote > 0 {
		// The copy is what this node last wrote and nobody wrote since.
		if err := r.loadKnown(); err != nil {
			return nil, false, err
		}
		r.seq = remote
		v.counters.reuses.Add(1)
	} else {
		if err := r.pull(ctx); err != nil {
			return nil, false, fmt.Errorf("read tenant %s from the vault: %w", id, err)
		}
		pulled = r.seq > 0
	}
	// Until it is closed cleanly the copy may be ahead of the vault.
	if err := writeHead(dir, vaultHead{Seq: r.seq, Born: r.born}); err != nil {
		return nil, false, err
	}
	return r, pulled, nil
}

func (r *replica) base(rel string) string {
	return filepath.Join(r.dir, vaultDir, "base", filepath.FromSlash(rel))
}

func (r *replica) next(rel string) string {
	return filepath.Join(r.dir, vaultDir, "next", filepath.FromSlash(rel))
}

// loadKnown lists the files of a reused copy from its base images.
func (r *replica) loadKnown() error {
	root := filepath.Join(r.dir, vaultDir, "base")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		r.known[filepath.ToSlash(rel)] = true
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// safeRel refuses a stored path that would land outside the tenant's
// directory or inside the vault's own.
func safeRel(rel string) bool {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	return rel != "" && clean == rel && !filepath.IsAbs(rel) && rel != "." && rel != ".." &&
		!strings.HasPrefix(rel, "../") && rel != vaultDir && !strings.HasPrefix(rel, vaultDir+"/")
}

// pull replaces the node's copy with the vault's state: every file is
// written to the directory and, as the image later checkpoints are compared
// with, to base. One snapshot transaction reads it all.
func (r *replica) pull(ctx context.Context) error {
	if err := os.RemoveAll(r.dir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(r.dir, vaultDir), 0o700); err != nil {
		return err
	}
	tx, err := r.vault.db.begin(ctx, true)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var seq int64
	err = tx.QueryRowContext(ctx, `SELECT seq FROM vault_heads WHERE tenant_id=?`, string(r.id)).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	type stored struct {
		path string
		size int64
	}
	rows, err := tx.QueryContext(ctx, `SELECT path,size FROM vault_files WHERE tenant_id=? ORDER BY path`, string(r.id))
	if err != nil {
		return err
	}
	files := []stored{}
	var total int64
	for rows.Next() {
		var file stored
		if err := rows.Scan(&file.path, &file.size); err != nil {
			rows.Close()
			return err
		}
		files = append(files, file)
		total += file.size
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if total > maxArchiveBytes {
		return errors.New("the tenant is larger than a tenant may be")
	}
	for _, file := range files {
		if !safeRel(file.path) || file.size < 0 {
			return fmt.Errorf("stored file %q is not inside the tenant's directory", file.path)
		}
		if err := r.pullFile(ctx, tx, file.path, file.size); err != nil {
			return fmt.Errorf("%s: %w", file.path, err)
		}
		r.known[file.path] = true
	}
	r.seq = seq
	r.vault.counters.pulls.Add(1)
	r.vault.counters.pulledBytes.Add(total)
	return nil
}

func (r *replica) pullFile(ctx context.Context, tx *fleetTx, rel string, size int64) (err error) {
	outputs := make([]*os.File, 0, 2)
	defer func() {
		for _, file := range outputs {
			err = errors.Join(err, file.Close())
		}
	}()
	for _, path := range []string{filepath.Join(r.dir, filepath.FromSlash(rel)), r.base(rel)} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		outputs = append(outputs, file)
	}
	rows, err := tx.QueryContext(ctx, `SELECT idx,data FROM vault_pages WHERE tenant_id=? AND path=? ORDER BY idx`, string(r.id), rel)
	if err != nil {
		return err
	}
	defer rows.Close()
	var written int64
	for want := 0; rows.Next(); want++ {
		var idx int
		var data []byte
		if err := rows.Scan(&idx, &data); err != nil {
			return err
		}
		// Every page but the last is full, and none is missing.
		if idx != want || len(data) == 0 || len(data) > vaultPage || written%vaultPage != 0 {
			return errors.New("its stored pages are not contiguous")
		}
		for _, file := range outputs {
			if _, err := file.Write(data); err != nil {
				return err
			}
		}
		written += int64(len(data))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if written != size {
		return fmt.Errorf("its stored pages hold %d bytes of %d", written, size)
	}
	return nil
}

// ---- reading the tenant's files ----

// scan lists the tenant's regular files, as slash paths in order. The
// vault's own directory, the lock file and SQLite's log files are not the
// tenant's data: the logs are read through the databases they belong to.
func (r *replica) scan() ([]string, error) {
	files := []string{}
	err := filepath.WalkDir(r.dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == r.dir {
			return err
		}
		rel, err := filepath.Rel(r.dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel == vaultDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() || rel == lockFile ||
			strings.HasSuffix(rel, "-wal") || strings.HasSuffix(rel, "-shm") || strings.HasSuffix(rel, "-journal") || strings.HasSuffix(rel, ".tmp") {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	slices.Sort(files)
	return files, err
}

const sqliteMagic = "SQLite format 3\x00"

func isDatabase(path string) bool {
	if !strings.HasSuffix(path, ".db") {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	header := make([]byte, len(sqliteMagic))
	_, err = io.ReadFull(file, header)
	return err == nil && string(header) == sqliteMagic
}

func openSnapshotConn(path string) (*sqlite3.SQLiteConn, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	conn, err := (&sqlite3.SQLiteDriver{}).Open((&url.URL{Scheme: "file", Path: absolute}).String())
	if err != nil {
		return nil, err
	}
	opened := conn.(*sqlite3.SQLiteConn)
	if _, err := opened.Exec("PRAGMA busy_timeout=2000", nil); err != nil {
		return nil, errors.Join(err, opened.Close())
	}
	return opened, nil
}

func (r *replica) source(rel string) (*vaultSource, error) {
	if source, ok := r.sources[rel]; ok {
		return source, nil
	}
	path := filepath.Join(r.dir, filepath.FromSlash(rel))
	witness, err := openSnapshotConn(path)
	if err != nil {
		return nil, err
	}
	source := &vaultSource{path: path, witness: witness, fresh: true}
	r.sources[rel] = source
	return source, nil
}

func queryInt(conn *sqlite3.SQLiteConn, query string) (int64, error) {
	rows, err := conn.Query(query, nil)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	row := make([]driver.Value, 1)
	if err := rows.Next(row); err != nil {
		return 0, err
	}
	value, ok := row[0].(int64)
	if !ok {
		return 0, fmt.Errorf("%s returned %T", query, row[0])
	}
	return value, nil
}

// cut opens a read snapshot on every database that changed since its last
// checkpoint, such that all the databases are seen at one instant: none of
// them, changed or not, was committed to between the first snapshot and the
// last. It returns each database's data version at that instant; a database
// with no snapshot is unchanged. The snapshots stay open until release.
func (r *replica) cut(sources []*vaultSource) (versions []int64, release func(), err error) {
	release = func() {
		for _, source := range sources {
			_ = source.release()
		}
	}
	read := func() ([]int64, error) {
		versions := make([]int64, len(sources))
		for i, source := range sources {
			version, err := queryInt(source.witness, "PRAGMA data_version")
			if err != nil {
				return nil, err
			}
			versions[i] = version
		}
		return versions, nil
	}
	for attempt := 0; attempt < cutAttempts; attempt++ {
		before, err := read()
		if err != nil {
			return nil, nil, err
		}
		for i, source := range sources {
			if !source.fresh && source.shipped == before[i] {
				continue
			}
			if source.snap, err = openSnapshotConn(source.path); err == nil {
				if _, err = source.snap.Exec("BEGIN", nil); err == nil {
					// A deferred transaction takes its snapshot at the first read.
					_, err = queryInt(source.snap, "SELECT COUNT(*) FROM sqlite_master")
				}
			}
			if err != nil {
				release()
				return nil, nil, err
			}
			if r.between != nil {
				r.between()
			}
		}
		after, err := read()
		if err != nil {
			release()
			return nil, nil, err
		}
		if slices.Equal(before, after) {
			return after, release, nil
		}
		release()
	}
	return nil, nil, errors.New("the tenant's databases kept changing; the next checkpoint tries again")
}

// image writes the snapshot of one database to a file of its own.
func (r *replica) image(source *vaultSource, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := removeIfExists(path + suffix); err != nil {
			return err
		}
	}
	target, err := openSnapshotConn(path)
	if err != nil {
		return err
	}
	backup, err := target.Backup("main", source.snap, "main")
	if err != nil {
		return errors.Join(err, target.Close())
	}
	_, err = backup.Step(-1)
	return errors.Join(err, backup.Finish(), target.Close())
}

// ---- checkpoints ----

type vaultPageData struct {
	idx  int
	data []byte
}

type vaultFile struct {
	path  string
	size  int64
	pages []vaultPageData
	// image is the file the pages were read from, which becomes the base
	// image once they are stored; moved says it may be renamed there.
	image string
	moved bool
}

// compare reads a file against its base image and returns the pages that
// differ. changed is false when the two are the same.
func (r *replica) compare(rel, image string) (file vaultFile, changed bool, err error) {
	file = vaultFile{path: rel, image: image}
	current, err := os.Open(image)
	if err != nil {
		return file, false, err
	}
	defer current.Close()
	var previous io.Reader = bytes.NewReader(nil)
	previousSize := int64(-1)
	if base, err := os.Open(r.base(rel)); err == nil {
		defer base.Close()
		if info, err := base.Stat(); err == nil {
			previousSize = info.Size()
		}
		previous = base
	} else if !errors.Is(err, os.ErrNotExist) {
		return file, false, err
	}
	page, old := make([]byte, vaultPage), make([]byte, vaultPage)
	for idx := 0; ; idx++ {
		n, err := io.ReadFull(current, page)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return file, false, err
		}
		if n == 0 {
			break
		}
		m, _ := io.ReadFull(previous, old)
		if !bytes.Equal(page[:n], old[:m]) {
			file.pages = append(file.pages, vaultPageData{idx: idx, data: bytes.Clone(page[:n])})
		}
		file.size += int64(n)
		if n < vaultPage {
			break
		}
	}
	return file, len(file.pages) > 0 || file.size != previousSize, nil
}

type vaultDelta struct {
	files []vaultFile
	gone  []string
	total int64
}

// checkpoint stores what changed since the last one. It reports whether the
// vault was written.
func (r *replica) checkpoint(ctx context.Context) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lost {
		return false, errFenced
	}
	return r.ship(ctx)
}

func (r *replica) ship(ctx context.Context) (bool, error) {
	files, err := r.scan()
	if err != nil {
		return false, err
	}
	present := map[string]bool{}
	plain, databases, sources := []string{}, []string{}, []*vaultSource{}
	for _, rel := range files {
		present[rel] = true
		if !isDatabase(filepath.Join(r.dir, filepath.FromSlash(rel))) {
			plain = append(plain, rel)
			continue
		}
		source, err := r.source(rel)
		if err != nil {
			return false, fmt.Errorf("open %s: %w", rel, err)
		}
		databases, sources = append(databases, rel), append(sources, source)
	}
	delta := vaultDelta{}
	for rel := range r.known {
		if !present[rel] {
			delta.gone = append(delta.gone, rel)
		}
	}
	slices.Sort(delta.gone)

	// The databases first, all at one instant; the plain files after, so a
	// file a turn wrote is never older than the turn's record of writing it.
	versions, release, err := r.cut(sources)
	if err != nil {
		return false, err
	}
	shipped := map[*vaultSource]int64{}
	for i, source := range sources {
		if source.snap == nil {
			continue
		}
		image := r.next(databases[i])
		if err := r.image(source, image); err != nil {
			release()
			return false, fmt.Errorf("snapshot %s: %w", databases[i], err)
		}
		file, changed, err := r.compare(databases[i], image)
		if err != nil {
			release()
			return false, err
		}
		shipped[source] = versions[i]
		if changed {
			file.moved = true
			delta.files = append(delta.files, file)
		} else if err := os.Remove(image); err != nil {
			release()
			return false, err
		}
	}
	release()
	stamps := map[string]fileStamp{}
	for _, rel := range plain {
		path := filepath.Join(r.dir, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		stamp := fileStamp{info.Size(), info.ModTime()}
		if r.known[rel] && r.stamps[rel] == stamp {
			continue
		}
		file, changed, err := r.compare(rel, path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		stamps[rel] = stamp
		if changed {
			delta.files = append(delta.files, file)
		}
	}
	written := len(delta.files) > 0 || len(delta.gone) > 0
	if written {
		if err := r.push(ctx, &delta); err != nil {
			if errors.Is(err, errFenced) {
				r.lost = true
			}
			return false, err
		}
		if err := r.promote(&delta); err != nil {
			r.lost = true
			return false, errors.Join(errCacheBroken, err)
		}
	}
	for source, version := range shipped {
		source.shipped, source.fresh = version, false
	}
	for rel, stamp := range stamps {
		r.stamps[rel] = stamp
	}
	return written, nil
}

// push writes one checkpoint. The first statement takes the placement row, so
// a node that claims the tenant waits for this checkpoint or makes it fail.
func (r *replica) push(ctx context.Context, delta *vaultDelta) (err error) {
	tx, err := r.vault.db.begin(ctx, false)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, tx.Rollback())
		}
	}()
	id := string(r.id)
	held, err := tx.ExecContext(ctx, `UPDATE placements SET since=since WHERE tenant_id=? AND node=?`, id, r.node)
	if err != nil {
		return err
	}
	if count, err := held.RowsAffected(); err != nil || count != 1 {
		return errors.Join(err, errFenced)
	}
	var seq int64
	err = tx.QueryRowContext(ctx, `SELECT seq FROM vault_heads WHERE tenant_id=?`, id).Scan(&seq)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if seq != r.seq {
		return errFenced
	}
	next := r.seq + 1
	var pages, size int64
	for _, file := range delta.files {
		if _, err = tx.ExecContext(ctx, `INSERT INTO vault_files(tenant_id,path,size,seq) VALUES(?,?,?,?)
			ON CONFLICT(tenant_id,path) DO UPDATE SET size=excluded.size, seq=excluded.seq`, id, file.path, file.size, next); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM vault_pages WHERE tenant_id=? AND path=? AND idx>=?`, id, file.path, (file.size+vaultPage-1)/vaultPage); err != nil {
			return err
		}
		for batch := range slices.Chunk(file.pages, vaultBatch) {
			query := strings.Builder{}
			query.WriteString(`INSERT INTO vault_pages(tenant_id,path,idx,data) VALUES `)
			args := make([]any, 0, 4*len(batch))
			for i, page := range batch {
				if i > 0 {
					query.WriteByte(',')
				}
				query.WriteString(`(?,?,?,?)`)
				args = append(args, id, file.path, page.idx, page.data)
				size += int64(len(page.data))
			}
			query.WriteString(` ON CONFLICT(tenant_id,path,idx) DO UPDATE SET data=excluded.data`)
			if _, err = tx.ExecContext(ctx, query.String(), args...); err != nil {
				return err
			}
			pages += int64(len(batch))
		}
	}
	for _, rel := range delta.gone {
		for _, table := range []string{"vault_files", "vault_pages"} {
			if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE tenant_id=? AND path=?`, id, rel); err != nil {
				return err
			}
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT CAST(COALESCE(SUM(size),0) AS BIGINT) FROM vault_files WHERE tenant_id=?`, id).Scan(&delta.total); err != nil {
		return err
	}
	now := millis(r.vault.now())
	if seq == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO vault_heads(tenant_id,seq,node,at,bytes) VALUES(?,?,?,?,?)`, id, next, r.node, now, delta.total)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE vault_heads SET seq=?, node=?, at=?, bytes=? WHERE tenant_id=?`, next, r.node, now, delta.total, id)
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	r.seq = next
	r.vault.counters.checkpoints.Add(1)
	r.vault.counters.pages.Add(pages)
	r.vault.counters.bytes.Add(size)
	return nil
}

// promote makes what was just stored the base the next checkpoint is
// compared with.
func (r *replica) promote(delta *vaultDelta) error {
	for _, file := range delta.files {
		base := r.base(file.path)
		if err := os.MkdirAll(filepath.Dir(base), 0o700); err != nil {
			return err
		}
		if file.moved {
			if err := os.Rename(file.image, base); err != nil {
				return err
			}
		} else {
			source, err := os.Open(file.image)
			if err != nil {
				return err
			}
			err = replaceFile(base, source)
			if err = errors.Join(err, source.Close()); err != nil {
				return err
			}
		}
		r.known[file.path] = true
	}
	for _, rel := range delta.gone {
		if err := removeIfExists(r.base(rel)); err != nil {
			return err
		}
		delete(r.known, rel)
		delete(r.stamps, rel)
		if source, ok := r.sources[rel]; ok {
			delete(r.sources, rel)
			if err := source.close(); err != nil {
				return err
			}
		}
	}
	return writeHead(r.dir, vaultHead{Seq: r.seq, Born: r.born})
}

func (r *replica) closeSources() error {
	var err error
	for rel, source := range r.sources {
		err = errors.Join(err, source.close())
		delete(r.sources, rel)
	}
	return err
}

// seal stores the last checkpoint of a tenant whose App is closed and marks
// the node's copy as matching the vault, so the node may reuse it.
func (r *replica) seal(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lost {
		return errors.Join(errFenced, r.closeSources(), removeIfExists(headPath(r.dir)))
	}
	_, err := r.ship(ctx)
	err = errors.Join(err, r.closeSources())
	if err != nil {
		return err
	}
	return writeHead(r.dir, vaultHead{Seq: r.seq, Born: r.born, Clean: true})
}

// abandon closes the link without storing anything: the copy is read from
// the vault again before it is used.
func (r *replica) abandon() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lost = true
	return errors.Join(r.closeSources(), removeIfExists(headPath(r.dir)))
}

func (r *replica) fenced() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lost
}

// ---- the fleet's side ----

// checkpoints copies an open tenant's changes to the vault until the tenant
// is closed.
func (f *Fleet) checkpoints(ctx context.Context, t *tenant) {
	defer close(t.synced)
	ticker := time.NewTicker(f.config.Checkpoint)
	defer ticker.Stop()
	failing := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		_, err := t.replica.checkpoint(ctx)
		switch {
		case err == nil:
			failing = false
		case errors.Is(err, errFenced) || errors.Is(err, errCacheBroken):
			f.lose(t, err)
			return
		case ctx.Err() == nil && !failing:
			// Said once per outage; the next tick tries again.
			failing = true
			fmt.Fprintf(f.log, "fleet: checkpoint %s: %v\n", t.id, err)
		}
	}
}

// barrier stores the tenant's state before something leaves the node that
// cannot be taken back. It fails when the state could not be stored; the
// caller must then not let it out.
func (f *Fleet) barrier(ctx context.Context, t *tenant) error {
	if t.replica == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	_, err := t.replica.checkpoint(ctx)
	if errors.Is(err, errFenced) || errors.Is(err, errCacheBroken) {
		f.lose(t, err)
	}
	return err
}

// lose closes a tenant the vault no longer takes from this node. Nothing it
// did since its last checkpoint left the node, and its holder runs it from
// that checkpoint.
func (f *Fleet) lose(t *tenant, cause error) {
	f.mu.Lock()
	closing := t.closing
	t.closing = true
	f.mu.Unlock()
	if closing {
		return
	}
	f.counters.abandons.Add(1)
	fmt.Fprintf(f.log, "fleet: giving up %s: %v\n", t.id, cause)
	go f.hibernate(t, time.Time{})
}

// heldResponse keeps a handler's answer until the barrier has passed.
type heldResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (h *heldResponse) Header() http.Header { return h.header }

func (h *heldResponse) WriteHeader(status int) {
	if h.status == 0 {
		h.status = status
	}
}

func (h *heldResponse) Write(data []byte) (int, error) {
	h.WriteHeader(http.StatusOK)
	return h.body.Write(data)
}

func (h *heldResponse) release(w http.ResponseWriter) {
	for name, values := range h.header {
		w.Header()[name] = values
	}
	h.WriteHeader(http.StatusOK)
	w.WriteHeader(h.status)
	_, _ = w.Write(h.body.Bytes())
}

// serveStored runs an owner's write and answers only once its result is in
// the vault. An owner who was told a write went through never sees it undone
// by a node that stopped.
func (f *Fleet) serveStored(w http.ResponseWriter, r *http.Request, t *tenant, handler http.Handler) {
	if t.replica == nil || r.Method == http.MethodGet || r.Method == http.MethodHead {
		handler.ServeHTTP(w, r)
		return
	}
	held := &heldResponse{header: http.Header{}}
	handler.ServeHTTP(held, r)
	if err := f.barrier(r.Context(), t); err != nil {
		if !errors.Is(err, errFenced) && !errors.Is(err, errCacheBroken) {
			fmt.Fprintf(f.log, "fleet: store %s before answering: %v\n", t.id, err)
		}
		busy(w, 1)
		return
	}
	held.release(w)
}

// sweepCache drops this node's copies of tenants it last closed before
// cutoff. The vault has them, so the next open reads them from there.
func (f *Fleet) sweepCache(ctx context.Context, cutoff time.Time) {
	root := filepath.Join(f.config.DataDir, "tenants")
	shards, err := os.ReadDir(root)
	if err != nil {
		return
	}
	dropped := 0
	for _, shard := range shards {
		entries, err := os.ReadDir(filepath.Join(root, shard.Name()))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			id := TenantID(entry.Name())
			dir := filepath.Join(root, shard.Name(), entry.Name())
			if ctx.Err() != nil || dropped == 64 {
				return
			}
			if !entry.IsDir() || !id.Valid() || dir != f.dir(id) {
				continue
			}
			info, err := os.Stat(headPath(dir))
			if err != nil {
				if info, err = entry.Info(); err != nil {
					continue
				}
			}
			if info.ModTime().After(cutoff) {
				continue
			}
			f.mu.Lock()
			if _, open := f.tenants[id]; open || f.closed {
				f.mu.Unlock()
				continue
			}
			hold := &tenant{id: id, closing: true, gone: make(chan struct{})}
			f.tenants[id] = hold
			f.mu.Unlock()
			// The placement keeps a node that shares this directory from
			// opening the copy while it goes; a deleted tenant has no row to
			// place and its copy goes too.
			err = f.place(ctx, id)
			if err == nil || errors.Is(err, ErrTenantUnknown) {
				if removed := os.RemoveAll(dir); removed != nil {
					fmt.Fprintf(f.log, "fleet: drop the cached copy of %s: %v\n", id, removed)
				} else {
					dropped++
				}
			}
			if err == nil {
				if err := f.registry.unplace(context.WithoutCancel(ctx), id, f.node); err != nil {
					fmt.Fprintf(f.log, "fleet: release %s after dropping its copy: %v\n", id, err)
				}
			}
			f.mu.Lock()
			delete(f.tenants, id)
			f.mu.Unlock()
			close(hold.gone)
		}
	}
}
