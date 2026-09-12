package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDriverPragmasAndPrivateModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), DatabaseFileName)
	driver := openTestDriver(t, path, false)
	defer driver.Close()
	assertPragma(t, driver.DB(), "journal_mode", "wal")
	assertPragma(t, driver.DB(), "synchronous", "2") // FULL
	assertPragma(t, driver.DB(), "busy_timeout", "10000")
	assertPragma(t, driver.DB(), "foreign_keys", "1")
	if got := driver.DB().Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("max open connections = %d, want 1", got)
	}
	if err := driver.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE values_table (id INTEGER PRIMARY KEY, value TEXT)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	assertPrivate(t, path)
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Lstat(path + suffix); err == nil {
			assertPrivate(t, path+suffix)
		}
	}
}

func TestDriverWriteRollbackAndAutoincrementDoesNotReuseCommittedIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), DatabaseFileName)
	driver := openTestDriver(t, path, false)
	defer driver.Close()
	mustWrite(t, driver, `CREATE TABLE sequence (id INTEGER PRIMARY KEY AUTOINCREMENT, value TEXT)`)
	mustWrite(t, driver, `INSERT INTO sequence(value) VALUES ('one')`)
	mustWrite(t, driver, `INSERT INTO sequence(value) VALUES ('two')`)
	if err := driver.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO sequence(value) VALUES ('rolled back')`)
		if err != nil {
			return err
		}
		return errors.New("injected failure")
	}); err == nil || err.Error() != "injected failure" {
		t.Fatalf("rollback error = %v", err)
	}
	mustWrite(t, driver, `DELETE FROM sequence WHERE id = 2`)
	mustWrite(t, driver, `INSERT INTO sequence(value) VALUES ('three')`)
	var id int
	if err := driver.DB().QueryRow(`SELECT id FROM sequence WHERE value = 'three'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if id != 3 {
		t.Fatalf("AUTOINCREMENT reused committed id: got %d, want 3", id)
	}
	var count int
	if err := driver.DB().QueryRow(`SELECT count(*) FROM sequence WHERE value = 'rolled back'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled-back row count = %d", count)
	}
}

func TestDriverReadIsDeferredAndHeldReadsDoNotBlockWriters(t *testing.T) {
	path := filepath.Join(t.TempDir(), DatabaseFileName)
	first := openTestDriver(t, path, false)
	defer first.Close()
	mustWrite(t, first, `CREATE TABLE locks (id INTEGER PRIMARY KEY, value TEXT)`)
	second := openTestDriver(t, path, false)
	defer second.Close()

	// BeginTx returns before the first read statement. A writer can commit
	// while the callback is paused here, proving BEGIN DEFERRED rather than
	// merely trusting database/sql TxOptions.
	begun := make(chan struct{})
	allowQuery := make(chan struct{})
	readDone := make(chan error, 1)
	go func() {
		readDone <- first.Read(context.Background(), func(tx *sql.Tx) error {
			close(begun)
			<-allowQuery
			var count int
			return tx.QueryRow(`SELECT count(*) FROM locks`).Scan(&count)
		})
	}()
	<-begun
	writeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	err := second.Write(writeCtx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO locks(value) VALUES ('before-read')`)
		return err
	})
	cancel()
	if err != nil {
		t.Fatalf("writer blocked before deferred read query: %v", err)
	}
	close(allowQuery)
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}

	// Once a read has actually observed a row, keep that transaction open.
	// WAL permits the independent writer to commit without waiting for the
	// held reader.
	readReady := make(chan struct{})
	releaseRead := make(chan struct{})
	readDone = make(chan error, 1)
	go func() {
		readDone <- first.Read(context.Background(), func(tx *sql.Tx) error {
			var count int
			if err := tx.QueryRow(`SELECT count(*) FROM locks`).Scan(&count); err != nil {
				return err
			}
			close(readReady)
			<-releaseRead
			return nil
		})
	}()
	<-readReady
	writeCtx, cancel = context.WithTimeout(context.Background(), 500*time.Millisecond)
	err = second.Write(writeCtx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO locks(value) VALUES ('beside-read')`)
		return err
	})
	cancel()
	if err != nil {
		t.Fatalf("writer blocked by held deferred read: %v", err)
	}
	close(releaseRead)
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
}

func TestDriverReadOnlyMissingDatabaseDoesNotCreateState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DatabaseFileName)
	before := directorySnapshot(t, dir)
	if _, err := OpenDriver(context.Background(), path, true); err == nil {
		t.Fatal("read-only missing database unexpectedly opened")
	}
	after := directorySnapshot(t, dir)
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatalf("read-only missing open changed directory: before=%v after=%v", before, after)
	}
}

func TestDriverReadOnlySeesCommittedWALWithoutFilesystemMutation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DatabaseFileName)
	writer := openTestDriver(t, path, false)
	defer writer.Close()
	mustWrite(t, writer, `CREATE TABLE wal_values (value TEXT)`)
	if _, err := writer.DB().Exec(`PRAGMA wal_autocheckpoint=0`); err != nil {
		t.Fatal(err)
	}
	// Establish the committed WAL state before taking the snapshot. No writer
	// mutation is allowed between the before/after observations.
	mustWrite(t, writer, `INSERT INTO wal_values(value) VALUES ('durable')`)
	before := snapshotDirectoryState(t, dir)
	reader := openTestDriver(t, path, true)
	defer reader.Close()
	var value string
	if err := reader.Read(context.Background(), func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT value FROM wal_values`).Scan(&value)
	}); err != nil {
		t.Fatal(err)
	}
	if value != "durable" {
		t.Fatalf("read-only value = %q", value)
	}
	after := snapshotDirectoryState(t, dir)
	if !sameReadOnlySnapshot(before, after, path+"-shm") {
		for name, beforeFile := range before {
			if !reflect.DeepEqual(beforeFile, after[name]) {
				t.Fatalf("read-only open/read changed %s: before mode=%v size=%d mtime=%d content=%d bytes, after mode=%v size=%d mtime=%d content=%d bytes", name, beforeFile.Mode, beforeFile.Size, beforeFile.ModTime, len(beforeFile.Content), after[name].Mode, after[name].Size, after[name].ModTime, len(after[name].Content))
			}
		}
		t.Fatalf("read-only open/read changed directory state")
	}
}

func TestDriverRejectsUnsafeMainAndSidecarPaths(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.db")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, DatabaseFileName)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDriver(context.Background(), link, false); err == nil {
		t.Fatal("symlink database was accepted")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(dir, "hardlink.db")
	if err := os.Link(target, hardlink); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDriver(context.Background(), hardlink, false); err == nil {
		t.Fatal("hard-linked database was accepted")
	}
	if err := os.Link(target, link+"-wal"); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDriver(context.Background(), link, false); err == nil {
		t.Fatal("hard-linked sidecar was accepted")
	}
	if err := os.Remove(link + "-wal"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link+"-shm"); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDriver(context.Background(), link, false); err == nil {
		t.Fatal("sidecar symlink was accepted")
	}
	if !contains(UnsafePathRaceWarning, "pre-open") || !contains(UnsafePathRaceWarning, "inode") {
		t.Fatalf("race warning is not explicit: %q", UnsafePathRaceWarning)
	}
}

func TestDriverCreatesPrivateStateWithPermissiveUmask(t *testing.T) {
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)
	path := filepath.Join(t.TempDir(), DatabaseFileName)
	driver := openTestDriver(t, path, false)
	defer driver.Close()
	mustWrite(t, driver, `CREATE TABLE permissive (value TEXT)`)
	mustWrite(t, driver, `INSERT INTO permissive(value) VALUES ('private')`)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		candidate := path + suffix
		if _, err := os.Lstat(candidate); err == nil {
			assertPrivate(t, candidate)
		}
	}
}

func TestDriverRejectsEveryUnsafeMainAndSidecarVariant(t *testing.T) {
	for _, component := range []string{"main", "-wal", "-shm"} {
		for _, kind := range []string{"symlink", "hardlink"} {
			t.Run(component+"/"+kind, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, DatabaseFileName)
				driver := openTestDriver(t, path, false)
				driver.Close()
				candidate := path
				if component != "main" {
					candidate += component
					_ = os.Remove(candidate)
				} else {
					if err := os.Remove(candidate); err != nil {
						t.Fatal(err)
					}
				}
				target := filepath.Join(dir, "unsafe-target")
				if err := os.WriteFile(target, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if kind == "symlink" {
					if err := os.Symlink(target, candidate); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Link(target, candidate); err != nil {
					t.Fatal(err)
				}
				if _, err := OpenDriver(context.Background(), path, false); err == nil {
					t.Fatalf("unsafe %s %s was accepted", component, kind)
				}
			})
		}
	}
}

func TestDriverDeterministicPathSwapIsDetected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DatabaseFileName)
	driver := openTestDriver(t, path, false)
	driver.Close()
	original := path + ".original"
	beforeSQLiteOpenHook = func(openPath string) {
		if err := os.Rename(openPath, original); err != nil {
			t.Fatalf("swap original: %v", err)
		}
		if err := os.Symlink(original, openPath); err != nil {
			t.Fatalf("swap symlink: %v", err)
		}
	}
	defer func() {
		beforeSQLiteOpenHook = nil
		_ = os.Remove(path)
		_ = os.Rename(original, path)
	}()
	if _, err := OpenDriver(context.Background(), path, true); err == nil {
		t.Fatal("deterministic path swap was accepted")
	}
	if !strings.Contains(UnsafePathRaceWarning, "residual") || !strings.Contains(UnsafePathRaceWarning, "pre-open hook") {
		t.Fatalf("race warning does not document residual hook limit: %q", UnsafePathRaceWarning)
	}
}

func TestDriverKilledTransactionsHaveDistinctDurabilityOutcomes(t *testing.T) {
	t.Run("uncommitted", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, DatabaseFileName)
		runKilledDriverHelper(t, "crash-uncommitted", path)
		driver := openTestDriver(t, path, true)
		defer driver.Close()
		var count int
		if err := driver.DB().QueryRow(`SELECT count(*) FROM crash_values`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("killed uncommitted rows = %d", count)
		}
	})
	t.Run("committed", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, DatabaseFileName)
		runKilledDriverHelper(t, "crash-committed", path)
		driver := openTestDriver(t, path, true)
		defer driver.Close()
		var count int
		if err := driver.DB().QueryRow(`SELECT count(*) FROM crash_values`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("killed committed rows = %d", count)
		}
	})
}

// TestDriverProcessHelper is deliberately part of the test binary so crash
// evidence is hermetic and does not depend on an external executable.
func TestDriverProcessHelper(t *testing.T) {
	mode := os.Getenv("WORKLEASE_DRIVER_HELPER")
	if mode == "" {
		return
	}
	arguments := driverHelperArgs(os.Args)
	path := arguments[0]
	openContext := context.Background()
	if mode == "serialize-contender" {
		var cancel context.CancelFunc
		openContext, cancel = context.WithTimeout(openContext, 300*time.Millisecond)
		defer cancel()
	}
	driver, err := OpenDriver(openContext, path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if mode == "serialize-holder" {
		mustWrite(t, driver, `CREATE TABLE IF NOT EXISTS serialized (value TEXT)`)
	} else if mode != "serialize-contender" {
		mustWrite(t, driver, `CREATE TABLE IF NOT EXISTS crash_values (value TEXT)`)
	}
	if mode == "crash-uncommitted" {
		tx, err := driver.DB().BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO crash_values(value) VALUES ('lost')`); err != nil {
			t.Fatal(err)
		}
		fmt.Print("ready")
		for {
			time.Sleep(time.Hour)
		}
	}
	if mode == "crash-committed" {
		mustWrite(t, driver, `INSERT INTO crash_values(value) VALUES ('kept')`)
		fmt.Print("ready")
		for {
			time.Sleep(time.Hour)
		}
	}
	if mode == "serialize-holder" {
		readyPath, releasePath := arguments[1], arguments[2]
		tx, err := driver.DB().BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`INSERT INTO serialized(value) VALUES ('held')`); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(readyPath, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		waitForFile(t, releasePath)
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		return
	}
	if mode == "serialize-contender" {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		err := driver.Write(ctx, func(tx *sql.Tx) error {
			_, err := tx.Exec(`INSERT INTO serialized(value) VALUES ('contender')`)
			return err
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("bounded contender write error = %v, want deadline exceeded", err)
		}
		fmt.Print("deadline")
	}
}

func driverHelperArgs(args []string) []string {
	for index, arg := range args {
		if arg == "--" {
			return args[index+1:]
		}
	}
	return nil
}

func openTestDriver(t *testing.T, path string, readOnly bool) *Driver {
	t.Helper()
	driver, err := OpenDriver(context.Background(), path, readOnly)
	if err != nil {
		t.Fatal(err)
	}
	return driver
}

func mustWrite(t *testing.T, driver *Driver, statement string) {
	t.Helper()
	if err := driver.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec(statement)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func assertPragma(t *testing.T, db *sql.DB, pragma, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("PRAGMA %s = %q, want %q", pragma, got, want)
	}
}

func assertPrivate(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("%s mode = %04o, want 0600", path, info.Mode().Perm())
	}
}

func directorySnapshot(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Name())
	}
	return result
}

type fileSnapshot struct {
	Mode    os.FileMode
	Size    int64
	ModTime int64
	Dev     uint64
	Ino     uint64
	Content []byte
}

func sameReadOnlySnapshot(before, after map[string]fileSnapshot, lockFile string) bool {
	if len(before) != len(after) {
		return false
	}
	for name, beforeFile := range before {
		afterFile, ok := after[name]
		if !ok {
			return false
		}
		if filepath.Base(filepath.Join(".", name)) == filepath.Base(lockFile) {
			// SQLite legitimately updates WAL shared-memory lock words while a
			// reader registers its snapshot. This is coordination metadata, not
			// a writer mutation; mode, size, inode and mtime remain immutable.
			beforeFile.Content = nil
			afterFile.Content = nil
		}
		if !reflect.DeepEqual(beforeFile, afterFile) {
			return false
		}
	}
	return true
}

func snapshotDirectoryState(t *testing.T, dir string) map[string]fileSnapshot {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	state := make(map[string]fileSnapshot, len(entries))
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		snapshot := fileSnapshot{Mode: info.Mode(), Size: info.Size(), ModTime: info.ModTime().UnixNano()}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			snapshot.Dev = uint64(stat.Dev)
			snapshot.Ino = uint64(stat.Ino)
		}
		if info.Mode().IsRegular() {
			snapshot.Content, err = os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
		}
		state[entry.Name()] = snapshot
	}
	return state
}

func runKilledDriverHelper(t *testing.T, mode, path string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestDriverProcessHelper$", "--", path)
	cmd.Env = append(os.Environ(), "WORKLEASE_DRIVER_HELPER="+mode)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make([]byte, 5)
	if _, err := stdout.Read(ready); err != nil {
		t.Fatal(err)
	}
	if string(ready) != "ready" {
		t.Fatalf("helper readiness = %q", ready)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("killed helper exited successfully")
	}
}

func contains(value, needle string) bool { return strings.Contains(value, needle) }

func TestPostCommitValidationFailureIsDefinitelyCommitted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DatabaseFileName)
	driver := openTestDriver(t, path, false)
	defer driver.Close()
	mustWrite(t, driver, `CREATE TABLE post_commit (value TEXT)`)
	target := filepath.Join(dir, "sidecar-target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := driver.Write(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO post_commit(value) VALUES ('durable')`); err != nil {
			return err
		}
		// SQLite already owns its sidecar descriptor. Replacing the directory
		// entry after the transaction work makes the post-commit safety check
		// fail without changing the commit outcome.
		if err := os.Remove(path + "-wal"); err != nil {
			return err
		}
		return os.Symlink(target, path+"-wal")
	})
	var commitErr *CommitError
	if !errors.As(err, &commitErr) || commitErr.Outcome != CommitCommitted {
		t.Fatalf("post-commit error = %v, want typed committed outcome", err)
	}
}

func TestCommitErrorOutcomeUsesFreshDurableReadback(t *testing.T) {
	path := filepath.Join(t.TempDir(), DatabaseFileName)
	driver := openTestDriver(t, path, false)
	defer driver.Close()
	mustWrite(t, driver, `CREATE TABLE outcomes (value TEXT)`)
	probe := func(want string) func(context.Context, *sql.DB) (bool, error) {
		return func(ctx context.Context, db *sql.DB) (bool, error) {
			var got string
			err := db.QueryRowContext(ctx, `SELECT value FROM outcomes LIMIT 1`).Scan(&got)
			if errors.Is(err, sql.ErrNoRows) {
				return false, nil
			}
			return got == want, err
		}
	}
	var commitErr *CommitError
	err := driver.classifyCommit(context.Background(), probe("present"), errors.New("injected commit error"))
	if !errors.As(err, &commitErr) || commitErr.Outcome != CommitNotCommitted {
		t.Fatalf("not-committed classification = %v", err)
	}
	mustWrite(t, driver, `INSERT INTO outcomes(value) VALUES ('present')`)
	err = driver.classifyCommit(context.Background(), probe("present"), errors.New("injected commit error"))
	if !errors.As(err, &commitErr) || commitErr.Outcome != CommitCommitted {
		t.Fatalf("committed classification = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	var probeContext context.Context
	probeSawLiveContext := false
	err = driver.classifyCommit(canceled, func(ctx context.Context, db *sql.DB) (bool, error) {
		probeContext = ctx
		probeSawLiveContext = ctx.Err() == nil
		return false, nil
	}, errors.New("injected commit error"))
	if !errors.As(err, &commitErr) || commitErr.Outcome != CommitNotCommitted {
		t.Fatalf("canceled-context classification = %v", err)
	}
	if probeContext == nil || !probeSawLiveContext {
		t.Fatalf("read-back used canceled caller context: %v", probeContext)
	}
	if _, ok := probeContext.Deadline(); !ok {
		t.Fatal("read-back context is not independently bounded")
	}
	err = driver.classifyCommit(context.Background(), nil, errors.New("injected commit error"))
	if !errors.As(err, &commitErr) || commitErr.Outcome != CommitUnknown {
		t.Fatalf("unknown classification = %v", err)
	}
}

func TestDriverCrossProcessBeginImmediateSerializesWriters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DatabaseFileName)
	readyPath := filepath.Join(dir, "ready")
	releasePath := filepath.Join(dir, "release")
	init := openTestDriver(t, path, false)
	mustWrite(t, init, `CREATE TABLE serialized (value TEXT)`)
	init.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^TestDriverProcessHelper$", "--", path, readyPath, releasePath)
	cmd.Env = append(os.Environ(), "WORKLEASE_DRIVER_HELPER=serialize-holder")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.WriteFile(releasePath, nil, 0o600)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	waitForFile(t, readyPath)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	contender := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDriverProcessHelper$", "--", path)
	contender.Env = append(os.Environ(), "WORKLEASE_DRIVER_HELPER=serialize-contender")
	var output bytes.Buffer
	contender.Stdout = &output
	if err := contender.Run(); err != nil {
		t.Fatalf("contender process failed before reporting its bounded write: %v (output %q)", err, output.String())
	}
	if ctx.Err() != nil {
		t.Fatalf("parent watchdog expired: %v", ctx.Err())
	}
	if !strings.Contains(output.String(), "deadline") {
		t.Fatalf("contender output = %q, want deadline", output.String())
	}
	if err := os.WriteFile(releasePath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
