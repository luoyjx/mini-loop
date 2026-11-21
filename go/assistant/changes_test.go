package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stateGetter reads /api/state through a handler, with the tag the page
// last saw and an optional wait.
type stateGetter func(tag, query string) *httptest.ResponseRecorder

func daemonState(handler http.Handler) stateGetter {
	return func(tag, query string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/api/state"+query, nil)
		request.Host = "127.0.0.1:8790"
		if tag != "" {
			request.Header.Set("If-None-Match", tag)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
}

// stableTag waits until the startup work has settled and two reads in a row
// give the same tag.
func stableTag(t *testing.T, get stateGetter) string {
	t.Helper()
	last := ""
	for range 100 {
		response := get("", "")
		if response.Code != http.StatusOK {
			t.Fatalf("state: %d %s", response.Code, response.Body.String())
		}
		tag := response.Header().Get("ETag")
		if tag != "" && tag == last {
			return tag
		}
		last = tag
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the state kept changing")
	return ""
}

// The page's long poll: a current tag is answered 304 at once; with a wait
// it is held until something changes and then answered with the new state,
// or answered 304 when the wait ends. The simulator's writes to the outside
// world count as changes too.
func TestStateLongPollAnswersWhenSomethingChanges(t *testing.T) {
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	get := daemonState(app.Handler())
	settle(t, app.Store)
	tag := stableTag(t, get)
	if again := get(tag, ""); again.Code != http.StatusNotModified || again.Body.Len() != 0 || again.Header().Get("ETag") != tag {
		t.Fatalf("a current tag is not sent the state again: %d %q", again.Code, again.Header().Get("ETag"))
	}
	if stale := get(`"an-older-process-0-0-0"`, "?wait=20"); stale.Code != http.StatusOK {
		t.Fatalf("a tag that is not current gets the state at once: %d", stale.Code)
	}

	start := time.Now()
	if idle := get(tag, "?wait=1"); idle.Code != http.StatusNotModified || time.Since(start) < 900*time.Millisecond {
		t.Fatalf("an unchanged state is held for the wait, then 304: %d after %v", idle.Code, time.Since(start))
	}

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- get(tag, "?wait=20") }()
	select {
	case response := <-done:
		t.Fatalf("answered before anything changed: %d", response.Code)
	case <-time.After(300 * time.Millisecond):
	}
	changedAt := time.Now()
	if _, err := app.Store.ChatFromUser(context.Background(), "今天日程", clock.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-done:
		if response.Code != http.StatusOK || response.Header().Get("ETag") == tag || !strings.Contains(response.Body.String(), "今天日程") {
			t.Fatalf("the held poll gets the new state: %d %q", response.Code, response.Header().Get("ETag"))
		}
		if waited := time.Since(changedAt); waited > stateRecheck {
			t.Fatalf("a store write wakes the poll at once, not at the recheck: %v", waited)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the held poll did not see the chat")
	}

	settle(t, app.Store)
	tag = stableTag(t, get)
	booked := httptest.NewRequest(http.MethodPost, "/api/simulate/event", strings.NewReader(`{}`))
	booked.Host = "127.0.0.1:8790"
	booked.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	app.Handler().ServeHTTP(recorder, booked)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("book an event: %d %s", recorder.Code, recorder.Body.String())
	}
	if response := get(tag, ""); response.Code != http.StatusOK {
		t.Fatalf("a meeting added to the outside calendar changes the state: %d", response.Code)
	}
}

func TestStateWaitIsBounded(t *testing.T) {
	cases := map[string]time.Duration{"": 0, "x": 0, "-3": 0, "0": 0, "5": 5 * time.Second, "25": maxStateWait, "9999999999999": maxStateWait}
	for value, want := range cases {
		if got := stateWait(value); got != want {
			t.Errorf("wait=%q: %v, want %v", value, got, want)
		}
	}
}

// A held poll is the owner's presence, so the reaper leaves the assistant
// open, but it does not hold up a close: stopping the assistant answers it.
func TestFleetHeldPollReturnsWhenTheTenantCloses(t *testing.T) {
	clock := newClock()
	fleet := openFleet(t, clock, nil)
	token := createTenant(t, fleet, "alice")
	handler := fleet.Handler("", "")
	get := func(tag, query string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/t/alice/api/state"+query, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		if tag != "" {
			request.Header.Set("If-None-Match", tag)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	tag := stableTag(t, get)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- get(tag, "?wait=20") }()
	waitFor(t, "the poll to be held", func() bool {
		fleet.mu.Lock()
		defer fleet.mu.Unlock()
		open := fleet.tenants["alice"]
		return open != nil && open.refs > 0
	})
	clock.Set(clock.Now().Add(2 * time.Minute))
	fleet.reap(context.Background())
	if fleet.opened("alice") == nil {
		t.Fatal("an owner's open poll keeps the assistant open")
	}
	start := time.Now()
	if err := fleet.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-done:
		if response.Code != http.StatusNotModified {
			t.Fatalf("the held poll is answered 304 when the assistant stops: %d", response.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the held poll outlived the close")
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("the close waited for the poll: %v", took)
	}
}

// A provider's push wakes a sleeping tenant at once instead of at its next
// timer: the waker opens it and its first poll turns the new mail into work.
// Pushes coalesce, a push to an open assistant polls it at once, one that
// arrives while it closes makes it due again, and tenants never opened or
// whose owner is dormant are not woken.
func TestFleetPushWakesASleepingTenantForNewMail(t *testing.T) {
	ctx := context.Background()
	clock := newClock()
	fleet := openFleet(t, clock, func(c *FleetConfig) { c.DormantAfter = 7 * 24 * time.Hour })
	createTenant(t, fleet, "alice")
	createTenant(t, fleet, "carol")
	const push = "push-token-for-the-fleet-test"
	handler := fleet.Handler("admin-token-for-the-fleet-test", push)
	call := func(id, token string) (int, PushOutcome) {
		t.Helper()
		code, body := tenantCall(handler, http.MethodPost, "/push/"+id, token, `{"source":"mail"}`)
		var response pushResponse
		_ = json.Unmarshal([]byte(body), &response)
		return code, response.Outcome
	}
	for _, token := range []string{"", "wrong", "admin-token-for-the-fleet-test"} {
		if code, _ := call("alice", token); code != http.StatusUnauthorized {
			t.Fatalf("push with token %q: %d", token, code)
		}
	}
	if code, _ := call("nobody", push); code != http.StatusNotFound {
		t.Fatalf("unknown tenant: %d", code)
	}
	if code, outcome := call("carol", push); code != http.StatusAccepted || outcome != PushDeferred {
		t.Fatalf("a tenant never opened has no mail cursor to catch up from: %d %q", code, outcome)
	}

	touch(t, fleet, "alice")
	clock.Set(clock.Now().Add(2 * time.Minute))
	fleet.reap(ctx)
	if fleet.opened("alice") != nil {
		t.Fatal("alice sleeps")
	}
	if got := wakeAt(t, record(t, fleet, "alice")); !got.Equal(today(clock, 7, 0)) {
		t.Fatalf("without a push she would sleep until the brief: %v", got)
	}
	world, err := OpenWorld(ctx, filepath.Join(fleet.dir("alice"), "mock-mcp", "world.db"), clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	mail, err := world.Deliver(ctx, PresetManager)
	if err := world.Close(); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if code, outcome := call("alice", push); code != http.StatusAccepted || outcome != PushScheduled {
			t.Fatalf("push to a sleeping tenant: %d %q", code, outcome)
		}
	}
	if got := wakeAt(t, record(t, fleet, "alice")); !got.Equal(clock.Now()) {
		t.Fatalf("a push makes the tenant due now: %v", got)
	}
	fleet.wakeDue(ctx)
	opened := fleet.opened("alice")
	if opened == nil {
		t.Fatal("the waker opens the pushed tenant")
	}
	triggers, err := opened.app.Store.Triggers(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	mailTriggers := 0
	for _, trigger := range triggers {
		if trigger.Ref == mail.ID {
			mailTriggers++
		}
	}
	if mailTriggers != 1 {
		t.Fatalf("the new mail became one trigger on the first poll: %+v", triggers)
	}
	settle(t, opened.app.Store)
	if code, outcome := call("alice", push); code != http.StatusAccepted || outcome != PushPolled {
		t.Fatalf("push to an open assistant: %d %q", code, outcome)
	}

	if !fleet.claimIdle(opened) {
		t.Fatal("alice can be closed")
	}
	if _, outcome := call("alice", push); outcome != PushScheduled {
		t.Fatalf("push while closing: %q", outcome)
	}
	fleet.hibernate(opened, time.Time{})
	if got := wakeAt(t, record(t, fleet, "alice")); got.After(clock.Now()) {
		t.Fatalf("a push during the close makes the tenant due again: %v", got)
	}

	clock.Set(clock.Now().Add(8 * 24 * time.Hour))
	if code, outcome := call("alice", push); code != http.StatusAccepted || outcome != PushDeferred {
		t.Fatalf("a dormant owner's tenant is not woken: %d %q", code, outcome)
	}
	stats, err := fleet.Stats(ctx)
	if err != nil || stats.Pushes != 8 {
		t.Fatalf("pushes counted: %+v %v", stats, err)
	}
}

// TestStatePollCost compares what a visible page cost the server per poll
// before the long poll, a full state build every 1.5 s, with what an
// unchanged check costs now. It is opt-in: MINILOOP_STATE_COST=1.
func TestStatePollCost(t *testing.T) {
	if os.Getenv("MINILOOP_STATE_COST") == "" {
		t.Skip("set MINILOOP_STATE_COST=1 to measure")
	}
	clock := newClock()
	app, _ := startApp(t, t.TempDir(), clock)
	for _, preset := range []Preset{PresetManager, PresetInviteConflict, PresetNewsletter, PresetFriend, PresetQuestion, PresetCc} {
		deliver(t, app, preset)
	}
	for _, text := range []string{"今天日程", "收件箱", "进行中的任务", "我的经理是谁"} {
		if _, err := app.Store.ChatFromUser(context.Background(), text, clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	settle(t, app.Store)
	get := daemonState(app.Handler())
	tag := stableTag(t, get)
	const rounds = 300
	measure := func(sent string, want int) (time.Duration, int) {
		start := time.Now()
		size := 0
		for range rounds {
			response := get(sent, "")
			if response.Code != want {
				t.Fatalf("status %d, want %d", response.Code, want)
			}
			size = response.Body.Len()
		}
		return time.Since(start) / rounds, size
	}
	full, fullSize := measure("", http.StatusOK)
	check, checkSize := measure(tag, http.StatusNotModified)
	t.Logf("full state: %v and %d bytes per poll; unchanged check: %v and %d bytes; %.0fx less server time", full, fullSize, check, checkSize, float64(full)/float64(check))
	t.Logf("a visible page left idle for a minute: before 40 full builds (%v); now 2-3 held polls of one check each plus a recheck every %v (%v)",
		40*full, stateRecheck, time.Duration(2+int(time.Minute/stateRecheck))*check)
}
