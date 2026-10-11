//go:build darwin || linux

package durable

import (
	"runtime"
	"syscall"
	"unsafe"
)

// All pointer conversions stay at the syscall boundary; no raw pointer escapes.
func openAt(parent int, name string, flags int, mode uint32) (int, error) {
	pointer, err := syscall.BytePtrFromString(name)
	if err != nil {
		return -1, err
	}
	fd, _, errno := syscall.Syscall6(openAtTrap, uintptr(parent), uintptr(unsafe.Pointer(pointer)), uintptr(flags), uintptr(mode), 0, 0)
	runtime.KeepAlive(pointer)
	if errno != 0 {
		return -1, errno
	}
	return int(fd), nil
}
func linkAt(parent int, source, target string) error {
	from, err := syscall.BytePtrFromString(source)
	if err != nil {
		return err
	}
	to, err := syscall.BytePtrFromString(target)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(linkAtTrap, uintptr(parent), uintptr(unsafe.Pointer(from)), uintptr(parent), uintptr(unsafe.Pointer(to)), 0, 0)
	runtime.KeepAlive(from)
	runtime.KeepAlive(to)
	if errno != 0 {
		return errno
	}
	return nil
}
func unlinkAt(parent int, name string) error {
	pointer, err := syscall.BytePtrFromString(name)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(unlinkAtTrap, uintptr(parent), uintptr(unsafe.Pointer(pointer)), 0, 0, 0, 0)
	runtime.KeepAlive(pointer)
	if errno != 0 {
		return errno
	}
	return nil
}
