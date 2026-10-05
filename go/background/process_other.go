//go:build !linux && !darwin

package background

import (
	"github.com/luoyjx/mini-loop/go/shell"
	"strconv"
)

func processAlive(shell.ProcessID) bool    { return false }
func pidString(pid shell.ProcessID) string { return strconv.Itoa(int(pid)) }
