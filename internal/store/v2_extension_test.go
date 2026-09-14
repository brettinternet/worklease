package store

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/brettinternet/worklease/internal/reason"
)

func makeOldV2Home(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	st := openStore(t, home, false)
	for _, statement := range []string{
		`DROP INDEX admin_operation_replays_by_deadline`,
		`DROP TABLE admin_operation_replays`,
		`DELETE FROM meta WHERE key='schema_v2_admin_operation_replays'`,
	} {
		if _, err := st.db().Exec(statement); err != nil {
			st.Close()
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestReadOnlyOldV2AdminReplaySchemaDoesNotMutate(t *testing.T) {
	home := makeOldV2Home(t)
	st, err := Open(context.Background(), home, Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	var marker, table, index int
	if err := st.db().QueryRow(`SELECT count(*) FROM meta WHERE key='schema_v2_admin_operation_replays'`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if err := st.db().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='admin_operation_replays'`).Scan(&table); err != nil {
		t.Fatal(err)
	}
	if err := st.db().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='admin_operation_replays_by_deadline'`).Scan(&index); err != nil {
		t.Fatal(err)
	}
	if marker != 0 || table != 0 || index != 0 {
		t.Fatalf("read-only open mutated old v2 schema marker=%d table=%d index=%d", marker, table, index)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOldV2AdminReplaySchemaCompletesTransactionally(t *testing.T) {
	home := makeOldV2Home(t)
	st, err := Open(context.Background(), home, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var marker, tables, indexes int
	if err := st.db().QueryRow(`SELECT count(*) FROM meta WHERE key='schema_v2_admin_operation_replays' AND value='1'`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if err := st.db().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='admin_operation_replays'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := st.db().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='admin_operation_replays_by_deadline'`).Scan(&indexes); err != nil {
		t.Fatal(err)
	}
	if marker != 1 || tables != 1 || indexes != 1 {
		t.Fatalf("extension marker=%d table=%d index=%d", marker, tables, indexes)
	}
}

func TestOldV2AdminReplaySchemaRollback(t *testing.T) {
	home := makeOldV2Home(t)
	beforeV2ExtensionStatementHook = func(index int, _ string) error {
		if index == 1 {
			return errors.New("extension failure")
		}
		return nil
	}
	t.Cleanup(func() { beforeV2ExtensionStatementHook = nil })
	if _, err := Open(context.Background(), home, Options{}); err == nil || !strings.Contains(err.Error(), "extension failure") {
		t.Fatalf("extension error=%v", err)
	}
	beforeV2ExtensionStatementHook = nil
	st, err := Open(context.Background(), home, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var tables, marker int
	if err := st.db().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='admin_operation_replays'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := st.db().QueryRow(`SELECT count(*) FROM meta WHERE key='schema_v2_admin_operation_replays'`).Scan(&marker); err != nil {
		t.Fatal(err)
	}
	if tables != 1 || marker != 1 {
		t.Fatalf("rollback did not complete on retry table=%d marker=%d", tables, marker)
	}
}

func TestConcurrentOldV2AdminReplaySchemaCompletion(t *testing.T) {
	home := makeOldV2Home(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := Open(context.Background(), home, Options{})
			if err == nil {
				err = st.Close()
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestMarkedAdminReplaySchemaCorruptionIsRejected(t *testing.T) {
	home := t.TempDir()
	st := openStore(t, home, false)
	if _, err := st.db().Exec(`DROP INDEX admin_operation_replays_by_deadline; CREATE INDEX admin_operation_replays_by_deadline ON admin_operation_replays(operation_id)`); err != nil {
		st.Close()
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), home, Options{}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonSchemaCorrupt {
		t.Fatalf("marked schema corruption error=%v", err)
	}
}
