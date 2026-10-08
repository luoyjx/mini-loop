package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/sessionquery"
)

var ErrRequestReconstruction = errors.New("stored request reconstruction failed")

// ReconstructRequest reads the configured historical store for this session.
// The caller owns authorization. It takes no turn admission or lease, performs
// no flush or repair, and never substitutes live history for persisted records.
func (session *ManagedSession) ReconstructRequest(ctx context.Context, sequence sessionquery.RequestSequence) (sessionquery.Reconstruction, error) {
	if err := ctx.Err(); err != nil {
		return sessionquery.Reconstruction{}, err
	}
	var store StateStore
	if p := session.core.persistence; p != nil {
		p.mu.Lock()
		if !p.disabled {
			store = p.store
		}
		p.mu.Unlock()
	}
	result, err := sessionquery.Reconstruct(ctx, storedRequestReader{store}, sessionquery.SessionID(session.ID()), sequence)
	if err != nil {
		return sessionquery.Reconstruction{}, fmt.Errorf("%w: %w", ErrRequestReconstruction, err)
	}
	return result, nil
}

type storedRequestReader struct{ store StateStore }

func (reader storedRequestReader) LoadRequestEvents(ctx context.Context, id sessionquery.SessionID) ([]sessionquery.RequestEvent, error) {
	if reader.store == nil {
		return nil, nil
	}
	var records []SessionEventRecord
	if err := stateFault(func() error {
		var err error
		records, err = reader.store.LoadEvents(ctx, SessionID(id), 0, nil)
		return err
	}); err != nil {
		return nil, err
	}
	events := make([]sessionquery.RequestEvent, 0, len(records))
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Ignored records still participate in physical sequence stopping. Their
		// unrelated payloads are not reconstruction inputs and need not serialize.
		var data []byte
		var err error
		switch record.Event.Kind() {
		case EventModelStart, EventToolCatalog, EventSystemPrompt, EventCapabilityPlan:
			data, err = json.Marshal(record)
		default:
			data, err = json.Marshal(struct {
				Seq EventSequence `json:"seq"`
			}{record.Sequence})
		}
		if err != nil {
			return nil, err
		}
		value, err := jsonvalue.Decode(string(data))
		if err != nil {
			return nil, err
		}
		event, err := sessionquery.DecodeRequestEvent(value)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (reader storedRequestReader) LoadRequestMessages(ctx context.Context, id sessionquery.SessionID, selector sessionquery.ArchiveValue) ([]sessionquery.ArchiveValue, error) {
	// Native stored events carry concrete integer epochs. The broader archive
	// reader contract remains available for Source's historical scalar selectors.
	decimal, ok := selector.Integer()
	if !ok {
		return nil, errors.New("native request epoch must be an integer")
	}
	number, err := strconv.Atoi(decimal)
	if err != nil || number < 1 {
		return nil, errors.New("native request epoch is out of range")
	}
	epoch := TranscriptEpoch(number)
	if reader.store == nil {
		return nil, nil
	}
	var rows []sessionquery.ArchiveValue
	err = stateFault(func() error {
		messages, err := reader.store.LoadMessages(ctx, SessionID(id), &epoch)
		if err != nil {
			return err
		}
		rows = make([]sessionquery.ArchiveValue, 0, len(messages))
		for _, message := range messages {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := message.Validate(); err != nil {
				return err
			}
			data, err := json.Marshal(message)
			if err != nil {
				return err
			}
			value, err := jsonvalue.Decode(string(data))
			if err != nil {
				return err
			}
			rows = append(rows, value)
		}
		return nil
	})
	return rows, err
}
