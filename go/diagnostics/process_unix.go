//go:build darwin || linux

package diagnostics

import (
	"os"
	"os/exec"
	"syscall"
)

func configureParserProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func killParserProcess(process *os.Process) {
	if process == nil {
		return
	}
	if process.Pid > 0 {
		_ = syscall.Kill(-process.Pid, syscall.SIGKILL)
	}
	_ = process.Kill()
}
