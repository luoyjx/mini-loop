//go:build darwin || (linux && (amd64 || arm64))

package astcontext

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

type snapshotFixtureFile struct {
	Path, Text, Hex string
	Directory, FIFO bool
	Symlink         *string
	Size            *int64
}

type snapshotFixtureManifest struct {
	Path, Kind, Mode, SHA256 string
	Bytes                    int
}

type snapshotFixtureRow struct {
	Recipe struct {
		Name    string
		Files   []snapshotFixtureFile
		Targets []string
		Limits  map[string]int64
		Mutate  *struct{ Path, Text string }
	}
	Paths, Rewrite   []string
	Manifest         []snapshotFixtureManifest
	OutsideWorkspace bool `json:"outside_workspace"`
	Cleaned          bool
	FrozenSHA256     string `json:"frozen_sha256"`
	ErrorClass       string `json:"error_class"`
	Error            string
}

type snapshotFixture struct {
	Limits              map[string]int64
	Suffixes, Basenames []string
	DefaultIgnores      []string `json:"default_ignores"`
	Snapshots           []snapshotFixtureRow
}

func snapshotSourceFixture(t *testing.T) snapshotFixture {
	t.Helper()
	data, err := os.ReadFile("../testdata/python-ast-snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture snapshotFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func snapshotFixtureLimits(t *testing.T, overrides map[string]int64) snapshotLimits {
	t.Helper()
	limits := defaultSnapshotLimits()
	for key, value := range overrides {
		switch key {
		case "_MAX_SNAPSHOT_BYTES":
			limits.bytes = value
		case "_MAX_SNAPSHOT_ENTRIES":
			limits.entries = int(value)
		case "_MAX_SNAPSHOT_FILES":
			limits.files = int(value)
		case "_MAX_SNAPSHOT_DEPTH":
			limits.depth = int(value)
		case "_MAX_IGNORE_BYTES":
			limits.ignoreBytes = value
		case "_MAX_IGNORE_PATTERNS":
			limits.ignorePatterns = int(value)
		case "_MAX_IGNORE_MATCH_OPS":
			limits.ignoreMatchOps = int(value)
		default:
			t.Fatalf("unrecognized Source limit %q", key)
		}
	}
	return limits
}

func snapshotFixtureTree(t *testing.T, files []snapshotFixtureFile) (string, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, outside := filepath.Join(base, "workspace"), filepath.Join(base, "outside")
	for _, path := range []string{root, outside} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	snapshotTestWrite(t, filepath.Join(outside, "secret.py"), "outside secret\n")
	for _, item := range files {
		path := filepath.Join(root, item.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		switch {
		case item.Directory:
			err = os.MkdirAll(path, 0700)
		case item.Symlink != nil:
			err = os.Symlink(strings.ReplaceAll(*item.Symlink, "<outside>", outside), path)
		case item.FIFO:
			err = syscall.Mkfifo(path, 0600)
		case item.Size != nil:
			file, openErr := os.Create(path)
			if openErr != nil {
				t.Fatal(openErr)
			}
			err = errors.Join(file.Truncate(*item.Size), file.Close())
		case item.Hex != "":
			data, decodeErr := hex.DecodeString(item.Hex)
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			err = os.WriteFile(path, data, 0600)
		default:
			err = os.WriteFile(path, []byte(item.Text), 0600)
		}
		if err != nil {
			t.Fatal(item.Path, err)
		}
	}
	return root, outside
}

func snapshotManifest(t *testing.T, root string) []snapshotFixtureManifest {
	t.Helper()
	manifest := []snapshotFixtureManifest{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		row := snapshotFixtureManifest{Path: filepath.ToSlash(relative)}
		if entry.IsDir() {
			row.Kind = "directory"
		} else {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			row.Kind, row.Bytes = "file", len(data)
			row.Mode = fmt.Sprintf("0o%o", info.Mode().Perm())
			row.SHA256 = fmt.Sprintf("%x", sha256.Sum256(data))
		}
		manifest = append(manifest, row)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Source sorted(Path) compares path components, not a rendered slash string.
	slices.SortFunc(manifest, func(left, right snapshotFixtureManifest) int {
		return slices.Compare(strings.Split(left.Path, "/"), strings.Split(right.Path, "/"))
	})
	return manifest
}

func snapshotTestAdapter(t *testing.T, seconds float64) *Adapter {
	t.Helper()
	parameters := DefaultParameters()
	parameters.TimeoutSeconds = seconds
	config, err := NewConfig(parameters)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewAdapter(config)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func TestSnapshotPolicyMatchesSource(t *testing.T) {
	fixture := snapshotSourceFixture(t)
	limits := defaultSnapshotLimits()
	actual := map[string]int64{
		"_MAX_SNAPSHOT_BYTES": limits.bytes, "_MAX_SNAPSHOT_ENTRIES": int64(limits.entries),
		"_MAX_SNAPSHOT_FILES": int64(limits.files), "_MAX_SNAPSHOT_DEPTH": int64(limits.depth),
		"_MAX_IGNORE_BYTES": limits.ignoreBytes, "_MAX_IGNORE_PATTERNS": int64(limits.ignorePatterns),
		"_MAX_IGNORE_MATCH_OPS": int64(limits.ignoreMatchOps), "_MAX_IGNORE_PATTERN_CHARS": maxIgnorePatternChars,
	}
	if !reflect.DeepEqual(actual, fixture.Limits) {
		t.Fatal("Source snapshot capacities changed", actual, fixture.Limits)
	}
	if !reflect.DeepEqual(snapshotSuffixes, fixture.Suffixes) || !reflect.DeepEqual(snapshotBasenames, fixture.Basenames) || !reflect.DeepEqual(snapshotDefaultIgnores, fixture.DefaultIgnores) {
		t.Fatal("Source snapshot language/default ignore inventory changed")
	}
}

func TestSnapshotMatchesActualSourceFilesystemRecipes(t *testing.T) {
	fixture := snapshotSourceFixture(t)
	if len(fixture.Snapshots) != 36 {
		t.Fatal("Source snapshot recipe inventory changed")
	}
	for _, row := range fixture.Snapshots {
		t.Run(row.Recipe.Name, func(t *testing.T) {
			root, outside := snapshotFixtureTree(t, row.Recipe.Files)
			targets := make([]string, len(row.Recipe.Targets))
			for i, target := range row.Recipe.Targets {
				if strings.HasPrefix(target, "<outside>") {
					targets[i] = strings.ReplaceAll(target, "<outside>", outside)
				} else {
					targets[i] = filepath.Join(root, target)
				}
			}
			called, privateRoot := false, ""
			err := snapshotTestAdapter(t, 15).withSnapshotLimits(context.Background(), root, targets, snapshotFixtureLimits(t, row.Recipe.Limits), func(snapshot sourceSnapshot) error {
				called, privateRoot = true, snapshot.root
				paths := make([]string, len(snapshot.paths))
				for i, path := range snapshot.paths {
					relative, err := filepath.Rel(snapshot.root, path)
					if err != nil {
						t.Fatal(err)
					}
					paths[i] = filepath.ToSlash(relative)
				}
				if !reflect.DeepEqual(paths, row.Paths) {
					t.Fatal("selected paths", paths, row.Paths)
				}
				if !reflect.DeepEqual(snapshot.rewrites, []snapshotRewrite{{snapshot: snapshot.root, workspace: root}}) || !reflect.DeepEqual(row.Rewrite, []string{"<snapshot>", "<workspace>"}) {
					t.Fatal("private path rewrites differ")
				}
				if contained(root, snapshot.root) || !row.OutsideWorkspace {
					t.Fatal("snapshot is inside model workspace")
				}
				if manifest := snapshotManifest(t, snapshot.root); !reflect.DeepEqual(manifest, row.Manifest) {
					t.Fatalf("manifest:\n%+v\nwant:\n%+v", manifest, row.Manifest)
				}
				if row.Recipe.Mutate != nil {
					mutation := row.Recipe.Mutate
					snapshotTestWrite(t, filepath.Join(root, mutation.Path), mutation.Text)
					data, err := os.ReadFile(snapshot.paths[0])
					if err != nil {
						t.Fatal(err)
					}
					if hash := fmt.Sprintf("%x", sha256.Sum256(data)); hash != row.FrozenSHA256 {
						t.Fatal("live mutation changed frozen content", hash, row.FrozenSHA256)
					}
				}
				return nil
			})
			if row.Error != "" {
				if called || err == nil || err.Error() != row.Error {
					t.Fatalf("error: %v; want %s %q; called %v", err, row.ErrorClass, row.Error, called)
				}
				switch row.ErrorClass {
				case "FileNotFoundError":
					if !errors.Is(err, syscall.ENOENT) {
						t.Fatal("missing errno cause", err)
					}
				case "NotADirectoryError":
					if !errors.Is(err, syscall.ENOTDIR) {
						t.Fatal("missing errno cause", err)
					}
				case "ValueError":
					var pathError *os.PathError
					if errors.As(err, &pathError) {
						t.Fatal("validation became OS failure", err)
					}
				default:
					t.Fatal("unrecognized Source error class", row.ErrorClass)
				}
				return
			}
			if err != nil || !called {
				t.Fatal(err, "snapshot consumer", called)
			}
			if _, err := os.Stat(privateRoot); !errors.Is(err, os.ErrNotExist) || !row.Cleaned {
				t.Fatal("private snapshot not cleaned", privateRoot, err)
			}
		})
	}
}

func TestSnapshotCallbackFailureAndCancellationCleanUp(t *testing.T) {
	root, _ := snapshotFixtureTree(t, []snapshotFixtureFile{{Path: "source.py", Text: "source\n"}})
	adapter := snapshotTestAdapter(t, 15)
	fault := errors.New("consumer failed")
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		privateRoot := ""
		err := adapter.withSnapshot(ctx, root, []string{root}, func(snapshot sourceSnapshot) error {
			privateRoot = snapshot.root
			if canceled {
				cancel()
				return ctx.Err()
			}
			return fault
		})
		cancel()
		want := fault
		if canceled {
			want = context.Canceled
		}
		if !errors.Is(err, want) || privateRoot == "" {
			t.Fatal(err, privateRoot)
		}
		if _, err := os.Stat(filepath.Dir(privateRoot)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("temporary parent retained", err)
		}
	}
}

func TestSnapshotPanicCleanUpAndConstructorAdmission(t *testing.T) {
	root, _ := snapshotFixtureTree(t, []snapshotFixtureFile{{Path: "source.py", Text: "source\n"}})
	adapter := snapshotTestAdapter(t, 15)
	privateRoot := ""
	fault := errors.New("snapshot consumer panic")
	func() {
		defer func() {
			if observed := recover(); observed != fault {
				t.Fatalf("consumer panic changed: %v", observed)
			}
		}()
		adapter.withSnapshot(context.Background(), root, []string{root}, func(snapshot sourceSnapshot) error {
			privateRoot = snapshot.root
			panic(fault)
		})
	}()
	if privateRoot == "" {
		t.Fatal("panic consumer never entered")
	}
	if _, err := os.Stat(filepath.Dir(privateRoot)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("panic retained temporary directory", err)
	}
	called := false
	consumer := func(sourceSnapshot) error { called = true; return nil }
	for _, invalid := range []*Adapter{nil, {}} {
		if err := invalid.withSnapshot(context.Background(), root, []string{root}, consumer); err == nil || called {
			t.Fatal("unconfigured adapter accepted", err, called)
		}
	}
	if err := adapter.withSnapshot(nil, root, []string{root}, consumer); err == nil || called {
		t.Fatal("nil context accepted", err, called)
	}
	if err := adapter.withSnapshot(context.Background(), root, []string{root}, nil); err == nil {
		t.Fatal("nil consumer accepted")
	}
	for _, target := range []string{"relative.py", root + "/child/../source.py", root + "/nul\x00tail"} {
		if err := adapter.withSnapshot(context.Background(), root, []string{target}, consumer); err == nil || called {
			t.Fatal("noncanonical target accepted", target, err, called)
		}
	}
}

func TestSnapshotTimedTraversalAndCallerCancellation(t *testing.T) {
	root, _ := snapshotFixtureTree(t, []snapshotFixtureFile{{Path: "source.py", Text: "source\n"}})
	called := false
	if err := snapshotTestAdapter(t, 0.000000001).withSnapshot(context.Background(), root, []string{root}, func(sourceSnapshot) error { called = true; return nil }); err == nil || err.Error() != "ast-outline source snapshot exceeded 1e-09s" || called {
		t.Fatal("internal deadline", err, called)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := snapshotTestAdapter(t, 15).withSnapshot(ctx, root, []string{root}, func(sourceSnapshot) error { called = true; return nil }); !errors.Is(err, context.Canceled) || called {
		t.Fatal("caller cancellation", err, called)
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := snapshotTestAdapter(t, 15).withSnapshot(ctx, root, []string{root}, func(sourceSnapshot) error { called = true; return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("caller deadline rewritten", err)
	}
}
