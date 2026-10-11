//go:build darwin || (linux && (amd64 || arm64))

package astcontext

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func snapshotTestRoot(t *testing.T) (string, *snapshotDirectory) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	directory, err := openSnapshotDirectory(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { directory.close() })
	return root, directory
}

func snapshotTestWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotDirectoryRemainsAnchoredAfterParentReplacement(t *testing.T) {
	root, directory := snapshotTestRoot(t)
	out := t.TempDir()
	snapshotTestWrite(t, filepath.Join(out, "source.py"), "outside secret")
	if err := os.Mkdir(filepath.Join(root, "source"), 0700); err != nil {
		t.Fatal(err)
	}
	snapshotTestWrite(t, filepath.Join(root, "source", "source.py"), "inside")
	child, err := directory.parent(context.Background(), []string{"source"})
	if err != nil {
		t.Fatal(err)
	}
	defer child.close()
	if err := os.Rename(filepath.Join(root, "source"), filepath.Join(root, "moved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(root, "source")); err != nil {
		t.Fatal(err)
	}
	metadata, err := child.metadata(context.Background(), "source.py")
	if err != nil || metadata.kind != snapshotRegular || metadata.size != 6 {
		t.Fatalf("anchored metadata: %+v %v", metadata, err)
	}
	file, err := child.regular(context.Background(), "source.py")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil || string(data) != "inside" {
		t.Fatalf("anchored read: %q %v", data, err)
	}
	names, err := child.names(context.Background(), 1)
	if err != nil || !reflect.DeepEqual(names, []string{"source.py"}) {
		t.Fatalf("anchored enumeration: %v %v", names, err)
	}
	if _, err := directory.parent(context.Background(), []string{"source"}); err == nil {
		t.Fatal("replacement symlink traversed")
	}
}

func TestSnapshotMetadataThenSymlinkSwapFailsClosed(t *testing.T) {
	for _, isDirectory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[isDirectory], func(t *testing.T) {
			root, directory := snapshotTestRoot(t)
			entry := filepath.Join(root, "entry")
			if isDirectory {
				if err := os.Mkdir(entry, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				snapshotTestWrite(t, entry, "inside")
			}
			if _, err := directory.metadata(context.Background(), "entry"); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(entry, entry+"-old"); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			if !isDirectory {
				outside = filepath.Join(outside, "secret")
				snapshotTestWrite(t, outside, "secret")
			}
			if err := os.Symlink(outside, entry); err != nil {
				t.Fatal(err)
			}
			if isDirectory {
				child, err := directory.child(context.Background(), "entry")
				if err == nil {
					child.close()
					t.Fatal("followed swapped directory")
				}
			} else {
				file, err := directory.regular(context.Background(), "entry")
				if err == nil {
					file.Close()
					t.Fatal("followed swapped file")
				}
			}
			metadata, err := directory.metadata(context.Background(), "entry")
			if err != nil || metadata.kind != snapshotSymlink {
				t.Fatalf("nofollow metadata: %+v %v", metadata, err)
			}
		})
	}
}

func TestSnapshotDirectoryEnumerationAndMetadata(t *testing.T) {
	root, directory := snapshotTestRoot(t)
	if err := os.Mkdir(filepath.Join(root, "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	snapshotTestWrite(t, filepath.Join(root, "z.py"), "z")
	snapshotTestWrite(t, filepath.Join(root, "a.py"), "aa")
	if err := os.Symlink("missing", filepath.Join(root, "broken")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	want := []string{"a.py", "broken", "dir", "pipe", "z.py"}
	for scan := 0; scan < 3; scan++ {
		names, err := directory.names(context.Background(), len(want))
		if err != nil || !reflect.DeepEqual(names, want) {
			t.Fatalf("scan %d: %v %v", scan, names, err)
		}
	}
	if _, err := directory.names(context.Background(), len(want)-1); !errors.Is(err, errSnapshotEntryLimit) {
		t.Fatalf("entry cap: %v", err)
	}
	for _, entry := range []struct {
		name string
		kind snapshotEntryKind
		size int64
	}{
		{"a.py", snapshotRegular, 2}, {"dir", snapshotDir, -1}, {"broken", snapshotSymlink, -1}, {"pipe", snapshotSpecial, -1},
	} {
		metadata, err := directory.metadata(context.Background(), entry.name)
		if err != nil || metadata.kind != entry.kind || (entry.size >= 0 && metadata.size != entry.size) {
			t.Fatalf("%s: %+v %v", entry.name, metadata, err)
		}
	}
	if _, err := directory.metadata(context.Background(), "missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	for _, name := range []string{"dir", "broken", "pipe"} {
		file, err := directory.regular(context.Background(), name)
		if err == nil {
			file.Close()
			t.Fatalf("accepted nonregular %s", name)
		}
	}
}

func TestSnapshotDirectoryAdmissionAndCancellation(t *testing.T) {
	root, directory := snapshotTestRoot(t)
	snapshotTestWrite(t, filepath.Join(root, "file"), "x")
	for _, name := range []string{"", ".", "..", "sub/child", "/absolute", "nul\x00tail"} {
		if _, err := directory.metadata(context.Background(), name); err == nil {
			t.Fatalf("metadata accepted %q", name)
		}
		if _, err := directory.child(context.Background(), name); err == nil {
			t.Fatalf("directory accepted %q", name)
		}
		if _, err := directory.regular(context.Background(), name); err == nil {
			t.Fatalf("file accepted %q", name)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := openSnapshotDirectory(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := directory.metadata(ctx, "file"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := directory.child(ctx, "file"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := directory.regular(ctx, "file"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := directory.names(ctx, 100); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := directory.parent(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := openSnapshotDirectory(nil, root); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := openSnapshotDirectory(context.Background(), "relative"); err == nil {
		t.Fatal("relative root accepted")
	}
	link := root + "-link"
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(link)
	if opened, err := openSnapshotDirectory(context.Background(), link); err == nil {
		opened.close()
		t.Fatal("symlink root accepted")
	}
}

func TestSnapshotDescriptorsHaveCloseOnExecAndIndependentLifetime(t *testing.T) {
	root, directory := snapshotTestRoot(t)
	snapshotTestWrite(t, filepath.Join(root, "file"), "content")
	child, err := directory.parent(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer child.close()
	file, err := directory.regular(context.Background(), "file")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, owned := range []*os.File{directory.file, child.file, file} {
		flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, owned.Fd(), syscall.F_GETFD, 0)
		if errno != 0 || flags&syscall.FD_CLOEXEC == 0 {
			t.Fatalf("descriptor inheritance flags: %x %v", flags, errno)
		}
	}
	if err := directory.close(); err != nil {
		t.Fatal(err)
	}
	metadata, err := child.metadata(context.Background(), "file")
	if err != nil || metadata.kind != snapshotRegular {
		t.Fatalf("child invalidated by anchor close: %+v %v", metadata, err)
	}
	data, err := io.ReadAll(file)
	if err != nil || string(data) != "content" {
		t.Fatalf("file invalidated by anchor close: %q %v", data, err)
	}
}
