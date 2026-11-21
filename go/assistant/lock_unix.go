//go:build darwin || linux

package assistant

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// lockDataDir takes an exclusive advisory lock on DataDir/daemon.lock, so a
// second daemon on the same directory refuses to start instead of claiming the
// same triggers and sending the same proposals. The kernel drops the lock when
// the process exits, so a crash never leaves a stale lock behind.
func lockDataDir(dir string) (func() error, error) { return lockDir(dir, false) }

// shareDataDir takes the same lock shared. The nodes of a fleet cluster hold
// it together; a daemon or a single-node fleet, which need it exclusively,
// are kept out while any node runs, and keep the nodes out while they run.
func shareDataDir(dir string) (func() error, error) { return lockDir(dir, true) }

func lockDir(dir string, shared bool) (func() error, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, lockFile)
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	how := syscall.LOCK_EX
	if shared {
		how = syscall.LOCK_SH
	}
	if err := syscall.Flock(int(file.Fd()), how|syscall.LOCK_NB); err != nil {
		holder, _ := os.ReadFile(path)
		err = errors.Join(err, file.Close())
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, err
		}
		if pid := strings.TrimSpace(string(holder)); pid != "" {
			return nil, fmt.Errorf("%w: %s (pid %s)", ErrDataDirBusy, dir, pid)
		}
		return nil, fmt.Errorf("%w: %s", ErrDataDirBusy, dir)
	}
	if shared {
		return file.Close, nil
	}
	if err := file.Truncate(0); err == nil {
		_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
	}
	return file.Close, nil
}
