package testkit

import (
	"os"
	"testing"

	"github.com/brettinternet/worklease/internal/privfs"
)

// Chmod applies a Unix permission mode. On Windows it installs the
// equivalent DACL: the owner's bits, and any group or other bits as an
// Everyone entry, so privacy checks see the same exposure.
func Chmod(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	if err := chmod(path, mode); err != nil {
		t.Fatalf("chmod %s %o: %v", path, mode, err)
	}
}

// AssertMode requires path to have exactly mode on Unix. On Windows, where
// modes are not observable, it requires matching owner privacy.
func AssertMode(t testing.TB, path string, mode os.FileMode) {
	t.Helper()
	if !windows {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("%s mode=%v, want %v", path, info.Mode().Perm(), mode)
		}
		return
	}
	stat, err := privfs.Lstat(path)
	if want := mode&0o077 == 0; err != nil || stat.Private != want || !stat.Owned {
		t.Fatalf("%s private=%t owned=%t err=%v, want private=%t", path, stat.Private, stat.Owned, err, want)
	}
}
