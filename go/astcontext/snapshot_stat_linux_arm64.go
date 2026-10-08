package astcontext

import (
	"os"
	"syscall"
)

func snapshotStatAt(parent *os.File, name string, stat *syscall.Stat_t) error {
	return snapshotStatCall(syscall.SYS_FSTATAT, parent, name, stat, 0x100)
}
