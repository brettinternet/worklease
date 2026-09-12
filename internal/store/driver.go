// Package store provides the SQLite authority storage boundary.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

const (
	// DatabaseFileName is the authority database file name under the home.
	DatabaseFileName = "worklease.db"

	// commitReadbackTimeout is deliberately independent of the caller's
	// context. Once Commit has returned an error, retrying is unsafe until a
	// fresh connection has established the durable outcome.
	commitReadbackTimeout = time.Second

	// CommitCommitted means a fresh read connection observed the transaction's
	// durable result after Commit returned an error.
	CommitCommitted CommitOutcome = "committed"
	// CommitNotCommitted means a fresh read connection established that the
	// transaction's result is absent after Commit returned an error.
	CommitNotCommitted CommitOutcome = "not-committed"
	// CommitUnknown means the result could not be established by read-back.
	CommitUnknown CommitOutcome = "unknown"

	// UnsafePathRaceWarning documents the limit of this package's path open.
	// modernc.org/sqlite accepts a path, rather than an already-open O_NOFOLLOW
	// descriptor. The lstat-to-open interval is therefore still racy; callers
	// that need the stronger guarantee must compare the opened inode (and keep
	// their trusted directory descriptor pinned) after OpenDriver returns.
	UnsafePathRaceWarning = "modernc.org/sqlite opens by path; pre-open lstat checks do not close the symlink replacement race; the deterministic pre-open hook detects a swap, but callers still need opened device/inode validation for the residual interval"
)

// beforeSQLiteOpenHook is test-only instrumentation for the documented
// lstat-to-open race. It is intentionally not exported; production callers
// must treat UnsafePathRaceWarning as a capability limit.
var beforeSQLiteOpenHook func(string)

// CommitOutcome is the result of classifying an error returned by Commit.
type CommitOutcome string

// CommitError preserves an ambiguous commit error and its read-back outcome.
// In particular, an error from Commit is never treated as proof of rollback.
type CommitError struct {
	Outcome     CommitOutcome
	Err         error
	ReadbackErr error
}

func (e *CommitError) Error() string {
	if e.ReadbackErr != nil {
		return fmt.Sprintf("sqlite commit outcome %s: %v (read-back: %v)", e.Outcome, e.Err, e.ReadbackErr)
	}
	return fmt.Sprintf("sqlite commit outcome %s: %v", e.Outcome, e.Err)
}

func (e *CommitError) Unwrap() error { return e.Err }

// Driver owns one database/sql pool. A Driver opened for read-only use cannot
// be used for Write. The pool is deliberately bounded to one connection: it
// is the write serialization boundary required by the authority contract.
type Driver struct {
	db       *sql.DB
	readDB   *sql.DB
	path     string
	readOnly bool
}

// OpenDriver opens path using the modernc.org/sqlite pure-Go driver. A write
// open may create path with mode 0600; a read-only open never creates or chmods
// any path and uses SQLite's mode=ro URI option. The parent directory is
// intentionally not created here; home ownership and directory checks belong
// to the authority store (TASK-85.6).
//
// Existing database, -wal, and -shm paths are checked before opening. Because
// the selected driver opens by path, this check has the residual pre-open race
// documented by UnsafePathRaceWarning.
func OpenDriver(ctx context.Context, path string, readOnly bool) (*Driver, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is required")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := checkDatabasePaths(path, readOnly); err != nil {
		return nil, err
	}

	if !readOnly {
		if err := createDatabaseIfAbsent(path); err != nil {
			return nil, err
		}
		if err := createPrivateSidecarsIfAbsent(path); err != nil {
			return nil, err
		}
	}
	if beforeSQLiteOpenHook != nil {
		beforeSQLiteOpenHook(path)
	}
	// Recheck after the hook. This closes the deterministic test race while
	// retaining the explicit warning about the unavoidable final path-open
	// interval in modernc.org/sqlite.
	if err := checkDatabasePaths(path, readOnly); err != nil {
		return nil, err
	}

	busyTimeout := int64(10000)
	if deadline, ok := ctx.Deadline(); ok {
		busyTimeout = timeoutMilliseconds(time.Until(deadline))
	}
	dsn := sqliteDSNTimeout(path, readOnly, busyTimeout)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	driver := &Driver{db: db, path: path, readOnly: readOnly}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}
	// Restore the contract timeout after a deadline-bounded open.
	if _, err := db.ExecContext(context.Background(), "PRAGMA busy_timeout = 10000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set sqlite busy timeout: %w", err)
	}
	if readOnly {
		driver.readDB = db
	} else {
		// A separate read-only pool is required because _txlock is a
		// connection setting. The write pool is intentionally immediate, while
		// Read must use modernc's verified _txlock=deferred connection.
		readDB, err := sql.Open("sqlite", sqliteDSN(path, true))
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("open sqlite read connection: %w", err)
		}
		readDB.SetMaxOpenConns(1)
		readDB.SetMaxIdleConns(1)
		if err := readDB.PingContext(ctx); err != nil {
			_ = readDB.Close()
			_ = db.Close()
			return nil, fmt.Errorf("ping sqlite read connection: %w", err)
		}
		driver.readDB = readDB
		if err := checkPrivateSidecars(path); err != nil {
			_ = driver.Close()
			return nil, err
		}
	}
	return driver, nil
}

// DB returns the underlying bounded database handle for schema and query code
// owned by the store package. Callers must not change its pool limits.
func (d *Driver) DB() *sql.DB { return d.db }

// Path returns the canonical path supplied to the SQLite opener.
func (d *Driver) Path() string { return d.path }

// Close closes the driver's database pool.
func (d *Driver) Close() error {
	if d.readDB != nil && d.readDB != d.db {
		if err := d.readDB.Close(); err != nil {
			_ = d.db.Close()
			return err
		}
	}
	return d.db.Close()
}

// Write runs fn in a context-aware BEGIN IMMEDIATE transaction. Callback
// failures and cancellation are rolled back. A commit error is wrapped in a
// CommitError with outcome unknown; use WriteWithReadback when the caller can
// provide the durable fact needed to classify an ambiguous commit.
func (d *Driver) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	return d.write(ctx, nil, fn)
}

// WriteWithReadback is Write with a durable outcome probe. probe is called on
// a newly opened read-only connection only when Commit returns an error. It
// must return true when the callback's durable result is present, false when
// it is definitely absent, and an error when it cannot decide.
func (d *Driver) WriteWithReadback(ctx context.Context, probe func(context.Context, *sql.DB) (bool, error), fn func(*sql.Tx) error) error {
	return d.write(ctx, probe, fn)
}

func (d *Driver) write(ctx context.Context, probe func(context.Context, *sql.DB) (bool, error), fn func(*sql.Tx) error) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if d.readOnly {
		return errors.New("write transaction on read-only sqlite driver")
	}
	if fn == nil {
		return errors.New("write callback is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	resetTimeout, err := d.boundBusyTimeout(ctx)
	if err != nil {
		return err
	}
	defer resetTimeout()
	tx, err := d.db.BeginTx(ctx, nil) // _txlock=immediate emits BEGIN IMMEDIATE.
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := ctx.Err(); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return d.classifyCommit(ctx, probe, err)
	}
	if err := checkPrivateSidecars(d.path); err != nil {
		// Commit has succeeded. This typed result is intentionally not
		// retryable, even though the post-commit validation failed.
		return &CommitError{Outcome: CommitCommitted, Err: err}
	}
	return nil
}

// Read runs fn in a deferred read transaction and always rolls it back after
// the callback. Read callbacks cannot accidentally make a durable change.
func (d *Driver) Read(ctx context.Context, fn func(*sql.Tx) error) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if fn == nil {
		return errors.New("read callback is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// The write pool is configured with _txlock=immediate. Use its separate
	// read-only pool, configured with _txlock=deferred, so this remains a
	// genuinely deferred transaction rather than relying on TxOptions (which
	// modernc.org/sqlite does not use to select SQLite's BEGIN mode).
	tx, err := d.readDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(tx)
}

func (d *Driver) boundBusyTimeout(ctx context.Context) (func(), error) {
	milliseconds := int64(10000)
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, ctx.Err()
		}
		milliseconds = remaining.Milliseconds()
		if milliseconds < 1 {
			milliseconds = 1
		}
		if milliseconds > 10000 {
			milliseconds = 10000
		}
	}
	if _, err := d.db.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout = %d", milliseconds)); err != nil {
		return nil, err
	}
	return func() {
		// Reset using a fresh context: the operation may have consumed its
		// deadline, but the connection must retain the D8 setting for the next
		// operation.
		_, _ = d.db.ExecContext(context.Background(), "PRAGMA busy_timeout = 10000")
	}, nil
}

func (d *Driver) classifyCommit(_ context.Context, probe func(context.Context, *sql.DB) (bool, error), commitErr error) error {
	outcome := CommitUnknown
	var readbackErr error
	if probe != nil {
		// A canceled caller context cannot be used to decide whether Commit
		// reached durable storage. Bound the independent probe instead.
		readCtx, cancel := context.WithTimeout(context.Background(), commitReadbackTimeout)
		defer cancel()
		readDB, err := sql.Open("sqlite", sqliteDSN(d.path, true))
		if err != nil {
			readbackErr = err
		} else {
			readDB.SetMaxOpenConns(1)
			readDB.SetMaxIdleConns(1)
			if err := readDB.PingContext(readCtx); err != nil {
				readbackErr = err
			} else {
				present, err := probe(readCtx, readDB)
				switch {
				case err != nil:
					readbackErr = err
				case present:
					outcome = CommitCommitted
				default:
					outcome = CommitNotCommitted
				}
			}
			_ = readDB.Close()
		}
	}
	return &CommitError{Outcome: outcome, Err: commitErr, ReadbackErr: readbackErr}
}

func sqliteDSN(path string, readOnly bool) string {
	return sqliteDSNTimeout(path, readOnly, 10000)
}

func sqliteDSNTimeout(path string, readOnly bool, busyTimeout int64) string {
	// Escaping URI query delimiters keeps absolute filesystem paths usable by
	// SQLite's URI parser.
	escaped := strings.ReplaceAll(strings.ReplaceAll(path, "%", "%25"), "?", "%3F")
	escaped = strings.ReplaceAll(escaped, "#", "%23")
	mode := "rwc"
	txlock := "immediate"
	pragmas := "&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)"
	if readOnly {
		mode, txlock = "ro", "deferred"
		// journal_mode is persistent and cannot be set by a read-only
		// connection; an existing WAL database reports it on read-back.
		// synchronous is connection-local and safe to set read-only.
		pragmas = "&_pragma=synchronous(FULL)"
	}
	return fmt.Sprintf("file:%s?mode=%s&_txlock=%s&_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)%s", escaped, mode, txlock, busyTimeout, pragmas)
}

func timeoutMilliseconds(remaining time.Duration) int64 {
	milliseconds := remaining.Milliseconds()
	if milliseconds < 1 {
		return 1
	}
	if milliseconds > 10000 {
		return 10000
	}
	return milliseconds
}

func checkDatabasePaths(path string, readOnly bool) error {
	if err := checkExistingPath(path, "database"); err != nil {
		if !os.IsNotExist(err) || readOnly {
			return err
		}
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := checkExistingPath(path+suffix, "database sidecar"); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func checkExistingPath(path, kind string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("unsafe %s path %q: must be a regular non-symlink file", kind, path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("unsafe %s path %q: permissions %04o are not private", kind, path, info.Mode().Perm())
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if uint32(stat.Uid) != uint32(os.Geteuid()) {
			return fmt.Errorf("unsafe %s path %q: owner differs from effective user", kind, path)
		}
		if stat.Nlink != 1 {
			return fmt.Errorf("unsafe %s path %q: hard links are not allowed", kind, path)
		}
	}
	return nil
}

func createDatabaseIfAbsent(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		if chmodErr := file.Chmod(0o600); chmodErr != nil {
			_ = file.Close()
			return fmt.Errorf("set database permissions: %w", chmodErr)
		}
		return file.Close()
	}
	if !os.IsExist(err) {
		return fmt.Errorf("create database: %w", err)
	}
	return checkExistingPath(path, "database")
}

func createPrivateSidecarsIfAbsent(path string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		sidecar := path + suffix
		file, err := os.OpenFile(sidecar, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			if err := file.Close(); err != nil {
				return fmt.Errorf("close private database sidecar: %w", err)
			}
			continue
		}
		if !os.IsExist(err) {
			return fmt.Errorf("create private database sidecar: %w", err)
		}
	}
	return checkPrivateSidecars(path)
}

func checkPrivateSidecars(path string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := checkExistingPath(path+suffix, "database sidecar"); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
