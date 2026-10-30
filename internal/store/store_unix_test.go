//go:build !windows

// Windows refuses to rename a file or directory while SQLite holds it open.

package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestStoreRejectsFinalSQLiteOpenIntervalSwap(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	st := openStore(t, home, false)
	st.Close()
	path := testkit.DatabasePath(home)
	for _, sidecar := range testkit.DatabaseSidecarPaths(home) {
		_ = os.Remove(sidecar)
	}
	original := path + ".original"
	replacement := path + ".replacement"
	beforeSQLiteOpenHook = func(openPath string) {
		if openPath != path {
			return
		}
		if err := os.Rename(path, original); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	afterSQLiteOpenHook = func(openPath string) {
		if openPath != path {
			return
		}
		if err := os.Rename(path, replacement); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(original, path); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		beforeSQLiteOpenHook = nil
		afterSQLiteOpenHook = nil
	}()
	if _, err := Open(context.Background(), home, Options{}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHomeUnsafe {
		t.Fatalf("final SQLite open swap error = %v", err)
	}
}
