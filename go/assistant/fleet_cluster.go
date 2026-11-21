package assistant

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// Several fleet processes can serve one data directory. Every node is the
// same: it takes any request, opens any tenant and runs the same loops, so
// capacity grows by starting another node behind the load balancer. A tenant
// is still open in one place at a time. The registry says where:
//
//   - nodes: one row per running node, with a heartbeat. A node whose
//     heartbeat is older than NodeTTL is dead.
//   - placements: the node that holds a tenant. A node writes its row before
//     it opens the tenant and removes it in the transaction that records the
//     tenant's sleep. A row of a dead node holds nothing.
//
// A request for a tenant that another live node holds is passed on to that
// node, and a node with no room passes an owner's request to one that has
// some. When a node dies its tenants are made due at once and whichever node
// wakes them first runs them; their turns were not settled, so their triggers
// are requeued the way a restart requeues them.
//
// The registry is a file the nodes share, and so are the tenants' directories.
// The nodes therefore need one filesystem with working file locks: the same
// host, or a cluster filesystem that honours flock. An App locks its tenant's
// directory while it is open, which is what stops two nodes from running one
// tenant even when the heartbeats are wrong about who is alive.

// hopHeader marks a request another node passed on. It is never passed on
// again, so two nodes that disagree cannot bounce a request between them.
const hopHeader = "X-Miniloop-Fleet-Hop"

// elsewhere is why a tenant could not be opened here: a live node holds it.
type elsewhere struct{ node, addr string }

func (e *elsewhere) Error() string { return "tenant is open on node " + e.node }

// errMoving means a tenant's holder changed while it was being looked up.
var errMoving = errors.New("tenant is moving between nodes")

// NodeInfo is one node's row in the registry.
type NodeInfo struct {
	ID        string    `json:"id"`
	Addr      string    `json:"addr,omitempty"`
	Started   time.Time `json:"started"`
	Beat      time.Time `json:"beat"`
	Open      int       `json:"open"`
	MaxActive int       `json:"max_active"`
	Alive     bool      `json:"alive"`
}

// ---- registry ----

func (r *registry) beat(ctx context.Context, node, addr string, started, now time.Time, open, maxActive int) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO nodes(id,addr,started,beat,open,max_active) VALUES(?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET addr=excluded.addr, beat=excluded.beat, open=excluded.open, max_active=excluded.max_active`,
		node, addr, millis(started), millis(now), open, maxActive)
	return err
}

// leave removes a node that closed cleanly, and with it any other node's
// row when others is set: a single-node fleet holds the directory alone.
func (r *registry) leave(ctx context.Context, node string, others bool) error {
	query := `DELETE FROM nodes WHERE id=?`
	if others {
		query = `DELETE FROM nodes WHERE id<>?`
	}
	_, err := r.db.ExecContext(ctx, query, node)
	return err
}

// claim places a tenant on a node. It succeeds when the tenant exists and no
// other live node holds it.
func (r *registry) claim(ctx context.Context, id TenantID, node string, now, alive time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `INSERT INTO placements(tenant_id,node,since)
		SELECT ?,?,? WHERE EXISTS (SELECT 1 FROM tenants WHERE id=?)
		ON CONFLICT(tenant_id) DO UPDATE SET node=excluded.node, since=excluded.since
		WHERE placements.node=excluded.node OR NOT EXISTS (SELECT 1 FROM nodes WHERE id=placements.node AND beat>=?)`,
		string(id), node, millis(now), string(id), millis(alive))
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

// unplace gives up a placement without recording a sleep, for a tenant that
// failed to open or was only held while it was archived.
func (r *registry) unplace(ctx context.Context, id TenantID, node string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM placements WHERE tenant_id=? AND node=?`, string(id), node)
	return err
}

// holder is the live node that holds a tenant.
func (r *registry) holder(ctx context.Context, id TenantID, alive time.Time) (node, addr string, ok bool, err error) {
	err = r.db.QueryRowContext(ctx, `SELECT n.id,n.addr FROM placements p JOIN nodes n ON n.id=p.node
		WHERE p.tenant_id=? AND n.beat>=?`, string(id), millis(alive)).Scan(&node, &addr)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	return node, addr, err == nil, err
}

// spare is the address of the live node, other than node, with the most room.
func (r *registry) spare(ctx context.Context, node string, alive time.Time) (string, bool, error) {
	var addr string
	err := r.db.QueryRowContext(ctx, `SELECT addr FROM nodes WHERE id<>? AND beat>=? AND addr<>'' AND open<max_active
		ORDER BY max_active-open DESC, id LIMIT 1`, node, millis(alive)).Scan(&addr)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return addr, err == nil, err
}

func (r *registry) stale(ctx context.Context, alive time.Time) (bool, error) {
	var found bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM nodes WHERE beat<?)`, millis(alive)).Scan(&found)
	return found, err
}

// recoverOrphans makes every tenant that no live node holds, but that is
// marked open, due now: its node crashed or was killed, and the tenant's
// loops requeue what that interrupted. Dead nodes and their placements go.
// A placement is written before a tenant is marked open and removed with the
// mark, so an open tenant without a live placement is never a running one.
func (r *registry) recoverOrphans(ctx context.Context, now, alive time.Time) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE tenants SET active=0, next_wake=? WHERE active=1 AND id NOT IN (
		SELECT p.tenant_id FROM placements p JOIN nodes n ON n.id=p.node WHERE n.beat>=?)`, millis(now), millis(alive))
	if err != nil {
		return 0, errors.Join(err, tx.Rollback())
	}
	for _, statement := range []string{
		`DELETE FROM placements WHERE node NOT IN (SELECT id FROM nodes WHERE beat>=?)`,
		`DELETE FROM nodes WHERE beat<?`,
	} {
		if _, err := tx.ExecContext(ctx, statement, millis(alive)); err != nil {
			return 0, errors.Join(err, tx.Rollback())
		}
	}
	recovered, err := result.RowsAffected()
	if err != nil {
		return 0, errors.Join(err, tx.Rollback())
	}
	return int(recovered), tx.Commit()
}

func (r *registry) nodes(ctx context.Context, alive time.Time) ([]NodeInfo, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,addr,started,beat,open,max_active FROM nodes ORDER BY started,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	nodes := []NodeInfo{}
	for rows.Next() {
		var node NodeInfo
		var started, beat int64
		if err := rows.Scan(&node.ID, &node.Addr, &started, &beat, &node.Open, &node.MaxActive); err != nil {
			return nil, err
		}
		node.Started, node.Beat, node.Alive = fromMillis(started), fromMillis(beat), beat >= millis(alive)
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

// ---- node ----

// clustered reports whether other nodes may share the data directory.
func (f *Fleet) clustered() bool { return f.config.Advertise != "" }

// alive is the oldest heartbeat of a live node.
func (f *Fleet) alive() time.Time { return f.now().Add(-f.config.NodeTTL) }

func checkAdvertise(address string) error {
	parsed, err := url.Parse(address)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return fmt.Errorf("the advertised address %q must be a base URL such as http://10.0.0.3:8791", address)
	}
	return nil
}

func nodeTransport() http.RoundTripper {
	return &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout: 5 * time.Second,
		MaxIdleConnsPerHost: 32,
		IdleConnTimeout:     90 * time.Second,
	}
}

// heartbeat tells the other nodes this one is alive and takes over what dead
// nodes held. A node that could not beat for longer than NodeTTL may have
// been taken for dead itself, so it closes its tenants; any the others did
// not take are simply due again.
func (f *Fleet) heartbeat(ctx context.Context) {
	now := f.now()
	f.mu.Lock()
	open := len(f.tenants)
	f.mu.Unlock()
	err := f.registry.beat(ctx, f.node, f.config.Advertise, f.started, now, open, f.config.MaxActive)
	f.beatMu.Lock()
	gap := now.Sub(f.lastBeat)
	if err == nil {
		f.lastBeat = now
	}
	f.beatMu.Unlock()
	if err != nil && ctx.Err() == nil {
		fmt.Fprintf(f.log, "fleet: heartbeat: %v\n", err)
	}
	if gap > f.config.NodeTTL {
		if fenced := f.fence(); fenced > 0 {
			f.counters.fences.Add(1)
			fmt.Fprintf(f.log, "fleet: no heartbeat for %v; closed %d tenants the other nodes may have taken\n", gap.Round(time.Second), fenced)
		}
	}
	if err != nil {
		return
	}
	stale, err := f.registry.stale(ctx, f.alive())
	if err != nil || !stale {
		return
	}
	f.takeOver(ctx)
}

// takeOver makes the tenants of dead nodes due and wakes the waker.
func (f *Fleet) takeOver(ctx context.Context) {
	recovered, err := f.registry.recoverOrphans(ctx, f.now(), f.alive())
	if err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(f.log, "fleet: take over dead nodes: %v\n", err)
		}
		return
	}
	if recovered == 0 {
		return
	}
	f.counters.takeovers.Add(int64(recovered))
	fmt.Fprintf(f.log, "fleet: %d tenants were open on a node that stopped; waking them\n", recovered)
	select {
	case f.kick <- struct{}{}:
	default:
	}
}

// fence closes every open tenant and reports how many.
func (f *Fleet) fence() int {
	f.mu.Lock()
	open := []*tenant{}
	for _, t := range f.tenants {
		if t.app != nil && !t.closing {
			t.closing = true
			open = append(open, t)
		}
	}
	f.mu.Unlock()
	parallel(open, fleetParallel, func(t *tenant) { f.hibernate(t, time.Time{}) })
	return len(open)
}

// place claims a tenant for this node, or says which node holds it.
func (f *Fleet) place(ctx context.Context, id TenantID) error {
	claimed, err := f.registry.claim(ctx, id, f.node, f.now(), f.alive())
	if err != nil || claimed {
		return err
	}
	node, addr, held, err := f.registry.holder(ctx, id, f.alive())
	switch {
	case err != nil:
		return err
	case held && node != f.node:
		return &elsewhere{node: node, addr: addr}
	}
	if _, known, err := f.registry.get(ctx, id); err != nil || !known {
		return errors.Join(err, ErrTenantUnknown)
	}
	return errMoving
}

// Nodes lists the nodes in the registry, oldest first. A fleet that holds its
// directory alone does not beat, and is alive because it answers.
func (f *Fleet) Nodes(ctx context.Context) ([]NodeInfo, error) {
	nodes, err := f.registry.nodes(ctx, f.alive())
	for i := range nodes {
		if nodes[i].ID == f.node {
			nodes[i].Alive = true
		}
	}
	return nodes, err
}

// ---- passing requests on ----

// forward sends the request to another node and copies its answer back. The
// owner's token travels with it and the other node checks it again, so nodes
// need no credential of their own.
func (f *Fleet) forward(w http.ResponseWriter, r *http.Request, addr string) {
	target, err := url.Parse(addr)
	if err != nil || addr == "" || !f.clustered() || r.Header.Get(hopHeader) != "" {
		busy(w, 1)
		return
	}
	f.counters.forwards.Add(1)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(target)
			p.Out.Host = p.In.Host
			p.Out.Header.Set(hopHeader, f.node)
		},
		Transport: f.transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if r.Context().Err() == nil {
				fmt.Fprintf(f.log, "fleet: pass %s on to %s: %v\n", r.URL.Path, addr, err)
			}
			busy(w, 1)
		},
	}
	proxy.ServeHTTP(w, r)
}

// overflow finds where an owner's request can go when this node is full: the
// node that holds the tenant, or else the one with the most room.
func (f *Fleet) overflow(ctx context.Context, id TenantID) (string, bool) {
	if !f.clustered() {
		return "", false
	}
	if node, addr, held, err := f.registry.holder(ctx, id, f.alive()); err == nil && held && node != f.node {
		return addr, true
	}
	addr, ok, err := f.registry.spare(ctx, f.node, f.alive())
	return addr, ok && err == nil
}

func busy(w http.ResponseWriter, after int) {
	w.Header().Set("Retry-After", fmt.Sprint(after))
	writeJSON(w, http.StatusServiceUnavailable, errorResponse{"the service is busy; try again shortly"})
}
