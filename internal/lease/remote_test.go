package lease

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
	"github.com/brettinternet/worklease/internal/testkit"
)

func openRemoteLeaseTest(t *testing.T) (*Service, *store.Store, *testkit.Clock, RemoteActor) {
	t.Helper()
	st, err := store.Open(context.Background(), t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	actor := RemoteActor{InstallationID: strings.Repeat("9", 32), AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID(), Credential: strings.Repeat("9", 64)}
	insertInstallation(t, st, actor.InstallationID, "admin", st.RestoreID())
	clock := testkit.NewClock(time.Now().UTC().Truncate(time.Microsecond))
	svc, err := NewRemote(st, clock, &testIDs{}, Defaults{TTL: 5 * time.Second}, RemotePolicy{Prefixes: []string{"github:", "coordination:generic:"}, MaxTTL: 10 * time.Second, MaxHold: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	return svc, st, clock, actor
}

func insertInstallation(t *testing.T, st *store.Store, id, role, restore string) {
	t.Helper()
	err := st.Write(context.Background(), func(tx *store.Tx) error {
		_, err := tx.ExecContext(context.Background(), `INSERT INTO installations(installation_id,credential_hash,role,label,enrolled_at,restore_id,enrolled_by_invite_id,request_id,request_hash) VALUES(?,?,?,?,?,?,?,?,?)`, id, hashToken(strings.Repeat(id[:1], 64)), role, "test", time.Now().UnixMicro(), restore, "invite-"+id, "request-"+id, "hash-"+id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func remoteAcquire(st *store.Store, clock *testkit.Clock, actor RemoteActor, digit string) AcquireRequest {
	return AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: strings.Repeat(digit, 32), Token: strings.Repeat(digit, 64), Resources: []string{"github:org/repo#42"}, AgentID: "agent", SessionID: "session", TTL: 5 * time.Second, MaxHold: 30 * time.Second, RequestNotAfter: clock.Now().Add(time.Hour), Actor: &actor}
}

func TestRemoteBlankActorInstallationCanonicalizesMutationHashes(t *testing.T) {
	svc, st, clock, actor := openRemoteLeaseTest(t)
	blank := actor
	blank.InstallationID = ""
	request := remoteAcquire(st, clock, blank, "1")
	grant, err := svc.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	creds := Credentials{AuthorityID: st.AuthorityID(), ClaimID: grant.ClaimID, Token: request.Token, Revision: grant.Revision, Actor: &blank}
	intent := OperationIntent{OperationID: strings.Repeat("2", 32), Kind: "exec", Request: map[string]any{"argv": []string{"true"}}, TTL: 5 * time.Second, RequestNotAfter: clock.Now().Add(time.Hour)}
	started, err := svc.BeginOperation(context.Background(), creds, intent)
	if err != nil {
		t.Fatal(err)
	}
	if started.RequestHash == "" {
		t.Fatal("remote operation hash is empty")
	}
	creds.Revision = started.Revision
	done, err := svc.CompleteOperation(context.Background(), creds, intent.OperationID, map[string]any{"exitStatus": 0})
	if err != nil {
		t.Fatal(err)
	}
	creds.Revision = done.Revision
	replayed, err := svc.BeginOperation(context.Background(), creds, intent)
	if err != nil || !replayed.Completed || replayed.RequestHash != started.RequestHash {
		t.Fatalf("begin replay=%+v err=%v", replayed, err)
	}
	released, err := svc.Release(context.Background(), creds, ReleaseRequest{OperationID: strings.Repeat("3", 32), RequestNotAfter: clock.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if released.RequestHash == "" {
		t.Fatal("remote release hash is empty")
	}
	if err := st.Write(context.Background(), func(tx *store.Tx) error {
		var installation string
		if err := tx.QueryRowContext(context.Background(), `SELECT installation_id FROM operations WHERE claim_id=? AND operation_id=?`, grant.ClaimID, intent.OperationID).Scan(&installation); err != nil {
			return err
		}
		if installation != actor.InstallationID {
			return fmt.Errorf("operation installation=%q, want %q", installation, actor.InstallationID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	secondRequest := remoteAcquire(st, clock, blank, "4")
	second, err := svc.Acquire(context.Background(), secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	transferred, err := svc.Transfer(context.Background(), Credentials{AuthorityID: st.AuthorityID(), ClaimID: second.ClaimID, Token: secondRequest.Token, Revision: second.Revision, Actor: &blank}, TransferRequest{OperationID: strings.Repeat("6", 32), SuccessorClaimID: strings.Repeat("5", 32), SuccessorToken: strings.Repeat("5", 64), ToAgent: "next", ToSession: "next-session", RequestNotAfter: clock.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if transferred.ClaimID != strings.Repeat("5", 32) || transferred.Receipt.RequestHash == "" {
		t.Fatalf("transfer=%+v", transferred)
	}
	if err := st.Write(context.Background(), func(tx *store.Tx) error {
		var installation string
		if err := tx.QueryRowContext(context.Background(), `SELECT installation_id FROM operations WHERE claim_id=? AND operation_id=?`, second.ClaimID, strings.Repeat("6", 32)).Scan(&installation); err != nil {
			return err
		}
		if installation != actor.InstallationID {
			return fmt.Errorf("transfer installation=%q, want %q", installation, actor.InstallationID)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Release(context.Background(), Credentials{AuthorityID: st.AuthorityID(), ClaimID: transferred.ClaimID, Token: strings.Repeat("5", 64), Revision: transferred.Revision, Actor: &blank}, ReleaseRequest{OperationID: strings.Repeat("7", 32), RequestNotAfter: clock.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteAcquireBindsActorIncarnationAdmissionAndPersistedLimits(t *testing.T) {
	svc, st, clock, actor := openRemoteLeaseTest(t)
	wrong := actor
	wrong.ExpectedRestoreID = strings.Repeat("8", 32)
	request := remoteAcquire(st, clock, wrong, "1")
	if _, err := svc.Acquire(context.Background(), request); !isReason(err, reason.ReasonAuthorityRestored) {
		t.Fatalf("wrong restore=%v", err)
	}

	request = remoteAcquire(st, clock, actor, "1")
	request.Resources = []string{"path:/tmp/local"}
	if _, err := svc.Acquire(context.Background(), request); !isReason(err, reason.ReasonResourceNotEnrolled) {
		t.Fatalf("host-local=%v", err)
	}
	request.Resources = []string{"github:org/repo#42"}
	request.TTL = 11 * time.Second
	if _, err := svc.Acquire(context.Background(), request); !isReason(err, reason.ReasonInvalidArgument) {
		t.Fatalf("ttl bound=%v", err)
	}
	request.TTL = 5 * time.Second
	request.MaxHold = 61 * time.Second
	if _, err := svc.Acquire(context.Background(), request); !isReason(err, reason.ReasonInvalidArgument) {
		t.Fatalf("hold bound=%v", err)
	}
	request.MaxHold = 30 * time.Second
	grant, err := svc.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if grant.InstallationID != actor.InstallationID || grant.RestoreID != st.RestoreID() {
		t.Fatalf("grant provenance=%+v", grant)
	}
	wrapped := WrapRemoteResponse(svc, grant)
	if wrapped.AuthorityID != st.AuthorityID() || wrapped.RestoreID != st.RestoreID() || !wrapped.AuthorityTime.Equal(clock.Now()) || wrapped.Result.ClaimID != grant.ClaimID {
		t.Fatalf("response envelope=%+v", wrapped)
	}
	var admittedTTL, admittedHold int64
	var installation, restore string
	var remote int
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT admitted_ttl_us,admitted_hold_until,installation_id,restore_id,remote FROM claims WHERE claim_id=?`, grant.ClaimID).Scan(&admittedTTL, &admittedHold, &installation, &restore, &remote)
	}); err != nil {
		t.Fatal(err)
	}
	if admittedTTL != (10*time.Second).Microseconds() || admittedHold != grant.AcquiredAt.Add(30*time.Second).UnixMicro() || installation != actor.InstallationID || restore != st.RestoreID() || remote != 1 {
		t.Fatalf("persisted ttl=%d hold=%d installation=%q restore=%q remote=%d", admittedTTL, admittedHold, installation, restore, remote)
	}

	changed, err := NewRemote(st, clock, nil, Defaults{TTL: 5 * time.Second}, RemotePolicy{Prefixes: []string{"coordination:generic:"}, MaxTTL: 2 * time.Second, MaxHold: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := changed.Acquire(context.Background(), request)
	if err != nil || !replay.Receipt.Idempotent {
		t.Fatalf("replay after policy change=%+v err=%v", replay, err)
	}
	staleReplay := request
	staleActor := actor
	staleActor.ExpectedRestoreID = strings.Repeat("8", 32)
	staleReplay.Actor = &staleActor
	if _, err := changed.Acquire(context.Background(), staleReplay); !isReason(err, reason.ReasonAuthorityRestored) {
		t.Fatalf("incarnation must precede replay: %v", err)
	}
	creds := Credentials{AuthorityID: st.AuthorityID(), ClaimID: grant.ClaimID, Token: request.Token, Revision: grant.Revision, Actor: &actor}
	if _, err := changed.Heartbeat(context.Background(), creds, Renew{OperationID: strings.Repeat("2", 32), TTL: 9 * time.Second, RequestNotAfter: clock.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("persisted ttl should outlive config change: %v", err)
	}
	creds.Revision++
	if _, err := changed.Heartbeat(context.Background(), creds, Renew{OperationID: strings.Repeat("3", 32), TTL: 11 * time.Second, RequestNotAfter: clock.Now().Add(time.Hour)}); !isReason(err, reason.ReasonInvalidArgument) {
		t.Fatalf("persisted ttl bound=%v", err)
	}
	started, err := changed.BeginOperation(context.Background(), creds, OperationIntent{OperationID: strings.Repeat("4", 32), Kind: "exec", Request: map[string]any{"argv": []string{"true"}}, TTL: 9 * time.Second, RequestNotAfter: clock.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	renewal := RemoteOperationRenewRequest{RenewalID: strings.Repeat("5", 32), OperationID: started.OperationID, TTL: 9 * time.Second, RequestNotAfter: clock.Now().Add(time.Hour)}
	preRenewal := creds
	preRenewal.Revision = started.Revision
	firstRenewal, err := changed.RemoteRenewOperation(context.Background(), preRenewal, renewal)
	if err != nil {
		t.Fatal(err)
	}
	replayedRenewal, err := changed.RemoteRenewOperation(context.Background(), preRenewal, renewal)
	if err != nil || !replayedRenewal.Idempotent || replayedRenewal.Revision != firstRenewal.Revision {
		t.Fatalf("renewal replay=%+v err=%v", replayedRenewal, err)
	}
	renewal.TTL = 8 * time.Second
	if _, err := changed.RemoteRenewOperation(context.Background(), preRenewal, renewal); !isReason(err, reason.ReasonOperationRequestMismatch) {
		t.Fatalf("changed renewal=%v", err)
	}
	if err := st.Write(context.Background(), func(tx *store.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE installations SET revoked_at=?,revoke_reason='test' WHERE installation_id=?`, clock.Now().UnixMicro(), actor.InstallationID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	preRenewal.Actor = &staleActor
	if _, err := changed.RemoteRenewOperation(context.Background(), preRenewal, renewal); !isReason(err, reason.ReasonInstallationRevoked) {
		t.Fatalf("revocation must precede incarnation and replay: %v", err)
	}
}

func TestRemoteRecoveryReplayClosureAndStartGate(t *testing.T) {
	svc, st, clock, actor := openRemoteLeaseTest(t)
	request := remoteAcquire(st, clock, actor, "1")
	grant, err := svc.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	creds := Credentials{AuthorityID: st.AuthorityID(), ClaimID: grant.ClaimID, Token: request.Token, Revision: grant.Revision, Actor: &actor}
	operation := OperationIntent{OperationID: strings.Repeat("2", 32), Kind: "exec", Request: map[string]any{"argv": []string{"true"}}, TTL: 5 * time.Second, RequestNotAfter: clock.Now().Add(time.Hour)}
	_, err = svc.BeginOperation(context.Background(), creds, operation)
	if err != nil {
		t.Fatal(err)
	}
	idleRequest := remoteAcquire(st, clock, actor, "4")
	idleRequest.Resources = []string{"github:org/repo#idle"}
	idleGrant, err := svc.Acquire(context.Background(), idleRequest)
	if err != nil {
		t.Fatal(err)
	}
	idleCreds := Credentials{AuthorityID: st.AuthorityID(), ClaimID: idleGrant.ClaimID, Token: idleRequest.Token, Revision: idleGrant.Revision, Actor: &actor}
	if err := st.Write(context.Background(), func(tx *store.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE recovery_state SET recovery_mode=1,recovery_revision=1,restored_at=? WHERE singleton=1`, clock.Now().UnixMicro())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BeginOperation(context.Background(), creds, operation); !isReason(err, reason.ReasonUnknownOutcome) {
		t.Fatalf("started replay precedence=%v", err)
	}
	fresh := operation
	fresh.OperationID = strings.Repeat("3", 32)
	if _, err := svc.BeginOperation(context.Background(), idleCreds, fresh); !isReason(err, reason.ReasonRecoveryClosed) {
		t.Fatalf("recovery start=%v", err)
	}
	clock.Advance(6 * time.Second)
	unrelated := remoteAcquire(st, clock, actor, "6")
	unrelated.Resources = []string{"github:org/repo#unrelated"}
	if _, err := svc.Acquire(context.Background(), unrelated); !isReason(err, reason.ReasonRecoveryRequired) {
		t.Fatalf("unrelated recovery acquire=%v", err)
	}
	covering := remoteAcquire(st, clock, actor, "5")
	recovered, err := svc.Acquire(context.Background(), covering)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(recovered.UnknownOperations) != fmt.Sprintf("[%s]", operation.OperationID) {
		t.Fatalf("unknown=%v", recovered.UnknownOperations)
	}
}

func TestRemoteRevocationAndEvidenceValidatedReopening(t *testing.T) {
	svc, st, clock, actor := openRemoteLeaseTest(t)
	request := remoteAcquire(st, clock, actor, "1")
	grant, err := svc.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	creds := Credentials{AuthorityID: st.AuthorityID(), ClaimID: grant.ClaimID, Token: request.Token, Revision: grant.Revision, Actor: &actor}
	started, err := svc.BeginOperation(context.Background(), creds, OperationIntent{OperationID: strings.Repeat("2", 32), Kind: "exec", Request: map[string]any{"argv": []string{"true"}}, TTL: 5 * time.Second, RequestNotAfter: clock.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := svc.RevokeClaim(context.Background(), actor, RevokeClaimRequest{OperationID: strings.Repeat("3", 32), ClaimID: grant.ClaimID, Reason: "compromised", RequestNotAfter: clock.Now().Add(time.Hour)})
	if err != nil || revoked.Result["reason"] != "revoked" {
		t.Fatalf("revoked=%+v err=%v", revoked, err)
	}
	var endReason, operationState string
	var events int
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		if err := tx.QueryRowContext(context.Background(), `SELECT end_reason FROM epochs WHERE claim_id=?`, grant.ClaimID).Scan(&endReason); err != nil {
			return err
		}
		if err := tx.QueryRowContext(context.Background(), `SELECT state FROM operations WHERE claim_id=? AND operation_id=?`, grant.ClaimID, started.OperationID).Scan(&operationState); err != nil {
			return err
		}
		return tx.QueryRowContext(context.Background(), `SELECT count(*) FROM events WHERE claim_id=? AND kind='revoked'`, grant.ClaimID).Scan(&events)
	}); err != nil {
		t.Fatal(err)
	}
	if endReason != "revoked" || operationState != "started" || events != 1 {
		t.Fatalf("end=%q operation=%q events=%d", endReason, operationState, events)
	}
	successorRequest := remoteAcquire(st, clock, actor, "4")
	successor, err := svc.Acquire(context.Background(), successorRequest)
	if err != nil || len(successor.UnknownOperations) != 1 {
		t.Fatalf("successor=%+v err=%v", successor, err)
	}
	successorCreds := Credentials{AuthorityID: st.AuthorityID(), ClaimID: successor.ClaimID, Token: successorRequest.Token, Revision: successor.Revision, Actor: &actor}
	_, err = svc.Reconcile(context.Background(), successorCreds, ReconcileRequest{OperationID: strings.Repeat("5", 32), TargetClaimID: grant.ClaimID, TargetOperationID: started.OperationID, ExpectedRequestSHA256: started.RequestHash, Outcome: "observed-failure", Evidence: json.RawMessage(`{"executorStopped":true,"outcome":"observed-failure"}`), TTL: 5 * time.Second, RequestNotAfter: clock.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	if err := st.Write(context.Background(), func(tx *store.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE recovery_state SET recovery_mode=1,recovery_revision=4,restored_at=?,cutoff_known=0,loss_start_known=0,loss_end_known=0,coverage_gaps='["quiet-completed-gap"]' WHERE singleton=1`, clock.Now().UnixMicro())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	reopen := ReopenRequest{OperationID: strings.Repeat("6", 32), RequestNotAfter: clock.Now().Add(time.Hour), ExpectedRecoveryRevision: 4, Attestation: ReopenAttestation{InventoryComplete: true, PendingSetsComplete: true, RetainedOutcomesComplete: true, NamespaceCessationEstablished: false, EvidenceReferences: []string{"private://cessation"}}}
	if _, err := svc.ReopenRecovery(context.Background(), actor, reopen); !isReason(err, reason.ReasonRecoveryRequired) {
		t.Fatalf("incomplete reopen=%v", err)
	}
	status, err := svc.RemoteRecoveryStatus(context.Background(), actor)
	if err != nil || !status.RecoveryMode {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	reopen.Attestation.NamespaceCessationEstablished = true
	if err := st.Write(context.Background(), func(tx *store.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE recovery_state SET cutoff_known=1 WHERE singleton=1`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReopenRecovery(context.Background(), actor, reopen); !isReason(err, reason.ReasonRecoveryRequired) {
		t.Fatalf("inconsistent cutoff reopen=%v", err)
	}
	if err := st.Write(context.Background(), func(tx *store.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE recovery_state SET cutoff_known=0 WHERE singleton=1`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	result, err := svc.ReopenRecovery(context.Background(), actor, reopen)
	if err != nil {
		t.Fatal(err)
	}
	if result.RecoveryMode || result.RecoveryRevision != 5 {
		t.Fatalf("reopen=%+v", result)
	}
	replay, err := svc.ReopenRecovery(context.Background(), actor, reopen)
	if err != nil || !replay.Idempotent {
		t.Fatalf("reopen replay=%+v err=%v", replay, err)
	}
	var attestation, gaps string
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT attestation,coverage_gaps FROM recovery_reopenings WHERE operation_id=?`, reopen.OperationID).Scan(&attestation, &gaps)
	}); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if json.Unmarshal([]byte(attestation), &decoded) != nil || decoded["namespaceCessationEstablished"] != true || gaps != `["quiet-completed-gap"]` {
		t.Fatalf("attestation=%s gaps=%s", attestation, gaps)
	}
}
