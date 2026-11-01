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
// assistant.db (triggers, mail cursor, tasks, outbox, channel, ledger), agent.db
// (session transcripts, action journal, leases) and memory/ (owner facts).
// Mail and calendar live behind Connector; NewApp takes ownership of it and
// closes it on failure or in Close.
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
	Manager    *agent.SessionManager
	ModelLabel string

	now    clock
	states *sqlitestate.Store
	wg     sync.WaitGroup
}

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
			Provider: config.Provider, Tools: catalog, StateStore: states,
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
	label := config.ModelLabel
	if label == "" {
		label = config.Model
	}
	scheduler := NewScheduler(store, config.Connector, router, now, config.SchedulerTick, config.RemindLead, config.Log)
	outbox := NewOutbox(store, config.Connector, now, config.Log)
	outbox.settled = scheduler.Wake
	return &App{
		Store: store, Connector: config.Connector, Router: router, Scheduler: scheduler,
		Poller:  NewPoller(store, config.Connector, router, now, config.PollInterval, config.Log),
		Outbox:  outbox,
		Manager: manager, ModelLabel: label, now: now, states: states,
	}, nil
}

// Start runs the dispatcher, scheduler (routines, task checks, event
// reminders), mail poller and outbox until ctx ends.
// The first poll runs before Start returns, so a first-time daemon pins its
// mail cursor to the service's head before anything else can deliver.
func (app *App) Start(ctx context.Context) {
	_, _ = app.Poller.PollOnce(ctx)
	loops := []func(context.Context){app.Router.Run, app.Scheduler.Run, app.Poller.Run, app.Outbox.Run}
	app.wg.Add(len(loops))
	for _, loop := range loops {
		go func() { defer app.wg.Done(); loop(ctx) }()
	}
}

// Close waits for the loops (their ctx must already be cancelled), stops the
// manager, closes both stores and stops the connector. Interrupted triggers
// stay queued and approved proposals stay approved for the next process.
func (app *App) Close(ctx context.Context) error {
	app.wg.Wait()
	return errors.Join(app.Manager.Stop(ctx), app.states.Close(), app.Store.Close(), app.Connector.Close())
}
