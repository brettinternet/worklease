//go:build !darwin && !linux

package queue

import "os/exec"

func prepareExternalProcess(*exec.Cmd) {}

func terminateExternalProcess(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
