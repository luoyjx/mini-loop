package sqlitestate

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/luoyjx/mini-loop/go/agent"
)

var _ agent.LeaseStore = (*Store)(nil)

func (s *Store) timestamp() float64 {
	if s.now != nil {
		return s.now()
	}
	now := time.Now()
	return float64(now.Unix()) + float64(now.Nanosecond())/1e9
}

// AcquireLease is one conditional UPDATE. Equality is still held: another
// process may acquire only after expiry, or when the stored owner is NULL.
func (s *Store) AcquireLease(ctx context.Context, id agent.SessionID, owner agent.LeaseOwner, ttl time.Duration) (bool, error) {
	now := s.timestamp()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return false, err
	}
	if err := validateText(string(id), string(owner)); err != nil {
		return false, err
	}
	result, err := s.conn.ExecContext(ctx, `UPDATE sessions SET lease_owner=?, lease_until=?
 WHERE session_id=? AND (lease_owner IS NULL OR lease_owner=? OR lease_until<?)`, owner, now+ttl.Seconds(), id, owner, now)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

// RenewLease cannot reclaim an expired or foreign-held lease.
func (s *Store) RenewLease(ctx context.Context, id agent.SessionID, owner agent.LeaseOwner, ttl time.Duration) (bool, error) {
	now := s.timestamp()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return false, err
	}
	if err := validateText(string(id), string(owner)); err != nil {
		return false, err
	}
	result, err := s.conn.ExecContext(ctx, `UPDATE sessions SET lease_until=?
 WHERE session_id=? AND lease_owner=? AND lease_until>=?`, now+ttl.Seconds(), id, owner, now)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (s *Store) ReleaseLease(ctx context.Context, id agent.SessionID, owner agent.LeaseOwner) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	if err := validateText(string(id), string(owner)); err != nil {
		return err
	}
	_, err := s.conn.ExecContext(ctx, "UPDATE sessions SET lease_owner=NULL, lease_until=0 WHERE session_id=? AND lease_owner=?", id, owner)
	return err
}

func (s *Store) LeaseHolder(ctx context.Context, id agent.SessionID) (agent.LeaseOwner, bool, error) {
	s.mu.Lock()
	if err := s.ready(ctx); err != nil {
		s.mu.Unlock()
		return "", false, err
	}
	if err := validateText(string(id)); err != nil {
		s.mu.Unlock()
		return "", false, err
	}
	var owner sql.NullString
	var until float64
	err := s.conn.QueryRowContext(ctx, "SELECT lease_owner, lease_until FROM sessions WHERE session_id=?", id).Scan(&owner, &until)
	s.mu.Unlock()
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !owner.Valid || until < s.timestamp() {
		return "", false, nil
	}
	return agent.LeaseOwner(owner.String), true, nil
}
