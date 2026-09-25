package lease

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

func GenerateInstallationCredential() (string, error) { return GenerateSecret() }

func TestRemoteAuthSecretsAndEnrollmentReplay(t *testing.T) {
	svc, st, clock, actor := openRemoteLeaseTest(t)
	invite, err := GenerateInviteCode()
	if err != nil {
		t.Fatal(err)
	}
	credential, err := GenerateInstallationCredential()
	if err != nil {
		t.Fatal(err)
	}
	if len(invite) != 64 || len(credential) != 64 || invite == credential {
		t.Fatalf("secret shape/entropy: %d %d", len(invite), len(credential))
	}
	deadline := clock.Now().Add(time.Hour)
	issued, err := svc.IssueInvite(context.Background(), actor, IssueInviteRequest{InviteID: strings.Repeat("1", 32), OperationID: strings.Repeat("1", 32), RequestNotAfter: deadline, Role: "write", Label: "builder", InviteSha256: HashSecret(invite)})
	if err != nil {
		t.Fatal(err)
	}
	if issued.InviteID == invite || issued.IssuedByInstallationID != actor.InstallationID {
		t.Fatalf("invite leaked or malformed: %+v", issued)
	}
	clock.Advance(time.Minute)
	reissued, err := svc.IssueInvite(context.Background(), actor, IssueInviteRequest{InviteID: strings.Repeat("1", 32), OperationID: strings.Repeat("1", 32), RequestNotAfter: deadline, Role: "write", Label: "builder", InviteSha256: HashSecret(invite)})
	if err != nil || reissued.InviteID != issued.InviteID || !reissued.ExpiresAt.Equal(issued.ExpiresAt) {
		t.Fatalf("omitted expiry replay=%+v err=%v", reissued, err)
	}
	var persistedInvite, persistedCredential string
	// Read through the store's typed transaction boundary and prove plaintext is absent.
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		if err := tx.QueryRowContext(context.Background(), `SELECT invite_hash FROM invites WHERE invite_id=?`, issued.InviteID).Scan(&persistedInvite); err != nil {
			return err
		}
		return tx.QueryRowContext(context.Background(), `SELECT credential_hash FROM installations WHERE installation_id=?`, actor.InstallationID).Scan(&persistedCredential)
	}); err != nil {
		t.Fatal(err)
	}
	if persistedInvite != HashSecret(invite) || persistedCredential != HashSecret(actor.Credential) || strings.Contains(persistedInvite, invite) || strings.Contains(persistedCredential, actor.Credential) {
		t.Fatalf("plaintext persisted")
	}

	enrolled, err := svc.Enroll(context.Background(), EnrollRequest{AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID(), RequestID: strings.Repeat("2", 32), RequestNotAfter: deadline, InstallationID: strings.Repeat("1", 31) + "1", Invite: invite, Credential: credential, Label: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT issuer_installation_id FROM installations WHERE installation_id=?`, enrolled.InstallationID).Scan(&issuer)
	}); err != nil {
		t.Fatal(err)
	}
	if issuer != actor.InstallationID {
		t.Fatalf("issuer provenance=%q", issuer)
	}
	clock.Advance(16 * time.Minute)
	replay, err := svc.Enroll(context.Background(), EnrollRequest{AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID(), RequestID: strings.Repeat("2", 32), RequestNotAfter: deadline, InstallationID: strings.Repeat("1", 31) + "1", Invite: invite, Credential: credential, Label: "worker"})
	if err != nil || replay.InstallationID != enrolled.InstallationID {
		t.Fatalf("enrollment replay=%+v err=%v", replay, err)
	}
	other, _ := GenerateInstallationCredential()
	if _, err := svc.Enroll(context.Background(), EnrollRequest{AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID(), RequestID: strings.Repeat("2", 32), RequestNotAfter: clock.Now().Add(time.Hour), InstallationID: strings.Repeat("1", 31) + "1", Invite: invite, Credential: other, Label: "worker"}); !isReason(err, reason.ReasonOperationRequestMismatch) {
		t.Fatalf("different credential=%v", err)
	}
	if removed, err := svc.GCAuthentication(context.Background()); err != nil || removed != 0 {
		t.Fatalf("early gc removed=%d err=%v", removed, err)
	}
	clock.Advance(50 * time.Minute)
	if removed, err := svc.GCAuthentication(context.Background()); err != nil || removed != 1 {
		t.Fatalf("replay gc removed=%d err=%v", removed, err)
	}
	if _, err := svc.Enroll(context.Background(), EnrollRequest{AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID(), RequestID: strings.Repeat("9", 32), RequestNotAfter: clock.Now().Add(time.Hour), InstallationID: strings.Repeat("1", 31) + "1", Invite: invite, Credential: credential, Label: "worker"}); !isReason(err, reason.ReasonInviteUsed) {
		t.Fatalf("burned invite after replay gc=%v", err)
	}
}

func TestRemoteAuthOrderingAndRevocationPreservesClaims(t *testing.T) {
	svc, st, clock, actor := openRemoteLeaseTest(t)
	blankID := actor
	blankID.InstallationID = ""
	if _, err := svc.ListInstallations(context.Background(), blankID, true); err != nil {
		t.Fatalf("derived installation ID=%v", err)
	}
	missing := actor
	missing.Credential = ""
	missing.AuthorityID = strings.Repeat("f", 32)
	if _, err := svc.ListInstallations(context.Background(), missing, true); !isReason(err, reason.ReasonAuthenticationRequired) {
		t.Fatalf("missing credential precedence=%v", err)
	}
	invite, _ := GenerateInviteCode()
	credential, _ := GenerateInstallationCredential()
	issued, err := svc.IssueInvite(context.Background(), actor, IssueInviteRequest{InviteID: strings.Repeat("3", 32), OperationID: strings.Repeat("3", 32), RequestNotAfter: clock.Now().Add(time.Hour), Role: "write", Label: "worker", InviteSha256: HashSecret(invite)})
	if err != nil {
		t.Fatal(err)
	}
	_ = issued
	enrolled, err := svc.Enroll(context.Background(), EnrollRequest{AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID(), RequestID: strings.Repeat("4", 32), RequestNotAfter: clock.Now().Add(time.Hour), InstallationID: strings.Repeat("2", 31) + "2", Invite: invite, Credential: credential, Label: "worker"})
	if err != nil {
		t.Fatal(err)
	}
	worker := RemoteActor{InstallationID: enrolled.InstallationID, Credential: credential, AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID()}
	if _, err := svc.ListInstallations(context.Background(), worker, false); !isReason(err, reason.ReasonAuthorizationDenied) {
		t.Fatalf("write administration=%v", err)
	}
	grant, err := svc.Acquire(context.Background(), AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: strings.Repeat("a", 32), Token: strings.Repeat("a", 64), Resources: []string{"github:org/repo#1"}, AgentID: "agent", SessionID: "session", WorkKey: "work", TTL: time.Second, MaxHold: time.Minute, RequestNotAfter: clock.Now().Add(time.Hour), Actor: &worker})
	if err != nil {
		t.Fatal(err)
	}
	started, err := svc.BeginOperation(context.Background(), Credentials{AuthorityID: st.AuthorityID(), ClaimID: grant.ClaimID, Token: strings.Repeat("a", 64), Revision: grant.Revision, Actor: &worker}, OperationIntent{OperationID: strings.Repeat("d", 32), Kind: "exec", Request: map[string]any{"argv": []string{"true"}}, TTL: time.Second, RequestNotAfter: clock.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	revokeDeadline := clock.Now().Add(time.Hour)
	revokeReq := RevokeInstallationRequest{InstallationID: worker.InstallationID, Reason: "rotation", OperationID: strings.Repeat("c", 32), RequestNotAfter: revokeDeadline}
	firstRevoke, err := svc.RevokeInstallation(context.Background(), actor, revokeReq)
	if err != nil {
		t.Fatal(err)
	}
	replayRevoke, err := svc.RevokeInstallation(context.Background(), actor, revokeReq)
	if err != nil || !replayRevoke.RevokedAt.Equal(firstRevoke.RevokedAt) {
		t.Fatalf("revoke replay=%+v err=%v", replayRevoke, err)
	}
	if _, err := svc.RevokeInstallation(context.Background(), actor, RevokeInstallationRequest{InstallationID: worker.InstallationID, Reason: "changed", OperationID: strings.Repeat("c", 32), RequestNotAfter: revokeDeadline}); !isReason(err, reason.ReasonOperationRequestMismatch) {
		t.Fatalf("changed revocation=%v", err)
	}
	if _, err := svc.Heartbeat(context.Background(), Credentials{AuthorityID: st.AuthorityID(), ClaimID: grant.ClaimID, Token: strings.Repeat("a", 64), Revision: grant.Revision, Actor: &worker}, Renew{OperationID: strings.Repeat("b", 32), TTL: time.Second, RequestNotAfter: clock.Now().Add(time.Hour)}); !isReason(err, reason.ReasonInstallationRevoked) {
		t.Fatalf("revocation auth=%v", err)
	}
	var claims int
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT count(*) FROM claims WHERE claim_id=?`, grant.ClaimID).Scan(&claims)
	}); err != nil {
		t.Fatal(err)
	}
	if claims != 1 {
		t.Fatalf("revocation removed claim: %d", claims)
	}
	var operationState string
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT state FROM operations WHERE claim_id=? AND operation_id=?`, grant.ClaimID, started.OperationID).Scan(&operationState)
	}); err != nil {
		t.Fatal(err)
	}
	if operationState != "started" {
		t.Fatalf("revocation removed unresolved operation: %s", operationState)
	}
	views, err := svc.ListInstallations(context.Background(), actor, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, view := range views {
		if view.InstallationID == worker.InstallationID {
			found = view.RevokedAt != nil
		}
	}
	if !found {
		t.Fatal("revoked installation was not listed with state")
	}
}
