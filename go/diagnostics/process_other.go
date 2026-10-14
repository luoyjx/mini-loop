//go:build !darwin && !linux

package diagnostics

import (
	"os"
	"os/exec"
)

func configureParserProcess(*exec.Cmd) {}
func killParserProcess(process *os.Process) {
	if process != nil {
		_ = process.Kill()
	}
}
