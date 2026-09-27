package queue

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// A device/inode pair can be reused after deletion. Birthtime distinguishes
// checkout incarnations without changing when Git updates HEAD or objects.
func checkoutInstance(path string) (string, bool) {
	var stat unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, path, 0, unix.STATX_INO|unix.STATX_BTIME, &stat); err != nil || stat.Mask&(unix.STATX_INO|unix.STATX_BTIME) != unix.STATX_INO|unix.STATX_BTIME {
		return "", false
	}
	return fmt.Sprintf("%d:%d:%d:%d:%d", stat.Dev_major, stat.Dev_minor, stat.Ino, stat.Btime.Sec, stat.Btime.Nsec), true
}
