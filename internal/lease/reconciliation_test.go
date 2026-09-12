package lease

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

func TestReconcileExpiredPredecessorRequiresCoveringCurrentClaimAndIsIdempotent(t *testing.T) {
	svc, st, clock := openLeaseTest(t)
	ctx := context.Background()
	oldID, oldToken := strings.Repeat("1", 32), strings.Repeat("a", 64)
	old, err := svc.Acquire(ctx, AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: oldID, Token: oldToken, Resources: []string{"a", "b"}, AgentID: "old", SessionID: "old", TTL: time.Second, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	targetID := strings.Repeat("2", 32)
	started, err := svc.BeginOperation(ctx, credentials(st, oldID, oldToken, old.Revision), OperationIntent{OperationID: targetID, Kind: "exec", Request: map[string]any{"cwd": "/tmp"}, TTL: time.Second, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Second)
	newID, newToken := strings.Repeat("3", 32), strings.Repeat("b", 64)
	current, err := svc.Acquire(ctx, AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: newID, Token: newToken, Resources: []string{"a", "b"}, AgentID: "new", SessionID: "new", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	req := ReconcileRequest{OperationID: strings.Repeat("4", 32), TargetClaimID: oldID, TargetOperationID: targetID, ExpectedRequestSHA256: strings.ToUpper(started.RequestHash), Outcome: "observed-success", Evidence: json.RawMessage(`{"outcome":"observed-success","executorStopped":true}`), TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)}
	receipt, err := svc.Reconcile(ctx, credentials(st, newID, newToken, current.Revision), req)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Revision != current.Revision+1 || receipt.Idempotent || receipt.TargetClaimID != oldID {
		t.Fatalf("receipt=%+v", receipt)
	}
	replay, err := svc.Reconcile(ctx, credentials(st, newID, newToken, current.Revision), req)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Idempotent {
		t.Fatalf("replay=%+v", replay)
	}
	if _, err := svc.Verify(ctx, credentials(st, newID, newToken, receipt.Revision), []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
}

func TestReconcileAtCurrentRevisionAuthenticatesAndRejectsChangedReplay(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	ctx := context.Background()
	id, token := strings.Repeat("1", 32), strings.Repeat("a", 64)
	grant, err := svc.Acquire(ctx, req("a", id, token))
	if err != nil {
		t.Fatal(err)
	}
	targetID := strings.Repeat("2", 32)
	started, err := svc.BeginOperation(ctx, credentials(st, id, token, grant.Revision), OperationIntent{OperationID: targetID, Kind: "exec", Request: map[string]any{"argv": []any{"sleep", "5"}}, TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	request := ReconcileRequest{OperationID: strings.Repeat("3", 32), TargetClaimID: id, TargetOperationID: targetID, ExpectedRequestSHA256: started.RequestHash, Outcome: "observed-failure", Evidence: json.RawMessage(`{"outcome":"observed-failure","executorStopped":true}`), TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)}
	stale := credentials(st, id, token, grant.Revision)
	if _, err := svc.Reconcile(ctx, stale, request); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonStaleRevision {
		t.Fatalf("strict stale revision err=%v", err)
	}
	foreign := stale
	foreign.Token = strings.Repeat("b", 64)
	if _, err := svc.ReconcileAtCurrentRevision(ctx, foreign, request); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonInvalidToken {
		t.Fatalf("foreign credential err=%v", err)
	}
	receipt, err := svc.ReconcileAtCurrentRevision(ctx, stale, request)
	if err != nil || receipt.Revision != started.Revision+1 {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	changed := request
	changed.Evidence = json.RawMessage(`{"changed":true,"outcome":"observed-failure","executorStopped":true}`)
	if _, err := svc.ReconcileAtCurrentRevision(ctx, stale, changed); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonReconciliationConflict {
		t.Fatalf("changed replay err=%v", err)
	}
}

func TestReconcileRejectsPartialCoverageHashAndMalformedEvidence(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	ctx := context.Background()
	id, token := strings.Repeat("1", 32), strings.Repeat("a", 64)
	g, err := svc.Acquire(ctx, AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: id, Token: token, Resources: []string{"a", "b"}, AgentID: "a", SessionID: "s", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	target := strings.Repeat("2", 32)
	started, err := svc.BeginOperation(ctx, credentials(st, id, token, g.Revision), OperationIntent{OperationID: target, Kind: "exec", Request: map[string]any{"x": 1}, TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	base := ReconcileRequest{OperationID: strings.Repeat("3", 32), TargetClaimID: id, TargetOperationID: target, ExpectedRequestSHA256: started.RequestHash, Outcome: "observed-failure", Evidence: json.RawMessage(`{"outcome":"observed-failure","executorStopped":true}`), TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)}
	bad := base
	bad.ExpectedRequestSHA256 = strings.Repeat("f", 64)
	if _, err := svc.Reconcile(ctx, credentials(st, id, token, started.Revision), bad); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonExpectedHashMismatch {
		t.Fatalf("hash err=%v", err)
	}
	bad = base
	bad.Evidence = json.RawMessage(`{"outcome":"observed-failure","executorStopped":false}`)
	if _, err := svc.Reconcile(ctx, credentials(st, id, token, started.Revision), bad); reason.As(err) == nil || reason.As(err).Code != reason.ExitInvalid {
		t.Fatalf("evidence err=%v", err)
	}
	if err := st.Write(ctx, func(tx *store.Tx) error {
		_, err := tx.ExecContext(ctx, `DELETE FROM claim_resources WHERE resource='b'`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reconcile(ctx, credentials(st, id, token, started.Revision), base); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonReconciliationConflict {
		t.Fatalf("coverage err=%v", err)
	}
}

func TestReconcileStorageFailureRollsBackTargetResolverAndAudit(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	ctx := context.Background()
	id, token := strings.Repeat("1", 32), strings.Repeat("a", 64)
	g, err := svc.Acquire(ctx, req("a", id, token))
	if err != nil {
		t.Fatal(err)
	}
	target := strings.Repeat("2", 32)
	started, err := svc.BeginOperation(ctx, credentials(st, id, token, g.Revision), OperationIntent{OperationID: target, Kind: "exec", Request: map[string]any{}, TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	request := ReconcileRequest{OperationID: strings.Repeat("f", 32), TargetClaimID: id, TargetOperationID: target, ExpectedRequestSHA256: started.RequestHash, Outcome: "observed-success", Evidence: json.RawMessage(`{"outcome":"observed-success","executorStopped":true}`), TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)}
	beforeReconciliationCommit = func() error { return errors.New("injected") }
	t.Cleanup(func() { beforeReconciliationCommit = nil })
	if _, err := svc.Reconcile(ctx, credentials(st, id, token, started.Revision), request); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonStorageFailure {
		t.Fatalf("err=%v", err)
	}
	var state string
	var revision int64
	var reconciliations, events int
	if err := st.Read(ctx, func(tx *store.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT state FROM operations WHERE claim_id=? AND operation_id=?`, id, target).Scan(&state); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM claims WHERE claim_id=?`, id).Scan(&revision); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM reconciliations`).Scan(&reconciliations); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM events WHERE kind='reconciled'`).Scan(&events)
	}); err != nil {
		t.Fatal(err)
	}
	if state != "started" || revision != started.Revision || reconciliations != 0 || events != 0 {
		t.Fatalf("state=%s revision=%d reconciliations=%d events=%d", state, revision, reconciliations, events)
	}
}
