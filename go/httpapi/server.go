// Package httpapi serves the implemented Go runtime through typed HTTP boundaries.
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/protocol"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const MaxRequestBytes = 10 * 1024 * 1024
const MaxIdempotencyKeys = 1024
const MaxRateWindows = 4096
const consoleCSP = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

type Config struct {
	Manager            *agent.SessionManager
	Auth               Authenticator
	RateLimitPerMinute int
	PingInterval       time.Duration
	FakeLLM            bool
	Build              string
	Now                func() time.Time
}

type cacheKey struct {
	Owner   agent.OwnerID
	Session agent.SessionID
	Key     string
}
type rateWindow struct {
	Minute int64
	Count  int
}
type Server struct {
	manager   *agent.SessionManager
	auth      Authenticator
	rateLimit int
	ping      time.Duration
	fake      bool
	build     string
	now       func() time.Time
	started   time.Time
	mux       *http.ServeMux
	mu        sync.Mutex
	cache     map[cacheKey]MessageResponse
	running   map[agent.SessionID]bool
	windows   map[agent.OwnerID]rateWindow
}

func New(config Config) (*Server, error) {
	if config.Manager == nil {
		return nil, errors.New("HTTP server requires a session manager")
	}
	if config.RateLimitPerMinute < 0 || config.PingInterval < 0 {
		return nil, errors.New("HTTP limits cannot be negative")
	}
	if config.Auth == nil {
		config.Auth = NullAuth{}
	}
	if config.PingInterval == 0 {
		config.PingInterval = 15 * time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Build == "" {
		config.Build = "development"
	}
	s := &Server{manager: config.Manager, auth: config.Auth, rateLimit: config.RateLimitPerMinute, ping: config.PingInterval, fake: config.FakeLLM, build: config.Build, now: config.Now, started: config.Now(), mux: http.NewServeMux(), cache: make(map[cacheKey]MessageResponse), running: make(map[agent.SessionID]bool), windows: make(map[agent.OwnerID]rateWindow)}
	s.routes()
	return s, nil
}

// Body admission wraps every route, including authentication and future routes.
// The declared-size path never reads a body; the stream path buffers at most cap+1.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > MaxRequestBytes {
		writeJSON(s, w, 413, ErrorResponse{"request body too large"})
		return
	}
	if r.Body != nil {
		data, err := io.ReadAll(io.LimitReader(r.Body, MaxRequestBytes+1))
		r.Body.Close()
		if len(data) > MaxRequestBytes {
			writeJSON(s, w, 413, ErrorResponse{"request body too large"})
			return
		}
		if err != nil {
			writeJSON(s, w, 400, ErrorResponse{"request body could not be read"})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(data))
	} else {
		r.Body = http.NoBody
	}
	w.Header().Set("Content-Security-Policy", consoleCSP)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	caller := Principal{}
	if !publicPath(r.URL.Path) {
		header := r.Header.Get("Authorization")
		if header == "" && eventPath(r.URL.Path) {
			if token := r.URL.Query().Get("access_token"); token != "" {
				header = "Bearer " + token
			}
		}
		var ok bool
		caller, ok = s.auth.Authenticate(header)
		if !ok || caller.ID == "" {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(s, w, 401, ErrorResponse{"a bearer token is required"})
			return
		}
	}
	// One admitted identity is carried through the handler; never authenticate twice.
	r = r.WithContext(withPrincipal(r.Context(), caller))
	s.mux.ServeHTTP(w, r)
}

func publicPath(path string) bool {
	switch path {
	case "/healthz", "/", "/ui", "/favicon.ico":
		return true
	}
	return false
}
func eventPath(path string) bool { return len(path) >= 7 && path[len(path)-7:] == "/events" }

// Projection operates on detached typed data before JSON escaping. Errors and
// panics fail closed; no raw failed projection is returned to a client.
func recordingJSON[T any](s *Server, value T) (data []byte, err error) {
	defer func() {
		if recover() != nil {
			data = nil
			err = errors.New("recording projection failed")
		}
	}()
	masker := s.manager.RecordingMasker()
	if masker == nil {
		return json.Marshal(value)
	}
	projected, err := protocol.MaskedPythonJSON(value, masker.MaskText, false, true)
	return []byte(projected), err
}

func writeJSON[T any](s *Server, w http.ResponseWriter, status int, value T) {
	data, err := recordingJSON(s, value)
	if err != nil {
		status = 500
		data = []byte(`{"detail":"response encoding failed"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(data)
}
func decodeBody[T any](s *Server, w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	var parsed *T
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&parsed); err != nil || parsed == nil {
		writeJSON(s, w, 422, ErrorResponse{"invalid request body"})
		return v, false
	}
	v = *parsed
	var extra struct{}
	if err := decoder.Decode(&extra); err != io.EOF {
		writeJSON(s, w, 422, ErrorResponse{"invalid request body"})
		return v, false
	}
	return v, true
}
func (s *Server) require(w http.ResponseWriter, r *http.Request) (*agent.ManagedSession, bool) {
	id := agent.SessionID(r.PathValue("session_id"))
	session, err := s.manager.Get(principal(r.Context()).ID, id)
	if err != nil {
		writeJSON(s, w, 404, ErrorResponse{fmt.Sprintf("No session '%s'", id)})
		return nil, false
	}
	return session, true
}
func busyDetail(id agent.SessionID) string {
	return fmt.Sprintf("session %s is running a turn; POST /sessions/%s/steer to redirect it, /sessions/%s/cancel to stop it, or retry", id, id, id)
}

// Called under mu so message cache lookup, rate accounting and admission can
// form one transition without replay spending budget during a completion race.
func (s *Server) rateLocked(caller Principal, now time.Time) int {
	if s.rateLimit == 0 {
		return 0
	}
	minute := now.Unix() / 60
	window, exists := s.windows[caller.ID]
	if window.Minute != minute {
		window = rateWindow{Minute: minute}
	}
	window.Count++
	if !exists && len(s.windows) >= MaxRateWindows {
		clear(s.windows)
	}
	s.windows[caller.ID] = window
	if window.Count <= s.rateLimit {
		return 0
	}
	return 60 - int(now.Unix()%60)
}
func (s *Server) rateDenial(w http.ResponseWriter, retry int) {
	w.Header().Set("Retry-After", strconv.Itoa(retry))
	writeJSON(s, w, 429, ErrorResponse{fmt.Sprintf("rate limit exceeded (%d/minute); retry in %ds", s.rateLimit, retry)})
}
func (s *Server) rate(w http.ResponseWriter, caller Principal) bool {
	if s.rateLimit == 0 {
		return true
	}
	now := s.now()
	s.mu.Lock()
	retry := s.rateLocked(caller, now)
	s.mu.Unlock()
	if retry == 0 {
		return true
	}
	s.rateDenial(w, retry)
	return false
}
