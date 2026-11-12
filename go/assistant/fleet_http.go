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
// /admin/ with adminToken; an empty adminToken disables the admin API.
// /healthz answers without a credential.
//
// Unlike a daemon's Handler it does not require a loopback host: a fleet is
// meant to be reached over the network, behind TLS.
func (f *Fleet) Handler(adminToken string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
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
	}
	return mux
}

// serveTenant authenticates the owner, opens the assistant if it sleeps and
// hands the request to the tenant's own API.
func (f *Fleet) serveTenant(w http.ResponseWriter, r *http.Request) {
	id := TenantID(r.PathValue("tenant"))
	if !f.Authenticate(r.Context(), id, bearer(r)) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="miniloop"`)
		writeJSON(w, http.StatusUnauthorized, errorResponse{"a valid tenant token is required"})
		return
	}
	t, err := f.acquire(r.Context(), id, true)
	switch {
	case err == nil:
	case errors.Is(err, ErrFleetFull), errors.Is(err, ErrFleetClosed):
		w.Header().Set("Retry-After", "5")
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{"the service is busy; try again shortly"})
		return
	case errors.Is(err, ErrTenantUnknown):
		writeJSON(w, http.StatusUnauthorized, errorResponse{"a valid tenant token is required"})
		return
	case r.Context().Err() != nil:
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
	http.StripPrefix(prefix, t.handler).ServeHTTP(w, r)
}

func bearer(r *http.Request) string {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func requireAdmin(hash string, next http.Handler) http.Handler {
	next = jsonOnly(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(hashToken(bearer(r))), []byte(hash)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="miniloop-admin"`)
			writeJSON(w, http.StatusUnauthorized, errorResponse{"admin token required"})
			return
		}
		next.ServeHTTP(w, r)
	})
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
	switch {
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
