package store

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
)

func TestHostedMarkerRefusesLocalWritesBeforeDatabaseOpen(t *testing.T) {
	home := filepath.Join(t.TempDir(), "hosted")
	if err := MarkHosted(home); err != nil {
		t.Fatal(err)
	}
	markerInfo, err := os.Stat(filepath.Join(home, HostedMarkerFileName))
	if err != nil || markerInfo.Mode().Perm() != 0o600 {
		t.Fatalf("marker info=%v err=%v", markerInfo, err)
	}
	if _, err := Open(context.Background(), home, Options{}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHostedHomeRequiresRemote {
		t.Fatalf("local hosted write error=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, DatabaseFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused write opened database: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, "handles")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused write created handles: %v", err)
	}
	readOnly, err := Open(context.Background(), home, Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	if !readOnly.Hosted() || !readOnly.Empty() {
		t.Fatalf("hosted read-only state: hosted=%v empty=%v", readOnly.Hosted(), readOnly.Empty())
	}
}

func TestHostedWriterLockContentionAndStableHandoff(t *testing.T) {
	home := filepath.Join(t.TempDir(), "hosted")
	if err := MarkHosted(home); err != nil {
		t.Fatal(err)
	}
	first, err := Open(context.Background(), home, Options{HostedWriter: true})
	if err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(home, HostedLockFileName)
	before, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), home, Options{HostedWriter: true}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHostedLockHeld {
		t.Fatalf("second hosted writer error=%v", err)
	}
	if _, err := Open(context.Background(), home, Options{}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHostedHomeRequiresRemote {
		t.Fatalf("local writer during lock error=%v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := Open(context.Background(), home, Options{HostedWriter: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeStat := before.Sys().(*syscall.Stat_t)
	afterStat := after.Sys().(*syscall.Stat_t)
	if beforeStat.Dev != afterStat.Dev || beforeStat.Ino != afterStat.Ino {
		t.Fatalf("hosted lock identity changed: %d/%d -> %d/%d", beforeStat.Dev, beforeStat.Ino, afterStat.Dev, afterStat.Ino)
	}
	if _, err := Open(context.Background(), home, Options{}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHostedHomeRequiresRemote {
		t.Fatalf("local writer after handoff error=%v", err)
	}
}

func TestHostedLockRejectsPathReplacementAfterOpen(t *testing.T) {
	home := filepath.Join(t.TempDir(), "hosted")
	if err := MarkHosted(home); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(home, HostedLockFileName)
	afterHostedLockOpenHook = func() {
		afterHostedLockOpenHook = nil
		if err := os.Rename(lockPath, lockPath+".replaced"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterHostedLockOpenHook = nil })
	if _, err := Open(context.Background(), home, Options{HostedWriter: true}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHomeUnsafe {
		t.Fatalf("replaced hosted lock error=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, DatabaseFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock replacement opened database: %v", err)
	}
}

func TestHostedLockRejectsHomeReplacementAfterOpen(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "hosted")
	if err := MarkHosted(home); err != nil {
		t.Fatal(err)
	}
	parked := filepath.Join(root, "parked")
	afterHostedLockOpenHook = func() {
		afterHostedLockOpenHook = nil
		if err := os.Rename(home, parked); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(home, 0o700); err != nil {
			t.Fatal(err)
		}
		markHostedFixture(t, home)
	}
	t.Cleanup(func() { afterHostedLockOpenHook = nil })
	if _, err := Open(context.Background(), home, Options{HostedWriter: true}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHomeUnsafe {
		t.Fatalf("replaced hosted home error=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, DatabaseFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("home replacement opened database: %v", err)
	}
}

func TestHostedWriterMigratesOnlyUnderLock(t *testing.T) {
	home := createV1Home(t, true)
	markHostedFixture(t, home)
	if _, err := Open(context.Background(), home, Options{}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHostedHomeRequiresRemote {
		t.Fatalf("local migration error=%v", err)
	}
	if version := rawUserVersion(t, home); version != 1 {
		t.Fatalf("refused local open migrated schema to %d", version)
	}
	st, err := Open(context.Background(), home, Options{HostedWriter: true})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if version := rawUserVersion(t, home); version != SchemaVersion {
		t.Fatalf("hosted migration schema=%d", version)
	}
}

func TestMarkHostedRefusesNonEmptyHome(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "existing"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MarkHosted(home); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonInvalidArgument {
		t.Fatalf("mark non-empty home error=%v", err)
	}
}

func TestOrdinaryHomeDoesNotCreateHostedFiles(t *testing.T) {
	home := filepath.Join(t.TempDir(), "local")
	first, err := Open(context.Background(), home, Options{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(context.Background(), home, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	defer second.Close()
	for _, name := range []string{HostedMarkerFileName, HostedLockFileName} {
		if _, err := os.Lstat(filepath.Join(home, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("ordinary home created %s: %v", name, err)
		}
	}
}

func TestHostedLockSurvivesPausedHolder(t *testing.T) {
	if os.Getenv("WORKLEASE_HOSTED_HOLDER") == "1" {
		hostedWriterHelper(t)
		return
	}
	home := filepath.Join(t.TempDir(), "hosted")
	if err := MarkHosted(home); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestHostedLockSurvivesPausedHolder$")
	cmd.Env = append(os.Environ(), "WORKLEASE_HOSTED_HOLDER=1", "WORKLEASE_HOSTED_HOME="+home)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	ready, err := reader.ReadString('\n')
	if err != nil || ready != "ready\n" {
		t.Fatalf("holder ready=%q err=%v", ready, err)
	}
	if err := cmd.Process.Signal(syscall.SIGSTOP); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), home, Options{HostedWriter: true}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHostedLockHeld {
		_ = cmd.Process.Signal(syscall.SIGCONT)
		t.Fatalf("competitor while paused error=%v", err)
	}
	if err := cmd.Process.Signal(syscall.SIGCONT); err != nil {
		t.Fatal(err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case err := <-wait:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("holder did not exit")
	}
	takeover, err := Open(context.Background(), home, Options{HostedWriter: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := takeover.Close(); err != nil {
		t.Fatal(err)
	}
}

func markHostedFixture(t *testing.T, home string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, HostedMarkerFileName), []byte(hostedMarkerContents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func rawUserVersion(t *testing.T, home string) int64 {
	t.Helper()
	db, err := sql.Open("sqlite", sqliteDSN(filepath.Join(home, DatabaseFileName), true))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int64
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}

func TestHostedSecretFsyncFailureLeavesNoStagedGrant(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "secrets")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "bootstrap")
	afterHostedSecretSyncHook = func() error { return errors.New("injected directory durability failure") }
	t.Cleanup(func() { afterHostedSecretSyncHook = nil })
	if err := WriteHostedSecret(path, strings.Repeat("a", 64)); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHomeUnsafe {
		t.Fatalf("secret fsync error=%v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed secret remained visible: %v", err)
	}
}

func TestHostedReadyRequirementFailsClosed(t *testing.T) {
	home := filepath.Join(t.TempDir(), "hosted")
	if err := MarkHosted(home); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), home, Options{HostedWriter: true, RequireHostedReady: true}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonStorageFailure {
		t.Fatalf("incomplete hosted open error=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, DatabaseFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete hosted open touched database: %v", err)
	}
	lock, err := AcquireHostedLock(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteHostedReady(lock); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceHostedDatabase(lock, filepath.Join(t.TempDir(), "missing.db")); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHomeUnsafe {
		t.Fatalf("missing backup error=%v", err)
	}
	st, err := Open(context.Background(), home, Options{HostedWriter: true, RequireHostedReady: true, HostedLock: lock})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
}

func hostedWriterHelper(t *testing.T) {
	home := os.Getenv("WORKLEASE_HOSTED_HOME")
	st, err := Open(context.Background(), home, Options{HostedWriter: true})
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
}
