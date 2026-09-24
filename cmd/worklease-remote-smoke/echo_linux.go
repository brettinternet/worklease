//go:build linux

package main

import "golang.org/x/sys/unix"

func terminalEchoEnabled(fd int) (bool, error) {
	state, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return false, err
	}
	return state.Lflag&unix.ECHO != 0, nil
}
