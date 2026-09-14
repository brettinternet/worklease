package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
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

	hostedMarkerContents = "worklease-hosted-v1\n"
)

// afterHostedLockOpenHook is test-only instrumentation for a lock pathname
// replacement between descriptor open and advisory lock acquisition.
var afterHostedLockOpenHook func()

// MarkHosted durably marks an existing private home as hosted. Hosted
// initialization calls this before opening the authority database.
func MarkHosted(home string) error {
	dir, _, err := secureHome(home, false)
	if err != nil {
		return err
	}
	defer dir.Close()

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

type hostedLock struct{ file *os.File }

func acquireHostedLock(ctx context.Context, dir *os.File) (*hostedLock, error) {
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
	return &hostedLock{file: file}, nil
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

func (l *hostedLock) close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	if closeErr := l.file.Close(); err == nil {
		err = closeErr
	}
	l.file = nil
	return err
}
