package sqlitestate

import (
	"context"
	"database/sql"
	"math"

	"github.com/luoyjx/mini-loop/go/agent"
)

var _ agent.EventStore = (*Store)(nil)

const eventsTable tableName = "events"

func (s *Store) AppendEvent(ctx context.Context, id agent.SessionID, record agent.SessionEventRecord) (agent.EventOrdinal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return 0, err
	}
	if err := validateText(string(id)); err != nil {
		return 0, err
	}
	var ordinal int64
	err := s.withImmediate(ctx, func() error {
		head, err := s.maxOrdinal(ctx, eventsTable, id)
		if err != nil {
			return err
		}
		if head < 0 || head == math.MaxInt64 {
			return ErrOrdinalRange
		}
		data, err := record.MarshalArchiveJSON()
		if err != nil {
			return err
		}
		payload, err := sourcePayload(data)
		if err != nil {
			return err
		}
		ordinal = head + 1
		_, err = s.conn.ExecContext(ctx, "INSERT INTO events (session_id,ordinal,payload) VALUES (?,?,?)", id, ordinal, payload)
		return err
	})
	if err != nil {
		return 0, err
	}
	return agent.EventOrdinal(ordinal), nil
}

func (s *Store) EventCursor(ctx context.Context, id agent.SessionID) (agent.EventOrdinal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return 0, err
	}
	if err := validateText(string(id)); err != nil {
		return 0, err
	}
	head, err := s.maxOrdinal(ctx, eventsTable, id)
	if err != nil {
		return 0, err
	}
	if head < 0 {
		return 0, ErrOrdinalRange
	}
	return agent.EventOrdinal(head), nil
}

func (s *Store) LoadEvents(ctx context.Context, id agent.SessionID, after agent.EventOrdinal, limit *int) ([]agent.SessionEventRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	if err := validateText(string(id)); err != nil {
		return nil, err
	}
	if after > math.MaxInt64 {
		return nil, ErrOrdinalRange
	}
	query := "SELECT payload FROM events WHERE session_id=? AND ordinal>? ORDER BY ordinal"
	if limit != nil {
		query += " LIMIT ?"
	}
	// Keep concrete calls rather than retaining an untyped parameter collection.
	var rows *sql.Rows
	var err error
	if limit == nil {
		rows, err = s.conn.QueryContext(ctx, query, id, int64(after))
	} else {
		rows, err = s.conn.QueryContext(ctx, query, id, int64(after), *limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []agent.SessionEventRecord{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		record, err := agent.DecodeStoredEvent([]byte(payload))
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return result, rows.Err()
}
