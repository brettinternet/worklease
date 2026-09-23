//go:build darwin || linux

package queue

import (
	"fmt"
	"os"
	"syscall"
)

func checkoutInstance(info os.FileInfo) (string, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), true
}
