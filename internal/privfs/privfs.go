// Package privfs provides the descriptor-relative, no-follow filesystem
// operations behind Worklease's owner-private files. Unix uses openat-family
// calls and mode bits; Windows uses handle-relative NT opens, reparse-point
// checks, and the file's owner SID and DACL.
package privfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrLocked reports that a non-blocking lock is held by another descriptor.
var ErrLocked = errors.New("privfs: file is locked")

// ID identifies one file on one volume.
type ID struct{ Dev, Ino uint64 }

// Stat is the metadata owner-private checks depend on.
type Stat struct {
	ID      ID
	Dir     bool
	Regular bool // a regular file, never a link or reparse point
	Link    bool // a symlink or name-surrogate reparse point
	Owned   bool // owned by the effective user
	// Private reports that no other user can access the file. Unix requires
	// mode&077 == 0. Windows requires a DACL whose allow entries name only the
	// owner, SYSTEM, Administrators, or TrustedInstaller.
	Private bool
	// Replaceable reports that another user can replace entries in this
	// directory: group/other write without the sticky bit on Unix, or an
	// effective delete-child, delete, or ACL-write grant on Windows.
	Replaceable bool
	// Writable reports that another user can modify the file: group/other
	// write on Unix, or an effective write, delete, or ACL-write grant on
	// Windows.
	Writable bool
	Nlink    uint64
	Size     int64
}

// OpenRoot opens the filesystem root of absolute and returns it with its
// path and the remaining components, which callers walk with OpenDirAt.
func OpenRoot(absolute string) (root *os.File, rootPath string, parts []string, err error) {
	if !filepath.IsAbs(absolute) {
		return nil, "", nil, errors.New("privfs: path is not absolute")
	}
	absolute = filepath.Clean(absolute)
	rootPath = filepath.VolumeName(absolute) + string(filepath.Separator)
	for _, part := range strings.Split(strings.TrimPrefix(absolute, rootPath), string(filepath.Separator)) {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	root, err = openRoot(rootPath)
	if err != nil {
		return nil, "", nil, err
	}
	return root, rootPath, parts, nil
}

func validName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\`+nameReserved)
}
