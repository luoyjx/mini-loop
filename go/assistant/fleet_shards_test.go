package assistant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// testSchema makes a schema of the test PostgreSQL database and returns the
// URL that selects it.
func testSchema(t *testing.T, address string) string {
	t.Helper()
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
	return parsed.String()
}

// shardedDatabase gives one test's fleets a registry and named shards of
// their own: schemas of the PostgreSQL database when postgresEnv is set,
// SQLite files otherwise. The function it returns configures a node with the
// registry and the shards named, or all of them; registryOnly names none.
func shardedDatabase(t *testing.T, names ...string) func(only ...string) func(*FleetConfig) {
	t.Helper()
	address := os.Getenv(postgresEnv)
	root := t.TempDir()
	registry := ""
	shards := map[string]string{}
	if address != "" {
		registry = testSchema(t, address)
	}
	for _, name := range names {
		if address == "" {
			shards[name] = filepath.Join(root, "shard-"+name)
		} else {
			shards[name] = testSchema(t, address)
		}
	}
	return func(only ...string) func(*FleetConfig) {
		if len(only) == 0 {
			only = names
		}
		return func(c *FleetConfig) {
			c.Checkpoint = time.Hour
			chosen := map[string]string{}
			for _, name := range only {
				if name != registryOnly {
					chosen[name] = shards[name]
				}
			}
			if address == "" {
				c.registryDir, c.offload, c.shardDirs = filepath.Join(root, "registry"), true, chosen
			} else {
				c.Database, c.Shards = registry, chosen
			}
		}
	}
}

const registryOnly = "-"

// stored counts the rows a shard holds for a tenant.
func stored(t *testing.T, fleet *Fleet, shard string, id TenantID) int {
	t.Helper()
	s, err := fleet.vault.shard(shard)
	if err != nil {
		t.Fatal(err)
	}
	db, err := s.use(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, table := range []string{"vault_heads", "vault_files", "vault_pages"} {
		var rows int
		if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM `+table+` WHERE tenant_id=?`, string(id)).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		total += rows
	}
	return total
}

func shardOf(t *testing.T, fleet *Fleet, id TenantID) string {
	t.Helper()
	return record(t, fleet, id).Shard
}

func shardInfo(t *testing.T, fleet *Fleet, name string) ShardInfo {
	t.Helper()
	shards, err := fleet.Shards(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, shard := range shards {
		if shard.Name == name {
			return shard
		}
	}
	t.Fatalf("no shard %s in %+v", name, shards)
	return ShardInfo{}
}

// sleep closes every quiet tenant of a fleet.
func sleep(t *testing.T, clock *fixedClock, fleets ...*Fleet) {
	t.Helper()
	clock.Set(clock.Now().Add(routeTTL + 2*time.Second))
	for _, fleet := range fleets {
		fleet.reap(context.Background())
		if fleet.held() != 0 {
			t.Fatalf("%d tenants stay open", fleet.held())
		}
	}
}

func brief(c *FleetConfig) { c.IdleTTL = time.Second }

// With shards, each tenant's data is in one of them and nowhere else: new
// tenants go to the shard with the fewest, the registry database holds no
// pages, and any node reads a tenant from the shard its row names.
func TestShardsSpreadTenantsAndKeepTheirDataApart(t *testing.T) {
	clock := newClock()
	database := shardedDatabase(t, "s1", "s2")
	a := storedNode(t, clock, database(), brief)
	b := storedNode(t, clock, database(), brief)
	ids := []TenantID{"alice", "bob", "carol", "dave"}
	for _, id := range ids {
		createTenant(t, a.Fleet, id)
		if got := shardOf(t, a.Fleet, id); got != "" {
			t.Fatalf("%s has a shard before it has data: %q", id, got)
		}
		say(t, a.Fleet, id, "kept for "+string(id))
	}
	sleep(t, clock, a.Fleet)
	count := map[string]int{}
	for _, id := range ids {
		shard := shardOf(t, a.Fleet, id)
		count[shard]++
		other := map[string]string{"s1": "s2", "s2": "s1"}[shard]
		if other == "" || stored(t, a.Fleet, shard, id) < 3 || stored(t, a.Fleet, other, id) != 0 || stored(t, a.Fleet, homeShard, id) != 0 {
			t.Fatalf("%s is stored in %q only", id, shard)
		}
	}
	if count["s1"] != 2 || count["s2"] != 2 {
		t.Fatalf("new tenants go to the shard with the fewest: %v", count)
	}
	if s1, home := shardInfo(t, a.Fleet, "s1"), shardInfo(t, a.Fleet, homeShard); s1.State != ShardOpen || s1.Tenants != 2 || !s1.Configured || s1.Down || home.State != ShardClosed || home.Tenants != 0 {
		t.Fatalf("the catalogue counts them, and the registry database takes none: %+v %+v", s1, home)
	}
	for _, id := range ids {
		if text := channelText(t, b.Fleet, id); !strings.Contains(text, "kept for "+string(id)) {
			t.Fatalf("another node reads %s from its shard: %q", id, text)
		}
	}
	if got := stats(t, b.Fleet); got.VaultPulls != 4 || len(got.Shards) != 3 {
		t.Fatalf("stats: %+v", got)
	}
}

// A shard that does not answer holds up only its own tenants. After a few
// failures the node stops asking it for a while, so opens of its tenants fail
// at once and the others are served; a node starts without it.
func TestShardThatIsAwayHoldsUpOnlyItsTenants(t *testing.T) {
	clock := newClock()
	database := shardedDatabase(t, "s1", "s2")
	fleet := storedNode(t, clock, database(), brief)
	alice, bob := createTenant(t, fleet.Fleet, "alice"), createTenant(t, fleet.Fleet, "bob")
	say(t, fleet.Fleet, "alice", "in the first shard")
	say(t, fleet.Fleet, "bob", "in the second shard")
	if shardOf(t, fleet.Fleet, "alice") != "s1" || shardOf(t, fleet.Fleet, "bob") != "s2" {
		t.Fatal("one tenant in each shard")
	}

	// The shard goes away while its tenant is open: the tenant stays open,
	// reads are answered, and a write is not acknowledged until it is stored.
	s1 := fleet.vault.shards["s1"]
	s1.cooldown = time.Hour
	s1.db.down.Store(true)
	if code, _ := tenantCall(fleet.handler, http.MethodGet, "/t/alice/api/state", alice, ""); code != http.StatusOK {
		t.Fatalf("a read needs no shard: %d", code)
	}
	for range shardTroubles {
		if code, _ := tenantCall(fleet.handler, http.MethodPost, "/t/alice/api/chat", alice, `{"text":"while the shard is away"}`); code != http.StatusServiceUnavailable {
			t.Fatalf("a write that is not stored is not acknowledged: %d", code)
		}
	}
	if fleet.opened("alice") == nil || !shardInfo(t, fleet.Fleet, "s1").Down || shardInfo(t, fleet.Fleet, "s2").Down {
		t.Fatal("the tenant stays open and the shard is marked down")
	}
	if code, body := tenantCall(fleet.handler, http.MethodPost, "/t/bob/api/chat", bob, `{"text":"the other shard works"}`); code != http.StatusAccepted {
		t.Fatalf("a tenant of another shard is served: %d %s", code, body)
	}
	// While the shard is left alone nothing is sent to it.
	s1.db.down.Store(false)
	if _, err := fleet.opened("alice").replica.checkpoint(context.Background()); !errors.Is(err, errShardDown) {
		t.Fatalf("a shard that is down is not asked: %v", err)
	}
	// After the cooldown the next statement goes through and the tenant's
	// changes, kept on the node meanwhile, are stored.
	s1.mu.Lock()
	s1.downUntil = time.Now().Add(-time.Second)
	s1.mu.Unlock()
	if !checkpoint(t, fleet.Fleet, "alice") || shardInfo(t, fleet.Fleet, "s1").Down {
		t.Fatal("the shard is used again once it answers")
	}
	// The chats started turns; a tenant sleeps once they are done.
	for _, id := range []TenantID{"alice", "bob"} {
		settle(t, touch(t, fleet.Fleet, id).Store)
	}
	sleep(t, clock, fleet.Fleet)

	// A node starts while a shard cannot be reached, serves the others, and
	// answers for the unreachable one's tenants with a retry.
	away := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(away, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	other := storedNode(t, clock, func(c *FleetConfig) {
		database()(c)
		if c.shardDirs != nil {
			c.shardDirs["s1"] = away
		} else {
			c.Shards["s1"] = "postgres://nobody:nothing@127.0.0.1:1/none?connect_timeout=1"
		}
	}, brief)
	if text := channelText(t, other.Fleet, "bob"); !strings.Contains(text, "in the second shard") {
		t.Fatalf("a node without one shard serves the others: %q", text)
	}
	if code, _ := tenantCall(other.handler, http.MethodGet, "/t/alice/api/state", alice, ""); code == http.StatusOK {
		t.Fatal("a tenant whose shard cannot be reached does not open as an empty one")
	}
	if other.opened("alice") != nil || holder(t, other.Fleet, "alice") != "" {
		t.Fatal("and it is not held by the node that could not open it")
	}
	if text := channelText(t, fleet.Fleet, "alice"); !strings.Contains(text, "in the first shard") || !strings.Contains(text, "while the shard is away") {
		t.Fatalf("a node that reaches the shard has everything: %q", text)
	}
}

// A shard that joins a running fleet takes no tenants until an operator
// opens it; a node that was not told of a shard the registry knows does not
// start; and the fleet always keeps a shard that takes new tenants.
func TestShardNewToTheFleetWaitsToBeOpened(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t, "s1", "s2")
	first := storedNode(t, clock, database("s1"), brief)
	createTenant(t, first.Fleet, "alice")
	say(t, first.Fleet, "alice", "before the second shard")

	second := storedNode(t, clock, database(), brief)
	if got := shardInfo(t, second.Fleet, "s2"); got.State != ShardClosed || got.Tenants != 0 {
		t.Fatalf("a shard new to the fleet is closed: %+v", got)
	}
	if got := shardInfo(t, first.Fleet, "s2"); got.Configured {
		t.Fatalf("the node that was not given it says so: %+v", got)
	}
	createTenant(t, second.Fleet, "bob")
	say(t, second.Fleet, "bob", "still the first shard")
	if shardOf(t, second.Fleet, "bob") != "s1" {
		t.Fatal("a closed shard takes no tenants")
	}
	start := func(advertise string, shards ...string) error {
		config := FleetConfig{DataDir: t.TempDir(), Provider: &ScriptedModel{Now: clock.Now}, Model: ScriptedModelID, Now: clock.Now, Advertise: advertise}
		database(shards...)(&config)
		fleet, err := OpenFleet(ctx, config)
		if err == nil {
			err = errors.Join(errors.New("it started"), fleet.Close())
		}
		return err
	}
	if err := start("http://third", "s2"); !strings.Contains(err.Error(), `shard "s1"`) {
		t.Fatalf("a node that lacks a shard the registry knows does not start: %v", err)
	}
	// A node that does not say how the others reach it takes the registry
	// for its own. With nodes running on the database it does not start.
	if os.Getenv(postgresEnv) != "" {
		if err := start(""); !strings.Contains(err.Error(), "2 nodes are running") {
			t.Fatalf("a node that would clear the others' rows does not start: %v", err)
		}
	}

	if err := second.SetShard(ctx, "s2", ShardOpen); err != nil {
		t.Fatal(err)
	}
	createTenant(t, second.Fleet, "carol")
	say(t, second.Fleet, "carol", "the new shard has the fewest")
	if shardOf(t, second.Fleet, "carol") != "s2" {
		t.Fatal("an open shard with the fewest tenants takes the next one")
	}
	if err := second.SetShard(ctx, "s1", ShardClosed); err != nil {
		t.Fatal(err)
	}
	if err := second.SetShard(ctx, "s2", ShardDraining); err == nil {
		t.Fatal("the only shard that takes new tenants stays open")
	}
	if err := second.SetShard(ctx, "s1", ShardRetired); !errors.Is(err, ErrShardInUse) {
		t.Fatalf("a shard with tenants is not retired: %v", err)
	}
	if err := second.SetShard(ctx, "s9", ShardOpen); !errors.Is(err, ErrShardUnknown) {
		t.Fatalf("an unknown shard: %v", err)
	}
	if err := second.SetShard(ctx, "s1", "gone"); err == nil {
		t.Fatal("an unknown state is refused")
	}
	if code, body := tenantCall(second.handler, http.MethodPost, "/admin/shards/s1", clusterAdmin, `{"state":"open"}`); code != http.StatusOK || !strings.Contains(body, `"name":"s1","state":"open","tenants":2`) {
		t.Fatalf("the admin API changes a shard: %d %s", code, body)
	}
	if code, body := tenantCall(second.handler, http.MethodGet, "/admin/shards", clusterAdmin, ""); code != http.StatusOK || !strings.Contains(body, `"name":"home","state":"closed"`) {
		t.Fatalf("and lists them: %d %s", code, body)
	}
	if code, _ := tenantCall(second.handler, http.MethodPost, "/admin/shards/s9", clusterAdmin, `{"state":"open"}`); code != http.StatusNotFound {
		t.Fatalf("an unknown shard is not found: %d", code)
	}
}

// Draining a shard moves its sleeping tenants to the open ones, with their
// data whole, and leaves nothing of them behind; the emptied shard can be
// retired, and nodes then start without it.
func TestDrainingAShardMovesItsTenants(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t, "s1", "s2")
	a := storedNode(t, clock, database(), brief)
	b := storedNode(t, clock, database(), brief)
	ids := []TenantID{"alice", "bob", "carol", "dave"}
	for _, id := range ids {
		createTenant(t, a.Fleet, id)
		say(t, a.Fleet, id, "kept for "+string(id))
	}
	// One of the first shard's tenants stays open on another node.
	var busy TenantID
	for _, id := range ids {
		if shardOf(t, a.Fleet, id) == "s1" {
			busy = id
		}
	}
	sleep(t, clock, a.Fleet)
	touch(t, b.Fleet, busy)

	if err := a.SetShard(ctx, "s1", ShardDraining); err != nil {
		t.Fatal(err)
	}
	a.drain(ctx)
	a.drain(ctx)
	for _, id := range ids {
		want := "s2"
		if id == busy {
			want = "s1"
		}
		if got := shardOf(t, a.Fleet, id); got != want {
			t.Fatalf("%s is in %s, not %s", id, got, want)
		}
	}
	if got := stats(t, a.Fleet); got.Moves != 1 || got.Leftovers != 0 || shardInfo(t, a.Fleet, "s1").Tenants != 1 || shardInfo(t, a.Fleet, "s2").Tenants != 3 {
		t.Fatalf("a tenant that is open waits; the rest moved: %+v", got)
	}
	if err := a.SetShard(ctx, "s1", ShardRetired); !errors.Is(err, ErrShardInUse) {
		t.Fatalf("not empty yet: %v", err)
	}
	// The node that last closed a tenant still has the right copy of it: the
	// move changed where the checkpoint is, not which one it is.
	for _, id := range ids {
		if id != busy {
			touch(t, a.Fleet, id)
		}
	}
	if got := stats(t, a.Fleet); got.CacheReuses != 3 || got.VaultPulls != 0 {
		t.Fatalf("a moved tenant's copy on the node is still good: %+v", got)
	}
	sleep(t, clock, a.Fleet, b.Fleet)
	a.drain(ctx)
	a.drain(ctx)
	for _, id := range ids {
		if shardOf(t, a.Fleet, id) != "s2" || stored(t, a.Fleet, "s1", id) != 0 || stored(t, a.Fleet, "s2", id) < 3 {
			t.Fatalf("%s moved whole and left nothing behind", id)
		}
		if text := channelText(t, b.Fleet, id); !strings.Contains(text, "kept for "+string(id)) {
			t.Fatalf("%s reads the same from its new shard: %q", id, text)
		}
	}
	sleep(t, clock, b.Fleet)
	if err := a.SetShard(ctx, "s1", ShardRetired); err != nil {
		t.Fatalf("an emptied shard is retired: %v", err)
	}
	if err := a.SetShard(ctx, "s1", ShardOpen); err == nil {
		t.Fatal("a retired shard stays retired")
	}
	without := storedNode(t, clock, database("s2"), brief)
	if text := channelText(t, without.Fleet, "alice"); !strings.Contains(text, "kept for alice") {
		t.Fatalf("a node without the retired shard serves every tenant: %q", text)
	}
}

// A move that stops half way is completed or undone by whoever looks next,
// and never while the move may still be running.
func TestMoveLeftHalfDoneIsCompletedOrUndone(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t, "s1", "s2")
	a := storedNode(t, clock, database(), brief)
	b := storedNode(t, clock, database(), brief)
	for _, id := range []TenantID{"alice", "bob"} {
		createTenant(t, a.Fleet, id)
		say(t, a.Fleet, id, "kept for "+string(id))
	}
	sleep(t, clock, a.Fleet)
	halfway := func(id TenantID, flip bool) (from, to string) {
		t.Helper()
		row := record(t, a.Fleet, id)
		from = row.Shard
		to = map[string]string{"s1": "s2", "s2": "s1"}[from]
		born, now := millis(row.CreatedAt), clock.Now()
		release, err := a.hold(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		if err := a.registry.intend(ctx, leftover{id, born, to}, now); err != nil {
			t.Fatal(err)
		}
		source, _ := a.vault.shards[from].use(ctx)
		target, _ := a.vault.shards[to].use(ctx)
		head, err := take(ctx, source, id, a.node, born, false, now)
		if err == nil {
			_, err = take(ctx, target, id, a.node, born, true, now)
		}
		if err == nil {
			err = copyTenant(ctx, source, target, id, a.node, head, now)
		}
		if err != nil {
			t.Fatal(err)
		}
		if flip {
			if moved, err := a.registry.moved(ctx, id, born, a.node, from, to, now); err != nil || !moved {
				t.Fatal(moved, err)
			}
		}
		return from, to
	}

	// Stopped after the copy: the tenant is still in its old shard and the
	// copy in the new one is the leftover.
	from, to := halfway("alice", false)
	if stats(t, a.Fleet).Leftovers != 1 || stored(t, a.Fleet, to, "alice") < 3 {
		t.Fatal("the unfinished copy is written down")
	}
	// While the mover may still be at it (it holds the tenant) nobody
	// removes the copy.
	release, err := a.hold(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	b.collect(ctx)
	kept, pending := stored(t, a.Fleet, to, "alice"), stats(t, a.Fleet).Leftovers
	release()
	if kept < 3 || pending != 1 {
		t.Fatal("a copy whose move may be running is left alone")
	}
	b.collect(ctx)
	if stored(t, a.Fleet, to, "alice") != 0 || stored(t, a.Fleet, from, "alice") < 3 || stats(t, a.Fleet).Leftovers != 0 || shardOf(t, a.Fleet, "alice") != from {
		t.Fatal("the copy of a move that never finished is removed")
	}
	if text := channelText(t, b.Fleet, "alice"); !strings.Contains(text, "kept for alice") {
		t.Fatalf("and the tenant is whole where it was: %q", text)
	}

	// Stopped after the registry named the new shard: the old one is the
	// leftover.
	from, to = halfway("bob", true)
	if shardOf(t, a.Fleet, "bob") != to || stored(t, a.Fleet, from, "bob") < 3 || stats(t, a.Fleet).Leftovers != 1 {
		t.Fatal("the old copy is written down")
	}
	b.collect(ctx)
	if stored(t, a.Fleet, from, "bob") != 0 || stats(t, a.Fleet).Leftovers != 0 {
		t.Fatal("the old copy is removed")
	}
	if text := channelText(t, b.Fleet, "bob"); !strings.Contains(text, "kept for bob") {
		t.Fatalf("and the tenant is whole in its new shard: %q", text)
	}
	if got := stats(t, b.Fleet); got.Purges != 2 {
		t.Fatalf("both are counted: %+v", got)
	}

	// A row that names the place where the tenant's data is now is stale,
	// whatever wrote it: the row goes and the data stays.
	sleep(t, clock, b.Fleet)
	row := record(t, b.Fleet, "bob")
	if err := b.registry.intend(ctx, leftover{"bob", millis(row.CreatedAt), row.Shard}, clock.Now()); err != nil {
		t.Fatal(err)
	}
	b.collect(ctx)
	if stats(t, b.Fleet).Leftovers != 0 || stored(t, b.Fleet, row.Shard, "bob") < 3 {
		t.Fatal("the tenant's own data is never removed as a leftover")
	}
}

// A deleted tenant leaves nothing in its shard. If the shard is away when the
// tenant is deleted, its data goes later, and a new tenant with the same name
// never sees it and loses nothing when it does go.
func TestDeletedTenantLeavesNothingInItsShard(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t, "s1")
	fleet := storedNode(t, clock, database(), brief)
	createTenant(t, fleet.Fleet, "alice")
	say(t, fleet.Fleet, "alice", "the first alice")
	if err := fleet.DeleteTenant(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if stored(t, fleet.Fleet, "s1", "alice") != 0 || stats(t, fleet.Fleet).Leftovers != 0 || shardInfo(t, fleet.Fleet, "s1").Tenants != 0 {
		t.Fatal("a deleted tenant's data goes with it")
	}

	createTenant(t, fleet.Fleet, "bob")
	say(t, fleet.Fleet, "bob", "the first bob")
	sleep(t, clock, fleet.Fleet)
	s1 := fleet.vault.shards["s1"]
	s1.db.down.Store(true)
	if err := fleet.DeleteTenant(ctx, "bob"); err != nil {
		t.Fatalf("a tenant is deleted while its shard is away: %v", err)
	}
	s1.db.down.Store(false)
	if _, _, err := fleet.Tenant(ctx, "bob"); !errors.Is(err, ErrTenantUnknown) || stats(t, fleet.Fleet).Leftovers != 1 || stored(t, fleet.Fleet, "s1", "bob") < 3 {
		t.Fatal("the tenant is gone and its data is written down to remove")
	}
	clock.Set(clock.Now().Add(time.Minute))
	createTenant(t, fleet.Fleet, "bob")
	if text := channelText(t, fleet.Fleet, "bob"); strings.Contains(text, "the first bob") {
		t.Fatalf("a new tenant does not get a deleted one's data: %q", text)
	}
	say(t, fleet.Fleet, "bob", "the second bob")
	checkpoint(t, fleet.Fleet, "bob")
	fleet.collect(ctx)
	if stats(t, fleet.Fleet).Leftovers != 0 {
		t.Fatal("the deleted tenant's row is cleared")
	}
	sleep(t, clock, fleet.Fleet)
	if err := os.RemoveAll(fleet.dir("bob")); err != nil {
		t.Fatal(err)
	}
	if text := channelText(t, fleet.Fleet, "bob"); !strings.Contains(text, "the second bob") || strings.Contains(text, "the first bob") {
		t.Fatalf("and the new tenant's data is untouched: %q", text)
	}
}

// A node reuses its copy of a tenant only when the copy is of the very
// checkpoint the shard holds. Checkpoint numbers start again when a tenant's
// data is written anew, so the number alone does not say that.
func TestVaultNodeDoesNotTrustACopyByItsNumberAlone(t *testing.T) {
	clock := newClock()
	database := shardedDatabase(t, "s1")
	a := storedNode(t, clock, database(), brief)
	b := storedNode(t, clock, database(), brief)
	createTenant(t, a.Fleet, "alice")
	say(t, a.Fleet, "alice", "the first node's version")
	sleep(t, clock, a.Fleet)
	say(t, b.Fleet, "alice", "the second node's version")
	sleep(t, clock, b.Fleet)
	stale, ok := readHead(a.dir("alice"))
	current, _ := readHead(b.dir("alice"))
	if !ok || !stale.Clean || stale.Mark == "" || stale.Mark == current.Mark || stale.Seq == current.Seq {
		t.Fatalf("each checkpoint has a name of its own: %+v %+v", stale, current)
	}
	// The first node's copy claims the number the shard is at.
	stale.Seq = current.Seq
	if err := writeHead(a.dir("alice"), stale); err != nil {
		t.Fatal(err)
	}
	pulls := stats(t, a.Fleet).VaultPulls
	if text := channelText(t, a.Fleet, "alice"); !strings.Contains(text, "the second node's version") {
		t.Fatalf("a copy of another checkpoint with the same number is not used: %q", text)
	}
	if got := stats(t, a.Fleet).VaultPulls; got != pulls+1 {
		t.Fatalf("the tenant is read from the shard: %d pulls", got-pulls)
	}
}

// Model use is what a bill would be built from. A call made while the
// registry is away is kept and recorded once the registry answers, once.
func TestFleetRecordsModelUseTheRegistryMissed(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	fleet := storedNode(t, clock, shardedDatabase(t)(), brief)
	createTenant(t, fleet.Fleet, "alice")
	key := usageKey{"alice", clock.Now().Format(slotLayout)}
	fleet.count(ctx, key, usageSum{1, 10, 5})
	fleet.registry.db.down.Store(true)
	fleet.count(ctx, key, usageSum{1, 20, 7})
	fleet.count(ctx, key, usageSum{1, 30, 9})
	fleet.settleUsage(ctx)
	fleet.registry.db.down.Store(false)
	if owed := stats(t, fleet.Fleet).UsageOwed; owed != 2 {
		t.Fatalf("two calls are kept while the registry is away: %d", owed)
	}
	// Later calls join the ones that are kept, so the registry gets them in
	// one statement and no call waits on a registry that may still be away.
	fleet.count(ctx, key, usageSum{1, 1, 1})
	if owed := stats(t, fleet.Fleet).UsageOwed; owed != 3 {
		t.Fatalf("a call made while others are kept is kept with them: %d", owed)
	}
	for range 2 {
		fleet.reap(ctx)
	}
	usage, err := fleet.registry.usage(ctx, "alice", key.day)
	if err != nil || usage.Calls != 4 || usage.InputTokens != 61 || usage.OutputTokens != 22 || stats(t, fleet.Fleet).UsageOwed != 0 {
		t.Fatalf("every call is recorded once: %+v %v", usage, err)
	}
}

// What a shard holds for a file is checked against the file's checksum when
// a node reads it: a tenant with a damaged page does not open, and says why.
func TestVaultRefusesPagesThatDoNotMatchTheirChecksum(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t, "s1")
	a := storedNode(t, clock, database(), brief)
	b := storedNode(t, clock, database(), brief)
	createTenant(t, a.Fleet, "alice")
	say(t, a.Fleet, "alice", "written whole")
	sleep(t, clock, a.Fleet)
	db, _ := a.vault.shards["s1"].use(ctx)
	damaged := make([]byte, vaultPage)
	if _, err := db.ExecContext(ctx, `UPDATE vault_pages SET data=? WHERE tenant_id=? AND path=? AND idx=1`, damaged, "alice", "assistant.db"); err != nil {
		t.Fatal(err)
	}
	err := b.Use(ctx, "alice", func(*App) error { return nil })
	if !errors.Is(err, errCorrupt) || !strings.Contains(err.Error(), "assistant.db") {
		t.Fatalf("a damaged page is noticed: %v", err)
	}
	if got := stats(t, b.Fleet); got.Corrupt != 1 || b.opened("alice") != nil || holder(t, b.Fleet, "alice") != "" {
		t.Fatalf("the tenant is not opened and not held: %+v", got)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM vault_pages WHERE tenant_id=? AND path=? AND idx=1`, "alice", "assistant.db"); err != nil {
		t.Fatal(err)
	}
	if err := b.Use(ctx, "alice", func(*App) error { return nil }); !errors.Is(err, errCorrupt) {
		t.Fatalf("a missing page is noticed: %v", err)
	}
}

// The nodes judge each other's heartbeats by the registry database's clock,
// so a node whose own clock is minutes off neither takes the others for dead
// nor is taken for dead.
func TestClusterJudgesHeartbeatsByTheRegistryClock(t *testing.T) {
	database := shardedDatabase(t)
	reference := newClock()
	ahead, behind := newClock(), newClock()
	ahead.Set(reference.Now().Add(7 * time.Minute))
	behind.Set(reference.Now().Add(-4 * time.Minute))
	shared := func(c *FleetConfig) {
		database()(c)
		c.registryClock = reference.Now
	}
	a := storedNode(t, behind, shared, nil)
	b := storedNode(t, ahead, shared, nil)
	createTenant(t, a.Fleet, "alice")
	touch(t, a.Fleet, "alice")
	advance := func(d time.Duration) {
		for _, clock := range []*fixedClock{reference, ahead, behind} {
			clock.Set(clock.Now().Add(d))
		}
	}
	for range 3 {
		advance(2 * time.Second)
		a.heartbeat(context.Background())
		b.heartbeat(context.Background())
	}
	if got := stats(t, b.Fleet); got.Takeovers != 0 || len(got.Nodes) != 2 || !got.Nodes[0].Alive || !got.Nodes[1].Alive {
		t.Fatalf("nodes whose clocks disagree see each other alive: %+v", got)
	}
	if holder(t, b.Fleet, "alice") != a.node || a.opened("alice") == nil {
		t.Fatal("and the tenant stays where it is")
	}
	// A node that really stops is still found out, by the same clock.
	a.crash(t)
	advance(a.config.NodeTTL + time.Second)
	b.heartbeat(context.Background())
	if got := stats(t, b.Fleet); got.Takeovers != 1 {
		t.Fatalf("a stopped node's tenants are taken over: %+v", got)
	}
}

// A node that cannot reach the registry keeps its tenants open, since the
// others cannot take them without the registry either. When the registry is
// back it keeps those nobody took and gives up the rest; if the registry
// stays away longer than RegistryGrace it closes them.
func TestClusterKeepsTenantsThroughARegistryOutage(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	database := shardedDatabase(t, "s1")
	a := storedNode(t, clock, database(), nil)
	b := storedNode(t, clock, database(), nil)
	createTenant(t, a.Fleet, "alice")
	createTenant(t, a.Fleet, "bob")
	say(t, a.Fleet, "alice", "before the outage")
	say(t, a.Fleet, "bob", "before the outage")
	checkpoint(t, a.Fleet, "alice")
	checkpoint(t, a.Fleet, "bob")

	a.registry.db.down.Store(true)
	for range 3 {
		clock.Set(clock.Now().Add(4 * time.Second))
		a.heartbeat(ctx)
		b.heartbeat(ctx)
	}
	if a.opened("alice") == nil || a.opened("bob") == nil {
		t.Fatal("the tenants stay open while the registry is away")
	}
	// An owner whose tenant is open is served; one whose token cannot be
	// checked is told to wait, not that the token is wrong.
	carol := createTenant(t, b.Fleet, "carol")
	if code, _ := tenantCall(a.handler, http.MethodGet, "/t/carol/api/state", carol, ""); code != http.StatusServiceUnavailable {
		t.Fatalf("a registry that is away is not a wrong token: %d", code)
	}
	// The tenants' shard still answers: what they do is stored.
	say(t, a.Fleet, "bob", "during the outage")
	if !checkpoint(t, a.Fleet, "bob") {
		t.Fatal("a checkpoint needs the shard, not the registry")
	}
	// Another node, which reaches the registry, has taken the node for dead
	// and opens one of its tenants.
	touch(t, b.Fleet, "alice")
	if holder(t, b.Fleet, "alice") != b.node || stats(t, b.Fleet).Takeovers != 2 {
		t.Fatalf("the other node took over: %+v", stats(t, b.Fleet))
	}

	a.registry.db.down.Store(false)
	a.heartbeat(ctx)
	deadline := time.Now().Add(10 * time.Second)
	for a.opened("alice") != nil {
		if time.Now().After(deadline) {
			t.Fatal("the tenant another node took is given up")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := stats(t, a.Fleet); got.Outages != 1 || got.Fences != 0 || a.opened("bob") == nil || holder(t, a.Fleet, "bob") != a.node || !record(t, a.Fleet, "bob").Active {
		t.Fatalf("the tenant nobody took is kept, and marked open again: %+v", got)
	}
	if !strings.Contains(channelText(t, a.Fleet, "bob"), "during the outage") || !strings.Contains(channelText(t, b.Fleet, "alice"), "before the outage") {
		t.Fatal("nothing was lost on either side")
	}

	// A registry that stays away longer than the grace is not waited for.
	a.registry.db.down.Store(true)
	clock.Set(clock.Now().Add(a.config.RegistryGrace + time.Second))
	a.heartbeat(ctx)
	if a.held() != 0 {
		t.Fatal("after the grace the node closes its tenants")
	}
	a.registry.db.down.Store(false)
	if got := stats(t, a.Fleet); got.Fences != 1 {
		t.Fatalf("it is counted: %+v", got)
	}
}

// The tables are built in numbered steps. A database from before shards is
// brought up to date in place with its tenants where they were; a database
// from a later version is refused; and shards cannot be turned on while a
// node from before them is running.
func TestFleetDatabaseIsUpgradedInPlace(t *testing.T) {
	ctx := context.Background()
	clock := newClock()
	database := shardedDatabase(t, "s1")
	var config FleetConfig
	database(registryOnly)(&config)

	// A database as the version before shards left it: the first step of
	// each set, one tenant with a file in the vault, and a running node.
	old, err := openFleetDB(ctx, databaseAt{url: config.Database, dir: config.registryDir}, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	engine := strings.NewReplacer("{rowid}", " WITHOUT ROWID", "{bytes}", "BLOB")
	if old.postgres {
		engine = strings.NewReplacer("{rowid}", "", "{bytes}", "BYTEA")
	}
	statements := []string{`DROP TABLE vault_heads`, `DROP TABLE vault_files`, `DROP TABLE vault_pages`, `DROP TABLE fleet_meta`}
	for _, statement := range append(vaultSteps[0], registrySteps[0]...) {
		statements = append(statements, engine.Replace(statement))
	}
	for _, statement := range statements {
		if _, err := old.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	born, note := millis(clock.Now()), []byte("written before shards\n")
	for _, statement := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO tenants(id,token_hash,state,created_at,activations) VALUES(?,?,?,?,1)`, []any{"alice", hashToken("token"), string(TenantWarm), born}},
		{`INSERT INTO vault_heads(tenant_id,seq,node,at,bytes) VALUES(?,3,'gone',?,?)`, []any{"alice", born, len(note)}},
		{`INSERT INTO vault_files(tenant_id,path,size,seq) VALUES(?,?,?,3)`, []any{"alice", "notes/old.txt", len(note)}},
		{`INSERT INTO vault_pages(tenant_id,path,idx,data) VALUES(?,?,0,?)`, []any{"alice", "notes/old.txt", note}},
		{`INSERT INTO nodes(id,addr,started,beat) VALUES('old','http://old',?,?)`, []any{born, born}},
	} {
		if _, err := old.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("%s: %v", statement.query, err)
		}
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	open := func(tune func(*FleetConfig)) (*Fleet, error) {
		config := FleetConfig{DataDir: t.TempDir(), Provider: &ScriptedModel{Now: clock.Now}, Model: ScriptedModelID, Now: clock.Now, Advertise: "http://new"}
		tune(&config)
		return OpenFleet(ctx, config)
	}
	if _, err := open(database()); err == nil || !strings.Contains(err.Error(), "1 running nodes") {
		t.Fatalf("shards wait until the nodes from before them have stopped: %v", err)
	}
	fleet, err := open(database(registryOnly))
	if err != nil {
		t.Fatalf("without shards the two versions run side by side: %v", err)
	}
	if got := record(t, fleet, "alice"); got.Shard != homeShard || shardInfo(t, fleet, homeShard).State != ShardOpen || shardInfo(t, fleet, homeShard).Tenants != 1 {
		t.Fatalf("the tenant's data is where it was, in the registry database: %+v", got)
	}
	// The upgrade itself says whose the stored data is: were the tenant
	// deleted before it is next opened, a later one of the same name must
	// not find it.
	var adopted int64
	if err := fleet.registry.db.QueryRowContext(ctx, `SELECT born FROM vault_heads WHERE tenant_id=?`, "alice").Scan(&adopted); err != nil || adopted != born {
		t.Fatalf("its head is stamped as this tenant's by the upgrade: %d %v", adopted, err)
	}
	touch(t, fleet, "alice")
	if text, err := os.ReadFile(filepath.Join(fleet.dir("alice"), "notes", "old.txt")); err != nil || string(text) != string(note) {
		t.Fatalf("and is read as it was written: %q %v", text, err)
	}
	var stamped int64
	var mark string
	if err := fleet.registry.db.QueryRowContext(ctx, `SELECT born,mark FROM vault_heads WHERE tenant_id=?`, "alice").Scan(&stamped, &mark); err != nil || stamped != born || mark == "" {
		t.Fatalf("its head names its checkpoint: %d %q %v", stamped, mark, err)
	}
	if err := fleet.Close(); err != nil {
		t.Fatal(err)
	}

	// Once the old node is gone, shards are turned on and the tenant stays
	// in the registry database until that is drained.
	clock.Set(clock.Now().Add(time.Minute))
	fleet, err = open(database())
	if err != nil {
		t.Fatal(err)
	}
	if got := shardInfo(t, fleet, "s1"); got.State != ShardClosed {
		t.Fatalf("the shard joins closed, as the fleet had one already: %+v", got)
	}
	if err := errors.Join(fleet.SetShard(ctx, "s1", ShardOpen), fleet.SetShard(ctx, homeShard, ShardDraining)); err != nil {
		t.Fatal(err)
	}
	fleet.drain(ctx)
	if shardOf(t, fleet, "alice") != "s1" || stored(t, fleet, homeShard, "alice") != 0 {
		t.Fatal("draining the registry database moves its tenants to the shard")
	}
	touch(t, fleet, "alice")
	if text, err := os.ReadFile(filepath.Join(fleet.dir("alice"), "notes", "old.txt")); err != nil || string(text) != string(note) {
		t.Fatalf("with their data: %q %v", text, err)
	}
	if _, err := fleet.registry.db.ExecContext(ctx, `UPDATE fleet_meta SET value=value+1 WHERE name='registry'`); err != nil {
		t.Fatal(err)
	}
	if err := fleet.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := open(database()); !errors.Is(err, errNewerSchema) {
		t.Fatalf("a database from a later version is refused: %v", err)
	}
}

// Old usage rows go after UsageRetention.
func TestFleetDropsOldUsage(t *testing.T) {
	clock := newClock()
	ctx := context.Background()
	fleet := openFleet(t, clock, func(c *FleetConfig) { c.UsageRetention = 30 * 24 * time.Hour })
	createTenant(t, fleet, "alice")
	old, recent := clock.Now().Add(-40*24*time.Hour).Format(slotLayout), clock.Now().Add(-10*24*time.Hour).Format(slotLayout)
	for _, day := range []string{old, recent} {
		if err := fleet.registry.addUsage(ctx, "alice", day, 1, 10, 5); err != nil {
			t.Fatal(err)
		}
	}
	fleet.janitor(ctx)
	gone, _ := fleet.registry.usage(ctx, "alice", old)
	kept, _ := fleet.registry.usage(ctx, "alice", recent)
	if gone.Calls != 0 || kept.Calls != 1 {
		t.Fatalf("only rows older than the retention go: %+v %+v", gone, kept)
	}
}

// /readyz says no once the node is about to stop, and /admin/metrics gives
// the counters in the Prometheus text format.
func TestFleetReadinessAndMetrics(t *testing.T) {
	clock := newClock()
	database := shardedDatabase(t, "s1")
	fleet := storedNode(t, clock, database(), nil)
	createTenant(t, fleet.Fleet, "alice")
	say(t, fleet.Fleet, "alice", "counted")
	checkpoint(t, fleet.Fleet, "alice")
	if code, _ := tenantCall(fleet.handler, http.MethodGet, "/readyz", "", ""); code != http.StatusOK {
		t.Fatalf("a running node is ready: %d", code)
	}
	code, body := tenantCall(fleet.handler, http.MethodGet, "/admin/metrics", clusterAdmin, "")
	for _, want := range []string{
		"miniloop_fleet_active_tenants 1\n", `miniloop_fleet_tenants{state="warm"} 1`, `miniloop_fleet_shard_tenants{shard="s1",state="open"} 1`,
		`miniloop_fleet_shard_down{shard="s1"} 0`, "miniloop_fleet_checkpoints_total 1\n", "# TYPE miniloop_fleet_checkpoints_total counter", "miniloop_fleet_nodes_alive 1\n",
	} {
		if code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("metrics lack %q: %d\n%s", want, code, body)
		}
	}
	if code, _ := tenantCall(fleet.handler, http.MethodGet, "/admin/metrics", "", ""); code != http.StatusUnauthorized {
		t.Fatalf("metrics need the admin token: %d", code)
	}
	fleet.Leave()
	if code, _ := tenantCall(fleet.handler, http.MethodGet, "/readyz", "", ""); code != http.StatusServiceUnavailable {
		t.Fatalf("a leaving node is not ready: %d", code)
	}
	if code, _ := tenantCall(fleet.handler, http.MethodGet, "/healthz", "", ""); code != http.StatusOK {
		t.Fatalf("but it is alive: %d", code)
	}
}

// Only errors that say a database could not be reached or could not serve
// count against a shard's health.
func TestDatabaseTroubleIsToldFromAnAnswer(t *testing.T) {
	for _, err := range []error{errUnreachable, context.DeadlineExceeded, fmt.Errorf("wrapped: %w", sql.ErrConnDone)} {
		if !dbTrouble(err) {
			t.Fatalf("%v is trouble", err)
		}
	}
	for _, err := range []error{nil, errFenced, errCorrupt, context.Canceled, sql.ErrNoRows, &os.PathError{Op: "write", Path: "x", Err: context.DeadlineExceeded}} {
		if dbTrouble(err) {
			t.Fatalf("%v is an answer, not trouble", err)
		}
	}
	// A statement whose caller set no deadline gets one, so a database that
	// never answers does not hold the caller for ever; a caller's own stands.
	if at, set := timely(context.Background()).Deadline(); !set || time.Until(at) > statementLimit || time.Until(at) < statementLimit-time.Second {
		t.Fatalf("no default deadline: %v %t", at, set)
	}
	own, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	if at, _ := timely(own).Deadline(); time.Until(at) < 59*time.Minute {
		t.Fatalf("the caller's deadline is kept: %v", at)
	}
	for _, address := range []string{
		"postgres://u:p@db:5432/fleet", "postgresql://u:p@a:5432,b:5433/fleet?target_session_attrs=read-write&sslmode=require",
	} {
		if err := checkDatabaseURL(address); err != nil {
			t.Fatalf("%s: %v", strings.Split(address, "@")[1], err)
		}
	}
	for _, address := range []string{"", "host=db user=u", "mysql://u:p@db/fleet", "postgres:///fleet", "postgres://u:secret-word@db:port/fleet"} {
		if err := checkDatabaseURL(address); err == nil || strings.Contains(err.Error(), "secret-word") {
			t.Fatalf("%q: %v", address, err)
		}
	}
}

// A tenant that is closed while its shard is away keeps the changes that
// could not be stored in the node's copy. If the same node opens it next it
// goes on from there; if another node wrote it meanwhile, that node's state
// is the tenant's and the copy is replaced.
func TestVaultKeepsChangesItCouldNotStoreAtClose(t *testing.T) {
	clock := newClock()
	database := shardedDatabase(t, "s1")
	a := storedNode(t, clock, database(), brief)
	b := storedNode(t, clock, database(), brief)
	createTenant(t, a.Fleet, "alice")
	say(t, a.Fleet, "alice", "stored in time")
	checkpoint(t, a.Fleet, "alice")
	away := func(text string) {
		t.Helper()
		say(t, a.Fleet, "alice", text)
		s1 := a.vault.shards["s1"]
		s1.db.down.Store(true)
		sleep(t, clock, a.Fleet)
		s1.db.down.Store(false)
		if head, ok := readHead(a.dir("alice")); !ok || head.Clean || !head.Ahead {
			t.Fatalf("the copy is marked as ahead of the vault: %+v", head)
		}
	}
	away("written while the shard was away")
	if text := channelText(t, a.Fleet, "alice"); !strings.Contains(text, "written while the shard was away") {
		t.Fatalf("the node goes on from its own copy: %q", text)
	}
	if got := stats(t, a.Fleet); got.CacheReuses != 1 || got.VaultPulls != 0 || !checkpoint(t, a.Fleet, "alice") {
		t.Fatalf("and stores what the vault lacked: %+v", got)
	}
	sleep(t, clock, a.Fleet)
	if text := channelText(t, b.Fleet, "alice"); !strings.Contains(text, "written while the shard was away") {
		t.Fatalf("which every node then has: %q", text)
	}
	sleep(t, clock, b.Fleet)

	away("never stored")
	say(t, b.Fleet, "alice", "written on the other node")
	sleep(t, clock, b.Fleet)
	pulls := stats(t, a.Fleet).VaultPulls
	text := channelText(t, a.Fleet, "alice")
	if !strings.Contains(text, "written on the other node") || strings.Contains(text, "never stored") {
		t.Fatalf("a copy another node wrote past is replaced: %q", text)
	}
	if got := stats(t, a.Fleet); got.VaultPulls != pulls+1 {
		t.Fatalf("it is read from the vault: %+v", got)
	}
}

// Nodes that start at the same moment all start. Each of them clears the
// nodes that stopped before, and PostgreSQL lets one transaction in the
// snapshot isolation remove a row: the others are told their view is out of
// date. A node that is told so leaves the clearing to the one that did it.
func TestNodesThatStartTogetherAllStart(t *testing.T) {
	clock := newClock()
	database := shardedDatabase(t)
	nodes := []*testNode{}
	for range 6 {
		nodes = append(nodes, storedNode(t, clock, database(), brief))
	}
	createTenant(t, nodes[0].Fleet, "alice")
	say(t, nodes[0].Fleet, "alice", "kept through the restarts")
	sleep(t, clock, nodes[0].Fleet)
	ctx := context.Background()
	for round := range 5 {
		// The nodes of an earlier start stopped without saying so.
		for dead := range 4 {
			gone := clock.Now().Add(-time.Hour)
			if err := nodes[0].registry.beat(ctx, fmt.Sprintf("gone-%d-%d", round, dead), "", gone, gone, 0, 0); err != nil {
				t.Fatal(err)
			}
		}
		failed := make([]error, len(nodes))
		var together sync.WaitGroup
		for i, node := range nodes {
			together.Add(1)
			go func() {
				defer together.Done()
				failed[i] = node.join(ctx)
			}()
		}
		together.Wait()
		if err := errors.Join(failed...); err != nil {
			t.Fatalf("round %d: a node that starts beside another starts: %v", round, err)
		}
		if stale, err := nodes[0].registry.stale(ctx, nodes[0].alive()); err != nil || stale {
			t.Fatalf("round %d: the nodes that stopped are cleared by one of them: %v %v", round, stale, err)
		}
	}
	if text := channelText(t, nodes[5].Fleet, "alice"); !strings.Contains(text, "kept through the restarts") {
		t.Fatalf("and the fleet serves: %q", text)
	}
}
