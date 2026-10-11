//go:build linux && !amd64 && !arm64

package astcontext

import (
	"os"
	"syscall"
)

func snapshotStatAt(_ *os.File, _ string, _ *syscall.Stat_t) error {
	return errSnapshotPlatform
}
