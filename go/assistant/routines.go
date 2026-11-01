package assistant

import (
	"context"
	"fmt"
	"io"
	"time"
)

const slotLayout = "2006-01-02"

// Scheduler turns routines into triggers. Each (routine, local day) slot is
// claimed in SQLite together with its trigger, so a slot fires once across
// ticks, restarts and concurrent daemons. Misfire policy: a slot whose time
// already passed today and was never claimed runs once as a catch-up, whether
// the daemon was stopped or the Mac was asleep; earlier days are not
// back-filled.
type Scheduler struct {
	store  *Store
	router *Router
	now    clock
	tick   time.Duration
	log    io.Writer
}

func NewScheduler(store *Store, router *Router, now clock, tick time.Duration, log io.Writer) *Scheduler {
	if now == nil {
		now = time.Now
	}
	if tick <= 0 {
		tick = 20 * time.Second
	}
	if log == nil {
		log = io.Discard
	}
	return &Scheduler{store: store, router: router, now: now, tick: tick, log: log}
}

func (s *Scheduler) Run(ctx context.Context) {
	s.Due(ctx)
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Due(ctx)
		}
	}
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
