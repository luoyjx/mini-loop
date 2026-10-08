//go:build darwin || (linux && (amd64 || arm64))

package astcontext

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type astPathFixtureRow struct {
	Name, Operation, Error string
	ErrorClass             string `json:"error_class"`
	Input, Output          json.RawMessage
	MaxEntries             *int     `json:"max_entries"`
	TimeoutSeconds         *float64 `json:"timeout_seconds"`
	Label                  string
}

type astPathFixture struct {
	Files                          []snapshotFixtureFile
	Selections, Workspaces, Arrays []astPathFixtureRow
	Limits                         struct {
		Items, Characters int
		GlobEntries       int `json:"glob_entries"`
	}
}

func astSourcePaths(t *testing.T) astPathFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-ast-paths.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture astPathFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func astNormalizeFixture(text, root, outside string) string {
	text = strings.ReplaceAll(text, root, "<workspace>")
	text = strings.ReplaceAll(text, outside, "<outside>")
	return strings.ReplaceAll(text, filepath.Dir(root), "<scratch>")
}

func TestASTCanonicalPathsAndGlobsActualSourceRecipes(t *testing.T) {
	fixture := astSourcePaths(t)
	if len(fixture.Selections) != 63 {
		t.Fatal("Source path-selection inventory changed")
	}
	for _, row := range fixture.Selections {
		t.Run(row.Operation+"/"+row.Name, func(t *testing.T) {
			root, outside := snapshotFixtureTree(t, fixture.Files)
			t.Setenv("HOME", filepath.Join(root, "home"))
			var input string
			if err := json.Unmarshal(row.Input, &input); err != nil {
				// JSON numbers cannot enter the native named-string boundary.
				if row.ErrorClass != "ValueError" {
					t.Fatal("Source accepted a non-string path", row.Error)
				}
				return
			}
			input = strings.ReplaceAll(strings.ReplaceAll(input, "<workspace>", root), "<outside>", outside)
			var actualString string
			var actualPaths []string
			var err error
			if row.Operation == "path" {
				actualString, err = astSafePath(context.Background(), root, input)
			} else {
				seconds, limit := 15.0, maxASTGlobEntries
				if row.TimeoutSeconds != nil {
					seconds = *row.TimeoutSeconds
				}
				if row.MaxEntries != nil {
					limit = *row.MaxEntries
				}
				actualPaths, err = snapshotTestAdapter(t, seconds).safeGlobPathsLimit(context.Background(), root, input, limit)
			}
			if row.Error != "" {
				if err == nil || astNormalizeFixture(err.Error(), root, outside) != row.Error {
					t.Fatal(err, row.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if row.Operation == "path" {
				var expected string
				if err := json.Unmarshal(row.Output, &expected); err != nil {
					t.Fatal(err)
				}
				if actual := astNormalizeFixture(actualString, root, outside); actual != expected {
					t.Fatal(actual, expected)
				}
			} else {
				var expected []string
				if err := json.Unmarshal(row.Output, &expected); err != nil {
					t.Fatal(err)
				}
				for i, path := range actualPaths {
					actualPaths[i] = astNormalizeFixture(path, root, outside)
				}
				if !reflect.DeepEqual(actualPaths, expected) {
					t.Fatal(actualPaths, expected)
				}
			}
		})
	}
}

func TestASTWorkspaceAndArrayActualSourceRecipes(t *testing.T) {
	fixture := astSourcePaths(t)
	if len(fixture.Workspaces) != 5 || len(fixture.Arrays) != 12 {
		t.Fatal("Source workspace/array inventory changed")
	}
	for _, row := range fixture.Workspaces {
		t.Run("workspace/"+row.Name, func(t *testing.T) {
			root, outside := snapshotFixtureTree(t, fixture.Files)
			t.Setenv("HOME", filepath.Join(root, "home"))
			var input, expected string
			if err := json.Unmarshal(row.Input, &input); err != nil {
				t.Fatal(err)
			}
			input = strings.ReplaceAll(input, "<workspace>", root)
			actual, err := astWorkspace(context.Background(), input)
			if row.Error != "" {
				if err == nil || astNormalizeFixture(err.Error(), root, outside) != row.Error {
					t.Fatal(err, row.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(row.Output, &expected); err != nil {
				t.Fatal(err)
			}
			if actual := astNormalizeFixture(actual, root, outside); actual != expected {
				t.Fatal(actual, expected)
			}
		})
	}
	for _, row := range fixture.Arrays {
		t.Run("array/"+row.Name, func(t *testing.T) {
			var input, expected []string
			if err := json.Unmarshal(row.Input, &input); err != nil {
				if row.ErrorClass != "ValueError" {
					t.Fatal("Source accepted non-string array", row.Error)
				}
				return
			}
			actual, err := astStrings(input, row.Label)
			if row.Error != "" {
				if err == nil || err.Error() != row.Error {
					t.Fatal(err, row.Error)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(row.Output, &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatal(actual, expected)
			}
		})
	}
	if fixture.Limits.Items != maxASTInputItems || fixture.Limits.Characters != maxASTInputCharacters || fixture.Limits.GlobEntries != maxASTGlobEntries {
		t.Fatal("Source AST input limits changed")
	}
	input := []string{"original"}
	output, err := astStrings(input, "symbols")
	if err != nil {
		t.Fatal(err)
	}
	output[0] = "changed"
	if input[0] != "original" {
		t.Fatal("array validation returned caller-owned storage")
	}
}

func TestASTSelectionToSnapshotRefusesPostSelectionSymlinkSwap(t *testing.T) {
	root, outside := snapshotFixtureTree(t, []snapshotFixtureFile{{Path: "source.py", Text: "source\n"}})
	adapter := snapshotTestAdapter(t, 15)
	paths, err := adapter.safeGlobPaths(context.Background(), root, "*.py")
	if err != nil || len(paths) != 1 {
		t.Fatal(paths, err)
	}
	if err := os.Remove(paths[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.py"), paths[0]); err != nil {
		t.Fatal(err)
	}
	called := false
	err = adapter.withSnapshot(context.Background(), root, paths, func(sourceSnapshot) error { called = true; return nil })
	if err == nil || err.Error() != "ast-outline snapshot target may not be a symlink" || called {
		t.Fatal("swapped match reached snapshot consumer", err, called)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := adapter.safeGlobPaths(ctx, root, "*.py"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := astSafePath(ctx, root, "source.py"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := astWorkspace(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := astWorkspace(context.Background(), "\x00"); err == nil || err.Error() != "embedded null byte" {
		t.Fatal("workspace NUL boundary", err)
	}
}
