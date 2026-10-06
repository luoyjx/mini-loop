//go:build darwin || linux

package durable

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
)

func TestActualPythonAnchoredCreateAndRead(t *testing.T) {
	raw, err := os.ReadFile("../testdata/python-durable-create.json")
	if err != nil {
		t.Fatal(err)
	}
	// JSON snake_case fields have explicit names in the boundary decoder.
	var wire struct {
		Cases []struct {
			Recipe                 string
			Read                   bool
			Limit                  int
			Hex                    string
			Failed, Exists, Unsafe bool
			TooLarge               bool `json:"too_large"`
			NotRegular             bool `json:"not_regular"`
			IdentityMatches        bool `json:"identity_matches"`
			Mode                   uint32
			Files                  []struct{ Path, Hex string }
			ScratchCount           int `json:"scratch_count"`
		}
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	for _, row := range wire.Cases {
		t.Run(row.Recipe, func(t *testing.T) {
			base := t.TempDir()
			base, err = filepath.EvalSymlinks(base)
			if err != nil {
				t.Fatal(err)
			}
			parent, outside := filepath.Join(base, "parent"), filepath.Join(base, "outside")
			for _, path := range []string{parent, outside} {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			target := filepath.Join(parent, "SKILL.md")
			write := func(path, text string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			link := func(from, to string) {
				t.Helper()
				if err := os.Symlink(from, to); err != nil {
					t.Fatal(err)
				}
			}
			switch row.Recipe {
			case "exists", "read-exact", "read-over":
				write(target, "existing")
			case "read-empty":
				write(target, "")
			case "directory", "read-directory":
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
			case "leaf-link", "read-leaf-link":
				write(filepath.Join(outside, "SKILL.md"), "outside")
				link(filepath.Join(outside, "SKILL.md"), target)
			case "parent-link", "read-parent-link":
				link(outside, filepath.Join(base, "alias"))
				write(filepath.Join(outside, "SKILL.md"), "outside")
				target = filepath.Join(base, "alias", "SKILL.md")
			case "link-parent":
				link(parent, filepath.Join(base, "alias"))
				target = base + "/alias/../SKILL.md"
			}
			var resultErr error
			if row.Read {
				data, err := ReadBytesNoFollow(context.Background(), target, row.Limit)
				resultErr = err
				if err == nil && hex.EncodeToString(data) != row.Hex {
					t.Fatal("read mismatch")
				}
			} else {
				identity, err := CreateText(context.Background(), target, "canonical\n")
				resultErr = err
				if err == nil {
					var stat syscall.Stat_t
					if err := syscall.Stat(target, &stat); err != nil {
						t.Fatal(err)
					}
					if !row.IdentityMatches || identity != (FileIdentity{uint64(stat.Dev), uint64(stat.Ino)}) || uint32(stat.Mode)&0777 != row.Mode {
						t.Fatal(identity, stat)
					}
				}
			}
			unsafe := errors.Is(resultErr, syscall.ELOOP) || errors.Is(resultErr, syscall.ENOTDIR)
			if (resultErr != nil) != row.Failed || os.IsExist(resultErr) != row.Exists || errors.Is(resultErr, ErrTooLarge) != row.TooLarge || unsafe != row.Unsafe || errors.Is(resultErr, ErrNotRegular) != row.NotRegular {
				t.Fatal(resultErr, row)
			}
			for _, file := range row.Files {
				data, err := os.ReadFile(filepath.Join(base, file.Path))
				if err != nil || hex.EncodeToString(data) != file.Hex {
					t.Fatal(file.Path, err)
				}
			}
			scratch := 0
			if err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if filepath.Ext(path) == ".tmp" {
					scratch++
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if scratch != row.ScratchCount {
				t.Fatal("scratch leaked", scratch)
			}
		})
	}
}

func TestConcurrentCreateCancellationAndAnchoredRename(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	base, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "SKILL.md")
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := CreateText(ctx, target, "committed"); results <- err }()
	}
	wg.Wait()
	close(results)
	won := 0
	for err := range results {
		if err == nil {
			won++
		} else if !os.IsExist(err) {
			t.Fatal(err)
		}
	}
	if won != 1 {
		t.Fatal(won)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := CreateText(cancelled, filepath.Join(base, "cancelled"), "no"); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(base, "cancelled")); !os.IsNotExist(err) {
		t.Fatal("cancelled publication")
	}
	parent := filepath.Join(base, "parent")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	dir, name, err := parentNoFollow(ctx, filepath.Join(parent, "anchored"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.close()
	moved := filepath.Join(base, "moved")
	outside := t.TempDir()
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := dir.create(ctx, name, []byte("anchored")); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(moved, name)); err != nil || string(data) != "anchored" {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, name)); !os.IsNotExist(err) {
		t.Fatal("redirected to replacement parent")
	}
	fifo := filepath.Join(base, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBytesNoFollow(ctx, fifo, 100); !errors.Is(err, ErrNotRegular) {
		t.Fatal(err)
	}
}
