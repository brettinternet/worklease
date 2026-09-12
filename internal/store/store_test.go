package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestOpenBootstrapsNormativeSchemaAndStableAuthority(t *testing.T) {
	home := filepath.Join(t.TempDir(), "nested", "home")
	st := openStore(t, home, false)
	id := st.AuthorityID()
	if !validAuthorityID(id) {
		t.Fatalf("authority ID = %q", id)
	}
	if st.db() == nil {
		t.Fatal("write store has no database")
	}
	assertMode(t, home, 0o700)
	assertMode(t, testkit.DatabasePath(home), 0o600)
	for _, sidecar := range testkit.DatabaseSidecarPaths(home) {
		if _, err := os.Lstat(sidecar); err == nil {
			assertMode(t, sidecar, 0o600)
		}
	}
	st.Close()
	reopened := openStore(t, home, false)
	defer reopened.Close()
	if reopened.AuthorityID() != id {
		t.Fatalf("authority ID changed: %q -> %q", id, reopened.AuthorityID())
	}
	for name := range requiredTables {
		assertSQLiteObject(t, reopened.db(), name, "table")
	}
	for name := range requiredIndexes {
		assertSQLiteObject(t, reopened.db(), name, "index")
	}
	var version int64
	if err := reopened.db().QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
		t.Fatalf("schema version = %d, err=%v", version, err)
	}
}

func TestCompetingWriteOpensBootstrapOneSchema(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	results := make(chan *Store, 2)
	errors := make(chan error, 2)
	for range 2 {
		go func() {
			st, err := Open(context.Background(), home, Options{})
			if err != nil {
				errors <- err
				return
			}
			results <- st
		}()
	}
	var stores []*Store
	for range 2 {
		select {
		case st := <-results:
			stores = append(stores, st)
		case err := <-errors:
			if classified := reason.As(err); classified != nil {
				t.Fatalf("%v details=%v", err, classified.Details)
			}
			t.Fatal(err)
		}
	}
	defer func() {
		for _, st := range stores {
			_ = st.Close()
		}
	}()
	if stores[0].AuthorityID() == "" || stores[0].AuthorityID() != stores[1].AuthorityID() {
		t.Fatalf("competing opens disagree on authority: %q and %q", stores[0].AuthorityID(), stores[1].AuthorityID())
	}
}

func TestOpenReadOnlyMissingStateDoesNotCreateOrChmod(t *testing.T) {
	home := filepath.Join(t.TempDir(), "missing", "home")
	st, err := Open(context.Background(), home, Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if !st.Empty() {
		t.Fatal("missing read-only authority is not empty")
	}
	if _, err := os.Lstat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only open created home: %v", err)
	}
}

func TestOpenRejectsUnsafeAncestorsAndHomeWithoutMutation(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), filepath.Join(link, "home"), Options{}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHomeUnsafe {
		t.Fatalf("symlink ancestor error = %v", err)
	}
	foreign := filepath.Join(root, "foreign")
	if err := os.Mkdir(foreign, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(foreign, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), filepath.Join(foreign, "home"), Options{}); err == nil {
		t.Fatal("unsafe writable ancestor accepted")
	}
}

func TestSchemaRejectsUnsupportedAndCorruptWithoutMigration(t *testing.T) {
	t.Run("unsupported", func(t *testing.T) {
		home := t.TempDir()
		path := testkit.DatabasePath(home)
		if err := os.MkdirAll(home, 0o700); err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite", sqliteDSN(path, false))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("PRAGMA user_version=99"); err != nil {
			t.Fatal(err)
		}
		db.Close()
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		_, err = Open(context.Background(), home, Options{})
		if e := reason.As(err); e == nil || e.Reason != reason.ReasonSchemaUnsupported {
			t.Fatalf("unsupported error = %v", err)
		}
	})
	t.Run("corrupt", func(t *testing.T) {
		home := t.TempDir()
		st := openStore(t, home, false)
		st.Close()
		// Removing a required object leaves user_version=1 and must never cause a migration.
		db, err := sql.Open("sqlite", sqliteDSN(testkit.DatabasePath(home), false))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("DROP TABLE events"); err != nil {
			t.Fatal(err)
		}
		db.Close()
		_, err = Open(context.Background(), home, Options{})
		if e := reason.As(err); e == nil || e.Reason != reason.ReasonSchemaCorrupt {
			t.Fatalf("corrupt error = %v", err)
		}
	})
}

func TestWriteRollbackReadDeferredAndAppendEventBoundary(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "home"), false)
	defer st.Close()
	if err := st.Write(context.Background(), func(tx *Tx) error {
		if _, err := tx.execContext(context.Background(), "CREATE TABLE rollback_probe(value TEXT)"); err != nil {
			return err
		}
		return errors.New("before commit")
	}); err == nil {
		t.Fatal("pre-commit failure was accepted")
	}
	if _, err := st.db().Query("SELECT 1 FROM rollback_probe"); err == nil {
		t.Fatal("rolled-back schema is visible")
	}
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = st.Read(context.Background(), func(tx *Tx) error { close(started); <-release; return nil })
	}()
	<-started
	if err := st.Write(context.Background(), func(tx *Tx) error {
		_, err := tx.execContext(context.Background(), "INSERT INTO meta(key,value) VALUES('probe','value')")
		return err
	}); err != nil {
		t.Fatalf("writer blocked by deferred read: %v", err)
	}
	close(release)
	var seq int64
	if err := st.Write(context.Background(), func(tx *Tx) error {
		returnValue, err := tx.AppendEvent(Event{At: time.Now(), Kind: "acquired", ClaimID: "c", Resources: []string{"r"}, AgentID: "a"})
		seq = returnValue
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if seq != 1 {
		t.Fatalf("event seq = %d", seq)
	}
	var watermark string
	if err := st.db().QueryRow("SELECT value FROM meta WHERE key='last_event_seq'").Scan(&watermark); err != nil || watermark != "1" {
		t.Fatalf("event watermark = %q, err=%v", watermark, err)
	}
}

func TestAppendEventRejectsPrivateAndUnknownNestedPayload(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "home"), false)
	defer st.Close()
	cases := []Event{
		{At: time.Now(), Kind: "acquired", Resources: []string{"r"}, Detail: map[string]any{"token": "secret"}},
		{At: time.Now(), Kind: "acquired", Resources: []string{"r"}, Detail: map[string]any{"reason": map[string]any{"checkpoint": "private"}}},
		{At: time.Now(), Kind: "not-a-kind", Resources: []string{"r"}},
	}
	for _, event := range cases {
		if err := st.Write(context.Background(), func(tx *Tx) error { _, err := tx.AppendEvent(event); return err }); err == nil {
			t.Fatalf("private/invalid event accepted: %+v", event)
		}
	}
	var count int
	if err := st.db().QueryRow("SELECT count(*) FROM events").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected events persisted: %d (%v)", count, err)
	}
}

func TestAppendEventCrossConnectionInvisibleBeforeCommit(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	st := openStore(t, home, false)
	defer st.Close()
	reader := openStore(t, home, true)
	defer reader.Close()
	entered := make(chan struct{})
	continueCommit := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- st.Write(context.Background(), func(tx *Tx) error {
			if _, err := tx.AppendEvent(Event{At: time.Now(), Kind: "renewed", ClaimID: "c"}); err != nil {
				return err
			}
			close(entered)
			<-continueCommit
			return nil
		})
	}()
	<-entered
	var count int
	if err := reader.db().QueryRow("SELECT count(*) FROM events").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("uncommitted event visible: %d", count)
	}
	close(continueCommit)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := reader.db().QueryRow("SELECT count(*) FROM events").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("committed event count = %d", count)
	}
}

func TestAppendEventCrossProcessSequenceOrder(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	st := openStore(t, home, false)
	st.Close()
	resultDir := t.TempDir()
	commands := make([]*exec.Cmd, 4)
	for index := range commands {
		result := filepath.Join(resultDir, strconv.Itoa(index))
		command := exec.Command(os.Args[0], "-test.run=^TestStoreEventProcessHelper$")
		command.Env = append(os.Environ(), "WORKLEASE_STORE_EVENT_HOME="+home, "WORKLEASE_STORE_EVENT_RESULT="+result)
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		commands[index] = command
	}
	for _, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatalf("event helper failed: %v", err)
		}
	}
	sequences := make([]int, 0, len(commands))
	for index := range commands {
		contents, err := os.ReadFile(filepath.Join(resultDir, strconv.Itoa(index)))
		if err != nil {
			t.Fatal(err)
		}
		sequence, err := strconv.Atoi(string(contents))
		if err != nil {
			t.Fatal(err)
		}
		sequences = append(sequences, sequence)
	}
	sort.Ints(sequences)
	for index, sequence := range sequences {
		if sequence != index+1 {
			t.Fatalf("cross-process sequences = %v", sequences)
		}
	}
}

func TestStoreEventProcessHelper(t *testing.T) {
	home := os.Getenv("WORKLEASE_STORE_EVENT_HOME")
	if home == "" {
		return
	}
	st := openStore(t, home, false)
	defer st.Close()
	var sequence int64
	if err := st.Write(context.Background(), func(tx *Tx) error {
		var err error
		sequence, err = tx.AppendEvent(Event{At: time.Now(), Kind: "renewed", ClaimID: "helper", Resources: []string{"resource"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("WORKLEASE_STORE_EVENT_RESULT"), []byte(strconv.FormatInt(sequence, 10)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOperationStartedConstraintAndEpochMetadata(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "home"), false)
	defer st.Close()
	if err := st.Write(context.Background(), func(tx *Tx) error {
		_, err := tx.execContext(context.Background(), `INSERT INTO epochs(claim_id,token_hash,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,acquired_seq) VALUES ('c','hash','a','s','w','local-coordination',1,1,1)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(context.Background(), func(tx *Tx) error {
		_, err := tx.execContext(context.Background(), `INSERT INTO operations(claim_id,operation_id,kind,request_hash,request_not_after,expected_revision,state,started_at,started_seq) VALUES ('c','one','exec','r',2,1,'started',1,1)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(context.Background(), func(tx *Tx) error {
		_, err := tx.execContext(context.Background(), `INSERT INTO operations(claim_id,operation_id,kind,request_hash,request_not_after,expected_revision,state,started_at,started_seq) VALUES ('c','two','exec','r',2,1,'started',1,1)`)
		return err
	}); err == nil {
		t.Fatal("second started operation accepted")
	}
	var state string
	if err := st.db().QueryRow("SELECT state FROM operations WHERE operation_id='one'").Scan(&state); err != nil || state != "started" {
		t.Fatalf("first operation state = %q, %v", state, err)
	}
}

func TestOpenCreatesAndValidatesPrivateHandlesDirectory(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	st := openStore(t, home, false)
	st.Close()
	assertMode(t, filepath.Join(home, "handles"), 0o700)

	if err := os.Chmod(filepath.Join(home, "handles"), 0o777); err != nil {
		t.Fatal(err)
	}
	st = openStore(t, home, false)
	st.Close()
	assertMode(t, filepath.Join(home, "handles"), 0o700)

	if err := os.Remove(filepath.Join(home, "handles")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(home, "handles")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), home, Options{}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHomeUnsafe {
		t.Fatalf("unsafe handles error = %v", err)
	}
}

func TestHomeSwapCannotChmodSymlinkTargetAndRootIsRejected(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	beforeHomeOpenHook = func(path string) {
		if path != home {
			return
		}
		if err := os.Rename(home, home+".original"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, home); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Open(context.Background(), home, Options{})
	beforeHomeOpenHook = nil
	if err == nil {
		t.Fatal("swapped home was accepted")
	}
	assertMode(t, target, 0o755)
	if _, err := Open(context.Background(), string(filepath.Separator), Options{}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHomeUnsafe {
		t.Fatalf("filesystem root error = %v", err)
	}
}

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

func TestSchemaRejectsWeakenedStartedOperationIndexAndWatermark(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	st := openStore(t, home, false)
	st.Close()
	db, err := sql.Open("sqlite", sqliteDSN(testkit.DatabasePath(home), false))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP INDEX one_started_per_claim; CREATE INDEX one_started_per_claim ON operations(claim_id);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Open(context.Background(), home, Options{}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonSchemaCorrupt {
		t.Fatalf("weakened index error = %v", err)
	}

	home = filepath.Join(t.TempDir(), "home")
	st = openStore(t, home, false)
	if _, err := st.db().Exec(`UPDATE meta SET value='not-a-number' WHERE key='last_event_seq'`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if _, err := Open(context.Background(), home, Options{}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonSchemaCorrupt {
		t.Fatalf("invalid watermark error = %v", err)
	}
}

func openStore(t *testing.T, home string, readOnly bool) *Store {
	t.Helper()
	st, err := Open(context.Background(), home, Options{ReadOnly: readOnly})
	if err != nil {
		if classified := reason.As(err); classified != nil {
			t.Fatalf("%v details=%v", err, classified.Details)
		}
		t.Fatal(err)
	}
	return st
}
func assertMode(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != mode {
		t.Fatalf("%s mode=%o, want %o", path, info.Mode().Perm(), mode)
	}
}
func assertSQLiteObject(t *testing.T, db *sql.DB, name, typ string) {
	t.Helper()
	var got string
	err := db.QueryRow("SELECT type FROM sqlite_master WHERE name=?", name).Scan(&got)
	if err != nil || got != typ {
		t.Fatalf("sqlite object %s=%q, want %s (%v)", name, got, typ, err)
	}
}
