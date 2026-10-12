// contractaudit inventories Source fixture references and their candidate Go test
// callers. A passed caller does not prove a fixture was read or fully asserted.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

type packageMetadata struct {
	ImportPath, Dir                              string
	GoFiles, CgoFiles, TestGoFiles, XTestGoFiles []string
	Module                                       *struct{ Main bool }
}
type reference struct {
	File        string   `json:"file"`
	Line        int      `json:"line"`
	Function    string   `json:"function"`
	EnclosingIf []string `json:"enclosing_if,omitempty"`
}
type testCaller struct {
	Package string `json:"package"`
	Name    string `json:"name"`
	Action  string `json:"action"`
}
type fixtureInventory struct {
	Name          string       `json:"name"`
	SHA256        string       `json:"sha256"`
	References    []reference  `json:"references"`
	Callers       []testCaller `json:"candidate_callers"`
	Status        string       `json:"status"`
	ProducerScope string       `json:"producer_scope"`
}
type inventory struct {
	Root           string             `json:"root"`
	Packages       int                `json:"packages"`
	SourceHashes   map[string]string  `json:"source_hashes"`
	ResultsSHA256  string             `json:"results_sha256,omitempty"`
	ManifestSHA256 string             `json:"source_manifest_sha256,omitempty"`
	Fixtures       []fixtureInventory `json:"fixtures"`
	Limitations    []string           `json:"limitations"`
}
type functionNode struct {
	key, group, name, packagePath string
	test                          bool
	calls                         []string
	references                    map[string][]reference
}
type testEvent struct{ Action, Package, Test string }
type manifestFixture struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}
type sourceManifest struct {
	ProducerSHA256 string            `json:"producer_sha256"`
	Files          []manifestFixture `json:"files"`
}

func digest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func compilerPackages(root string) ([]packageMetadata, error) {
	command := exec.Command("go", "list", "-mod=readonly", "-json", "./...")
	command.Dir = root
	data, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	var result []packageMetadata
	for {
		var p packageMetadata
		err = decoder.Decode(&p)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if p.Module != nil && p.Module.Main {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no main-module packages")
	}
	return result, nil
}
func testFunction(decl *ast.FuncDecl, imports map[string]string) bool {
	if decl.Recv != nil || !strings.HasPrefix(decl.Name.Name, "Test") || len(decl.Name.Name) <= 4 {
		return false
	}
	if unicode.IsLower([]rune(decl.Name.Name[4:])[0]) || decl.Type.Params == nil || len(decl.Type.Params.List) != 1 {
		return false
	}
	if decl.Type.Results != nil && len(decl.Type.Results.List) > 0 {
		return false
	}
	field := decl.Type.Params.List[0]
	if len(field.Names) > 1 {
		return false
	}
	pointer, ok := field.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	selector, ok := pointer.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	owner, ok := selector.X.(*ast.Ident)
	return ok && imports[owner.Name] == "testing" && selector.Sel.Name == "T"
}
func functions(root string, packages []packageMetadata, fixtures map[string]bool) ([]functionNode, map[string]string, error) {
	var nodes []functionNode
	hashes := map[string]string{}
	positions := token.NewFileSet()
	for _, p := range packages {
		files := append(append(append(slices.Clone(p.GoFiles), p.CgoFiles...), p.TestGoFiles...), p.XTestGoFiles...)
		for _, name := range files {
			path := filepath.Join(p.Dir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, nil, err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return nil, nil, err
			}
			relative = filepath.ToSlash(relative)
			hashes[relative] = digest(data)
			file, err := parser.ParseFile(positions, path, data, parser.AllErrors)
			if err != nil {
				return nil, nil, err
			}
			imports := map[string]string{}
			for _, item := range file.Imports {
				value, err := strconv.Unquote(item.Path.Value)
				if err != nil {
					return nil, nil, err
				}
				alias := filepath.Base(value)
				if item.Name != nil {
					alias = item.Name.Name
				}
				imports[alias] = value
			}
			group := p.ImportPath + "#" + file.Name.Name
			for _, item := range file.Decls {
				decl, ok := item.(*ast.FuncDecl)
				if !ok || decl.Body == nil {
					continue
				}
				node := functionNode{group: group, name: decl.Name.Name, packagePath: p.ImportPath, references: map[string][]reference{}}
				node.key = group + "." + node.name
				// Methods cannot be resolved by this direct-function graph; keep their
				// references visible without inventing a receiver or interface call edge.
				if decl.Recv != nil {
					node.key = fmt.Sprintf("%s@%s:%d", node.key, relative, positions.Position(decl.Pos()).Line)
				}
				node.test = strings.HasSuffix(name, "_test.go") && testFunction(decl, imports)
				var ancestors []ast.Node
				ast.Inspect(decl.Body, func(value ast.Node) bool {
					if value == nil {
						ancestors = ancestors[:len(ancestors)-1]
						return false
					}
					ancestors = append(ancestors, value)
					switch v := value.(type) {
					case *ast.BasicLit:
						if v.Kind != token.STRING {
							return true
						}
						text, err := strconv.Unquote(v.Value)
						if err != nil {
							return true
						}
						base := filepath.Base(text)
						if fixtures[base] {
							var guards []string
							for index, parent := range ancestors {
								if branch, ok := parent.(*ast.IfStmt); ok {
									start, end := positions.Position(branch.Cond.Pos()).Offset, positions.Position(branch.Cond.End()).Offset
									label := "condition: "
									if index+1 < len(ancestors) {
										if ancestors[index+1] == branch.Body {
											label = "then: "
										} else if ancestors[index+1] == branch.Else {
											label = "else: "
										}
									}
									guards = append(guards, label+string(data[start:end]))
								}
							}
							node.references[base] = append(node.references[base], reference{File: relative, Line: positions.Position(v.Pos()).Line, Function: node.name, EnclosingIf: guards})
						}
					case *ast.CallExpr:
						target, ok := v.Fun.(*ast.Ident)
						if ok && (target.Obj == nil || target.Obj.Kind == ast.Fun) {
							node.calls = append(node.calls, group+"."+target.Name)
						}
					}
					return true
				})
				nodes = append(nodes, node)
			}
		}
	}
	return nodes, hashes, nil
}
func testResults(path string) (map[string]string, string, error) {
	if path == "" {
		return nil, "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("empty test result stream")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	result := map[string]string{}
	for {
		var event testEvent
		err = decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("test results: %w", err)
		}
		if event.Test != "" && !strings.Contains(event.Test, "/") && (event.Action == "pass" || event.Action == "skip" || event.Action == "fail") {
			result[event.Package+"."+event.Test] = event.Action
		}
	}
	return result, digest(data), nil
}
func buildInventory(root string, packages []packageMetadata, resultsPath, manifestPath string) (inventory, error) {
	out := inventory{Root: root, Packages: len(packages), Limitations: []string{
		"Active go list profile only; Source freshness must be checked independently.",
		"Literal references and direct named function calls form candidate links, not runtime fixture-read or assertion proof.",
		"Methods, interface calls, aliases, global constants and dynamically constructed fixture paths require separate review.",
		"A passed test caller may skip a branch, omit assertions or be diagnostic-only; semantic parity remains a separate requirement.",
	}}
	entries, err := os.ReadDir(filepath.Join(root, "testdata"))
	if err != nil {
		return out, err
	}
	names := map[string]bool{}
	for _, entry := range entries {
		n := entry.Name()
		if !entry.IsDir() && strings.HasPrefix(n, "python-") && (strings.HasSuffix(n, ".json") || strings.HasSuffix(n, ".sql")) {
			names[n] = true
		}
	}
	if len(names) == 0 {
		return out, fmt.Errorf("no Source fixture files")
	}
	manifestNames := map[string]string{}
	if manifestPath != "" {
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			return out, err
		}
		var manifest sourceManifest
		if err = json.Unmarshal(data, &manifest); err != nil {
			return out, err
		}
		if len(manifest.Files) == 0 || len(manifest.ProducerSHA256) != 64 {
			return out, fmt.Errorf("invalid Source manifest")
		}
		for _, file := range manifest.Files {
			if !names[file.Name] {
				return out, fmt.Errorf("missing Source fixture %s", file.Name)
			}
			if _, exists := manifestNames[file.Name]; exists {
				return out, fmt.Errorf("duplicate Source fixture %s", file.Name)
			}
			manifestNames[file.Name] = file.SHA256
		}
		out.ManifestSHA256 = digest(data)
	}
	nodes, hashes, err := functions(root, packages, names)
	if err != nil {
		return out, err
	}
	out.SourceHashes = hashes
	results, resultSHA, err := testResults(resultsPath)
	if err != nil {
		return out, err
	}
	out.ResultsSHA256 = resultSHA
	graph := map[string]functionNode{}
	for _, node := range nodes {
		if _, exists := graph[node.key]; exists {
			return out, fmt.Errorf("ambiguous function %s", node.key)
		}
		graph[node.key] = node
	}
	callers := map[string][]testCaller{}
	for _, rootNode := range nodes {
		if !rootNode.test {
			continue
		}
		seen := map[string]bool{}
		queue := []string{rootNode.key}
		for len(queue) > 0 {
			key := queue[0]
			queue = queue[1:]
			if seen[key] {
				continue
			}
			seen[key] = true
			node, ok := graph[key]
			if !ok {
				continue
			}
			action := "not_observed"
			if resultsPath == "" {
				action = "not_supplied"
			} else if value, exists := results[rootNode.packagePath+"."+rootNode.name]; exists {
				action = value
			}
			callers[key] = append(callers[key], testCaller{Package: rootNode.packagePath, Name: rootNode.name, Action: action})
			queue = append(queue, node.calls...)
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	slices.Sort(ordered)
	for _, name := range ordered {
		data, err := os.ReadFile(filepath.Join(root, "testdata", name))
		if err != nil {
			return out, err
		}
		value := fixtureInventory{Name: name, SHA256: digest(data), References: []reference{}, Callers: []testCaller{}, Status: "unreferenced"}
		value.ProducerScope = "not_supplied"
		if manifestPath != "" {
			value.ProducerScope = "outside_current_export"
			if expected, ok := manifestNames[name]; ok {
				value.ProducerScope = "current_export"
				if value.SHA256 != expected {
					return out, fmt.Errorf("Source fixture hash mismatch: %s", name)
				}
			}
		}
		unique := map[string]bool{}
		for _, node := range nodes {
			refs := node.references[name]
			if len(refs) == 0 {
				continue
			}
			value.References = append(value.References, refs...)
			for _, caller := range callers[node.key] {
				key := caller.Package + "." + caller.Name
				if !unique[key] {
					value.Callers = append(value.Callers, caller)
					unique[key] = true
				}
			}
		}
		slices.SortFunc(value.References, func(a, b reference) int {
			if a.File != b.File {
				return strings.Compare(a.File, b.File)
			}
			return a.Line - b.Line
		})
		slices.SortFunc(value.Callers, func(a, b testCaller) int { return strings.Compare(a.Package+"."+a.Name, b.Package+"."+b.Name) })
		if len(value.References) > 0 {
			value.Status = "unlinked"
		}
		if len(value.Callers) > 0 {
			value.Status = "test_not_observed"
			if resultsPath == "" {
				value.Status = "candidate_link"
			}
		}
		for _, caller := range value.Callers {
			if caller.Action == "pass" {
				value.Status = "test_pass_observed"
				break
			}
		}
		out.Fixtures = append(out.Fixtures, value)
	}
	return out, nil
}
func requireResults(value inventory) error {
	if value.ResultsSHA256 == "" {
		return fmt.Errorf("-require-results needs a nonempty Go test JSON result file")
	}
	missing := 0
	for _, fixture := range value.Fixtures {
		if fixture.Status != "test_pass_observed" {
			missing++
		}
	}
	if missing > 0 {
		return fmt.Errorf("%d fixtures lack a candidate caller with an observed passing test result", missing)
	}
	return nil
}
func run() int {
	root := flag.String("root", ".", "Go module root")
	results := flag.String("results", "", "Go test -json stdout; keep stderr separate")
	manifest := flag.String("manifest", "", "successful Source exporter manifest; verify current fixture bytes")
	require := flag.Bool("require-results", false, "reject absent candidate passing test results; does not certify parity")
	flag.Parse()
	absolute, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	packages, err := compilerPackages(absolute)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	value, err := buildInventory(absolute, packages, *results, *manifest)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *require {
		if err = requireResults(value); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	return 0
}
func main() { os.Exit(run()) }
