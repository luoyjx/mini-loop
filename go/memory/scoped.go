package memory

import (
	"context"
	"errors"
)

// ScopedStore exposes no owner override. Replacement always uses the bound owner.
type ScopedStore struct {
	store *Store
	owner OwnerID
}

func Bind(store *Store, owner OwnerID) (*ScopedStore, error) {
	if store == nil {
		return nil, errors.New("memory store is required")
	}
	return &ScopedStore{store, owner}, nil
}
func (s *ScopedStore) Owner() OwnerID { return s.owner }
func (s *ScopedStore) Write(ctx context.Context, input Input) (string, error) {
	return s.store.Write(ctx, s.owner, input)
}
func (s *ScopedStore) List(ctx context.Context) ([]Record, error) { return s.store.List(ctx, &s.owner) }
func (s *ScopedStore) Index(ctx context.Context) (string, error)  { return s.store.Index(ctx, &s.owner) }
func (s *ScopedStore) Search(ctx context.Context, query string, limit int) ([]Record, error) {
	return s.store.Search(ctx, &s.owner, query, limit)
}
func (s *ScopedStore) ReplaceAll(ctx context.Context, memories []Input, origin Origin) error {
	return s.store.ReplaceAll(ctx, &s.owner, memories, origin)
}
