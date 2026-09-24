//go:build darwin

package main

import "golang.org/x/sys/unix"

func terminalEchoEnabled(fd int) (bool, error) {
	state, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return false, err
	}
	return state.Lflag&unix.ECHO != 0, nil
}
