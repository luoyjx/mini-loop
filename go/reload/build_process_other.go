//go:build !darwin && !linux

package reload

import (
	"os"
	"os/exec"
)

func configureBuildGroup(*exec.Cmd)      {}
func killBuildGroup(process *os.Process) { _ = process.Kill() }
