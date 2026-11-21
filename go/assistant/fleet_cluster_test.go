package assistant

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	clusterAdmin = "admin-token-for-the-cluster-test"
	clusterPush  = "push-token-for-the-cluster-tests"
)

// testNode is one fleet process of a cluster, with the listener the other
// nodes reach it on. Tests call its handler directly, as a load balancer
// would, and the nodes reach each other over real HTTP.
type testNode struct {
	*Fleet
	handler http.Handler
	server  *httptest.Server
}

func openNode(t *testing.T, clock *fixedClock, dir string, tune func(*FleetConfig)) *testNode {
	t.Helper()
	var handler atomic.Pointer[http.Handler]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		(*handler.Load()).ServeHTTP(w, r)
	}))
	fleet := openFleet(t, clock, func(c *FleetConfig) {
		c.DataDir, c.Advertise = dir, server.URL
		if tune != nil {
			tune(c)
		}
	})
	served := fleet.Handler(clusterAdmin, clusterPush)
	handler.Store(&served)
	t.Cleanup(server.Close)
	return &testNode{Fleet: fleet, handler: served, server: server}
}

// crash stops a node the way a killed process stops: its listener and its
// tenants' files are released and nothing is written to the registry.
func (n *testNode) crash(t *testing.T) {
	t.Helper()
	n.server.Close()
	n.mu.Lock()
	n.closed = true
	open := n.tenants
	n.tenants = map[TenantID]*tenant{}
	n.mu.Unlock()
	n.stop()
	n.wg.Wait()
	for _, tenant := range open {
		tenant.cancel()
		tenant.app.Wait()
		if err := errors.Join(tenant.app.Close(context.Background()), tenant.extra()); err != nil {
			t.Fatal(err)
		}
		if tenant.replica != nil {
			// Nothing more reaches the vault, and the copy stays unmarked.
			<-tenant.synced
			if err := tenant.replica.closeSources(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := errors.Join(n.registry.Close(), n.unlock()); err != nil {
		t.Fatal(err)
	}
}

func holder(t *testing.T, fleet *Fleet, id TenantID) string {
	t.Helper()
	node, _, _, err := fleet.registry.holder(context.Background(), id, fleet.alive())
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func stats(t *testing.T, fleet *Fleet) FleetStats {
	t.Helper()
	got, err := fleet.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// Any node answers for any tenant: a request, a push or a delete that
// reaches a node which does not hold the tenant is passed on to the one that
// does, and the tenant stays open in one place.
func TestClusterPassesRequestsToTheHolder(t *testing.T) {
	clock := newClock()
	dir := t.TempDir()
	brief := func(c *FleetConfig) { c.IdleTTL = time.Second }
	a := openNode(t, clock, dir, brief)
	b := openNode(t, clock, dir, brief)
	alice := createTenant(t, b.Fleet, "alice")

	if code, body := tenantCall(a.handler, http.MethodGet, "/t/alice/api/state", alice, ""); code != http.StatusOK {
		t.Fatalf("the first node opens it: %d %s", code, body)
	}
	if a.opened("alice") == nil || holder(t, b.Fleet, "alice") != a.node {
		t.Fatal("the registry places the tenant on the node that opened it")
	}
	code, body := tenantCall(b.handler, http.MethodGet, "/t/alice/api/state", alice, "")
	if code != http.StatusOK || !strings.Contains(body, `"channel"`) {
		t.Fatalf("the other node passes the request on: %d %s", code, body)
	}
	if code, _ := tenantCall(b.handler, http.MethodGet, "/t/alice/api/state", "wrong", ""); code != http.StatusUnauthorized {
		t.Fatalf("a wrong token is refused before anything is passed on: %d", code)
	}
	if b.opened("alice") != nil || stats(t, b.Fleet).Forwards != 1 {
		t.Fatalf("it is not opened twice: %+v", stats(t, b.Fleet))
	}
	// For a moment the node remembers where the tenant is and whose token it
	// checked, and passes the owner's next requests on without the registry.
	b.registry.db.down.Store(true)
	code, body = tenantCall(b.handler, http.MethodGet, "/t/alice/api/state", alice, "")
	b.registry.db.down.Store(false)
	if got := stats(t, b.Fleet); code != http.StatusOK || !strings.Contains(body, `"channel"`) || got.RouteHits != 1 || got.Forwards != 2 {
		t.Fatalf("a remembered route needs no registry: %d %+v", code, got)
	}
	if code, body := tenantCall(b.handler, http.MethodPost, "/push/alice", clusterPush, "{}"); code != http.StatusAccepted || !strings.Contains(body, string(PushPolled)) {
		t.Fatalf("a push reaches the node that holds the tenant: %d %s", code, body)
	}
	if got := stats(t, a.Fleet); got.Pushes != 1 || len(got.Nodes) != 2 || !got.Nodes[0].Alive || !got.Nodes[1].Alive {
		t.Fatalf("stats list both nodes: %+v", got)
	}

	// Once it sleeps and the route is forgotten, the node the next request
	// reaches opens it.
	clock.Set(clock.Now().Add(routeTTL + time.Second))
	a.reap(context.Background())
	if a.opened("alice") != nil || holder(t, a.Fleet, "alice") != "" {
		t.Fatal("a sleeping tenant is placed nowhere")
	}
	if code, _ := tenantCall(b.handler, http.MethodGet, "/t/alice/api/state", alice, ""); code != http.StatusOK || b.opened("alice") == nil {
		t.Fatalf("the second node opens a sleeping tenant itself: %d", code)
	}

	if code, body := tenantCall(a.handler, http.MethodDelete, "/admin/tenants/alice", clusterAdmin, ""); code != http.StatusNoContent {
		t.Fatalf("a delete is passed on to the holder: %d %s", code, body)
	}
	if _, _, err := a.Tenant(context.Background(), "alice"); !errors.Is(err, ErrTenantUnknown) || b.opened("alice") != nil || exists(b.dir("alice")) {
		t.Fatalf("the tenant and its files are gone: %v", err)
	}
}

// When a node stops without closing its tenants, the others make them due
// and one of them runs them. Until its heartbeat is too old the dead node
// still holds them, and requests for them are told to retry.
func TestClusterTakesOverAStoppedNode(t *testing.T) {
	clock := newClock()
	dir := t.TempDir()
	a := openNode(t, clock, dir, nil)
	b := openNode(t, clock, dir, nil)
	alice := createTenant(t, a.Fleet, "alice")
	createTenant(t, a.Fleet, "bob")
	touch(t, a.Fleet, "alice")
	a.crash(t)

	if got := record(t, b.Fleet, "alice"); !got.Active || holder(t, b.Fleet, "alice") != a.node {
		t.Fatalf("a crash leaves the tenant marked open on the dead node: %+v", got)
	}
	if code, _ := tenantCall(b.handler, http.MethodGet, "/t/alice/api/state", alice, ""); code != http.StatusServiceUnavailable {
		t.Fatalf("before the heartbeat is too old the request cannot be passed on: %d", code)
	}
	b.heartbeat(context.Background())
	if got := record(t, b.Fleet, "alice"); !got.Active {
		t.Fatalf("a fresh heartbeat is not given up: %+v", got)
	}

	clock.Set(clock.Now().Add(b.config.NodeTTL + time.Second))
	b.heartbeat(context.Background())
	if got := record(t, b.Fleet, "alice"); got.Active || !wakeAt(t, got).Equal(clock.Now()) {
		t.Fatalf("the dead node's tenant is due now: %+v", got)
	}
	if got := record(t, b.Fleet, "bob"); got.NextWake != nil {
		t.Fatalf("a tenant that was not open stays asleep: %+v", got)
	}
	b.wakeDue(context.Background())
	if b.opened("alice") == nil || b.opened("bob") != nil || holder(t, b.Fleet, "alice") != b.node {
		t.Fatal("the surviving node runs the dead node's tenant")
	}
	if got := stats(t, b.Fleet); got.Takeovers != 1 || len(got.Nodes) != 1 || got.Nodes[0].ID != b.node {
		t.Fatalf("the dead node left the registry: %+v", got)
	}
	if code, _ := tenantCall(b.handler, http.MethodGet, "/t/alice/api/state", alice, ""); code != http.StatusOK {
		t.Fatalf("the owner is served again: %d", code)
	}
}

// A node that was taken for dead while it was only stalled still has its
// tenants' files locked, so no other node can run them. When it beats again
// it closes them, and then another node may.
func TestClusterNodeTakenForDeadClosesItsTenants(t *testing.T) {
	clock := newClock()
	dir := t.TempDir()
	a := openNode(t, clock, dir, nil)
	b := openNode(t, clock, dir, nil)
	createTenant(t, a.Fleet, "alice")
	createTenant(t, a.Fleet, "bob")
	touch(t, a.Fleet, "alice")
	touch(t, a.Fleet, "bob")

	clock.Set(clock.Now().Add(a.config.NodeTTL + time.Second))
	b.heartbeat(context.Background())
	// A delete does not wait for a node that looks dead.
	if err := b.DeleteTenant(context.Background(), "bob"); err != nil {
		t.Fatal(err)
	}
	b.wakeDue(context.Background())
	if b.opened("alice") != nil || a.opened("alice") == nil {
		t.Fatal("the files' lock keeps the second node out while the first still runs the tenant")
	}
	if got := stats(t, b.Fleet); got.Takeovers != 2 || got.WakeFailures != 1 {
		t.Fatalf("the takeover failed at the lock: %+v", got)
	}
	if holder(t, b.Fleet, "alice") != "" {
		t.Fatal("a failed open gives its placement up")
	}

	a.heartbeat(context.Background())
	if a.opened("alice") != nil || stats(t, a.Fleet).Fences != 1 {
		t.Fatal("the stalled node closes its tenants once it sees how long it was silent")
	}
	if exists(a.dir("bob")) {
		t.Fatal("the stalled node's late close leaves nothing of a deleted tenant")
	}
	if got := record(t, a.Fleet, "alice"); got.Active || got.NextWake == nil {
		t.Fatalf("its late close does not overwrite the row the others own: %+v", got)
	}
	clock.Set(clock.Now().Add(failedRetry))
	b.wakeDue(context.Background())
	if b.opened("alice") == nil {
		t.Fatal("the second node runs the tenant once the lock is free")
	}
}

// A node with no room passes an owner's request to a node that has some.
func TestClusterOverflowsToANodeWithRoom(t *testing.T) {
	clock := newClock()
	dir := t.TempDir()
	a := openNode(t, clock, dir, func(c *FleetConfig) { c.MaxActive = 1 })
	b := openNode(t, clock, dir, func(c *FleetConfig) { c.MaxActive = 4 })
	createTenant(t, a.Fleet, "alice")
	bob := createTenant(t, a.Fleet, "bob")
	busy, err := a.acquire(context.Background(), "alice", true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.release(busy)

	if code, body := tenantCall(a.handler, http.MethodGet, "/t/bob/api/state", bob, ""); code != http.StatusOK {
		t.Fatalf("the full node passes the request on: %d %s", code, body)
	}
	if a.opened("bob") != nil || b.opened("bob") == nil || stats(t, a.Fleet).Forwards != 1 {
		t.Fatal("the node with room opens the tenant")
	}
	// A request that was already passed on is not passed on again.
	request := httptest.NewRequest(http.MethodGet, "/t/bob/api/state", nil)
	request.Header.Set("Authorization", "Bearer "+bob)
	request.Header.Set(hopHeader, "elsewhere")
	recorder := httptest.NewRecorder()
	a.handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable || recorder.Header().Get("Retry-After") == "" {
		t.Fatalf("a passed-on request stops at the full node: %d", recorder.Code)
	}
}

// Every node's waker reads the same due list, and each due tenant is opened
// by exactly one of them.
func TestClusterWakesEachDueTenantOnce(t *testing.T) {
	clock := newClock()
	dir := t.TempDir()
	a := openNode(t, clock, dir, nil)
	b := openNode(t, clock, dir, nil)
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

// A fleet that holds its directory alone and the nodes of a cluster keep
// each other out, and a node that closes leaves the registry.
func TestClusterAndSingleFleetExcludeEachOther(t *testing.T) {
	clock := newClock()
	dir := t.TempDir()
	alone := FleetConfig{DataDir: dir, Provider: &ScriptedModel{}}
	node := openNode(t, clock, dir, nil)
	if _, err := OpenFleet(context.Background(), alone); !errors.Is(err, ErrDataDirBusy) {
		t.Fatalf("a single fleet beside a cluster node: %v", err)
	}
	second := openNode(t, clock, dir, nil)
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if got := stats(t, node.Fleet); len(got.Nodes) != 1 || got.Nodes[0].ID != node.node || got.Nodes[0].Addr != node.server.URL {
		t.Fatalf("a closed node leaves the registry: %+v", got.Nodes)
	}
	if err := node.Close(); err != nil {
		t.Fatal(err)
	}
	single := openFleet(t, clock, func(c *FleetConfig) { c.DataDir = dir })
	clustered := alone
	clustered.Advertise = "http://127.0.0.1:1"
	if _, err := OpenFleet(context.Background(), clustered); !errors.Is(err, ErrDataDirBusy) {
		t.Fatalf("a cluster node beside a single fleet: %v", err)
	}
	if got := stats(t, single); len(got.Nodes) != 1 || got.Nodes[0].Addr != "" {
		t.Fatalf("a single fleet is its own only node: %+v", got.Nodes)
	}
	for _, address := range []string{"10.0.0.3:8791", "ftp://10.0.0.3", "http://user@10.0.0.3", "http://10.0.0.3/base", "http://10.0.0.3?x=1"} {
		config := alone
		config.DataDir, config.Advertise = t.TempDir(), address
		if _, err := OpenFleet(context.Background(), config); err == nil || errors.Is(err, ErrDataDirBusy) {
			t.Errorf("advertised address %q: %v", address, err)
		}
	}
}
