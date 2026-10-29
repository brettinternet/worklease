//go:build !windows

package store

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestDriverCreatesPrivateStateWithPermissiveUmask(t *testing.T) {
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)
	path := filepath.Join(t.TempDir(), DatabaseFileName)
	driver := openTestDriver(t, path, false)
	defer driver.Close()
	mustWrite(t, driver, `CREATE TABLE permissive (value TEXT)`)
	mustWrite(t, driver, `INSERT INTO permissive(value) VALUES ('private')`)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		candidate := path + suffix
		if _, err := os.Lstat(candidate); err == nil {
			assertPrivate(t, candidate)
		}
	}
}
