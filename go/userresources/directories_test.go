package userresources

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestActualPythonOwnerDirectoryPolicy(t *testing.T) {
	raw, e := os.ReadFile("../testdata/python-owner-directories.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Cases []struct {
			Recipe, Owner, Root string
			OwnerRoot           string `json:"owner_root"`
			Modes               []uint32
			Cached, Error       bool
			SymlinkRefusal      bool   `json:"symlink_refusal"`
			OutsideMode         uint32 `json:"outside_mode"`
		}
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	for _, row := range f.Cases {
		t.Run(row.Recipe, func(t *testing.T) {
			base := t.TempDir()
			base, e = filepath.EvalSymlinks(base)
			if e != nil {
				t.Fatal(e)
			}
			root, outside := filepath.Join(base, "configured"), filepath.Join(base, "outside")
			if e = os.Mkdir(outside, 0755); e != nil {
				t.Fatal(e)
			}
			os.Chmod(outside, 0755)
			switch row.Recipe {
			case "lax":
				os.Mkdir(root, 0755)
			case "root-link":
				os.Symlink(outside, root)
			case "dangling-root":
				os.Symlink(filepath.Join(base, "missing-target"), root)
			case "link-parent":
				nested := filepath.Join(outside, "nested")
				os.Mkdir(nested, 0755)
				os.Symlink(nested, filepath.Join(base, "jump"))
				root = base + "/jump/../configured"
			case "cycle-root":
				os.Symlink(root, root)
			}
			r, e := NewDirectoryResolver(context.Background(), root)
			if e == nil {
				key, _ := OwnerDirectoryKey(row.Owner)
				owner := filepath.Join(r.root, string(key))
				if strings.HasSuffix(row.Recipe, "-link") && row.Recipe != "root-link" && row.Recipe != "dangling-root" {
					target := owner
					if row.Recipe != "owner-link" {
						os.Mkdir(owner, 0700)
						target = filepath.Join(owner, strings.Split(row.Recipe, "-")[0])
					}
					if x := os.Symlink(outside, target); x != nil {
						t.Fatal(x)
					}
				}
				if row.Recipe == "owner-file" {
					if x := os.WriteFile(owner, []byte("keep"), 0600); x != nil {
						t.Fatal(x)
					}
				}
				var binding DirectoryBinding
				binding, e = r.ForOwner(context.Background(), OwnerID(row.Owner))
				if e == nil {
					rel, _ := filepath.Rel(base, r.root)
					orel, _ := filepath.Rel(base, binding.Root())
					if rel != row.Root || orel != row.OwnerRoot {
						t.Fatal(rel, orel)
					}
					for i, path := range []string{r.root, binding.Root(), binding.Skills(), binding.Memory()} {
						info, x := os.Stat(path)
						if x != nil || uint32(info.Mode().Perm()) != row.Modes[i] {
							t.Fatal(path, x)
						}
					}
					again, x := r.ForOwner(context.Background(), OwnerID(row.Owner))
					if x != nil || again != binding || !row.Cached {
						t.Fatal("cache changed")
					}
				}
			}
			if (e != nil) != row.Error || (row.SymlinkRefusal && !errors.Is(e, errLink)) {
				t.Fatal(e, row.Error)
			}
			info, x := os.Stat(outside)
			if x != nil || uint32(info.Mode().Perm()) != row.OutsideMode {
				t.Fatal("outside permissions changed", x)
			}
			if e != nil && strings.Contains(e.Error(), base) {
				t.Fatal("failure leaks host path")
			}
		})
	}
}
func TestDirectoryBindingIsolationConcurrencyAndCancellation(t *testing.T) {
	r, e := NewDirectoryResolver(context.Background(), filepath.Join(t.TempDir(), "root"))
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	bindings := make(chan DirectoryBinding, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, e := r.ForOwner(context.Background(), "alice")
			if e != nil {
				t.Error(e)
			}
			bindings <- b
		}()
	}
	wg.Wait()
	close(bindings)
	var first DirectoryBinding
	for b := range bindings {
		if first.root == "" {
			first = b
		}
		if b != first {
			t.Fatal("snapshot differs")
		}
	}
	bob, e := r.ForOwner(context.Background(), "bob")
	if e != nil || bob.Root() == first.Root() || bob.Owner() == first.Owner() {
		t.Fatal("owner crossed")
	}
	<-r.permit
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, e = r.ForOwner(ctx, "waiting")
	r.permit <- struct{}{}
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	root := filepath.Join(t.TempDir(), "untouched")
	if _, e = NewDirectoryResolver(cancelled, root); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e = os.Stat(root); !os.IsNotExist(e) {
		t.Fatal("cancelled construction mutated root")
	}
}
