//go:build darwin || linux

package astcontext

import (
	"os"
	"os/exec"
	"syscall"
)

func configureGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func killGroup(process *os.Process) {
	// Retain the launched group ID after the direct child exits; its descendants
	// can still hold stdout/stderr open and must share the original deadline.
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
