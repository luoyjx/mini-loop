//go:build darwin || linux

// Package durable provides anchored create-only instruction-file boundaries.
package durable

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"strings"
	"syscall"
)

type FileIdentity struct{ Device, Inode uint64 }

var ErrTooLarge = errors.New("secure read exceeds the configured bound")
var ErrNotRegular = errors.New("secure read requires a regular file")
var ErrInvalidLimit = errors.New("secure read limit is invalid")

// directory is one owned descriptor. Operations remain anchored after a rename.
type directory struct{ fd int }

func parentNoFollow(ctx context.Context, path string) (directory, string, error) {
	if err := ctx.Err(); err != nil {
		return directory{}, "", err
	}
	if path == "" {
		return directory{}, "", syscall.EINVAL
	}
	if !strings.HasPrefix(path, "/") {
		cwd, err := os.Getwd()
		if err != nil {
			return directory{}, "", err
		}
		path = cwd + "/" + path
	}
	path = strings.TrimRight(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[len(parts)-1] == "" || parts[len(parts)-1] == "." || parts[len(parts)-1] == ".." {
		return directory{}, "", syscall.EINVAL
	}
	flags := syscall.O_RDONLY | syscall.O_DIRECTORY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
	fd, err := syscall.Open("/", flags, 0)
	if err != nil {
		return directory{}, "", err
	}
	for _, part := range parts[1 : len(parts)-1] {
		if part == "" || part == "." {
			continue
		}
		if err := ctx.Err(); err != nil {
			syscall.Close(fd)
			return directory{}, "", err
		}
		following, err := openAt(fd, part, flags, 0)
		syscall.Close(fd)
		if err != nil {
			return directory{}, "", err
		}
		fd = following
	}
	return directory{fd}, parts[len(parts)-1], nil
}
func (dir directory) close() { _ = syscall.Close(dir.fd) }

// CreateText commits at the no-replace hard link. Once committed, cancellation,
// scratch cleanup, directory fsync and descriptor close cannot revoke success.
func CreateText(ctx context.Context, path, text string) (FileIdentity, error) {
	dir, name, err := parentNoFollow(ctx, path)
	if err != nil {
		return FileIdentity{}, err
	}
	defer dir.close()
	return dir.create(ctx, name, []byte(text))
}
func (dir directory) create(ctx context.Context, name string, payload []byte) (FileIdentity, error) {
	if err := ctx.Err(); err != nil {
		return FileIdentity{}, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return FileIdentity{}, err
	}
	scratch := "." + name + "." + hex.EncodeToString(random[:]) + ".tmp"
	fd, err := openAt(dir.fd, scratch, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return FileIdentity{}, err
	}
	defer func() { _ = unlinkAt(dir.fd, scratch) }()
	file := os.NewFile(uintptr(fd), scratch)
	defer file.Close()
	if err := file.Chmod(0600); err != nil {
		return FileIdentity{}, err
	}
	if count, err := file.Write(payload); err != nil {
		return FileIdentity{}, err
	} else if count != len(payload) {
		return FileIdentity{}, io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return FileIdentity{}, err
	}
	var metadata syscall.Stat_t
	if err := syscall.Fstat(fd, &metadata); err != nil {
		return FileIdentity{}, err
	}
	identity := FileIdentity{uint64(metadata.Dev), uint64(metadata.Ino)}
	if err := file.Close(); err != nil {
		return FileIdentity{}, err
	}
	if err := ctx.Err(); err != nil {
		return FileIdentity{}, err
	}
	if err := linkAt(dir.fd, scratch, name); err != nil {
		return FileIdentity{}, err
	}
	_ = syscall.Fsync(dir.fd)
	return identity, nil
}

// ReadBytesNoFollow reads a regular file under an anchored parent, with one
// extra byte for overflow detection. NONBLOCK makes hostile FIFOs refuse promptly.
func ReadBytesNoFollow(ctx context.Context, path string, maxBytes int) ([]byte, error) {
	if maxBytes < 0 || maxBytes == math.MaxInt {
		return nil, ErrInvalidLimit
	}
	dir, name, err := parentNoFollow(ctx, path)
	if err != nil {
		return nil, err
	}
	defer dir.close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := openAt(dir.fd, name, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	var metadata syscall.Stat_t
	if err := syscall.Fstat(fd, &metadata); err != nil {
		return nil, err
	}
	if metadata.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return nil, ErrNotRegular
	}
	payload, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > maxBytes {
		return nil, ErrTooLarge
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return payload, nil
}
