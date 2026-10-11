package durable

// Apple XNU f6217f891ac0bb64f3d375211650a4c1ff8ca1ea,
// bsd/kern/syscalls.master: openat=463, linkat=471, unlinkat=472.
// Go 1.23's Darwin Syscall6 is a kernel syscall entry (both arm64/amd64).
const openAtTrap = 463
const linkAtTrap = 471
const unlinkAtTrap = 472
