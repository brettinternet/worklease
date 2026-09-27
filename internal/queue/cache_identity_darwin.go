package queue

import (
	"fmt"
	"syscall"
)

// A device/inode pair can be reused after deletion. Birthtime distinguishes
// checkout incarnations without changing when Git updates HEAD or objects.
func checkoutInstance(path string) (string, bool) {
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		return "", false
	}
	return fmt.Sprintf("%d:%d:%d:%d", stat.Dev, stat.Ino, stat.Birthtimespec.Sec, stat.Birthtimespec.Nsec), true
}
