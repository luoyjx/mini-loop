package workspace

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
	"unicode/utf8"

	"github.com/luoyjx/mini-loop/go/protocol"
)

type globLink struct {
	Path   string `json:"path"`
	Target string `json:"target"`
}
type globSeries struct {
	Directory string `json:"directory"`
	Prefix    string `json:"prefix"`
	Count     int    `json:"count"`
}
type globCase struct {
	Name           string      `json:"name"`
	Pattern        string      `json:"pattern"`
	Files          []string    `json:"files"`
	Directories    []string    `json:"directories"`
	Links          []globLink  `json:"links"`
	Series         *globSeries `json:"series"`
	ExpectedOutput string      `json:"expected_output"`
	ExpectedError  bool        `json:"expected_error"`
}
type componentPatternCase struct {
	Pattern string   `json:"pattern"`
	Matches []string `json:"matches"`
}
type globContract struct {
	OutputCap  int                    `json:"output_cap"`
	Names      []string               `json:"names"`
	Components []componentPatternCase `json:"component_patterns"`
	Cases      []globCase             `json:"cases"`
}

func readGlobContract(t *testing.T) globContract {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-glob-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract globContract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	if contract.OutputCap != OutputCap || len(contract.Cases) != 38 {
		t.Fatalf("glob contract changed: %d cases cap=%d", len(contract.Cases), contract.OutputCap)
	}
	return contract
}

func TestFilenamePatternsMatchPythonFnmatch(t *testing.T) {
	contract := readGlobContract(t)
	for _, fixture := range contract.Components {
		t.Run(fixture.Pattern, func(t *testing.T) {
			matched := make([]string, 0)
			pattern := compileFilenamePattern(fixture.Pattern)
			for _, name := range contract.Names {
				if pattern.matches(name) {
					matched = append(matched, name)
				}
			}
			if !reflect.DeepEqual(matched, fixture.Matches) {
				t.Fatalf("matches=%q want=%q", matched, fixture.Matches)
			}
		})
	}
}

func TestGlobMatchesPythonContracts(t *testing.T) {
	contract := readGlobContract(t)
	for _, fixture := range contract.Cases {
		t.Run(fixture.Name, func(t *testing.T) {
			parent := t.TempDir()
			files, err := NewFiles(filepath.Join(parent, "workspace"))
			if err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(parent, "outside")
			if err := os.Mkdir(outside, 0o755); err != nil {
				t.Fatal(err)
			}
			outside, err = ResolvePath(outside)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outside, "outside.txt"), []byte("outside"), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, directory := range fixture.Directories {
				if err := os.MkdirAll(filepath.Join(files.Root(), directory), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			names := append([]string(nil), fixture.Files...)
			if fixture.Series != nil {
				for i := 0; i < fixture.Series.Count; i++ {
					names = append(names, fixture.Series.Directory+fixture.Series.Prefix+fmt.Sprintf("_%04d.txt", i))
				}
			}
			for _, name := range names {
				target := filepath.Join(files.Root(), name)
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, nil, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for _, link := range fixture.Links {
				target := strings.ReplaceAll(link.Target, "$OUTSIDE", outside)
				if err := os.Symlink(target, filepath.Join(files.Root(), link.Path)); err != nil {
					t.Fatal(err)
				}
			}
			pattern := strings.NewReplacer("$WORKSPACE", files.Root(), "$OUTSIDE", outside).Replace(fixture.Pattern)
			output, err := files.Glob(context.Background(), protocol.GlobInput{Pattern: pattern})
			if (err != nil) != fixture.ExpectedError {
				t.Fatalf("error=%v expected failure=%v", err, fixture.ExpectedError)
			}
			if err != nil {
				output = "Error: " + err.Error()
			}
			output = strings.NewReplacer(files.Root(), "$WORKSPACE", outside, "$OUTSIDE").Replace(output)
			if output != fixture.ExpectedOutput {
				t.Fatalf("output differs: got %q want %q", preview(output), preview(fixture.ExpectedOutput))
			}
			if utf8.RuneCountInString(output) > OutputCap {
				t.Fatal("glob exceeded the output cap")
			}
			for _, name := range names {
				content, err := os.ReadFile(filepath.Join(files.Root(), name))
				if err != nil || len(content) != 0 {
					t.Fatalf("glob changed %q: %q %v", name, content, err)
				}
			}
		})
	}
}

func TestGlobStopsOnCancellation(t *testing.T) {
	files, err := NewFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(files.Root(), "a.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	search := globSearch{ctx: ctx, root: files.Root(), patterns: make(map[string]filenamePattern)}
	err = search.descendants(files.Root(), false, func(string) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("enumeration ignored cancellation: %v", err)
	}
	if _, err := files.Glob(ctx, protocol.GlobInput{Pattern: "**"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled call returned %v", err)
	}
}
