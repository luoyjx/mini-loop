package assistant

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
)

// A Fleet hosts many owners' assistants in one process. Each tenant is an
// unchanged single-owner App on its own data directory, so tenants share no
// tables and one tenant's data can be moved, archived or deleted as a unit.
// With a database the directory is a cache and the tenant's data lives in
// the vault, where any node can read it.
// The fleet keeps only the assistants that have something to do in memory:
// an owner's request opens one, and once it is quiet and its owner has gone
// it is closed again and its next due time is written to the registry, where
// the waker finds it. An idle tenant therefore costs a registry row and its
// files, not goroutines, memory or polling.
//
// Model work is the fleet's largest cost. Every tenant's turns pass one
// fleet-wide gate, every model call is counted per tenant and day, and a
// tenant over its daily quota is held until the next day.
type Fleet struct {
	config   FleetConfig
	registry *registry
	// vault keeps the tenants' data in the fleet's database; nil keeps it in
	// the data directory only.
	vault    *vault
	unlock   func() error
	gate     *turnGate
	now      func() time.Time
	log      io.Writer
	calls    atomic.Int64
	counters fleetCounters

	mu      sync.Mutex
	drained *sync.Cond
	tenants map[TenantID]*tenant
	closed  bool
	// unrecorded holds tenants this node closed while the registry could not
	// be written, with when each must wake; the heartbeat records them.
	unrecorded map[TenantID]time.Time
	// kick runs the waker at once, after a push made a sleeping tenant due.
	// node names this process in the registry; a restart is a new node.
	node      string
	started   time.Time
	transport http.RoundTripper
	beatMu    sync.Mutex
	lastBeat  time.Time

	kick chan struct{}
	run  context.Context
	stop context.CancelFunc
	wg   sync.WaitGroup
}

// FleetConfig sizes a fleet. Zero values take the defaults noted per field;
// DailyCalls, DormantAfter, ArchiveAfter and Sweep stay off at zero.
type FleetConfig struct {
	// DataDir holds fleet.db and tenants/<shard>/<id>/; with Database it
	// holds only this node's copies of the tenants it opened.
	DataDir string
	// Provider is the model shared by every tenant; Model names it.
	Provider   agent.Provider
	Model      string
	ModelLabel string
	// Connect gives a tenant its mail and calendar; nil uses the in-process
	// mock world inside the tenant's directory.
	Connect TenantConnect
	Now     func() time.Time
	Log     io.Writer

	// MaxActive bounds the assistants open at once (default 256). An owner's
	// request may close the least recently used quiet one to make room; the
	// waker stops below the bound so owners always find room.
	MaxActive int
	// IdleTTL is how long an assistant stays open after its owner's last
	// request (default 2 minutes).
	IdleTTL time.Duration
	// Turns bounds the model turns running at once across the fleet
	// (default 8); ChatTurns of them are kept for owners' chat (default a
	// quarter, at least one).
	Turns     int
	ChatTurns int
	// TenantWorkers bounds one tenant's concurrent turns (default 2).
	TenantWorkers int
	// DailyCalls is each tenant's model-call quota per local day; zero is
	// unlimited.
	DailyCalls int
	// DormantAfter stops waking a tenant whose owner has not made a request
	// for this long; its next request wakes it again. Zero keeps waking.
	DormantAfter time.Duration
	// ArchiveAfter compresses the data directory of a tenant whose owner
	// has been absent this long into one file; zero never archives. With
	// Database nothing is archived: a node drops its copy of a tenant it has
	// not opened for this long.
	ArchiveAfter time.Duration
	// Sweep wakes a sleeping tenant at least this often, for connectors
	// that cannot announce new mail; zero relies on the tenant's own timers.
	Sweep time.Duration
	// StallAfter closes an assistant that stayed busy without running or
	// waiting for a turn this long (default 10 minutes), and wakes it again
	// after StallBackoff (default 5 minutes).
	StallAfter   time.Duration
	StallBackoff time.Duration
	// WakeTick is how often the waker looks for due tenants (default 1s);
	// ReapTick how often open assistants are checked for sleep (default 5s);
	// JanitorTick how often dormancy and archiving run (default 10 minutes).
	WakeTick    time.Duration
	ReapTick    time.Duration
	JanitorTick time.Duration

	// Advertise is this node's base URL as the other nodes reach it, such as
	// http://10.0.0.3:8791. Setting it lets several fleet processes share
	// DataDir: each passes on requests for tenants another one holds. Empty,
	// the fleet holds DataDir alone.
	Advertise string
	// NodeTTL is how long a node may go without a heartbeat before the
	// others take its tenants (default 10s); BeatTick is how often a node
	// beats (default a fifth of NodeTTL).
	NodeTTL  time.Duration
	BeatTick time.Duration

	// Database is the URL of a PostgreSQL database, such as
	// postgres://user:password@host:5432/name. Set, the registry and every
	// tenant's data live there and DataDir is only this node's cache, so
	// nodes need no shared disk. Empty, both live in DataDir.
	Database string
	// Checkpoint is how often an open tenant's changes are copied to the
	// database (default 2s); a node that is lost loses at most this much of
	// a running turn. It applies only with Database.
	Checkpoint time.Duration
	// registryDir and offload are for tests: a SQLite registry in a directory
	// of its own, and tenants kept in it the way they are kept in PostgreSQL.
	registryDir string
	offload     bool

	// The rest is passed to every tenant's App unchanged.
	SchedulerTick time.Duration
	PollInterval  time.Duration
	RemindLead    time.Duration
	TurnLimit     time.Duration
	LeaseTTL      time.Duration
}

// TenantConnect opens one tenant's mail and calendar connector. extra closes
// what the connector does not own itself; it may be nil.
type TenantConnect func(ctx context.Context, tenant TenantID, dir string, now func() time.Time) (connector *Connector, extra func() error, err error)

var (
	// ErrFleetFull means the fleet holds MaxActive busy assistants.
	ErrFleetFull   = errors.New("fleet is at capacity")
	ErrFleetClosed = errors.New("fleet is closed")
)

const (
	// fleetHorizon is the longest a tenant sleeps without a timer of its own,
	// and how far ahead its calendar is read for reminders.
	fleetHorizon = 30 * 24 * time.Hour
	// unsureRetry is when a tenant whose calendar could not be read is looked
	// at again; failedRetry the same for one that could not be read at all
	// or failed to open.
	unsureRetry = 5 * time.Minute
	failedRetry = time.Minute
	// fleetParallel bounds concurrent opens by the waker and closes by the
	// reaper.
	fleetParallel = 4
)

type fleetCounters struct {
	activations, hibernations, evictions, rejections, stalls, restores, archives, retired, wakeFailures, pushes, forwards, takeovers, fences, abandons atomic.Int64
}

// tenant is one open (or opening, or closing) assistant. Fields other than
// those set before ready closes are guarded by Fleet.mu.
type tenant struct {
	id    TenantID
	ready chan struct{}
	gone  chan struct{}
	err   error

	app     *App
	meter   *tenantMeter
	handler http.Handler
	cancel  context.CancelFunc
	extra   func() error
	token   string
	limiter *tokenBucket
	// replica copies the tenant to the vault; synced closes when its
	// checkpoint loop has stopped. Both are nil without a vault.
	replica *replica
	synced  chan struct{}

	refs    int
	touched time.Time
	opened  time.Time
	closing bool
	// pushed records a push that arrived while the tenant was closing; it
	// is made due again once it sleeps.
	pushed bool
	// unquiet is when the reaper first saw the tenant busy with nothing
	// running and nothing waiting for a turn; only the reaper uses it.
	unquiet time.Time
}

// lastUse is when the tenant was last needed, for picking whom to evict.
func (t *tenant) lastUse() time.Time {
	if t.touched.After(t.opened) {
		return t.touched
	}
	return t.opened
}

// OpenFleet opens the registry. Tenants a previous process left open are
// made due now, so whatever it interrupted runs again.
func OpenFleet(ctx context.Context, config FleetConfig) (*Fleet, error) {
	if config.DataDir == "" || config.Provider == nil {
		return nil, errors.New("fleet requires a data directory and a model provider")
	}
	defaults := []struct {
		value *int
		def   int
	}{{&config.MaxActive, 256}, {&config.Turns, 8}, {&config.TenantWorkers, 2}}
	for _, d := range defaults {
		if *d.value <= 0 {
			*d.value = d.def
		}
	}
	if config.ChatTurns <= 0 {
		config.ChatTurns = max(1, config.Turns/4)
	}
	if config.ChatTurns >= config.Turns {
		return nil, fmt.Errorf("fleet needs more turns (%d) than it keeps for chat (%d)", config.Turns, config.ChatTurns)
	}
	durations := []struct {
		value *time.Duration
		def   time.Duration
	}{
		{&config.IdleTTL, 2 * time.Minute}, {&config.StallAfter, 10 * time.Minute}, {&config.StallBackoff, 5 * time.Minute},
		{&config.WakeTick, time.Second}, {&config.ReapTick, 5 * time.Second}, {&config.JanitorTick, 10 * time.Minute},
		{&config.NodeTTL, 10 * time.Second}, {&config.Checkpoint, 2 * time.Second},
	}
	for _, d := range durations {
		if *d.value <= 0 {
			*d.value = d.def
		}
	}
	if config.BeatTick <= 0 {
		config.BeatTick = config.NodeTTL / 5
	}
	if config.BeatTick >= config.NodeTTL {
		return nil, fmt.Errorf("fleet nodes must beat (%v) more often than they are given up (%v)", config.BeatTick, config.NodeTTL)
	}
	take := lockDataDir
	if config.Advertise != "" {
		if err := checkAdvertise(config.Advertise); err != nil {
			return nil, err
		}
		take = shareDataDir
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Log == nil {
		config.Log = io.Discard
	}
	if config.Connect == nil {
		config.Connect = MockTenantConnect
	}
	if err := os.MkdirAll(filepath.Join(config.DataDir, "tenants"), 0o700); err != nil {
		return nil, err
	}
	// Two fleets that do not know of each other would wake, archive and
	// delete the same tenants; the same lock also keeps a daemon off the
	// fleet's root. The nodes of a cluster hold it shared.
	unlock, err := take(config.DataDir)
	if err != nil {
		return nil, err
	}
	home := config.DataDir
	if config.registryDir != "" {
		home = config.registryDir
	}
	database, err := openFleetDB(ctx, home, config.Database)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("open fleet registry: %w", err), unlock())
	}
	registry := &registry{database}
	run, stop := context.WithCancel(context.Background())
	fleet := &Fleet{
		config: config, registry: registry, unlock: unlock, gate: newTurnGate(config.Turns, config.ChatTurns),
		now: config.Now, log: &lockedWriter{w: config.Log}, tenants: map[TenantID]*tenant{}, unrecorded: map[TenantID]time.Time{}, run: run, stop: stop, kick: make(chan struct{}, 1),
		node: randomHex(8), started: config.Now(), lastBeat: config.Now(), transport: nodeTransport(),
	}
	if config.Database != "" || config.offload {
		fleet.vault = &vault{db: database, now: config.Now}
	}
	fleet.drained = sync.NewCond(&fleet.mu)
	if err := fleet.join(ctx); err != nil {
		stop()
		return nil, errors.Join(err, registry.Close(), unlock())
	}
	return fleet, nil
}

// join enters the node in the registry and makes the tenants that a stopped
// node left open due now, so whatever it interrupted runs again. A fleet
// that holds the directory alone knows every other node has stopped.
func (f *Fleet) join(ctx context.Context) error {
	if !f.clustered() {
		if err := f.registry.leave(ctx, f.node, true); err != nil {
			return err
		}
	}
	if err := f.registry.beat(ctx, f.node, f.config.Advertise, f.started, f.started, 0, f.config.MaxActive); err != nil {
		return err
	}
	recovered, err := f.registry.recoverOrphans(ctx, f.started, f.alive())
	if err != nil {
		return err
	}
	if recovered > 0 {
		f.counters.takeovers.Add(int64(recovered))
		fmt.Fprintf(f.log, "fleet: %d tenants were open when a node stopped; waking them\n", recovered)
	}
	return nil
}

// MockTenantConnect gives a tenant the in-process mock mail and calendar,
// stored in the tenant's own directory.
func MockTenantConnect(ctx context.Context, _ TenantID, dir string, now func() time.Time) (*Connector, func() error, error) {
	world, err := OpenWorld(ctx, filepath.Join(dir, "mock-mcp", "world.db"), now)
	if err != nil {
		return nil, nil, err
	}
	client, err := NewInProcessMock(world)
	if err != nil {
		return nil, nil, errors.Join(err, world.Close())
	}
	return NewConnector(client, "in-process"), world.Close, nil
}

// Start runs the waker, the reaper and the janitor until Close, and in a
// cluster the heartbeat.
func (f *Fleet) Start() {
	type loop struct {
		every time.Duration
		step  func(context.Context)
		kick  <-chan struct{}
	}
	loops := []loop{{f.config.WakeTick, f.wakeDue, f.kick}, {f.config.ReapTick, f.reap, nil}, {f.config.JanitorTick, f.janitor, nil}}
	if f.clustered() {
		// A fleet that holds its directory alone has nobody to tell, and an
		// idle one should not write to its registry every few seconds.
		loops = append(loops, loop{f.config.BeatTick, f.heartbeat, nil})
	}
	f.wg.Add(len(loops))
	for _, loop := range loops {
		go func() {
			defer f.wg.Done()
			ticker := time.NewTicker(loop.every)
			defer ticker.Stop()
			for {
				select {
				case <-f.run.Done():
					return
				case <-ticker.C:
					loop.step(f.run)
				case <-loop.kick:
					loop.step(f.run)
				}
			}
		}()
	}
}

// Close stops the fleet's loops and puts every open assistant to sleep,
// recording when each must wake. Turns in flight are interrupted; their
// triggers are requeued when the tenant opens again, which is due at once.
func (f *Fleet) Close() error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil
	}
	f.closed = true
	f.mu.Unlock()
	f.stop()
	f.wg.Wait()
	for {
		f.mu.Lock()
		open := []*tenant{}
		waits := []chan struct{}{}
		for _, t := range f.tenants {
			switch {
			case t.app == nil && t.err == nil && !t.closing:
				waits = append(waits, t.ready)
			case t.closing:
				waits = append(waits, t.gone)
			default:
				t.closing = true
				open = append(open, t)
			}
		}
		f.mu.Unlock()
		if len(open) == 0 && len(waits) == 0 {
			break
		}
		parallel(open, fleetParallel, func(t *tenant) { f.hibernate(t, time.Time{}) })
		for _, wait := range waits {
			<-wait
		}
	}
	leave, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return errors.Join(f.registry.leave(leave, f.node, false), f.registry.Close(), f.unlock())
}

// ---- tenants ----

// CreateTenant registers a tenant and returns its owner's bearer token, which
// the fleet keeps only as a hash. An empty id is generated. No files are
// created until the owner's first request.
func (f *Fleet) CreateTenant(ctx context.Context, id TenantID) (TenantID, string, error) {
	if id == "" {
		id = TenantID(randomHex(8))
	}
	if !id.Valid() {
		return "", "", fmt.Errorf("invalid tenant id %q: use 3-40 lowercase letters, digits or dashes", id)
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes(32))
	if err := f.registry.create(ctx, id, hashToken(token), f.now()); err != nil {
		return "", "", err
	}
	return id, token, nil
}

// Tenant reads one tenant's row and today's usage.
func (f *Fleet) Tenant(ctx context.Context, id TenantID) (TenantRecord, TenantUsage, error) {
	record, ok, err := f.registry.get(ctx, id)
	if err != nil || !ok {
		return TenantRecord{}, TenantUsage{}, errors.Join(err, ErrTenantUnknown)
	}
	usage, err := f.registry.usage(ctx, id, f.now().Format(slotLayout))
	return record, usage, err
}

// Tenants pages through the registry in ID order.
func (f *Fleet) Tenants(ctx context.Context, after TenantID, limit int) ([]TenantRecord, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	return f.registry.list(ctx, after, limit)
}

// DeleteTenant removes a tenant and all its data. Requests already inside it
// finish first; later ones find no tenant. A tenant that another node holds
// is not removed here: the error names that node, which must close it first.
func (f *Fleet) DeleteTenant(ctx context.Context, id TenantID) error {
	if !id.Valid() {
		return ErrTenantUnknown
	}
	removed, err := f.registry.remove(ctx, id, f.node, f.alive())
	if err != nil {
		return err
	}
	if !removed {
		if _, ok, err := f.registry.get(ctx, id); err != nil || !ok {
			return errors.Join(err, ErrTenantUnknown)
		}
		node, addr, held, err := f.registry.holder(ctx, id, f.alive())
		if err != nil || !held {
			return errors.Join(err, errMoving)
		}
		return &elsewhere{node: node, addr: addr}
	}
	for {
		f.mu.Lock()
		t, ok := f.tenants[id]
		if !ok {
			f.mu.Unlock()
			break
		}
		if t.closing || t.app == nil {
			wait := t.gone
			if !t.closing {
				wait = t.ready
			}
			f.mu.Unlock()
			<-wait
			continue
		}
		t.closing = true
		f.mu.Unlock()
		f.hibernate(t, time.Time{})
	}
	// Usage a winding-down turn recorded after the first removal goes too.
	dir := f.dir(id)
	_, err = f.registry.remove(ctx, id, f.node, f.alive())
	return errors.Join(err, f.registry.unplace(ctx, id, f.node), os.RemoveAll(dir), removeIfExists(archivePath(dir)))
}

// Authenticate checks an owner's bearer token in constant time, against the
// open tenant when there is one and the registry otherwise.
func (f *Fleet) Authenticate(ctx context.Context, id TenantID, token string) bool {
	if !id.Valid() || token == "" {
		return false
	}
	presented := hashToken(token)
	f.mu.Lock()
	t, ok := f.tenants[id]
	stored := ""
	if ok && t.app != nil {
		stored = t.token
	}
	f.mu.Unlock()
	if stored == "" {
		record, ok, err := f.registry.get(ctx, id)
		if err != nil || !ok {
			return false
		}
		stored = record.tokenHash
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(stored)) == 1
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func randomHex(n int) string { return hex.EncodeToString(randomBytes(n)) }

// dir is the tenant's data directory. The shard level keeps any one directory
// small when the fleet holds many tenants.
func (f *Fleet) dir(id TenantID) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(f.config.DataDir, "tenants", hex.EncodeToString(sum[:1]), string(id))
}

// ---- activation ----

// Use runs fn with the tenant's assistant open, opening it first if it
// sleeps. An owner's use counts as presence: it keeps the assistant open for
// IdleTTL and may evict a quiet one to make room.
func (f *Fleet) Use(ctx context.Context, id TenantID, fn func(*App) error) error {
	t, err := f.acquire(ctx, id, true)
	if err != nil {
		return err
	}
	defer f.release(t)
	return fn(t.app)
}

// acquire returns the open tenant with a reference held, opening it when it
// sleeps. owner marks the owner's own request; the waker passes false and
// never evicts.
func (f *Fleet) acquire(ctx context.Context, id TenantID, owner bool) (*tenant, error) {
	for attempts := 0; ; attempts++ {
		if attempts > 8 {
			return nil, fmt.Errorf("tenant %s keeps closing while being opened", id)
		}
		f.mu.Lock()
		if f.closed {
			f.mu.Unlock()
			return nil, ErrFleetClosed
		}
		t, ok := f.tenants[id]
		if !ok {
			if len(f.tenants) >= f.ceiling(owner) {
				f.mu.Unlock()
				if !owner || !f.evict(ctx) {
					f.counters.rejections.Add(1)
					return nil, ErrFleetFull
				}
				continue
			}
			t = &tenant{id: id, ready: make(chan struct{}), gone: make(chan struct{}), refs: 1}
			f.tenants[id] = t
			f.mu.Unlock()
			err := f.open(context.WithoutCancel(ctx), t)
			f.mu.Lock()
			if err != nil {
				delete(f.tenants, id)
				t.err = err
				close(t.ready)
				close(t.gone)
				f.mu.Unlock()
				return nil, err
			}
			if owner {
				t.touched = f.now()
			}
			close(t.ready)
			f.mu.Unlock()
			return t, nil
		}
		var wait chan struct{}
		switch {
		case t.closing:
			wait = t.gone
		case t.app == nil:
			wait = t.ready
		default:
			t.refs++
			if owner {
				t.touched = f.now()
			}
			f.mu.Unlock()
			return t, nil
		}
		f.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-wait:
		}
		if t.err != nil {
			return nil, t.err
		}
	}
}

// ceiling is how many assistants may be open before this request: owners may
// use every slot, the waker leaves an eighth free for them.
func (f *Fleet) ceiling(owner bool) int {
	if owner {
		return f.config.MaxActive
	}
	return max(1, f.config.MaxActive-f.config.MaxActive/8)
}

func (f *Fleet) release(t *tenant) {
	f.mu.Lock()
	t.refs--
	if t.refs == 0 {
		f.drained.Broadcast()
	}
	f.mu.Unlock()
}

// open restores, provisions and starts one tenant's assistant.
func (f *Fleet) open(ctx context.Context, t *tenant) (err error) {
	record, ok, err := f.registry.get(ctx, t.id)
	if err != nil || !ok {
		return errors.Join(err, ErrTenantUnknown)
	}
	if err := f.place(ctx, t.id); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, f.registry.unplace(ctx, t.id, f.node))
		}
	}()
	dir := f.dir(t.id)
	var link *replica
	if f.vault != nil {
		if link, _, err = f.vault.attach(ctx, t.id, f.node, dir, record.CreatedAt); err != nil {
			return err
		}
		defer func() {
			if err != nil {
				err = errors.Join(err, link.abandon())
			}
		}()
	} else if err := f.restore(record, dir); err != nil {
		return fmt.Errorf("restore tenant %s: %w", t.id, err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	now := f.now()
	meter := &tenantMeter{fleet: f, id: t.id}
	usage, err := f.registry.usage(ctx, t.id, now.Format(slotLayout))
	if err != nil {
		return err
	}
	meter.day, meter.calls = usage.Day, usage.Calls
	meter.notified = f.config.DailyCalls > 0 && usage.Calls >= f.config.DailyCalls
	connector, extra, err := f.config.Connect(ctx, t.id, dir, f.now)
	if err != nil {
		return fmt.Errorf("connect tenant %s: %w", t.id, err)
	}
	if extra == nil {
		extra = func() error { return nil }
	}
	app, err := NewApp(ctx, Config{
		DataDir: dir, Connector: connector, Provider: meter.provider(f.config.Provider),
		Model: f.config.Model, ModelLabel: f.config.ModelLabel, Now: f.now,
		Log: &prefixWriter{w: f.log, prefix: "[" + string(t.id) + "] "}, Workers: f.config.TenantWorkers,
		TurnLimit: f.config.TurnLimit, SchedulerTick: f.config.SchedulerTick, PollInterval: f.config.PollInterval,
		RemindLead: f.config.RemindLead, LeaseTTL: f.config.LeaseTTL, Admission: meter,
	})
	if err != nil {
		return errors.Join(fmt.Errorf("open tenant %s: %w", t.id, err), extra())
	}
	meter.post = func(text string) {
		if _, err := app.Store.Post(context.Background(), ChannelMessage{Role: RoleSystem, Text: text}, f.now()); err != nil {
			fmt.Fprintf(f.log, "fleet: post to %s: %v\n", t.id, err)
		}
	}
	if err := f.registry.activated(ctx, t.id); err != nil {
		return errors.Join(err, app.Close(ctx), extra())
	}
	run, cancel := context.WithCancel(f.run)
	if link != nil {
		t.replica, t.synced = link, make(chan struct{})
		// An approved effect leaves the node only after the approval is in
		// the vault.
		app.Outbox.durable = func(ctx context.Context) error { return f.barrier(ctx, t) }
	}
	app.Start(run)
	if link != nil {
		go f.checkpoints(run, t)
	}
	t.app, t.meter, t.handler, t.cancel, t.extra = app, meter, jsonOnly(app.routes()), cancel, extra
	t.token, t.limiter, t.opened = record.tokenHash, newTokenBucket(tenantRate, tenantBurst, time.Now()), now
	f.counters.activations.Add(1)
	return nil
}

// hibernate closes an assistant the caller marked closing: it stops the
// loops, lets requests inside it finish, reads when it must run again and
// records that in the registry. floor delays that time, for a stalled one.
func (f *Fleet) hibernate(t *tenant, floor time.Time) {
	t.cancel()
	t.app.Wait()
	if t.synced != nil {
		<-t.synced
	}
	f.mu.Lock()
	for t.refs > 0 {
		f.drained.Wait()
	}
	touched := t.touched
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	next := f.nextWake(ctx, t)
	if next.Before(floor) {
		next = floor
	}
	if err := errors.Join(t.app.Close(ctx), t.extra()); err != nil {
		fmt.Fprintf(f.log, "fleet: close %s: %v\n", t.id, err)
	}
	if t.replica != nil {
		// The last checkpoint goes in before the placement is given up. If
		// it fails the vault keeps the one before, and the node's copy is
		// read from the vault again before anyone uses it.
		if err := t.replica.seal(ctx); err != nil && !errors.Is(err, errFenced) {
			fmt.Fprintf(f.log, "fleet: store %s: %v\n", t.id, err)
		}
	}
	// When another node took the tenant for dead its row is that node's now,
	// and it wakes the tenant itself.
	_, unrecorded := f.registry.hibernated(ctx, t.id, f.node, next, touched)
	if unrecorded != nil {
		fmt.Fprintf(f.log, "fleet: record sleep of %s: %v\n", t.id, unrecorded)
	}
	f.counters.hibernations.Add(1)
	f.mu.Lock()
	delete(f.tenants, t.id)
	if unrecorded != nil {
		// The registry still says this node runs it, so nobody would wake
		// it. The heartbeat writes the record once the registry answers.
		f.unrecorded[t.id] = next
	}
	pushed := t.pushed
	f.mu.Unlock()
	close(t.gone)
	if pushed {
		if _, err := f.nudge(ctx, t.id); err != nil && !errors.Is(err, ErrTenantUnknown) {
			fmt.Fprintf(f.log, "fleet: wake %s after a push: %v\n", t.id, err)
		}
	}
}

// nextWake is when a closed assistant must run again: its earliest timer or
// queued work (held work at the next day), now for work a close interrupted,
// and no later than the sweep or the calendar horizon.
func (f *Fleet) nextWake(ctx context.Context, t *tenant) time.Time {
	now := f.now()
	standby, err := t.app.Standby(ctx, fleetHorizon)
	if err != nil {
		fmt.Fprintf(f.log, "fleet: read standby of %s: %v\n", t.id, err)
		return now.Add(failedRetry)
	}
	if standby.Running > 0 {
		return now
	}
	held, resume := t.meter.hold(now)
	next := standby.Next(held, resume)
	limit := fleetHorizon
	if f.config.Sweep > 0 {
		limit = f.config.Sweep
	}
	if standby.Unsure {
		limit = min(limit, unsureRetry)
	}
	return earliest(next, now.Add(limit))
}

// quiet reads whether an open assistant may be closed now.
func (f *Fleet) quiet(ctx context.Context, t *tenant, now time.Time) (Standby, bool) {
	standby, err := t.app.Standby(ctx, fleetHorizon)
	if err != nil {
		fmt.Fprintf(f.log, "fleet: read standby of %s: %v\n", t.id, err)
		return Standby{}, false
	}
	return standby, standby.Quiet(now, t.meter.Held())
}

// claimIdle marks the tenant closing if nothing holds it.
func (f *Fleet) claimIdle(t *tenant) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t.refs > 0 || t.closing || t.app == nil {
		return false
	}
	t.closing = true
	return true
}

// evict closes the least recently used quiet assistant to make room for an
// owner's request.
func (f *Fleet) evict(ctx context.Context) bool {
	f.mu.Lock()
	candidates := []*tenant{}
	for _, t := range f.tenants {
		if t.app != nil && t.refs == 0 && !t.closing {
			candidates = append(candidates, t)
		}
	}
	f.mu.Unlock()
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].lastUse().Before(candidates[j].lastUse()) })
	now := f.now()
	for i, t := range candidates {
		if i == 16 {
			break
		}
		if _, quiet := f.quiet(ctx, t, now); quiet && f.claimIdle(t) {
			f.hibernate(t, time.Time{})
			f.counters.evictions.Add(1)
			return true
		}
	}
	return false
}

// reap puts to sleep every open assistant whose owner has gone and that has
// nothing to do, and closes one that stays busy without making progress.
func (f *Fleet) reap(ctx context.Context) {
	f.recordSleeps(ctx)
	now := f.now()
	f.mu.Lock()
	candidates := []*tenant{}
	for _, t := range f.tenants {
		if t.app != nil && t.refs == 0 && !t.closing && now.Sub(t.touched) >= f.config.IdleTTL {
			candidates = append(candidates, t)
		}
	}
	f.mu.Unlock()
	parallel(candidates, fleetParallel, func(t *tenant) {
		if ctx.Err() != nil {
			return
		}
		standby, quiet := f.quiet(ctx, t, now)
		var floor time.Time
		switch {
		case quiet:
			t.unquiet = time.Time{}
		case standby.Running > 0 || t.meter.waiting.Load() > 0:
			t.unquiet = time.Time{}
			return
		case t.unquiet.IsZero():
			t.unquiet = now
			return
		case now.Sub(t.unquiet) < f.config.StallAfter:
			return
		default:
			floor = now.Add(f.config.StallBackoff)
		}
		if !f.claimIdle(t) {
			return
		}
		if !floor.IsZero() {
			f.counters.stalls.Add(1)
			fmt.Fprintf(f.log, "fleet: %s stayed busy without progress for %v; closing it until %s\n", t.id, f.config.StallAfter, floor.Format(time.RFC3339))
		}
		f.hibernate(t, floor)
	})
}

// recordSleeps writes the sleep of every tenant that was closed while the
// registry could not be written. It holds the tenant's slot meanwhile, so a
// request that opens the tenant waits and then finds a consistent row.
func (f *Fleet) recordSleeps(ctx context.Context) {
	f.mu.Lock()
	pending := make([]TenantID, 0, len(f.unrecorded))
	for id := range f.unrecorded {
		pending = append(pending, id)
	}
	f.mu.Unlock()
	for _, id := range pending {
		f.mu.Lock()
		next, waiting := f.unrecorded[id]
		if _, open := f.tenants[id]; open || !waiting || f.closed {
			// Opened here again: the row is right as it is.
			delete(f.unrecorded, id)
			f.mu.Unlock()
			continue
		}
		hold := &tenant{id: id, closing: true, gone: make(chan struct{})}
		f.tenants[id] = hold
		f.mu.Unlock()
		_, err := f.registry.hibernated(ctx, id, f.node, next, time.Time{})
		f.mu.Lock()
		delete(f.tenants, id)
		if err == nil {
			delete(f.unrecorded, id)
		}
		f.mu.Unlock()
		close(hold.gone)
	}
}

// wakeDue opens the sleeping tenants whose time has come, oldest first, up
// to the waker's ceiling. They then run like any open assistant and the
// reaper puts them back to sleep once they are quiet.
func (f *Fleet) wakeDue(ctx context.Context) {
	f.mu.Lock()
	room := f.ceiling(false) - len(f.tenants)
	f.mu.Unlock()
	if room <= 0 {
		return
	}
	now := f.now()
	var cutoff time.Time
	if f.config.DormantAfter > 0 {
		cutoff = now.Add(-f.config.DormantAfter)
	}
	due, err := f.registry.due(ctx, now, cutoff, min(room, 64))
	if err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(f.log, "fleet: list due tenants: %v\n", err)
		}
		return
	}
	parallel(due, fleetParallel, func(id TenantID) {
		t, err := f.acquire(ctx, id, false)
		if err != nil {
			var away *elsewhere
			if errors.Is(err, ErrFleetFull) || errors.Is(err, ErrFleetClosed) || ctx.Err() != nil ||
				errors.As(err, &away) || errors.Is(err, errMoving) || errors.Is(err, ErrTenantUnknown) {
				// Another node woke it first, or it was deleted meanwhile.
				return
			}
			f.counters.wakeFailures.Add(1)
			fmt.Fprintf(f.log, "fleet: wake %s: %v\n", id, err)
			if err := f.registry.postpone(context.WithoutCancel(ctx), id, f.now().Add(failedRetry)); err != nil {
				fmt.Fprintf(f.log, "fleet: postpone %s: %v\n", id, err)
			}
			return
		}
		f.release(t)
	})
}

// ---- push ----

// PushOutcome says what a provider's push did to a tenant.
type PushOutcome string

const (
	// PushPolled: the assistant was open and polled its mailbox at once.
	PushPolled PushOutcome = "polled"
	// PushScheduled: it sleeps and the waker opens it now.
	PushScheduled PushOutcome = "scheduled"
	// PushDeferred: it was never opened or its owner is dormant, so it is
	// not woken; it catches up from its cursor when its owner returns.
	PushDeferred PushOutcome = "deferred"
)

// Push tells a tenant that its mailbox or calendar changed. The push carries
// no content: the assistant reads what changed through its own connector, so
// a forged push can wake a tenant but cannot put anything in it. Repeated
// pushes coalesce: an open assistant polls once more, and a sleeping one is
// due now, which it already is after the first. The poller's mail cursor and
// ingested IDs remove duplicates.
func (f *Fleet) Push(ctx context.Context, id TenantID) (PushOutcome, error) {
	if !id.Valid() {
		return "", ErrTenantUnknown
	}
	f.counters.pushes.Add(1)
	for {
		f.mu.Lock()
		if f.closed {
			f.mu.Unlock()
			return "", ErrFleetClosed
		}
		t := f.tenants[id]
		switch {
		case t == nil:
			f.mu.Unlock()
			return f.nudge(ctx, id)
		case t.closing:
			// Its next wake is being recorded from state that does not
			// include this change; hibernate makes it due again.
			t.pushed = true
			f.mu.Unlock()
			return PushScheduled, nil
		}
		select {
		case <-t.ready:
		default:
			// Still opening: its first poll may have run before this change.
			ready := t.ready
			f.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-ready:
			}
			continue
		}
		// Holding a reference keeps the reaper from closing it before the
		// poll has turned the new mail into work it can see.
		t.refs++
		f.mu.Unlock()
		_, err := t.app.Poller.PollOnce(ctx)
		t.app.Scheduler.Wake()
		f.release(t)
		return PushPolled, err
	}
}

// nudge makes a sleeping tenant due now, unless it was never opened or its
// owner is dormant, and runs the waker.
func (f *Fleet) nudge(ctx context.Context, id TenantID) (PushOutcome, error) {
	now := f.now()
	var cutoff time.Time
	if f.config.DormantAfter > 0 {
		cutoff = now.Add(-f.config.DormantAfter)
	}
	due, err := f.registry.nudge(ctx, id, now, cutoff)
	if err != nil {
		return "", err
	}
	if !due {
		if _, ok, err := f.registry.get(ctx, id); err != nil || !ok {
			return "", errors.Join(err, ErrTenantUnknown)
		}
		// Open on another node: that node must poll, so the caller passes
		// the push on.
		if node, addr, held, err := f.registry.holder(ctx, id, f.alive()); err != nil {
			return "", err
		} else if held && node != f.node {
			return "", &elsewhere{node: node, addr: addr}
		}
		return PushDeferred, nil
	}
	select {
	case f.kick <- struct{}{}:
	default:
	}
	return PushScheduled, nil
}

// janitor stops waking dormant tenants and archives long-absent ones.
func (f *Fleet) janitor(ctx context.Context) {
	now := f.now()
	if f.config.DormantAfter > 0 {
		retired, err := f.registry.retire(ctx, now.Add(-f.config.DormantAfter))
		if err != nil && ctx.Err() == nil {
			fmt.Fprintf(f.log, "fleet: retire dormant tenants: %v\n", err)
		}
		f.counters.retired.Add(int64(retired))
	}
	switch {
	case f.config.ArchiveAfter <= 0:
	case f.vault != nil:
		// The vault has the tenants; an absent one costs this node only its
		// cached copy, which is dropped instead of archived.
		f.sweepCache(ctx, time.Now().Add(-f.config.ArchiveAfter))
	default:
		f.archiveAbsent(ctx, now.Add(-f.config.ArchiveAfter))
	}
}

// ---- stats ----

// FleetStats is the operator's view of the fleet.
type FleetStats struct {
	Tenants      map[TenantState]int `json:"tenants"`
	Active       int                 `json:"active"`
	MaxActive    int                 `json:"max_active"`
	Turns        int                 `json:"turns"`
	TurnsInUse   int                 `json:"turns_in_use"`
	TurnsWaiting int64               `json:"turns_waiting"`
	Activations  int64               `json:"activations"`
	Hibernations int64               `json:"hibernations"`
	Evictions    int64               `json:"evictions"`
	Rejections   int64               `json:"rejections"`
	Stalls       int64               `json:"stalls"`
	Restores     int64               `json:"restores"`
	Archives     int64               `json:"archives"`
	Retired      int64               `json:"retired"`
	WakeFailures int64               `json:"wake_failures"`
	Pushes       int64               `json:"pushes"`
	// Node is this process; the counters above are its own. Nodes lists
	// every node in the registry. Forwards counts requests passed on to
	// another node, Takeovers tenants recovered from stopped nodes, Fences
	// the times this node closed its tenants after missing heartbeats.
	Node      string     `json:"node"`
	Nodes     []NodeInfo `json:"nodes"`
	Forwards  int64      `json:"forwards"`
	Takeovers int64      `json:"takeovers"`
	Fences    int64      `json:"fences"`
	// Database is the engine of the fleet's database. With a vault, the
	// rest counts this node's traffic with it: checkpoints written and their
	// pages and bytes, tenants read from it and their bytes, opens that
	// reused the node's cached copy, and tenants the node gave up because
	// the vault refused its checkpoint.
	Database         string `json:"database"`
	Vault            bool   `json:"vault"`
	Checkpoints      int64  `json:"checkpoints"`
	CheckpointPages  int64  `json:"checkpoint_pages"`
	CheckpointBytes  int64  `json:"checkpoint_bytes"`
	VaultPulls       int64  `json:"vault_pulls"`
	VaultPulledBytes int64  `json:"vault_pulled_bytes"`
	CacheReuses      int64  `json:"cache_reuses"`
	Abandons         int64  `json:"abandons"`
	// ModelCalls counts calls since the process started; Today sums every
	// tenant's use on the current local day from the registry.
	ModelCalls int64       `json:"model_calls"`
	Today      TenantUsage `json:"today"`
}

func (f *Fleet) Stats(ctx context.Context) (FleetStats, error) {
	counts, err := f.registry.counts(ctx)
	if err != nil {
		return FleetStats{}, err
	}
	today, err := f.registry.usageTotal(ctx, f.now().Format(slotLayout))
	if err != nil {
		return FleetStats{}, err
	}
	nodes, err := f.Nodes(ctx)
	if err != nil {
		return FleetStats{}, err
	}
	f.mu.Lock()
	active := len(f.tenants)
	f.mu.Unlock()
	c := &f.counters
	stats := FleetStats{
		Tenants: counts, Active: active, MaxActive: f.config.MaxActive,
		Turns: f.config.Turns, TurnsInUse: f.gate.inUse(), TurnsWaiting: f.gate.waiting.Load(),
		Activations: c.activations.Load(), Hibernations: c.hibernations.Load(), Evictions: c.evictions.Load(),
		Rejections: c.rejections.Load(), Stalls: c.stalls.Load(), Restores: c.restores.Load(),
		Archives: c.archives.Load(), Retired: c.retired.Load(), WakeFailures: c.wakeFailures.Load(), Pushes: c.pushes.Load(),
		Node: f.node, Nodes: nodes, Forwards: c.forwards.Load(), Takeovers: c.takeovers.Load(), Fences: c.fences.Load(),
		ModelCalls: f.calls.Load(), Today: today,
		Database: f.registry.db.Engine(), Vault: f.vault != nil, Abandons: c.abandons.Load(),
	}
	if f.vault != nil {
		v := &f.vault.counters
		stats.Checkpoints, stats.CheckpointPages, stats.CheckpointBytes = v.checkpoints.Load(), v.pages.Load(), v.bytes.Load()
		stats.VaultPulls, stats.VaultPulledBytes, stats.CacheReuses = v.pulls.Load(), v.pulledBytes.Load(), v.reuses.Load()
	}
	return stats, nil
}

// ---- helpers ----

// parallel runs fn over items with at most n at once and returns when all
// are done.
func parallel[T any](items []T, n int, fn func(T)) {
	slots := make(chan struct{}, n)
	var wg sync.WaitGroup
	for _, item := range items {
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			fn(item)
		}()
	}
	wg.Wait()
}

// lockedWriter serialises writes from every tenant to one log.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// prefixWriter marks each write with the tenant it came from.
type prefixWriter struct {
	w      io.Writer
	prefix string
}

func (p *prefixWriter) Write(b []byte) (int, error) {
	if _, err := p.w.Write(append([]byte(p.prefix), b...)); err != nil {
		return 0, err
	}
	return len(b), nil
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
