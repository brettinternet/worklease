//go:build !windows

package store

import (
	"os"
	"syscall"
)

// trustedSystemSymlink reports whether path is a root-owned link to a
// root-owned directory that others cannot write.
func trustedSystemSymlink(path string) bool {
	link, err := os.Lstat(path)
	if err != nil {
		return false
	}
	stat, ok := link.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return false
	}
	target, err := os.Stat(path)
	if err != nil || !target.IsDir() {
		return false
	}
	targetStat, ok := target.Sys().(*syscall.Stat_t)
	return ok && targetStat.Uid == 0 && target.Mode().Perm()&0o002 == 0
}
