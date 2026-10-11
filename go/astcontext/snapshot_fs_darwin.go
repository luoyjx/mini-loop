package astcontext

import (
	"os"
	"syscall"
)

// Apple XNU f6217f891ac0bb64f3d375211650a4c1ff8ca1ea:
// bsd/kern/syscalls.master openat=463, fstatat64=470;
// bsd/sys/fcntl.h AT_SYMLINK_NOFOLLOW=0x20.
// Go's Darwin Stat_t uses the 64-bit inode layout on arm64 and amd64.
const snapshotOpenAtTrap = 463

func snapshotStatAt(parent *os.File, name string, stat *syscall.Stat_t) error {
	return snapshotStatCall(470, parent, name, stat, 0x20)
}
