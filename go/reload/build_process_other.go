//go:build !darwin && !linux

package reload

import (
	"os"
	"os/exec"
)

const inheritedListenersSupported = false

func signalChildStop(process *os.Process) { _ = process.Kill() }

func configureBuildGroup(*exec.Cmd)      {}
func killBuildGroup(process *os.Process) { _ = process.Kill() }
