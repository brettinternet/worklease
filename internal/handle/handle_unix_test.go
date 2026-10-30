//go:build !windows

// Windows refuses to rename a directory while handles are open beneath it,
// so these parent-replacement races cannot occur there.

package handle

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPinnedLockRejectsParentReplacement(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "handles")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "h.json")
	if err := Write(path, testHandle()); err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireLock(context.Background(), path+".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	old := filepath.Join(root, "handles-old")
	if err := os.Rename(parent, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Read(path); err == nil {
		t.Fatal("read replacement parent through old lock")
	}
}

func TestPinnedLockRejectsParentReplacementBeforePersistence(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "handles")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "h.json")
	lock, err := AcquireLock(context.Background(), path+".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := os.Rename(parent, filepath.Join(root, "handles-old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := lock.Write(path, testHandle()); err == nil {
		t.Fatal("persisted through replacement parent")
	}
}

func TestAcquireLockRejectsParentReplacementWhileWaiting(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "handles")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "h.json")
	first, err := AcquireLock(context.Background(), path+".lock")
	if err != nil {
		t.Fatal(err)
	}
	opened := make(chan struct{})
	resume := make(chan struct{})
	afterLockOpenForTest = func() {
		close(opened)
		<-resume
	}
	defer func() { afterLockOpenForTest = func() {} }()
	result := make(chan error, 1)
	go func() {
		second, acquireErr := AcquireLock(context.Background(), path+".lock")
		if second != nil {
			_ = second.Close()
		}
		result <- acquireErr
	}()
	<-opened
	if err := os.Rename(parent, filepath.Join(root, "handles-old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	close(resume)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err == nil {
		t.Fatal("acquired lock after parent replacement")
	}
}
