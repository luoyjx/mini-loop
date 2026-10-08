//go:build !darwin && !linux

package astcontext

import (
	"os"
	"os/exec"
)

func configureGroup(*exec.Cmd)                   {}
func killGroup(process *os.Process)              { _ = process.Kill() }
func processExitCode(state *os.ProcessState) int { return state.ExitCode() }
