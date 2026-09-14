package lease

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

func TestInviteExplicitExpiryReplaysCanonicalTimestamp(t *testing.T) {
	svc, _, clock, actor := openRemoteLeaseTest(t)
	invite, _ := GenerateInviteCode()
	expires := clock.Now().Add(2*time.Minute + time.Nanosecond)
	req := IssueInviteRequest{OperationID: strings.Repeat("0", 32), RequestNotAfter: clock.Now().Add(time.Hour), InviteID: strings.Repeat("f", 32), Role: "read", Label: "reader", ExpiresAt: expires, InviteSha256: HashSecret(invite)}
	first, err := svc.IssueInvite(context.Background(), actor, req)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.IssueInvite(context.Background(), actor, req)
	if err != nil {
		t.Fatal(err)
	}
	want := expires.UTC().Truncate(time.Microsecond)
	if !first.ExpiresAt.Equal(want) || !replay.ExpiresAt.Equal(want) {
		t.Fatalf("expiry first=%s replay=%s want=%s", first.ExpiresAt, replay.ExpiresAt, want)
	}
}

func TestEnrollmentExpiryDoesNotBurnInviteOrInsert(t *testing.T) {
	svc, st, clock, actor := openRemoteLeaseTest(t)
	invite, _ := GenerateInviteCode()
	credential, _ := GenerateInstallationCredential()
	deadline := clock.Now().Add(time.Hour)
	_, err := svc.IssueInvite(context.Background(), actor, IssueInviteRequest{InviteID: strings.Repeat("d", 32), OperationID: strings.Repeat("d", 32), RequestNotAfter: deadline, Role: "write", Label: "short", ExpiresAt: clock.Now().Add(2 * time.Minute), InviteSha256: HashSecret(invite)})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Minute)
	req := EnrollRequest{AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID(), RequestID: strings.Repeat("e", 32), RequestNotAfter: clock.Now().Add(time.Hour), InstallationID: strings.Repeat("e", 32), Invite: invite, Credential: credential, Label: "worker"}
	if _, err := svc.Enroll(context.Background(), req); !isReason(err, reason.ReasonInviteExpired) {
		t.Fatalf("expired enrollment=%v", err)
	}
	var state string
	var installations, redemptions int
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		if err := tx.QueryRowContext(context.Background(), `SELECT state FROM invites WHERE invite_hash=?`, HashSecret(invite)).Scan(&state); err != nil {
			return err
		}
		if err := tx.QueryRowContext(context.Background(), `SELECT count(*) FROM installations WHERE installation_id=?`, req.InstallationID).Scan(&installations); err != nil {
			return err
		}
		return tx.QueryRowContext(context.Background(), `SELECT count(*) FROM invite_redemptions WHERE invite_id=(SELECT invite_id FROM invites WHERE invite_hash=?)`, HashSecret(invite)).Scan(&redemptions)
	}); err != nil {
		t.Fatal(err)
	}
	if state != "active" {
		t.Fatalf("expired invite was burned: state=%s", state)
	}
	if installations != 0 || redemptions != 0 {
		t.Fatalf("expired enrollment mutated: installations=%d redemptions=%d", installations, redemptions)
	}
}

func TestEnrollmentReplayRevocationAndMissingInstallationPrecedence(t *testing.T) {
	svc, st, clock, actor := openRemoteLeaseTest(t)
	invite, _ := GenerateInviteCode()
	credential, _ := GenerateInstallationCredential()
	_, err := svc.IssueInvite(context.Background(), actor, IssueInviteRequest{InviteID: strings.Repeat("f", 32), OperationID: strings.Repeat("f", 32), RequestNotAfter: clock.Now().Add(time.Hour), Role: "write", Label: "worker", InviteSha256: HashSecret(invite)})
	if err != nil {
		t.Fatal(err)
	}
	req := EnrollRequest{AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID(), RequestID: strings.Repeat("a", 32), RequestNotAfter: clock.Now().Add(time.Hour), InstallationID: strings.Repeat("b", 32), Invite: invite, Credential: credential, Label: "worker"}
	if _, err := svc.Enroll(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RevokeInstallation(context.Background(), actor, RevokeInstallationRequest{InstallationID: req.InstallationID, OperationID: strings.Repeat("c", 32), RequestNotAfter: clock.Now().Add(time.Hour), Reason: "rotation"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Enroll(context.Background(), req); !isReason(err, reason.ReasonInstallationRevoked) {
		t.Fatalf("revoked replay precedence=%v", err)
	}
	if err := st.Write(context.Background(), func(tx *store.Tx) error {
		_, err := tx.ExecContext(context.Background(), `DELETE FROM installations WHERE installation_id=?`, req.InstallationID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Enroll(context.Background(), req); !isReason(err, reason.ReasonAuthenticationRequired) {
		t.Fatalf("missing installation precedence=%v", err)
	}
}

func TestRemoteLifecycleRejectsLocalOnlyReplayFields(t *testing.T) {
	svc, st, clock, actor := openRemoteLeaseTest(t)
	request := remoteAcquire(st, clock, actor, "6")
	grant, err := svc.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	creds := Credentials{AuthorityID: st.AuthorityID(), ClaimID: grant.ClaimID, Token: request.Token, Revision: grant.Revision, Actor: &actor}
	renew := Renew{OperationID: strings.Repeat("7", 32), TTL: time.Second, RequestNotAfter: clock.Now().Add(time.Hour)}
	receipt, err := svc.Heartbeat(context.Background(), creds, renew)
	if err != nil {
		t.Fatal(err)
	}
	renew.TTL = 2 * time.Second
	renew.LegacyRequestHash = receipt.RequestHash
	if _, err := svc.Heartbeat(context.Background(), creds, renew); !isReason(err, reason.ReasonInvalidArgument) {
		t.Fatalf("remote legacy replay hash=%v", err)
	}
	checkpoint := CheckpointRequest{OperationID: strings.Repeat("8", 32), TTL: time.Second, Data: json.RawMessage(`{"phase":"test"}`), RequestNotAfter: clock.Now().Add(time.Hour), HoldUntil: clock.Now().Add(time.Minute)}
	if _, err := svc.Checkpoint(context.Background(), creds, checkpoint); !isReason(err, reason.ReasonInvalidArgument) {
		t.Fatalf("remote hold deadline=%v", err)
	}
}

func TestRecoveryBootstrapAndRoleMatrix(t *testing.T) {
	svc, st, clock, admin := openRemoteLeaseTest(t)
	// A bootstrap invite is bound to recovery_state and is valid for initial
	// enrollment even before recovery mode is entered.
	bootstrapInvite, _ := GenerateInviteCode()
	bootstrapCredential, _ := GenerateInstallationCredential()
	bootstrapID := strings.Repeat("1", 32)
	if err := st.Write(context.Background(), func(tx *store.Tx) error {
		_, err := tx.ExecContext(context.Background(), `INSERT INTO invites(invite_id,invite_hash,role,label,issued_at,expires_at,restore_id,issue_operation_id,issue_request_hash,request_not_after,bootstrap,state) VALUES(?,?,?,?,?,?,?,?,?,?,?, 'active')`, bootstrapID, HashSecret(bootstrapInvite), "admin", "bootstrap", clock.Now().UnixMicro(), clock.Now().Add(time.Hour).UnixMicro(), st.RestoreID(), strings.Repeat("2", 32), strings.Repeat("3", 64), clock.Now().Add(time.Hour).UnixMicro(), 1)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(context.Background(), `UPDATE recovery_state SET bootstrap_invite_id=?,bootstrap_ready=1 WHERE singleton=1`, bootstrapID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	bootstrapReq := EnrollRequest{AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID(), RequestID: strings.Repeat("4", 32), RequestNotAfter: clock.Now().Add(time.Hour), InstallationID: strings.Repeat("5", 32), Invite: bootstrapInvite, Credential: bootstrapCredential, Label: "bootstrap-admin"}
	if result, err := svc.Enroll(context.Background(), bootstrapReq); err != nil || result.Role != "admin" {
		t.Fatalf("initial bootstrap result=%+v err=%v", result, err)
	}
	ordinaryInvite, _ := GenerateInviteCode()
	ordinaryCredential, _ := GenerateInstallationCredential()
	ordinaryIssue := IssueInviteRequest{InviteID: strings.Repeat("a", 32), OperationID: strings.Repeat("a", 32), RequestNotAfter: clock.Now().Add(time.Hour), Role: "admin", Label: "ordinary", InviteSha256: HashSecret(ordinaryInvite)}
	if _, err := svc.IssueInvite(context.Background(), admin, ordinaryIssue); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(context.Background(), func(tx *store.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE recovery_state SET recovery_mode=1 WHERE singleton=1`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if replay, err := svc.IssueInvite(context.Background(), admin, ordinaryIssue); err != nil || replay.InviteID == "" {
		t.Fatalf("invite replay during recovery=%+v err=%v", replay, err)
	}
	ordinaryReq := EnrollRequest{AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID(), RequestID: strings.Repeat("b", 32), RequestNotAfter: clock.Now().Add(time.Hour), InstallationID: strings.Repeat("c", 32), Invite: ordinaryInvite, Credential: ordinaryCredential, Label: "ordinary"}
	if _, err := svc.Enroll(context.Background(), ordinaryReq); !isReason(err, reason.ReasonRecoveryClosed) {
		t.Fatalf("ordinary recovery enrollment=%v", err)
	}
	recoveryInvite, _ := GenerateInviteCode()
	recoveryCredential, _ := GenerateInstallationCredential()
	recoveryInviteID := strings.Repeat("d", 32)
	if err := st.Write(context.Background(), func(tx *store.Tx) error {
		_, err := tx.ExecContext(context.Background(), `INSERT INTO invites(invite_id,invite_hash,role,label,issued_at,expires_at,restore_id,issue_operation_id,issue_request_hash,request_not_after,bootstrap,state) VALUES(?,?,?,?,?,?,?,?,?,?,?, 'active')`, recoveryInviteID, HashSecret(recoveryInvite), "admin", "recovery-bootstrap", clock.Now().UnixMicro(), clock.Now().Add(time.Hour).UnixMicro(), st.RestoreID(), strings.Repeat("e", 32), strings.Repeat("f", 64), clock.Now().Add(time.Hour).UnixMicro(), 1)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(context.Background(), `UPDATE recovery_state SET bootstrap_invite_id=?,bootstrap_ready=1 WHERE singleton=1`, recoveryInviteID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	recoveryReq := EnrollRequest{AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID(), RequestID: strings.Repeat("1", 32), RequestNotAfter: clock.Now().Add(time.Hour), InstallationID: strings.Repeat("2", 32), Invite: recoveryInvite, Credential: recoveryCredential, Label: "recovery-admin"}
	if result, err := svc.Enroll(context.Background(), recoveryReq); err != nil || result.Role != "admin" {
		t.Fatalf("recovery bootstrap result=%+v err=%v", result, err)
	}
	if err := st.Write(context.Background(), func(tx *store.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE recovery_state SET recovery_mode=0 WHERE singleton=1`)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Role grants: admin can issue; write can mutate claims but cannot
	// administer; read can inspect but cannot mutate.
	issueInvite, _ := GenerateInviteCode()
	issueCredential, _ := GenerateInstallationCredential()
	if _, err := svc.IssueInvite(context.Background(), admin, IssueInviteRequest{InviteID: strings.Repeat("6", 32), OperationID: strings.Repeat("6", 32), RequestNotAfter: clock.Now().Add(time.Hour), Role: "read", Label: "reader", InviteSha256: HashSecret(issueInvite)}); err != nil {
		t.Fatal(err)
	}
	readReq := EnrollRequest{AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID(), RequestID: strings.Repeat("7", 32), RequestNotAfter: clock.Now().Add(time.Hour), InstallationID: strings.Repeat("8", 32), Invite: issueInvite, Credential: issueCredential, Label: "reader"}
	readResult, err := svc.Enroll(context.Background(), readReq)
	if err != nil {
		t.Fatal(err)
	}
	readActor := RemoteActor{InstallationID: readResult.InstallationID, Credential: issueCredential, AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID()}
	if _, err := svc.Status(context.Background(), Selector{AuthorityID: st.AuthorityID(), Resource: "github:org/repo#role", Actor: &readActor}); err != nil {
		t.Fatalf("read status=%v", err)
	}
	if _, err := svc.Acquire(context.Background(), AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: strings.Repeat("9", 32), Token: strings.Repeat("9", 64), Resources: []string{"github:org/repo#role"}, AgentID: "agent", SessionID: "session", TTL: time.Second, MaxHold: time.Minute, RequestNotAfter: clock.Now().Add(time.Hour), Actor: &readActor}); !isReason(err, reason.ReasonAuthorizationDenied) {
		t.Fatalf("read acquire=%v", err)
	}
	if _, err := svc.ListInstallations(context.Background(), readActor, false); !isReason(err, reason.ReasonAuthorizationDenied) {
		t.Fatalf("read administration=%v", err)
	}
	encoded, _ := json.Marshal(IssueInviteResult{InviteID: bootstrapID})
	if strings.Contains(string(encoded), bootstrapInvite) || strings.Contains(string(encoded), bootstrapCredential) {
		t.Fatal("secret appeared in result")
	}
}
