package assistant

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// largeFiles configures a node with a directory archive store and keeps the
// files of 64 KiB or more in it.
func largeFiles(root string) func(*FleetConfig) {
	return func(c *FleetConfig) {
		c.IdleTTL, c.Archive, c.LargeFile = time.Second, ArchiveStore{URL: "file://" + root}, 64<<10
	}
}

// noise is n bytes that do not compress, different for each seed.
func noise(n int, seed string) []byte {
	out := make([]byte, 0, n+sha256.Size)
	block := sha256.Sum256([]byte(seed))
	for len(out) < n {
		out = append(out, block[:]...)
		block = sha256.Sum256(block[:])
	}
	return out[:n]
}

func writeTenantFile(t *testing.T, fleet *testNode, id TenantID, rel string, content []byte) {
	t.Helper()
	path := filepath.Join(fleet.dir(id), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// layoutIn reads how a shard keeps a tenant's files.
func layoutIn(t *testing.T, fleet *Fleet, shard string, id TenantID) map[string]storedFile {
	t.Helper()
	s, err := fleet.vault.shard(shard)
	if err != nil {
		t.Fatal(err)
	}
	db, err := s.use(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found, err := layouts(context.Background(), db, id)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// pagesIn counts the pages a shard holds of one file.
func pagesIn(t *testing.T, fleet *Fleet, shard string, id TenantID, rel string) int {
	t.Helper()
	s, _ := fleet.vault.shard(shard)
	db, err := s.use(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var pages int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM vault_pages WHERE tenant_id=? AND path=?`, string(id), rel).Scan(&pages); err != nil {
		t.Fatal(err)
	}
	return pages
}

// objectsMatch checks that the store holds an object for each of the tenant's
// files that has one, and nothing else.
func objectsMatch(t *testing.T, fleet *Fleet, root string, id TenantID) map[string]storedFile {
	t.Helper()
	row := record(t, fleet, id)
	files := layoutIn(t, fleet, row.Shard, id)
	based := 0
	for _, file := range files {
		if file.base == "" {
			continue
		}
		based++
		if !exists(filepath.Join(root, filepath.FromSlash(fileKey(id, millis(row.CreatedAt), file.base)))) {
			t.Fatalf("the object of %s is missing", file.path)
		}
	}
	if got := objects(t, root); got != based {
		t.Fatalf("the store holds %d objects for %d large files", got, based)
	}
	return files
}

func readTenantFile(t *testing.T, fleet *testNode, id TenantID, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fleet.dir(id), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// A file of LargeFile or more keeps its bulk as one object in the archive
// store, and its shard only the pages that differ from it. A file rewritten
// whole becomes a new object and the old one goes; one that shrinks goes back
// to pages; and any node reads the file as it was written.
func TestLargeFilesKeepTheirBulkInTheArchiveStore(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t, "s1")
	root := t.TempDir()
	a := storedNode(t, clock, database(), largeFiles(root))
	b := storedNode(t, clock, database(), largeFiles(root))
	createTenant(t, a.Fleet, "alice")
	touch(t, a.Fleet, "alice")
	const big = "notes/big.bin"
	content := noise(50*vaultPage+123, "first")
	writeTenantFile(t, a, "alice", big, content)
	writeTenantFile(t, a, "alice", "notes/small.txt", []byte("stays in pages"))
	checkpoint(t, a.Fleet, "alice")
	shard := shardOf(t, a.Fleet, "alice")
	files := objectsMatch(t, a.Fleet, root, "alice")
	first := files[big]
	if first.base == "" || first.baseSize != int64(len(content)) || first.overlay != 0 || pagesIn(t, a.Fleet, shard, "alice", big) != 0 || files["notes/small.txt"].base != "" {
		t.Fatalf("a large file is one object and no pages; a small one is pages: %+v", files)
	}
	if got := stats(t, a.Fleet); got.ObjectsStored < 1 || got.ObjectBytes < int64(len(content)) || got.LargeFile != 64<<10 || got.LargeFallbacks != 0 {
		t.Fatalf("stats: %+v", got)
	}
	stored := stats(t, a.Fleet).ObjectsStored

	// One page changes: only that page goes to the shard.
	content[10*vaultPage+5] ^= 0xff
	writeTenantFile(t, a, "alice", big, content)
	checkpoint(t, a.Fleet, "alice")
	files = objectsMatch(t, a.Fleet, root, "alice")
	if got := files[big]; got.base != first.base || got.overlay != 1 || pagesIn(t, a.Fleet, shard, "alice", big) != 1 || stats(t, a.Fleet).ObjectsStored != stored {
		t.Fatalf("a changed page is a page over the object: %+v", got)
	}
	// It grows by a page: its last page is full now and a new one follows.
	content = append(content, noise(vaultPage, "more")...)
	writeTenantFile(t, a, "alice", big, content)
	checkpoint(t, a.Fleet, "alice")
	if got := objectsMatch(t, a.Fleet, root, "alice")[big]; got.base != first.base || got.overlay != 3 || got.size != int64(len(content)) {
		t.Fatalf("a file that grows keeps its object: %+v", got)
	}

	// The node that wrote it last opens its own copy again and goes on
	// from how the shard keeps the file.
	sleep(t, clock, a.Fleet)
	reused := stats(t, a.Fleet).CacheReuses
	touch(t, a.Fleet, "alice")
	content[20*vaultPage] ^= 0xff
	writeTenantFile(t, a, "alice", big, content)
	checkpoint(t, a.Fleet, "alice")
	if got := objectsMatch(t, a.Fleet, root, "alice")[big]; stats(t, a.Fleet).CacheReuses != reused+1 || got.base != first.base || got.overlay != 4 {
		t.Fatalf("a reused copy keeps the file's object: %+v", got)
	}

	// Another node reads it whole.
	sleep(t, clock, a.Fleet)
	touch(t, b.Fleet, "alice")
	if got := readTenantFile(t, b, "alice", big); !bytes.Equal(got, content) {
		t.Fatalf("the file is read as it was written: %d bytes of %d", len(got), len(content))
	}
	if got := stats(t, b.Fleet); got.ObjectsRead < 1 || got.Corrupt != 0 {
		t.Fatalf("stats: %+v", got)
	}

	// Rewritten whole: a new object, and the old one goes.
	content = noise(len(content), "second")
	writeTenantFile(t, b, "alice", big, content)
	checkpoint(t, b.Fleet, "alice")
	files = objectsMatch(t, b.Fleet, root, "alice")
	if got := files[big]; got.base == "" || got.base == first.base || got.overlay != 0 || pagesIn(t, b.Fleet, shard, "alice", big) != 0 || stats(t, b.Fleet).ObjectsRemoved < 1 {
		t.Fatalf("a rewritten file is a new object and the old one is removed: %+v", got)
	}
	if stats(t, b.Fleet).Leftovers != 0 {
		t.Fatal("nothing is left written down once a checkpoint is stored")
	}

	// Below half the size it goes back to pages, and its object goes.
	content = content[:20<<10]
	writeTenantFile(t, b, "alice", big, content)
	checkpoint(t, b.Fleet, "alice")
	if got := objectsMatch(t, b.Fleet, root, "alice")[big]; got.base != "" || pagesIn(t, b.Fleet, shard, "alice", big) != 5 {
		t.Fatalf("a file that shrank is pages again: %+v", got)
	}
	sleep(t, clock, b.Fleet)
	touch(t, a.Fleet, "alice")
	if got := readTenantFile(t, a, "alice", big); !bytes.Equal(got, content) {
		t.Fatalf("and is read as it was written: %d bytes", len(got))
	}

	// A large file that is removed takes its object with it, and so does a
	// deleted tenant.
	writeTenantFile(t, a, "alice", "notes/gone.bin", noise(30*vaultPage, "gone"))
	checkpoint(t, a.Fleet, "alice")
	if objectsMatch(t, a.Fleet, root, "alice")["notes/gone.bin"].base == "" {
		t.Fatal("the new large file is an object")
	}
	if err := os.Remove(filepath.Join(a.dir("alice"), "notes", "gone.bin")); err != nil {
		t.Fatal(err)
	}
	checkpoint(t, a.Fleet, "alice")
	objectsMatch(t, a.Fleet, root, "alice")
	say(t, a.Fleet, "alice", "the database's changes")
	checkpoint(t, a.Fleet, "alice")
	objectsMatch(t, a.Fleet, root, "alice")
	sleep(t, clock, a.Fleet)
	if err := b.DeleteTenant(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if objects(t, root) != 0 || stats(t, b.Fleet).Leftovers != 0 {
		t.Fatalf("a deleted tenant's objects go with it: %d", objects(t, root))
	}
}

// A tenant's large files go where the tenant goes: a move copies the
// references, and both copies' objects are the same; archiving puts the files
// whole into the archive, and the objects go; reading it back stores them as
// new objects.
func TestLargeFilesGoWhereTheirTenantGoes(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t, "s1", "s2")
	root := t.TempDir()
	tune := both(largeFiles(root), func(c *FleetConfig) { c.DormantAfter, c.ArchiveAfter = 3*day, 7*day })
	a := storedNode(t, clock, database(), tune)
	b := storedNode(t, clock, database(), tune)
	createTenant(t, a.Fleet, "alice")
	touch(t, a.Fleet, "alice")
	const big = "files/report.pdf"
	content := noise(40*vaultPage+9, "report")
	writeTenantFile(t, a, "alice", big, content)
	say(t, a.Fleet, "alice", "written before the move")
	sleep(t, clock, a.Fleet)
	first := objectsMatch(t, a.Fleet, root, "alice")[big]

	from := shardOf(t, a.Fleet, "alice")
	if err := a.relocate(ctx, "alice", from); err != nil {
		t.Fatal(err)
	}
	to := shardOf(t, a.Fleet, "alice")
	if got := objectsMatch(t, a.Fleet, root, "alice")[big]; to == from || got.base != first.base || stored(t, a.Fleet, from, "alice") != 0 || stats(t, a.Fleet).Leftovers != 0 {
		t.Fatalf("the moved copy refers to the same object and the old copy is gone: %s→%s %+v", from, to, got)
	}
	touch(t, b.Fleet, "alice")
	if got := readTenantFile(t, b, "alice", big); !bytes.Equal(got, content) {
		t.Fatal("the moved tenant's large file is read whole")
	}
	sleep(t, clock, b.Fleet)

	freeze(t, b.Fleet, "alice")
	if row := record(t, b.Fleet, "alice"); row.State != TenantCold || objects(t, root) != 1 || !exists(archiveFile(t, root, b.Fleet, "alice")) || stats(t, b.Fleet).Leftovers != 0 {
		t.Fatalf("an archived tenant is its archive alone: %+v, %d objects", row, objects(t, root))
	}
	if err := os.RemoveAll(a.dir("alice")); err != nil {
		t.Fatal(err)
	}
	if text := channelText(t, a.Fleet, "alice"); !strings.Contains(text, "written before the move") {
		t.Fatalf("the archive holds the tenant: %q", text)
	}
	if got := readTenantFile(t, a, "alice", big); !bytes.Equal(got, content) {
		t.Fatal("the archive holds the large file whole")
	}
	thawed := objectsMatch(t, a.Fleet, root, "alice")[big]
	sleep(t, clock, a.Fleet)
	freeze(t, a.Fleet, "alice")
	// A row of an object of a tenant that is archived, as a node that
	// stopped half way leaves it: the archive holds the file, and the object
	// goes.
	if err := a.registry.intend(ctx, leftover{"alice", millis(record(t, a.Fleet, "alice").CreatedAt), fileAt(thawed.base)}, clock.Now()); err != nil {
		t.Fatal(err)
	}
	a.collect(ctx)
	if objects(t, root) != 1 || stats(t, a.Fleet).Leftovers != 0 {
		t.Fatalf("the janitor removes the objects of an archived tenant: %d objects", objects(t, root))
	}
	touch(t, a.Fleet, "alice")
	thawed = objectsMatch(t, a.Fleet, root, "alice")[big]
	if thawed.base == "" || thawed.base == first.base {
		t.Fatalf("read back, the file is a new object: %+v", thawed)
	}
	sleep(t, clock, a.Fleet)
	if err := a.DeleteTenant(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if objects(t, root) != 0 || stats(t, a.Fleet).Leftovers != 0 {
		t.Fatalf("nothing is left of a deleted tenant: %d objects", objects(t, root))
	}
}

// flakyStore fails to store objects while it is down, does not answer
// stores and removals while it hangs, and fails to read objects while it is
// unreachable. It counts what it was asked to store and remove, and the
// requests that came without a deadline, which it fails at once.
type flakyStore struct {
	coldStore
	down, hangs, unreachable atomic.Bool
	puts, removes, unbounded atomic.Int64
}

func (s *flakyStore) hang(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		s.unbounded.Add(1)
		return errors.New("asked without a deadline")
	}
	<-ctx.Done()
	return ctx.Err()
}

func (s *flakyStore) remove(ctx context.Context, key string) error {
	s.removes.Add(1)
	if s.hangs.Load() {
		return s.hang(ctx)
	}
	return s.coldStore.remove(ctx, key)
}

func (s *flakyStore) get(ctx context.Context, key string, into io.Writer) error {
	if s.unreachable.Load() {
		return &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	}
	return s.coldStore.get(ctx, key, into)
}

func (s *flakyStore) put(ctx context.Context, key string, body io.ReadSeeker, size int64, sum [sha256.Size]byte) error {
	s.puts.Add(1)
	if s.hangs.Load() {
		return s.hang(ctx)
	}
	if s.down.Load() {
		return errors.New("the archive store does not answer")
	}
	return s.coldStore.put(ctx, key, body, size, sum)
}

// A checkpoint does not wait for the archive store: while it is away a large
// file goes to the shard as pages, and becomes an object once it is back and
// the node asks it again.
func TestLargeFileIsPagesWhileTheStoreIsAway(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t, "s1")
	root := t.TempDir()
	a := storedNode(t, clock, database(), largeFiles(root))
	flaky := &flakyStore{coldStore: a.vault.cold}
	a.vault.cold = flaky
	createTenant(t, a.Fleet, "alice")
	touch(t, a.Fleet, "alice")
	flaky.down.Store(true)
	const big = "notes/big.bin"
	content := noise(30*vaultPage, "away")
	writeTenantFile(t, a, "alice", big, content)
	checkpoint(t, a.Fleet, "alice")
	shard := shardOf(t, a.Fleet, "alice")
	if got := layoutIn(t, a.Fleet, shard, "alice")[big]; got.base != "" || pagesIn(t, a.Fleet, shard, "alice", big) != 30 {
		t.Fatalf("the file is stored as pages: %+v", got)
	}
	if got := stats(t, a.Fleet); got.LargeFallbacks < 1 || got.Leftovers < 1 {
		t.Fatalf("the fallback is counted, and the object that may exist is written down: %+v", got)
	}
	sleep(t, clock, a.Fleet)
	a.collect(ctx)
	if got := stats(t, a.Fleet); got.Leftovers != 0 || objects(t, root) != 0 {
		t.Fatalf("the janitor removes the object nothing refers to: %+v", got)
	}

	flaky.down.Store(false)
	touch(t, a.Fleet, "alice")
	content[0] ^= 1
	writeTenantFile(t, a, "alice", big, content)
	asked := flaky.puts.Load()
	checkpoint(t, a.Fleet, "alice")
	if got := layoutIn(t, a.Fleet, shard, "alice")[big]; got.base != "" || flaky.puts.Load() != asked {
		t.Fatalf("the store that failed is not asked again at once: %+v, %d puts", got, flaky.puts.Load()-asked)
	}
	clock.Set(clock.Now().Add(storeCooldown))
	touch(t, a.Fleet, "alice")
	content[0] ^= 1
	writeTenantFile(t, a, "alice", big, content)
	checkpoint(t, a.Fleet, "alice")
	if got := objectsMatch(t, a.Fleet, root, "alice")[big]; got.base == "" || pagesIn(t, a.Fleet, shard, "alice", big) != 0 {
		t.Fatalf("once the store is back the file is an object: %+v", got)
	}
	sleep(t, clock, a.Fleet)

	// A node that cannot reach the store cannot open the tenant, and the
	// store's trouble is not counted against the shard.
	b := storedNode(t, clock, database(), largeFiles(root))
	unreachable := &flakyStore{coldStore: b.vault.cold}
	unreachable.unreachable.Store(true)
	b.vault.cold = unreachable
	createTenant(t, b.Fleet, "bob")
	for range shardTroubles + 1 {
		if err := b.Use(ctx, "alice", func(*App) error { return nil }); err == nil || errors.Is(err, errCorrupt) {
			t.Fatalf("the tenant does not open without its large file: %v", err)
		}
	}
	s := b.vault.shards[shard]
	s.mu.Lock()
	troubles := s.troubles
	s.mu.Unlock()
	if troubles != 0 || shardInfo(t, b.Fleet, shard).Down {
		t.Fatalf("the store's trouble is not counted against the shard: %d", troubles)
	}
	touch(t, b.Fleet, "bob")
	unreachable.unreachable.Store(false)
	if got := channelText(t, b.Fleet, "alice"); got == "" {
		t.Fatal("once the store answers the tenant opens")
	}
	if got := readTenantFile(t, b, "alice", big); !bytes.Equal(got, content) {
		t.Fatal("with its large file")
	}
}

// An object is checked like a page: a tenant whose large file's object was
// changed or lost does not open, and says why.
func TestLargeFileObjectIsCheckedWhenRead(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t, "s1")
	root := t.TempDir()
	a := storedNode(t, clock, database(), largeFiles(root))
	b := storedNode(t, clock, database(), largeFiles(root))
	createTenant(t, a.Fleet, "alice")
	touch(t, a.Fleet, "alice")
	const big = "notes/big.bin"
	content := noise(20*vaultPage, "checked")
	writeTenantFile(t, a, "alice", big, content)
	checkpoint(t, a.Fleet, "alice")
	content[3*vaultPage] ^= 1
	writeTenantFile(t, a, "alice", big, content)
	checkpoint(t, a.Fleet, "alice")
	file := objectsMatch(t, a.Fleet, root, "alice")[big]
	if file.overlay != 1 {
		t.Fatalf("one page over the object: %+v", file)
	}
	sleep(t, clock, a.Fleet)
	s, _ := a.vault.shard(shardOf(t, a.Fleet, "alice"))
	db, _ := s.use(ctx)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	// A page over the object that is not the one written, and one that lies
	// outside the file.
	exec(`UPDATE vault_pages SET data=? WHERE tenant_id=? AND path=? AND idx=3`, make([]byte, vaultPage), "alice", big)
	if err := b.Use(ctx, "alice", func(*App) error { return nil }); !errors.Is(err, errCorrupt) || !strings.Contains(err.Error(), "does not match its checksum") {
		t.Fatalf("a damaged page over an object is noticed: %v", err)
	}
	exec(`UPDATE vault_pages SET data=? WHERE tenant_id=? AND path=? AND idx=3`, content[3*vaultPage:4*vaultPage], "alice", big)
	exec(`INSERT INTO vault_pages(tenant_id,path,idx,data) VALUES(?,?,?,?)`, "alice", big, 40, make([]byte, vaultPage))
	if err := b.Use(ctx, "alice", func(*App) error { return nil }); !errors.Is(err, errCorrupt) || !strings.Contains(err.Error(), "page 40") {
		t.Fatalf("a page outside the file is noticed: %v", err)
	}
	exec(`DELETE FROM vault_pages WHERE tenant_id=? AND path=? AND idx=40`, "alice", big)
	object := filepath.Join(root, filepath.FromSlash(fileKey("alice", millis(record(t, a.Fleet, "alice").CreatedAt), file.base)))
	if err := os.WriteFile(object, noise(20*vaultPage, "damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := b.Use(ctx, "alice", func(*App) error { return nil })
	if !errors.Is(err, errCorrupt) || !strings.Contains(err.Error(), "the object of "+big) {
		t.Fatalf("a changed object is noticed, and blamed: %v", err)
	}
	if err := os.Remove(object); err != nil {
		t.Fatal(err)
	}
	if err := b.Use(ctx, "alice", func(*App) error { return nil }); !errors.Is(err, errCorrupt) {
		t.Fatalf("a missing object is noticed: %v", err)
	}
	if got := stats(t, b.Fleet); got.Corrupt != 4 || b.opened("alice") != nil {
		t.Fatalf("the tenant is not opened: %+v", got)
	}
	// A row that names something other than an object the fleet writes is
	// not followed into the store.
	exec(`UPDATE vault_files SET base=? WHERE tenant_id=? AND path=?`, "../../elsewhere", "alice", big)
	if err := b.Use(ctx, "alice", func(*App) error { return nil }); !errors.Is(err, errCorrupt) || !strings.Contains(err.Error(), "names no object") {
		t.Fatalf("a row naming no object is refused: %v", err)
	}
}

// Data that is removed from a shard, because it is written anew or because
// the tenant it belonged to is gone, has its objects written down before it
// goes, and the janitor removes them.
func TestRemovedDataWritesItsObjectsDownFirst(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t, "s1")
	root := t.TempDir()
	a := storedNode(t, clock, database(), largeFiles(root))
	createTenant(t, a.Fleet, "alice")
	touch(t, a.Fleet, "alice")
	writeTenantFile(t, a, "alice", "notes/one.bin", noise(20*vaultPage, "one"))
	writeTenantFile(t, a, "alice", "notes/two.bin", noise(20*vaultPage, "two"))
	checkpoint(t, a.Fleet, "alice")
	sleep(t, clock, a.Fleet)
	if objects(t, root) < 2 {
		t.Fatal("both files are objects")
	}
	row := record(t, a.Fleet, "alice")
	born := millis(row.CreatedAt)
	// Rows of objects the data refers to, as a node that stopped before it
	// cleared them leaves them, and of a tenant that is gone.
	for _, file := range layoutIn(t, a.Fleet, row.Shard, "alice") {
		if file.base != "" {
			if err := a.registry.intend(ctx, leftover{"alice", born, fileAt(file.base)}, clock.Now()); err != nil {
				t.Fatal(err)
			}
		}
	}
	ghost := filepath.Join(root, filepath.FromSlash(fileKey("ghost", born, "0123456789abcdef")))
	if err := os.MkdirAll(filepath.Dir(ghost), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ghost, []byte("left by a deleted tenant"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := a.registry.intend(ctx, leftover{"ghost", born, fileAt("0123456789abcdef")}, clock.Now()); err != nil {
		t.Fatal(err)
	}
	a.collect(ctx)
	if got := stats(t, a.Fleet); got.Leftovers != 0 || exists(ghost) {
		t.Fatalf("the janitor clears the rows of objects in use and removes the deleted tenant's: %+v", got)
	}
	objectsMatch(t, a.Fleet, root, "alice")
	release, err := a.hold(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.vault.take(ctx, a.vault.shards[row.Shard], "alice", a.node, born, true)
	release()
	if err != nil {
		t.Fatal(err)
	}
	if got := stats(t, a.Fleet); stored(t, a.Fleet, row.Shard, "alice") != 1 || got.Leftovers < 2 {
		t.Fatalf("the data is gone and its objects are written down: %+v", got)
	}
	a.collect(ctx)
	if got := stats(t, a.Fleet); got.Leftovers != 0 || objects(t, root) != 0 {
		t.Fatalf("and the janitor removes them: %+v, %d objects", got, objects(t, root))
	}
}

// A removal that stops after the rows went and before their objects did
// leaves the objects written down, and the janitor removes them.
func TestRemovalThatStopsLeavesItsObjectsWrittenDown(t *testing.T) {
	clock := newClock()
	database := shardedDatabase(t, "s1")
	root := t.TempDir()
	a := storedNode(t, clock, database(), largeFiles(root))
	createTenant(t, a.Fleet, "alice")
	touch(t, a.Fleet, "alice")
	writeTenantFile(t, a, "alice", "notes/big.bin", noise(20*vaultPage, "removed"))
	checkpoint(t, a.Fleet, "alice")
	sleep(t, clock, a.Fleet)
	ctx, stop := context.WithCancel(context.Background())
	a.vault.pause = func(at string) {
		if at == "purged" {
			stop()
		}
	}
	_ = a.DeleteTenant(ctx, "alice")
	a.vault.pause = nil
	if objects(t, root) == 0 || stats(t, a.Fleet).Leftovers == 0 {
		t.Fatal("the objects are still there, written down")
	}
	a.collect(context.Background())
	if objects(t, root) != 0 || stats(t, a.Fleet).Leftovers != 0 {
		t.Fatalf("the janitor removes them: %d objects", objects(t, root))
	}
}

// stage stores only an image of the node's own: the file itself may be
// written while it is read, and its pages and checksum are what was compared.
func TestStageStoresOnlyImagesOfItsOwn(t *testing.T) {
	clock := newClock()
	database := shardedDatabase(t, "s1")
	root := t.TempDir()
	a := storedNode(t, clock, database(), largeFiles(root))
	createTenant(t, a.Fleet, "alice")
	touch(t, a.Fleet, "alice")
	content := noise(30*vaultPage, "live")
	writeTenantFile(t, a, "alice", "notes/live.bin", content)
	r := a.opened("alice").replica
	pages, err := allPages(filepath.Join(a.dir("alice"), "notes", "live.bin"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	delta := vaultDelta{files: []vaultFile{{path: "notes/live.bin", size: int64(len(content)), sum: hex.EncodeToString(sum[:]), pages: pages, image: filepath.Join(a.dir("alice"), "notes", "live.bin")}}}
	if err := r.stage(context.Background(), &delta); err != nil {
		t.Fatal(err)
	}
	if delta.files[0].object != "" || len(delta.added) != 0 || objects(t, root) != 0 {
		t.Fatalf("the file itself is not stored as an object: %+v", delta.added)
	}
}

// A node that is held up while it stores an object, and is taken for dead
// meanwhile, loses the checkpoint that would refer to the object, and the
// object goes. The janitor makes itself the tenant's writer before it looks,
// so the object it removes is one no checkpoint can come to refer to; and an
// object stored after the janitor looked is written down again.
func TestLargeFileOfANodeTakenForDeadIsNotLeft(t *testing.T) {
	for _, held := range []struct {
		name, step string
		write      bool
	}{
		{"before the object is stored", "upload", false},
		{"after the object is stored", "put", false},
		{"while another node writes the tenant", "put", true},
	} {
		t.Run(held.name, func(t *testing.T) {
			clock := newClock()
			ctx := context.Background()
			database := shardedDatabase(t, "s1")
			root := t.TempDir()
			a := storedNode(t, clock, database(), largeFiles(root))
			b := storedNode(t, clock, database(), largeFiles(root))
			createTenant(t, a.Fleet, "alice")
			say(t, a.Fleet, "alice", "written at the start")
			checkpoint(t, a.Fleet, "alice")
			written := []string{"written at the start"}
			writeTenantFile(t, a, "alice", "notes/big.bin", noise(20*vaultPage, "held up"))
			a.vault.pause = func(at string) {
				if at != held.step {
					return
				}
				a.vault.pause = nil
				clock.Set(clock.Now().Add(b.config.NodeTTL + time.Second))
				b.heartbeat(ctx)
				if held.write {
					say(t, b.Fleet, "alice", "written while the first node was held up")
					written = append(written, "written while the first node was held up")
					sleep(t, clock, b.Fleet)
				}
				b.collect(ctx)
			}
			if _, err := a.opened("alice").replica.checkpoint(ctx); !errors.Is(err, errFenced) {
				t.Fatalf("the held-up node's checkpoint is refused: %v", err)
			}
			if a.vault.pause != nil {
				t.Fatal("the step was never reached")
			}
			b.collect(ctx)
			if got := stats(t, b.Fleet); got.Leftovers != 0 {
				t.Fatalf("nothing is left written down: %+v", got)
			}
			objectsMatch(t, b.Fleet, root, "alice")
			if _, ok := layoutIn(t, b.Fleet, shardOf(t, b.Fleet, "alice"), "alice")["notes/big.bin"]; ok {
				t.Fatal("the refused checkpoint left no trace in the shard")
			}
			text := channelText(t, b.Fleet, "alice")
			for _, line := range written {
				if !strings.Contains(text, line) {
					t.Fatalf("%q is lost: %q", line, text)
				}
			}
		})
	}
}

// Large files need every running node to read them. While a node of the
// version before them runs, a node that would keep them in the archive store
// does not start; one with large files off does.
func TestLargeFilesWaitForTheNodesBeforeThem(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t, "s1")
	root := t.TempDir()
	a := storedNode(t, clock, database(), nil)
	if _, err := a.registry.db.ExecContext(ctx, `INSERT INTO nodes(id,addr,started,beat,version) VALUES('old','http://old',?,?,2)`, millis(clock.Now()), millis(clock.Now())); err != nil {
		t.Fatal(err)
	}
	open := func(large int64) error {
		config := FleetConfig{DataDir: t.TempDir(), Provider: &ScriptedModel{Now: clock.Now}, Model: ScriptedModelID, Now: clock.Now, Advertise: "http://new"}
		database()(&config)
		largeFiles(root)(&config)
		config.LargeFile = large
		fleet, err := OpenFleet(ctx, config)
		if err == nil {
			err = fleet.Close()
		}
		return err
	}
	if err := open(64 << 10); err == nil || !strings.Contains(err.Error(), "keeps every file in its shard") {
		t.Fatalf("large files wait for the older node: %v", err)
	}
	if err := open(0); err != nil {
		t.Fatalf("with large files off the two versions run side by side: %v", err)
	}
}

// A store that takes requests and never answers holds a checkpoint up for
// storeWait at most, and for at most half the time the checkpoint has: the
// shard is written in the other half and is not taken for down. Then the
// node's checkpoints do not ask the store for storeCooldown, not even to
// remove what they no longer refer to; the janitor removes that once the
// store answers.
func TestLargeFileCheckpointDoesNotWaitForAStoreThatHangs(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t, "s1")
	root := t.TempDir()
	a := storedNode(t, clock, database(), largeFiles(root))
	hanging := &flakyStore{coldStore: a.vault.cold}
	a.vault.cold = hanging
	createTenant(t, a.Fleet, "alice")
	touch(t, a.Fleet, "alice")
	const kept, other, big = "notes/kept.bin", "notes/other.bin", "notes/big.bin"
	writeTenantFile(t, a, "alice", kept, noise(30*vaultPage, "kept"))
	writeTenantFile(t, a, "alice", other, noise(30*vaultPage, "other"))
	checkpoint(t, a.Fleet, "alice")
	shard := shardOf(t, a.Fleet, "alice")
	layout := layoutIn(t, a.Fleet, shard, "alice")
	if layout[kept].base == "" || layout[other].base == "" {
		t.Fatal("the files are objects while the store answers")
	}
	tenant := a.opened("alice")
	gone := func(name string) {
		t.Helper()
		if err := os.Remove(filepath.Join(a.dir("alice"), "notes", name)); err != nil {
			t.Fatal(err)
		}
	}
	hanging.hangs.Store(true)

	// A checkpoint whose context has no deadline, as the node's own have,
	// removes an object it no longer refers to and waits storeWait.
	gone("kept.bin")
	start := time.Now()
	checkpoint(t, a.Fleet, "alice")
	if took := time.Since(start); took > storeWait+time.Second || hanging.unbounded.Load() != 0 || hanging.removes.Load() != 1 {
		t.Fatalf("the removal waited %s, %d without a deadline", took, hanging.unbounded.Load())
	}
	if got := stats(t, a.Fleet); got.Leftovers < 1 || objects(t, root) < 2 {
		t.Fatalf("the object of the removed file stays written down: %+v", got)
	}

	// Once storeCooldown has passed, a checkpoint with two seconds asks the
	// store once and for one second, then removes nothing.
	clock.Set(clock.Now().Add(storeCooldown))
	content := noise(30*vaultPage, "hangs")
	writeTenantFile(t, a, "alice", big, content)
	gone("other.bin")
	limited, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	asked, before := hanging.puts.Load(), stats(t, a.Fleet).LargeFallbacks
	start = time.Now()
	if _, err := tenant.replica.checkpoint(limited); err != nil {
		t.Fatalf("the checkpoint is written without the store: %v", err)
	}
	if took := time.Since(start); took > 1500*time.Millisecond {
		t.Fatalf("the checkpoint waited %s for the store", took)
	}
	if got := layoutIn(t, a.Fleet, shard, "alice")[big]; got.base != "" || pagesIn(t, a.Fleet, shard, "alice", big) != 30 {
		t.Fatalf("the file is stored as pages: %+v", got)
	}
	if got := hanging.puts.Load() - asked; got != 1 || hanging.removes.Load() != 1 {
		t.Fatalf("the store that did not answer is asked once: %d puts, %d removes", got, hanging.removes.Load())
	}
	s := a.vault.shards[shard]
	s.mu.Lock()
	troubles := s.troubles
	s.mu.Unlock()
	if troubles != 0 || shardInfo(t, a.Fleet, shard).Down {
		t.Fatalf("the store's trouble is not counted against the shard: %d", troubles)
	}
	asked = hanging.puts.Load()
	content[0] ^= 1
	writeTenantFile(t, a, "alice", big, content)
	start = time.Now()
	checkpoint(t, a.Fleet, "alice")
	if took := time.Since(start); hanging.puts.Load() != asked || took > time.Second {
		t.Fatalf("the next checkpoint does not ask the store: %d puts, %s", hanging.puts.Load()-asked, took)
	}
	if got := stats(t, a.Fleet).LargeFallbacks - before; got < 2 {
		t.Fatalf("both fallbacks are counted: %d", got)
	}

	sleep(t, clock, a.Fleet)
	cleaning, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	a.collect(cleaning)
	if got := stats(t, a.Fleet); got.Leftovers < 2 || hanging.unbounded.Load() != 0 {
		t.Fatalf("the janitor asks with a deadline and leaves the rows: %+v, %d unbounded", got, hanging.unbounded.Load())
	}
	hanging.hangs.Store(false)
	a.collect(ctx)
	if got := stats(t, a.Fleet); got.Leftovers != 0 {
		t.Fatalf("the janitor removes the objects once the store answers: %+v", got)
	}
	for _, token := range []string{layout[kept].base, layout[other].base} {
		if _, err := os.Stat(filepath.Join(root, fileKey("alice", tenant.replica.born, token))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("the object of a removed file is gone: %v", err)
		}
	}
}

// The same pages changed checkpoint after checkpoint are one set of pages
// over the object, not more each time: the file is not stored anew for them.
func TestLargeFileThatChangesInPlaceIsNotStoredAgain(t *testing.T) {
	clock := newClock()
	database := shardedDatabase(t, "s1")
	root := t.TempDir()
	a := storedNode(t, clock, database(), largeFiles(root))
	createTenant(t, a.Fleet, "alice")
	touch(t, a.Fleet, "alice")
	const big = "notes/big.bin"
	content := noise(20*vaultPage, "in place")
	writeTenantFile(t, a, "alice", big, content)
	checkpoint(t, a.Fleet, "alice")
	first := objectsMatch(t, a.Fleet, root, "alice")[big].base
	for round := range 4 {
		for _, page := range []int{0, 7, 19} {
			content[page*vaultPage] = byte(round + 1)
		}
		writeTenantFile(t, a, "alice", big, content)
		checkpoint(t, a.Fleet, "alice")
	}
	if file := objectsMatch(t, a.Fleet, root, "alice")[big]; first == "" || file.base != first || file.overlay != 3 {
		t.Fatalf("three pages over the first object %s: %+v", first, file)
	}
	// Changing a quarter of it at once stores it anew.
	for page := range 6 {
		content[page*vaultPage+1] ^= 1
	}
	writeTenantFile(t, a, "alice", big, content)
	checkpoint(t, a.Fleet, "alice")
	if file := objectsMatch(t, a.Fleet, root, "alice")[big]; file.base == first || file.overlay != 0 {
		t.Fatalf("a quarter changed at once is a new object: %+v", file)
	}
}
