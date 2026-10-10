//go:build linux || (darwin && cgo)

package reload

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestNativeEventsActualSourceRecipes(t *testing.T) {
	var fixture struct {
		Cases []struct {
			DirectoryAlias  string   `json:"directory_alias"`
			DirectoryTarget string   `json:"directory_target"`
			Name            string   `json:"name"`
			Path            string   `json:"path"`
			Initial         bool     `json:"initial"`
			Operation       string   `json:"operation"`
			Mtime           int64    `json:"mtime"`
			Destination     string   `json:"destination"`
			SymlinkTarget   string   `json:"symlink_target"`
			Changed         []string `json:"changed"`
		} `json:"native_events"`
	}
	fixturePath := "../testdata/python-watchfiles.json"
	if runtime.GOOS == "linux" {
		fixturePath = "../testdata/python-watchfiles-linux.json"
	}
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	expectedCases := 16
	if runtime.GOOS == "linux" {
		expectedCases = 18
	}
	if len(fixture.Cases) != expectedCases {
		t.Fatalf("expected %d native cases, got %d", expectedCases, len(fixture.Cases))
	}
	for _, recipe := range fixture.Cases {
		t.Run(recipe.Name, func(t *testing.T) {
			adapt := func(name string) string { return strings.ReplaceAll(name, ".py", ".go") }
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if recipe.DirectoryAlias != "" {
				if err := os.Mkdir(filepath.Join(root, recipe.DirectoryTarget), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(recipe.DirectoryTarget, filepath.Join(root, recipe.DirectoryAlias)); err != nil {
					t.Fatal(err)
				}
			}
			sourcePath := filepath.Join(root, adapt(recipe.Path))
			if err = os.MkdirAll(filepath.Dir(sourcePath), 0700); err != nil {
				t.Fatal(err)
			}
			write := func(path, text string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			stamp := func(seconds int64) {
				t.Helper()
				value := time.Unix(seconds, 0)
				if err := os.Chtimes(sourcePath, value, value); err != nil {
					t.Fatal(err)
				}
			}
			if recipe.SymlinkTarget != "" {
				write(filepath.Join(root, recipe.SymlinkTarget), "before\n")
				if err = os.Symlink(recipe.SymlinkTarget, sourcePath); err != nil {
					t.Fatal(err)
				}
			}
			if recipe.Initial {
				write(sourcePath, "before\n")
				stamp(100)
			}
			observer, err := NewNativeEvents([]string{root}, GoSource)
			if err != nil {
				t.Fatal(err)
			}
			defer observer.Close()
			// Drain pre-registration writes while retaining this exact subscription.
			for {
				ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				_, err := observer.Next(ctx)
				cancel()
				if errors.Is(err, context.DeadlineExceeded) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			switch recipe.Operation {
			case "write", "add":
				write(sourcePath, "after\n")
				stamp(recipe.Mtime)
			case "unchanged-write":
				write(sourcePath, "before\n")
				stamp(recipe.Mtime)
			case "touch":
				stamp(recipe.Mtime)
			case "remove":
				err = os.Remove(sourcePath)
			case "rename":
				err = os.Rename(sourcePath, filepath.Join(root, adapt(recipe.Destination)))
			case "mkdir":
				err = os.Mkdir(sourcePath, 0700)
			case "transient":
				write(sourcePath, "temporary\n")
				err = os.Remove(sourcePath)
			default:
				t.Fatalf("unknown actual recipe %q", recipe.Operation)
			}
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			batch, err := observer.Next(ctx)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(batch.Paths))
			for _, path := range batch.Paths {
				relative, err := filepath.Rel(root, path)
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, filepath.ToSlash(relative))
			}
			want := make([]string, 0, len(recipe.Changed))
			for _, path := range recipe.Changed {
				want = append(want, adapt(path))
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v want %v", got, want)
			}
		})
	}
}
