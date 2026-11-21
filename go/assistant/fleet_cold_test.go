package assistant

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const day = 24 * time.Hour

// archived configures a node with an archive store and the ages after which
// absent owners' tenants stop being woken and are archived.
func archived(store ArchiveStore) func(*FleetConfig) {
	return func(c *FleetConfig) {
		c.IdleTTL, c.DormantAfter, c.ArchiveAfter, c.Archive = time.Second, 3*day, 7*day, store
	}
}

// archiveFile is the file a directory archive store holds for a tenant that
// is archived.
func archiveFile(t *testing.T, root string, fleet *Fleet, id TenantID) string {
	t.Helper()
	row := record(t, fleet, id)
	if row.State != TenantCold || row.archive == "" {
		t.Fatalf("tenant %s is not archived: %+v", id, row)
	}
	return filepath.Join(root, filepath.FromSlash(coldKey(id, millis(row.CreatedAt), row.archive)))
}

// objects counts what a directory archive store holds.
func objects(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			count++
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func both(tunes ...func(*FleetConfig)) func(*FleetConfig) {
	return func(c *FleetConfig) {
		for _, tune := range tunes {
			tune(c)
		}
	}
}

// A tenant whose owner has been away for ArchiveAfter leaves its shard for
// one compressed object in the archive store, and the first node that needs
// it reads it back whole. Deleting an archived tenant removes the object.
func TestAbsentTenantsMoveToTheArchiveStoreAndBack(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t, "s1", "s2")
	root := t.TempDir()
	store := archived(ArchiveStore{URL: "file://" + root})
	a := storedNode(t, clock, both(database(), store), nil)
	b := storedNode(t, clock, both(database(), store), nil)
	alice := createTenant(t, a.Fleet, "alice")
	if code, body := tenantCall(a.handler, http.MethodPost, "/t/alice/api/chat", alice, `{"text":"记住 周三下午不开会"}`); code != http.StatusAccepted {
		t.Fatalf("chat: %d %s", code, body)
	}
	settle(t, touch(t, a.Fleet, "alice").Store)
	sleep(t, clock, a.Fleet)
	row := record(t, a.Fleet, "alice")
	shard := row.Shard

	clock.Set(clock.Now().Add(4 * day))
	a.janitor(ctx)
	if got := record(t, a.Fleet, "alice"); got.State != TenantWarm || got.NextWake != nil || objects(t, root) != 0 {
		t.Fatalf("a dormant tenant stays in its shard: %+v", got)
	}
	clock.Set(clock.Now().Add(4 * day))
	a.janitor(ctx)
	got := stats(t, a.Fleet)
	object := archiveFile(t, root, a.Fleet, "alice")
	if row := record(t, a.Fleet, "alice"); row.Shard != "" || !exists(object) || objects(t, root) != 1 || stored(t, a.Fleet, shard, "alice") != 0 || exists(a.dir("alice")) {
		t.Fatalf("an absent tenant is one object in the archive store and nothing in its shard: %+v", row)
	}
	if got.Frozen != 1 || got.Archives != 1 || got.Leftovers != 0 || shardInfo(t, a.Fleet, shard).Tenants != 0 || got.Tenants[TenantCold] != 1 || !strings.HasPrefix(got.ArchiveStore, "file://") {
		t.Fatalf("stats: %+v", got)
	}
	t.Logf("tenant: %d KB of files in the shard, %d KB archived", got.FrozenBytes/1024, got.ArchiveBytes/1024)
	if got.ArchiveBytes*3 > got.FrozenBytes {
		t.Fatalf("the archive (%d bytes) should be far smaller than the files (%d bytes)", got.ArchiveBytes, got.FrozenBytes)
	}

	// Any node reads it back when its owner returns.
	code, body := tenantCall(b.handler, http.MethodGet, "/t/alice/api/state", alice, "")
	if code != http.StatusOK || !strings.Contains(body, "周三下午不开会") {
		t.Fatalf("the owner's next request brings the tenant back with its history: %d", code)
	}
	row = record(t, b.Fleet, "alice")
	if row.State != TenantWarm || row.Shard == "" || exists(object) || stored(t, b.Fleet, row.Shard, "alice") < 3 {
		t.Fatalf("it is warm in a shard again and the object is gone: %+v", row)
	}
	if got := stats(t, b.Fleet); got.Thawed != 1 || got.Leftovers != 0 || shardInfo(t, b.Fleet, row.Shard).Tenants != 1 {
		t.Fatalf("stats: %+v", got)
	}
	say(t, b.Fleet, "alice", "written after coming back")
	sleep(t, clock, b.Fleet)

	// Archived again, as an object of its own, and deleted while it is
	// archived.
	clock.Set(clock.Now().Add(8 * day))
	b.janitor(ctx)
	again := archiveFile(t, root, b.Fleet, "alice")
	if again == object || !exists(again) || objects(t, root) != 1 {
		t.Fatalf("archived again under a name of its own: %s", again)
	}
	if err := a.DeleteTenant(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if objects(t, root) != 0 || stats(t, a.Fleet).Leftovers != 0 {
		t.Fatal("a deleted tenant's archive goes with it")
	}
}

// A tenant that is due to run soon is not archived only to be read back.
func TestTenantDueSoonIsNotArchived(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t)
	fleet := storedNode(t, clock, both(database(), func(c *FleetConfig) {
		c.IdleTTL, c.ArchiveAfter, c.Archive = time.Second, 7*day, ArchiveStore{URL: "file://" + t.TempDir()}
	}), nil)
	createTenant(t, fleet.Fleet, "alice")
	say(t, fleet.Fleet, "alice", "has a routine every morning")
	sleep(t, clock, fleet.Fleet)
	clock.Set(clock.Now().Add(8 * day))
	if err := fleet.registry.postpone(ctx, "alice", clock.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	fleet.janitor(ctx)
	if got := record(t, fleet.Fleet, "alice"); got.State != TenantWarm {
		t.Fatalf("a tenant due within a day stays in its shard: %+v", got)
	}
	if err := fleet.registry.postpone(ctx, "alice", clock.Now().Add(3*day)); err != nil {
		t.Fatal(err)
	}
	fleet.janitor(ctx)
	if got := record(t, fleet.Fleet, "alice"); got.State != TenantCold {
		t.Fatalf("one that is not due for days is archived: %+v", got)
	}
}

// trippingStore runs a hook after an archive has been read.
type trippingStore struct {
	coldStore
	afterGet func()
}

func (s *trippingStore) get(ctx context.Context, key string, into io.Writer) error {
	err := s.coldStore.get(ctx, key, into)
	if err == nil && s.afterGet != nil {
		s.afterGet()
	}
	return err
}

// freeze archives a sleeping tenant now.
func freeze(t *testing.T, fleet *Fleet, id TenantID) {
	t.Helper()
	ctx := context.Background()
	release, err := fleet.hold(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := fleet.vault.freeze(ctx, record(t, fleet, id), fleet.node); err != nil {
		t.Fatal(err)
	}
}

// While the registry calls a tenant cold its archive is its data. A reading
// back that stops before the registry knows of it is simply done again; a
// tenant whose archive is missing does not open as an empty one; and what an
// earlier stay left in a shard is never taken for the tenant's data.
func TestArchiveIsTheTruthWhileATenantIsCold(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t, "s1", "s2")
	root := t.TempDir()
	tune := both(database(), func(c *FleetConfig) { c.IdleTTL, c.Archive = time.Second, ArchiveStore{URL: "file://" + root} })
	fleet := storedNode(t, clock, tune, nil)
	createTenant(t, fleet.Fleet, "alice")
	say(t, fleet.Fleet, "alice", "the first version")
	draft := filepath.Join(fleet.dir("alice"), "notes", "draft.txt")
	if err := errors.Join(os.MkdirAll(filepath.Dir(draft), 0o700), os.WriteFile(draft, []byte("only in the first version\n"), 0o600)); err != nil {
		t.Fatal(err)
	}
	sleep(t, clock, fleet.Fleet)

	// The tenant moves from s1 to s2, and the move stops before the old
	// copy is removed.
	row := record(t, fleet.Fleet, "alice")
	born := millis(row.CreatedAt)
	if row.Shard != "s1" {
		t.Fatalf("the first tenant is in the first shard: %+v", row)
	}
	source, _ := fleet.vault.shards["s1"].use(ctx)
	target, _ := fleet.vault.shards["s2"].use(ctx)
	head, err := take(ctx, source, "alice", fleet.node, born, false, clock.Now())
	if err == nil {
		_, err = take(ctx, target, "alice", fleet.node, born, true, clock.Now())
	}
	if err == nil {
		err = copyTenant(ctx, source, target, "alice", fleet.node, head, clock.Now())
	}
	if err != nil {
		t.Fatal(err)
	}
	held, err := fleet.hold(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	moved, err := fleet.registry.moved(ctx, "alice", born, fleet.node, "s1", "s2", clock.Now())
	held()
	if err != nil || !moved {
		t.Fatal(moved, err)
	}
	say(t, fleet.Fleet, "alice", "the second version")
	if err := os.Remove(draft); err != nil {
		t.Fatal(err)
	}
	sleep(t, clock, fleet.Fleet)
	freeze(t, fleet.Fleet, "alice")
	if stored(t, fleet.Fleet, "s1", "alice") < 3 || stored(t, fleet.Fleet, "s2", "alice") != 0 || stats(t, fleet.Fleet).Leftovers != 1 {
		t.Fatal("the old copy is still in the first shard, written down to remove")
	}
	// The first shard is the only open one, so the tenant comes back to
	// where its stale copy is.
	if err := fleet.SetShard(ctx, "s2", ShardClosed); err != nil {
		t.Fatal(err)
	}

	// Reading back stops after the shard has the data and before the
	// registry is told.
	inner := fleet.vault.cold
	fleet.vault.cold = &trippingStore{coldStore: inner, afterGet: func() { fleet.registry.db.down.Store(true) }}
	err = fleet.Use(ctx, "alice", func(*App) error { return nil })
	fleet.registry.db.down.Store(false)
	fleet.vault.cold = inner
	if err == nil || fleet.opened("alice") != nil {
		t.Fatalf("a tenant whose reading back did not finish is not open: %v", err)
	}
	if got := record(t, fleet.Fleet, "alice"); got.State != TenantCold || got.Shard != "s1" {
		t.Fatalf("it is still cold: %+v", got)
	}
	text := channelText(t, fleet.Fleet, "alice")
	if !strings.Contains(text, "the second version") || !strings.Contains(text, "the first version") {
		t.Fatalf("the next open reads the archive again, not the shard's stale copy: %q", text)
	}
	if got := stats(t, fleet.Fleet); got.Thawed != 1 || got.Leftovers != 0 || record(t, fleet.Fleet, "alice").State != TenantWarm {
		t.Fatalf("and then it is warm, with nothing left to remove: %+v", got)
	}
	// What the stale copy had and the tenant no longer has is gone from the
	// shard too: a node that reads the tenant afresh does not get it back.
	sleep(t, clock, fleet.Fleet)
	if err := os.RemoveAll(fleet.dir("alice")); err != nil {
		t.Fatal(err)
	}
	touch(t, fleet.Fleet, "alice")
	if exists(draft) {
		t.Fatal("a file of the stale copy came back")
	}
	// A row that names the archive of a tenant that is archived is stale:
	// the archive is the tenant's data.
	sleep(t, clock, fleet.Fleet)
	freeze(t, fleet.Fleet, "alice")
	object := archiveFile(t, root, fleet.Fleet, "alice")
	if err := fleet.registry.intend(ctx, leftover{"alice", born, archiveAt(record(t, fleet.Fleet, "alice").archive)}, clock.Now()); err != nil {
		t.Fatal(err)
	}
	fleet.collect(ctx)
	if !exists(object) || stats(t, fleet.Fleet).Leftovers != 0 {
		t.Fatal("an archived tenant's archive is never removed as a leftover")
	}
	channelText(t, fleet.Fleet, "alice")

	// An archive that is gone is an error, not an empty tenant.
	sleep(t, clock, fleet.Fleet)
	freeze(t, fleet.Fleet, "alice")
	object = archiveFile(t, root, fleet.Fleet, "alice")
	if err := os.Rename(object, object+".away"); err != nil {
		t.Fatal(err)
	}
	if err := fleet.Use(ctx, "alice", func(*App) error { return nil }); !errors.Is(err, errNoArchive) {
		t.Fatalf("a missing archive: %v", err)
	}
	if got := record(t, fleet.Fleet, "alice"); got.State != TenantCold || holder(t, fleet.Fleet, "alice") != "" {
		t.Fatalf("the tenant stays cold and unheld: %+v", got)
	}
	if err := os.Rename(object+".away", object); err != nil {
		t.Fatal(err)
	}
	if text := channelText(t, fleet.Fleet, "alice"); !strings.Contains(text, "the second version") {
		t.Fatalf("with its archive back it opens: %q", text)
	}
}

// A node that is held up in the middle of moving, archiving, reading back or
// cleaning up a tenant for longer than the others wait has lost the tenant
// without knowing it. Whatever it does when it goes on changes nothing: the
// registry takes no word on where the tenant is from a node that does not
// hold it, and a shard lets a node remove only a copy no node has taken
// since.
func TestNodeTakenForDeadChangesNothingWhenItGoesOn(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	root := t.TempDir()
	database := shardedDatabase(t, "s1", "s2")
	tune := both(database(), func(c *FleetConfig) { c.IdleTTL, c.Archive = time.Second, ArchiveStore{URL: "file://" + root} })
	a := storedNode(t, clock, tune, nil)
	b := storedNode(t, clock, tune, nil)
	other := map[string]string{"s1": "s2", "s2": "s1"}
	createTenant(t, a.Fleet, "alice")
	say(t, a.Fleet, "alice", "written at the start")
	sleep(t, clock, a.Fleet)
	written := []string{"written at the start"}

	// heldUp stops the first node at one step. The others then take it for
	// dead, and the second node does to the tenant what meanwhile says.
	heldUp := func(step string, meanwhile func()) {
		a.vault.pause = func(at string) {
			if at != step {
				return
			}
			a.vault.pause = nil
			clock.Set(clock.Now().Add(b.config.NodeTTL + time.Second))
			b.heartbeat(ctx)
			meanwhile()
		}
	}
	write := func(text string) func() {
		return func() {
			say(t, b.Fleet, "alice", text)
			written = append(written, text)
			sleep(t, clock, b.Fleet)
		}
	}
	// whole checks that the tenant has everything that was written, read
	// from where the registry says it is and not from a node's copy, and
	// that no copy is left anywhere else.
	whole := func(what string) {
		t.Helper()
		if a.vault.pause != nil {
			t.Fatalf("%s: the step was never reached", what)
		}
		a.heartbeat(ctx)
		b.heartbeat(ctx)
		b.collect(ctx)
		if err := os.RemoveAll(a.dir("alice")); err != nil {
			t.Fatal(err)
		}
		text := channelText(t, a.Fleet, "alice")
		for _, line := range written {
			if !strings.Contains(text, line) {
				t.Fatalf("%s: %q is lost: %q", what, line, text)
			}
		}
		sleep(t, clock, a.Fleet)
		row := record(t, a.Fleet, "alice")
		if got := stats(t, a.Fleet); got.Leftovers != 0 || stored(t, a.Fleet, other[row.Shard], "alice") != 0 || objects(t, root) != 0 {
			t.Fatalf("%s: copies are left: %+v, %d objects", what, got, objects(t, root))
		}
	}
	archive := func() error {
		release, err := a.hold(ctx, "alice")
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		return a.vault.freeze(ctx, record(t, a.Fleet, "alice"), a.node)
	}

	// Held up after the copy, before the registry is told: the second node
	// writes the tenant where it still is.
	from := shardOf(t, a.Fleet, "alice")
	heldUp("move", write("written while the first node was moving it"))
	if err := a.relocate(ctx, "alice", from); err == nil || shardOf(t, a.Fleet, "alice") != from {
		t.Fatalf("a move by a node that lost the tenant is refused: %v", err)
	}
	whole("a move the registry was not told of")

	// Held up after the registry was told, before the old copy goes: the
	// second node moves the tenant back to where the old copy is.
	from = shardOf(t, a.Fleet, "alice")
	heldUp("moved", func() {
		if err := b.relocate(ctx, "alice", other[from]); err != nil {
			t.Fatal(err)
		}
	})
	if err := a.relocate(ctx, "alice", from); !errors.Is(err, errFenced) || shardOf(t, a.Fleet, "alice") != from {
		t.Fatalf("a copy another node has taken since is not removed: %v", err)
	}
	whole("a move whose old copy became the tenant's data again")

	// Held up after the archive was stored, before the registry is told.
	heldUp("freeze", write("written while the first node was archiving it"))
	if err := archive(); !errors.Is(err, errFenced) || record(t, a.Fleet, "alice").State != TenantWarm {
		t.Fatalf("an archiving by a node that lost the tenant is refused: %v", err)
	}
	if objects(t, root) != 1 || stats(t, a.Fleet).Leftovers != 1 {
		t.Fatal("the object it stored is written down to remove")
	}
	whole("an archiving the registry was not told of")

	// Held up before the archive is stored, and the second node meanwhile
	// removes what was written down: the object stored after that is written
	// down again.
	heldUp("store", func() {
		write("written before the first node stored its archive")()
		b.collect(ctx)
		if stats(t, b.Fleet).Leftovers != 0 {
			t.Fatal("the row of an object that is not there yet is removed")
		}
	})
	if err := archive(); !errors.Is(err, errFenced) || objects(t, root) != 1 || stats(t, a.Fleet).Leftovers != 1 {
		t.Fatalf("an object stored after its row was removed is written down again: %v", err)
	}
	whole("an archiving whose object came late")

	// The registry is lost between storing the archive and telling it: the
	// object was written down before it was stored.
	a.vault.pause = func(at string) {
		if at == "freeze" {
			a.vault.pause = nil
			a.registry.db.down.Store(true)
		}
	}
	err := archive()
	a.registry.db.down.Store(false)
	if !errors.Is(err, errUnreachable) || objects(t, root) != 1 || stats(t, a.Fleet).Leftovers != 1 || record(t, a.Fleet, "alice").State != TenantWarm {
		t.Fatalf("an archive the registry never heard of is written down to remove: %v", err)
	}
	a.collect(ctx)
	whole("an archiving that lost the registry")

	// Held up after the registry was told, before the shard's copy goes: the
	// second node reads the tenant back into the same shard and writes it.
	from = shardOf(t, a.Fleet, "alice")
	if err := a.SetShard(ctx, other[from], ShardClosed); err != nil {
		t.Fatal(err)
	}
	heldUp("frozen", write("written after the first node archived it"))
	if err := archive(); !errors.Is(err, errFenced) || shardOf(t, a.Fleet, "alice") != from {
		t.Fatalf("a copy another node has taken since is not removed: %v", err)
	}
	whole("an archiving whose shard became the tenant's again")
	if err := a.SetShard(ctx, other[from], ShardOpen); err != nil {
		t.Fatal(err)
	}

	// Held up while reading an archive back, before the registry is told:
	// the second node reads it back itself and writes the tenant.
	freeze(t, b.Fleet, "alice")
	heldUp("thaw", write("written while the first node was reading it back"))
	if err := a.Use(ctx, "alice", func(*App) error { return nil }); !errors.Is(err, errFenced) || a.opened("alice") != nil {
		t.Fatalf("a reading back by a node that lost the tenant is refused: %v", err)
	}
	whole("a reading back the registry was not told of")

	// Held up while removing a leftover: the copy of a move that never
	// finished becomes the tenant's data when the second node moves it there.
	from = shardOf(t, a.Fleet, "alice")
	heldUp("move", func() {})
	if err := a.relocate(ctx, "alice", from); err == nil || stats(t, a.Fleet).Leftovers != 1 {
		t.Fatalf("a move that never finished leaves its copy written down: %v", err)
	}
	a.heartbeat(ctx)
	heldUp("clean", func() {
		if err := b.relocate(ctx, "alice", from); err != nil {
			t.Fatal(err)
		}
	})
	a.collect(ctx)
	if shardOf(t, a.Fleet, "alice") != other[from] || stored(t, a.Fleet, other[from], "alice") < 3 {
		t.Fatal("a leftover another node has made the tenant's data is not removed")
	}
	whole("a removal of a copy that became the tenant's data")

	// The same, held up one step earlier: the second node has moved the
	// tenant there before the first makes itself the copy's writer.
	from = shardOf(t, a.Fleet, "alice")
	heldUp("move", func() {})
	if err := a.relocate(ctx, "alice", from); err == nil || stats(t, a.Fleet).Leftovers != 1 {
		t.Fatalf("a move that never finished leaves its copy written down: %v", err)
	}
	a.heartbeat(ctx)
	heldUp("seize", func() {
		if err := b.relocate(ctx, "alice", from); err != nil {
			t.Fatal(err)
		}
	})
	a.collect(ctx)
	if shardOf(t, a.Fleet, "alice") != other[from] || stored(t, a.Fleet, other[from], "alice") < 3 {
		t.Fatal("a node looks again where the tenant is once it is the copy's writer")
	}
	whole("a removal of a copy that had become the tenant's data")

	// And the registry's statements each refuse a node that does not hold
	// the tenant, whatever else is true.
	row := record(t, a.Fleet, "alice")
	born, now := millis(row.CreatedAt), clock.Now()
	if moved, err := a.registry.moved(ctx, "alice", born, a.node, row.Shard, other[row.Shard], now); err != nil || moved {
		t.Fatalf("moved without the placement: %t %v", moved, err)
	}
	if frozen, err := a.registry.frozen(ctx, "alice", born, a.node, row.Shard, "0123456789abcdef", now); err != nil || frozen {
		t.Fatalf("archived without the placement: %t %v", frozen, err)
	}
	// Nor is a tenant some node has open called archived by the node that
	// holds it.
	release, err := a.hold(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	believed := map[int]bool{}
	for _, active := range []int{1, 0} {
		if _, err = a.registry.db.ExecContext(ctx, `UPDATE tenants SET active=? WHERE id=?`, active, "alice"); err != nil {
			break
		}
		if believed[active], err = a.registry.frozen(ctx, "alice", born, a.node, row.Shard, "0123456789abcdef", now); err != nil {
			break
		}
	}
	release()
	if err != nil || believed[1] || !believed[0] {
		t.Fatalf("archived while open: %v, while asleep: %v, %v", believed[1], believed[0], err)
	}
	if got := record(t, a.Fleet, "alice"); got.State != TenantCold || got.archive != "0123456789abcdef" {
		t.Fatalf("the holder of a sleeping tenant is believed: %+v", got)
	}
	if thawed, err := a.registry.thawed(ctx, "alice", born, a.node, "0123456789abcdef", now); err != nil || thawed {
		t.Fatalf("read back without the placement: %t %v", thawed, err)
	}
}

// A node's copies of tenants are a cache with a size: when they take more
// than CacheBytes the ones opened longest ago go, never one that is open.
func TestVaultNodeKeepsItsCopiesWithinABudget(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t)
	fleet := storedNode(t, clock, both(database(), brief), nil)
	ids := []TenantID{"alice", "bob", "carol"}
	sizes := map[TenantID]int64{}
	var total int64
	for i, id := range ids {
		createTenant(t, fleet.Fleet, id)
		say(t, fleet.Fleet, id, "kept in the vault for "+string(id))
		sleep(t, clock, fleet.Fleet)
		head, ok := readHead(fleet.dir(id))
		if !ok || head.Bytes == 0 {
			t.Fatalf("a closed copy records its size: %+v", head)
		}
		sizes[id] = head.Bytes
		total += head.Bytes
		at := time.Now().Add(time.Duration(i-len(ids)) * time.Hour)
		if err := os.Chtimes(headPath(fleet.dir(id)), at, at); err != nil {
			t.Fatal(err)
		}
	}
	fleet.config.CacheBytes = total - 1
	fleet.janitor(ctx)
	if exists(fleet.dir("alice")) || !exists(fleet.dir("bob")) || !exists(fleet.dir("carol")) {
		t.Fatal("the copy opened longest ago goes first, and only as many as needed")
	}
	if got := stats(t, fleet.Fleet); got.CacheDrops != 1 || got.CacheBytes != total-sizes["alice"] {
		t.Fatalf("stats: %+v", got)
	}
	touch(t, fleet.Fleet, "bob")
	fleet.config.CacheBytes = 1
	fleet.janitor(ctx)
	if !exists(fleet.dir("bob")) || exists(fleet.dir("carol")) {
		t.Fatal("an open tenant's copy stays whatever the budget")
	}
	if text := channelText(t, fleet.Fleet, "alice"); !strings.Contains(text, "kept in the vault for alice") {
		t.Fatalf("a dropped copy is read from the vault again: %q", text)
	}
}

// ---- S3 ----

// The signature of the example request in Amazon's Signature Version 4
// documentation for S3 ("GET Object", with a Range header). The key pair is
// the documentation's own example and opens nothing.
func TestS3SignatureMatchesTheDocumentedExample(t *testing.T) {
	store := &s3Store{region: "us-east-1", access: "AKIAIOSFODNN7EXAMPLE", secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		now: func() time.Time { return time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC) }}
	request, err := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Range", "bytes=0-9")
	store.sign(request, emptySHA256)
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := request.Header.Get("Authorization"); got != want {
		t.Fatalf("signature:\n got %s\nwant %s", got, want)
	}
}

// bucket is a server that speaks the three S3 requests the archive store
// uses and refuses any whose signature it cannot reproduce from the request
// as it arrived, or whose body is not the one that was signed.
type bucket struct {
	mu      sync.Mutex
	objects map[string][]byte
	access  string
	secret  string
	region  string
	refused []string
}

func (b *bucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	refuse := func(why string) {
		b.refused = append(b.refused, r.Method+" "+r.URL.Path+": "+why)
		// As S3 does, the answer names the key the request was signed with.
		http.Error(w, "<Error><Code>SignatureDoesNotMatch</Code><AWSAccessKeyId>"+b.access+"</AWSAccessKeyId></Error>", http.StatusForbidden)
	}
	digest := sha256.Sum256(body)
	if r.Header.Get("X-Amz-Content-Sha256") != hex.EncodeToString(digest[:]) {
		refuse("the body is not the one that was signed")
		return
	}
	authorization := r.Header.Get("Authorization")
	fields := map[string]string{}
	for _, field := range strings.Split(strings.TrimPrefix(authorization, "AWS4-HMAC-SHA256 "), ", ") {
		name, value, _ := strings.Cut(field, "=")
		fields[name] = value
	}
	stamp := r.Header.Get("X-Amz-Date")
	if len(stamp) < 8 || fields["Credential"] != b.access+"/"+stamp[:8]+"/"+b.region+"/s3/aws4_request" {
		refuse("wrong credential scope")
		return
	}
	names := strings.Split(fields["SignedHeaders"], ";")
	if !sort.StringsAreSorted(names) || !strings.Contains(fields["SignedHeaders"], "host") {
		refuse("signed headers")
		return
	}
	canonical := r.Method + "\n" + r.URL.EscapedPath() + "\n" + r.URL.RawQuery + "\n"
	for _, name := range names {
		value := r.Header.Get(name)
		if name == "host" {
			value = r.Host
		}
		canonical += name + ":" + value + "\n"
	}
	canonical += "\n" + fields["SignedHeaders"] + "\n" + r.Header.Get("X-Amz-Content-Sha256")
	mac := func(key []byte, message string) []byte {
		h := hmac.New(sha256.New, key)
		h.Write([]byte(message))
		return h.Sum(nil)
	}
	hashed := sha256.Sum256([]byte(canonical))
	scope := stamp[:8] + "/" + b.region + "/s3/aws4_request"
	key := mac(mac(mac(mac([]byte("AWS4"+b.secret), stamp[:8]), b.region), "s3"), "aws4_request")
	if fields["Signature"] != hex.EncodeToString(mac(key, "AWS4-HMAC-SHA256\n"+stamp+"\n"+scope+"\n"+hex.EncodeToString(hashed[:]))) {
		refuse("signature")
		return
	}
	switch r.Method {
	case http.MethodPut:
		b.objects[r.URL.Path] = body
	case http.MethodGet:
		object, ok := b.objects[r.URL.Path]
		if !ok {
			http.Error(w, "<Error><Code>NoSuchKey</Code></Error>", http.StatusNotFound)
			return
		}
		_, _ = w.Write(object)
	case http.MethodDelete:
		delete(b.objects, r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// Tenants are archived to and read back from an S3 bucket with signed
// requests, and a wrong key is an error that leaves the tenant in its shard.
func TestArchiveStoreSpeaksS3(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	objects := &bucket{objects: map[string][]byte{}, access: "test-access-key", secret: "test-secret-key", region: "eu-test-1"}
	server := httptest.NewServer(objects)
	defer server.Close()
	database := shardedDatabase(t)
	s3 := ArchiveStore{URL: server.URL + "/fleet-bucket/archive/", Region: "eu-test-1", AccessKeyID: "test-access-key", SecretAccessKey: "test-secret-key"}
	fleet := storedNode(t, clock, both(database(), func(c *FleetConfig) { c.IdleTTL, c.Archive = time.Second, s3 }), nil)
	createTenant(t, fleet.Fleet, "alice")
	say(t, fleet.Fleet, "alice", "kept in a bucket")
	sleep(t, clock, fleet.Fleet)
	freeze(t, fleet.Fleet, "alice")
	row := record(t, fleet.Fleet, "alice")
	path := "/fleet-bucket/archive/" + coldKey("alice", millis(row.CreatedAt), row.archive)
	if object := objects.objects[path]; len(object) < 100 || !bytes.HasPrefix(object, []byte{0x1f, 0x8b}) || len(objects.refused) != 0 {
		t.Fatalf("the archive is a gzip object under the prefix: %d bytes, refused %v", len(object), objects.refused)
	}
	if text := channelText(t, fleet.Fleet, "alice"); !strings.Contains(text, "kept in a bucket") {
		t.Fatalf("it is read back from the bucket: %q", text)
	}
	if len(objects.objects) != 0 || len(objects.refused) != 0 {
		t.Fatalf("and the object is deleted: %d objects, refused %v", len(objects.objects), objects.refused)
	}

	// A wrong key: the bucket refuses, and the tenant stays where it is.
	sleep(t, clock, fleet.Fleet)
	fleet.vault.cold.(*s3Store).secret = "not-the-secret"
	release, err := fleet.hold(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	err = fleet.vault.freeze(ctx, record(t, fleet.Fleet, "alice"), fleet.node)
	release()
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "SignatureDoesNotMatch") || strings.Contains(err.Error(), "not-the-secret") || strings.Contains(err.Error(), "test-access-key") {
		t.Fatalf("a refused request says what the store said, without the keys: %v", err)
	}
	if got := record(t, fleet.Fleet, "alice"); got.State != TenantWarm || got.Shard == "" || stored(t, fleet.Fleet, got.Shard, "alice") < 3 {
		t.Fatalf("a tenant that could not be archived stays warm: %+v", got)
	}

	for _, bad := range []ArchiveStore{
		{URL: "ftp://host/bucket"}, {URL: "https://host/bucket"}, {URL: "https://user:pass@host/bucket", AccessKeyID: "a", SecretAccessKey: "b"},
		{URL: "https://host/bucket?x=1", AccessKeyID: "a", SecretAccessKey: "b"}, {URL: "https://host/a b", AccessKeyID: "a", SecretAccessKey: "b"}, {URL: "file://relative/path"},
	} {
		if store, err := openColdStore(bad); err == nil || store != nil {
			t.Fatalf("%s is refused", bad.URL)
		}
	}
	if store, err := openColdStore(ArchiveStore{URL: "s3://my-bucket/fleet", Region: "ap-east-1", AccessKeyID: "a", SecretAccessKey: "b"}); err != nil || store.String() != "https://my-bucket.s3.ap-east-1.amazonaws.com/fleet" {
		t.Fatalf("an s3 URL names the bucket's own host: %v %v", store, err)
	}
}

// The same three requests against a real S3-compatible server, when the
// environment names one: MINILOOP_TEST_S3_URL (the bucket's base URL), and
// its keys in MINILOOP_TEST_S3_ACCESS_KEY_ID and
// MINILOOP_TEST_S3_SECRET_ACCESS_KEY.
func TestArchiveStoreAgainstARealBucket(t *testing.T) {
	address := os.Getenv("MINILOOP_TEST_S3_URL")
	if address == "" {
		t.Skip("MINILOOP_TEST_S3_URL is not set")
	}
	ctx := context.Background()
	store, err := openColdStore(ArchiveStore{URL: address, Region: os.Getenv("MINILOOP_TEST_S3_REGION"), AccessKeyID: os.Getenv("MINILOOP_TEST_S3_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("MINILOOP_TEST_S3_SECRET_ACCESS_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	key := coldKey(TenantID("probe-"+randomHex(4)), time.Now().UnixMilli(), randomHex(8))
	body := bytes.Repeat([]byte("mini-loop archive probe\n"), 4096)
	if err := store.put(ctx, key, bytes.NewReader(body), int64(len(body)), sha256.Sum256(body)); err != nil {
		t.Fatalf("put: %v", err)
	}
	var back bytes.Buffer
	if err := store.get(ctx, key, &back); err != nil || !bytes.Equal(back.Bytes(), body) {
		t.Fatalf("get: %d bytes, %v", back.Len(), err)
	}
	if err := store.remove(ctx, key); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := store.get(ctx, key, io.Discard); !errors.Is(err, errNoArchive) {
		t.Fatalf("a removed object is missing: %v", err)
	}
	if err := store.remove(ctx, key); err != nil {
		t.Fatalf("removing what is not there is not an error: %v", err)
	}
	wrong := *store.(*s3Store)
	wrong.secret = "not-the-secret"
	if err := wrong.put(ctx, key, bytes.NewReader(body), int64(len(body)), sha256.Sum256(body)); err == nil {
		t.Fatal("a wrong key is refused")
	}
}
