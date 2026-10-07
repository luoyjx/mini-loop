package memory

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/problems"
)

// ScopedStore exposes no owner override. Replacement always uses the bound owner.
type ScopedStore struct {
	store       *Store
	owner       OwnerID
	diagnostics problems.Log
}

func Bind(store *Store, owner OwnerID) (*ScopedStore, error) {
	if store == nil {
		return nil, errors.New("memory store is required")
	}
	return &ScopedStore{store: store, owner: owner}, nil
}
func (s *ScopedStore) Owner() OwnerID { return s.owner }

// WithLifecycle serializes a multi-operation memory lifecycle across every
// binding of this Store. The callback may call ordinary scoped operations, but
// must not recursively acquire this lock. It provides no cross-process fencing
// or rollback; separate Store instances have separate lifecycle locks.
func (s *ScopedStore) WithLifecycle(ctx context.Context, run func() error) error {
	if run == nil {
		return errors.New("memory lifecycle callback is required")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.store.lifecycle:
	}
	defer func() { s.store.lifecycle <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return err
	}
	return run()
}

func (s *ScopedStore) Write(ctx context.Context, input Input) (string, error) {
	if err := s.store.acquire(ctx); err != nil {
		return "", err
	}
	defer s.store.release()
	return s.store.write(ctx, s.owner, input, &s.diagnostics)
}
func (s *ScopedStore) List(ctx context.Context) ([]Record, error) { return s.store.List(ctx, &s.owner) }
func (s *ScopedStore) Index(ctx context.Context) (string, error)  { return s.store.Index(ctx, &s.owner) }
func (s *ScopedStore) Search(ctx context.Context, query string, limit int) ([]Record, error) {
	return s.store.Search(ctx, &s.owner, query, limit)
}
func (s *ScopedStore) ReplaceAll(ctx context.Context, memories []Input, origin Origin) error {
	return s.store.replaceAll(ctx, &s.owner, memories, origin, &s.diagnostics)
}
