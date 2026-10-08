package agent

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/diagnostics"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workspace"
)

func (handler *runtimeHandler) executeDiagnostics(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, error) {
	// Capture the current execution binding under the handler lock, then let
	// the read/parallel provider run without holding mutation or session locks.
	files, provider, err := handler.diagnosticsBinding(ctx, authority)
	if err != nil {
		return "", err
	}
	value, ok := input.Diagnostics()
	if !ok {
		return "", errors.New("diagnostics input has the wrong variant")
	}
	return diagnostics.Tool(ctx, files, provider, value.Path)
}

func (handler *runtimeHandler) diagnosticsBinding(ctx context.Context, authority ToolAuthority) (*workspace.Files, diagnostics.Provider, error) {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if authority.SessionID != handler.binding.SessionID || authority.OwnerID != handler.binding.OwnerID {
		return nil, nil, errors.New("runtime handler identity does not match bound session")
	}
	root, err := workspace.ResolvePath(authority.Workspace)
	if err != nil || root != handler.binding.Workspace {
		return nil, nil, errors.New("runtime handler workspace does not match bound session")
	}
	if handler.session == nil || handler.session.files == nil || handler.session.files.Root() != root || handler.diagnostics == nil {
		return nil, nil, errors.New("diagnostics provider is not bound")
	}
	return handler.session.files, handler.diagnostics, nil
}
