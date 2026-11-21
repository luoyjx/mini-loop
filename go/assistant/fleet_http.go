package assistant

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Each owner may make tenantRate requests per second with bursts of
// tenantBurst; the page polls about once a second while it is visible.
const (
	tenantRate  = 20
	tenantBurst = 40
)

// Handler serves the fleet. Owners use /t/{id}/ with the bearer token
// CreateTenant returned; the page itself carries no tenant data and is served
// without it, and reads the token from the link's fragment. Operators use
// /admin/ with adminToken; an empty adminToken disables the admin API. The
// mail and calendar provider's push bridge uses POST /push/{id} with
// pushToken, which can only wake tenants; an empty pushToken disables it.
// /healthz answers without a credential while the process runs, and /readyz
// until the node is about to stop, which is what a load balancer should ask.
//
// Unlike a daemon's Handler it does not require a loopback host: a fleet is
// meant to be reached over the network, behind TLS.
func (f *Fleet) Handler(adminToken, pushToken string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		// Only the node's own state: a registry or a shard that is away is
		// away for every node, and a fleet with no node ready serves nobody,
		// not even the tenants that are open.
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if f.leaving.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("leaving\n"))
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /t/{tenant}", func(w http.ResponseWriter, r *http.Request) {
		if !TenantID(r.PathValue("tenant")).Valid() {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/t/"+r.PathValue("tenant")+"/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /t/{tenant}/{$}", func(w http.ResponseWriter, r *http.Request) {
		if !TenantID(r.PathValue("tenant")).Valid() {
			http.NotFound(w, r)
			return
		}
		serveIndex(w, r)
	})
	mux.HandleFunc("/t/{tenant}/api/", f.serveTenant)
	if adminToken != "" {
		admin := hashToken(adminToken)
		handle := func(pattern string, handler http.HandlerFunc) {
			mux.Handle(pattern, requireAdmin(admin, handler))
		}
		handle("POST /admin/tenants", f.handleCreateTenant)
		handle("GET /admin/tenants", f.handleListTenants)
		handle("GET /admin/tenants/{tenant}", f.handleGetTenant)
		handle("DELETE /admin/tenants/{tenant}", f.handleDeleteTenant)
		handle("GET /admin/stats", f.handleStats)
		handle("GET /admin/metrics", f.handleMetrics)
		handle("GET /admin/shards", f.handleListShards)
		handle("POST /admin/shards/{shard}", f.handleSetShard)
	}
	if pushToken != "" {
		mux.Handle("POST /push/{tenant}", requireToken(hashToken(pushToken), "miniloop-push", "push token required", http.HandlerFunc(f.handlePush)))
	}
	return mux
}

// serveTenant authenticates the owner, opens the assistant if it sleeps and
// hands the request to the tenant's own API.
func (f *Fleet) serveTenant(w http.ResponseWriter, r *http.Request) {
	id := TenantID(r.PathValue("tenant"))
	if addr, ok := f.routed(r, id); ok {
		f.forward(w, r, addr)
		return
	}
	known, err := f.authenticate(r.Context(), id, bearer(r))
	if err != nil && r.Context().Err() == nil {
		// The registry could not be asked. That is not the owner's token
		// being wrong, and must not look like it.
		busy(w, 2)
		return
	}
	if !known {
		w.Header().Set("WWW-Authenticate", `Bearer realm="miniloop"`)
		writeJSON(w, http.StatusUnauthorized, errorResponse{"a valid tenant token is required"})
		return
	}
	t, err := f.acquire(r.Context(), id, true)
	var away *elsewhere
	switch {
	case err == nil:
	case errors.As(err, &away):
		// The token was just checked against the registry.
		f.routes.put(id, away.addr, hashToken(bearer(r)), f.now())
		f.forward(w, r, away.addr)
		return
	case errors.Is(err, errMoving), errors.Is(err, errShardDown), errors.Is(err, errNoShard):
		busy(w, 1)
		return
	case errors.Is(err, ErrFleetFull):
		// Another node may hold it or have room; the owner should not have
		// to know which node the load balancer picked.
		if addr, ok := f.overflow(r.Context(), id); ok && r.Header.Get(hopHeader) == "" {
			f.forward(w, r, addr)
			return
		}
		busy(w, 5)
		return
	case errors.Is(err, ErrFleetClosed):
		busy(w, 5)
		return
	case errors.Is(err, ErrTenantUnknown):
		writeJSON(w, http.StatusUnauthorized, errorResponse{"a valid tenant token is required"})
		return
	case r.Context().Err() != nil:
		return
	case dbTrouble(err):
		// A database that is away is back in a moment or is being failed
		// over; the owner's client should try again, not give up.
		fmt.Fprintf(f.log, "fleet: open %s: %v\n", id, err)
		busy(w, 2)
		return
	default:
		fmt.Fprintf(f.log, "fleet: open %s: %v\n", id, err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{"the assistant could not be opened"})
		return
	}
	defer f.release(t)
	if !t.limiter.allow(time.Now()) {
		w.Header().Set("Retry-After", "1")
		writeJSON(w, http.StatusTooManyRequests, errorResponse{"too many requests"})
		return
	}
	prefix := "/t/" + string(id)
	if r.Method == http.MethodPost && r.URL.Path == prefix+"/api/chat" {
		// Chat over the quota would only queue until tomorrow; say so
		// instead of accepting it silently.
		now := f.now()
		if held, resume := t.meter.hold(now); held {
			w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(resume.Sub(now).Seconds()))))
			writeJSON(w, http.StatusTooManyRequests, errorResponse{fmt.Sprintf("今天的模型额度已用完，%s 之后再发", resume.Format("01-02 15:04"))})
			return
		}
	}
	f.serveStored(w, r, t, http.StripPrefix(prefix, t.handler))
}

// routed finds where to pass an owner's request on without asking the
// registry: the node that held the tenant a moment ago, when the request
// carries the token that was checked then.
func (f *Fleet) routed(r *http.Request, id TenantID) (string, bool) {
	if !f.clustered() || r.Header.Get(hopHeader) != "" {
		return "", false
	}
	known, ok := f.routes.get(id, f.now())
	if !ok || subtle.ConstantTimeCompare([]byte(hashToken(bearer(r))), []byte(known.token)) != 1 {
		return "", false
	}
	f.mu.Lock()
	_, here := f.tenants[id]
	f.mu.Unlock()
	if here {
		return "", false
	}
	f.counters.routeHits.Add(1)
	return known.addr, true
}

func bearer(r *http.Request) string {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func requireAdmin(hash string, next http.Handler) http.Handler {
	return requireToken(hash, "miniloop-admin", "admin token required", next)
}

// requireToken lets through same-origin JSON writes that carry the token
// whose hash is given, compared in constant time.
func requireToken(hash, realm, refusal string, next http.Handler) http.Handler {
	next = jsonOnly(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(hashToken(bearer(r))), []byte(hash)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+realm+`"`)
			writeJSON(w, http.StatusUnauthorized, errorResponse{refusal})
			return
		}
		next.ServeHTTP(w, r)
	})
}

type pushResponse struct {
	Outcome PushOutcome `json:"outcome"`
}

// handlePush is the provider-facing door for external events. Its body is
// not read: a push only says that something changed for this tenant.
func (f *Fleet) handlePush(w http.ResponseWriter, r *http.Request) {
	outcome, err := f.Push(r.Context(), TenantID(r.PathValue("tenant")))
	var away *elsewhere
	switch {
	case errors.As(err, &away):
		f.forward(w, r, away.addr)
	case errors.Is(err, ErrTenantUnknown):
		writeJSON(w, http.StatusNotFound, errorResponse{err.Error()})
	case errors.Is(err, ErrFleetClosed):
		w.Header().Set("Retry-After", "5")
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{err.Error()})
	case err != nil && outcome == "":
		writeJSON(w, http.StatusInternalServerError, errorResponse{err.Error()})
	default:
		// A failed poll of an open assistant is retried by its own poller
		// and shows on its health board; the push itself was taken.
		writeJSON(w, http.StatusAccepted, pushResponse{outcome})
	}
}

type createTenantRequest struct {
	ID TenantID `json:"id"`
}

// TenantCreated is the only time the owner's token is shown; Link opens the
// owner's page with the token in the fragment, which browsers do not send.
type TenantCreated struct {
	ID    TenantID `json:"id"`
	Token string   `json:"token"`
	Link  string   `json:"link"`
}

func (f *Fleet) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	var request createTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid JSON body"})
		return
	}
	id, token, err := f.CreateTenant(r.Context(), request.ID)
	switch {
	case errors.Is(err, ErrTenantExists):
		writeJSON(w, http.StatusConflict, errorResponse{err.Error()})
	case err != nil && !request.ID.Valid():
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorResponse{err.Error()})
	default:
		writeJSON(w, http.StatusCreated, TenantCreated{ID: id, Token: token, Link: "/t/" + string(id) + "/#token=" + token})
	}
}

func (f *Fleet) handleListTenants(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	records, err := f.Tenants(r.Context(), TenantID(r.URL.Query().Get("after")), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{err.Error()})
		return
	}
	next := ""
	if len(records) > 0 {
		next = string(records[len(records)-1].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenants": records, "next": next})
}

func (f *Fleet) handleGetTenant(w http.ResponseWriter, r *http.Request) {
	record, today, err := f.Tenant(r.Context(), TenantID(r.PathValue("tenant")))
	switch {
	case errors.Is(err, ErrTenantUnknown):
		writeJSON(w, http.StatusNotFound, errorResponse{err.Error()})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorResponse{err.Error()})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"tenant": record, "today": today})
	}
}

func (f *Fleet) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	err := f.DeleteTenant(context.WithoutCancel(r.Context()), TenantID(r.PathValue("tenant")))
	var away *elsewhere
	switch {
	case errors.As(err, &away):
		f.forward(w, r, away.addr)
	case errors.Is(err, errMoving):
		busy(w, 1)
	case errors.Is(err, ErrTenantUnknown):
		writeJSON(w, http.StatusNotFound, errorResponse{err.Error()})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, errorResponse{err.Error()})
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (f *Fleet) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := f.Stats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (f *Fleet) handleListShards(w http.ResponseWriter, r *http.Request) {
	shards, err := f.Shards(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, shardsResponse{shards})
}

type shardsResponse struct {
	Shards []ShardInfo `json:"shards"`
}

type setShardRequest struct {
	State ShardState `json:"state"`
}

func (f *Fleet) handleSetShard(w http.ResponseWriter, r *http.Request) {
	var request setShardRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid JSON body"})
		return
	}
	err := f.SetShard(r.Context(), r.PathValue("shard"), request.State)
	switch {
	case errors.Is(err, ErrShardUnknown):
		writeJSON(w, http.StatusNotFound, errorResponse{err.Error()})
	case err != nil:
		writeJSON(w, http.StatusConflict, errorResponse{err.Error()})
	default:
		f.handleListShards(w, r)
	}
}

// metricsTTL is how long one reading of the metrics is served. Counting the
// registry's tenants reads every row, and a scraper asks every few seconds.
const metricsTTL = 10 * time.Second

// handleMetrics writes the fleet's counters in the Prometheus text format.
func (f *Fleet) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	f.metrics.mu.Lock()
	defer f.metrics.mu.Unlock()
	if f.metrics.text != "" && time.Since(f.metrics.at) < metricsTTL {
		_, _ = w.Write([]byte(f.metrics.text))
		return
	}
	stats, err := f.Stats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{err.Error()})
		return
	}
	var out strings.Builder
	gauge := func(name, help string, value int64) {
		fmt.Fprintf(&out, "# HELP miniloop_fleet_%s %s\n# TYPE miniloop_fleet_%s gauge\nminiloop_fleet_%s %d\n", name, help, name, name, value)
	}
	counter := func(name, help string, value int64) {
		fmt.Fprintf(&out, "# HELP miniloop_fleet_%s_total %s\n# TYPE miniloop_fleet_%s_total counter\nminiloop_fleet_%s_total %d\n", name, help, name, name, value)
	}
	gauge("active_tenants", "Assistants open on this node.", int64(stats.Active))
	gauge("max_active_tenants", "Assistants this node may have open.", int64(stats.MaxActive))
	gauge("turns_in_use", "Model turns running on this node.", int64(stats.TurnsInUse))
	gauge("turns_waiting", "Loops waiting for a model turn on this node.", stats.TurnsWaiting)
	gauge("leftovers", "Copies of tenants' data written down to be removed.", int64(stats.Leftovers))
	gauge("usage_owed", "Model calls this node has still to record in the registry.", int64(stats.UsageOwed))
	gauge("cache_bytes", "What this node's copies of tenants took at the last sweep.", stats.CacheBytes)
	fmt.Fprint(&out, "# HELP miniloop_fleet_tenants Tenants in the registry by where their data is.\n# TYPE miniloop_fleet_tenants gauge\n")
	for _, state := range []TenantState{TenantNew, TenantWarm, TenantCold} {
		fmt.Fprintf(&out, "miniloop_fleet_tenants{state=%q} %d\n", string(state), stats.Tenants[state])
	}
	alive := 0
	for _, node := range stats.Nodes {
		if node.Alive {
			alive++
		}
	}
	gauge("nodes_alive", "Nodes whose heartbeat is current.", int64(alive))
	fmt.Fprint(&out, "# HELP miniloop_fleet_shard_tenants Tenants whose data is in the shard.\n# TYPE miniloop_fleet_shard_tenants gauge\n")
	for _, shard := range stats.Shards {
		fmt.Fprintf(&out, "miniloop_fleet_shard_tenants{shard=%q,state=%q} %d\n", shard.Name, string(shard.State), shard.Tenants)
	}
	fmt.Fprint(&out, "# HELP miniloop_fleet_shard_down Whether the shard failed this node's last statements.\n# TYPE miniloop_fleet_shard_down gauge\n")
	for _, shard := range stats.Shards {
		down := 0
		if shard.Down {
			down = 1
		}
		fmt.Fprintf(&out, "miniloop_fleet_shard_down{shard=%q} %d\n", shard.Name, down)
	}
	for _, c := range []struct {
		name, help string
		value      int64
	}{
		{"activations", "Assistants opened.", stats.Activations}, {"hibernations", "Assistants put to sleep.", stats.Hibernations},
		{"evictions", "Quiet assistants closed to make room.", stats.Evictions}, {"rejections", "Opens refused because the node was full.", stats.Rejections},
		{"stalls", "Assistants closed for staying busy without progress.", stats.Stalls}, {"wake_failures", "Due tenants that failed to open.", stats.WakeFailures},
		{"pushes", "Pushes received.", stats.Pushes}, {"forwards", "Requests passed on to another node.", stats.Forwards},
		{"route_hits", "Requests passed on without asking the registry.", stats.RouteHits}, {"takeovers", "Tenants recovered from stopped nodes.", stats.Takeovers},
		{"fences", "Times this node closed its tenants after missing heartbeats.", stats.Fences}, {"outages", "Registry outages this node kept its tenants open through.", stats.Outages},
		{"abandons", "Tenants given up because the vault refused a checkpoint.", stats.Abandons}, {"checkpoints", "Checkpoints written.", stats.Checkpoints},
		{"checkpoint_bytes", "Bytes of pages written in checkpoints.", stats.CheckpointBytes}, {"vault_pulls", "Tenants read from the vault.", stats.VaultPulls},
		{"vault_pulled_bytes", "Bytes of tenants read from the vault.", stats.VaultPulledBytes}, {"cache_reuses", "Opens that reused the node's copy.", stats.CacheReuses},
		{"cache_drops", "Copies of tenants dropped from the node.", stats.CacheDrops}, {"moves", "Tenants moved between shards.", stats.Moves},
		{"purges", "Leftover copies removed.", stats.Purges}, {"corrupt", "Tenants whose stored pages failed their checksum.", stats.Corrupt},
		{"frozen", "Tenants moved to the archive store.", stats.Frozen}, {"thawed", "Tenants read back from the archive store.", stats.Thawed},
		{"model_calls", "Model calls made.", stats.ModelCalls},
	} {
		counter(c.name, c.help, c.value)
	}
	f.metrics.text, f.metrics.at = out.String(), time.Now()
	_, _ = w.Write([]byte(f.metrics.text))
}

// tokenBucket limits one owner's request rate.
type tokenBucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	at     time.Time
}

func newTokenBucket(rate, burst float64, now time.Time) *tokenBucket {
	return &tokenBucket{rate: rate, burst: burst, tokens: burst, at: now}
}

func (b *tokenBucket) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if elapsed := now.Sub(b.at).Seconds(); elapsed > 0 {
		b.tokens = min(b.burst, b.tokens+elapsed*b.rate)
		b.at = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
