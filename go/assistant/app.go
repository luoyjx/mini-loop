package assistant

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
	"github.com/luoyjx/mini-loop/go/memory"
	"github.com/luoyjx/mini-loop/go/sqlitestate"
)

// Config wires one assistant daemon. DataDir holds three durable parts:
// assistant.db (triggers, mail cursor, tasks, outbox, channel, ledger, pause
// switch, dismissals), agent.db (session transcripts, action journal, leases)
// and memory/ (owner facts), plus daemon.lock, which one daemon holds while it
// runs. Mail and calendar live behind Connector; NewApp takes ownership of it
// and closes it on failure or in Close.
type Config struct {
	DataDir       string
	Connector     *Connector
	Provider      agent.Provider
	Model         string
	ModelLabel    string
	Now           func() time.Time
	Log           io.Writer
	Workers       int
	TurnLimit     time.Duration
	SchedulerTick time.Duration
	PollInterval  time.Duration
	RemindLead    time.Duration
	LeaseTTL      time.Duration
	// Embedder adds the dense retrieval leg; nil keeps memory search lexical.
	Embedder Embedder
	// Remotes are the real messaging channels the owner also talks through
	// (Telegram, Discord, email); none by default, leaving only the page.
	Remotes []RemoteChannel
	// Admission is set by a host that runs many assistants in one process
	// (see Fleet) to bound the model work this one starts; nil for a daemon.
	Admission Admission
}

type App struct {
	Store     *Store
	Connector *Connector
	Router    *Router
	Scheduler *Scheduler
	Poller    *Poller
	Outbox    *Outbox
	Preparer  *Preparer
	Manager   *agent.SessionManager
	// Memory is the owner's memory, shared with the workers' remember tool;
	// Search ranks it for memory_search, the per-turn prefetch and the page.
	Memory *memory.ScopedStore
	Search *MemorySearch
	Health *Health
	// Bridges connect the remote channels, one per channel.
	Bridges    []*Bridge
	ModelLabel string

	now     clock
	states  *sqlitestate.Store
	release func() error
	wg      sync.WaitGroup
}

// lockFile is the data directory's single-daemon lock.
const lockFile = "daemon.lock"

// ErrDataDirBusy refuses a second daemon on a data directory another daemon
// holds.
var ErrDataDirBusy = errors.New("data directory is in use by another assistant daemon")

func NewApp(ctx context.Context, config Config) (app *App, err error) {
	if config.Connector == nil {
		return nil, errors.New("assistant requires a mail/calendar connector")
	}
	closers := []func() error{config.Connector.Close}
	defer func() {
		if err != nil {
			for i := len(closers) - 1; i >= 0; i-- {
				err = errors.Join(err, closers[i]())
			}
		}
	}()
	if config.DataDir == "" || config.Provider == nil {
		return nil, errors.New("assistant requires a data directory and a model provider")
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	if config.LeaseTTL == 0 {
		config.LeaseTTL = 15 * time.Second
	}
	release, err := lockDataDir(config.DataDir)
	if err != nil {
		return nil, err
	}
	closers = append(closers, release)
	store, err := OpenStore(ctx, filepath.Join(config.DataDir, "assistant.db"))
	if err != nil {
		return nil, fmt.Errorf("open assistant store: %w", err)
	}
	closers = append(closers, store.Close)
	if err = store.Seed(ctx, now()); err != nil {
		return nil, fmt.Errorf("seed assistant: %w", err)
	}
	states, err := sqlitestate.Open(ctx, filepath.Join(config.DataDir, "agent.db"))
	if err != nil {
		return nil, fmt.Errorf("open agent state: %w", err)
	}
	closers = append(closers, states.Close)
	memories, err := memory.NewStore(ctx, filepath.Join(config.DataDir, "memory"), nil)
	if err != nil {
		return nil, fmt.Errorf("open memory: %w", err)
	}
	if err = SeedMemory(ctx, memories); err != nil {
		return nil, fmt.Errorf("seed memory: %w", err)
	}
	owned, err := memory.Bind(memories, memory.OwnerID(Owner))
	if err != nil {
		return nil, err
	}
	if err = refreshProfile(ctx, store, owned, now()); err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}
	router := NewRouter(RouterConfig{Store: store, Connector: config.Connector, Rules: DefaultRules(), Now: now, Log: config.Log, Workers: config.Workers, TurnLimit: config.TurnLimit, Admission: config.Admission})
	router.triage = &Triage{provider: observeProvider(config.Provider), model: config.Model}
	router.consolidator = NewConsolidator(store, config.Connector, owned, now)
	router.consolidator.aliaser = &Aliaser{provider: observeProvider(config.Provider), model: config.Model}
	router.consolidator.extractor = &FactExtractor{provider: observeProvider(config.Provider), model: config.Model}
	search := NewMemorySearch(owned, store)
	if config.Embedder != nil {
		search.vectors = NewVectorIndex(store, config.Embedder, now)
	}
	router.search = search
	catalog, err := NewToolCatalog(store, config.Connector, router, search, now)
	if err != nil {
		return nil, err
	}
	system := WorkerSystem
	memoryAuto := false
	manager, err := agent.NewSessionManager(agent.ManagerConfig{
		WorkspaceRoot: filepath.Join(config.DataDir, "workspaces"),
		StateLeaseTTL: config.LeaseTTL,
		Defaults:      agent.SessionDefaults{Model: config.Model, PermissionMode: agent.ModeInteractive, MaxRounds: 12, System: &system},
		Services: agent.ManagerServices{
			Provider: observeProvider(config.Provider), Tools: catalog, StateStore: states,
			Memory: memories, MemoryTools: true, MemoryAuto: &memoryAuto,
			Hooks: agent.GateHooks{Observers: []agent.ResultObserver{router}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("session manager: %w", err)
	}
	router.Attach(manager)
	if _, err = store.RequeueInterrupted(ctx, now()); err != nil {
		return nil, errors.Join(err, manager.Stop(ctx))
	}
	if err = store.RequeueJobs(ctx); err != nil {
		return nil, errors.Join(err, manager.Stop(ctx))
	}
	preparer := NewPreparer(store, router, manager, now, config.Log)
	router.preparer = preparer
	label := config.ModelLabel
	if label == "" {
		label = config.Model
	}
	health := NewHealth()
	router.health = health
	if search.vectors != nil {
		search.vectors.health = health
	}
	scheduler := NewScheduler(store, config.Connector, router, now, config.SchedulerTick, config.RemindLead, config.Log)
	scheduler.health = health
	outbox := NewOutbox(store, config.Connector, now, config.Log)
	outbox.settled = scheduler.Wake
	outbox.health = health
	poller := NewPoller(store, config.Connector, router, now, config.PollInterval, config.Log)
	poller.health = health
	bridges := []*Bridge{}
	seen := map[string]bool{}
	for _, remote := range config.Remotes {
		if seen[remote.Name()] {
			return nil, errors.Join(fmt.Errorf("remote channel %s configured twice", remote.Name()), manager.Stop(ctx))
		}
		seen[remote.Name()] = true
		bridges = append(bridges, NewBridge(remote, store, router.Wake, health, now, config.Log))
	}
	return &App{
		Store: store, Connector: config.Connector, Router: router, Scheduler: scheduler,
		Poller: poller, Outbox: outbox, Preparer: preparer, Manager: manager, Memory: owned, Search: search, Health: health,
		Bridges: bridges, ModelLabel: label, now: now, states: states, release: release,
	}, nil
}

// Start runs the dispatcher, scheduler (routines, task checks, event
// reminders, watchdog), mail poller, outbox, preparation runner and each
// remote channel's receive and delivery loops until ctx ends.
// The first poll runs before Start returns, so a first-time daemon pins its
// mail cursor to the service's head before anything else can deliver.
func (app *App) Start(ctx context.Context) {
	_, _ = app.Poller.PollOnce(ctx)
	loops := []func(context.Context){app.Router.Run, app.Scheduler.Run, app.Poller.Run, app.Outbox.Run, app.Preparer.Run}
	if app.Search.vectors != nil {
		loops = append(loops, app.warmVectors)
	}
	for _, bridge := range app.Bridges {
		loops = append(loops, bridge.ReceiveLoop, bridge.DeliverLoop)
	}
	app.wg.Add(len(loops))
	for _, loop := range loops {
		go func() { defer app.wg.Done(); loop(ctx) }()
	}
}

// warmVectors embeds the memories once at start, so the first search does
// not wait for the whole index, and probes the embedder when every vector
// was already stored. A failure only shows in the status.
func (app *App) warmVectors(ctx context.Context) {
	records, err := app.Memory.List(ctx)
	if err == nil {
		err = app.Search.vectors.Sync(ctx, records)
	}
	if err == nil {
		err = app.Search.vectors.Probe(ctx)
	}
	if err != nil && ctx.Err() == nil {
		fmt.Fprintf(app.Router.log, "assistant: embed memories: %v\n", err)
	}
}

// Close waits for the loops (their ctx must already be cancelled), stops the
// manager, closes both stores, stops the connector and releases the data
// directory. Interrupted triggers stay queued and approved proposals stay
// approved for the next process.
func (app *App) Close(ctx context.Context) error {
	app.wg.Wait()
	return errors.Join(app.Manager.Stop(ctx), app.states.Close(), app.Store.Close(), app.Connector.Close(), app.release())
}

// SetPaused pauses or resumes all processing for the user. Resuming wakes
// every loop so the backlog, due checks and today's missed routines catch up.
func (app *App) SetPaused(ctx context.Context, paused bool) (bool, error) {
	changed, err := app.Store.SetPaused(ctx, paused, "me（信道）", app.now())
	if err != nil || !changed || paused {
		return changed, err
	}
	app.Router.Wake()
	app.Outbox.Wake()
	app.Scheduler.Wake()
	app.Preparer.Wake()
	for _, bridge := range app.Bridges {
		bridge.Wake()
	}
	return true, nil
}

// Remotes is the page's view of the remote channels.
func (app *App) Remotes() []RemoteStatus {
	statuses := make([]RemoteStatus, 0, len(app.Bridges))
	for _, bridge := range app.Bridges {
		statuses = append(statuses, bridge.Status())
	}
	return statuses
}

// Attention is everything that waits for the user: the store's items, then
// components that keep failing.
func (app *App) Attention(ctx context.Context) ([]AttentionItem, error) {
	items, err := app.Store.Attention(ctx, app.now())
	if err != nil {
		return nil, err
	}
	return append(items, HealthItems(app.Health.Problems())...), nil
}
