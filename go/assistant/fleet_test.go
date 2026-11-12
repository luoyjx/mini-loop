package assistant

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// openFleet runs a fleet on the test clock. Its loops are not started: tests
// call wakeDue, reap and janitor themselves, so every transition is explicit.
func openFleet(t *testing.T, clock *fixedClock, tune func(*FleetConfig)) *Fleet {
	t.Helper()
	config := FleetConfig{
		DataDir: t.TempDir(), Provider: &ScriptedModel{Now: clock.Now}, Model: ScriptedModelID, Now: clock.Now,
		IdleTTL: time.Minute, SchedulerTick: time.Hour, PollInterval: time.Hour, LeaseTTL: 2 * time.Second,
	}
	if tune != nil {
		tune(&config)
	}
	fleet, err := OpenFleet(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := fleet.Close(); err != nil {
			t.Error(err)
		}
	})
	return fleet
}

func createTenant(t *testing.T, fleet *Fleet, id TenantID) string {
	t.Helper()
	_, token, err := fleet.CreateTenant(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// open returns the tenant's open assistant, or nil while it sleeps.
func (f *Fleet) opened(id TenantID) *tenant {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t := f.tenants[id]; t != nil && t.app != nil {
		return t
	}
	return nil
}

func record(t *testing.T, fleet *Fleet, id TenantID) TenantRecord {
	t.Helper()
	got, _, err := fleet.Tenant(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func touch(t *testing.T, fleet *Fleet, id TenantID) *App {
	t.Helper()
	var app *App
	if err := fleet.Use(context.Background(), id, func(a *App) error { app = a; return nil }); err != nil {
		t.Fatal(err)
	}
	return app
}

func wakeAt(t *testing.T, record TenantRecord) time.Time {
	t.Helper()
	if record.NextWake == nil {
		t.Fatalf("%s has no wake time", record.ID)
	}
	return *record.NextWake
}

func today(clock *fixedClock, hour, minute int) time.Time {
	now := clock.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// A registered tenant has no files until its owner's first request, and an
// assistant whose owner left sleeps with its next timer in the registry and
// no SQLite WAL files on disk.
func TestFleetProvisionsLazilyAndSleepsUntilTheNextTimer(t *testing.T) {
	clock := newClock() // 06:30, before the 07:00 morning brief
	fleet := openFleet(t, clock, nil)
	createTenant(t, fleet, "alice")
	dir := fleet.dir("alice")
	if exists(dir) || record(t, fleet, "alice").State != TenantNew {
		t.Fatal("a new tenant has no directory")
	}
	touch(t, fleet, "alice")
	if got := record(t, fleet, "alice"); got.State != TenantWarm || !got.Active || got.NextWake != nil || got.Activations != 1 {
		t.Fatalf("an open tenant is warm, active and has no wake time: %+v", got)
	}
	if !exists(filepath.Join(dir, "assistant.db")) || !exists(filepath.Join(dir, "mock-mcp", "world.db")) {
		t.Fatal("the first request provisions the tenant's directory")
	}
	fleet.reap(context.Background())
	if fleet.opened("alice") == nil {
		t.Fatal("an assistant stays open for IdleTTL after its owner's request")
	}
	clock.Set(clock.Now().Add(2 * time.Minute))
	fleet.reap(context.Background())
	if fleet.opened("alice") != nil {
		t.Fatal("a quiet assistant whose owner left is put to sleep")
	}
	got := record(t, fleet, "alice")
	if got.Active || !wakeAt(t, got).Equal(today(clock, 7, 0)) || got.LastSeen == nil {
		t.Fatalf("it sleeps until the morning brief and remembers its owner: %+v", got)
	}
	if exists(filepath.Join(dir, "assistant.db-wal")) || exists(filepath.Join(dir, "agent.db-wal")) {
		t.Fatal("a sleeping tenant keeps no WAL files")
	}
	stats, err := fleet.Stats(context.Background())
	if err != nil || stats.Active != 0 || stats.Activations != 1 || stats.Hibernations != 1 || stats.Tenants[TenantWarm] != 1 {
		t.Fatalf("stats %+v %v", stats, err)
	}
}

// The waker opens a tenant when its timer is due; the assistant does the
// work on its own loops and goes back to sleep until its next timer.
func TestFleetWakesATenantForItsRoutine(t *testing.T) {
	clock := newClock()
	fleet := openFleet(t, clock, nil)
	createTenant(t, fleet, "alice")
	touch(t, fleet, "alice")
	clock.Set(today(clock, 6, 40))
	fleet.reap(context.Background())
	fleet.wakeDue(context.Background())
	if fleet.opened("alice") != nil {
		t.Fatal("nothing is due before 07:00")
	}
	clock.Set(today(clock, 7, 0).Add(30 * time.Second))
	fleet.wakeDue(context.Background())
	opened := fleet.opened("alice")
	if opened == nil {
		t.Fatal("the waker opens a tenant whose routine is due")
	}
	waitFor(t, "the morning brief", func() bool {
		triggers, err := opened.app.Store.Triggers(context.Background(), 50)
		if err != nil {
			t.Fatal(err)
		}
		return len(triggers) > 0 && triggers[0].Kind == TriggerRoutine
	})
	triggers := settle(t, opened.app.Store)
	if len(triggers) != 1 || triggers[0].Status != TriggerDone {
		t.Fatalf("the brief ran once: %+v", triggers)
	}
	channel, _ := opened.app.Store.Channel(context.Background(), 50)
	if last := channel[len(channel)-1].Text; !strings.Contains(last, "早上好") {
		t.Fatalf("the brief reached the owner's channel: %q", last)
	}
	waitFor(t, "sleep after the brief", func() bool {
		fleet.reap(context.Background())
		return fleet.opened("alice") == nil
	})
	// The design review today at 15:00 is reminded 15 minutes ahead.
	if got := wakeAt(t, record(t, fleet, "alice")); !got.Equal(today(clock, 14, 45)) {
		t.Fatalf("next wake %v, want the 14:45 reminder", got)
	}
	fleet.wakeDue(context.Background())
	if fleet.opened("alice") != nil {
		t.Fatal("a tenant is not woken again before its next timer")
	}
}

// A process that stopped with tenants open leaves them active in the
// registry; the next process makes them due at once so interrupted work
// runs again. Only one fleet holds a directory at a time.
func TestFleetRecoversTenantsLeftOpenByACrash(t *testing.T) {
	clock := newClock()
	dir := t.TempDir()
	first := openFleet(t, clock, func(c *FleetConfig) { c.DataDir = dir })
	createTenant(t, first, "alice")
	createTenant(t, first, "bob")
	if err := first.registry.activated(context.Background(), "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFleet(context.Background(), FleetConfig{DataDir: dir, Provider: &ScriptedModel{}}); !errors.Is(err, ErrDataDirBusy) {
		t.Fatalf("a second fleet on an open fleet's directory: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := openFleet(t, clock, func(c *FleetConfig) { c.DataDir = dir })
	if got := record(t, second, "alice"); got.Active || !wakeAt(t, got).Equal(clock.Now()) {
		t.Fatalf("a crashed tenant is due now: %+v", got)
	}
	if got := record(t, second, "bob"); got.NextWake != nil {
		t.Fatalf("a tenant that was not open stays asleep: %+v", got)
	}
	second.wakeDue(context.Background())
	if second.opened("alice") == nil || second.opened("bob") != nil {
		t.Fatal("the waker reopens only the crashed tenant")
	}
}

// At capacity an owner's request closes the least recently used quiet
// assistant; when every open one is in use it is refused, and the waker
// never evicts.
func TestFleetEvictsTheLeastRecentlyUsedAtCapacity(t *testing.T) {
	clock := newClock()
	fleet := openFleet(t, clock, func(c *FleetConfig) { c.MaxActive = 2 })
	for _, id := range []TenantID{"alice", "bob", "carol"} {
		createTenant(t, fleet, id)
	}
	touch(t, fleet, "alice")
	clock.Set(clock.Now().Add(time.Second))
	touch(t, fleet, "bob")
	clock.Set(clock.Now().Add(time.Second))
	if _, err := fleet.acquire(context.Background(), "carol", false); !errors.Is(err, ErrFleetFull) {
		t.Fatalf("the waker does not evict: %v", err)
	}
	touch(t, fleet, "carol")
	if fleet.opened("alice") != nil || fleet.opened("bob") == nil || fleet.opened("carol") == nil {
		t.Fatal("the least recently used assistant made room")
	}
	if got := record(t, fleet, "alice"); got.Active || got.NextWake == nil {
		t.Fatalf("an evicted assistant sleeps with its wake time: %+v", got)
	}
	held := []*tenant{}
	for _, id := range []TenantID{"bob", "carol"} {
		opened, err := fleet.acquire(context.Background(), id, true)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, opened)
	}
	if err := fleet.Use(context.Background(), "alice", func(*App) error { return nil }); !errors.Is(err, ErrFleetFull) {
		t.Fatalf("assistants in use are not evicted: %v", err)
	}
	for _, opened := range held {
		fleet.release(opened)
	}
	stats, _ := fleet.Stats(context.Background())
	if stats.Evictions != 1 || stats.Rejections != 2 {
		t.Fatalf("stats %+v", stats)
	}
}

// A tenant over its daily quota is held: its chat is refused at the edge,
// queued work waits, the owner is told once, and it sleeps until the next
// day or its next timer.
func TestFleetHoldsATenantOverItsDailyQuota(t *testing.T) {
	clock := newClock()
	fleet := openFleet(t, clock, func(c *FleetConfig) { c.DailyCalls = 1 })
	token := createTenant(t, fleet, "alice")
	handler := fleet.Handler("")
	if code, body := tenantCall(handler, http.MethodPost, "/t/alice/api/chat", token, `{"text":"今天日程"}`); code != http.StatusAccepted && code != http.StatusOK {
		t.Fatalf("chat %d %s", code, body)
	}
	app := fleet.opened("alice").app
	settle(t, app.Store)
	usage := mustUsage(t, fleet, "alice")
	if usage.Calls < 1 {
		t.Fatalf("model calls are metered: %+v", usage)
	}
	code, body := tenantCall(handler, http.MethodPost, "/t/alice/api/chat", token, `{"text":"收件箱"}`)
	if code != http.StatusTooManyRequests || !strings.Contains(body, "额度") {
		t.Fatalf("chat over the quota is refused: %d %s", code, body)
	}
	if _, err := app.Store.ChatFromUser(context.Background(), "收件箱", clock.Now()); err != nil {
		t.Fatal(err)
	}
	app.Router.Wake()
	time.Sleep(300 * time.Millisecond)
	if runnable, err := app.Store.Runnable(context.Background(), clock.Now(), false); err != nil || !runnable {
		t.Fatalf("held work stays queued: %v %v", runnable, err)
	}
	messages, _ := app.Store.Channel(context.Background(), 50)
	notices := 0
	for _, message := range messages {
		if message.Role == RoleSystem && strings.Contains(message.Text, "额度") {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("the owner is told once, got %d notices", notices)
	}
	clock.Set(clock.Now().Add(2 * time.Minute))
	fleet.reap(context.Background())
	if fleet.opened("alice") != nil {
		t.Fatal("a held tenant with only queued model work sleeps")
	}
	if got := wakeAt(t, record(t, fleet, "alice")); !got.Equal(today(clock, 7, 0)) {
		t.Fatalf("it wakes for its next timer, %v", got)
	}
}

func mustUsage(t *testing.T, fleet *Fleet, id TenantID) TenantUsage {
	t.Helper()
	_, usage, err := fleet.Tenant(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return usage
}

// While every background turn of the fleet is taken, a tenant's mail waits
// unclaimed (its attempts untouched) and the owner's chat still runs on the
// slot kept for chat.
func TestFleetGateLetsChatPassQueuedBackgroundWork(t *testing.T) {
	clock := newClock()
	fleet := openFleet(t, clock, func(c *FleetConfig) { c.Turns, c.ChatTurns = 2, 1 })
	token := createTenant(t, fleet, "alice")
	app := touch(t, fleet, "alice")
	busy, ok := fleet.gate.acquire(context.Background(), false, nil)
	if !ok {
		t.Fatal("the background slot is free")
	}
	mail := deliver(t, app, PresetFriend)
	if code, body := tenantCall(fleet.Handler(""), http.MethodPost, "/t/alice/api/chat", token, `{"text":"今天日程"}`); code != http.StatusAccepted && code != http.StatusOK {
		t.Fatalf("chat %d %s", code, body)
	}
	waitFor(t, "the chat reply", func() bool {
		triggers, _ := app.Store.Triggers(context.Background(), 10)
		for _, trigger := range triggers {
			if trigger.Kind == TriggerChat && trigger.Status == TriggerDone {
				return true
			}
		}
		return false
	})
	triggers, _ := app.Store.Triggers(context.Background(), 10)
	if waiting := triggerByID(t, triggers, mail); waiting.Status != TriggerPending || waiting.Attempts != 0 {
		t.Fatalf("background mail waits for a fleet slot without spending attempts: %+v", waiting)
	}
	busy()
	app.Router.Wake()
	if got := triggerByID(t, settle(t, app.Store), mail); got.Status != TriggerQuiet {
		t.Fatalf("the mail runs once a slot frees: %s", got.Status)
	}
}

// An assistant busy with something no loop picks up is closed after
// StallAfter and woken again only after the backoff.
func TestFleetClosesAStalledTenant(t *testing.T) {
	clock := newClock()
	ticked := &calendarSeen{seen: make(chan struct{})}
	fleet := openFleet(t, clock, func(c *FleetConfig) {
		c.StallAfter, c.StallBackoff = 10*time.Minute, 5*time.Minute
		c.Connect = ticked.connect
	})
	createTenant(t, fleet, "alice")
	app := touch(t, fleet, "alice")
	// The scheduler ticks once at start and then hourly here, so a task
	// check that comes due after the first tick is not claimed: the
	// assistant stays busy without running anything.
	select {
	case <-ticked.seen:
	case <-time.After(10 * time.Second):
		t.Fatal("the scheduler's first tick did not run")
	}
	if _, _, err := app.Store.CreateTask(context.Background(), Task{Title: "等回复", Thread: "thread-stall"}, clock.Now().Add(time.Minute), "test", clock.Now()); err != nil {
		t.Fatal(err)
	}
	clock.Set(clock.Now().Add(2 * time.Minute))
	fleet.reap(context.Background())
	clock.Set(clock.Now().Add(5 * time.Minute))
	fleet.reap(context.Background())
	if fleet.opened("alice") == nil {
		t.Fatal("a busy assistant is kept open until StallAfter")
	}
	clock.Set(clock.Now().Add(6 * time.Minute))
	fleet.reap(context.Background())
	if fleet.opened("alice") != nil {
		t.Fatal("a stalled assistant is closed")
	}
	if got := wakeAt(t, record(t, fleet, "alice")); !got.Equal(clock.Now().Add(5 * time.Minute)) {
		t.Fatalf("it wakes after the backoff, not at once: %v", got)
	}
	if stats, _ := fleet.Stats(context.Background()); stats.Stalls != 1 {
		t.Fatalf("stats %+v", stats)
	}
}

// calendarSeen signals the first calendar read, which the scheduler's first
// tick makes after it looked at routines and task checks.
type calendarSeen struct {
	mcpCaller
	once sync.Once
	seen chan struct{}
}

func (c *calendarSeen) CallTool(ctx context.Context, name string, args jsonvalue.Value) (string, error) {
	if name == MCPCalendarList {
		c.once.Do(func() { close(c.seen) })
	}
	return c.mcpCaller.CallTool(ctx, name, args)
}

func (c *calendarSeen) connect(ctx context.Context, _ TenantID, dir string, now func() time.Time) (*Connector, func() error, error) {
	world, err := OpenWorld(ctx, filepath.Join(dir, "mock-mcp", "world.db"), now)
	if err != nil {
		return nil, nil, err
	}
	client, err := NewInProcessMock(world)
	if err != nil {
		return nil, nil, errors.Join(err, world.Close())
	}
	c.mcpCaller = client
	return NewConnector(c, "in-process"), world.Close, nil
}

// Absent owners stop costing wakes after DormantAfter and disk after
// ArchiveAfter; the next request restores the tenant with its history.
func TestFleetRetiresAndArchivesAbsentTenants(t *testing.T) {
	clock := newClock()
	fleet := openFleet(t, clock, func(c *FleetConfig) { c.DormantAfter = 3 * 24 * time.Hour; c.ArchiveAfter = 7 * 24 * time.Hour })
	token := createTenant(t, fleet, "alice")
	handler := fleet.Handler("")
	if code, body := tenantCall(handler, http.MethodPost, "/t/alice/api/chat", token, `{"text":"记住 周三下午不开会"}`); code != http.StatusAccepted && code != http.StatusOK {
		t.Fatalf("chat %d %s", code, body)
	}
	settle(t, fleet.opened("alice").app.Store)
	clock.Set(clock.Now().Add(2 * time.Minute))
	fleet.reap(context.Background())
	dir := fleet.dir("alice")
	before := dirBytes(t, dir)

	clock.Set(clock.Now().Add(4 * 24 * time.Hour))
	fleet.janitor(context.Background())
	if got := record(t, fleet, "alice"); got.NextWake != nil || got.State != TenantWarm {
		t.Fatalf("a dormant tenant leaves the wake index but keeps its files: %+v", got)
	}
	clock.Set(clock.Now().Add(4 * 24 * time.Hour))
	fleet.janitor(context.Background())
	archive := archivePath(dir)
	if got := record(t, fleet, "alice"); got.State != TenantCold || exists(dir) || !exists(archive) {
		t.Fatalf("an absent tenant is archived: %+v", got)
	}
	info, err := os.Stat(archive)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("tenant on disk: %d KB warm, %d KB archived", before/1024, info.Size()/1024)
	if info.Size()*3 > before {
		t.Fatalf("the archive (%d bytes) should be far smaller than the directory (%d bytes)", info.Size(), before)
	}
	code, body := tenantCall(handler, http.MethodGet, "/t/alice/api/state", token, "")
	if code != http.StatusOK || !strings.Contains(body, "周三下午不开会") {
		t.Fatalf("the owner's next request restores the history: %d", code)
	}
	if got := record(t, fleet, "alice"); got.State != TenantWarm || !exists(dir) || exists(archive) {
		t.Fatalf("a restored tenant is warm again: %+v", got)
	}
	if stats, _ := fleet.Stats(context.Background()); stats.Archives != 1 || stats.Restores != 1 || stats.Retired != 1 {
		t.Fatalf("stats %+v", stats)
	}
}

func dirBytes(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	err := filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

// When both a directory and an archive exist (a crash between the two
// steps), the directory wins and the archive is dropped.
func TestFleetRestorePrefersTheDirectory(t *testing.T) {
	clock := newClock()
	fleet := openFleet(t, clock, nil)
	createTenant(t, fleet, "alice")
	touch(t, fleet, "alice")
	clock.Set(clock.Now().Add(2 * time.Minute))
	fleet.reap(context.Background())
	dir := fleet.dir("alice")
	if err := os.WriteFile(archivePath(dir), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	touch(t, fleet, "alice")
	if exists(archivePath(dir)) {
		t.Fatal("a leftover archive beside the directory is removed")
	}
}

func TestReadArchiveStaysInsideTheDirectory(t *testing.T) {
	entries := []tar.Header{
		{Name: "../escape", Mode: 0o600, Size: 1, Typeflag: tar.TypeReg},
		{Name: "/abs", Mode: 0o600, Size: 1, Typeflag: tar.TypeReg},
		{Name: "a/../../escape", Mode: 0o600, Size: 1, Typeflag: tar.TypeReg},
		{Name: "escape", Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: "../escape"},
	}
	for _, entry := range entries {
		name := entry.Name
		var buffer bytes.Buffer
		zipper := gzip.NewWriter(&buffer)
		archive := tar.NewWriter(zipper)
		if err := archive.WriteHeader(&entry); err != nil {
			t.Fatal(err)
		}
		if entry.Size > 0 {
			_, _ = archive.Write([]byte("x"))
		}
		_ = archive.Close()
		_ = zipper.Close()
		path := filepath.Join(t.TempDir(), "evil.tar.gz")
		if err := os.WriteFile(path, buffer.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		root := t.TempDir()
		if err := readArchive(path, filepath.Join(root, "out")); err == nil {
			t.Errorf("%s: an escaping entry is refused", name)
		}
		if exists(filepath.Join(root, "escape")) {
			t.Errorf("%s: nothing is written outside", name)
		}
	}
}

func tenantCall(handler http.Handler, method, path, token, payload string) (int, string) {
	request := httptest.NewRequest(method, path, strings.NewReader(payload))
	if payload != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.String()
}

// Every tenant route needs that tenant's token; the page needs none and
// carries no tenant data; the admin API needs the admin token.
func TestFleetHTTPAuthenticatesEveryTenantRequest(t *testing.T) {
	clock := newClock()
	fleet := openFleet(t, clock, nil)
	const admin = "admin-token-for-the-fleet-test"
	handler := fleet.Handler(admin)
	if code, _ := tenantCall(handler, http.MethodPost, "/admin/tenants", "", `{"id":"alice"}`); code != http.StatusUnauthorized {
		t.Fatalf("admin without token: %d", code)
	}
	code, body := tenantCall(handler, http.MethodPost, "/admin/tenants", admin, `{"id":"alice"}`)
	var created TenantCreated
	if err := json.Unmarshal([]byte(body), &created); code != http.StatusCreated || err != nil || created.Token == "" || created.Link != "/t/alice/#token="+created.Token {
		t.Fatalf("create %d %s", code, body)
	}
	if code, _ := tenantCall(handler, http.MethodPost, "/admin/tenants", admin, `{"id":"alice"}`); code != http.StatusConflict {
		t.Fatalf("duplicate id: %d", code)
	}
	if code, _ := tenantCall(handler, http.MethodPost, "/admin/tenants", admin, `{"id":"../x"}`); code != http.StatusBadRequest {
		t.Fatalf("invalid id: %d", code)
	}
	bob := createTenant(t, fleet, "bob")
	cases := []struct {
		method, path, token, payload string
		want                         int
	}{
		{http.MethodGet, "/t/alice/", "", "", http.StatusOK},
		{http.MethodGet, "/t/alice", "", "", http.StatusMovedPermanently},
		{http.MethodGet, "/t/alice/api/state", "", "", http.StatusUnauthorized},
		{http.MethodGet, "/t/alice/api/state", "wrong", "", http.StatusUnauthorized},
		{http.MethodGet, "/t/alice/api/state", bob, "", http.StatusUnauthorized},
		{http.MethodGet, "/t/nobody/api/state", bob, "", http.StatusUnauthorized},
		{http.MethodGet, "/t/alice/api/state", created.Token, "", http.StatusOK},
		{http.MethodPost, "/t/alice/api/pause", created.Token, "", http.StatusForbidden},
		{http.MethodGet, "/admin/stats", created.Token, "", http.StatusUnauthorized},
		{http.MethodGet, "/admin/stats", admin, "", http.StatusOK},
		{http.MethodGet, "/healthz", "", "", http.StatusOK},
	}
	for _, c := range cases {
		if code, body := tenantCall(handler, c.method, c.path, c.token, c.payload); code != c.want {
			t.Errorf("%s %s token=%q: %d, want %d (%s)", c.method, c.path, c.token, code, c.want, body)
		}
	}
	if _, page := tenantCall(handler, http.MethodGet, "/t/alice/", "", ""); strings.Contains(page, "alice") || !strings.Contains(page, `fetch("api/state"`) {
		t.Fatal("the page is the shared one, with relative API paths")
	}
	if code, _ := tenantCall(handler, http.MethodDelete, "/admin/tenants/alice", admin, ""); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := tenantCall(handler, http.MethodGet, "/t/alice/api/state", created.Token, ""); code != http.StatusUnauthorized || exists(fleet.dir("alice")) {
		t.Fatalf("a deleted tenant's token stops working and its files are gone: %d", code)
	}
	if code, _ := tenantCall(fleet.Handler(""), http.MethodGet, "/admin/stats", admin, ""); code != http.StatusNotFound {
		t.Fatalf("no admin token, no admin API: %d", code)
	}
}

func TestTokenBucketRefills(t *testing.T) {
	start := time.Unix(0, 0)
	bucket := newTokenBucket(2, 3, start)
	for i := 0; i < 3; i++ {
		if !bucket.allow(start) {
			t.Fatalf("burst request %d refused", i)
		}
	}
	if bucket.allow(start) {
		t.Fatal("a request past the burst is refused")
	}
	if !bucket.allow(start.Add(500*time.Millisecond)) || bucket.allow(start.Add(500*time.Millisecond)) {
		t.Fatal("half a second at 2/s buys one request")
	}
}

// Chat may take the slots kept for it and any background slot; background
// work never takes a chat slot, and an interrupt ends a wait.
func TestTurnGateKeepsSlotsForChat(t *testing.T) {
	gate := newTurnGate(2, 1)
	ctx := context.Background()
	release, ok := gate.acquire(ctx, false, nil)
	if !ok {
		t.Fatal("the background slot is free")
	}
	interrupt := make(chan struct{})
	waited := make(chan bool, 1)
	go func() {
		_, ok := gate.acquire(ctx, false, interrupt)
		waited <- ok
	}()
	chat, ok := gate.acquire(ctx, true, nil)
	if !ok || gate.inUse() != 2 {
		t.Fatal("chat takes its reserved slot while background work waits")
	}
	close(interrupt)
	if <-waited {
		t.Fatal("an interrupt ends the background wait without a slot")
	}
	release()
	chatToo, ok := gate.acquire(ctx, true, nil)
	if !ok {
		t.Fatal("chat may also take a free background slot")
	}
	chat()
	chatToo()
	if gate.inUse() != 0 {
		t.Fatal("every slot is returned")
	}
}

func TestStandbyQuietAndNext(t *testing.T) {
	now := time.Date(2026, 11, 2, 9, 0, 0, 0, time.Local)
	later, tomorrow := now.Add(time.Hour), now.Add(15*time.Hour)
	cases := []struct {
		name    string
		standby Standby
		held    bool
		quiet   bool
		next    time.Time
	}{
		{"idle", Standby{}, false, true, time.Time{}},
		{"running", Standby{Running: 1}, false, false, time.Time{}},
		{"timer due", Standby{Timer: now}, false, false, now},
		{"timer later", Standby{Timer: later}, false, true, later},
		{"work due", Standby{Work: now}, false, false, now},
		{"work held", Standby{Work: now}, true, true, tomorrow},
		{"held work behind a timer", Standby{Work: now, Timer: later}, true, true, later},
	}
	for _, c := range cases {
		if got := c.standby.Quiet(now, c.held); got != c.quiet {
			t.Errorf("%s: quiet %v", c.name, got)
		}
		if got := c.standby.Next(c.held, tomorrow); !got.Equal(c.next) {
			t.Errorf("%s: next %v, want %v", c.name, got, c.next)
		}
	}
}
