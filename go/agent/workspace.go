package agent

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type workspaceFileHandler struct {
	files *workspace.Files
	name  protocol.ToolName
}

func (handler workspaceFileHandler) ExecuteTool(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, error) {
	if authority.Workspace == "" {
		return "", errors.New("file handler requires a bound workspace")
	}
	root, err := workspace.ResolvePath(authority.Workspace)
	if err != nil {
		return "", err
	}
	if root != handler.files.Root() {
		return "", errors.New("file handler workspace does not match tool authority")
	}
	if input.Name() != handler.name {
		return "", errors.New("file handler received a different tool input")
	}
	switch handler.name {
	case protocol.ToolReadFile:
		value, _ := input.ReadFile()
		return handler.files.Read(ctx, value)
	case protocol.ToolWriteFile:
		value, _ := input.WriteFile()
		return handler.files.Write(ctx, value)
	case protocol.ToolEditFile:
		value, _ := input.EditFile()
		return handler.files.Edit(ctx, value)
	case protocol.ToolGlob:
		value, _ := input.Glob()
		return handler.files.Glob(ctx, value)
	default:
		return "", errors.New("unsupported workspace file handler")
	}
}

// NewWorkspaceToolCatalog adds the four implemented file tools to injected
// Bash. The remaining Python defaults are not registered until ported.
func NewWorkspaceToolCatalog(executor BashExecutor, files *workspace.Files) (*ToolCatalog, error) {
	if files == nil {
		return nil, errors.New("workspace file backend is required")
	}
	bash, err := NewBashToolCatalog(executor)
	if err != nil {
		return nil, err
	}
	definitions := append([]ToolDefinition(nil), bash.ordered...)
	for _, name := range []protocol.ToolName{protocol.ToolReadFile, protocol.ToolWriteFile, protocol.ToolEditFile, protocol.ToolGlob} {
		traits := ToolTraits{Risk: RiskWrite, Capabilities: []Capability{CapabilityWorkspaceWrite}}
		if name == protocol.ToolReadFile {
			traits = ToolTraits{Risk: RiskRead, Readonly: true, ParallelSafe: true, Capabilities: []Capability{CapabilityRepoRead}}
		}
		if name == protocol.ToolGlob {
			traits = ToolTraits{Risk: RiskRead, Readonly: true, ParallelSafe: true, Capabilities: []Capability{CapabilityRepoSearch}}
		}
		definition, err := NewToolDefinition(name, traits, workspaceFileHandler{files, name})
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	return NewToolCatalog(definitions...)
}

func NewWorkspaceSession(id SessionID, owner OwnerID, provider Provider, executor BashExecutor, root string, mode PermissionMode, maxRounds int) (*Session, error) {
	// Validate before creating any workspace directory.
	if id == "" || owner == "" || provider == nil || executor == nil || !mode.Valid() || maxRounds < 1 {
		return nil, errors.New("workspace session requires valid identity, provider, executor, mode and round limit")
	}
	files, err := workspace.NewFiles(root)
	if err != nil {
		return nil, err
	}
	catalog, err := NewWorkspaceToolCatalog(executor, files)
	if err != nil {
		return nil, err
	}
	gate, err := NewToolGate(catalog, DefaultPermissionPolicy(nil), GateHooks{})
	if err != nil {
		return nil, err
	}
	return NewSessionWithGate(id, owner, provider, gate, mode, files.Root(), maxRounds)
}
