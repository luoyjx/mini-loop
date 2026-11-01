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
}

type App struct {
	Store      *Store
	Connector  *Connector
	Router     *Router
	Scheduler  *Scheduler
	Poller     *Poller
	Outbox     *Outbox
	Preparer   *Preparer
	Manager    *agent.SessionManager
	Health     *Health
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
	router := NewRouter(RouterConfig{Store: store, Connector: config.Connector, Rules: DefaultRules(), Now: now, Log: config.Log, Workers: config.Workers, TurnLimit: config.TurnLimit})
	router.triage = &Triage{provider: observeProvider(config.Provider), model: config.Model}
	catalog, err := NewToolCatalog(store, config.Connector, router, now)
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
	scheduler := NewScheduler(store, config.Connector, router, now, config.SchedulerTick, config.RemindLead, config.Log)
	scheduler.health = health
	outbox := NewOutbox(store, config.Connector, now, config.Log)
	outbox.settled = scheduler.Wake
	outbox.health = health
	poller := NewPoller(store, config.Connector, router, now, config.PollInterval, config.Log)
	poller.health = health
	return &App{
		Store: store, Connector: config.Connector, Router: router, Scheduler: scheduler,
		Poller: poller, Outbox: outbox, Preparer: preparer, Manager: manager, Health: health,
		ModelLabel: label, now: now, states: states, release: release,
	}, nil
}

// Start runs the dispatcher, scheduler (routines, task checks, event
// reminders, watchdog), mail poller, outbox and preparation runner until ctx
// ends.
// The first poll runs before Start returns, so a first-time daemon pins its
// mail cursor to the service's head before anything else can deliver.
func (app *App) Start(ctx context.Context) {
	_, _ = app.Poller.PollOnce(ctx)
	loops := []func(context.Context){app.Router.Run, app.Scheduler.Run, app.Poller.Run, app.Outbox.Run, app.Preparer.Run}
	app.wg.Add(len(loops))
	for _, loop := range loops {
		go func() { defer app.wg.Done(); loop(ctx) }()
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
	return true, nil
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
