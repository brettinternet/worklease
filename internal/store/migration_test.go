package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/testkit"
)

func createV1Home(t *testing.T, populated bool) string {
	t.Helper()
	home := t.TempDir()
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	path := testkit.DatabasePath(home)
	db, err := sql.Open("sqlite", sqliteDSN(path, false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	statements := []string{
		`CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE claims (claim_id TEXT PRIMARY KEY, token_hash TEXT NOT NULL, revision INTEGER NOT NULL, agent_id TEXT NOT NULL, session_id TEXT NOT NULL, work_key TEXT NOT NULL, guarantee TEXT NOT NULL CHECK (guarantee = 'local-coordination'), local_replace_allowed INTEGER NOT NULL CHECK (local_replace_allowed IN (0,1)), acquired_at INTEGER NOT NULL, ttl_us INTEGER NOT NULL, heartbeat_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, checkpoint TEXT)`,
		`CREATE TABLE claim_resources (resource TEXT PRIMARY KEY, claim_id TEXT NOT NULL REFERENCES claims(claim_id) ON DELETE CASCADE, position INTEGER NOT NULL)`,
		`CREATE INDEX claim_resources_by_claim ON claim_resources(claim_id)`,
		`CREATE TABLE epochs (claim_id TEXT PRIMARY KEY, token_hash TEXT NOT NULL, agent_id TEXT NOT NULL, session_id TEXT NOT NULL, work_key TEXT NOT NULL, guarantee TEXT NOT NULL, local_replace_allowed INTEGER NOT NULL, acquired_at INTEGER NOT NULL, acquired_seq INTEGER NOT NULL, ended_at INTEGER, ended_seq INTEGER, ended_recorded_at INTEGER, end_reason TEXT CHECK (end_reason IN ('released','transferred','expired')), final_revision INTEGER, successor_claim_id TEXT, checkpoint TEXT)`,
		`CREATE INDEX epochs_by_acquired_seq ON epochs(acquired_seq)`,
		`CREATE TABLE epoch_resources (claim_id TEXT NOT NULL REFERENCES epochs(claim_id) ON DELETE CASCADE, resource TEXT NOT NULL, position INTEGER NOT NULL, PRIMARY KEY (claim_id, position))`,
		`CREATE INDEX epoch_resources_by_resource ON epoch_resources(resource, claim_id)`,
		`CREATE TABLE operations (claim_id TEXT NOT NULL, operation_id TEXT NOT NULL, kind TEXT NOT NULL, request_hash TEXT NOT NULL, request_not_after INTEGER NOT NULL, expected_revision INTEGER NOT NULL, state TEXT NOT NULL CHECK (state IN ('started','completed','reconciled')), receipt TEXT, started_at INTEGER NOT NULL, started_seq INTEGER NOT NULL, completed_at INTEGER, completed_seq INTEGER, PRIMARY KEY (claim_id, operation_id))`,
		`CREATE INDEX operations_by_state ON operations(state)`,
		`CREATE UNIQUE INDEX one_started_per_claim ON operations(claim_id) WHERE state = 'started'`,
		`CREATE TABLE reconciliations (claim_id TEXT NOT NULL, operation_id TEXT NOT NULL, outcome TEXT NOT NULL CHECK (outcome IN ('observed-success','observed-failure')), evidence TEXT NOT NULL, request_hash TEXT NOT NULL, reconcile_operation_id TEXT NOT NULL, resolver_claim_id TEXT NOT NULL, resolver_agent_id TEXT NOT NULL, resolver_session_id TEXT NOT NULL, recorded_at INTEGER NOT NULL, recorded_seq INTEGER NOT NULL, PRIMARY KEY (claim_id, operation_id))`,
		`CREATE TABLE events (seq INTEGER PRIMARY KEY AUTOINCREMENT, at INTEGER NOT NULL, kind TEXT NOT NULL, claim_id TEXT, resources TEXT NOT NULL, operation_id TEXT, revision INTEGER, agent_id TEXT, detail TEXT NOT NULL DEFAULT '{}')`,
		`CREATE INDEX events_by_claim ON events(claim_id, seq)`,
		`CREATE INDEX events_by_at ON events(at)`,
		`INSERT INTO meta(key,value) VALUES ('created_at','1'),('authority_id','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'),('last_observed_at','2'),('last_event_seq','0'),('pruned_through_seq','0')`,
		`PRAGMA user_version = 1`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("create v1: %v\n%s", err, statement)
		}
	}
	if populated {
		population := []string{
			`INSERT INTO claims VALUES('11111111111111111111111111111111','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',3,'agent-label','session','work','local-coordination',1,10,20,11,31,'{"saved":true}')`,
			`INSERT INTO claim_resources VALUES('resource','11111111111111111111111111111111',0)`,
			`INSERT INTO epochs VALUES('11111111111111111111111111111111','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','agent-label','session','work','local-coordination',1,10,4,NULL,NULL,NULL,NULL,NULL,NULL,'{"saved":true}')`,
			`INSERT INTO epoch_resources VALUES('11111111111111111111111111111111','resource',0)`,
			`INSERT INTO operations VALUES('11111111111111111111111111111111','22222222222222222222222222222222','exec','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',50,2,'started',NULL,12,5,NULL,NULL)`,
			`INSERT INTO operations VALUES('11111111111111111111111111111111','33333333333333333333333333333333','acquire','cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc',50,1,'completed','{"committed":true}',10,4,10,4)`,
			`INSERT INTO events(seq,at,kind,claim_id,resources,operation_id,revision,agent_id,detail) VALUES(4,10,'acquired','11111111111111111111111111111111','["resource"]','33333333333333333333333333333333',1,'agent-label','{}')`,
			`INSERT INTO events(seq,at,kind,claim_id,resources,operation_id,revision,agent_id,detail) VALUES(5,12,'exec-started','11111111111111111111111111111111','["resource"]','22222222222222222222222222222222',2,'agent-label','{}')`,
			`UPDATE meta SET value='5' WHERE key='last_event_seq'`,
		}
		for _, statement := range population {
			if _, err := db.Exec(statement); err != nil {
				t.Fatalf("populate v1: %v\n%s", err, statement)
			}
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, sidecar := range testkit.DatabaseSidecarPaths(home) {
		if _, err := os.Lstat(sidecar); err == nil {
			if err := os.Chmod(sidecar, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return home
}

func TestOpenMigratesEmptyAndPopulatedV1Homes(t *testing.T) {
	for _, populated := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "populated"}[populated], func(t *testing.T) {
			home := createV1Home(t, populated)
			st, err := Open(context.Background(), home, Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			if st.AuthorityID() != strings.Repeat("a", 32) || !validAuthorityID(st.RestoreID()) || st.RestoreID() == st.AuthorityID() {
				t.Fatalf("identities authority=%q restore=%q", st.AuthorityID(), st.RestoreID())
			}
			var version int64
			if err := st.db().QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != SchemaVersion {
				t.Fatalf("version=%d err=%v", version, err)
			}
			if !populated {
				return
			}
			var claims, started, replay, events int
			var checkpoint, epochCheckpoint string
			if err := st.db().QueryRow(`SELECT count(*),checkpoint FROM claims`).Scan(&claims, &checkpoint); err != nil {
				t.Fatal(err)
			}
			if err := st.db().QueryRow(`SELECT checkpoint FROM epochs WHERE claim_id='11111111111111111111111111111111'`).Scan(&epochCheckpoint); err != nil {
				t.Fatal(err)
			}
			if err := st.db().QueryRow(`SELECT count(*) FROM operations WHERE state='started'`).Scan(&started); err != nil {
				t.Fatal(err)
			}
			if err := st.db().QueryRow(`SELECT count(*) FROM operations WHERE receipt IS NOT NULL`).Scan(&replay); err != nil {
				t.Fatal(err)
			}
			if err := st.db().QueryRow(`SELECT count(*) FROM events`).Scan(&events); err != nil {
				t.Fatal(err)
			}
			if claims != 1 || started != 1 || replay != 1 || events != 2 || checkpoint != `{"saved":true}` || epochCheckpoint != checkpoint {
				t.Fatalf("preservation claims=%d started=%d replay=%d events=%d checkpoints=%q/%q", claims, started, replay, events, checkpoint, epochCheckpoint)
			}
			var sequence string
			if err := st.db().QueryRow(`SELECT value FROM meta WHERE key='last_event_seq'`).Scan(&sequence); err != nil || sequence != "5" {
				t.Fatalf("cursor watermark=%q err=%v", sequence, err)
			}
		})
	}
}

func TestConcurrentV1MigrationRunsOnce(t *testing.T) {
	home := createV1Home(t, true)
	var wg sync.WaitGroup
	stores := make(chan *Store, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st, err := Open(context.Background(), home, Options{})
			if err != nil {
				errs <- err
				return
			}
			stores <- st
		}()
	}
	wg.Wait()
	close(stores)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var restore string
	for st := range stores {
		if restore == "" {
			restore = st.RestoreID()
		} else if st.RestoreID() != restore {
			t.Fatalf("restore IDs differ: %q and %q", restore, st.RestoreID())
		}
		_ = st.Close()
	}
}

func TestV1MigrationFailureRollsBackCompletely(t *testing.T) {
	home := createV1Home(t, true)
	beforeMigrationStatementHook = func(index int, _ string) error {
		if index == 16 {
			return errors.New("injected migration failure")
		}
		return nil
	}
	t.Cleanup(func() { beforeMigrationStatementHook = nil })
	if _, err := Open(context.Background(), home, Options{}); err == nil || !strings.Contains(err.Error(), "injected migration failure") {
		t.Fatalf("migration error=%v", err)
	}
	beforeMigrationStatementHook = nil
	db, err := sql.Open("sqlite", sqliteDSN(testkit.DatabasePath(home), false))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int64
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 1 {
		t.Fatalf("rolled-back version=%d err=%v", version, err)
	}
	var restoreRows, recoveryTables int
	if err := db.QueryRow(`SELECT count(*) FROM meta WHERE key='restore_id'`).Scan(&restoreRows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='recovery_state'`).Scan(&recoveryTables); err != nil {
		t.Fatal(err)
	}
	if restoreRows != 0 || recoveryTables != 0 {
		t.Fatalf("partial migration restoreRows=%d recoveryTables=%d", restoreRows, recoveryTables)
	}
}

func TestV2RemoteSchemaConstraints(t *testing.T) {
	st := openStore(t, t.TempDir(), false)
	defer st.Close()
	ctx := context.Background()
	if err := st.Write(ctx, func(tx *Tx) error {
		for index, endReason := range []string{"restored", "revoked"} {
			id := strings.Repeat(string(rune('1'+index)), 32)
			if _, err := tx.ExecContext(ctx, `INSERT INTO epochs(claim_id,token_hash,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,acquired_seq,ended_at,end_reason,final_revision) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, strings.Repeat("a", 64), "label", "session", "work", "local-coordination", 0, 1, index+1, 2, endReason, 1); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	invalid := []string{
		`INSERT INTO installations(installation_id,credential_hash,role,label,enrolled_at,restore_id,enrolled_by_invite_id,request_id,request_hash) VALUES('UPPERCASE00000000000000000000000','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','admin','duplicate label',1,'restore','invite','request','hash')`,
		`INSERT INTO installations(installation_id,credential_hash,role,label,enrolled_at,restore_id,enrolled_by_invite_id,request_id,request_hash) VALUES('11111111111111111111111111111111','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','owner','duplicate label',1,'restore','invite','request','hash')`,
		`INSERT INTO invites(invite_id,invite_hash,role,label,issued_at,expires_at,restore_id,issue_operation_id,issue_request_hash,request_not_after,state,used_at) VALUES('22222222222222222222222222222222','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','admin','duplicate label',1,2,'restore','operation','hash',3,'used',2)`,
		`INSERT INTO invites(invite_id,invite_hash,role,label,issued_at,expires_at,restore_id,issue_operation_id,issue_request_hash,request_not_after,revoked_by_installation_id) VALUES('33333333333333333333333333333333','cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc','admin','duplicate label',1,2,'restore','operation','hash',3,'44444444444444444444444444444444')`,
	}
	for _, statement := range invalid {
		if err := st.Write(ctx, func(tx *Tx) error { _, err := tx.ExecContext(ctx, statement); return err }); err == nil {
			t.Fatalf("invalid remote state accepted: %s", statement)
		}
	}
	if err := st.Write(ctx, func(tx *Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO invites(invite_id,invite_hash,role,label,issued_at,expires_at,restore_id,issue_operation_id,issue_request_hash,request_not_after) VALUES(?,?,?,?,?,?,?,?,?,?)`, strings.Repeat("2", 32), strings.Repeat("b", 64), "admin", "duplicate label", 1, 10, st.RestoreID(), "operation", "hash", 20); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO invite_redemptions(invite_id,request_id,request_hash,request_not_after,expected_restore_id,installation_id,credential_hash,result,redeemed_at,replay_until) VALUES(?,?,?,?,?,?,?,?,?,?)`, strings.Repeat("2", 32), "request", "hash", 20, st.RestoreID(), strings.Repeat("3", 32), strings.Repeat("c", 64), `{}`, 10, 21)
		return err
	}); err == nil {
		t.Fatal("redemption replay beyond request deadline was accepted")
	}
	for _, forbidden := range []string{"invite_code", "credential", "bearer", "token"} {
		var columns int
		if err := st.db().QueryRow(`SELECT count(*) FROM pragma_table_info('invites') WHERE name=?`, forbidden).Scan(&columns); err != nil || columns != 0 {
			t.Fatalf("plaintext invite column %q present: count=%d err=%v", forbidden, columns, err)
		}
	}
}

func TestOpenRejectsWeakenedV2CredentialIndex(t *testing.T) {
	home := t.TempDir()
	st := openStore(t, home, false)
	if _, err := st.db().Exec(`DROP INDEX installations_by_credential`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db().Exec(`CREATE INDEX installations_by_credential ON installations(credential_hash)`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), home, Options{}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonSchemaCorrupt {
		t.Fatalf("weakened credential index error=%v", err)
	}
}

func TestSchemaVersionCompatibilityDetails(t *testing.T) {
	home := createV1Home(t, false)
	if _, err := Open(context.Background(), home, Options{ReadOnly: true}); reason.As(err) == nil || reason.As(err).Details["supportedVersion"] != int64(2) || reason.As(err).Details["foundVersion"] != int64(1) {
		t.Fatalf("v2 read-only v1 error=%v details=%+v", err, reason.As(err).Details)
	}
	st, err := Open(context.Background(), home, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	if err := openAsV1Binary(testkit.DatabasePath(home)); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonSchemaUnsupported {
		t.Fatalf("v1 binary compatibility error=%v", err)
	}
}

func openAsV1Binary(path string) error {
	db, err := sql.Open("sqlite", sqliteDSN(path, true))
	if err != nil {
		return err
	}
	defer db.Close()
	var found int64
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&found); err != nil {
		return err
	}
	if found != 1 {
		return reason.New(reason.ReasonSchemaUnsupported, "unsupported schema version").With("supportedVersion", int64(1)).With("foundVersion", found)
	}
	return nil
}
