package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

const MaxEventCatchup = 2000

var ErrEventCatchup = errors.New("event catch-up failed")

// CatchUpEvents reads a bounded historical window from the configured backend.
// Callers must subscribe before reading, then de-duplicate queued events against
// the greatest delivered sequence. A zero cursor keeps the fresh backlog path.
// This read does not claim a lease, install authority, or enable a backend.
func (session *ManagedSession) CatchUpEvents(ctx context.Context, cursor EventSequence) ([]SessionEventRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p := session.core.persistence
	if cursor == 0 || p == nil {
		return nil, nil
	}
	p.mu.Lock()
	store, disabled := p.store, p.disabled
	p.mu.Unlock()
	if disabled {
		return nil, nil
	}
	// Never hold the persistence lock across backend reads: live capture must
	// remain able to persist and publish while a subscriber is catching up.
	var head EventOrdinal
	var rows []SessionEventRecord
	err := stateFault(func() error {
		var err error
		head, err = store.EventCursor(ctx, session.ID())
		if err != nil {
			return err
		}
		after := EventOrdinal(0)
		if head > MaxEventCatchup {
			after = head - MaxEventCatchup
		}
		limit := MaxEventCatchup
		rows, err = store.LoadEvents(ctx, session.ID(), after, &limit)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEventCatchup, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(rows) > MaxEventCatchup {
		return nil, fmt.Errorf("%w: backend exceeded window", ErrEventCatchup)
	}
	out := make([]SessionEventRecord, 0, len(rows))
	var previous EventSequence
	for _, row := range rows {
		if row.SessionID != session.ID() || row.Sequence <= previous || row.Event.Ephemeral() {
			return nil, fmt.Errorf("%w: invalid stored scope/order", ErrEventCatchup)
		}
		// Re-decode the closed archival projection. Even an embedding backend
		// cannot introduce live capabilities or aliased mutable payloads here.
		encoded, err := json.Marshal(row)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrEventCatchup, err)
		}
		decoded, err := DecodeStoredEvent(encoded)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrEventCatchup, err)
		}
		previous = decoded.Sequence
		if decoded.Sequence > cursor {
			out = append(out, decoded)
		}
	}
	return out, nil
}
