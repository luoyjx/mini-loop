//go:build darwin || linux

package reload

import (
	"os"
	"os/exec"
	"syscall"
)

const inheritedListenersSupported = true

func signalChildStop(process *os.Process) { _ = syscall.Kill(-process.Pid, syscall.SIGTERM) }

func configureBuildGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func killBuildGroup(process *os.Process) {
	if process.Pid > 0 {
		_ = syscall.Kill(-process.Pid, syscall.SIGKILL)
	}
	_ = process.Kill()
}
