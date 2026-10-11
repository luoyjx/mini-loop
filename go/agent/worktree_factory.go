package agent

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/worktrees"
)

// WorktreeWorkspaceFactory adapts the source worktree provisioning helper to
// SessionManager's typed factory seam. The returned path is managed scratch,
// including when Git falls back to a plain directory. Delete therefore applies
// ordinary scratch reclamation: it removes the directory without Git's dirty
// checks, registration cleanup or branch deletion. PreserveWorkspace or explicit
// bound-workspace admission is needed when the caller wants to retain the work.
type WorktreeWorkspaceFactory struct {
	service *worktrees.Manager
}

func NewWorktreeWorkspaceFactory(service *worktrees.Manager) (*WorktreeWorkspaceFactory, error) {
	if service == nil {
		return nil, errors.New("worktree workspace factory requires an explicit service")
	}
	return &WorktreeWorkspaceFactory{service: service}, nil
}

func (factory *WorktreeWorkspaceFactory) WorkspaceFor(ctx context.Context, id SessionID) (string, error) {
	if factory == nil || factory.service == nil {
		return "", errors.New("worktree workspace factory requires an explicit service")
	}
	return factory.service.WorkspaceFor(ctx, string(id))
}

var _ WorkspaceFactory = (*WorktreeWorkspaceFactory)(nil)
