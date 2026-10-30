package privfs

import (
	"errors"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ':' would name an alternate data stream.
const nameReserved = ":"

const (
	fileLinkInformation          = 11
	fileRenameInformationEx      = 65
	fileDispositionInformationEx = 64
	reparseTagNameSurrogate      = 0x20000000
	fileDeleteChild              = 0x40
	readAccess                   = windows.FILE_GENERIC_READ
	statAccess                   = windows.FILE_READ_ATTRIBUTES | windows.READ_CONTROL | windows.SYNCHRONIZE
	shareAll                     = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE
	// Lock one byte far beyond any real file size. Windows byte-range locks
	// are mandatory, so locking real content would block reads through other
	// handles; a lock past EOF only excludes other lockers.
	lockOffsetHigh = 0x7fffffff
)

type fileNameInformation struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

type fileAttributeTagInfo struct {
	FileAttributes uint32
	ReparseTag     uint32
}

type fileStandardInfo struct {
	AllocationSize int64
	EndOfFile      int64
	NumberOfLinks  uint32
	DeletePending  bool
	Directory      bool
}

func openRoot(path string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, readAccess, shareAll, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return checkOpened(h, path, true)
}

// ntOpen opens name relative to dir without following a final reparse point.
func ntOpen(dir *os.File, name string, access, disposition, options uint32, sd *windows.SECURITY_DESCRIPTOR) (windows.Handle, error) {
	if !validName(name) {
		return windows.InvalidHandle, syscall.EINVAL
	}
	return ntOpenRelative(windows.Handle(dir.Fd()), name, access, disposition, options, sd)
}

func ntOpenRelative(root windows.Handle, name string, access, disposition, options uint32, sd *windows.SECURITY_DESCRIPTOR) (windows.Handle, error) {
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return windows.InvalidHandle, err
	}
	attrs := windows.OBJECT_ATTRIBUTES{
		RootDirectory:      root,
		ObjectName:         objectName,
		Attributes:         windows.OBJ_CASE_INSENSITIVE,
		SecurityDescriptor: sd,
	}
	attrs.Length = uint32(unsafe.Sizeof(attrs))
	var h windows.Handle
	err = windows.NtCreateFile(&h, windows.SYNCHRONIZE|access, &attrs, &windows.IO_STATUS_BLOCK{}, nil,
		windows.FILE_ATTRIBUTE_NORMAL, shareAll, disposition,
		windows.FILE_OPEN_REPARSE_POINT|windows.FILE_SYNCHRONOUS_IO_NONALERT|options, 0, 0)
	if err != nil {
		return windows.InvalidHandle, ntError(err)
	}
	return h, nil
}

func ntError(err error) error {
	status, ok := err.(windows.NTStatus)
	if !ok {
		return err
	}
	switch status {
	case windows.STATUS_OBJECT_NAME_COLLISION:
		return syscall.EEXIST
	case windows.STATUS_NOT_A_DIRECTORY:
		return syscall.ENOTDIR
	case windows.STATUS_FILE_IS_A_DIRECTORY:
		return syscall.EISDIR
	case windows.STATUS_REPARSE_POINT_ENCOUNTERED:
		return syscall.ELOOP
	}
	return status.Errno()
}

// checkOpened rejects a link opened in place of a file or directory and
// wraps the handle.
func checkOpened(h windows.Handle, name string, wantDir bool) (*os.File, error) {
	var tag fileAttributeTagInfo
	err := windows.GetFileInformationByHandleEx(h, windows.FileAttributeTagInfo, (*byte)(unsafe.Pointer(&tag)), uint32(unsafe.Sizeof(tag)))
	switch {
	case err != nil:
	case isLink(tag):
		err = syscall.ELOOP
	case wantDir && tag.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0:
		err = syscall.ENOTDIR
	}
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	return os.NewFile(uintptr(h), name), nil
}

func isLink(tag fileAttributeTagInfo) bool {
	return tag.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 && tag.ReparseTag&reparseTagNameSurrogate != 0
}

// OpenDirAt opens the directory name in dir without following a link.
func OpenDirAt(dir *os.File, name string) (*os.File, error) {
	h, err := ntOpen(dir, name, readAccess, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE, nil)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: err}
	}
	return checkOpened(h, name, true)
}

// MkdirAt creates an owner-private directory name in dir.
func MkdirAt(dir *os.File, name string) error {
	sd, err := ownerOnly(true)
	if err != nil {
		return err
	}
	h, err := ntOpen(dir, name, readAccess, windows.FILE_CREATE, windows.FILE_DIRECTORY_FILE, sd)
	if err != nil {
		return &os.PathError{Op: "mkdirat", Path: name, Err: err}
	}
	return windows.CloseHandle(h)
}

// OpenAt opens the non-directory name in dir without following a link. flag
// accepts os.O_RDONLY, os.O_WRONLY, os.O_RDWR, os.O_CREATE, and os.O_EXCL.
// A created file gets perm; on Windows a perm without group or other bits
// gets an owner-only DACL instead.
func OpenAt(dir *os.File, name string, flag int, perm os.FileMode) (*os.File, error) {
	if flag&^(os.O_RDONLY|os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_EXCL) != 0 {
		return nil, syscall.EINVAL
	}
	access := uint32(statAccess)
	switch flag & (os.O_RDONLY | os.O_WRONLY | os.O_RDWR) {
	case os.O_RDONLY:
		access |= windows.FILE_GENERIC_READ
	case os.O_WRONLY:
		access |= windows.FILE_GENERIC_WRITE
	default:
		access |= windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE
	}
	disposition := uint32(windows.FILE_OPEN)
	var sd *windows.SECURITY_DESCRIPTOR
	if flag&os.O_CREATE != 0 {
		disposition = windows.FILE_OPEN_IF
		if flag&os.O_EXCL != 0 {
			disposition = windows.FILE_CREATE
		}
		if perm&0o077 == 0 {
			var err error
			if sd, err = ownerOnly(false); err != nil {
				return nil, err
			}
		}
	}
	h, err := ntOpen(dir, name, access, disposition, windows.FILE_NON_DIRECTORY_FILE, sd)
	if err != nil {
		return nil, &os.PathError{Op: "openat", Path: name, Err: err}
	}
	return checkOpened(h, name, false)
}

// OpenNoFollow opens path like os.OpenFile, refusing a final link component.
func OpenNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	f, err := os.OpenFile(path, flag|windows.FILE_FLAG_OPEN_REPARSE_POINT, perm)
	if err != nil {
		return nil, err
	}
	var tag fileAttributeTagInfo
	err = windows.GetFileInformationByHandleEx(windows.Handle(f.Fd()), windows.FileAttributeTagInfo, (*byte)(unsafe.Pointer(&tag)), uint32(unsafe.Sizeof(tag)))
	if err == nil && isLink(tag) {
		err = syscall.ELOOP
	}
	if err != nil {
		_ = f.Close()
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return f, nil
}

// StatFile returns metadata for an open file.
func StatFile(f *os.File) (Stat, error) { return statHandle(windows.Handle(f.Fd())) }

// StatAt returns metadata for name in dir without following a link.
func StatAt(dir *os.File, name string) (Stat, error) {
	h, err := ntOpen(dir, name, statAccess, windows.FILE_OPEN, 0, nil)
	if err != nil {
		return Stat{}, &os.PathError{Op: "fstatat", Path: name, Err: err}
	}
	defer windows.CloseHandle(h)
	return statHandle(h)
}

// Lstat returns metadata for path without following a final link.
func Lstat(path string) (Stat, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return Stat{}, err
	}
	h, err := windows.CreateFile(name, statAccess, shareAll, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return Stat{}, &os.PathError{Op: "lstat", Path: path, Err: err}
	}
	defer windows.CloseHandle(h)
	return statHandle(h)
}

func statHandle(h windows.Handle) (Stat, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return Stat{}, err
	}
	var tag fileAttributeTagInfo
	if err := windows.GetFileInformationByHandleEx(h, windows.FileAttributeTagInfo, (*byte)(unsafe.Pointer(&tag)), uint32(unsafe.Sizeof(tag))); err != nil {
		return Stat{}, err
	}
	var standard fileStandardInfo
	if err := windows.GetFileInformationByHandleEx(h, windows.FileStandardInfo, (*byte)(unsafe.Pointer(&standard)), uint32(unsafe.Sizeof(standard))); err != nil {
		return Stat{}, err
	}
	st := Stat{
		ID:    ID{Dev: uint64(info.VolumeSerialNumber), Ino: uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)},
		Link:  isLink(tag),
		Nlink: uint64(info.NumberOfLinks),
		Size:  int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow),
	}
	if standard.DeletePending {
		st.Nlink = 0
	}
	if !st.Link {
		st.Dir = info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
		st.Regular = !st.Dir
	}
	sd, err := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return Stat{}, err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return Stat{}, err
	}
	ids, err := identity()
	if err != nil {
		return Stat{}, err
	}
	st.Owned = owner.Equals(ids.user) || owner.Equals(ids.defaultOwner)
	dacl, _, err := sd.DACL()
	if err != nil {
		return Stat{}, err
	}
	st.Private, st.Replaceable, st.Writable = inspectDACL(dacl, ids)
	return st, nil
}

// inspectDACL reports whether only trusted principals hold allow entries,
// whether an untrusted principal can delete, rename, or re-ACL entries, and
// whether one can also write data or attributes.
// Unrecognized allow entries count as untrusted.
func inspectDACL(dacl *windows.ACL, ids *identities) (private, replaceable, writable bool) {
	if dacl == nil {
		return false, true, true
	}
	const replaceRights = fileDeleteChild | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER | windows.GENERIC_ALL
	const writeRights = replaceRights | windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA | windows.FILE_WRITE_EA | windows.FILE_WRITE_ATTRIBUTES | windows.GENERIC_WRITE
	private = true
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return false, true, true
		}
		switch ace.Header.AceType {
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
			if ids.trusted(sid) {
				continue
			}
			private = false
			if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE == 0 {
				replaceable = replaceable || uint32(ace.Mask)&replaceRights != 0
				writable = writable || uint32(ace.Mask)&writeRights != 0
			}
		case 4, 5, 9, 11: // compound, object, callback, and callback-object allow entries
			private = false
			replaceable, writable = true, true
		}
	}
	return private, replaceable, writable
}

type identities struct {
	user, defaultOwner *windows.SID
	trustedSIDs        []*windows.SID
}

func (ids *identities) trusted(sid *windows.SID) bool {
	if sid.Equals(ids.user) {
		return true
	}
	for _, t := range ids.trustedSIDs {
		if sid.Equals(t) {
			return true
		}
	}
	return false
}

var identity = sync.OnceValues(func() (*identities, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	ids := &identities{}
	if ids.user, err = user.User.Sid.Copy(); err != nil {
		return nil, err
	}
	// Members of Administrators may create objects owned by that group.
	var n uint32
	_ = windows.GetTokenInformation(token, windows.TokenOwner, nil, 0, &n)
	buf := make([]byte, n)
	if err = windows.GetTokenInformation(token, windows.TokenOwner, &buf[0], n, &n); err != nil {
		return nil, err
	}
	if ids.defaultOwner, err = (*struct{ Owner *windows.SID })(unsafe.Pointer(&buf[0])).Owner.Copy(); err != nil {
		return nil, err
	}
	for _, s := range []string{
		"S-1-5-18",     // SYSTEM
		"S-1-5-32-544", // Administrators
		"S-1-3-0",      // CREATOR OWNER
		"S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464", // TrustedInstaller
	} {
		sid, err := windows.StringToSid(s)
		if err != nil {
			return nil, err
		}
		ids.trustedSIDs = append(ids.trustedSIDs, sid)
	}
	return ids, nil
})

// ownerOnly returns a protected security descriptor that grants full
// access only to the current user.
func ownerOnly(dir bool) (*windows.SECURITY_DESCRIPTOR, error) {
	ids, err := identity()
	if err != nil {
		return nil, err
	}
	inherit := ""
	if dir {
		inherit = "OICI"
	}
	return windows.SecurityDescriptorFromString("D:P(A;" + inherit + ";FA;;;" + ids.user.String() + ")")
}

// MakePrivate replaces an open directory's DACL with an owner-only one.
func MakePrivate(dir *os.File) error {
	sd, err := ownerOnly(true)
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	// An empty name relative to a handle reopens the same file.
	h, err := ntOpenRelative(windows.Handle(dir.Fd()), "", windows.READ_CONTROL|windows.WRITE_DAC, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE, nil)
	if err != nil {
		return &os.PathError{Op: "chmod", Path: dir.Name(), Err: err}
	}
	defer windows.CloseHandle(h)
	// SetSecurityInfo reads the current descriptor to propagate inheritance,
	// so the handle needs READ_CONTROL as well as WRITE_DAC.
	if err := windows.SetSecurityInfo(h, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return &os.PathError{Op: "chmod", Path: dir.Name(), Err: err}
	}
	return nil
}

func setName(h windows.Handle, class uint32, flags uint32, dir *os.File, name string) error {
	utf16, err := windows.UTF16FromString(name)
	if err != nil {
		return err
	}
	nameBytes := (len(utf16) - 1) * 2
	buf := make([]byte, int(unsafe.Offsetof(fileNameInformation{}.FileName))+nameBytes+2)
	info := (*fileNameInformation)(unsafe.Pointer(&buf[0]))
	info.Flags = flags
	info.RootDirectory = windows.Handle(dir.Fd())
	info.FileNameLength = uint32(nameBytes)
	copy(unsafe.Slice(&info.FileName[0], len(utf16)), utf16)
	return windows.NtSetInformationFile(h, &windows.IO_STATUS_BLOCK{}, &buf[0], uint32(len(buf)), class)
}

func unsupported(err error) bool {
	return err == windows.STATUS_INVALID_INFO_CLASS || err == windows.STATUS_INVALID_PARAMETER || err == windows.STATUS_NOT_SUPPORTED
}

// RenameAt atomically replaces to with from, both in dir.
func RenameAt(dir *os.File, from, to string) error {
	if !validName(to) {
		return syscall.EINVAL
	}
	h, err := ntOpen(dir, from, windows.DELETE, windows.FILE_OPEN, 0, nil)
	if err != nil {
		return &os.LinkError{Op: "renameat", Old: from, New: to, Err: err}
	}
	defer windows.CloseHandle(h)
	// POSIX semantics replace a target that another handle holds open.
	err = setName(h, fileRenameInformationEx, windows.FILE_RENAME_REPLACE_IF_EXISTS|windows.FILE_RENAME_POSIX_SEMANTICS, dir, to)
	if unsupported(err) {
		err = setName(h, windows.FileRenameInformation, 1, dir, to)
	}
	if err != nil {
		return &os.LinkError{Op: "renameat", Old: from, New: to, Err: ntError(err)}
	}
	return nil
}

// LinkAt hard-links from to to in dir, failing when to exists.
func LinkAt(dir *os.File, from, to string) error {
	if !validName(to) {
		return syscall.EINVAL
	}
	h, err := ntOpen(dir, from, windows.FILE_WRITE_ATTRIBUTES, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, nil)
	if err != nil {
		return &os.LinkError{Op: "linkat", Old: from, New: to, Err: err}
	}
	defer windows.CloseHandle(h)
	if err = setName(h, fileLinkInformation, 0, dir, to); err != nil {
		return &os.LinkError{Op: "linkat", Old: from, New: to, Err: ntError(err)}
	}
	return nil
}

// UnlinkAt removes the non-directory name from dir.
func UnlinkAt(dir *os.File, name string) error {
	h, err := ntOpen(dir, name, windows.DELETE|windows.FILE_READ_ATTRIBUTES, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, nil)
	if err != nil {
		return &os.PathError{Op: "unlinkat", Path: name, Err: err}
	}
	defer windows.CloseHandle(h)
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS)
	err = windows.NtSetInformationFile(h, &windows.IO_STATUS_BLOCK{}, (*byte)(unsafe.Pointer(&flags)), 4, fileDispositionInformationEx)
	if unsupported(err) {
		deleteFile := uint32(1)
		err = windows.NtSetInformationFile(h, &windows.IO_STATUS_BLOCK{}, (*byte)(unsafe.Pointer(&deleteFile)), 4, windows.FileDispositionInformation)
	}
	if err != nil {
		return &os.PathError{Op: "unlinkat", Path: name, Err: ntError(err)}
	}
	return nil
}

// SyncDir makes completed entry changes in dir durable. NTFS journals
// directory changes, and Windows cannot flush a directory handle.
func SyncDir(*os.File) error { return nil }

// TryLock takes a non-blocking shared or exclusive lock on f, returning
// ErrLocked when another descriptor holds a conflicting lock.
func TryLock(f *os.File, exclusive bool) error {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	err := windows.LockFileEx(windows.Handle(f.Fd()), flags, 0, 1, 0, &windows.Overlapped{OffsetHigh: lockOffsetHigh})
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return ErrLocked
	}
	return err
}

// Unlock releases a lock taken by TryLock.
func Unlock(f *os.File) error {
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &windows.Overlapped{OffsetHigh: lockOffsetHigh})
}

// TryLockDir takes a non-blocking exclusive lock that serializes processes
// working in dir. The caller closes the returned file to release it.
func TryLockDir(dir string) (*os.File, error) {
	root, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := OpenAt(root, ".worklease.lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err = TryLock(f, true); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// DupDescriptor returns a non-inheritable duplicate of an inherited handle.
// 0, 1, and 2 name the standard handles.
func DupDescriptor(fd int, name string) (*os.File, error) {
	h := windows.Handle(fd)
	if std := [...]uint32{windows.STD_INPUT_HANDLE, windows.STD_OUTPUT_HANDLE, windows.STD_ERROR_HANDLE}; fd >= 0 && fd < len(std) {
		var err error
		if h, err = windows.GetStdHandle(std[fd]); err != nil {
			return nil, err
		}
	}
	process := windows.CurrentProcess()
	var dup windows.Handle
	if err := windows.DuplicateHandle(process, h, process, &dup, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(dup), name), nil
}
