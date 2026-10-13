package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDirectoryKeepsBoundWorkspace(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	files, err := NewFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := files.EnsureDirectory(context.Background(), "nested/output"); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(filepath.Join(root, "nested", "output")); err != nil || !info.IsDir() {
		t.Fatal(info, err)
	}
	if err := files.EnsureDirectory(context.Background(), "nested/output"); err != nil {
		t.Fatal("existing directory rejected", err)
	}
	if err := os.WriteFile(filepath.Join(root, "blocking"), []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := files.EnsureDirectory(context.Background(), "blocking"); err == nil {
		t.Fatal("existing file admitted as directory")
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	var escape *PathEscapeError
	if err := files.EnsureDirectory(context.Background(), "escape/child"); !errors.As(err, &escape) {
		t.Fatal("outside symlink admitted", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "child")); !os.IsNotExist(err) {
		t.Fatal("outside directory created", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := files.EnsureDirectory(ctx, "cancelled"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled create admitted", err)
	}
	if _, err := os.Stat(filepath.Join(root, "cancelled")); !os.IsNotExist(err) {
		t.Fatal("cancelled directory created", err)
	}
}
