package queue

import (
	"fmt"
	"syscall"
)

// A device/inode pair can be reused after deletion. Birthtime distinguishes
// checkout incarnations without changing when Git updates HEAD or objects.
func checkoutInstance(path string) (string, bool) {
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil || stat.Birthtimespec.Sec == 0 && stat.Birthtimespec.Nsec == 0 {
		// Some filesystems do not report birthtime. Inode alone is reusable.
		return "", false
	}
	return fmt.Sprintf("%d:%d:%d:%d", stat.Dev, stat.Ino, stat.Birthtimespec.Sec, stat.Birthtimespec.Nsec), true
}
