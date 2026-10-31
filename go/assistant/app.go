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
// assistant.db (world, triggers, outbox, channel, ledger), agent.db (session
// transcripts, action journal, approvals) and memory/ (owner facts).
type Config struct {
	DataDir       string
	Provider      agent.Provider
	Model         string
	ModelLabel    string
	Now           func() time.Time
	Log           io.Writer
	Workers       int
	TurnLimit     time.Duration
	SchedulerTick time.Duration
	LeaseTTL      time.Duration
}

type App struct {
	Store      *Store
	Router     *Router
	Scheduler  *Scheduler
	Manager    *agent.SessionManager
	ModelLabel string

	now    clock
	states *sqlitestate.Store
	wg     sync.WaitGroup
}

func NewApp(ctx context.Context, config Config) (app *App, err error) {
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
	closers := []func() error{store.Close}
	defer func() {
		if err != nil {
			for i := len(closers) - 1; i >= 0; i-- {
				err = errors.Join(err, closers[i]())
			}
		}
	}()
	if err = store.Seed(ctx, now()); err != nil {
		return nil, fmt.Errorf("seed demo world: %w", err)
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
	router := NewRouter(RouterConfig{Store: store, Rules: DefaultRules(), Now: now, Log: config.Log, Workers: config.Workers, TurnLimit: config.TurnLimit})
	catalog, err := NewToolCatalog(store, router, now)
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
	return &App{Store: store, Router: router, Scheduler: NewScheduler(store, router, now, config.SchedulerTick, config.Log), Manager: manager, ModelLabel: label, now: now, states: states}, nil
}

// Start runs the dispatcher and the scheduler until ctx ends.
func (app *App) Start(ctx context.Context) {
	app.wg.Add(2)
	go func() { defer app.wg.Done(); app.Router.Run(ctx) }()
	go func() { defer app.wg.Done(); app.Scheduler.Run(ctx) }()
}

// Close waits for the loops (their ctx must already be cancelled), stops the
// manager and closes both stores. Interrupted triggers stay queued.
func (app *App) Close(ctx context.Context) error {
	app.wg.Wait()
	return errors.Join(app.Manager.Stop(ctx), app.states.Close(), app.Store.Close())
}
