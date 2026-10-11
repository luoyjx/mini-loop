package protocol

import (
	"encoding/json"
	"errors"
	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

const (
	ToolRepoMap          ToolName = "repo_map"
	ToolFileOutline      ToolName = "file_outline"
	ToolShowSymbol       ToolName = "show_symbol"
	ToolSymbolReferences ToolName = "symbol_references"
)

type ASTDensity string

const (
	ASTNames   ASTDensity = "names"
	ASTCompact ASTDensity = "compact"
	ASTDefault ASTDensity = "default"
	ASTWide    ASTDensity = "wide"
)

type ASTView string

const (
	ASTFull    ASTView = "full"
	ASTPublic  ASTView = "public"
	ASTMinimal ASTView = "minimal"
)

type ASTReferenceKind string

const (
	ASTDefinition ASTReferenceKind = "def"
	ASTCall       ASTReferenceKind = "call"
	ASTReference  ASTReferenceKind = "ref"
	ASTImport     ASTReferenceKind = "import"
	ASTComment    ASTReferenceKind = "comment"
	ASTString     ASTReferenceKind = "string"
)

// Optional pointers preserve supplied fields for replay; defaults apply only at execution.
type RepoMapInput struct {
	Paths          []string    `json:"paths"`
	Density        *ASTDensity `json:"density,omitempty"`
	IncludeImports *bool       `json:"include_imports,omitempty"`
}
type FileOutlineInput struct {
	Paths          []string `json:"paths"`
	View           *ASTView `json:"view,omitempty"`
	IncludeImports *bool    `json:"include_imports,omitempty"`
}
type ShowSymbolInput struct {
	PathOrGlob    string   `json:"path_or_glob"`
	Symbols       []string `json:"symbols"`
	SignatureOnly *bool    `json:"signature_only,omitempty"`
}
type SymbolReferencesInput struct {
	Patterns   []string            `json:"patterns"`
	Paths      []string            `json:"paths"`
	Kinds      *[]ASTReferenceKind `json:"kinds,omitempty"`
	MaxPerFile *int                `json:"max_per_file,omitempty"`
}

func astPointer[T ~string | ~bool | ~int](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
func astStringsCopy(values []string) []string {
	return append(make([]string, 0, len(values)), values...)
}
func RepoMapToolInput(v RepoMapInput) ToolInput {
	v.Paths = astStringsCopy(v.Paths)
	v.Density = astPointer(v.Density)
	v.IncludeImports = astPointer(v.IncludeImports)
	return ToolInput{name: ToolRepoMap, repoMap: v}
}
func FileOutlineToolInput(v FileOutlineInput) ToolInput {
	v.Paths = astStringsCopy(v.Paths)
	v.View = astPointer(v.View)
	v.IncludeImports = astPointer(v.IncludeImports)
	return ToolInput{name: ToolFileOutline, fileOutline: v}
}
func ShowSymbolToolInput(v ShowSymbolInput) ToolInput {
	v.Symbols = astStringsCopy(v.Symbols)
	v.SignatureOnly = astPointer(v.SignatureOnly)
	return ToolInput{name: ToolShowSymbol, showSymbol: v}
}
func SymbolReferencesToolInput(v SymbolReferencesInput) ToolInput {
	v.Paths = astStringsCopy(v.Paths)
	v.Patterns = astStringsCopy(v.Patterns)
	v.MaxPerFile = astPointer(v.MaxPerFile)
	if v.Kinds != nil {
		kinds := append(make([]ASTReferenceKind, 0, len(*v.Kinds)), (*v.Kinds)...)
		v.Kinds = &kinds
	}
	return ToolInput{name: ToolSymbolReferences, symbolReferences: v}
}
func (i ToolInput) RepoMap() (RepoMapInput, bool) {
	return RepoMapToolInput(i.repoMap).repoMap, i.name == ToolRepoMap
}
func (i ToolInput) FileOutline() (FileOutlineInput, bool) {
	return FileOutlineToolInput(i.fileOutline).fileOutline, i.name == ToolFileOutline
}
func (i ToolInput) ShowSymbol() (ShowSymbolInput, bool) {
	return ShowSymbolToolInput(i.showSymbol).showSymbol, i.name == ToolShowSymbol
}
func (i ToolInput) SymbolReferences() (SymbolReferencesInput, bool) {
	return SymbolReferencesToolInput(i.symbolReferences).symbolReferences, i.name == ToolSymbolReferences
}
func IsASTTool(name ToolName) bool {
	return name == ToolRepoMap || name == ToolFileOutline || name == ToolShowSymbol || name == ToolSymbolReferences
}
func (i ToolInput) marshalAST() ([]byte, error) {
	switch i.name {
	case ToolRepoMap:
		return json.Marshal(i.repoMap)
	case ToolFileOutline:
		return json.Marshal(i.fileOutline)
	case ToolShowSymbol:
		return json.Marshal(i.showSymbol)
	case ToolSymbolReferences:
		return json.Marshal(i.symbolReferences)
	}
	return nil, errors.New("wrong AST input variant")
}
func (i ToolInput) validateAST() error {
	switch i.name {
	case ToolRepoMap:
		if d := i.repoMap.Density; d != nil && *d != ASTNames && *d != ASTCompact && *d != ASTDefault && *d != ASTWide {
			return errors.New("invalid AST density")
		}
	case ToolFileOutline:
		if v := i.fileOutline.View; v != nil && *v != ASTFull && *v != ASTPublic && *v != ASTMinimal {
			return errors.New("invalid AST view")
		}
	case ToolSymbolReferences:
		if kinds := i.symbolReferences.Kinds; kinds != nil {
			for _, k := range *kinds {
				switch k {
				case ASTDefinition, ASTCall, ASTReference, ASTImport, ASTComment, ASTString:
				default:
					return errors.New("invalid AST reference kind")
				}
			}
		}
	}
	return nil
}
func decodeASTInput(name ToolName, data []byte) (ToolInput, error) {
	// This transient closed tree checks required presence and explicit-null fields;
	// stored/executed payloads are the named variants below.
	tree, err := jsonvalue.Decode(string(data))
	if err != nil {
		return ToolInput{}, err
	}
	if tree.Kind() != jsonvalue.Object {
		return ToolInput{}, errors.New("AST input must be an object")
	}
	for _, key := range tree.Keys() {
		v, _ := tree.Lookup(key)
		if v.Kind() == jsonvalue.Null {
			return ToolInput{}, errors.New("AST fields cannot be null")
		}
	}
	required := []string{"paths"}
	if name == ToolShowSymbol {
		required = []string{"path_or_glob", "symbols"}
	}
	if name == ToolSymbolReferences {
		required = []string{"patterns", "paths"}
	}
	for _, key := range required {
		if _, ok := tree.Lookup(key); !ok {
			return ToolInput{}, errors.New("required AST input field missing: " + key)
		}
	}
	var result ToolInput
	switch name {
	case ToolRepoMap:
		var v RepoMapInput
		err = decodeToolObject(data, &v)
		result = RepoMapToolInput(v)
	case ToolFileOutline:
		var v FileOutlineInput
		err = decodeToolObject(data, &v)
		result = FileOutlineToolInput(v)
	case ToolShowSymbol:
		var v ShowSymbolInput
		err = decodeToolObject(data, &v)
		result = ShowSymbolToolInput(v)
	case ToolSymbolReferences:
		var v SymbolReferencesInput
		err = decodeToolObject(data, &v)
		result = SymbolReferencesToolInput(v)
	}
	if err != nil {
		return ToolInput{}, err
	}
	if err = result.Validate(); err != nil {
		return ToolInput{}, err
	}
	return result, nil
}
func (i ToolInput) maskAST(mask func(string) string) ToolInput {
	apply := func(items []string) {
		for n, text := range items {
			items[n] = mask(text)
		}
	}
	switch i.name {
	case ToolRepoMap:
		apply(i.repoMap.Paths)
		if i.repoMap.Density != nil {
			*i.repoMap.Density = ASTDensity(mask(string(*i.repoMap.Density)))
		}
	case ToolFileOutline:
		apply(i.fileOutline.Paths)
		if i.fileOutline.View != nil {
			*i.fileOutline.View = ASTView(mask(string(*i.fileOutline.View)))
		}
	case ToolShowSymbol:
		i.showSymbol.PathOrGlob = mask(i.showSymbol.PathOrGlob)
		apply(i.showSymbol.Symbols)
	case ToolSymbolReferences:
		apply(i.symbolReferences.Patterns)
		apply(i.symbolReferences.Paths)
		if i.symbolReferences.Kinds != nil {
			for n, k := range *i.symbolReferences.Kinds {
				(*i.symbolReferences.Kinds)[n] = ASTReferenceKind(mask(string(k)))
			}
		}
	}
	return i
}
func ASTToolSchemas() []ToolSchema {
	integer := func(v int) *int { return &v }
	def := func(v jsonvalue.Value) *jsonvalue.Value { return &v }
	strings := func() InputSchema {
		return InputSchema{Type: SchemaArray, Items: &InputSchema{Type: SchemaString, MaxLength: integer(2048)}, MinItems: integer(1), MaxItems: integer(64)}
	}
	imports := InputSchema{Type: SchemaBoolean, Default: def(jsonvalue.BoolValue(false))}
	maps := SchemaProperties{"paths": strings(), "density": {Type: SchemaString, Enum: []string{"names", "compact", "default", "wide"}, Default: def(jsonvalue.TextValue("default"))}, "include_imports": imports}
	outline := SchemaProperties{"paths": strings(), "view": {Type: SchemaString, Enum: []string{"full", "public", "minimal"}, Default: def(jsonvalue.TextValue("full"))}, "include_imports": imports}
	show := SchemaProperties{"path_or_glob": {Type: SchemaString, MaxLength: integer(2048), Description: "Workspace source file, or a non-recursive basename glob in a static workspace directory; '**' and intermediate wildcards are rejected."}, "symbols": strings(), "signature_only": imports}
	references := SchemaProperties{"patterns": strings(), "paths": strings(), "kinds": {Type: SchemaArray, Items: &InputSchema{Type: SchemaString, Enum: []string{"def", "call", "ref", "import", "comment", "string"}}, MaxItems: integer(6), Default: def(jsonvalue.ArrayValue(nil))}, "max_per_file": {Type: SchemaInteger, Minimum: integer(1), Maximum: integer(10000)}}
	schema := func(name ToolName, description string, props SchemaProperties, required ...string) ToolSchema {
		return ToolSchema{Name: name, Description: description, InputSchema: InputSchema{Type: SchemaObject, Additional: AdditionalAllowed(false), Properties: &props, Required: required}}
	}
	return []ToolSchema{
		schema(ToolRepoMap, "Map supported source files with ast-outline digest; returns JSON status applied, no_match, partial, error, missing, or incompatible.", maps, "paths"),
		schema(ToolFileOutline, "Read structural signatures and line ranges for files or directories without full method bodies.", outline, "paths"),
		schema(ToolShowSymbol, "Read exact source or signature for named symbols inside one workspace source file or non-recursive basename glob.", show, "path_or_glob", "symbols"),
		schema(ToolSymbolReferences, "Find definitions, calls, references, or imports grouped by enclosing scope with ast-outline grep.", references, "patterns", "paths"),
	}
}
