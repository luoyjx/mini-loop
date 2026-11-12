package assistant

import (
	"context"
	"database/sql"
	"time"
)

// Standby tells a host that runs many assistants when this one may be put to
// sleep and when it has to run again. Everything in it is read from durable
// state, so a closed assistant loses nothing: the host reopens it at Next and
// the loops catch up exactly as after a restart.
type Standby struct {
	// Running counts turns and preparation jobs in flight.
	Running int
	// Work is when the earliest queued model work becomes runnable: a pending
	// trigger's not_before, or now for a queued job. Zero means none.
	Work time.Time
	// Timer is the earliest time source that needs no model: an approved
	// effect's next attempt, a task check, a routine slot or an event
	// reminder. Zero means none.
	Timer time.Time
	// Unsure means the calendar could not be read, so Timer may miss a
	// reminder; the host should look again soon.
	Unsure bool
}

// Quiet reports whether nothing is in flight or due at now. held tells it the
// host is holding this assistant's model work, which then cannot make it busy.
func (s Standby) Quiet(now time.Time, held bool) bool {
	if s.Running > 0 || due(s.Timer, now) {
		return false
	}
	return held || !due(s.Work, now)
}

// Next is when the assistant must be running again, or zero when only the
// user can give it something to do. Held model work waits until resume.
func (s Standby) Next(held bool, resume time.Time) time.Time {
	work := s.Work
	if held && !work.IsZero() && work.Before(resume) {
		work = resume
	}
	return earliest(s.Timer, work)
}

func due(at, now time.Time) bool { return !at.IsZero() && !at.After(now) }

func earliest(a, b time.Time) time.Time {
	if a.IsZero() || (!b.IsZero() && b.Before(a)) {
		return b
	}
	return a
}

// Wait blocks until every loop Start launched has returned; the context given
// to Start must already be cancelled.
func (app *App) Wait() { app.wg.Wait() }

// Standby reads the assistant's standby state. horizon is how far ahead the
// calendar is searched for events that still need a reminder; it should cover
// the longest time the host lets an assistant sleep.
func (app *App) Standby(ctx context.Context, horizon time.Duration) (Standby, error) {
	now := app.now()
	standby, err := app.Store.standby(ctx, now)
	if err != nil {
		return Standby{}, err
	}
	if _, paused := app.Store.Paused(); paused {
		// A paused assistant claims and sends nothing until the user resumes.
		return Standby{Running: standby.Running}, nil
	}
	timer, unsure, err := app.Scheduler.next(ctx, now, horizon)
	if err != nil {
		return Standby{}, err
	}
	standby.Timer, standby.Unsure = earliest(standby.Timer, timer), unsure
	return standby, nil
}

// standby reads what the queues hold: turns and jobs in flight, the earliest
// pending trigger or queued job, and the earliest approved effect or task
// check. A task whose previous check is still queued or running is left out,
// as ClaimDueTasks leaves it.
func (s *Store) standby(ctx context.Context, now time.Time) (Standby, error) {
	var running, queued int
	var trigger, proposal, task sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM triggers WHERE status=?) + (SELECT COUNT(*) FROM jobs WHERE status=?),
		(SELECT COUNT(*) FROM jobs WHERE status=?),
		(SELECT MIN(not_before) FROM triggers WHERE status=?),
		(SELECT MIN(not_before) FROM proposals WHERE status=?),
		(SELECT MIN(wake_at) FROM tasks WHERE status=? AND wake_at>0
			AND NOT EXISTS (SELECT 1 FROM triggers t WHERE t.id=tasks.last_trigger AND t.kind=? AND t.status IN (?,?)))`,
		string(TriggerRunning), string(JobRunning), string(JobQueued), string(TriggerPending), string(ProposalApproved),
		string(TaskActive), string(TriggerTaskWakeup), string(TriggerPending), string(TriggerRunning)).Scan(&running, &queued, &trigger, &proposal, &task)
	if err != nil {
		return Standby{}, err
	}
	at := func(v sql.NullInt64) time.Time {
		if !v.Valid {
			return time.Time{}
		}
		if v.Int64 <= millis(now) {
			return now
		}
		return fromMillis(v.Int64)
	}
	standby := Standby{Running: running, Work: at(trigger), Timer: earliest(at(proposal), at(task))}
	if queued > 0 {
		standby.Work = now
	}
	return standby, nil
}

// Runnable reports whether ClaimBatch would claim something at now.
func (s *Store) Runnable(ctx context.Context, now time.Time, chatOnly bool) (bool, error) {
	only := ""
	if chatOnly {
		only = string(TriggerChat)
	}
	var found bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM triggers t WHERE status=? AND not_before<=? AND (?='' OR kind=?)
		AND NOT EXISTS (SELECT 1 FROM triggers r WHERE r.thread=t.thread AND r.status=?))`,
		string(TriggerPending), millis(now), only, only, string(TriggerRunning)).Scan(&found)
	return found, err
}

// next is the earliest moment a tick would claim a routine slot or an event
// reminder: now for a slot that passed today unclaimed or an event already
// inside the lead time. A calendar that cannot be read is reported as unsure
// instead of failing, so an outage does not keep the assistant awake.
func (s *Scheduler) next(ctx context.Context, now time.Time, horizon time.Duration) (time.Time, bool, error) {
	routines, err := s.store.Routines(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	claimed, err := s.store.routineSlots(ctx, now.Format(slotLayout))
	if err != nil {
		return time.Time{}, false, err
	}
	var next time.Time
	for _, routine := range routines {
		if !routine.Enabled {
			continue
		}
		at := time.Date(now.Year(), now.Month(), now.Day(), routine.Hour, routine.Minute, 0, 0, now.Location())
		switch {
		case now.Before(at):
		case claimed[routine.ID]:
			at = time.Date(now.Year(), now.Month(), now.Day()+1, routine.Hour, routine.Minute, 0, 0, now.Location())
		default:
			at = now
		}
		next = earliest(next, at)
	}
	events, err := s.connector.Events(ctx, now, now.Add(horizon+s.lead))
	if err != nil {
		return next, ctx.Err() == nil, ctx.Err()
	}
	for _, event := range events {
		if event.Status != EventConfirmed || !event.Start.After(now) {
			continue
		}
		reminded, err := s.store.reminded(ctx, event)
		if err != nil {
			return time.Time{}, false, err
		}
		if reminded {
			continue
		}
		at := event.Start.Add(-s.lead)
		if at.Before(now) {
			at = now
		}
		next = earliest(next, at)
	}
	return next, false, nil
}

// routineSlots lists the routines that already claimed the given day's slot.
func (s *Store) routineSlots(ctx context.Context, slot string) (map[RoutineID]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT routine_id FROM routine_runs WHERE slot=?`, slot)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	claimed := map[RoutineID]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		claimed[RoutineID(id)] = true
	}
	return claimed, rows.Err()
}

func (s *Store) reminded(ctx context.Context, event CalendarEvent) (bool, error) {
	var found bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM reminders WHERE event_id=? AND start_at=?)`, event.ID, millis(event.Start)).Scan(&found)
	return found, err
}
