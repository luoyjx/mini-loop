package agent

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/memory"
)

// MemoryRecords reads the fixed owner binding used by this session's runtime.
// It does not resolve another owner's cache, enable tools or regenerate an index.
func (session *ManagedSession) MemoryRecords(ctx context.Context) (records []memory.Record, err error) {
	defer func() {
		if recover() != nil {
			records = nil
			err = errors.New("owner memory unavailable")
		}
	}()
	store := session.core.memory
	if store == nil || store.Owner() != memory.OwnerID(session.Owner()) {
		return nil, errors.New("owner memory unavailable")
	}
	return store.List(ctx)
}
