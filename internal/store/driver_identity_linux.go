package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// OpenedDatabaseIdentities verifies the live SQLite descriptors, not merely
// another lookup of the database pathname.
func (d *Driver) OpenedDatabaseIdentities() ([]fileIdentity, error) {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return nil, fmt.Errorf("enumerate open descriptors: %w", err)
	}
	wanted, err := filepath.EvalSymlinks(d.path)
	if err != nil {
		return nil, fmt.Errorf("resolve database descriptor path: %w", err)
	}
	var identities []fileIdentity
	for _, entry := range entries {
		fd, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err != nil || filepath.Clean(strings.TrimSuffix(target, " (deleted)")) != filepath.Clean(wanted) {
			continue
		}
		var stat syscall.Stat_t
		if err := syscall.Fstat(fd, &stat); err != nil {
			continue
		}
		identities = append(identities, fileIdentity{Dev: uint64(stat.Dev), Ino: uint64(stat.Ino)})
	}
	if len(identities) == 0 {
		return nil, errors.New("SQLite database descriptor identity unavailable")
	}
	return identities, nil
}
