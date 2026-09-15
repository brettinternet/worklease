package main

import (
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/ledger"
)

func TestRequireOperationPendingClearedRejectsRetainedBeginHandle(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handlesRoot := filepath.Join(root, "handles")
	if err := os.Mkdir(handlesRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	operationID := strings.Repeat("d", 32)
	authorityID, claimID := strings.Repeat("a", 32), strings.Repeat("b", 32)
	handlePath := filepath.Join(handlesRoot, "retained.json")
	stored := handle.Handle{
		SchemaVersion: handle.RemoteSchemaVersion,
		AuthorityID:   authorityID,
		ClaimID:       claimID,
		Token:         strings.Repeat("c", 64),
		Resources:     []string{"coordination:retained"},
		AgentID:       "test-agent",
		SessionID:     "test-session",
		State:         "pending",
		PendingRequest: &handle.PendingRequest{
			OperationID: operationID, Kind: "exec", AuthorityID: authorityID, ClaimID: claimID,
			RequestHash: strings.Repeat("e", 64), RequestNotAfter: time.Now().Add(time.Hour), Inputs: map[string]any{"request": "retained"},
		},
	}
	if err := handle.Write(handlePath, stored); err != nil {
		t.Fatal(err)
	}
	if err := requireOperationPendingCleared(handlePath, filepath.Join(root, "pending"), operationID); err == nil || !strings.Contains(err.Error(), "remains pending in handle") {
		t.Fatalf("retained begin handle was not rejected: %v", err)
	}
}

func TestBackupOperationPresenceTreatsDuplicateIDsAsPresent(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "backup.db")
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE operations (claim_id TEXT NOT NULL, operation_id TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	operationID := strings.Repeat("e", 32)
	if _, err := database.Exec(`INSERT INTO operations(claim_id, operation_id) VALUES (?, ?), (?, ?)`, strings.Repeat("a", 32), operationID, strings.Repeat("b", 32), operationID); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	presence, err := backupOperationPresence(databasePath, operationID, strings.Repeat("f", 32))
	if err != nil {
		t.Fatal(err)
	}
	if !presence[operationID] || presence[strings.Repeat("f", 32)] {
		t.Fatalf("unexpected operation presence: %v", presence)
	}
}

func TestRequireWatchEvent(t *testing.T) {
	cursor := "before"
	valid := map[string]any{
		"cursor": cursor, "nextCursor": "after",
		"event": map[string]any{"kind": "acquired", "resources": []any{"coordination:other", "coordination:wanted"}},
	}
	if err := requireWatchEvent(valid, cursor, "acquired", "coordination:wanted"); err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string]map[string]any{
		"wrong cursor":   {"cursor": "other", "nextCursor": "after", "event": valid["event"]},
		"not advanced":   {"cursor": cursor, "nextCursor": cursor, "event": valid["event"]},
		"wrong kind":     {"cursor": cursor, "nextCursor": "after", "event": map[string]any{"kind": "released", "resources": []any{"coordination:wanted"}}},
		"wrong resource": {"cursor": cursor, "nextCursor": "after", "event": map[string]any{"kind": "acquired", "resources": []any{"coordination:other"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := requireWatchEvent(invalid, cursor, "acquired", "coordination:wanted"); err == nil {
				t.Fatal("invalid watch result accepted")
			}
		})
	}
}

func TestRequireWatchFreeAfterEvent(t *testing.T) {
	resource := "coordination:wanted"
	valid := map[string]any{"free": true, "timedOut": false, "cursor": "release", "nextCursor": "release", "resources": []any{map[string]any{"resource": resource, "state": "free"}}}
	if err := requireWatchFreeAfterEvent(valid, resource); err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string]map[string]any{
		"not free":      {"free": false, "cursor": "release", "nextCursor": "release", "resources": valid["resources"]},
		"timed out":     {"free": true, "timedOut": true, "cursor": "release", "nextCursor": "release", "resources": valid["resources"]},
		"no event scan": {"free": true, "cursor": "", "nextCursor": "release", "resources": valid["resources"]},
		"wrong cursor":  {"free": true, "cursor": "release", "nextCursor": "other", "resources": valid["resources"]},
		"wrong state":   {"free": true, "cursor": "release", "nextCursor": "release", "resources": []any{map[string]any{"resource": resource, "state": "active"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := requireWatchFreeAfterEvent(invalid, resource); err == nil {
				t.Fatal("invalid watch release accepted")
			}
		})
	}
}

func TestRequireWatchTimeout(t *testing.T) {
	cursor := "resume"
	if err := requireWatchTimeout(map[string]any{"timedOut": true, "cursor": cursor, "nextCursor": cursor}, cursor); err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string]map[string]any{
		"event":        {"timedOut": true, "cursor": cursor, "nextCursor": cursor, "event": map[string]any{"kind": "acquired"}},
		"not timeout":  {"timedOut": false, "cursor": cursor, "nextCursor": cursor},
		"wrong cursor": {"timedOut": true, "cursor": "other", "nextCursor": cursor},
		"advanced":     {"timedOut": true, "cursor": cursor, "nextCursor": "other"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := requireWatchTimeout(invalid, cursor); err == nil {
				t.Fatal("invalid watch timeout accepted")
			}
		})
	}
}

func TestRequireRetentionGapsRejectFalsePasses(t *testing.T) {
	authorityID, restoreID := strings.Repeat("a", 32), strings.Repeat("b", 32)
	cursor := ledger.EncodeCursor(authorityID, restoreID, "events", "", 0)
	reset := ledger.EncodeCursor(authorityID, restoreID, "events", "", 7)
	events := map[string]any{"gap": true, "events": []any{}, "nextCursor": reset}
	if err := requireEventsGap(events, cursor, "7"); err != nil {
		t.Fatal(err)
	}
	watch := map[string]any{"gap": true, "nextCursor": reset, "resetCursor": reset}
	if err := requireWatchGap(watch, cursor, "7"); err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string]map[string]any{
		"events with rows": {"gap": true, "events": []any{map[string]any{"kind": "acquired"}}, "nextCursor": reset},
		"events no gap":    {"gap": false, "events": []any{}, "nextCursor": reset},
		"events no reset":  {"gap": true, "events": []any{}, "nextCursor": cursor},
	} {
		t.Run(name, func(t *testing.T) {
			if err := requireEventsGap(invalid, cursor, "7"); err == nil {
				t.Fatal("invalid events gap accepted")
			}
		})
	}
	for name, invalid := range map[string]map[string]any{
		"watch with event":  {"gap": true, "event": map[string]any{"kind": "acquired"}, "nextCursor": reset, "resetCursor": reset},
		"watch no gap":      {"gap": false, "nextCursor": reset, "resetCursor": reset},
		"watch wrong reset": {"gap": true, "nextCursor": reset, "resetCursor": cursor},
	} {
		t.Run(name, func(t *testing.T) {
			if err := requireWatchGap(invalid, cursor, "7"); err == nil {
				t.Fatal("invalid watch gap accepted")
			}
		})
	}
}

func TestEventsContainKindResourceRequiresBoth(t *testing.T) {
	result := map[string]any{"events": []any{
		map[string]any{"kind": "acquired", "resources": []any{"coordination:one"}},
		map[string]any{"kind": "exec-started", "resources": []any{"coordination:stuck"}},
	}}
	if !eventsContainKindResource(result, "exec-started", "coordination:stuck") {
		t.Fatal("matching event was not found")
	}
	if eventsContainKindResource(result, "released", "coordination:stuck") || eventsContainKindResource(result, "exec-started", "coordination:one") {
		t.Fatal("event kind and resource were not jointly required")
	}
}

func TestAgeRetentionFixtureAgesEveryRetentionClock(t *testing.T) {
	root := t.TempDir()
	if err := writeAcceptanceOwnerMarker(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "authority"), 0o700); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(root, "authority", "worklease.db")
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE claims(acquired_at INTEGER,heartbeat_at INTEGER,expires_at INTEGER)`,
		`CREATE TABLE epochs(acquired_at INTEGER,ended_at INTEGER,ended_recorded_at INTEGER)`,
		`CREATE TABLE operations(request_not_after INTEGER,started_at INTEGER,completed_at INTEGER)`,
		`CREATE TABLE reconciliations(recorded_at INTEGER)`,
		`CREATE TABLE events(at INTEGER)`,
		`CREATE TABLE invites(issued_at INTEGER,request_not_after INTEGER,expires_at INTEGER,used_at INTEGER,revoked_at INTEGER)`,
		`CREATE TABLE invite_redemptions(redeemed_at INTEGER,request_not_after INTEGER,replay_until INTEGER)`,
		`CREATE TABLE admin_operation_replays(request_not_after INTEGER)`,
		`INSERT INTO claims VALUES(1,2,3)`, `INSERT INTO epochs VALUES(1,2,3)`,
		`INSERT INTO operations VALUES(1,2,3)`, `INSERT INTO reconciliations VALUES(1)`, `INSERT INTO events VALUES(1)`,
		`INSERT INTO invites VALUES(1,2,3,4,5)`, `INSERT INTO invite_redemptions VALUES(1,2,3)`, `INSERT INTO admin_operation_replays VALUES(1)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	const aged = int64(42)
	if err := ageRetentionFixture(database, aged); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, query := range []string{
		`SELECT acquired_at FROM claims`, `SELECT heartbeat_at FROM claims`, `SELECT expires_at FROM claims`,
		`SELECT acquired_at FROM epochs`, `SELECT ended_at FROM epochs`, `SELECT ended_recorded_at FROM epochs`,
		`SELECT request_not_after FROM operations`, `SELECT started_at FROM operations`, `SELECT completed_at FROM operations`,
		`SELECT recorded_at FROM reconciliations`, `SELECT at FROM events`,
		`SELECT issued_at FROM invites`, `SELECT request_not_after-2 FROM invites`, `SELECT expires_at-1 FROM invites`,
		`SELECT used_at FROM invites`, `SELECT revoked_at FROM invites`,
		`SELECT redeemed_at FROM invite_redemptions`, `SELECT request_not_after-2 FROM invite_redemptions`, `SELECT replay_until-1 FROM invite_redemptions`,
		`SELECT request_not_after FROM admin_operation_replays`,
	} {
		var got int64
		if err := db.QueryRow(query).Scan(&got); err != nil || got != aged {
			t.Fatalf("%s: got=%d err=%v", query, got, err)
		}
	}
}

func TestFullVolumeFixtureFailsAndClearsDeterministically(t *testing.T) {
	root := t.TempDir()
	if err := writeAcceptanceOwnerMarker(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "authority"), 0o700); err != nil {
		t.Fatal(err)
	}
	database := filepath.Join(root, "authority", "worklease.db")
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`CREATE TABLE admin_operation_replays(value TEXT)`,
		`CREATE TABLE meta(key TEXT PRIMARY KEY,value TEXT)`,
		`INSERT INTO meta VALUES('last_observed_at','1'),('last_event_seq','2'),('pruned_through_seq','3')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	state, err := installFullVolumeFixture(database)
	if err != nil {
		t.Fatal(err)
	}
	if state.PageCount != state.MaxPageCount {
		t.Fatalf("fixture is not full: %+v", state)
	}
	db, err = sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	var applied int64
	if err := db.QueryRow(`PRAGMA max_page_count=` + strconv.FormatInt(state.MaxPageCount, 10)).Scan(&applied); err != nil || applied != state.MaxPageCount {
		t.Fatalf("apply fixture page limit: applied=%d err=%v", applied, err)
	}
	_, insertErr := db.Exec(`INSERT INTO admin_operation_replays VALUES('blocked')`)
	if closeErr := db.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if !isSQLiteFull(insertErr) {
		t.Fatalf("fixture insert error=%v, want SQLITE_FULL", insertErr)
	}
	if err := clearFullVolumeFixture(database); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO admin_operation_replays VALUES('allowed')`); err != nil {
		t.Fatalf("insert after fixture cleanup: %v", err)
	}
}

func TestAgeRetentionFixtureRejectsUnownedDatabase(t *testing.T) {
	database := filepath.Join(t.TempDir(), "worklease.db")
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE claims(expires_at INTEGER); INSERT INTO claims VALUES(9)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ageRetentionFixture(database, 42); err == nil {
		t.Fatal("unowned database was accepted")
	}
	db, err = sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got int64
	if err := db.QueryRow(`SELECT expires_at FROM claims`).Scan(&got); err != nil || got != 9 {
		t.Fatalf("unowned database changed: got=%d err=%v", got, err)
	}
}

func TestVerifyReplayExpiredResponseRequiresMatchingRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fault.log")
	requestID := strings.Repeat("a", 32)
	valid := "path=/v1/enroll status=409 requestSha256=hash requestId=" + requestID + " reason=replay-expired at=now\n"
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyReplayExpiredResponse(path, requestID); err != nil {
		t.Fatal(err)
	}
	if err := verifyReplayExpiredResponse(path, strings.Repeat("b", 32)); err == nil {
		t.Fatal("wrong replay request accepted")
	}
}

func TestVerifyStorageFailureResponseRequiresServerReason(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fault.log")
	valid := "path=/v1/admin/gc status=503 requestSha256=hash dropped=false reason=storage-failure at=now\n"
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyStorageFailureResponse(path); err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string]string{
		"wrong path":   strings.ReplaceAll(valid, "/v1/admin/gc", "/v1/claims/acquire"),
		"wrong status": strings.ReplaceAll(valid, "status=503", "status=500"),
		"wrong reason": strings.ReplaceAll(valid, "storage-failure", "internal"),
		"duplicated":   valid + valid,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := verifyStorageFailureResponse(path); err == nil {
				t.Fatal("invalid storage-failure evidence accepted")
			}
		})
	}
}

func TestVerifyDroppedWatchRequiresExactlyOneSuccessfulDrop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fault.log")
	valid := "path=/v1/watch phase=forwarded requestSha256=hash at=now\n" +
		"path=/v1/watch status=200 requestSha256=hash dropped=true responseDelay=0s at=now\n"
	if err := os.WriteFile(path, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyDroppedWatch(path); err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string]string{
		"missing":    strings.ReplaceAll(valid, "dropped=true", "dropped=false"),
		"failed":     strings.ReplaceAll(valid, "status=200", "status=422"),
		"duplicated": valid + valid,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := verifyDroppedWatch(path); err == nil {
				t.Fatal("invalid disconnect evidence accepted")
			}
		})
	}
}

func TestValidateSSHHostRejectsShellSyntax(t *testing.T) {
	for _, host := range []string{"", "-oProxyCommand=bad", "remote.example;touch /tmp/pwned", "remote.example\nother"} {
		if err := validateSSHHost(host); err == nil {
			t.Errorf("validateSSHHost(%q) accepted unsafe host", host)
		}
	}
	if err := validateSSHHost("remote.example"); err != nil {
		t.Fatalf("validateSSHHost(remote.example): %v", err)
	}
}

func TestVerifyFaultReplaysRequiresMatchingBody(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "fault.log")
	log := "path=/v1/operations/begin status=200 requestSha256=aaa dropped=true at=now\n" +
		"path=/v1/operations/begin status=422 requestSha256=aaa dropped=false at=now\n"
	if err := os.WriteFile(logPath, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFaultReplays(logPath, []string{"/v1/operations/begin"}); err != nil {
		t.Fatal(err)
	}
	if err := verifyFaultReplays(logPath, []string{"/v1/operations/complete"}); err == nil {
		t.Fatal("missing replay accepted")
	}
}

func TestUnexpectedEnrollmentResponseErrorRedactsEnvelope(t *testing.T) {
	credential := strings.Repeat("a", 64)
	err := unexpectedEnrollmentResponseError(500, map[string]any{"error": map[string]any{"reason": "internal", "message": credential}})
	if strings.Contains(err.Error(), credential) || err.Error() != `mismatched enrollment status=500 reason="internal"` {
		t.Fatalf("unsafe mismatch error: %v", err)
	}
}

func TestRequireSecretValuesAbsentScansLogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authority.log")
	secret := []byte(strings.Repeat("b", 64))
	if err := os.WriteFile(path, append([]byte("echoed="), secret...), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := requireSecretValuesAbsent([]string{path}, secret); err == nil {
		t.Fatal("echoed credential was not detected")
	}
}

func TestVerifyExactFaultReplayRequiresOneDroppedIdenticalReplay(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "fault.log")
	requestID := strings.Repeat("a", 32)
	valid := "path=/v1/enroll status=200 requestSha256=hash dropped=true historicalResultSha256=result requestId=" + requestID + "\n" +
		"path=/v1/enroll status=200 requestSha256=hash dropped=false historicalResultSha256=result requestId=" + requestID + "\n"
	if err := os.WriteFile(logPath, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyExactFaultReplay(logPath, "/v1/enroll", requestID); err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string]string{
		"changed body":   strings.Replace(valid, "requestSha256=hash dropped=false", "requestSha256=other dropped=false", 1),
		"changed result": strings.Replace(valid, "historicalResultSha256=result requestId="+requestID+"\n", "historicalResultSha256=other requestId="+requestID+"\n", 1),
		"not dropped":    strings.Replace(valid, "dropped=true", "dropped=false", 1),
		"extra replay":   valid + "path=/v1/enroll status=200 requestSha256=hash dropped=false historicalResultSha256=result requestId=" + requestID + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(logPath, []byte(invalid), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := verifyExactFaultReplay(logPath, "/v1/enroll", requestID); err == nil {
				t.Fatal("invalid replay evidence accepted")
			}
		})
	}
}

func TestVerifyDroppedFaultCommitRequiresOneCommittedDispatch(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "fault.log")
	requestID := strings.Repeat("b", 32)
	valid := "path=/v1/enroll status=200 requestSha256=body dropped=true historicalResultSha256=result requestId=" + requestID + "\n"
	if err := os.WriteFile(logPath, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyDroppedFaultCommit(logPath, "/v1/enroll", requestID, "body"); err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string]string{
		"wrong body":    valid,
		"not dropped":   strings.Replace(valid, "dropped=true", "dropped=false", 1),
		"not committed": strings.Replace(valid, "status=200", "status=500", 1),
		"duplicate":     valid + valid,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(logPath, []byte(invalid), 0o600); err != nil {
				t.Fatal(err)
			}
			expectedHash := "body"
			if name == "wrong body" {
				expectedHash = "other"
			}
			if err := verifyDroppedFaultCommit(logPath, "/v1/enroll", requestID, expectedHash); err == nil {
				t.Fatal("invalid dropped commit evidence accepted")
			}
		})
	}
}

func TestVerifyFreshReplayEnvelopeRequiresStableResultAndNewerTime(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "fault.log")
	log := "path=/v1/operations/complete status=200 requestSha256=aaa dropped=true authorityId=authority restoreId=restore authorityTime=2026-09-14T12:00:00Z historicalResultSha256=result at=now\n" +
		"path=/v1/operations/complete status=200 requestSha256=aaa dropped=false authorityId=authority restoreId=restore authorityTime=2026-09-14T12:00:01Z historicalResultSha256=result at=now\n"
	if err := os.WriteFile(logPath, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFreshReplayEnvelope(logPath, "/v1/operations/complete", "authority"); err != nil {
		t.Fatal(err)
	}
	if err := verifyFreshReplayEnvelope(logPath, "/v1/operations/complete", "other"); err == nil {
		t.Fatal("wrong authority identity accepted")
	}
}

func TestHistoricalResultHashIgnoresReplayMarker(t *testing.T) {
	first := historicalResultHash([]byte(`{"operationId":"abc","idempotent":false,"result":{"exitStatus":0}}`))
	replay := historicalResultHash([]byte(`{"result":{"exitStatus":0},"idempotent":true,"operationId":"abc"}`))
	if first == "" || first != replay {
		t.Fatalf("historical hashes differ: first=%q replay=%q", first, replay)
	}
}

func TestVerifyLateAcknowledgmentEvidenceRequiresExactReplayAndLateResponse(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "fault.log")
	replayed, late := strings.Repeat("1", 32), strings.Repeat("6", 32)
	log := "path=/v1/operations/begin status=200 requestSha256=aaa dropped=true responseDelay=0s requestId=" + replayed + " at=now\n" +
		"path=/v1/operations/begin status=200 requestSha256=aaa dropped=false responseDelay=0s requestId=" + replayed + " at=now\n" +
		"path=/v1/operations/begin status=200 requestSha256=bbb dropped=false responseDelay=1.7s requestId=" + late + " at=now\n"
	if err := os.WriteFile(logPath, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyLateAcknowledgmentEvidence(logPath, replayed, late, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	for name, invalidLine := range map[string]string{
		"missing":             "",
		"application failure": "path=/v1/operations/begin status=422 requestSha256=bbb dropped=false responseDelay=1.7s requestId=" + late + " at=now\n",
		"lost response":       "path=/v1/operations/begin status=200 requestSha256=bbb dropped=true responseDelay=1.7s requestId=" + late + " at=now\n",
	} {
		t.Run(name, func(t *testing.T) {
			invalid := strings.Join(strings.Split(log, "\n")[:2], "\n") + "\n" + invalidLine
			if err := os.WriteFile(logPath, []byte(invalid), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := verifyLateAcknowledgmentEvidence(logPath, replayed, late, 2*time.Second); err == nil {
				t.Fatal("invalid late acknowledgment accepted")
			}
		})
	}
}

func TestWriteProviderSubmissionRefusesDuplicateDispatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider-submitted.txt")
	effectID := strings.Repeat("7", 32)
	if err := writeProviderSubmission(path, effectID); err != nil {
		t.Fatal(err)
	}
	if err := writeProviderSubmission(path, effectID); err == nil {
		t.Fatal("duplicate provider submission accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "provider-submitted "+effectID+"\n" {
		t.Fatalf("submission evidence=%q err=%v", data, err)
	}
}

func TestVerifyProviderEffectLogRequiresExactlyOneCompletionAfterReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider-completed.log")
	effectID := strings.Repeat("7", 32)
	receipt := time.Now().UTC()
	completed := receipt.Add(time.Second)
	line := "provider-completed " + effectID + " at=" + completed.Format(time.RFC3339Nano) + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	observed, err := verifyProviderEffectLog(path, effectID, receipt)
	if err != nil || !observed.Equal(completed) {
		t.Fatalf("observed=%s err=%v", observed, err)
	}
	if err := os.WriteFile(path, []byte(line+line), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyProviderEffectLog(path, effectID, receipt); err == nil {
		t.Fatal("duplicate provider completion accepted")
	}
	if err := os.WriteFile(path, []byte("provider-completed "+effectID+" at="+receipt.Add(-time.Second).Format(time.RFC3339Nano)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyProviderEffectLog(path, effectID, receipt); err == nil {
		t.Fatal("provider completion before receipt accepted")
	}
}

func TestVerifyClockBoundEvidenceRequiresLowerBoundAndNoExpiredDispatch(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "fault.log")
	generated, expired, late := strings.Repeat("4", 32), strings.Repeat("5", 32), strings.Repeat("6", 32)
	log := "path=/.well-known/worklease status=200 requestSha256=aaa dropped=false responseDelay=1.2s authorityId=authority restoreId=restore authorityTime=2026-09-14T12:00:00Z historicalResultSha256=result at=2026-09-14T13:00:01.2Z\n" +
		"path=/v1/claims/heartbeat status=200 requestSha256=bbb dropped=false responseDelay=0s requestId=" + generated + " requestNotAfter=2026-09-15T12:00:00.1Z authorityId=authority restoreId=restore authorityTime=2026-09-14T12:00:01Z historicalResultSha256=result at=now\n" +
		"path=/v1/operations/begin status=200 requestSha256=ccc dropped=false responseDelay=2.5s requestId=" + late + " requestNotAfter=2026-09-15T12:00:02Z authorityId=authority restoreId=restore authorityTime=2026-09-14T12:00:02Z historicalResultSha256=result at=now\n" +
		"path=/.well-known/worklease phase=forwarded requestSha256=ddd at=2026-09-14T13:00:03Z\n"
	if err := os.WriteFile(logPath, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyClockBoundEvidence(logPath, generated, expired, late, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte(log+"path=/v1/claims/heartbeat requestId="+expired+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyClockBoundEvidence(logPath, generated, expired, late, 2*time.Second); err == nil {
		t.Fatal("expired request dispatch accepted")
	}
}

func TestVerifyNoFaultDispatchRejectsMatchingPath(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "fault.log")
	if err := os.WriteFile(logPath, []byte("path=/v1/admin/gc status=200\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyNoFaultDispatch(logPath, "/v1/claims/acquire"); err != nil {
		t.Fatal(err)
	}
	if err := verifyNoFaultDispatch(logPath, "/v1/admin/gc"); err == nil {
		t.Fatal("matching authority dispatch accepted")
	}
}

func TestWithBlockedPendingRootRestoresDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pending")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "retained")
	if err := os.WriteFile(marker, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := withBlockedPendingRoot(root, func() error {
		info, err := os.Stat(root)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("pending root was not blocked by a regular file")
		}
		return errors.New("injected callback failure")
	}); err == nil || err.Error() != "injected callback failure" {
		t.Fatalf("callback error not preserved: %v", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "ok" {
		t.Fatalf("pending root was not restored: data=%q err=%v", data, err)
	}
}

func TestVerifyFaultGatesRequiresMatchedRequestHashes(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "fault.log")
	log := "path=/v1/claims/acquire phase=held requestSha256=aaa at=now\n" +
		"path=/v1/claims/acquire phase=released requestSha256=aaa at=now\n" +
		"path=/v1/claims/acquire phase=held requestSha256=bbb at=now\n" +
		"path=/v1/claims/acquire phase=released requestSha256=bbb at=now\n"
	if err := os.WriteFile(logPath, []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyFaultGates(logPath, "/v1/claims/acquire", 2); err != nil {
		t.Fatal(err)
	}
	if err := verifyFaultGates(logPath, "/v1/claims/acquire", 1); err == nil {
		t.Fatal("wrong gate count accepted")
	}
}

func TestFaultProxyConsumesBoundedResponseFault(t *testing.T) {
	dir := t.TempDir()
	control := filepath.Join(dir, "control")
	handler := &faultProxyHandler{control: control, log: filepath.Join(dir, "fault.log")}
	if err := os.WriteFile(control, []byte("delay-response /v1/test 25ms -1h\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	delay, offset := handler.consumeResponseFault("/v1/test", "request-hash")
	if delay != 25*time.Millisecond || offset != -time.Hour {
		t.Fatalf("delay=%s offset=%s", delay, offset)
	}
	delay, offset = handler.consumeResponseFault("/v1/test", "request-hash")
	if delay != 0 || offset != 0 {
		t.Fatalf("response fault was not one-shot: delay=%s offset=%s", delay, offset)
	}
}

func TestFaultProxyHoldsBeforeForwardingUntilReleased(t *testing.T) {
	dir := t.TempDir()
	control := filepath.Join(dir, "control")
	handler := &faultProxyHandler{control: control, log: filepath.Join(dir, "fault.log")}
	if err := os.WriteFile(control, []byte("hold /v1/claims/acquire\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- handler.waitIfHeld("/v1/claims/acquire", "request-hash") }()
	deadline := time.Now().Add(time.Second)
	for {
		data, err := os.ReadFile(control)
		if err == nil && string(data) == "held /v1/claims/acquire\n" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("proxy did not report held request")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatalf("held request returned before release: %v", err)
	default:
	}
	if err := os.WriteFile(control, []byte("release /v1/claims/acquire\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("proxy did not release held request")
	}
}

func TestWriteCertificateIncludesRemoteAddressSAN(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	if err := writeCertificate(certPath, keyPath, net.ParseIP("192.0.2.10"), "remote.example"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		t.Fatal("certificate file contained no PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := cert.VerifyHostname("192.0.2.10"); err != nil {
		t.Fatalf("certificate SANs do not include remote address: %v", cert.IPAddresses)
	}
}
