package launcher

import (
	"context"

	"github.com/luoyjx/mini-loop/go/agent"
)

// RestoreSessions is an operator operation over the explicitly injected backend.
// Invoke it before Serve to expose restored handles to HTTP clients. It never
// opens/closes the store, starts turns, arms jobs or restores human grants.
// Earlier handles may be returned with a later error: restoration is not atomic
// across the fleet. Stop cancels and joins restoration through the manager.
func (a *App) RestoreSessions(ctx context.Context) ([]*agent.ManagedSession, error) {
	return a.manager.RestoreSessions(ctx)
}
