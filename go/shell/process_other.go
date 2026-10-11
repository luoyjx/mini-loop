//go:build !darwin && !linux

package shell

import (
	"os"
	"os/exec"
)

const groupsSupported = false

func configureGroup(*exec.Cmd)                   {}
func killGroup(process *os.Process)              { _ = process.Kill() }
func processExitCode(state *os.ProcessState) int { return state.ExitCode() }
