//go:build !windows

// Package procgroup runs a command with its descendants in one group so they
// can be stopped together. Unix uses a process group; Windows uses a job
// object.
package procgroup

import (
	"os/exec"
	"syscall"
)

// Group is a started command's process tree.
type Group struct{ pgid int }

// Prepare configures cmd, before Start, to lead a new group.
func Prepare(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// Attach returns the group of a command started after Prepare.
func Attach(cmd *exec.Cmd) (*Group, error) { return &Group{pgid: cmd.Process.Pid}, nil }

// Terminate asks every member to exit. It fails when the group is empty.
// Windows has no request signal and ends every member immediately.
func (g *Group) Terminate() error { return syscall.Kill(-g.pgid, syscall.SIGTERM) }

// Kill ends every member immediately.
func (g *Group) Kill() error { return syscall.Kill(-g.pgid, syscall.SIGKILL) }

// Empty reports whether no member remains.
func (g *Group) Empty() bool { return syscall.Kill(-g.pgid, 0) != nil }

// Close releases the group's resources without signaling it.
func (g *Group) Close() error { return nil }
