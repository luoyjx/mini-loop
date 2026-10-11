package astcontext

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

type Density uint8

const (
	DefaultDensity Density = iota
	NamesDensity
	CompactDensity
	WideDensity
)

func (d Density) String() string {
	switch d {
	case DefaultDensity:
		return "default"
	case NamesDensity:
		return "names"
	case CompactDensity:
		return "compact"
	case WideDensity:
		return "wide"
	}
	return ""
}

type OutlineView uint8

const (
	FullView OutlineView = iota
	PublicView
	MinimalView
)

func (v OutlineView) String() string {
	switch v {
	case FullView:
		return "full"
	case PublicView:
		return "public"
	case MinimalView:
		return "minimal"
	}
	return ""
}

type ReferenceKind uint8

const (
	InvalidReferenceKind ReferenceKind = iota
	DefinitionReference
	CallReference
	SymbolReference
	ImportReference
	CommentReference
	StringReference
)

func (k ReferenceKind) String() string {
	switch k {
	case DefinitionReference:
		return "def"
	case CallReference:
		return "call"
	case SymbolReference:
		return "ref"
	case ImportReference:
		return "import"
	case CommentReference:
		return "comment"
	case StringReference:
		return "string"
	}
	return ""
}

type RepoMapRequest struct {
	Paths          []string
	Density        Density
	IncludeImports bool
}
type FileOutlineRequest struct {
	Paths          []string
	View           OutlineView
	IncludeImports bool
}
type ShowSymbolRequest struct {
	PathOrGlob    string
	Symbols       []string
	SignatureOnly bool
}
type SymbolReferencesRequest struct {
	Patterns, Paths []string
	Kinds           []ReferenceKind
	MaxPerFile      *int
}

func (a *Adapter) operationContext(ctx context.Context) error {
	if a == nil || !a.config.valid {
		return errors.New("configured AST adapter is required")
	}
	return snapshotContext(ctx)
}
func operationFailure(ctx context.Context, op Operation, err error) (Result, error) {
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	var iteration PayloadIterationError
	if errors.As(err, &iteration) || errors.Is(err, jsonvalue.ErrDepth) {
		return Result{}, err
	}
	return NewResult(ResultParameters{Status: Error, Operation: op, Message: err.Error()})
}
func selectedPaths(ctx context.Context, root string, paths []string) ([]string, error) {
	validated, err := astStrings(paths, "paths")
	if err != nil {
		return nil, err
	}
	selected := make([]string, len(validated))
	for i, path := range validated {
		selected[i], err = astSafePath(ctx, root, path)
		if err != nil {
			return nil, err
		}
	}
	return selected, nil
}
func (a *Adapter) invoke(ctx context.Context, root string, op Operation, args []string, includeImports *bool, rewrites []snapshotRewrite) (Result, error) {
	probe, err := a.Probe(ctx, root)
	if err != nil {
		return Result{}, err
	}
	if probe.status != Applied {
		return classifyPayload(op, probe, processResult{}, includeImports, rewrites, a.config.parameters.SchemaVersion)
	}
	process, err := a.run(ctx, root, args)
	if err != nil {
		return Result{}, err
	}
	return classifyPayload(op, probe, process, includeImports, rewrites, a.config.parameters.SchemaVersion)
}
func (a *Adapter) snapshotOperation(ctx context.Context, root string, paths []string, op Operation, imports *bool, regularFile bool, argv func(sourceSnapshot) []string) (Result, error) {
	var result Result
	err := a.withSnapshot(ctx, root, paths, func(frozen sourceSnapshot) error {
		if regularFile {
			info, err := os.Stat(frozen.paths[0])
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return errors.New("show_symbol requires a regular source file")
			}
		}
		var err error
		result, err = a.invoke(ctx, root, op, argv(frozen), imports, frozen.rewrites)
		return err
	})
	if err != nil {
		return operationFailure(ctx, op, err)
	}
	return result, nil
}
func (a *Adapter) RepoMap(ctx context.Context, workspace string, request RepoMapRequest) (Result, error) {
	if err := a.operationContext(ctx); err != nil {
		return Result{}, err
	}
	if request.Density.String() == "" {
		return operationFailure(ctx, RepoMap, errors.New("density must be one of ['compact', 'default', 'names', 'wide']"))
	}
	root, err := astWorkspace(ctx, workspace)
	if err != nil {
		return operationFailure(ctx, RepoMap, err)
	}
	paths, err := selectedPaths(ctx, root, request.Paths)
	if err != nil {
		return operationFailure(ctx, RepoMap, err)
	}
	return a.snapshotOperation(ctx, root, paths, RepoMap, &request.IncludeImports, false, func(frozen sourceSnapshot) []string {
		args := []string{"digest", "--format", request.Density.String()}
		if request.IncludeImports {
			args = append(args, "--imports")
		}
		args = append(args, "--json", "--")
		return append(args, frozen.paths...)
	})
}
func (a *Adapter) FileOutline(ctx context.Context, workspace string, request FileOutlineRequest) (Result, error) {
	if err := a.operationContext(ctx); err != nil {
		return Result{}, err
	}
	if request.View.String() == "" {
		return operationFailure(ctx, FileOutline, errors.New("view must be one of ['full', 'minimal', 'public']"))
	}
	root, err := astWorkspace(ctx, workspace)
	if err != nil {
		return operationFailure(ctx, FileOutline, err)
	}
	paths, err := selectedPaths(ctx, root, request.Paths)
	if err != nil {
		return operationFailure(ctx, FileOutline, err)
	}
	return a.snapshotOperation(ctx, root, paths, FileOutline, &request.IncludeImports, false, func(frozen sourceSnapshot) []string {
		args := []string{"outline"}
		if request.View != FullView {
			args = append(args, "--no-private")
		}
		if request.View == MinimalView {
			args = append(args, "--no-fields", "--no-docs", "--no-attrs")
		}
		if request.IncludeImports {
			args = append(args, "--imports")
		}
		args = append(args, "--json", "--")
		return append(args, frozen.paths...)
	})
}
func (a *Adapter) ShowSymbol(ctx context.Context, workspace string, request ShowSymbolRequest) (Result, error) {
	if err := a.operationContext(ctx); err != nil {
		return Result{}, err
	}
	root, err := astWorkspace(ctx, workspace)
	if err != nil {
		return operationFailure(ctx, ShowSymbol, err)
	}
	symbols, err := astStrings(request.Symbols, "symbols")
	if err != nil {
		return operationFailure(ctx, ShowSymbol, err)
	}
	var target string
	if astHasGlob(request.PathOrGlob) {
		targets, err := a.safeGlobPaths(ctx, root, request.PathOrGlob)
		if err != nil {
			return operationFailure(ctx, ShowSymbol, err)
		}
		if len(targets) == 0 {
			payload, _ := NewPayload(jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "matches", Value: jsonvalue.ArrayValue(nil)}}))
			return NewResult(ResultParameters{Status: NoMatch, Operation: ShowSymbol, Data: &payload, Message: "basename glob matched no workspace files"})
		}
		if len(targets) > 1 {
			visible := []jsonvalue.Value{}
			size := 0
			for _, path := range targets {
				relative, err := filepath.Rel(root, path)
				if err != nil {
					return operationFailure(ctx, ShowSymbol, err)
				}
				if len(visible) >= 32 || size+len(relative) > 16384 {
					break
				}
				visible = append(visible, jsonvalue.TextValue(relative))
				size += len(relative)
			}
			payload, _ := NewPayload(jsonvalue.ObjectValue([]jsonvalue.Field{{Name: "matches", Value: jsonvalue.ArrayValue(visible)}, {Name: "total_match_count", Value: jsonvalue.IntegerValue(int64(len(targets)))}, {Name: "truncated", Value: jsonvalue.BoolValue(len(visible) < len(targets))}}))
			return NewResult(ResultParameters{Status: Ambiguous, Operation: ShowSymbol, Data: &payload, Message: fmt.Sprintf("basename glob matched %d files; choose one explicit path", len(targets))})
		}
		target = targets[0]
	} else {
		target, err = astSafePath(ctx, root, request.PathOrGlob)
		if err != nil {
			return operationFailure(ctx, ShowSymbol, err)
		}
	}
	return a.snapshotOperation(ctx, root, []string{target}, ShowSymbol, nil, true, func(frozen sourceSnapshot) []string {
		args := []string{"show"}
		if request.SignatureOnly {
			args = append(args, "--signature")
		}
		args = append(args, "--json", "--", frozen.paths[0])
		return append(args, symbols...)
	})
}
func (a *Adapter) SymbolReferences(ctx context.Context, workspace string, request SymbolReferencesRequest) (Result, error) {
	if err := a.operationContext(ctx); err != nil {
		return Result{}, err
	}
	root, err := astWorkspace(ctx, workspace)
	if err != nil {
		return operationFailure(ctx, SymbolReferences, err)
	}
	patterns, err := astStrings(request.Patterns, "patterns")
	if err != nil {
		return operationFailure(ctx, SymbolReferences, err)
	}
	paths, err := selectedPaths(ctx, root, request.Paths)
	if err != nil {
		return operationFailure(ctx, SymbolReferences, err)
	}
	if len(request.Kinds) > 6 {
		return operationFailure(ctx, SymbolReferences, errors.New("too many kinds"))
	}
	kinds := make([]string, len(request.Kinds))
	for i, k := range request.Kinds {
		kinds[i] = k.String()
		if kinds[i] == "" {
			return operationFailure(ctx, SymbolReferences, errors.New("kinds must be drawn from ['call', 'comment', 'def', 'import', 'ref', 'string']"))
		}
	}
	var maxCount string
	if request.MaxPerFile != nil {
		count := *request.MaxPerFile
		if count <= 0 || count > maxASTGlobEntries {
			return operationFailure(ctx, SymbolReferences, errors.New("max_per_file must be an integer from 1 to 10000"))
		}
		maxCount = strconv.Itoa(count)
	}
	return a.snapshotOperation(ctx, root, paths, SymbolReferences, nil, false, func(frozen sourceSnapshot) []string {
		args := []string{"grep", "--json"}
		if len(kinds) > 0 {
			args = append(args, "--kind="+strings.Join(kinds, ","))
		}
		if maxCount != "" {
			args = append(args, "--max-count="+maxCount)
		}
		for _, p := range patterns {
			args = append(args, "--expression="+p)
		}
		args = append(args, "--")
		return append(args, frozen.paths...)
	})
}
