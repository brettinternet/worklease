package lease

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/store"
)

func openHostedAdminTest(t *testing.T) (*Service, *store.Store, *store.HostedLock) {
	t.Helper()
	home := t.TempDir()
	if err := store.MarkHosted(home); err != nil {
		t.Fatal(err)
	}
	lock, err := store.AcquireHostedLock(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), home, store.Options{HostedWriter: true, HostedLock: lock})
	if err != nil {
		lock.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc, err := NewRemote(st, nil, nil, Defaults{}, RemotePolicy{Prefixes: []string{"coordination:"}, MaxTTL: time.Hour, MaxHold: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return svc, st, lock
}

func TestHostedRestoreEndsClaimsAndEntersRecovery(t *testing.T) {
	svc, st, _ := openHostedAdminTest(t)
	ctx := context.Background()
	secret := strings.Repeat("a", 64)
	first, err := svc.HostedInitialize(ctx, secret)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := svc.HostedInitialize(ctx, secret)
	if err != nil || resumed.InviteID != first.InviteID {
		t.Fatalf("bootstrap resume result=%+v err=%v", resumed, err)
	}
	credential := strings.Repeat("f", 64)
	installationID := strings.Repeat("4", 32)
	if _, err := svc.Enroll(ctx, EnrollRequest{AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID(), RequestID: strings.Repeat("5", 32), RequestNotAfter: time.Now().Add(time.Hour), InstallationID: installationID, Invite: secret, Credential: credential, Label: "admin"}); err != nil {
		t.Fatal(err)
	}
	actor := RemoteActor{InstallationID: installationID, AuthorityID: st.AuthorityID(), ExpectedRestoreID: st.RestoreID(), Credential: credential}
	ordinaryInvite, err := GenerateInviteCode()
	if err != nil {
		t.Fatal(err)
	}
	ordinaryInviteID := strings.Repeat("6", 32)
	if _, err := svc.IssueInvite(ctx, actor, IssueInviteRequest{OperationID: strings.Repeat("7", 32), RequestNotAfter: time.Now().Add(time.Hour), InviteID: ordinaryInviteID, Role: "write", Label: "worker", InviteSha256: HashSecret(ordinaryInvite)}); err != nil {
		t.Fatal(err)
	}
	claimSecret := strings.Repeat("b", 64)
	grant, err := svc.Acquire(ctx, AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: strings.Repeat("1", 32), Token: claimSecret, Resources: []string{"coordination:item"}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	operationID := strings.Repeat("8", 32)
	if _, err := svc.BeginOperation(ctx, Credentials{AuthorityID: st.AuthorityID(), ClaimID: grant.ClaimID, Token: claimSecret, Revision: grant.Revision}, OperationIntent{OperationID: operationID, Kind: "exec", Request: map[string]any{"argv": []string{"effect"}}, TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	oldRestore := st.RestoreID()
	cutoff, lossStart, lossEnd := time.Now().Add(-2*time.Minute).UTC(), time.Now().Add(-time.Minute).UTC(), time.Now().UTC()
	result, err := svc.HostedRestore(ctx, HostedRestoreRequest{SelectedCutoff: cutoff, LossStart: lossStart, LossEnd: lossEnd}, strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	if result.RestoreID == oldRestore || st.RestoreID() != result.RestoreID {
		t.Fatalf("restore id did not rotate: old=%s new=%s stored=%s", oldRestore, result.RestoreID, st.RestoreID())
	}
	var mode, cutoffKnown, startKnown, endKnown int
	var ended, restored, started, revokedInstallation, revokedInvite, activeBootstrap int
	var storedCutoff, storedStart, storedEnd int64
	if err := st.Read(ctx, func(tx *store.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT recovery_mode,selected_durable_cutoff,cutoff_known,loss_interval_start,loss_interval_end,loss_start_known,loss_end_known FROM recovery_state WHERE singleton=1`).Scan(&mode, &storedCutoff, &cutoffKnown, &storedStart, &storedEnd, &startKnown, &endKnown); err != nil {
			return err
		}
		queries := []struct {
			query string
			args  []any
			out   *int
		}{
			{`SELECT count(*) FROM claims`, nil, &ended},
			{`SELECT count(*) FROM epochs WHERE claim_id=? AND end_reason='restored'`, []any{grant.ClaimID}, &restored},
			{`SELECT count(*) FROM operations WHERE claim_id=? AND operation_id=? AND state='started'`, []any{grant.ClaimID, operationID}, &started},
			{`SELECT count(*) FROM installations WHERE installation_id=? AND revoked_at IS NOT NULL`, []any{installationID}, &revokedInstallation},
			{`SELECT count(*) FROM invites WHERE invite_id=? AND state='revoked'`, []any{ordinaryInviteID}, &revokedInvite},
			{`SELECT count(*) FROM invites WHERE bootstrap=1 AND state='active' AND restore_id=?`, []any{result.RestoreID}, &activeBootstrap},
		}
		for _, query := range queries {
			if err := tx.QueryRowContext(ctx, query.query, query.args...).Scan(query.out); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if mode != 1 || ended != 0 || restored != 1 || started != 1 || revokedInstallation != 1 || revokedInvite != 1 || activeBootstrap != 1 {
		t.Fatalf("restore state mode=%d active=%d restored=%d started=%d installation=%d invite=%d bootstrap=%d", mode, ended, restored, started, revokedInstallation, revokedInvite, activeBootstrap)
	}
	if cutoffKnown != 1 || startKnown != 1 || endKnown != 1 || storedCutoff != cutoff.Truncate(time.Microsecond).UnixMicro() || storedStart != lossStart.Truncate(time.Microsecond).UnixMicro() || storedEnd != lossEnd.Truncate(time.Microsecond).UnixMicro() {
		t.Fatalf("restore bounds cutoff=%d/%d start=%d/%d end=%d/%d known=%d,%d,%d", storedCutoff, cutoff.UnixMicro(), storedStart, lossStart.UnixMicro(), storedEnd, lossEnd.UnixMicro(), cutoffKnown, startKnown, endKnown)
	}
}

func TestHostedBootstrapReissuePreservesLifecycleState(t *testing.T) {
	svc, st, _ := openHostedAdminTest(t)
	ctx := context.Background()
	if _, err := svc.HostedInitialize(ctx, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	grant, err := svc.Acquire(ctx, AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: strings.Repeat("1", 32), Token: strings.Repeat("b", 64), Resources: []string{"coordination:item"}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	oldRestore := st.RestoreID()
	before := map[string]int{}
	if err := st.Read(ctx, func(tx *store.Tx) error {
		for _, table := range []string{"claims", "epochs", "operations", "events"} {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil {
				return err
			}
			before[table] = count
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.HostedBootstrapReissue(ctx, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	if st.RestoreID() != oldRestore {
		t.Fatal("reissue changed restore ID")
	}
	if err := st.Read(ctx, func(tx *store.Tx) error {
		for table, want := range before {
			var count int
			if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&count); err != nil {
				return err
			}
			if count != want {
				return fmt.Errorf("%s count=%d want=%d", table, count, want)
			}
		}
		var claimCount, activeBootstrap, revokedBootstrap int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM claims WHERE claim_id=?`, grant.ClaimID).Scan(&claimCount); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM invites WHERE bootstrap=1 AND state='active'`).Scan(&activeBootstrap); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM invites WHERE bootstrap=1 AND state='revoked'`).Scan(&revokedBootstrap); err != nil {
			return err
		}
		if claimCount != 1 || activeBootstrap != 1 || revokedBootstrap != 1 {
			return fmt.Errorf("claim=%d active bootstrap=%d revoked bootstrap=%d", claimCount, activeBootstrap, revokedBootstrap)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestHostedRetirementInventoryRedactsPrivateFields(t *testing.T) {
	svc, st, _ := openHostedAdminTest(t)
	ctx := context.Background()
	secret := strings.Repeat("d", 64)
	if _, err := svc.HostedInitialize(ctx, secret); err != nil {
		t.Fatal(err)
	}
	claimToken := strings.Repeat("e", 64)
	claimID := strings.Repeat("2", 32)
	grant, err := svc.Acquire(ctx, AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: claimToken, Resources: []string{"coordination:item"}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BeginOperation(ctx, Credentials{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: claimToken, Revision: grant.Revision}, OperationIntent{OperationID: strings.Repeat("3", 32), Kind: "exec", Request: map[string]any{"argv": []string{"private"}, "checkpoint": "private"}, TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	status, err := svc.HostedRetirementStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.ActiveClaims != 1 || status.Unresolved != 1 {
		t.Fatalf("status=%+v", status)
	}
	var data bytes.Buffer
	exported, err := svc.WriteHostedRetirementExport(ctx, &data)
	if err != nil {
		t.Fatal(err)
	}
	if exported != status {
		t.Fatalf("exported=%+v status=%+v", exported, status)
	}
	if strings.Contains(data.String(), "private") || strings.Contains(data.String(), "argv") || strings.Contains(data.String(), claimToken) {
		t.Fatalf("private data leaked: %s", data.String())
	}
	if lines := strings.Count(data.String(), "\n"); lines != 4 {
		t.Fatalf("export lines=%d data=%s", lines, data.String())
	}
}
