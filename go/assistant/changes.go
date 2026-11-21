package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// The page subscribes to changes with a long poll. GET /api/state answers
// with an ETag. A request that sends that tag back in If-None-Match and adds
// ?wait=N is held until the state changes, then answered with the new state,
// or answered 304 after N seconds. An open page then costs a cheap version
// check per change instead of a full state build every 1.5 s, and an idle one
// a single request every maxStateWait.

// maxStateWait bounds one long poll. It stays well inside the fleet's write
// timeout and the idle timeout of common proxies.
const maxStateWait = 25 * time.Second

// stateRecheck is how often a held poll reads the version again without a
// signal. Health counters, remote channel status and the poll error live in
// memory and change without a store write, and so do the few store writes
// that do not signal.
const stateRecheck = 2 * time.Second

// changeFeed wakes the polls waiting on a store. A signal only says "look
// again"; the version decides whether anything changed.
type changeFeed struct {
	mu sync.Mutex
	ch chan struct{}
}

// next returns a channel that the next signal closes.
func (f *changeFeed) next() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ch == nil {
		f.ch = make(chan struct{})
	}
	return f.ch
}

func (f *changeFeed) signal() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ch != nil {
		close(f.ch)
		f.ch = nil
	}
}

// Version grows with every row this process writes to the store, read-only
// transactions excluded. SQLite counts the rows changed on a connection and
// the store pins one. Should database/sql ever open a fresh connection, which
// counts from zero again, the old count is carried over so the version never
// goes back.
func (s *Store) Version(ctx context.Context) (int64, error) {
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT total_changes()`).Scan(&total); err != nil {
		return 0, err
	}
	s.versionMu.Lock()
	defer s.versionMu.Unlock()
	if total < s.lastTotal {
		s.base += s.lastTotal
	}
	s.lastTotal = total
	return s.base + total, nil
}

// liveState is the part of the page's state that is kept in memory.
type liveState struct {
	Router  RouterStats     `json:"router"`
	Remotes []RemoteStatus  `json:"remotes"`
	Health  []HealthProblem `json:"health"`
	Poll    string          `json:"poll"`
	Vectors *VectorStatus   `json:"vectors"`
}

// stateTag names the page's current state without building it: this
// process, the store's version, changes to the simulated world that the
// store does not see, and a hash of what lives in memory. It costs one
// trivial query.
func (app *App) stateTag(ctx context.Context) (string, error) {
	version, err := app.Store.Version(ctx)
	if err != nil {
		return "", err
	}
	live := liveState{Router: app.Router.Stats(), Remotes: app.Remotes(), Health: app.Health.Problems(), Poll: app.Poller.LastError()}
	if app.Search.vectors != nil {
		status := app.Search.vectors.Status()
		status.CheckedAt = nil
		live.Vectors = &status
	}
	hash := fnv.New64a()
	if err := json.NewEncoder(hash).Encode(live); err != nil {
		return "", err
	}
	return fmt.Sprintf(`"%s-%d-%d-%x"`, app.epoch, version, app.world.Load(), hash.Sum64()), nil
}

// worldChanged records a change the page shows but the store does not hold:
// the simulator wrote to the external mailbox or calendar.
func (app *App) worldChanged() {
	app.world.Add(1)
	app.Store.changes.signal()
}

// awaitChange holds a poll until the state's tag is no longer tag, the wait
// ends, the client leaves or the assistant stops, and returns the tag then.
func (app *App) awaitChange(ctx context.Context, tag string, wait time.Duration) (string, error) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	recheck := time.NewTicker(stateRecheck)
	defer recheck.Stop()
	for {
		changed := app.Store.changes.next()
		current, err := app.stateTag(ctx)
		if ctx.Err() != nil {
			return tag, nil
		}
		if err != nil || current != tag {
			return current, err
		}
		select {
		case <-ctx.Done():
			return tag, nil
		case <-app.halt:
			return tag, nil
		case <-timer.C:
			return tag, nil
		case <-changed:
		case <-recheck.C:
		}
	}
}

// notModified answers a poll whose state has not changed, or the client's
// tag when it is current. It reports whether it answered.
func (app *App) notModified(w http.ResponseWriter, r *http.Request, tag string) (string, bool, error) {
	known := r.Header.Get("If-None-Match")
	if known == "" || known != tag {
		return tag, false, nil
	}
	if wait := stateWait(r.URL.Query().Get("wait")); wait > 0 {
		var err error
		if tag, err = app.awaitChange(r.Context(), tag, wait); err != nil {
			return tag, false, err
		}
	}
	if known != tag {
		return tag, false, nil
	}
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotModified)
	return tag, true, nil
}

// stateWait reads ?wait=N, in seconds, capped at maxStateWait.
func stateWait(value string) time.Duration {
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds <= 0 {
		return 0
	}
	return min(time.Duration(min(seconds, 3600))*time.Second, maxStateWait)
}
