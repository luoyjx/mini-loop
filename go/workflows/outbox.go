package workflows

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type EnqueueOutboxInput struct {
	RunID   RunID
	Kind    OutboxKind
	Payload Value
}

// EnqueueOutbox deduplicates run/kind before considering a replacement payload.
func (s *InMemoryStore) EnqueueOutbox(input EnqueueOutboxInput) (OutboxSnapshot, error) {
	if input.Kind == "" {
		return OutboxSnapshot{}, storeError(StoreValueFailure, "outbox kind is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.runLocked(input.RunID)
	if err != nil {
		return OutboxSnapshot{}, err
	}
	key := outboxKey{input.RunID, input.Kind}
	if id, ok := s.outboxKeys[key]; ok {
		return s.outbox[id].Clone(), nil
	}
	if input.Payload.Kind() != jsonvalue.Object {
		return OutboxSnapshot{}, storeError(StoreFailure, "outbox payload must be an object")
	}
	raw, err := workflowID20("wfout_")
	if err != nil {
		return OutboxSnapshot{}, err
	}
	m := OutboxSnapshot{MessageID: OutboxID(raw), RunID: input.RunID, SessionID: r.SessionID, Kind: input.Kind, Payload: input.Payload, CreatedAt: wallTime()}
	s.outbox[m.MessageID] = m
	s.outboxOrder = append(s.outboxOrder, m.MessageID)
	s.outboxKeys[key] = m.MessageID
	return m.Clone(), nil
}

type ClaimOutboxInput struct {
	SessionID SessionID
	// Nil means any run; an explicit empty slice admits no run.
	RunIDs []RunID
	// Nil selects 30 seconds. This is the source caller-supplied lease window,
	// not an expiry saved in each message.
	LeaseSeconds *float64
	// Nil is unbounded; zero/negative limits return a fresh token and no messages.
	Limit *int
}

type OutboxLease struct {
	Token    ClaimToken
	Messages []OutboxSnapshot
}

func sortOutbox(messages []OutboxSnapshot) {
	slices.SortFunc(messages, func(a, b OutboxSnapshot) int {
		if order := cmp.Compare(a.CreatedAt, b.CreatedAt); order != 0 {
			return order
		}
		return cmp.Compare(a.MessageID, b.MessageID)
	})
}

// ClaimOutbox selects in insertion order before sorting the returned selection.
// Claiming never marks a message delivered and always returns a fresh token.
func (s *InMemoryStore) ClaimOutbox(input ClaimOutboxInput) (OutboxLease, error) {
	seconds := 30.0
	if input.LeaseSeconds != nil {
		seconds = *input.LeaseSeconds
	}
	if seconds <= 0 {
		return OutboxLease{}, storeError(StoreValueFailure, "lease_seconds must be positive")
	}
	now := wallTime()
	raw, err := workflowID20("wfclaim_")
	if err != nil {
		return OutboxLease{}, err
	}
	lease := OutboxLease{Token: ClaimToken(raw), Messages: []OutboxSnapshot{}}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.outboxOrder {
		if input.Limit != nil && len(lease.Messages) >= *input.Limit {
			break
		}
		m := s.outbox[id]
		if m.SessionID != input.SessionID || m.DeliveredAt != nil {
			continue
		}
		if input.RunIDs != nil && !slices.Contains(input.RunIDs, m.RunID) {
			continue
		}
		if m.ClaimToken != nil && m.ClaimedAt != nil && now-*m.ClaimedAt < seconds {
			continue
		}
		m.ClaimToken, m.ClaimedAt = recordPointer(&lease.Token), recordPointer(&now)
		s.outbox[id] = m
		lease.Messages = append(lease.Messages, m.Clone())
	}
	sortOutbox(lease.Messages)
	return lease, nil
}

type SettleOutboxInput struct {
	SessionID  SessionID
	MessageIDs []OutboxID
	Token      ClaimToken
}

func (s *InMemoryStore) ownedOutboxLocked(id OutboxID, session SessionID) (OutboxSnapshot, error) {
	m, ok := s.outbox[id]
	if !ok || m.SessionID != session {
		return OutboxSnapshot{}, storeError(StoreNotFound, fmt.Sprintf("outbox message %s not found", id))
	}
	return m, nil
}

func matchingOutboxLease(m OutboxSnapshot, token ClaimToken) error {
	if m.ClaimToken == nil || *m.ClaimToken != token {
		return storeError(StoreVersionConflict, fmt.Sprintf("outbox message %s lease does not match", m.MessageID))
	}
	return nil
}

// AcknowledgeOutbox records delivery only after the caller's successful append.
// Python settles IDs sequentially: a later refusal retains earlier effects.
func (s *InMemoryStore) AcknowledgeOutbox(input SettleOutboxInput) ([]OutboxSnapshot, error) {
	if input.Token == "" {
		return nil, storeError(StoreValueFailure, "claim_token is required")
	}
	now := wallTime()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []OutboxSnapshot{}
	for _, id := range input.MessageIDs {
		m, err := s.ownedOutboxLocked(id, input.SessionID)
		if err != nil {
			return nil, err
		}
		if m.DeliveredAt != nil {
			out = append(out, m.Clone())
			continue
		}
		if err := matchingOutboxLease(m, input.Token); err != nil {
			return nil, err
		}
		m.ClaimToken, m.ClaimedAt, m.DeliveredAt = nil, nil, recordPointer(&now)
		s.outbox[id] = m
		out = append(out, m.Clone())
	}
	sortOutbox(out)
	return out, nil
}

// ReleaseOutbox preserves the source empty-token behavior and sequential effects.
func (s *InMemoryStore) ReleaseOutbox(input SettleOutboxInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range input.MessageIDs {
		m, err := s.ownedOutboxLocked(id, input.SessionID)
		if err != nil {
			return err
		}
		if m.DeliveredAt != nil {
			continue
		}
		if err := matchingOutboxLease(m, input.Token); err != nil {
			return err
		}
		m.ClaimToken, m.ClaimedAt = nil, nil
		s.outbox[id] = m
	}
	return nil
}
