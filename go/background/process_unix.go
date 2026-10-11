//go:build linux || darwin

package background

import (
	"errors"
	"github.com/luoyjx/mini-loop/go/shell"
	"strconv"
	"syscall"
)

func processAlive(pid shell.ProcessID) bool {
	return !errors.Is(syscall.Kill(int(pid), 0), syscall.ESRCH)
}
func pidString(pid shell.ProcessID) string { return strconv.Itoa(int(pid)) }
