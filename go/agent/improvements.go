package agent

import (
	"context"
	"errors"
	"github.com/luoyjx/mini-loop/go/improvement"
)

// ListImprovements consumes an already admitted owner. Nil is an explicit
// operator view; HTTP derives this choice from authenticator configuration.
// The archive is rooted at the manager workspace, with no startup archive IO.
func (m *SessionManager) ListImprovements(ctx context.Context, owner *OwnerID) ([]improvement.ArchiveValue, error) {
	if m == nil || m.improvements == nil {
		return nil, errors.New("improvement archive is not initialized")
	}
	query := improvement.ArchiveQuery{}
	if owner != nil {
		value := improvement.ArchiveOwnerID(*owner)
		query.Owner = &value
	}
	return m.improvements.List(ctx, query)
}
