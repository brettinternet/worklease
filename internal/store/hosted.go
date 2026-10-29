package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/brettinternet/worklease/internal/privfs"
	"github.com/brettinternet/worklease/internal/reason"
)

const (
	// HostedMarkerFileName marks a home whose writes must use the remote
	// authority's admission and recovery boundaries.
	HostedMarkerFileName = "hosted"
	// HostedLockFileName is the stable process-lifetime writer lock.
	HostedLockFileName = "hosted.lock"
	// HostedReadyFileName is created after bootstrap state is committed.
	HostedReadyFileName = "hosted.ready"
	// HostedResetFileName makes destructive reset cleanup restartable.
	HostedResetFileName = "hosted.reset"

	hostedMarkerContents = "worklease-hosted-v1\n"
	hostedReadyContents  = "worklease-hosted-ready-v1\n"
)

// Test-only instrumentation for crash-boundary coverage.
var (
	afterHostedLockOpenHook   func()
	afterHostedSecretSyncHook func() error
)

// ValidateHostedHome verifies owner and private permissions without opening
// SQLite or creating filesystem state.
func ValidateHostedHome(home string) error {
	resolved, err := filepath.Abs(filepath.Clean(home))
	if err != nil {
		return homeUnsafe(err)
	}
	dir, _, err := secureHome(resolved, true)
	if err != nil {
		return err
	}
	if dir == nil {
		return nil
	}
	defer dir.Close()
	if !privateDir(dir) {
		return homeUnsafe(errors.New("hosted home is not owner-private"))
	}
	return nil
}

// ValidateHostedPrivateFile verifies an existing owner-private regular file.
func ValidateHostedPrivateFile(path string) error {
	resolved, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return homeUnsafe(err)
	}
	file, err := privfs.OpenNoFollow(resolved, os.O_RDONLY, 0)
	if err != nil {
		return homeUnsafe(err)
	}
	defer file.Close()
	stat, err := privfs.StatFile(file)
	if err != nil {
		return homeUnsafe(err)
	}
	if err := validateHostedFile(stat, "file"); err != nil {
		return err
	}
	return nil
}

// MarkHosted durably marks an existing private home as hosted. Hosted
// initialization calls this before opening the authority database.
func MarkHosted(home string) error {
	dir, _, err := secureHome(home, false)
	if err != nil {
		return err
	}
	defer dir.Close()
	if !privateDir(dir) {
		return homeUnsafe(errors.New("hosted home is not owner-private"))
	}

	marked, err := hostedMarker(dir)
	if err != nil || marked {
		return err
	}
	names, err := dir.Readdirnames(1)
	if err != nil && !errors.Is(err, io.EOF) {
		return homeUnsafe(err)
	}
	if len(names) != 0 {
		return reason.Invalid("hosted initialization requires an empty authority home")
	}
	marker, err := privfs.OpenAt(dir, HostedMarkerFileName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return homeUnsafe(err)
	}
	removeIncomplete := true
	defer func() {
		_ = marker.Close()
		if removeIncomplete {
			_ = privfs.UnlinkAt(dir, HostedMarkerFileName)
		}
	}()
	if _, err := marker.WriteString(hostedMarkerContents); err != nil {
		return homeUnsafe(err)
	}
	if err := marker.Sync(); err != nil {
		return homeUnsafe(err)
	}
	if err := privfs.SyncDir(dir); err != nil {
		return homeUnsafe(err)
	}
	removeIncomplete = false
	return nil
}

func hostedMarker(dir *os.File) (bool, error) {
	if dir == nil {
		return false, nil
	}
	entry, err := privfs.StatAt(dir, HostedMarkerFileName)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, homeUnsafe(err)
	}
	if err := validateHostedFile(entry, "marker"); err != nil {
		return false, err
	}
	marker, err := privfs.OpenAt(dir, HostedMarkerFileName, os.O_RDONLY, 0)
	if err != nil {
		return false, homeUnsafe(err)
	}
	defer marker.Close()
	opened, err := privfs.StatFile(marker)
	if err != nil {
		return false, homeUnsafe(err)
	}
	if opened.ID != entry.ID {
		return false, homeUnsafe(errors.New("hosted marker changed while opening"))
	}
	contents := make([]byte, len(hostedMarkerContents)+1)
	n, err := marker.Read(contents)
	if err != nil {
		return false, homeUnsafe(err)
	}
	if string(contents[:n]) != hostedMarkerContents {
		return false, homeUnsafe(errors.New("hosted marker is invalid"))
	}
	return true, nil
}

// HostedLock is an offline writer lock. Its directory descriptor remains
// open so the caller can safely replace the database while holding the lock.
type HostedLock struct {
	file *os.File
	dir  *os.File
	id   privfs.ID
}

type hostedLock = HostedLock

// AcquireHostedLock acquires the hosted lock before SQLite is opened.
func AcquireHostedLock(ctx context.Context, home string) (*HostedLock, error) {
	resolved, err := filepath.Abs(filepath.Clean(os.ExpandEnv(home)))
	if err != nil {
		return nil, homeUnsafe(err)
	}
	dir, _, err := secureHome(resolved, false)
	if err != nil {
		return nil, err
	}
	if !privateDir(dir) {
		_ = dir.Close()
		return nil, homeUnsafe(errors.New("hosted home is not owner-private"))
	}
	marked, err := hostedMarker(dir)
	if err != nil || !marked {
		_ = dir.Close()
		if err != nil {
			return nil, err
		}
		return nil, reason.New(reason.ReasonHostedHomeRequiresRemote, "home is not a hosted authority")
	}
	lock, err := acquireHostedLock(ctx, dir)
	if err != nil {
		_ = dir.Close()
		return nil, err
	}
	stat, err := privfs.StatFile(dir)
	if err != nil {
		_ = lock.Close()
		return nil, homeUnsafe(errors.New("hosted home identity unavailable"))
	}
	lock.dir, lock.id = dir, stat.ID
	return lock, nil
}

// WriteHostedReady creates the durable finalization marker while the caller
// retains the hosted writer lock.
func WriteHostedReady(lock *HostedLock) error {
	dir, err := lock.directory()
	if err != nil {
		return err
	}
	file, err := privfs.OpenAt(dir, HostedReadyFileName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		ready, checkErr := hostedReady(dir)
		if checkErr != nil {
			return checkErr
		}
		if ready {
			return nil
		}
		return homeUnsafe(errors.New("hosted ready marker is invalid"))
	}
	if err != nil {
		return homeUnsafe(err)
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = privfs.UnlinkAt(dir, HostedReadyFileName)
		}
	}()
	if _, err = file.WriteString(hostedReadyContents); err != nil {
		return homeUnsafe(err)
	}
	if err = file.Sync(); err != nil {
		return homeUnsafe(err)
	}
	if err = privfs.SyncDir(dir); err != nil {
		return homeUnsafe(err)
	}
	keep = true
	return nil
}

// ClearHostedReady removes the finalization marker before a destructive
// restore, ensuring a crash cannot make an unfinalized database serve.
func ClearHostedReady(lock *HostedLock) error {
	dir, err := lock.directory()
	if err != nil {
		return err
	}
	if err := privfs.UnlinkAt(dir, HostedReadyFileName); err != nil && !errors.Is(err, os.ErrNotExist) {
		return homeUnsafe(err)
	}
	return privfs.SyncDir(dir)
}

// HostedReady verifies the finalization marker without opening SQLite.
func HostedReady(home string) (bool, error) {
	resolved, err := filepath.Abs(filepath.Clean(home))
	if err != nil {
		return false, homeUnsafe(err)
	}
	dir, _, err := secureHome(resolved, true)
	if err != nil {
		return false, err
	}
	if dir == nil {
		return false, nil
	}
	defer dir.Close()
	return hostedReady(dir)
}

type HostedResetIntent struct {
	Version              int    `json:"version"`
	BootstrapInvitePath  string `json:"bootstrapInvitePath"`
	UnresolvedExportPath string `json:"unresolvedExportPath,omitempty"`
}

func WriteHostedResetIntent(lock *HostedLock, intent HostedResetIntent) error {
	if intent.Version != 1 || !filepath.IsAbs(intent.BootstrapInvitePath) || intent.UnresolvedExportPath != "" && !filepath.IsAbs(intent.UnresolvedExportPath) {
		return reason.Invalid("hosted reset intent is invalid")
	}
	dir, err := lock.directory()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	file, err := privfs.OpenAt(dir, HostedResetFileName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return homeUnsafe(err)
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = privfs.UnlinkAt(dir, HostedResetFileName)
		}
	}()
	if _, err = file.Write(encoded); err != nil {
		return homeUnsafe(err)
	}
	if err = file.Sync(); err != nil {
		return homeUnsafe(err)
	}
	if err = privfs.SyncDir(dir); err != nil {
		return homeUnsafe(err)
	}
	keep = true
	return nil
}

func ReadHostedResetIntent(lock *HostedLock) (HostedResetIntent, bool, error) {
	var intent HostedResetIntent
	dir, err := lock.directory()
	if err != nil {
		return intent, false, err
	}
	file, err := privfs.OpenAt(dir, HostedResetFileName, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return intent, false, nil
	}
	if err != nil {
		return intent, false, homeUnsafe(err)
	}
	defer file.Close()
	if stat, err := privfs.StatFile(file); err != nil || validateHostedFile(stat, "reset intent") != nil {
		return intent, false, homeUnsafe(errors.New("hosted reset intent is unsafe"))
	}
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&intent); err != nil || intent.Version != 1 || !filepath.IsAbs(intent.BootstrapInvitePath) || intent.UnresolvedExportPath != "" && !filepath.IsAbs(intent.UnresolvedExportPath) {
		return HostedResetIntent{}, false, homeUnsafe(errors.New("hosted reset intent is invalid"))
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return HostedResetIntent{}, false, homeUnsafe(errors.New("hosted reset intent is invalid"))
	}
	return intent, true, nil
}

func RemoveHostedResetIntent(lock *HostedLock) error {
	dir, err := lock.directory()
	if err != nil {
		return err
	}
	if err := privfs.UnlinkAt(dir, HostedResetFileName); err != nil && !errors.Is(err, os.ErrNotExist) {
		return homeUnsafe(err)
	}
	return privfs.SyncDir(dir)
}

func hostedReady(dir *os.File) (bool, error) {
	file, err := privfs.OpenAt(dir, HostedReadyFileName, os.O_RDONLY, 0)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, homeUnsafe(err)
	}
	defer file.Close()
	stat, err := privfs.StatFile(file)
	if err != nil {
		return false, homeUnsafe(err)
	}
	if err := validateHostedFile(stat, "ready marker"); err != nil {
		return false, err
	}
	contents, err := io.ReadAll(io.LimitReader(file, int64(len(hostedReadyContents)+1)))
	if err != nil {
		return false, homeUnsafe(err)
	}
	if string(contents) != hostedReadyContents {
		return false, homeUnsafe(errors.New("hosted ready marker is invalid"))
	}
	return true, nil
}

// WriteHostedSecret durably creates an owner-private plaintext secret file.
// It refuses replacement so a staged secret is never silently changed.
func WriteHostedSecret(path, secret string) error {
	if strings.TrimSpace(secret) == "" || strings.ContainsAny(secret, "\x00\r\n") {
		return reason.Invalid("secret is invalid")
	}
	resolved, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return homeUnsafe(err)
	}
	parent, err := filepath.Abs(filepath.Dir(resolved))
	if err != nil {
		return homeUnsafe(err)
	}
	dir, err := os.Open(parent)
	if err != nil {
		return homeUnsafe(err)
	}
	defer dir.Close()
	if !privateDir(dir) {
		return homeUnsafe(errors.New("secret parent is not owner-private"))
	}
	name := filepath.Base(resolved)
	file, err := privfs.OpenAt(dir, name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return homeUnsafe(err)
	}
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = privfs.UnlinkAt(dir, name)
		}
	}()
	if _, err = file.WriteString(secret + "\n"); err != nil {
		return homeUnsafe(err)
	}
	if err = file.Sync(); err != nil {
		return homeUnsafe(err)
	}
	if afterHostedSecretSyncHook != nil {
		if err = afterHostedSecretSyncHook(); err != nil {
			return homeUnsafe(err)
		}
	}
	if err = privfs.SyncDir(dir); err != nil {
		return homeUnsafe(err)
	}
	keep = true
	return nil
}

// ReadHostedSecret reads and validates an existing owner-private staged secret.
func ReadHostedSecret(path string) (string, error) {
	resolved, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", homeUnsafe(err)
	}
	file, err := privfs.OpenNoFollow(resolved, os.O_RDONLY, 0)
	if err != nil {
		return "", homeUnsafe(err)
	}
	defer file.Close()
	stat, err := privfs.StatFile(file)
	if err != nil {
		return "", homeUnsafe(err)
	}
	if err := validateHostedFile(stat, "secret file"); err != nil {
		return "", err
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", homeUnsafe(err)
	}
	if len(data) > 4096 {
		return "", reason.Invalid("secret file is too large")
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", reason.Invalid("secret is empty")
	}
	return value, nil
}

// WriteHostedExport durably streams an externally located retirement export.
func WriteHostedExport(path string, write func(io.Writer) error) error {
	if write == nil {
		return errors.New("retirement export writer is required")
	}
	resolved, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return homeUnsafe(err)
	}
	parent := filepath.Dir(resolved)
	dir, _, err := secureHome(parent, true)
	if err != nil {
		return err
	}
	if dir == nil {
		return homeUnsafe(errors.New("export parent does not exist"))
	}
	defer dir.Close()
	if !privateDir(dir) {
		return homeUnsafe(errors.New("export parent is not owner-private"))
	}
	name := filepath.Base(resolved)
	tmpName := "." + name + ".tmp-" + strconv.Itoa(os.Getpid())
	file, err := privfs.OpenAt(dir, tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return homeUnsafe(err)
	}
	defer func() {
		_ = file.Close()
		_ = privfs.UnlinkAt(dir, tmpName)
	}()
	if err = write(file); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return homeUnsafe(err)
	}
	if err = file.Close(); err != nil {
		return homeUnsafe(err)
	}
	if err = privfs.LinkAt(dir, tmpName, name); err != nil {
		if errors.Is(err, os.ErrExist) {
			return reason.Invalid("retirement export already exists")
		}
		return homeUnsafe(err)
	}
	if err = privfs.UnlinkAt(dir, tmpName); err != nil {
		return homeUnsafe(err)
	}
	return privfs.SyncDir(dir)
}

// RemoveHostedFiles removes the known authority files after a safe retirement.
// The lock itself remains until the caller releases it, preserving fencing.
func RemoveHostedFiles(lock *HostedLock) error {
	dir, err := lock.directory()
	if err != nil {
		return err
	}
	// Clear readiness before database files so a crash can never leave a
	// serviceable marker pointing at a missing or partial authority. Keep the
	// hosted marker so the directory cannot silently become a local authority.
	for _, name := range []string{HostedReadyFileName, DatabaseFileName, DatabaseFileName + "-wal", DatabaseFileName + "-shm"} {
		if err := privfs.UnlinkAt(dir, name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return homeUnsafe(err)
		}
	}
	if err := privfs.SyncDir(dir); err != nil {
		return homeUnsafe(err)
	}
	return nil
}

// ReplaceHostedDatabase installs a private backup while the caller retains
// the hosted lock. SQLite is opened only after this operation returns.
func ReplaceHostedDatabase(lock *HostedLock, source string) error {
	dir, err := lock.directory()
	if err != nil {
		return err
	}
	input, err := privfs.OpenNoFollow(source, os.O_RDONLY, 0)
	if err != nil {
		return homeUnsafe(err)
	}
	defer input.Close()
	sourceStat, err := privfs.StatFile(input)
	if err != nil {
		return homeUnsafe(err)
	}
	if err := validateHostedFile(sourceStat, "backup"); err != nil {
		return err
	}
	tmpName := ".hosted-restore-" + strconv.Itoa(os.Getpid())
	tmp, err := privfs.OpenAt(dir, tmpName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return homeUnsafe(err)
	}
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = privfs.UnlinkAt(dir, tmpName)
		}
	}()
	if _, err = io.Copy(tmp, input); err != nil {
		return homeUnsafe(err)
	}
	if err = tmp.Sync(); err != nil {
		return homeUnsafe(err)
	}
	for _, sidecar := range []string{DatabaseFileName + "-wal", DatabaseFileName + "-shm"} {
		if err := privfs.UnlinkAt(dir, sidecar); err != nil && !errors.Is(err, os.ErrNotExist) {
			return homeUnsafe(err)
		}
	}
	if err = privfs.RenameAt(dir, tmpName, DatabaseFileName); err != nil {
		return homeUnsafe(err)
	}
	keep = true
	if err = privfs.SyncDir(dir); err != nil {
		return homeUnsafe(err)
	}
	return nil
}

func acquireHostedLock(ctx context.Context, dir *os.File) (*HostedLock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := privfs.OpenAt(dir, HostedLockFileName, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, homeUnsafe(err)
	}
	closeFile := true
	defer func() {
		if closeFile {
			_ = file.Close()
		}
	}()
	opened, err := privfs.StatFile(file)
	if err != nil {
		return nil, homeUnsafe(err)
	}
	if err := validateHostedFile(opened, "lock"); err != nil {
		return nil, err
	}
	if afterHostedLockOpenHook != nil {
		afterHostedLockOpenHook()
	}
	if err := privfs.TryLock(file, true); err != nil {
		if errors.Is(err, privfs.ErrLocked) {
			return nil, reason.New(reason.ReasonHostedLockHeld, "hosted authority is already open by another writer")
		}
		return nil, homeUnsafe(err)
	}
	if entry, err := privfs.StatAt(dir, HostedLockFileName); err != nil || entry.ID != opened.ID {
		_ = privfs.Unlock(file)
		if err != nil {
			return nil, homeUnsafe(err)
		}
		return nil, homeUnsafe(errors.New("hosted lock changed while acquiring"))
	}
	closeFile = false
	return &HostedLock{file: file}, nil
}

func verifyHostedHomePath(dir *os.File, path string) error {
	opened, err := privfs.StatFile(dir)
	if err != nil {
		return homeUnsafe(err)
	}
	entry, err := privfs.Lstat(path)
	if err != nil {
		return homeUnsafe(err)
	}
	if !entry.Dir || entry.ID != opened.ID {
		return homeUnsafe(errors.New("hosted home changed while opening"))
	}
	return nil
}

func privateDir(dir *os.File) bool {
	st, err := privfs.StatFile(dir)
	return err == nil && st.Dir && st.Private
}

func validateHostedFile(st privfs.Stat, kind string) error {
	if !st.Regular || !st.Private || !st.Owned || st.Nlink != 1 {
		return homeUnsafe(fmt.Errorf("hosted %s is unsafe", kind))
	}
	return nil
}

// Close releases the lock and its pinned directory descriptor.
func (l *HostedLock) Close() error {
	if l == nil {
		return nil
	}
	var first error
	if l.file != nil {
		if err := privfs.Unlock(l.file); err != nil {
			first = err
		}
		if err := l.file.Close(); err != nil && first == nil {
			first = err
		}
		l.file = nil
	}
	if l.dir != nil {
		if err := l.dir.Close(); err != nil && first == nil {
			first = err
		}
		l.dir = nil
	}
	return first
}
func (l *HostedLock) close() error { return l.Close() }

func (l *HostedLock) directory() (*os.File, error) {
	if l == nil || l.file == nil || l.dir == nil {
		return nil, reason.New(reason.ReasonHostedLockHeld, "hosted writer lock is required")
	}
	return l.dir, nil
}
