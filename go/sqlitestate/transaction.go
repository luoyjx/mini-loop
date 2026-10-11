package sqlitestate

import (
	"context"
	"errors"
)

// withImmediate is called with Store.mu held. BEGIN IMMEDIATE acquires the
// SQLite writer reservation before reading a head or deleting operational rows.
// Rollback uses a live cleanup context even if the caller has cancelled.
func (s *Store) withImmediate(ctx context.Context, operation func() error) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	if _, err = s.conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, rollbackError := s.conn.ExecContext(context.Background(), "ROLLBACK")
			err = errors.Join(err, rollbackError)
		}
	}()
	if err = operation(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	_, err = s.conn.ExecContext(ctx, "COMMIT")
	committed = err == nil
	return err
}
