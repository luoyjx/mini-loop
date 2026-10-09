package reload

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWatcherMatchesActualStatReload(t *testing.T) {
	data, err := os.ReadFile("../testdata/python-reload.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Steps []struct {
			Operation, Path string
			Mtime           int64
			Changed         []string
		}
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	for _, language := range []Language{PythonSource, GoSource} {
		t.Run(string(language), func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			watcher, err := NewWatcher(WatchConfig{Roots: []string{root}, Language: language})
			if err != nil {
				t.Fatal(err)
			}
			adapt := func(path string) string {
				if language == GoSource {
					return strings.ReplaceAll(path, ".py", ".go")
				}
				return path
			}
			for index, step := range corpus.Steps {
				path := filepath.Join(root, adapt(step.Path))
				switch step.Operation {
				case "write":
					if err := os.WriteFile(path, []byte("changed source"), 0600); err != nil {
						t.Fatal(err)
					}
					stamp := time.Unix(step.Mtime, 0)
					if err := os.Chtimes(path, stamp, stamp); err != nil {
						t.Fatal(err)
					}
				case "remove":
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				case "symlink":
					if err := os.Symlink(filepath.Join(root, "notes.txt"), path); err != nil {
						t.Fatal(err)
					}
				case "mkdir", "touch":
					if step.Operation == "mkdir" {
						if err := os.Mkdir(path, 0700); err != nil {
							t.Fatal(err)
						}
					}
					stamp := time.Unix(step.Mtime, 0)
					if err := os.Chtimes(path, stamp, stamp); err != nil {
						t.Fatal(err)
					}
				case "reconstruct":
					watcher.Reset()
				case "scan":
					change, err := watcher.Scan(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					got := []string{}
					if change.Path != "" {
						relative, err := filepath.Rel(root, change.Path)
						if err != nil {
							t.Fatal(err)
						}
						got = append(got, relative)
					}
					want := []string{}
					for _, path := range step.Changed {
						want = append(want, adapt(path))
					}
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("step %d: %v; want %v", index, got, want)
					}
				default:
					t.Fatal("unknown Source operation", step.Operation)
				}
			}
		})
	}
}

func TestWatcherCancellationAndOwnedConfig(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	roots := []string{root}
	delay := time.Hour
	watcher, err := NewWatcher(WatchConfig{Roots: roots, Delay: &delay})
	if err != nil {
		t.Fatal(err)
	}
	roots[0], delay = "/missing", 0
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if change, err := watcher.Next(ctx); !errors.Is(err, context.Canceled) || change.Path != "" {
		t.Fatal(change, err)
	}
	if change, err := watcher.Scan(ctx); !errors.Is(err, context.Canceled) || change.Path != "" {
		t.Fatal(change, err)
	}
	if watcher.delay != time.Hour || watcher.roots[0] != root {
		t.Fatal("caller changed retained config")
	}
}

func TestWatcherValidation(t *testing.T) {
	negative := -time.Second
	for _, config := range []WatchConfig{
		{}, {Roots: []string{""}}, {Roots: []string{"/missing"}},
		{Roots: []string{t.TempDir()}, Language: "unknown"},
		{Roots: []string{t.TempDir()}, Delay: &negative},
	} {
		if _, err := NewWatcher(config); err == nil {
			t.Fatal("invalid watcher admitted", config)
		}
	}
}
