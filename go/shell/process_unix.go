//go:build darwin || linux

package shell

import (
	"os"
	"os/exec"
	"syscall"
)

const groupsSupported = true

func configureGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func killGroup(process *os.Process) {
	// The PID is the group ID even after the direct shell has been reaped.
	// Looking it up from an exited shell loses children still holding our pipes.
	if process.Pid > 0 {
		_ = syscall.Kill(-process.Pid, syscall.SIGKILL)
	}
	_ = process.Kill()
}
func processExitCode(state *os.ProcessState) int {
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return -int(status.Signal())
	}
	return state.ExitCode()
}
