package assistant

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
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
	"sort"
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
//   - A checkpoint is refused unless the tenant's head row still names the
//     node as its writer and the sequence number is the one it last wrote. A
//     node the others took for dead cannot write over the tenant's new
//     holder, which became the writer when it opened the tenant.
//   - Every file is stored with its SHA-256, and a node that reads a tenant
//     checks each file against it, so pages that were lost or mixed up stop
//     the tenant from opening instead of opening as a damaged database.
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
	moves, purges, freezes, thaws, corrupt, cacheDrops    atomic.Int64
	// frozenBytes is what the archived tenants' files held, archiveBytes what
	// their archives hold.
	frozenBytes, archiveBytes atomic.Int64
	// The objects of large files: written and their bytes, read, removed,
	// and the times a large file went as pages instead.
	objectPuts, objectBytes, objectGets, objectRemoves, fallbacks atomic.Int64
}

// vault is where the fleet keeps its tenants' data: the shards (see shard),
// and the archive store for the tenants nobody has needed for long (see
// coldStore).
type vault struct {
	registry *registry
	shards   map[string]*shard
	cold     coldStore
	now      func() time.Time
	// tmp holds archives on their way to or from the archive store.
	tmp string
	// largeFile is the size from which a file keeps its bulk in cold; zero
	// when none does.
	largeFile int64
	counters  vaultCounters
	// storeDown is when, in v.now's nanoseconds, this node next asks the
	// store from a checkpoint after it failed (see storeAway).
	storeDown atomic.Int64
	// pause runs between the steps of a change to where a tenant's data is;
	// tests use it to hold the node up at the worst moment.
	pause func(step string)
}

// vaultHead is the note a node leaves in its copy of a tenant: which
// checkpoint the copy matches and whether it was closed cleanly. Born is the
// tenant's creation time, so the copy of a deleted tenant is never taken for
// a new tenant with the same name. Mark names the checkpoint: sequence
// numbers start again when a tenant's data is written anew, and a shard
// restored from a backup counts on from an older one. Bytes is roughly what
// the copy takes on the node's disk.
type vaultHead struct {
	Seq   int64  `json:"seq"`
	Born  int64  `json:"born"`
	Mark  string `json:"mark,omitempty"`
	Bytes int64  `json:"bytes,omitempty"`
	Clean bool   `json:"clean"`
	// Ahead marks a copy that was closed in order but whose last changes
	// could not be stored: it is the vault's checkpoint and more.
	Ahead bool `json:"ahead,omitempty"`
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
	shard *shard
	id    TenantID
	node  string
	dir   string
	born  int64
	// archive names the object an archived tenant is read back from.
	archive string

	mu  sync.Mutex
	seq int64
	// mark names the checkpoint the vault holds; total is the size of the
	// tenant's files there.
	mark  string
	total int64
	// lost is set once the vault refused this node; nothing more is written.
	lost bool
	// known is every file the vault holds for the tenant; stamps remembers
	// the plain files as they were when last compared; layout is how the
	// shard keeps the files that have an object in the archive store.
	known   map[string]bool
	stamps  map[string]fileStamp
	layout  map[string]storedFile
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
// read from the vault. The caller holds the tenant's placement and read
// record after it took it, so the row says where the tenant's data is.
func (v *vault) attach(ctx context.Context, record TenantRecord, node, dir string) (*replica, bool, error) {
	id, name, archived := record.ID, record.Shard, record.State == TenantCold
	if name == "" {
		var err error
		if name, err = v.registry.assign(ctx, id); err != nil {
			return nil, false, fmt.Errorf("find a shard for tenant %s: %w", id, err)
		}
	}
	shard, err := v.shard(name)
	if err != nil {
		return nil, false, err
	}
	r := &replica{vault: v, shard: shard, id: id, node: node, dir: dir, born: millis(record.CreatedAt), archive: record.archive, known: map[string]bool{}, stamps: map[string]fileStamp{}, layout: map[string]storedFile{}, sources: map[string]*vaultSource{}}
	// From here on this node is the tenant's writer, and what the head says
	// stays true until the node writes it. An archived tenant's shard is
	// emptied: the archive is its data.
	head, err := v.take(ctx, shard, id, node, r.born, archived)
	if err != nil {
		return nil, false, fmt.Errorf("take tenant %s in shard %s: %w", id, name, err)
	}
	r.seq, r.mark = head.seq, head.mark
	local, ok := readHead(dir)
	pulled := false
	switch {
	case archived:
		if err := r.thaw(ctx); err != nil {
			return nil, false, errors.Join(fmt.Errorf("read tenant %s back from the archive: %w", id, err), r.closeSources())
		}
		pulled = true
	case ok && (local.Clean || local.Ahead) && local.Born == r.born && r.seq > 0 && local.Seq == r.seq && local.Mark == r.mark && r.mark != "":
		// The copy is what this node last wrote and nobody wrote since. One
		// that is ahead holds changes the vault lacks; the next checkpoint
		// stores them.
		if err := r.loadKnown(); err != nil {
			return nil, false, err
		}
		err := shard.do(ctx, func(db *fleetDB) (err error) {
			r.layout, err = layouts(ctx, db, id)
			return err
		})
		if err != nil {
			return nil, false, errors.Join(fmt.Errorf("read how shard %s keeps tenant %s: %w", name, id, err), r.closeSources())
		}
		r.total = head.bytes
		v.counters.reuses.Add(1)
	default:
		if err := r.pull(ctx, head.bytes); err != nil {
			if errors.Is(err, errCorrupt) {
				v.counters.corrupt.Add(1)
			}
			return nil, false, fmt.Errorf("read tenant %s from shard %s: %w", id, name, err)
		}
		pulled = r.seq > 0
	}
	// Until it is closed cleanly the copy may be ahead of the vault.
	if err := writeHead(dir, r.head(false)); err != nil {
		return nil, false, errors.Join(err, r.closeSources())
	}
	return r, pulled, nil
}

// head is the note for the node's copy. The copy holds each file twice, as
// the tenant uses it and as the vault has it.
func (r *replica) head(clean bool) vaultHead {
	return vaultHead{Seq: r.seq, Born: r.born, Mark: r.mark, Bytes: 2 * r.total, Clean: clean}
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
// with, to base. One snapshot transaction reads it all, in the time size
// bytes take.
func (r *replica) pull(ctx context.Context, size int64) error {
	ctx, cancel := bounded(ctx, size)
	defer cancel()
	if err := os.RemoveAll(r.dir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(r.dir, vaultDir), 0o700); err != nil {
		return err
	}
	if r.seq == 0 {
		return nil
	}
	return r.shard.do(ctx, func(db *fleetDB) error {
		tx, err := db.begin(ctx, true)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var seq int64
		if err := tx.QueryRowContext(ctx, `SELECT seq FROM vault_heads WHERE tenant_id=? AND node=?`, string(r.id), r.node).Scan(&seq); err != nil || seq != r.seq {
			// Another node became the writer since this one did.
			return errors.Join(err, errFenced)
		}
		files, total, err := storedFiles(ctx, tx, r.id)
		if err != nil {
			return err
		}
		for _, file := range files {
			if err := r.pullFile(ctx, tx, file); err != nil {
				return fmt.Errorf("%s: %w", file.path, err)
			}
			r.known[file.path] = true
			r.layout[file.path] = file
		}
		r.total = total
		r.vault.counters.pulls.Add(1)
		r.vault.counters.pulledBytes.Add(total)
		return nil
	})
}

// pullFile writes one stored file to the directory and, as the image the
// next checkpoint is compared with, to base.
func (r *replica) pullFile(ctx context.Context, tx *fleetTx, stored storedFile) (err error) {
	path := filepath.Join(r.dir, filepath.FromSlash(stored.path))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if err := r.vault.writeFile(ctx, tx, r.id, r.born, stored, file); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return replaceFile(r.base(stored.path), file)
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
	sum   string
	pages []vaultPageData
	// image is the file the pages were read from, which becomes the base
	// image once they are stored; moved says it may be renamed there.
	image string
	moved bool
	// object names the new object that holds the file now, and whole says a
	// file that had one goes back to pages, all of them (see stage). overlay
	// counts the pages of a file with an object once they are stored.
	object  string
	whole   bool
	overlay int64
}

// compare reads a file against its base image and returns the pages that
// differ and the file's checksum. changed is false when the two are the same.
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
	digest := sha256.New()
	defer func() { file.sum = hex.EncodeToString(digest.Sum(nil)) }()
	for idx := 0; ; idx++ {
		n, err := io.ReadFull(current, page)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return file, false, err
		}
		if n == 0 {
			break
		}
		digest.Write(page[:n])
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
	// added are the objects the checkpoint refers to anew, dropped those it
	// stops referring to.
	added, dropped []string
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
		image := path
		if r.tiered(rel, info.Size()) {
			if image, err = r.pin(rel, path); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return false, err
			}
		}
		file, changed, err := r.compare(rel, image)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		stamps[rel] = stamp
		switch {
		case changed:
			file.moved = image != path
			delta.files = append(delta.files, file)
		case image != path:
			if err := os.Remove(image); err != nil {
				return false, err
			}
		}
	}
	written := len(delta.files) > 0 || len(delta.gone) > 0
	if written {
		if err := r.stage(ctx, &delta); err != nil {
			return false, err
		}
		if err := r.push(ctx, &delta); err != nil {
			if errors.Is(err, errFenced) {
				r.lost = true
			}
			r.unstage(ctx, &delta)
			return false, err
		}
		r.settle(ctx, &delta)
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

// push writes one checkpoint. The first statement takes the tenant's head row
// and advances it, and changes nothing unless the head still names this node
// as the writer at the checkpoint the node last wrote: a node that took the
// tenant waits for this checkpoint or makes it fail.
func (r *replica) push(ctx context.Context, delta *vaultDelta) error {
	var changed int64
	for _, file := range delta.files {
		changed += int64(len(file.pages)) * vaultPage
	}
	ctx, cancel := bounded(ctx, changed)
	defer cancel()
	next, mark := r.seq+1, randomHex(8)
	var pages, size int64
	err := r.shard.do(ctx, func(db *fleetDB) (err error) {
		tx, err := db.begin(ctx, false)
		if err != nil {
			return err
		}
		defer func() {
			if err != nil {
				err = errors.Join(err, tx.Rollback())
			}
		}()
		id := string(r.id)
		if ours, err := tx.affected(ctx, `UPDATE vault_heads SET seq=?, mark=?, at=? WHERE tenant_id=? AND node=? AND seq=?`,
			next, mark, millis(r.vault.now()), id, r.node, r.seq); err != nil || !ours {
			return errors.Join(err, errFenced)
		}
		pages, size = 0, 0
		for i := range delta.files {
			file := &delta.files[i]
			// Pages past the end go, and all of them when the file's bulk
			// is a new object or it has just left its object.
			keep := (file.size + vaultPage - 1) / vaultPage
			if file.object != "" || file.whole {
				keep = 0
				if _, err = tx.ExecContext(ctx, `INSERT INTO vault_files(tenant_id,path,size,seq,sum,base,base_size,base_sum,overlay) VALUES(?,?,?,?,?,?,?,?,0)
					ON CONFLICT(tenant_id,path) DO UPDATE SET size=excluded.size, seq=excluded.seq, sum=excluded.sum,
					base=excluded.base, base_size=excluded.base_size, base_sum=excluded.base_sum, overlay=0`,
					id, file.path, file.size, next, file.sum, file.object, objectSize(file), objectSum(file)); err != nil {
					return err
				}
			} else if _, err = tx.ExecContext(ctx, `INSERT INTO vault_files(tenant_id,path,size,seq,sum) VALUES(?,?,?,?,?)
				ON CONFLICT(tenant_id,path) DO UPDATE SET size=excluded.size, seq=excluded.seq, sum=excluded.sum`, id, file.path, file.size, next, file.sum); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM vault_pages WHERE tenant_id=? AND path=? AND idx>=?`, id, file.path, keep); err != nil {
				return err
			}
			for batch := range slices.Chunk(file.pages, vaultBatch) {
				if err = insertPages(ctx, tx, r.id, file.path, batch); err != nil {
					return err
				}
				for _, page := range batch {
					size += int64(len(page.data))
				}
				pages += int64(len(batch))
			}
			if file.object == "" && !file.whole && r.layout[file.path].base != "" {
				if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM vault_pages WHERE tenant_id=? AND path=?`, id, file.path).Scan(&file.overlay); err != nil {
					return err
				}
				if _, err = tx.ExecContext(ctx, `UPDATE vault_files SET overlay=? WHERE tenant_id=? AND path=?`, file.overlay, id, file.path); err != nil {
					return err
				}
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
		if _, err = tx.ExecContext(ctx, `UPDATE vault_heads SET bytes=? WHERE tenant_id=?`, delta.total, id); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return err
	}
	r.seq, r.mark, r.total = next, mark, delta.total
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
		switch layout := r.layout[file.path]; {
		case file.object != "":
			r.layout[file.path] = storedFile{path: file.path, size: file.size, sum: file.sum, base: file.object, baseSize: file.size, baseSum: file.sum}
		case file.whole || layout.base == "":
			delete(r.layout, file.path)
		default:
			layout.size, layout.sum, layout.overlay = file.size, file.sum, file.overlay
			r.layout[file.path] = layout
		}
	}
	for _, rel := range delta.gone {
		if err := removeIfExists(r.base(rel)); err != nil {
			return err
		}
		delete(r.known, rel)
		delete(r.stamps, rel)
		delete(r.layout, rel)
		if source, ok := r.sources[rel]; ok {
			delete(r.sources, rel)
			if err := source.close(); err != nil {
				return err
			}
		}
	}
	return writeHead(r.dir, r.head(false))
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
// the node's copy as matching the vault, or as ahead of it when the vault
// could not be written, so the node may reuse it.
func (r *replica) seal(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lost {
		return errors.Join(errFenced, r.closeSources(), removeIfExists(headPath(r.dir)))
	}
	_, err := r.ship(ctx)
	closed := r.closeSources()
	switch {
	case errors.Is(err, errFenced) || errors.Is(err, errCacheBroken):
		return errors.Join(err, closed, removeIfExists(headPath(r.dir)))
	case err != nil && closed == nil:
		// The vault did not take the last changes. The tenant's files were
		// closed in order and the images of what the vault has are intact,
		// so if nobody writes the tenant before this node opens it again,
		// it goes on from here and nothing is lost.
		head := r.head(false)
		head.Ahead = true
		return errors.Join(err, writeHead(r.dir, head))
	case err != nil || closed != nil:
		return errors.Join(err, closed)
	}
	return writeHead(r.dir, r.head(true))
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

// sweepCache bounds this node's copies of tenants. A copy the node last
// closed before cutoff goes (a zero cutoff keeps them all), and then the
// oldest closed copies go until the copies fit in budget bytes (zero is no
// bound). The vault has every one of them, so the next open reads the tenant
// from there.
func (f *Fleet) sweepCache(ctx context.Context, cutoff time.Time, budget int64) {
	type cached struct {
		id    TenantID
		dir   string
		at    time.Time
		bytes int64
	}
	root := filepath.Join(f.config.DataDir, "tenants")
	shards, err := os.ReadDir(root)
	if err != nil {
		return
	}
	copies := []cached{}
	var total int64
	for _, shard := range shards {
		entries, err := os.ReadDir(filepath.Join(root, shard.Name()))
		if err != nil {
			continue
		}
		for _, entry := range entries {
			id := TenantID(entry.Name())
			dir := filepath.Join(root, shard.Name(), entry.Name())
			if !entry.IsDir() || !id.Valid() || dir != f.dir(id) {
				continue
			}
			info, err := os.Stat(headPath(dir))
			if err != nil {
				if info, err = entry.Info(); err != nil {
					continue
				}
			}
			head, _ := readHead(dir)
			total += head.Bytes
			copies = append(copies, cached{id, dir, info.ModTime(), head.Bytes})
		}
	}
	sort.Slice(copies, func(i, j int) bool { return copies[i].at.Before(copies[j].at) })
	dropped := 0
	for _, c := range copies {
		old := !cutoff.IsZero() && !c.at.After(cutoff)
		if ctx.Err() != nil || dropped == 256 || (!old && (budget <= 0 || total <= budget)) {
			break
		}
		// A tenant that is open here keeps its copy whatever the budget.
		release, ok := f.reserve(c.id)
		if !ok {
			continue
		}
		// The placement keeps a node that shares this directory from opening
		// the copy while it goes; a deleted tenant has no row to place and
		// its copy goes too.
		err := f.place(ctx, c.id)
		if err == nil || errors.Is(err, ErrTenantUnknown) {
			if removed := os.RemoveAll(c.dir); removed != nil {
				fmt.Fprintf(f.log, "fleet: drop the cached copy of %s: %v\n", c.id, removed)
			} else {
				dropped++
				total -= c.bytes
				f.vault.counters.cacheDrops.Add(1)
			}
		}
		if err == nil {
			if err := f.registry.unplace(context.WithoutCancel(ctx), c.id, f.node); err != nil {
				fmt.Fprintf(f.log, "fleet: release %s after dropping its copy: %v\n", c.id, err)
			}
		}
		release()
	}
	f.cacheBytes.Store(total)
}
