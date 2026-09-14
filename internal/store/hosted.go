package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/brettinternet/worklease/internal/reason"
	"golang.org/x/sys/unix"
)

const (
	// HostedMarkerFileName marks a home whose writes must use the remote
	// authority's admission and recovery boundaries.
	HostedMarkerFileName = "hosted"
	// HostedLockFileName is the stable process-lifetime writer lock.
	HostedLockFileName = "hosted.lock"
	// HostedReadyFileName is created after bootstrap state is committed.
	HostedReadyFileName = "hosted.ready"

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
	info, err := dir.Stat()
	if err != nil || info.Mode().Perm()&0o077 != 0 {
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
	fd, err := unix.Open(resolved, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return homeUnsafe(err)
	}
	file := os.NewFile(uintptr(fd), filepath.Base(resolved))
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return homeUnsafe(err)
	}
	if err := validateHostedFile(&stat, "file"); err != nil {
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
	if info, statErr := dir.Stat(); statErr != nil || info.Mode().Perm()&0o077 != 0 {
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
	fd, err := unix.Openat(int(dir.Fd()), HostedMarkerFileName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return homeUnsafe(err)
	}
	marker := os.NewFile(uintptr(fd), HostedMarkerFileName)
	removeIncomplete := true
	defer func() {
		_ = marker.Close()
		if removeIncomplete {
			_ = unix.Unlinkat(int(dir.Fd()), HostedMarkerFileName, 0)
		}
	}()
	if _, err := marker.WriteString(hostedMarkerContents); err != nil {
		return homeUnsafe(err)
	}
	if err := marker.Sync(); err != nil {
		return homeUnsafe(err)
	}
	if err := unix.Fsync(int(dir.Fd())); err != nil {
		return homeUnsafe(err)
	}
	removeIncomplete = false
	return nil
}

func hostedMarker(dir *os.File) (bool, error) {
	if dir == nil {
		return false, nil
	}
	var entry unix.Stat_t
	if err := unix.Fstatat(int(dir.Fd()), HostedMarkerFileName, &entry, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return false, nil
	} else if err != nil {
		return false, homeUnsafe(err)
	}
	if err := validateHostedFile(&entry, "marker"); err != nil {
		return false, err
	}
	fd, err := unix.Openat(int(dir.Fd()), HostedMarkerFileName, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false, homeUnsafe(err)
	}
	marker := os.NewFile(uintptr(fd), HostedMarkerFileName)
	defer marker.Close()
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		return false, homeUnsafe(err)
	}
	if opened.Dev != entry.Dev || opened.Ino != entry.Ino {
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
	dev  uint64
	ino  uint64
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
	info, statErr := dir.Stat()
	if statErr != nil || info.Mode().Perm()&0o077 != 0 {
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
	info, statErr = dir.Stat()
	var stat *syscall.Stat_t
	if statErr == nil {
		stat, _ = info.Sys().(*syscall.Stat_t)
	}
	if stat == nil {
		_ = lock.Close()
		return nil, homeUnsafe(errors.New("hosted home identity unavailable"))
	}
	lock.dir, lock.dev, lock.ino = dir, uint64(stat.Dev), uint64(stat.Ino)
	return lock, nil
}

// WriteHostedReady creates the durable finalization marker while the caller
// retains the hosted writer lock.
func WriteHostedReady(lock *HostedLock) error {
	dir, err := lock.directory()
	if err != nil {
		return err
	}
	fd, err := unix.Openat(int(dir.Fd()), HostedReadyFileName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if errors.Is(err, unix.EEXIST) {
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
	file := os.NewFile(uintptr(fd), HostedReadyFileName)
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = unix.Unlinkat(int(dir.Fd()), HostedReadyFileName, 0)
		}
	}()
	if _, err = file.WriteString(hostedReadyContents); err != nil {
		return homeUnsafe(err)
	}
	if err = file.Sync(); err != nil {
		return homeUnsafe(err)
	}
	if err = unix.Fsync(int(dir.Fd())); err != nil {
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
	if err := unix.Unlinkat(int(dir.Fd()), HostedReadyFileName, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return homeUnsafe(err)
	}
	return unix.Fsync(int(dir.Fd()))
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

func hostedReady(dir *os.File) (bool, error) {
	fd, err := unix.Openat(int(dir.Fd()), HostedReadyFileName, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, homeUnsafe(err)
	}
	file := os.NewFile(uintptr(fd), HostedReadyFileName)
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return false, homeUnsafe(err)
	}
	if err := validateHostedFile(&stat, "ready marker"); err != nil {
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
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return homeUnsafe(errors.New("secret parent is not owner-private"))
	}
	fd, err := unix.Open(resolved, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return homeUnsafe(err)
	}
	file := os.NewFile(uintptr(fd), filepath.Base(resolved))
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(resolved)
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
	if dir, e := os.Open(parent); e == nil {
		if e = dir.Sync(); e != nil {
			_ = dir.Close()
			return homeUnsafe(e)
		}
		if e = dir.Close(); e != nil {
			return homeUnsafe(e)
		}
	} else {
		return homeUnsafe(e)
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
	fd, err := unix.Open(resolved, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", homeUnsafe(err)
	}
	file := os.NewFile(uintptr(fd), filepath.Base(resolved))
	defer file.Close()
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return "", homeUnsafe(err)
	}
	if err := validateHostedFile(&stat, "secret file"); err != nil {
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
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return homeUnsafe(errors.New("export parent is not owner-private"))
	}
	tmp := resolved + ".tmp-" + strconv.Itoa(os.Getpid())
	fd, err := unix.Open(tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return homeUnsafe(err)
	}
	file := os.NewFile(uintptr(fd), filepath.Base(tmp))
	keep := false
	defer func() {
		_ = file.Close()
		if !keep {
			_ = os.Remove(tmp)
		}
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
	if err = os.Rename(tmp, resolved); err != nil {
		return homeUnsafe(err)
	}
	keep = true
	dir, err := os.Open(parent)
	if err != nil {
		return homeUnsafe(err)
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return homeUnsafe(err)
	}
	return nil
}

// RemoveHostedFiles removes the known authority files after a safe retirement.
// The lock itself remains until the caller releases it, preserving fencing.
func RemoveHostedFiles(lock *HostedLock) error {
	dir, err := lock.directory()
	if err != nil {
		return err
	}
	// Keep the marker so a retired directory cannot silently become a local
	// authority after the lock is released.
	for _, name := range []string{DatabaseFileName, DatabaseFileName + "-wal", DatabaseFileName + "-shm", HostedReadyFileName} {
		if err := unix.Unlinkat(int(dir.Fd()), name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return homeUnsafe(err)
		}
	}
	if err := unix.Fsync(int(dir.Fd())); err != nil {
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
	sourceFD, err := unix.Open(source, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return homeUnsafe(err)
	}
	input := os.NewFile(uintptr(sourceFD), filepath.Base(source))
	defer input.Close()
	var sourceStat unix.Stat_t
	if err := unix.Fstat(sourceFD, &sourceStat); err != nil {
		return homeUnsafe(err)
	}
	if err := validateHostedFile(&sourceStat, "backup"); err != nil {
		return err
	}
	tmpName := ".hosted-restore-" + strconv.Itoa(os.Getpid())
	fd, err := unix.Openat(int(dir.Fd()), tmpName, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return homeUnsafe(err)
	}
	tmp := os.NewFile(uintptr(fd), tmpName)
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = unix.Unlinkat(int(dir.Fd()), tmpName, 0)
		}
	}()
	if _, err = io.Copy(tmp, input); err != nil {
		return homeUnsafe(err)
	}
	if err = tmp.Sync(); err != nil {
		return homeUnsafe(err)
	}
	for _, sidecar := range []string{DatabaseFileName + "-wal", DatabaseFileName + "-shm"} {
		if err := unix.Unlinkat(int(dir.Fd()), sidecar, 0); err != nil && !errors.Is(err, unix.ENOENT) {
			return homeUnsafe(err)
		}
	}
	if err = unix.Renameat(int(dir.Fd()), tmpName, int(dir.Fd()), DatabaseFileName); err != nil {
		return homeUnsafe(err)
	}
	keep = true
	if err = unix.Fsync(int(dir.Fd())); err != nil {
		return homeUnsafe(err)
	}
	return nil
}

func acquireHostedLock(ctx context.Context, dir *os.File) (*HostedLock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(dir.Fd()), HostedLockFileName, unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, homeUnsafe(err)
	}
	file := os.NewFile(uintptr(fd), HostedLockFileName)
	closeFile := true
	defer func() {
		if closeFile {
			_ = file.Close()
		}
	}()
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		return nil, homeUnsafe(err)
	}
	if err := validateHostedFile(&opened, "lock"); err != nil {
		return nil, err
	}
	if afterHostedLockOpenHook != nil {
		afterHostedLockOpenHook()
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, reason.New(reason.ReasonHostedLockHeld, "hosted authority is already open by another writer")
		}
		return nil, homeUnsafe(err)
	}
	var entry unix.Stat_t
	if err := unix.Fstatat(int(dir.Fd()), HostedLockFileName, &entry, unix.AT_SYMLINK_NOFOLLOW); err != nil || entry.Dev != opened.Dev || entry.Ino != opened.Ino {
		_ = unix.Flock(fd, unix.LOCK_UN)
		if err != nil {
			return nil, homeUnsafe(err)
		}
		return nil, homeUnsafe(errors.New("hosted lock changed while acquiring"))
	}
	closeFile = false
	return &HostedLock{file: file}, nil
}

func verifyHostedHomePath(dir *os.File, path string) error {
	opened, err := dir.Stat()
	if err != nil {
		return homeUnsafe(err)
	}
	entry, err := os.Lstat(path)
	if err != nil || entry.Mode()&os.ModeSymlink != 0 || !entry.IsDir() {
		if err != nil {
			return homeUnsafe(err)
		}
		return homeUnsafe(errors.New("hosted home changed while opening"))
	}
	openedStat, openedOK := opened.Sys().(*syscall.Stat_t)
	entryStat, entryOK := entry.Sys().(*syscall.Stat_t)
	if !openedOK || !entryOK || openedStat.Dev != entryStat.Dev || openedStat.Ino != entryStat.Ino {
		return homeUnsafe(errors.New("hosted home changed while opening"))
	}
	return nil
}

func validateHostedFile(st *unix.Stat_t, kind string) error {
	if st == nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0o077 != 0 || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 {
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
		if err := unix.Flock(int(l.file.Fd()), unix.LOCK_UN); err != nil {
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
