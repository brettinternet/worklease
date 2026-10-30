//go:build !windows

package store

import (
	"context"
	"database/sql"
	"errors"
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

func TestPostCommitValidationFailureIsDefinitelyCommitted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DatabaseFileName)
	driver := openTestDriver(t, path, false)
	defer driver.Close()
	mustWrite(t, driver, `CREATE TABLE post_commit (value TEXT)`)
	target := filepath.Join(dir, "sidecar-target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := driver.Write(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO post_commit(value) VALUES ('durable')`); err != nil {
			return err
		}
		// SQLite already owns its sidecar descriptor. Replacing the directory
		// entry after the transaction work makes the post-commit safety check
		// fail without changing the commit outcome.
		if err := os.Remove(path + "-wal"); err != nil {
			return err
		}
		return os.Symlink(target, path+"-wal")
	})
	var commitErr *CommitError
	if !errors.As(err, &commitErr) || commitErr.Outcome != CommitCommitted {
		t.Fatalf("post-commit error = %v, want typed committed outcome", err)
	}
}
