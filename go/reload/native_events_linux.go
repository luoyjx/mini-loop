//go:build linux

package reload

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// The worker owns its nonblocking descriptor and directory registration map.
// Close signals stop and joins; the worker releases every kernel watch via fd close.
type inotifySubscription struct {
	fd      int
	paths   map[int]string
	watches map[string]int
	source  *NativeEvents
}

const inotifyMask = syscall.IN_ATTRIB | syscall.IN_MODIFY | syscall.IN_CREATE | syscall.IN_DELETE | syscall.IN_MOVED_FROM | syscall.IN_MOVED_TO | syscall.IN_ONLYDIR

func (source *NativeEvents) start(ctx context.Context, roots []string) error {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		close(source.done)
		return err
	}
	subscription := &inotifySubscription{fd: fd, paths: make(map[int]string), watches: make(map[string]int), source: source}
	for _, root := range roots {
		if err := subscription.addTree(ctx, root, true); err != nil {
			_ = syscall.Close(fd)
			close(source.done)
			return err
		}
	}
	go subscription.run()
	return nil
}

func (subscription *inotifySubscription) addTree(ctx context.Context, root string, watchSelf bool) error {
	ancestors := make(map[string]bool)
	var walk func(string) error
	walk = func(path string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		select {
		case <-subscription.source.stop:
			return ErrEventsClosed
		default:
		}
		if insideBuildWorkspace(subscription.source.excludedWorkspace, path) {
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			if path == root && watchSelf && !errors.Is(err, fs.ErrPermission) {
				return err
			}
			return nil // WalkDir/filter_dir skips failed descendant metadata, including link loops.
		}
		if !info.IsDir() {
			return nil
		}
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil
		}
		if insideBuildWorkspace(subscription.source.excludedWorkspace, target) {
			return nil
		}
		if ancestors[target] {
			return nil
		}
		mask := uint32(inotifyMask)
		if path == root && watchSelf {
			mask |= syscall.IN_DELETE_SELF | syscall.IN_MOVE_SELF
		}
		descriptor, err := syscall.InotifyAddWatch(subscription.fd, path, mask|syscall.IN_MASK_ADD)
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.ENOENT) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("register inotify directory %s: %w", path, err)
		}
		// Notify 8 overwrites this inode's lexical projection on each registration.
		// Read directory entries in filesystem order, as WalkDir/ReadDir does.
		subscription.paths[descriptor] = path
		subscription.watches[path] = descriptor
		ancestors[target] = true
		defer delete(ancestors, target)
		directory, err := os.Open(path)
		if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		entries, err := directory.ReadDir(-1)
		closeErr := directory.Close()
		if err == nil {
			err = closeErr
		}
		if errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := walk(filepath.Join(path, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}

func (subscription *inotifySubscription) removeTree(root string) {
	for path, descriptor := range subscription.watches {
		if insideBuildWorkspace(root, path) {
			delete(subscription.watches, path)
			delete(subscription.paths, descriptor)
			_, _ = syscall.InotifyRmWatch(subscription.fd, uint32(descriptor))
		}
	}
}

func (subscription *inotifySubscription) consume(data []byte) error {
	for len(data) > 0 {
		if len(data) < 16 {
			return errors.New("truncated inotify event header")
		}
		descriptor := int(int32(binary.NativeEndian.Uint32(data[:4])))
		mask := binary.NativeEndian.Uint32(data[4:8])
		length := uint64(binary.NativeEndian.Uint32(data[12:16]))
		if length > uint64(len(data)-16) {
			return errors.New("truncated inotify event name")
		}
		name := data[16 : 16+int(length)]
		data = data[16+int(length):]
		if mask&syscall.IN_Q_OVERFLOW != 0 {
			return errors.New("inotify event queue overflow; rescan required")
		}
		root, found := subscription.paths[descriptor]
		if !found {
			continue
		}
		if mask&syscall.IN_IGNORED != 0 {
			delete(subscription.paths, descriptor)
			continue
		}
		if mask&syscall.IN_UNMOUNT != 0 {
			return errors.New("inotify watched filesystem unmounted")
		}
		if end := bytes.IndexByte(name, 0); end >= 0 {
			name = name[:end]
		}
		path := root
		if len(name) > 0 {
			path = filepath.Join(root, string(name))
		}
		subscription.source.recordEvent(eventFingerprint{path: path, flags: mask, kind: linuxNativeObservation})
		if mask&(syscall.IN_DELETE|syscall.IN_MOVED_FROM) != 0 {
			subscription.removeTree(path)
		}
		if mask&(syscall.IN_CREATE|syscall.IN_MOVED_TO) != 0 {
			if err := subscription.addTree(context.Background(), path, false); err != nil {
				return err
			}
		}
		if mask&syscall.IN_DELETE_SELF != 0 {
			subscription.removeTree(root)
		}
		// Notify 8 retains the lexical root projection after MOVE_SELF; child
		// events continue on the same inode even when that old path is absent.

	}
	return nil
}

func (subscription *inotifySubscription) run() {
	defer close(subscription.source.done)
	defer syscall.Close(subscription.fd)
	buffer := make([]byte, 64*1024)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-subscription.source.stop:
			return
		default:
		}
		count, err := syscall.Read(subscription.fd, buffer)
		if err == nil && count > 0 {
			if err = subscription.consume(buffer[:count]); err != nil {
				subscription.source.fail(err)
				return
			}
			continue
		}
		if err != nil && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			subscription.source.fail(err)
			return
		}
		select {
		case <-subscription.source.stop:
			return
		case <-ticker.C:
		}
	}
}
