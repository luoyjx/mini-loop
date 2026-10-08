package astcontext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type operationAudit struct {
	Args []string `json:"args"`
	Cwd  string   `json:"cwd"`
}

func operationTestChild() {
	cwd, _ := os.Getwd()
	file, err := os.OpenFile(os.Getenv("AST_CALLS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(9)
	}
	_ = json.NewEncoder(file).Encode(operationAudit{os.Args[1:], cwd})
	_ = file.Close()
	if reflect.DeepEqual(os.Args[1:], []string{"--version"}) {
		fmt.Printf("ast-outline 1.9.0\nhomepage: %s\n", Homepage)
		return
	}
	if changed := os.Getenv("AST_CHANGE_WORKSPACE"); changed != "" {
		_ = os.WriteFile(changed, []byte("changed during child\n"), 0600)
	}
	separator := 0
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator == 0 {
		os.Exit(10)
	}
	paths := os.Args[separator+1:]
	if os.Args[1] == "show" {
		paths = paths[:1]
	}
	for _, path := range paths {
		if contained(cwd, path) {
			os.Exit(11)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			os.Exit(12)
		}
		expected := "class One: pass\n"
		if filepath.Base(path) == "other.py" {
			expected = "class Other: pass\n"
		}
		if string(data) != expected {
			os.Exit(13)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0400 {
			os.Exit(14)
		}
	}
	_, _ = fmt.Fprintf(os.Stdout, `{"tool":"ast-outline","schema_version":1,"command":%q,"files":[],"results":[]}`, os.Args[1])
}
func TestPublicOperationsReplayActualSourceContracts(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-ast-operations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Operations []struct {
			Case struct {
				Name, Operation, Density, View  string
				Paths, Symbols, Patterns, Kinds []string
				IncludeImports                  bool   `json:"include_imports"`
				PathOrGlob                      string `json:"path_or_glob"`
				SignatureOnly                   bool   `json:"signature_only"`
				MaxPerFile                      *int   `json:"max_per_file"`
			}
			ResultJSON string `json:"result_json"`
			Calls      []operationAudit
			Cleaned    bool
		}
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Operations) != 26 {
		t.Fatal("Source operation inventory changed", len(fixture.Operations))
	}
	p, workspace := nativeAdapter(t)
	root, err := astWorkspace(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"code.py": "class One: pass\n", "other.py": "class Other: pass\n"} {
		if err = os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	calls := filepath.Join(t.TempDir(), "calls")
	p.EnvPassthrough = append(p.EnvPassthrough, "AST_CALLS", "AST_CHANGE_WORKSPACE")
	t.Setenv("AST_MODE", "operation")
	t.Setenv("AST_CALLS", calls)
	a := adapter(t, p)
	for _, row := range fixture.Operations {
		t.Run(row.Case.Name, func(t *testing.T) {
			_ = os.Remove(calls)
			c := row.Case
			var result Result
			var err error
			switch c.Operation {
			case "repo_map":
				d := DefaultDensity
				switch c.Density {
				case "names":
					d = NamesDensity
				case "compact":
					d = CompactDensity
				case "wide":
					d = WideDensity
				case "bad":
					d = Density(255)
				}
				result, err = a.RepoMap(context.Background(), root, RepoMapRequest{Paths: c.Paths, Density: d, IncludeImports: c.IncludeImports})
			case "file_outline":
				v := FullView
				switch c.View {
				case "public":
					v = PublicView
				case "minimal":
					v = MinimalView
				case "bad":
					v = OutlineView(255)
				}
				result, err = a.FileOutline(context.Background(), root, FileOutlineRequest{Paths: c.Paths, View: v, IncludeImports: c.IncludeImports})
			case "show_symbol":
				result, err = a.ShowSymbol(context.Background(), root, ShowSymbolRequest{PathOrGlob: c.PathOrGlob, Symbols: c.Symbols, SignatureOnly: c.SignatureOnly})
			case "symbol_references":
				kinds := make([]ReferenceKind, len(c.Kinds))
				for i, k := range c.Kinds {
					for _, kind := range []ReferenceKind{DefinitionReference, CallReference, SymbolReference, ImportReference, CommentReference, StringReference} {
						if kind.String() == k {
							kinds[i] = kind
						}
					}
				}
				result, err = a.SymbolReferences(context.Background(), root, SymbolReferencesRequest{Paths: c.Paths, Patterns: c.Patterns, Kinds: kinds, MaxPerFile: c.MaxPerFile})
			default:
				t.Fatal("unknown Source operation")
			}
			if err != nil {
				t.Fatal(err)
			}
			projection, err := result.ToJSON()
			if err != nil {
				t.Fatal(err)
			}
			projection = strings.ReplaceAll(projection, root, "<workspace>")
			if projection != row.ResultJSON {
				t.Fatalf("got %s\nwant %s", projection, row.ResultJSON)
			}
			actual := []operationAudit{}
			data, err := os.ReadFile(calls)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				if line == "" {
					continue
				}
				var audit operationAudit
				if err = json.Unmarshal([]byte(line), &audit); err != nil {
					t.Fatal(err)
				}
				audit.Cwd = strings.ReplaceAll(audit.Cwd, root, "<workspace>")
				for i, arg := range audit.Args {
					if strings.Contains(arg, "/mini-loop-ast-") {
						if _, err := os.Stat(arg); !errors.Is(err, os.ErrNotExist) {
							t.Fatal("snapshot remains", arg, err)
						}
						index := strings.Index(arg, "/workspace")
						if index < 0 {
							t.Fatal("unexpected snapshot path", arg)
						}
						audit.Args[i] = "<snapshot>" + arg[index+len("/workspace"):]
					}
				}
				actual = append(actual, audit)
			}
			if !reflect.DeepEqual(actual, row.Calls) {
				t.Fatalf("calls got %#v\nwant %#v", actual, row.Calls)
			}
			if !row.Cleaned {
				t.Fatal("Source cleanup failed")
			}
		})
	}
	// A workspace mutation after copy cannot change the bytes read by the CLI.
	t.Setenv("AST_CHANGE_WORKSPACE", filepath.Join(root, "code.py"))
	result, err := a.FileOutline(context.Background(), root, FileOutlineRequest{Paths: []string{"code.py"}})
	if err != nil || result.Parameters().Status != NoMatch {
		t.Fatal("child did not read frozen source", result.Parameters(), err)
	}
}

func TestPublicOperationsRefuseWorkspaceExecutableAndCallerCancellation(t *testing.T) {
	p, workspace := nativeAdapter(t)
	binary, err := os.ReadFile(p.Binary)
	if err != nil {
		t.Fatal(err)
	}
	p.Binary = filepath.Join(workspace, "model-cli")
	if err = os.WriteFile(p.Binary, binary, 0700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "marker")
	t.Setenv("AST_MARKER", marker)
	a := adapter(t, p)
	if err = os.WriteFile(filepath.Join(workspace, "code.py"), []byte("class One: pass\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := a.FileOutline(context.Background(), workspace, FileOutlineRequest{Paths: []string{"code.py"}})
	if err != nil || result.Parameters().Status != Incompatible {
		t.Fatal(result.Parameters(), err)
	}
	if _, err = os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("workspace executable ran", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = a.ShowSymbol(ctx, workspace, ShowSymbolRequest{PathOrGlob: "code.py", Symbols: []string{"One"}}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestAmbiguousShowBoundsBeforeAnyExecutableProbe(t *testing.T) {
	p, workspace := nativeAdapter(t)
	// A missing executable would change the result if ambiguity reached the probe.
	p.Binary = filepath.Join(workspace, "not-installed")
	a := adapter(t, p)
	for i := 0; i < 40; i++ {
		if err := os.WriteFile(filepath.Join(workspace, fmt.Sprintf("file-%02d.py", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := a.ShowSymbol(context.Background(), workspace, ShowSymbolRequest{PathOrGlob: "*.py", Symbols: []string{"One"}})
	if err != nil {
		t.Fatal(err)
	}
	parameters := result.Parameters()
	if parameters.Status != Ambiguous || parameters.Probe != nil {
		t.Fatal(parameters)
	}
	value := parameters.Data.Value()
	matches, _ := value.Lookup("matches")
	items, _ := matches.Array()
	total, _ := value.Lookup("total_match_count")
	count, _ := total.Integer()
	truncated, _ := value.Lookup("truncated")
	cut, _ := truncated.Bool()
	if len(items) != 32 || count != "40" || !cut {
		t.Fatal("visible match count cap failed", value)
	}
	parts := make([]string, 3)
	for i := range parts {
		parts[i] = strings.Repeat("α", 120)
	}
	relative := filepath.Join(parts...)
	directory := filepath.Join(workspace, relative)
	if err = os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if err = os.WriteFile(filepath.Join(directory, fmt.Sprintf("file-%02d.py", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	result, err = a.ShowSymbol(context.Background(), workspace, ShowSymbolRequest{PathOrGlob: filepath.Join(relative, "*.py"), Symbols: []string{"One"}})
	if err != nil {
		t.Fatal(err)
	}
	parameters = result.Parameters()
	if parameters.Status != Ambiguous || parameters.Probe != nil {
		t.Fatal(parameters)
	}
	matches, _ = parameters.Data.Value().Lookup("matches")
	items, _ = matches.Array()
	size := 0
	for _, item := range items {
		text, _ := item.Text()
		size += len(text)
	}
	next := filepath.Join(relative, "file-29.py")
	if len(items) == 0 || len(items) >= 30 || size > 16384 || size+len(next) <= 16384 {
		t.Fatal("visible UTF8 byte cap failed", len(items), size)
	}
}
