package agent

import (
	"context"
	"errors"

	"github.com/luoyjx/mini-loop/go/astcontext"
	"github.com/luoyjx/mini-loop/go/protocol"
	"github.com/luoyjx/mini-loop/go/workspace"
)

func configuredAST(config *astcontext.Config) (*astcontext.Adapter, error) {
	if config == nil {
		return nil, nil
	}
	return astcontext.NewAdapter(*config)
}
func (handler *runtimeHandler) astBinding(ctx context.Context, authority ToolAuthority) (string, *astcontext.Adapter, error) {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if authority.SessionID != handler.binding.SessionID || authority.OwnerID != handler.binding.OwnerID {
		return "", nil, errors.New("runtime handler identity does not match bound session")
	}
	root, err := workspace.ResolvePath(authority.Workspace)
	if err != nil || root != handler.binding.Workspace {
		return "", nil, errors.New("runtime handler workspace does not match bound session")
	}
	if handler.session == nil || handler.session.files == nil || handler.session.files.Root() != root || handler.ast == nil {
		return "", nil, errors.New("AST adapter is not bound")
	}
	return root, handler.ast, nil
}
func (handler *runtimeHandler) executeAST(ctx context.Context, authority ToolAuthority, input protocol.ToolInput) (string, error) {
	if err := input.Validate(); err != nil {
		return "", err
	}
	root, adapter, err := handler.astBinding(ctx, authority)
	if err != nil {
		return "", err
	}
	var result astcontext.Result
	switch input.Name() {
	case protocol.ToolRepoMap:
		v, _ := input.RepoMap()
		density := astcontext.DefaultDensity
		if v.Density != nil {
			switch *v.Density {
			case protocol.ASTNames:
				density = astcontext.NamesDensity
			case protocol.ASTCompact:
				density = astcontext.CompactDensity
			case protocol.ASTWide:
				density = astcontext.WideDensity
			}
		}
		result, err = adapter.RepoMap(ctx, root, astcontext.RepoMapRequest{Paths: v.Paths, Density: density, IncludeImports: v.IncludeImports != nil && *v.IncludeImports})
	case protocol.ToolFileOutline:
		v, _ := input.FileOutline()
		view := astcontext.FullView
		if v.View != nil {
			switch *v.View {
			case protocol.ASTPublic:
				view = astcontext.PublicView
			case protocol.ASTMinimal:
				view = astcontext.MinimalView
			}
		}
		result, err = adapter.FileOutline(ctx, root, astcontext.FileOutlineRequest{Paths: v.Paths, View: view, IncludeImports: v.IncludeImports != nil && *v.IncludeImports})
	case protocol.ToolShowSymbol:
		v, _ := input.ShowSymbol()
		result, err = adapter.ShowSymbol(ctx, root, astcontext.ShowSymbolRequest{PathOrGlob: v.PathOrGlob, Symbols: v.Symbols, SignatureOnly: v.SignatureOnly != nil && *v.SignatureOnly})
	case protocol.ToolSymbolReferences:
		v, _ := input.SymbolReferences()
		kinds := []astcontext.ReferenceKind{}
		if v.Kinds != nil {
			for _, k := range *v.Kinds {
				switch k {
				case protocol.ASTDefinition:
					kinds = append(kinds, astcontext.DefinitionReference)
				case protocol.ASTCall:
					kinds = append(kinds, astcontext.CallReference)
				case protocol.ASTReference:
					kinds = append(kinds, astcontext.SymbolReference)
				case protocol.ASTImport:
					kinds = append(kinds, astcontext.ImportReference)
				case protocol.ASTComment:
					kinds = append(kinds, astcontext.CommentReference)
				case protocol.ASTString:
					kinds = append(kinds, astcontext.StringReference)
				}
			}
		}
		result, err = adapter.SymbolReferences(ctx, root, astcontext.SymbolReferencesRequest{Paths: v.Paths, Patterns: v.Patterns, Kinds: kinds, MaxPerFile: v.MaxPerFile})
	default:
		return "", errors.New("AST input has the wrong variant")
	}
	if err != nil {
		return "", err
	}
	return result.ToJSON()
}
func astTraits(name protocol.ToolName) ToolTraits {
	capability := CapabilityRepoSemanticOutline
	if name == protocol.ToolShowSymbol {
		capability = CapabilityRepoSymbol
	}
	if name == protocol.ToolSymbolReferences {
		capability = CapabilityRepoReferences
	}
	return ToolTraits{Risk: RiskRead, Readonly: true, ParallelSafe: true, Capabilities: []Capability{capability}}
}
