package durable

import "syscall"

const openAtTrap = syscall.SYS_OPENAT
const linkAtTrap = syscall.SYS_LINKAT
const unlinkAtTrap = syscall.SYS_UNLINKAT
