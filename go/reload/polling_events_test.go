package reload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPollingEventsActualSourceRecipes(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Name, Path, Operation, Destination string
			SymlinkTarget                      string `json:"symlink_target"`
			DirectoryAlias                     string `json:"directory_alias"`
			DirectoryTarget                    string `json:"directory_target"`
			Initial                            bool
			InitialMtime                       *float64 `json:"initial_mtime"`
			Mtime                              float64
			Changed                            []string
		} `json:"polling_events"`
	}
	data, err := os.ReadFile("../testdata/python-watchfiles.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) != 20 {
		t.Fatal("missing actual polling cases", len(fixture.Cases))
	}
	profile, err := SelectObserverProfile(map[string]string{"WATCHFILES_FORCE_POLLING": "1"}, ObserverHost{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, recipe := range fixture.Cases {
		t.Run(recipe.Name, func(t *testing.T) {
			adapt := func(path string) string { return strings.ReplaceAll(path, ".py", ".go") }
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, adapt(recipe.Path))
			if recipe.DirectoryAlias != "" {
				if err = os.Mkdir(filepath.Join(root, recipe.DirectoryTarget), 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(recipe.DirectoryTarget, filepath.Join(root, recipe.DirectoryAlias)); err != nil {
					t.Fatal(err)
				}
			}
			if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			write := func(path, text string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			stamp := func(seconds float64) {
				t.Helper()
				whole := int64(math.Floor(seconds))
				value := time.Unix(whole, int64((seconds-float64(whole))*1e9))
				if err := os.Chtimes(path, value, value); err != nil {
					t.Fatal(err)
				}
			}
			if recipe.SymlinkTarget != "" {
				write(filepath.Join(root, recipe.SymlinkTarget), "before\n")
				if err = os.Symlink(recipe.SymlinkTarget, path); err != nil {
					t.Fatal(err)
				}
			}
			if recipe.Initial {
				write(path, "before\n")
				initial := float64(100)
				if recipe.InitialMtime != nil {
					initial = *recipe.InitialMtime
				}
				stamp(initial)
			}
			observer, err := NewObserver(context.Background(), ObserverConfig{Roots: []string{root}, Language: GoSource, Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			defer observer.Close()
			switch recipe.Operation {
			case "write", "add":
				write(path, "after\n")
				stamp(recipe.Mtime)
			case "unchanged-write":
				write(path, "before\n")
				stamp(recipe.Mtime)
			case "touch":
				stamp(recipe.Mtime)
			case "remove":
				err = os.Remove(path)
			case "rename":
				err = os.Rename(path, filepath.Join(root, adapt(recipe.Destination)))
			case "mkdir":
				err = os.Mkdir(path, 0700)
			default:
				t.Fatalf("unknown Source polling operation %q", recipe.Operation)
			}
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 1100*time.Millisecond)
			defer cancel()
			batch, err := observer.Next(ctx)
			if recipe.Changed == nil {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("Source had no batch", batch, err)
				}
				return
			}
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

func TestObserverPollingCloseLongDelayAndDebug(t *testing.T) {
	for _, delay := range []string{"0", "18446744073709551615"} {
		profile, err := SelectObserverProfile(map[string]string{"WATCHFILES_FORCE_POLLING": "0", "WATCHFILES_POLL_DELAY_MS": delay}, ObserverHost{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		observer, err := NewObserver(context.Background(), ObserverConfig{Roots: []string{t.TempDir()}, Profile: profile})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := observer.(*PollingEvents); !ok {
			t.Fatal("wrong selected backend")
		}
		done := make(chan error, 1)
		go func() { done <- observer.Close() }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("polling close did not join")
		}
		if _, err = observer.Next(context.Background()); !errors.Is(err, ErrEventsClosed) {
			t.Fatal(err)
		}
	}
	if _, err := NewObserver(context.Background(), ObserverConfig{Roots: []string{t.TempDir()}}); err == nil {
		t.Fatal("zero profile accepted")
	}
	profile, _ := SelectObserverProfile(map[string]string{"WATCHFILES_FORCE_POLLING": "true", "WATCHFILES_DEBUG": "false", "WATCHFILES_POLL_DELAY_MS": "10"}, ObserverHost{}, nil)
	var output bytes.Buffer
	root := t.TempDir()
	observer, err := NewObserver(context.Background(), ObserverConfig{Roots: []string{root}, Profile: profile, DiagnosticWriter: &output})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "hidden.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err = observer.Next(ctx); err != nil {
		t.Fatal(err)
	}
	if err = observer.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "hidden.txt") {
		t.Fatal("debug omitted raw excluded path", output.String())
	}
}

func TestPollingEventsActualSourceMissingPathsAndCycle(t *testing.T) {
	var fixture struct {
		Boundaries struct {
			Cycle struct {
				ErrorType string `json:"error_type"`
				Closed    bool
			} `json:"cycle"`
			Missing []struct {
				Name    string
				Batches [][]string
				Closed  bool
			} `json:"missing_paths"`
		} `json:"polling_boundaries"`
	}
	data, err := os.ReadFile("../testdata/python-watchfiles.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Boundaries.Cycle.ErrorType != "WatchfilesRustInternalError" || !fixture.Boundaries.Cycle.Closed || len(fixture.Boundaries.Missing) != 2 {
		t.Fatal("missing actual boundary evidence")
	}
	profile, err := SelectObserverProfile(map[string]string{"WATCHFILES_FORCE_POLLING": "true", "WATCHFILES_POLL_DELAY_MS": "50"}, ObserverHost{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err = os.Symlink(".", filepath.Join(root, "cycle")); err != nil {
		t.Fatal(err)
	}
	if _, err = NewObserver(context.Background(), ObserverConfig{Roots: []string{root}, Profile: profile}); err == nil {
		t.Fatal("cycle accepted")
	}
	for _, recipe := range fixture.Boundaries.Missing {
		t.Run(recipe.Name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "root.go")
			if err = os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			root, err = filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			if recipe.Name == "dangling" {
				if err = os.Symlink("absent", filepath.Join(root, "link.go")); err != nil {
					t.Fatal(err)
				}
			}
			observer, err := NewObserver(context.Background(), ObserverConfig{Roots: []string{root}, Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			defer observer.Close()
			if recipe.Name == "missing-root" {
				if err = os.Remove(root); err != nil {
					t.Fatal(err)
				}
			}
			for _, wanted := range recipe.Batches {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				batch, err := observer.Next(ctx)
				cancel()
				if err != nil {
					t.Fatal(err)
				}
				got := make([]string, 0, len(batch.Paths))
				for _, path := range batch.Paths {
					relative, err := filepath.Rel(root, path)
					if err != nil {
						t.Fatal(err)
					}
					if relative == "." {
						relative = "<root>"
					}
					got = append(got, relative)
				}
				want := make([]string, 0, len(wanted))
				for _, path := range wanted {
					want = append(want, strings.ReplaceAll(path, ".py", ".go"))
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatal(got, "want", want)
				}
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = NewObserver(ctx, ObserverConfig{Roots: []string{t.TempDir()}, Profile: profile}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
