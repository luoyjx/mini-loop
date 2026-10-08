//go:build darwin || linux

package astcontext

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

const snapshotFileFlags = syscall.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK | syscall.O_CLOEXEC
const snapshotDirectoryFlags = snapshotFileFlags | syscall.O_DIRECTORY

func openSnapshotDirectory(ctx context.Context, root string) (*snapshotDirectory, error) {
	if err := snapshotContext(ctx); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errors.New("snapshot root must be a canonical absolute directory")
	}
	file, err := os.OpenFile(root, snapshotDirectoryFlags, 0)
	if err != nil {
		return nil, err
	}
	return &snapshotDirectory{file: file}, nil
}

func snapshotOpenAt(parent *os.File, name string, flags int) (*os.File, error) {
	pointer, err := syscall.BytePtrFromString(name)
	if err != nil {
		return nil, err
	}
	fd, _, errno := syscall.Syscall6(snapshotOpenAtTrap, parent.Fd(), uintptr(unsafe.Pointer(pointer)), uintptr(flags), 0, 0, 0)
	runtime.KeepAlive(pointer)
	runtime.KeepAlive(parent)
	if errno != 0 {
		return nil, &os.PathError{Op: "openat", Path: name, Err: errno}
	}
	return os.NewFile(fd, name), nil
}

func (directory *snapshotDirectory) scanFile() (*os.File, error) {
	return snapshotOpenAt(directory.file, ".", snapshotDirectoryFlags)
}

func (directory *snapshotDirectory) child(ctx context.Context, name string) (*snapshotDirectory, error) {
	if err := snapshotContext(ctx); err != nil {
		return nil, err
	}
	if err := snapshotComponent(name); err != nil {
		return nil, err
	}
	file, err := snapshotOpenAt(directory.file, name, snapshotDirectoryFlags)
	if err != nil {
		return nil, err
	}
	return &snapshotDirectory{file: file}, nil
}

func (directory *snapshotDirectory) regular(ctx context.Context, name string) (*os.File, error) {
	if err := snapshotContext(ctx); err != nil {
		return nil, err
	}
	if err := snapshotComponent(name); err != nil {
		return nil, err
	}
	file, err := snapshotOpenAt(directory.file, name, snapshotFileFlags)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		if err == nil {
			err = errors.New("ast-outline snapshot accepts regular files only")
		}
		return nil, err
	}
	return file, nil
}

func (directory *snapshotDirectory) metadata(ctx context.Context, name string) (snapshotMetadata, error) {
	if err := snapshotContext(ctx); err != nil {
		return snapshotMetadata{}, err
	}
	if err := snapshotComponent(name); err != nil {
		return snapshotMetadata{}, err
	}
	var stat syscall.Stat_t
	if err := snapshotStatAt(directory.file, name, &stat); err != nil {
		return snapshotMetadata{}, &os.PathError{Op: "fstatat", Path: name, Err: err}
	}
	kind := snapshotSpecial
	switch uint32(stat.Mode) & syscall.S_IFMT {
	case syscall.S_IFREG:
		kind = snapshotRegular
	case syscall.S_IFDIR:
		kind = snapshotDir
	case syscall.S_IFLNK:
		kind = snapshotSymlink
	}
	return snapshotMetadata{kind: kind, size: stat.Size}, nil
}

// Pointers never leave the syscall boundary. The OS supplies the Stat_t layout;
// no pathname-based fallback may replace a descriptor-relative metadata check.
func snapshotStatCall(trap uintptr, parent *os.File, name string, stat *syscall.Stat_t, flags int) error {
	pointer, err := syscall.BytePtrFromString(name)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(trap, parent.Fd(), uintptr(unsafe.Pointer(pointer)), uintptr(unsafe.Pointer(stat)), uintptr(flags), 0, 0)
	runtime.KeepAlive(pointer)
	runtime.KeepAlive(stat)
	runtime.KeepAlive(parent)
	if errno != 0 {
		return errno
	}
	return nil
}
