package assistant

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
)

// postgresEnv names the environment variable that holds the URL of a
// PostgreSQL database for the tests. Unset, they use a SQLite file with the
// tenants kept in it the same way.
const postgresEnv = "MINILOOP_TEST_POSTGRES"

// sharedDatabase gives one test's fleets a database of their own with the
// tenants kept in it: a schema of the PostgreSQL database when postgresEnv is
// set, a SQLite file otherwise. The nodes keep separate data directories.
func sharedDatabase(t *testing.T) func(*FleetConfig) {
	t.Helper()
	address := os.Getenv(postgresEnv)
	if address == "" {
		dir := t.TempDir()
		return func(c *FleetConfig) { c.registryDir, c.offload, c.Checkpoint = dir, true, time.Hour }
	}
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatalf("%s is not a URL", postgresEnv)
	}
	schema := "t_" + randomHex(8)
	admin, err := sql.Open("pgx", address)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return func(c *FleetConfig) { c.Database, c.Checkpoint = parsed.String(), time.Hour }
}

// storedNode opens a cluster node with a data directory of its own.
func storedNode(t *testing.T, clock *fixedClock, database, tune func(*FleetConfig)) *testNode {
	t.Helper()
	return openNode(t, clock, t.TempDir(), func(c *FleetConfig) {
		database(c)
		if tune != nil {
			tune(c)
		}
	})
}

// held counts the tenants a fleet has open, opening or closing.
func (f *Fleet) held() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tenants)
}

func checkpoint(t *testing.T, fleet *Fleet, id TenantID) bool {
	t.Helper()
	open := fleet.opened(id)
	if open == nil || open.replica == nil {
		t.Fatalf("%s is not open with a vault", id)
	}
	written, err := open.replica.checkpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return written
}

func channelText(t *testing.T, fleet *Fleet, id TenantID) string {
	t.Helper()
	var text strings.Builder
	err := fleet.Use(context.Background(), id, func(app *App) error {
		messages, err := app.Store.Channel(context.Background(), 200)
		for _, message := range messages {
			text.WriteString(message.Text + "\n")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return text.String()
}

func say(t *testing.T, fleet *Fleet, id TenantID, text string) {
	t.Helper()
	err := fleet.Use(context.Background(), id, func(app *App) error {
		_, err := app.Store.Post(context.Background(), ChannelMessage{Role: RoleSystem, Text: text}, app.now())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Nodes that share only the database serve the same tenants: what one wrote
// before the tenant slept, another reads from the vault, and a node that
// opens a tenant it closed itself reuses its own copy.
func TestVaultLetsNodesWithTheirOwnDisksShareTenants(t *testing.T) {
	clock := newClock()
	database := sharedDatabase(t)
	brief := func(c *FleetConfig) { c.IdleTTL = time.Second }
	a := storedNode(t, clock, database, brief)
	b := storedNode(t, clock, database, brief)
	alice := createTenant(t, b.Fleet, "alice")

	if code, body := tenantCall(a.handler, http.MethodPost, "/t/alice/api/chat", alice, `{"text":"记住 周三下午不开会"}`); code != http.StatusAccepted {
		t.Fatalf("chat: %d %s", code, body)
	}
	settle(t, touch(t, a.Fleet, "alice").Store)
	if code, body := tenantCall(b.handler, http.MethodGet, "/t/alice/api/state", alice, ""); code != http.StatusOK || !strings.Contains(body, "周三下午不开会") {
		t.Fatalf("the other node passes a request on to the holder: %d", code)
	}
	clock.Set(clock.Now().Add(2 * time.Second))
	a.reap(context.Background())
	if a.opened("alice") != nil || exists(b.dir("alice")) {
		t.Fatal("the tenant sleeps, and the second node has no copy of it yet")
	}
	if head, ok := readHead(a.dir("alice")); !ok || !head.Clean || head.Seq == 0 {
		t.Fatalf("a clean close marks the node's copy: %+v", head)
	}

	if text := channelText(t, b.Fleet, "alice"); !strings.Contains(text, "周三下午不开会") {
		t.Fatalf("the second node reads the tenant from the vault: %q", text)
	}
	if got := stats(t, b.Fleet); !got.Vault || got.VaultPulls != 1 || got.VaultPulledBytes == 0 || got.CacheReuses != 0 {
		t.Fatalf("it was read once: %+v", got)
	}
	say(t, b.Fleet, "alice", "written on the second node")
	clock.Set(clock.Now().Add(2 * time.Second))
	b.reap(context.Background())
	if b.opened("alice") != nil {
		t.Fatal("the tenant sleeps again")
	}

	// The first node's copy is one checkpoint behind: it reads the vault.
	if text := channelText(t, a.Fleet, "alice"); !strings.Contains(text, "written on the second node") {
		t.Fatalf("the first node sees what the second wrote: %q", text)
	}
	if got := stats(t, a.Fleet); got.VaultPulls != 1 {
		t.Fatalf("a stale copy is replaced: %+v", got)
	}
	clock.Set(clock.Now().Add(2 * time.Second))
	a.reap(context.Background())
	touch(t, a.Fleet, "alice")
	if got := stats(t, a.Fleet); got.VaultPulls != 1 || got.CacheReuses != 1 {
		t.Fatalf("a node reuses the copy it closed itself: %+v", got)
	}
}

// A checkpoint writes only the pages that changed, and nothing when nothing
// did.
func TestVaultStoresOnlyChangedPages(t *testing.T) {
	clock := newClock()
	fleet := openFleet(t, clock, sharedDatabase(t))
	createTenant(t, fleet, "alice")
	touch(t, fleet, "alice")
	if !checkpoint(t, fleet, "alice") {
		t.Fatal("the first checkpoint stores the tenant")
	}
	// The loops that started with the tenant may still be writing.
	deadline := time.Now().Add(10 * time.Second)
	for quiet := 0; quiet < 3; {
		if time.Now().After(deadline) {
			t.Fatal("the tenant never came to rest")
		}
		time.Sleep(20 * time.Millisecond)
		quiet++
		if checkpoint(t, fleet, "alice") {
			quiet = 0
		}
	}
	first := stats(t, fleet)
	if first.CheckpointPages < 50 {
		t.Fatalf("the first checkpoints hold every page: %+v", first)
	}
	say(t, fleet, "alice", "one short line")
	if !checkpoint(t, fleet, "alice") {
		t.Fatal("a change is stored")
	}
	second := stats(t, fleet)
	if pages := second.CheckpointPages - first.CheckpointPages; second.Checkpoints != first.Checkpoints+1 || pages == 0 || pages > 8 {
		t.Fatalf("one line costs a few pages, not the tenant: %d pages", pages)
	}
	if checkpoint(t, fleet, "alice") {
		t.Fatal("an unchanged tenant writes nothing")
	}
	var files, pages, size, total, expected int64
	err := fleet.registry.db.QueryRowContext(context.Background(),
		`SELECT (SELECT COUNT(*) FROM vault_files WHERE tenant_id=?), (SELECT COUNT(*) FROM vault_pages WHERE tenant_id=?),
			(SELECT bytes FROM vault_heads WHERE tenant_id=?), (SELECT CAST(SUM(size) AS BIGINT) FROM vault_files WHERE tenant_id=?),
			(SELECT CAST(SUM((size+4095)/4096) AS BIGINT) FROM vault_files WHERE tenant_id=?)`,
		"alice", "alice", "alice", "alice", "alice").Scan(&files, &pages, &size, &total, &expected)
	if err != nil || files < 3 || pages != expected || size != total || size == 0 {
		t.Fatalf("the vault holds each page of each file once: %d files, %d pages of %d, %d bytes of %d, %v", files, pages, expected, size, total, err)
	}
}

// What a node checkpointed survives the node; what it did after its last
// checkpoint is gone with it, and the node that takes the tenant over runs it
// from the checkpoint.
func TestVaultKeepsCheckpointsOfAKilledNode(t *testing.T) {
	clock := newClock()
	database := sharedDatabase(t)
	a := storedNode(t, clock, database, nil)
	b := storedNode(t, clock, database, nil)
	createTenant(t, a.Fleet, "alice")
	say(t, a.Fleet, "alice", "before the checkpoint")
	checkpoint(t, a.Fleet, "alice")
	say(t, a.Fleet, "alice", "after the checkpoint")
	a.crash(t)

	clock.Set(clock.Now().Add(b.config.NodeTTL + time.Second))
	b.heartbeat(context.Background())
	b.wakeDue(context.Background())
	if b.opened("alice") == nil || stats(t, b.Fleet).Takeovers != 1 {
		t.Fatal("the surviving node takes the tenant over")
	}
	text := channelText(t, b.Fleet, "alice")
	if !strings.Contains(text, "before the checkpoint") || strings.Contains(text, "after the checkpoint") {
		t.Fatalf("the tenant resumes from its last checkpoint: %q", text)
	}
}

// An owner's write is answered only once it is in the vault, so the node may
// stop right after and the write is still there.
func TestVaultAnswersAWriteOnceItIsStored(t *testing.T) {
	clock := newClock()
	database := sharedDatabase(t)
	a := storedNode(t, clock, database, nil)
	b := storedNode(t, clock, database, nil)
	alice := createTenant(t, a.Fleet, "alice")
	if code, body := tenantCall(a.handler, http.MethodPost, "/t/alice/api/chat", alice, `{"text":"acknowledged line"}`); code != http.StatusAccepted || !strings.Contains(body, "trigger_id") {
		t.Fatalf("chat: %d %s", code, body)
	}
	a.crash(t)
	clock.Set(clock.Now().Add(b.config.NodeTTL + time.Second))
	b.heartbeat(context.Background())
	if text := channelText(t, b.Fleet, "alice"); !strings.Contains(text, "acknowledged line") {
		t.Fatalf("an acknowledged write survives the node: %q", text)
	}
}

// A node the others took for dead cannot write the tenant any more: its
// checkpoint is refused, the write it could not store is answered with a
// retry, and it closes the tenant without touching what the new holder has.
func TestVaultRefusesANodeThatLostTheTenant(t *testing.T) {
	clock := newClock()
	database := sharedDatabase(t)
	a := storedNode(t, clock, database, nil)
	b := storedNode(t, clock, database, nil)
	alice := createTenant(t, a.Fleet, "alice")
	say(t, a.Fleet, "alice", "stored by the first node")
	checkpoint(t, a.Fleet, "alice")
	stalled := a.opened("alice").replica

	// The first node stalls: the second takes the tenant and writes it.
	clock.Set(clock.Now().Add(b.config.NodeTTL + time.Second))
	b.heartbeat(context.Background())
	b.wakeDue(context.Background())
	if b.opened("alice") == nil || a.opened("alice") == nil {
		t.Fatal("both nodes have the tenant open for a moment; no file lock is shared between their disks")
	}
	// The placement alone stops the stalled node, before the holder has
	// written anything.
	say(t, a.Fleet, "alice", "written by the stalled node")
	if _, err := stalled.ship(context.Background()); !errors.Is(err, errFenced) {
		t.Fatalf("a checkpoint without the placement: %v", err)
	}
	stalled.lost = false
	say(t, b.Fleet, "alice", "stored by the second node")
	checkpoint(t, b.Fleet, "alice")
	// A checkpoint number that moved under a node is refused even while the
	// node holds the placement.
	held := b.opened("alice").replica
	if _, err := b.registry.db.ExecContext(context.Background(), `UPDATE vault_heads SET seq=seq+1 WHERE tenant_id=?`, "alice"); err != nil {
		t.Fatal(err)
	}
	say(t, b.Fleet, "alice", "written over a moved number")
	if _, err := held.ship(context.Background()); !errors.Is(err, errFenced) {
		t.Fatalf("a checkpoint on top of one the node never read: %v", err)
	}
	if _, err := b.registry.db.ExecContext(context.Background(), `UPDATE vault_heads SET seq=seq-1 WHERE tenant_id=?`, "alice"); err != nil {
		t.Fatal(err)
	}
	held.lost = false

	request := httptest.NewRequest(http.MethodPost, "/t/alice/api/chat", strings.NewReader(`{"text":"sent to the stalled node"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+alice)
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") == "" || strings.Contains(recorder.Body.String(), "trigger_id") {
		t.Fatalf("a write the node cannot store is not acknowledged: %d %s", recorder.Code, recorder.Body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for a.held() > 0 {
		if time.Now().After(deadline) {
			t.Fatal("the node that lost the tenant closes it")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := stats(t, a.Fleet); got.Abandons != 1 {
		t.Fatalf("it is counted: %+v", got)
	}
	if _, ok := readHead(a.dir("alice")); ok {
		t.Fatal("its copy is not marked usable")
	}
	if holder(t, a.Fleet, "alice") != b.node {
		t.Fatal("the holder keeps the placement")
	}
	text := channelText(t, b.Fleet, "alice")
	if !strings.Contains(text, "stored by the second node") || strings.Contains(text, "stalled node") {
		t.Fatalf("the holder's state is untouched: %q", text)
	}
	checkpoint(t, b.Fleet, "alice")
}

// An approved effect is executed only after the approval is stored.
func TestOutboxWaitsUntilTheApprovalIsStored(t *testing.T) {
	ctx := context.Background()
	clock := newClock()
	dir := t.TempDir()
	store, err := OpenStore(ctx, filepath.Join(dir, "assistant.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	connector, closeWorld, err := MockTenantConnect(ctx, "alice", dir, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer closeWorld()
	defer connector.Close()
	draft, _, err := store.CreateProposal(ctx, Proposal{Kind: ProposalSendEmail, TriggerID: 1, Thread: "t-q4-plan", SessionID: "s", ActionID: "a", To: ManagerAddress, Subject: "Re", Body: "ok"}, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DecideProposal(ctx, draft.ID, true, "me", clock.Now()); err != nil {
		t.Fatal(err)
	}
	outbox := NewOutbox(store, connector, clock.Now, nil)
	stored := false
	outbox.durable = func(context.Context) error {
		if !stored {
			return errors.New("the vault is away")
		}
		return nil
	}
	if outbox.Step(ctx) {
		t.Fatal("nothing is sent while the approval is not stored")
	}
	if got, _, _ := store.Proposal(ctx, draft.ID); got.Status != ProposalApproved || got.Attempts != 0 {
		t.Fatalf("the proposal waits, approved and untried: %+v", got)
	}
	stored = true
	if !outbox.Step(ctx) {
		t.Fatal("the effect is executed once the approval is stored")
	}
	if got, _, _ := store.Proposal(ctx, draft.ID); got.Status != ProposalSent {
		t.Fatalf("sent: %+v", got)
	}
}

// A deleted tenant leaves nothing in the vault, and a node that still has a
// copy of it does not hand that copy to a new tenant with the same name.
func TestVaultForgetsADeletedTenant(t *testing.T) {
	clock := newClock()
	database := sharedDatabase(t)
	brief := func(c *FleetConfig) { c.IdleTTL = time.Second }
	a := storedNode(t, clock, database, brief)
	b := storedNode(t, clock, database, brief)
	createTenant(t, a.Fleet, "alice")
	say(t, a.Fleet, "alice", "the first alice's line")
	clock.Set(clock.Now().Add(2 * time.Second))
	a.reap(context.Background())
	if !exists(a.dir("alice")) {
		t.Fatal("the first node keeps its copy")
	}
	if err := b.DeleteTenant(context.Background(), "alice"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"vault_heads", "vault_files", "vault_pages"} {
		var rows int
		if err := b.registry.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM `+table+` WHERE tenant_id=?`, "alice").Scan(&rows); err != nil || rows != 0 {
			t.Fatalf("%s still holds %d rows: %v", table, rows, err)
		}
	}
	clock.Set(clock.Now().Add(time.Minute))
	createTenant(t, b.Fleet, "alice")
	say(t, b.Fleet, "alice", "the second alice's line")
	clock.Set(clock.Now().Add(2 * time.Second))
	b.reap(context.Background())
	// Even when the new tenant's checkpoint number is the one the stale copy
	// carries, the copy is of another tenant.
	stale, _ := readHead(a.dir("alice"))
	fresh, ok := readHead(b.dir("alice"))
	if b.opened("alice") != nil || !ok || !stale.Clean || stale.Born == fresh.Born {
		t.Fatalf("the new tenant sleeps: stale copy %+v, new copy %+v", stale, fresh)
	}
	if err := writeHead(a.dir("alice"), vaultHead{Seq: fresh.Seq, Born: stale.Born, Clean: true}); err != nil {
		t.Fatal(err)
	}
	text := channelText(t, a.Fleet, "alice")
	if strings.Contains(text, "the first alice's line") || !strings.Contains(text, "the second alice's line") {
		t.Fatalf("a new tenant does not inherit a deleted one's copy: %q", text)
	}
}

// A node drops its copies of tenants it has not opened for long; the next
// open reads them from the vault.
func TestVaultNodeDropsCopiesOfAbsentTenants(t *testing.T) {
	clock := newClock()
	database := sharedDatabase(t)
	fleet := openFleet(t, clock, func(c *FleetConfig) {
		database(c)
		c.IdleTTL, c.ArchiveAfter = time.Second, 24*time.Hour
	})
	createTenant(t, fleet, "alice")
	createTenant(t, fleet, "bob")
	say(t, fleet, "alice", "kept in the vault")
	touch(t, fleet, "bob")
	clock.Set(clock.Now().Add(2 * time.Second))
	fleet.reap(context.Background())
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(headPath(fleet.dir("alice")), old, old); err != nil {
		t.Fatal(err)
	}
	fleet.janitor(context.Background())
	if exists(fleet.dir("alice")) || !exists(fleet.dir("bob")) || exists(archivePath(fleet.dir("alice"))) {
		t.Fatal("only the long-unused copy goes, and nothing is archived on the node")
	}
	if got := record(t, fleet, "alice"); got.State != TenantWarm || holder(t, fleet, "alice") != "" {
		t.Fatalf("the tenant itself is unchanged: %+v", got)
	}
	if text := channelText(t, fleet, "alice"); !strings.Contains(text, "kept in the vault") {
		t.Fatalf("it is read from the vault again: %q", text)
	}
}

// Every checkpoint shows the tenant's databases at one instant. A commit
// that lands in both databases after the first was read and before the second
// would give a state that never existed: the first without it, the second
// with it. The checkpoint notices and reads again.
func TestVaultSnapshotsDatabasesAtOneInstant(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	const ddl = `CREATE TABLE IF NOT EXISTS counter(n INTEGER NOT NULL); INSERT INTO counter SELECT 0 WHERE NOT EXISTS (SELECT 1 FROM counter)`
	names := []string{"a.db", "b.db"}
	databases := []*sql.DB{}
	for _, name := range names {
		db, err := openSQLite(ctx, filepath.Join(dir, name), ddl)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		databases = append(databases, db)
	}
	link := &replica{dir: dir, known: map[string]bool{}, stamps: map[string]fileStamp{}, sources: map[string]*vaultSource{}}
	defer link.closeSources()
	sources := []*vaultSource{}
	for _, name := range names {
		source, err := link.source(name)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, source)
	}
	// Twice, right after the first database's snapshot, both are written.
	calls, commits := 0, 0
	link.between = func() {
		calls++
		if calls%2 == 1 && commits < 2 {
			commits++
			for _, db := range databases {
				if _, err := db.Exec(`UPDATE counter SET n=n+1`); err != nil {
					t.Error(err)
				}
			}
		}
	}
	_, release, err := link.cut(sources)
	if err != nil {
		t.Fatal(err)
	}
	counts := []int64{}
	for i, source := range sources {
		image := link.next(names[i])
		if err := link.image(source, image); err != nil {
			t.Fatal(err)
		}
		conn, err := openSnapshotConn(image)
		if err != nil {
			t.Fatal(err)
		}
		n, err := queryInt(conn, "SELECT n FROM counter")
		if err = errors.Join(err, conn.Close()); err != nil {
			t.Fatal(err)
		}
		counts = append(counts, n)
	}
	release()
	if commits != 2 || calls != 6 || counts[0] != 2 || counts[1] != 2 {
		t.Fatalf("after %d commits and %d snapshots the images show %v; both must show the state after the last commit", commits, calls, counts)
	}

	// A tenant that never rests is not stored half-read: the checkpoint
	// gives up and the next one tries again.
	link.between = func() {
		if _, err := databases[0].Exec(`UPDATE counter SET n=n+1`); err != nil {
			t.Error(err)
		}
	}
	if _, _, err := link.cut(sources); err == nil {
		t.Fatal("a cut across databases that keep changing must fail")
	}
}

// The registry's statements run the same on both engines.
func TestRegistryOnEitherEngine(t *testing.T) {
	ctx := context.Background()
	clock := newClock()
	fleet := openFleet(t, clock, sharedDatabase(t))
	r, now := fleet.registry, clock.Now()
	for _, id := range []TenantID{"ann", "ben", "cat"} {
		if err := r.create(ctx, id, "hash-"+string(id), now); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.create(ctx, "ann", "other", now); !errors.Is(err, ErrTenantExists) {
		t.Fatalf("a second create: %v", err)
	}
	if page, err := r.list(ctx, "ann", 10); err != nil || len(page) != 2 || page[0].ID != "ben" {
		t.Fatalf("list after ann: %+v %v", page, err)
	}
	alive := now.Add(-time.Minute)
	if err := errors.Join(r.beat(ctx, "n1", "http://n1", now, now, 0, 4), r.beat(ctx, "n2", "http://n2", now, now, 3, 8), r.beat(ctx, "n1", "http://n1", now, now, 1, 4)); err != nil {
		t.Fatal(err)
	}
	if addr, ok, err := r.spare(ctx, "n1", alive); err != nil || !ok || addr != "http://n2" {
		t.Fatalf("spare: %q %t %v", addr, ok, err)
	}
	if claimed, err := r.claim(ctx, "ann", "n1", now, alive); err != nil || !claimed {
		t.Fatalf("claim: %t %v", claimed, err)
	}
	if claimed, err := r.claim(ctx, "ann", "n2", now, alive); err != nil || claimed {
		t.Fatalf("a held tenant is not claimed: %t %v", claimed, err)
	}
	if claimed, err := r.claim(ctx, "nobody", "n1", now, alive); err != nil || claimed {
		t.Fatalf("an unknown tenant is not claimed: %t %v", claimed, err)
	}
	if node, addr, held, err := r.holder(ctx, "ann", alive); err != nil || !held || node != "n1" || addr != "http://n1" {
		t.Fatalf("holder: %q %q %t %v", node, addr, held, err)
	}
	if err := r.activated(ctx, "ann"); err != nil {
		t.Fatal(err)
	}
	if removed, err := r.remove(ctx, "ann", "n2", alive); err != nil || removed {
		t.Fatalf("a tenant another live node holds is not removed: %t %v", removed, err)
	}
	later := now.Add(time.Hour)
	if slept, err := r.hibernated(ctx, "ann", "n2", later, now); err != nil || slept {
		t.Fatalf("a node that does not hold the tenant records nothing: %t %v", slept, err)
	}
	if slept, err := r.hibernated(ctx, "ann", "n1", later, now); err != nil || !slept {
		t.Fatalf("hibernated: %t %v", slept, err)
	}
	if got, _, err := r.get(ctx, "ann"); err != nil || got.Active || !wakeAt(t, got).Equal(later) || got.LastSeen == nil || got.Activations != 1 || got.State != TenantWarm {
		t.Fatalf("after sleep: %+v %v", got, err)
	}
	if err := r.seen(ctx, "ann", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := r.get(ctx, "ann"); !got.LastSeen.Equal(now) {
		t.Fatalf("an older presence does not move last seen back: %v", got.LastSeen)
	}
	if due, err := r.nudge(ctx, "ann", now, time.Time{}); err != nil || !due {
		t.Fatalf("nudge: %t %v", due, err)
	}
	if due, err := r.nudge(ctx, "ben", now, time.Time{}); err != nil || due {
		t.Fatalf("a tenant that was never opened is not nudged: %t %v", due, err)
	}
	if ids, err := r.due(ctx, now, time.Time{}, 10); err != nil || len(ids) != 1 || ids[0] != "ann" {
		t.Fatalf("due: %v %v", ids, err)
	}
	if err := r.postpone(ctx, "ann", later); err != nil {
		t.Fatal(err)
	}
	if retired, err := r.retire(ctx, now.Add(time.Minute)); err != nil || retired != 1 {
		t.Fatalf("retire: %d %v", retired, err)
	}
	if ids, err := r.archivable(ctx, now.Add(time.Minute), 10); err != nil || len(ids) != 1 {
		t.Fatalf("archivable: %v %v", ids, err)
	}
	if marked, err := r.archived(ctx, "ann"); err != nil || !marked {
		t.Fatalf("archived: %t %v", marked, err)
	}
	if counts, err := r.counts(ctx); err != nil || counts[TenantCold] != 1 || counts[TenantNew] != 2 {
		t.Fatalf("counts: %v %v", counts, err)
	}
	day := now.Format(slotLayout)
	if err := errors.Join(r.addUsage(ctx, "ann", day, 10, 3), r.addUsage(ctx, "ann", day, 5, 2), r.addUsage(ctx, "ben", day, 1, 1)); err != nil {
		t.Fatal(err)
	}
	if usage, err := r.usage(ctx, "ann", day); err != nil || usage.Calls != 2 || usage.InputTokens != 15 || usage.OutputTokens != 5 {
		t.Fatalf("usage: %+v %v", usage, err)
	}
	if total, err := r.usageTotal(ctx, day); err != nil || total.Calls != 3 || total.InputTokens != 16 {
		t.Fatalf("usage total: %+v %v", total, err)
	}

	// A node that stopped beating: its tenant is due and it leaves.
	if claimed, err := r.claim(ctx, "ben", "n2", now, alive); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	if err := r.activated(ctx, "ben"); err != nil {
		t.Fatal(err)
	}
	if err := r.beat(ctx, "n1", "http://n1", now, later, 0, 4); err != nil {
		t.Fatal(err)
	}
	if stale, err := r.stale(ctx, later.Add(-time.Minute)); err != nil || !stale {
		t.Fatalf("stale: %t %v", stale, err)
	}
	if recovered, err := r.recoverOrphans(ctx, later, later.Add(-time.Minute)); err != nil || recovered != 1 {
		t.Fatalf("recover: %d %v", recovered, err)
	}
	if nodes, err := r.nodes(ctx, later.Add(-time.Minute)); err != nil || len(nodes) != 1 || nodes[0].ID != "n1" || !nodes[0].Alive || nodes[0].MaxActive != 4 {
		t.Fatalf("only the node that still beats remains: %+v %v", nodes, err)
	}
	if got, _, _ := r.get(ctx, "ben"); got.Active || !wakeAt(t, got).Equal(later) {
		t.Fatalf("the dead node's tenant is due: %+v", got)
	}
	if claimed, err := r.claim(ctx, "ben", "n1", later, later.Add(-time.Minute)); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	if err := r.unplace(ctx, "ben", "n1"); err != nil {
		t.Fatal(err)
	}
	if removed, err := r.remove(ctx, "ann", "n1", alive); err != nil || !removed {
		t.Fatalf("remove: %t %v", removed, err)
	}
	if usage, _ := r.usage(ctx, "ann", day); usage.Calls != 0 {
		t.Fatalf("a removed tenant's usage goes with it: %+v", usage)
	}
	if err := r.leave(ctx, "n1", false); err != nil {
		t.Fatal(err)
	}
}

// stallingModel answers like the scripted model until its hold-th call,
// which it keeps until the turn is cancelled.
type stallingModel struct {
	inner agent.Provider
	hold  int
	held  chan struct{}

	mu    sync.Mutex
	calls int
}

func (m *stallingModel) Complete(ctx context.Context, request protocol.ModelRequest) (protocol.ModelReply, error) {
	m.mu.Lock()
	m.calls++
	stall := m.calls == m.hold
	m.mu.Unlock()
	if stall {
		close(m.held)
		<-ctx.Done()
		return protocol.ModelReply{}, ctx.Err()
	}
	return m.inner.Complete(ctx, request)
}

// A turn that was running on a node that stopped is not lost and does not
// start from nothing: the node that takes the tenant over finds the trigger
// still claimed and the worker's session as far as the last checkpoint, runs
// the trigger again in that session, and the draft the first run already
// made is not made twice.
func TestVaultResumesATurnOfAKilledNode(t *testing.T) {
	clock := newClock()
	database := sharedDatabase(t)
	model := &stallingModel{inner: &ScriptedModel{Now: clock.Now}, hold: 3, held: make(chan struct{})}
	a := storedNode(t, clock, database, func(c *FleetConfig) { c.Provider = model })
	b := storedNode(t, clock, database, nil)
	createTenant(t, a.Fleet, "alice")
	app := touch(t, a.Fleet, "alice")
	mail := deliver(t, app, PresetManager)
	select {
	case <-model.held:
	case <-time.After(20 * time.Second):
		t.Fatal("the turn never reached its third model call")
	}
	ctx := context.Background()
	before := triggerByID(t, mustTriggers(t, app.Store), mail)
	drafted, err := app.Store.ProposalsForTrigger(ctx, mail)
	if err != nil || before.Status != TriggerRunning || before.Attempts != 1 || len(drafted) != 1 {
		t.Fatalf("the turn is in flight and has drafted its reply: %+v, %d drafts, %v", before, len(drafted), err)
	}
	checkpoint(t, a.Fleet, "alice")
	// The checkpoint holds the session as far as the turn got: the prompt and
	// the two model replies with their tool results.
	stored, err := openSnapshotConn(a.opened("alice").replica.base("agent.db"))
	if err != nil {
		t.Fatal(err)
	}
	steps, err := queryInt(stored, "SELECT COUNT(*) FROM messages")
	if err = errors.Join(err, stored.Close()); err != nil || steps < 5 {
		t.Fatalf("the stored session holds %d messages: %v", steps, err)
	}
	a.crash(t)

	clock.Set(clock.Now().Add(b.config.NodeTTL + time.Second))
	b.heartbeat(context.Background())
	b.wakeDue(context.Background())
	if b.opened("alice") == nil {
		t.Fatal("the surviving node takes the tenant over")
	}
	resumed := b.opened("alice").app
	after := triggerByID(t, settle(t, resumed.Store), mail)
	if after.Status != TriggerDone || after.Attempts != 2 {
		t.Fatalf("the trigger ran again to its end: %+v", after)
	}
	proposals, err := resumed.Store.ProposalsForTrigger(ctx, mail)
	if err != nil || len(proposals) != 1 || proposals[0].ID != drafted[0].ID {
		t.Fatalf("one draft, the one the first run made: before %+v, after %+v, %v", drafted, proposals, err)
	}
	if session, ok, err := resumed.Store.Subscription(ctx, after.Thread); err != nil || !ok || session != after.SessionID {
		t.Fatalf("the thread keeps its worker: %q %t %v", session, ok, err)
	}
	ledger, _ := resumed.Store.Ledger(ctx, 200)
	recovered, restored := false, false
	for _, entry := range ledger {
		recovered = recovered || entry.Kind == LedgerRecovery
		restored = restored || (entry.Kind == LedgerRoute && entry.TriggerID == mail && strings.Contains(entry.Detail, "从持久状态恢复"))
	}
	if !recovered || !restored {
		t.Fatalf("the ledger records the recovery (%t) and the restored worker (%t)", recovered, restored)
	}
}

func mustTriggers(t *testing.T, store *Store) []Trigger {
	t.Helper()
	triggers, err := store.Triggers(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	return triggers
}

// The vault follows files that shrink or go, so what another node reads back
// is the directory as it last was.
func TestVaultFollowsFilesThatShrinkAndGo(t *testing.T) {
	clock := newClock()
	database := sharedDatabase(t)
	brief := func(c *FleetConfig) { c.IdleTTL = time.Second }
	a := storedNode(t, clock, database, brief)
	b := storedNode(t, clock, database, brief)
	createTenant(t, a.Fleet, "alice")
	touch(t, a.Fleet, "alice")
	write := func(rel string, size int, fill byte) {
		t.Helper()
		path := filepath.Join(a.dir("alice"), rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Repeat(string(fill), size)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("notes/long.txt", 3*vaultPage+100, 'a')
	write("notes/gone.txt", 10, 'b')
	write("notes/empty.txt", 0, 'c')
	checkpoint(t, a.Fleet, "alice")
	write("notes/long.txt", vaultPage+7, 'd')
	if err := os.Remove(filepath.Join(a.dir("alice"), "notes/gone.txt")); err != nil {
		t.Fatal(err)
	}
	checkpoint(t, a.Fleet, "alice")
	var pages int
	if err := a.registry.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM vault_pages WHERE tenant_id=? AND path IN (?,?)`, "alice", "notes/long.txt", "notes/gone.txt").Scan(&pages); err != nil || pages != 2 {
		t.Fatalf("the pages past the new end and the removed file's are gone: %d %v", pages, err)
	}
	clock.Set(clock.Now().Add(2 * time.Second))
	a.reap(context.Background())
	touch(t, b.Fleet, "alice")
	long, err := os.ReadFile(filepath.Join(b.dir("alice"), "notes/long.txt"))
	if err != nil || string(long) != strings.Repeat("d", vaultPage+7) {
		t.Fatalf("the shrunk file is read back as it was: %d bytes, %v", len(long), err)
	}
	if empty, err := os.ReadFile(filepath.Join(b.dir("alice"), "notes/empty.txt")); err != nil || len(empty) != 0 {
		t.Fatalf("an empty file is kept: %v", err)
	}
	if exists(filepath.Join(b.dir("alice"), "notes/gone.txt")) {
		t.Fatal("a removed file does not come back")
	}
}

// An open tenant is checkpointed on its own every Checkpoint interval, so a
// node that stops takes at most that much with it.
func TestVaultCheckpointsAnOpenTenantOnItsOwn(t *testing.T) {
	clock := newClock()
	database := sharedDatabase(t)
	often := func(c *FleetConfig) { c.Checkpoint = 20 * time.Millisecond }
	a := storedNode(t, clock, database, often)
	b := storedNode(t, clock, database, often)
	createTenant(t, a.Fleet, "alice")
	say(t, a.Fleet, "alice", "never checkpointed by hand")
	waitFor(t, "a checkpoint of its own", func() bool { return stats(t, a.Fleet).Checkpoints > 0 })
	// The checkpoint that was counted may have begun before the line was
	// written; ten intervals later one that saw it has been stored.
	time.Sleep(200 * time.Millisecond)
	a.crash(t)
	clock.Set(clock.Now().Add(b.config.NodeTTL + time.Second))
	b.heartbeat(context.Background())
	if text := channelText(t, b.Fleet, "alice"); !strings.Contains(text, "never checkpointed by hand") {
		t.Fatalf("the line survived the node: %q", text)
	}
}

// Nodes that share only the database still open each due tenant once: the
// placement is one row, whichever engine holds it.
func TestVaultWakesEachDueTenantOnce(t *testing.T) {
	clock := newClock()
	database := sharedDatabase(t)
	a := storedNode(t, clock, database, nil)
	b := storedNode(t, clock, database, nil)
	ids := []TenantID{"ann", "ben", "cat", "dan", "eve", "fay", "gus", "hal"}
	for _, id := range ids {
		createTenant(t, a.Fleet, id)
		if err := a.registry.postpone(context.Background(), id, clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	var wakers sync.WaitGroup
	for _, node := range []*testNode{a, b} {
		wakers.Add(1)
		go func() {
			defer wakers.Done()
			node.wakeDue(context.Background())
		}()
	}
	wakers.Wait()
	for _, id := range ids {
		onA, onB := a.opened(id) != nil, b.opened(id) != nil
		if onA == onB {
			t.Errorf("%s: open on the first node %t, on the second %t", id, onA, onB)
		}
	}
	if failures := stats(t, a.Fleet).WakeFailures + stats(t, b.Fleet).WakeFailures; failures != 0 {
		t.Fatalf("losing a tenant to the other node is not a failure: %d", failures)
	}
}

// A tenant that was closed while the registry could not be written is still
// marked as running on this node, so nobody would wake it. The node writes
// the record once the registry answers again.
func TestFleetRecordsASleepTheRegistryMissed(t *testing.T) {
	ctx := context.Background()
	clock := newClock()
	database := sharedDatabase(t)
	fleet := openFleet(t, clock, func(c *FleetConfig) {
		database(c)
		c.IdleTTL = time.Second
	})
	createTenant(t, fleet, "alice")
	touch(t, fleet, "alice")
	rename := func(from, to string) {
		t.Helper()
		if _, err := fleet.registry.db.ExecContext(ctx, `ALTER TABLE `+from+` RENAME TO `+to); err != nil {
			t.Fatal(err)
		}
	}
	rename("placements", "placements_away")
	clock.Set(clock.Now().Add(2 * time.Second))
	fleet.reap(ctx)
	if fleet.held() != 0 || !record(t, fleet, "alice").Active {
		t.Fatal("the tenant is closed here while its row still says it runs")
	}
	rename("placements_away", "placements")
	fleet.reap(ctx)
	if got := record(t, fleet, "alice"); got.Active || got.NextWake == nil || holder(t, fleet, "alice") != "" {
		t.Fatalf("the sleep is recorded once the registry answers: %+v", got)
	}
	if text := channelText(t, fleet, "alice"); text == "" {
		t.Fatal("the tenant opens again")
	}
}
