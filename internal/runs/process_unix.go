//go:build !windows

package runs

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const readyFD = 3

// passReady gives the supervisor the readiness pipe as descriptor 3 and a
// new session so it outlives the caller's terminal.
func passReady(cmd *exec.Cmd, writer *os.File) (string, error) {
	cmd.ExtraFiles = []*os.File{writer}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return strconv.Itoa(readyFD), nil
}

func openReady(value string) *os.File {
	if value != strconv.Itoa(readyFD) {
		return nil
	}
	syscall.CloseOnExec(readyFD)
	return os.NewFile(readyFD, "worklease-run-ready")
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
