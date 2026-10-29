//go:build !windows

package privfs

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

const nameReserved = ""

func openRoot(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

// OpenDirAt opens the directory name in dir without following a link.
func OpenDirAt(dir *os.File, name string) (*os.File, error) {
	if !validName(name) {
		return nil, unix.EINVAL
	}
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

// MkdirAt creates an owner-private directory name in dir.
func MkdirAt(dir *os.File, name string) error {
	if !validName(name) {
		return unix.EINVAL
	}
	return unix.Mkdirat(int(dir.Fd()), name, 0o700)
}

// OpenAt opens the non-directory name in dir without following a link. flag
// accepts os.O_RDONLY, os.O_WRONLY, os.O_RDWR, os.O_CREATE, and os.O_EXCL.
// A created file gets perm; on Windows a perm without group or other bits
// gets an owner-only DACL instead.
func OpenAt(dir *os.File, name string, flag int, perm os.FileMode) (*os.File, error) {
	if !validName(name) || flag&^(os.O_RDONLY|os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_EXCL) != 0 {
		return nil, unix.EINVAL
	}
	fd, err := unix.Openat(int(dir.Fd()), name, flag|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(perm.Perm()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

// OpenNoFollow opens path like os.OpenFile, refusing a final link component.
func OpenNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, perm)
}

// StatFile returns metadata for an open file.
func StatFile(f *os.File) (Stat, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return Stat{}, err
	}
	return fromStat(&st), nil
}

// StatAt returns metadata for name in dir without following a link.
func StatAt(dir *os.File, name string) (Stat, error) {
	if !validName(name) {
		return Stat{}, unix.EINVAL
	}
	var st unix.Stat_t
	if err := unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return Stat{}, err
	}
	return fromStat(&st), nil
}

// Lstat returns metadata for path without following a final link.
func Lstat(path string) (Stat, error) {
	var st unix.Stat_t
	if err := unix.Lstat(path, &st); err != nil {
		return Stat{}, err
	}
	return fromStat(&st), nil
}

func fromStat(st *unix.Stat_t) Stat {
	kind := st.Mode & unix.S_IFMT
	return Stat{
		ID:          ID{Dev: uint64(st.Dev), Ino: uint64(st.Ino)},
		Dir:         kind == unix.S_IFDIR,
		Regular:     kind == unix.S_IFREG,
		Link:        kind == unix.S_IFLNK,
		Owned:       st.Uid == uint32(os.Geteuid()),
		Private:     st.Mode&0o077 == 0,
		Replaceable: st.Mode&0o022 != 0 && st.Mode&unix.S_ISVTX == 0,
		Nlink:       uint64(st.Nlink),
		Size:        st.Size,
	}
}

// MakePrivate removes group and other access from an open directory.
func MakePrivate(dir *os.File) error { return dir.Chmod(0o700) }

// RenameAt atomically replaces to with from, both in dir.
func RenameAt(dir *os.File, from, to string) error {
	if !validName(from) || !validName(to) {
		return unix.EINVAL
	}
	return unix.Renameat(int(dir.Fd()), from, int(dir.Fd()), to)
}

// LinkAt hard-links from to to in dir, failing when to exists.
func LinkAt(dir *os.File, from, to string) error {
	if !validName(from) || !validName(to) {
		return unix.EINVAL
	}
	return unix.Linkat(int(dir.Fd()), from, int(dir.Fd()), to, 0)
}

// UnlinkAt removes the non-directory name from dir.
func UnlinkAt(dir *os.File, name string) error {
	if !validName(name) {
		return unix.EINVAL
	}
	return unix.Unlinkat(int(dir.Fd()), name, 0)
}

// SyncDir makes completed entry changes in dir durable.
func SyncDir(dir *os.File) error { return dir.Sync() }

// TryLock takes a non-blocking shared or exclusive lock on f, returning
// ErrLocked when another descriptor holds a conflicting lock.
func TryLock(f *os.File, exclusive bool) error {
	how := unix.LOCK_SH
	if exclusive {
		how = unix.LOCK_EX
	}
	err := unix.Flock(int(f.Fd()), how|unix.LOCK_NB)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return ErrLocked
	}
	return err
}

// Unlock releases a lock taken by TryLock.
func Unlock(f *os.File) error { return unix.Flock(int(f.Fd()), unix.LOCK_UN) }

// TryLockDir takes a non-blocking exclusive lock that serializes processes
// working in dir. The caller closes the returned file to release it.
func TryLockDir(dir string) (*os.File, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	if err = TryLock(f, true); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// DupDescriptor returns a close-on-exec duplicate of an inherited descriptor.
func DupDescriptor(fd int, name string) (*os.File, error) {
	dup, err := unix.Dup(fd)
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(dup)
	return os.NewFile(uintptr(dup), name), nil
}
