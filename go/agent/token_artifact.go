package agent

import (
	"context"
	"errors"
	"github.com/luoyjx/mini-loop/go/efficiency"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workspace"
)

type tokenArtifactHandler struct {
	binding ToolAuthority
	store   *efficiency.RawStore
}

func (h tokenArtifactHandler) ExecuteTool(ctx context.Context, a ToolAuthority, input protocol.ToolInput) (string, error) {
	v, ok := input.ReadTokenArtifact()
	if !ok {
		return "", errors.New("artifact recovery received a different input variant")
	}
	root, err := workspace.ResolvePath(a.Workspace)
	if err != nil || root != h.binding.Workspace || a.SessionID != h.binding.SessionID || a.OwnerID != h.binding.OwnerID {
		return "", errors.New("artifact recovery authority does not match bound session")
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	offsetNull, limitNull := input.TokenArtifactNullPagination()
	if offsetNull {
		return "Error: offset must be a non-negative integer", nil
	}
	if v.Offset != nil && v.Offset.Sign() < 0 {
		return "Error: offset must be a non-negative integer", nil
	}
	if limitNull {
		return "Error: limit must be a positive integer", nil
	}
	return efficiency.ReadMaskedPage(h.store, efficiency.RecoveryPage{Ref: v.RawRef, Offset: v.Offset, Limit: v.Limit})
}
func tokenArtifactDefinition(binding ToolAuthority, store *efficiency.RawStore) (ToolDefinition, error) {
	if err := binding.Validate(); err != nil {
		return ToolDefinition{}, err
	}
	root, err := workspace.ResolvePath(binding.Workspace)
	if err != nil {
		return ToolDefinition{}, err
	}
	binding.Workspace = root
	if store != nil && store.Workspace() != root {
		return ToolDefinition{}, errors.New("artifact store workspace does not match bound session")
	}
	return NewToolDefinitionWithSchema(protocol.ReadTokenArtifactSchema(), ToolTraits{Risk: RiskRead, Readonly: true, ParallelSafe: true, Capabilities: []Capability{CapabilityObservationRecover}}, tokenArtifactHandler{binding, store})
}

// InstallTokenArtifactTool preserves a same-name custom definition and catalogue
// identity. The caller owns store lifetime; a new handler pins session authority.
func InstallTokenArtifactTool(catalog *ToolCatalog, binding ToolAuthority, store *efficiency.RawStore) (*ToolCatalog, error) {
	if catalog == nil {
		return nil, errors.New("artifact recovery requires a catalogue")
	}
	if _, ok := catalog.Lookup(protocol.ToolReadTokenArtifact); ok {
		return catalog, nil
	}
	definition, err := tokenArtifactDefinition(binding, store)
	if err != nil {
		return nil, err
	}
	definitions := append([]ToolDefinition{}, catalog.ordered...)
	definitions = append(definitions, definition)
	return NewToolCatalog(definitions...)
}
