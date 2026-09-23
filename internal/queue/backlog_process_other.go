//go:build !darwin && !linux

package queue

import "os/exec"

func prepareBacklogCommand(*exec.Cmd) {}
