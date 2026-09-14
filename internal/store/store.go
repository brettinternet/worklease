package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
)

const SchemaVersion int64 = 2

// beforeHomeOpenHook is test-only instrumentation for an adversarial path
// replacement between validation and descriptor open.
var beforeHomeOpenHook func(string)

// Options controls authority opening.
type Options struct{ ReadOnly bool }

// Store is the local SQLite authority. A read-only Store may represent an
// empty authority when its home or database does not exist.
type Store struct {
	driver    *Driver
	home      *os.File
	homePath  string
	authority string
	restoreID string
	readOnly  bool
}

// Open opens an authority under home. Read-only opens never create or chmod
// filesystem state; a missing home or database is an empty authority.
// ValidateHome checks the complete existing ancestry and home metadata without
// opening a database or creating or changing filesystem state.
func ValidateHome(home string) error {
	file, _, err := secureHome(home, true)
	if file != nil {
		_ = file.Close()
	}
	return err
}

func Open(ctx context.Context, home string, opts Options) (*Store, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(home) == "" {
		return nil, reason.New(reason.ReasonHomeUnsafe, "authority home is required")
	}
	resolved, err := filepath.Abs(filepath.Clean(os.ExpandEnv(home)))
	if err != nil {
		return nil, reason.New(reason.ReasonHomeUnsafe, "authority home is unsafe")
	}
	dir, exists, err := secureHome(resolved, opts.ReadOnly)
	if err != nil {
		return nil, err
	}
	st := &Store{home: dir, homePath: resolved, readOnly: opts.ReadOnly}
	if !opts.ReadOnly {
		handles, _, err := secureHome(filepath.Join(resolved, "handles"), false)
		if err != nil {
			_ = st.Close()
			return nil, err
		}
		if err := handles.Close(); err != nil {
			_ = st.Close()
			return nil, homeUnsafe(err)
		}
	}
	if opts.ReadOnly && !exists {
		return st, nil
	}
	database := filepath.Join(resolved, DatabaseFileName)
	if opts.ReadOnly {
		if _, err := os.Lstat(database); errors.Is(err, os.ErrNotExist) {
			_ = st.Close()
			return &Store{homePath: resolved, readOnly: true}, nil
		} else if err != nil {
			_ = st.Close()
			return nil, homeUnsafe(err)
		}
	}
	before, beforeExists, err := identityAt(database)
	if err != nil {
		_ = st.Close()
		return nil, homeUnsafe(err)
	}
	driver, err := openDriverRetry(ctx, database, opts.ReadOnly)
	if err != nil {
		_ = st.Close()
		return nil, homeUnsafe(err)
	}
	st.driver = driver
	after, afterExists, err := identityAt(database)
	if err != nil || !afterExists || (beforeExists && (before.Dev != after.Dev || before.Ino != after.Ino)) {
		_ = st.Close()
		if err != nil {
			return nil, homeUnsafe(err)
		}
		return nil, reason.New(reason.ReasonHomeUnsafe, "authority database changed while opening")
	}
	opened, err := driver.OpenedDatabaseIdentities()
	if err != nil {
		_ = st.Close()
		return nil, homeUnsafe(err)
	}
	for _, identity := range opened {
		if identity != after {
			_ = st.Close()
			return nil, reason.New(reason.ReasonHomeUnsafe, "SQLite opened a replaced authority database")
		}
	}
	version, err := userVersion(driver.DB())
	if err != nil {
		_ = st.Close()
		return nil, schemaCorrupt(err)
	}
	if version == 0 || version == 1 {
		if opts.ReadOnly {
			_ = st.Close()
			if version == 0 {
				return nil, reason.New(reason.ReasonSchemaCorrupt, "empty database has no schema")
			}
			return nil, unsupportedSchema(version)
		}
		authority, err := newAuthorityID()
		if err != nil {
			_ = st.Close()
			return nil, reason.New(reason.ReasonStorageFailure, "generate authority identity")
		}
		restoreID, err := newAuthorityID()
		if err != nil {
			_ = st.Close()
			return nil, reason.New(reason.ReasonStorageFailure, "generate restore identity")
		}
		if err := driver.Write(ctx, func(tx *sql.Tx) error {
			// Re-read under BEGIN IMMEDIATE: another opener may have
			// completed bootstrap or migration after our initial probe.
			var current int64
			if err := tx.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
				return err
			}
			switch current {
			case SchemaVersion:
				return nil
			case 1:
				return migrateSchemaV1(tx, restoreID)
			case 0:
				var objects int
				if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE type IN ('table','index') AND name NOT LIKE 'sqlite_%'").Scan(&objects); err != nil {
					return err
				}
				if objects != 0 {
					return reason.New(reason.ReasonSchemaCorrupt, "database has objects but no schema version")
				}
				return createSchema(tx, authority, restoreID, time.Now().UnixMicro())
			default:
				return unsupportedSchema(current)
			}
		}); err != nil {
			_ = st.Close()
			return nil, err
		}
	} else if version != SchemaVersion {
		_ = st.Close()
		return nil, unsupportedSchema(version)
	}
	if err := verifySchema(driver.DB()); err != nil {
		_ = st.Close()
		return nil, err
	}
	authority, err := readMeta(driver.DB(), "authority_id")
	if err != nil || !validAuthorityID(authority) {
		_ = st.Close()
		return nil, reason.New(reason.ReasonSchemaCorrupt, "authority identity is missing or invalid")
	}
	restoreID, err := readMeta(driver.DB(), "restore_id")
	if err != nil || !validAuthorityID(restoreID) {
		_ = st.Close()
		return nil, reason.New(reason.ReasonSchemaCorrupt, "restore identity is missing or invalid")
	}
	st.authority = authority
	st.restoreID = restoreID
	return st, nil
}

// Close releases the database and the pinned home directory descriptor.
func (s *Store) Close() error {
	var first error
	if s.driver != nil {
		if err := s.driver.Close(); err != nil {
			first = err
		}
	}
	if s.home != nil {
		if err := s.home.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// db returns the pool only to store package implementation and tests. Other
// packages use Read and typed store operations so writable SQL cannot bypass
// event validation and watermark updates.
func (s *Store) db() *sql.DB {
	if s.driver == nil {
		return nil
	}
	return s.driver.DB()
}
func (s *Store) Path() string        { return filepath.Join(s.homePath, DatabaseFileName) }
func (s *Store) Home() string        { return s.homePath }
func (s *Store) AuthorityID() string { return s.authority }
func (s *Store) RestoreID() string   { return s.restoreID }
func (s *Store) Empty() bool         { return s.driver == nil }

// LastObservedAt returns the authority wall-clock watermark without changing
// state. A missing read-only authority has no watermark.
func (s *Store) LastObservedAt(ctx context.Context) (time.Time, error) {
	if s == nil || s.driver == nil {
		return time.Time{}, nil
	}
	var micros int64
	if err := s.Read(ctx, func(tx *Tx) error {
		return tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='last_observed_at'`).Scan(&micros)
	}); err != nil {
		return time.Time{}, err
	}
	return time.UnixMicro(micros).UTC(), nil
}

// ReadbackProbe establishes whether a transaction-specific durable marker is
// present after Commit returns an error. The supplied database is a fresh,
// bounded read-only connection.
type ReadbackProbe func(context.Context, *sql.DB) (bool, error)

// Write executes one typed BEGIN IMMEDIATE transaction. Callers whose durable
// effect can be probed after an uncertain commit should use WriteWithReadback.
func (s *Store) Write(ctx context.Context, fn func(*Tx) error) error {
	return s.write(ctx, nil, fn, time.Now().UnixMicro())
}

// WriteAt is Write with an authority wall-clock observation supplied by the
// domain service. Tests and callers that own the authority clock use this to
// keep persisted clock-regression detection independent of process time.
func (s *Store) WriteAt(ctx context.Context, now time.Time, fn func(*Tx) error) error {
	return s.write(ctx, nil, fn, now.UnixMicro())
}

// WriteWithReadback executes a write and classifies a commit error as
// committed, not-committed, or unknown using probe on an independent reader.
func (s *Store) WriteWithReadback(ctx context.Context, probe ReadbackProbe, fn func(*Tx) error) error {
	if probe == nil {
		return errors.New("read-back probe is required")
	}
	return s.write(ctx, probe, fn, time.Now().UnixMicro())
}

func (s *Store) write(ctx context.Context, probe ReadbackProbe, fn func(*Tx) error, observedAt int64) error {
	if s.driver == nil {
		return reason.New(reason.ReasonStorageFailure, "authority is not available")
	}
	if fn == nil {
		return errors.New("write callback is required")
	}
	apply := func(tx *sql.Tx) error {
		if err := fn(&Tx{tx: tx, write: true}); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE meta SET value = CASE WHEN CAST(value AS INTEGER) > ? THEN value ELSE ? END WHERE key='last_observed_at'`, observedAt, fmt.Sprintf("%d", observedAt))
		return err
	}
	if probe != nil {
		return s.driver.WriteWithReadback(ctx, probe, apply)
	}
	return s.driver.Write(ctx, apply)
}

// Read executes one typed deferred transaction. The callback is not invoked
// for an empty read-only authority because there is no SQL state to inspect.
func (s *Store) Read(ctx context.Context, fn func(*Tx) error) error {
	if ctx == nil {
		return errors.New("nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if fn == nil {
		return errors.New("read callback is required")
	}
	if s.driver == nil {
		return nil
	}
	return s.driver.Read(ctx, func(tx *sql.Tx) error { return fn(&Tx{tx: tx}) })
}

// Tx is the only transaction boundary exposed by Store.
type Tx struct {
	tx    *sql.Tx
	write bool
}

func (t *Tx) execContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, query, args...)
}

// ExecContext is the typed write-side SQL escape hatch used by domain
// packages. Transactions remain the only way to issue mutations.
func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if t == nil || t.tx == nil || !t.write {
		return nil, errors.New("writes require a write transaction")
	}
	return t.tx.ExecContext(ctx, query, args...)
}
func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, query, args...)
}
func (t *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, query, args...)
}
func openDriverRetry(ctx context.Context, path string, readOnly bool) (*Driver, error) {
	attemptCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok {
		attemptCtx, cancel = context.WithTimeout(ctx, 10*time.Second)
	}
	defer cancel()
	var last error
	for {
		driver, err := OpenDriver(attemptCtx, path, readOnly)
		if err == nil {
			return driver, nil
		}
		last = err
		if !strings.Contains(strings.ToLower(err.Error()), "locked") {
			return nil, err
		}
		if err := attemptCtx.Err(); err != nil {
			return nil, last
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-attemptCtx.Done():
			timer.Stop()
			return nil, last
		case <-timer.C:
		}
	}
}

func userVersion(db *sql.DB) (int64, error) {
	var v int64
	err := db.QueryRow("PRAGMA user_version").Scan(&v)
	return v, err
}
func unsupportedSchema(found int64) error {
	return reason.New(reason.ReasonSchemaUnsupported, fmt.Sprintf("unsupported schema version %d", found)).With("supportedVersion", SchemaVersion).With("foundVersion", found)
}
func readMeta(db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&value)
	return value, err
}
func newAuthorityID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
func validAuthorityID(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

// fileIdentity is deliberately obtained from lstat, never stat, so a path
// swap cannot be silently followed.
type fileIdentity struct{ Dev, Ino uint64 }

func identityAt(path string) (fileIdentity, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileIdentity{}, false, nil
	}
	if err != nil {
		return fileIdentity{}, false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fileIdentity{}, true, errors.New("database path is not a regular file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileIdentity{}, true, errors.New("database identity unavailable")
	}
	return fileIdentity{Dev: uint64(stat.Dev), Ino: uint64(stat.Ino)}, true, nil
}

func homeUnsafe(err error) error {
	return reason.New(reason.ReasonHomeUnsafe, "authority home is unsafe").With("cause", err.Error())
}
func schemaCorrupt(err error) error {
	return reason.New(reason.ReasonSchemaCorrupt, "authority schema is corrupt").With("cause", err.Error())
}

func secureHome(path string, readOnly bool) (*os.File, bool, error) {
	cleaned := filepath.Clean(path)
	if cleaned == string(filepath.Separator) || cleaned == filepath.VolumeName(cleaned)+string(filepath.Separator) {
		return nil, false, homeUnsafe(errors.New("filesystem root cannot be an authority home"))
	}
	parts := strings.Split(cleaned, string(filepath.Separator))
	current := string(filepath.Separator)
	if filepath.VolumeName(path) != "" {
		current = filepath.VolumeName(path) + string(filepath.Separator)
	}
	for i, part := range parts {
		if part == "" || (i == 0 && part == current) {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if readOnly {
				return nil, false, nil
			}
			if err := os.Mkdir(current, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return nil, false, homeUnsafe(err)
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return nil, false, homeUnsafe(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// macOS exposes /var as a root-owned compatibility symlink to
			// /private/var. Permit only that class of trusted system ancestor;
			// user-owned or writable symlink ancestors remain unsafe.
			if i >= len(parts)-1 || !trustedSystemSymlink(current, info) {
				return nil, false, homeUnsafe(errors.New("home and ancestors must be directories without unsafe symlinks"))
			}
		} else if !info.IsDir() {
			return nil, false, homeUnsafe(errors.New("home and ancestors must be directories"))
		}
		if i < len(parts)-1 && info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return nil, false, homeUnsafe(errors.New("unsafe writable ancestor"))
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, false, homeUnsafe(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, false, homeUnsafe(errors.New("home is not a directory"))
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint32(stat.Uid) != uint32(os.Geteuid()) {
		return nil, false, homeUnsafe(errors.New("home owner differs from effective user"))
	}
	if readOnly && info.Mode().Perm()&0o077 != 0 {
		return nil, false, homeUnsafe(errors.New("home permissions are not private"))
	}
	if beforeHomeOpenHook != nil {
		beforeHomeOpenHook(path)
	}
	flags := os.O_RDONLY | syscall.O_CLOEXEC
	if nofollow := getattrNoFollow(); nofollow != 0 {
		flags |= nofollow
	}
	if directory := getattrDirectory(); directory != 0 {
		flags |= directory
	}
	file, err := os.OpenFile(path, flags, 0)
	if err != nil {
		return nil, false, homeUnsafe(err)
	}
	opened, err := file.Stat()
	if err != nil || !opened.IsDir() {
		_ = file.Close()
		return nil, false, homeUnsafe(errors.New("opened home is not a directory"))
	}
	openedStat, openedOK := opened.Sys().(*syscall.Stat_t)
	pathStat, pathOK := info.Sys().(*syscall.Stat_t)
	if !openedOK || !pathOK || openedStat.Dev != pathStat.Dev || openedStat.Ino != pathStat.Ino {
		_ = file.Close()
		return nil, false, homeUnsafe(errors.New("home changed while opening"))
	}
	if !readOnly {
		if err := file.Chmod(0o700); err != nil {
			_ = file.Close()
			return nil, false, homeUnsafe(err)
		}
	}
	return file, true, nil
}

func trustedSystemSymlink(path string, link os.FileInfo) bool {
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

func getattrNoFollow() int  { return syscall.O_NOFOLLOW }
func getattrDirectory() int { return syscall.O_DIRECTORY }
