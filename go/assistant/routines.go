package assistant

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

const slotLayout = "2006-01-02"

// Scheduler is the daemon's clock. Each tick it claims three kinds of
// time-based triggers, each in one SQLite transaction with its trigger so it
// fires once across ticks, restarts and concurrent daemons:
//
//   - routines: one slot per (routine, local day). Misfire policy: a slot
//     whose time already passed today and was never claimed runs once as a
//     catch-up, whether the daemon was stopped or the Mac was asleep; earlier
//     days are not back-filled.
//   - task checks: every active task whose next check is due (see
//     Store.ClaimDueTasks). Missed checks run on the first tick after a
//     restart or wake.
//   - event reminders: one per confirmed event, read through MCP
//     calendar_list, when it starts within the lead time. An event that
//     already started while the daemon was down or asleep is not reminded.
//
// While the user has paused processing a tick claims nothing; the first tick
// after resuming catches up as after downtime, and an event that started
// during the pause is not reminded.
type Scheduler struct {
	store     *Store
	connector *Connector
	router    *Router
	now       clock
	tick      time.Duration
	lead      time.Duration
	log       io.Writer
	wake      chan struct{}
	// health counts calendar reads that fail in a row (nil records nothing).
	health *Health

	mu       sync.Mutex
	calendar string
}

const defaultRemindLead = 15 * time.Minute

func NewScheduler(store *Store, connector *Connector, router *Router, now clock, tick, lead time.Duration, log io.Writer) *Scheduler {
	if now == nil {
		now = time.Now
	}
	if tick <= 0 {
		tick = 20 * time.Second
	}
	if lead <= 0 {
		lead = defaultRemindLead
	}
	if log == nil {
		log = io.Discard
	}
	return &Scheduler{store: store, connector: connector, router: router, now: now, tick: tick, lead: lead, log: log, wake: make(chan struct{}, 1)}
}

// Wake asks for a tick now, for example after a task check was brought
// forward; the ticker is the fallback.
func (s *Scheduler) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	s.Tick(ctx)
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
		s.Tick(ctx)
	}
}

// Tick runs every time source once and returns how many triggers it queued.
func (s *Scheduler) Tick(ctx context.Context) int {
	if _, paused := s.store.Paused(); paused {
		return 0
	}
	return s.Due(ctx) + s.DueTasks(ctx) + s.Upcoming(ctx)
}

// Due claims every routine whose slot time has passed today. A claim more
// than two ticks late is labelled a catch-up: the daemon was down or the
// machine slept through the slot (tickers do not fire during sleep).
func (s *Scheduler) Due(ctx context.Context) int {
	routines, err := s.store.Routines(ctx)
	if err != nil {
		fmt.Fprintf(s.log, "assistant: routines: %v\n", err)
		return 0
	}
	now := s.now()
	fired := 0
	for _, routine := range routines {
		if !routine.Enabled {
			continue
		}
		at := time.Date(now.Year(), now.Month(), now.Day(), routine.Hour, routine.Minute, 0, 0, now.Location())
		if now.Before(at) {
			continue
		}
		catchup := now.Sub(at) > 2*s.tick
		if _, claimed, err := s.store.ClaimRoutineSlot(ctx, routine, now.Format(slotLayout), catchup, now); err != nil {
			fmt.Fprintf(s.log, "assistant: claim routine %s: %v\n", routine.ID, err)
		} else if claimed {
			fired++
		}
	}
	if fired > 0 {
		s.router.Wake()
	}
	return fired
}

// RunNow fires a routine immediately in its own manual slot.
func (s *Scheduler) RunNow(ctx context.Context, id RoutineID) (int64, error) {
	routine, ok, err := s.store.Routine(ctx, id)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("routine %q not found", id)
	}
	now := s.now()
	trigger, _, err := s.store.ClaimRoutineSlot(ctx, routine, fmt.Sprintf("manual-%d", now.UnixMilli()), false, now)
	if err == nil {
		s.router.Wake()
	}
	return trigger, err
}

// DueTasks wakes every task whose next check is due. A check more than two
// ticks late is labelled a catch-up.
func (s *Scheduler) DueTasks(ctx context.Context) int {
	woken, stalled, err := s.store.ClaimDueTasks(ctx, s.now(), 2*s.tick)
	if err != nil {
		if ctx.Err() == nil {
			fmt.Fprintf(s.log, "assistant: claim tasks: %v\n", err)
		}
		return 0
	}
	if stalled > 0 {
		fmt.Fprintf(s.log, "assistant: %d task(s) ran out of checks and were handed to the user\n", stalled)
	}
	if woken > 0 {
		s.router.Wake()
	}
	return woken
}

// Upcoming reminds once per confirmed event that starts within the lead time.
// The calendar is read through MCP; an outage is logged once and retried on
// the next tick.
func (s *Scheduler) Upcoming(ctx context.Context) int {
	now := s.now()
	events, err := s.connector.Events(ctx, now, now.Add(s.lead))
	if ctx.Err() == nil {
		s.health.Record(HealthCalendar, err, now)
	}
	s.record(err)
	if err != nil {
		return 0
	}
	fired := 0
	for _, event := range events {
		if event.Status != EventConfirmed || !event.Start.After(now) || event.Start.Sub(now) > s.lead {
			continue
		}
		if _, claimed, err := s.store.ClaimReminder(ctx, event, now); err != nil {
			fmt.Fprintf(s.log, "assistant: claim reminder %s: %v\n", event.ID, err)
		} else if claimed {
			fired++
		}
	}
	if fired > 0 {
		s.router.Wake()
	}
	return fired
}

func (s *Scheduler) record(err error) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	s.mu.Lock()
	changed := message != s.calendar
	s.calendar = message
	s.mu.Unlock()
	if changed && err != nil {
		fmt.Fprintf(s.log, "assistant: calendar watch: %v\n", err)
	}
}
