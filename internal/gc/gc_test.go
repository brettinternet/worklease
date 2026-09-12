package gc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestCollectStrictCutoffAndDryRunDoesNotMutate(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	clock := testkit.NewClock(now)
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, clock, nil, lease.Defaults{TTL: time.Minute})
	id, token := strings.Repeat("1", 32), strings.Repeat("a", 64)
	if _, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: id, Token: token, Resources: []string{"opaque"}, AgentID: "agent", SessionID: "session", TTL: time.Second, RequestNotAfter: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Second)
	before := countRows(t, st, "claims")
	beforeAuthority, beforeFS := snapshotAuthority(t, st), snapshotFilesystem(t, st.Home())
	result, err := New(st).Collect(ctx, Request{Cutoff: now.Add(2 * time.Second), Now: now.Add(2 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || result.Eligible["expiredClaims"].Count != 1 {
		t.Fatalf("result=%+v", result)
	}
	if result.Eligible["epochs"].Count != 0 {
		t.Fatalf("open epoch unexpectedly eligible: %+v", result)
	}
	if got := countRows(t, st, "claims"); got != before {
		t.Fatalf("dry run changed claims: %d != %d", got, before)
	}
	if got := watermark(t, st, "pruned_through_seq"); got != "0" {
		t.Fatalf("dry run changed watermark: %q", got)
	}
	if got := snapshotAuthority(t, st); got != beforeAuthority {
		t.Fatalf("dry run changed authority rows:\nbefore=%s\nafter=%s", beforeAuthority, got)
	}
	if got := snapshotFilesystem(t, st.Home()); got != beforeFS {
		t.Fatalf("dry run changed filesystem:\nbefore=%s\nafter=%s", beforeFS, got)
	}
	// The boundary itself is retained by the strict comparison.
	boundary, err := New(st).Collect(ctx, Request{Cutoff: now.Add(time.Second), Now: now.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if boundary.Eligible["expiredClaims"].Count != 0 {
		t.Fatalf("boundary was collected: %+v", boundary)
	}
}

func TestCollectRetiresExpiredClaimAndKeepsItUntilNextRun(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	clock := testkit.NewClock(now)
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, clock, nil, lease.Defaults{TTL: time.Minute})
	id, token := strings.Repeat("2", 32), strings.Repeat("b", 64)
	if _, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: id, Token: token, Resources: []string{"one", "two"}, AgentID: "agent", SessionID: "session", TTL: time.Second, RequestNotAfter: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * 24 * time.Hour)
	firstNow := clock.Now()
	result, err := New(st).Collect(ctx, Request{Cutoff: now.Add(24 * time.Hour), Now: firstNow, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Retired["expiredClaims"].Count != 1 {
		t.Fatalf("retired=%+v", result.Retired)
	}
	if countRows(t, st, "claims") != 0 || countRows(t, st, "epochs") != 1 {
		t.Fatalf("retirement did not preserve epoch")
	}
	var ended, recorded int64
	if err := st.Read(ctx, func(tx *store.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT ended_at,ended_recorded_at FROM epochs WHERE claim_id=?`, id).Scan(&ended, &recorded)
	}); err != nil {
		t.Fatal(err)
	}
	if ended <= 0 || ended >= recorded || recorded != firstNow.UnixMicro() {
		t.Fatalf("termination timestamps=%d,%d", ended, recorded)
	}
	// A second run after the recorded-end retention window may remove the
	// complete epoch and its operation receipts.
	second, err := New(st).Collect(ctx, Request{Cutoff: firstNow.Add(time.Hour), Now: firstNow.Add(time.Hour), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if second.Collected["epochs"].Count != 1 || countRows(t, st, "epochs") != 0 || countRows(t, st, "operations") != 0 {
		t.Fatalf("second collection=%+v", second)
	}
	if watermark(t, st, "last_event_seq") == "0" {
		t.Fatal("last event watermark was lost")
	}
}

func TestRequestDeadlineUsesAuthorityNowNotRetentionCutoff(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	id := strings.Repeat("4", 32)
	if err := st.WriteAt(ctx, now, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO epochs(claim_id,token_hash,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,acquired_seq,ended_at,ended_seq,ended_recorded_at,end_reason,final_revision) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, strings.Repeat("a", 64), "agent", "session", "r", "local-coordination", 1, now.Add(-3*time.Hour).UnixMicro(), 1, now.Add(-2*time.Hour).UnixMicro(), 1, now.Add(-2*time.Hour).UnixMicro(), "released", 1); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO epoch_resources(claim_id,resource,position) VALUES(?,?,0)`, id, "r"); err != nil {
			return err
		}
		// This deadline is newer than the retention cutoff but already past.
		_, err := tx.ExecContext(ctx, `INSERT INTO operations(claim_id,operation_id,kind,request_hash,request_not_after,expected_revision,state,started_at,started_seq,completed_at,completed_seq) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, strings.Repeat("b", 32), "release", strings.Repeat("c", 64), now.Add(-30*time.Minute).UnixMicro(), 1, "completed", now.Add(-2*time.Hour).UnixMicro(), 1, now.Add(-2*time.Hour).UnixMicro(), 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	result, err := New(st).Collect(ctx, Request{Cutoff: now.Add(-time.Hour), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if result.Eligible["epochs"].Count != 1 {
		t.Fatalf("deadline was compared with cutoff: %+v", result)
	}
}

func TestCollectProtectsStartedOperationAndFutureRequestDeadline(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	clock := testkit.NewClock(now)
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, clock, nil, lease.Defaults{TTL: time.Minute})
	id, token := strings.Repeat("3", 32), strings.Repeat("c", 64)
	deadline := now.Add(3 * time.Hour)
	g, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: id, Token: token, Resources: []string{"r"}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Release(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: id, Token: token, Revision: g.Revision}, lease.ReleaseRequest{OperationID: strings.Repeat("4", 32), RequestNotAfter: deadline}); err != nil {
		t.Fatal(err)
	}
	// Make the ended epoch old while retaining an operation deadline that is
	// still within the replay window; GC must preserve authentication material.
	clock.Advance(2 * time.Hour)
	if err := st.WriteAt(ctx, clock.Now(), func(tx *store.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE epochs SET ended_recorded_at=? WHERE claim_id=?`, now.Add(-time.Hour).UnixMicro(), id)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	result, err := New(st).Collect(ctx, Request{Cutoff: now.Add(time.Hour), Now: clock.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Protected["epochs"].Count == 0 {
		t.Fatalf("epoch with recent request deadline not protected: %+v", result)
	}
}

func TestApplyRollbackLeavesRetirementAndWatermarksUntouched(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	clock := testkit.NewClock(now)
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, clock, nil, lease.Defaults{TTL: time.Minute})
	id, token := strings.Repeat("5", 32), strings.Repeat("d", 64)
	g, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: id, Token: token, Resources: []string{"rollback"}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Release(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: id, Token: token, Revision: g.Revision}, lease.ReleaseRequest{OperationID: strings.Repeat("6", 32), RequestNotAfter: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteAt(ctx, now, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE epochs SET ended_recorded_at=?,ended_at=? WHERE claim_id=?`, now.Add(-2*time.Hour).UnixMicro(), now.Add(-2*time.Hour).UnixMicro(), id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE operations SET request_not_after=? WHERE claim_id=?`, now.Add(-2*time.Hour).UnixMicro(), id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `CREATE TRIGGER gc_test_abort BEFORE DELETE ON epochs BEGIN SELECT RAISE(ABORT, 'gc rollback'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	beforeEvents, beforeEpochs := countRows(t, st, "events"), countRows(t, st, "epochs")
	if _, err := New(st).Collect(ctx, Request{Cutoff: now.Add(-time.Hour), Now: now, Apply: true}); err == nil {
		t.Fatal("expected trigger failure")
	}
	if countRows(t, st, "events") != beforeEvents || countRows(t, st, "epochs") != beforeEpochs || countRows(t, st, "claims") != 0 {
		t.Fatal("failed GC partially committed")
	}
	if watermark(t, st, "pruned_through_seq") != "0" {
		t.Fatal("failed GC changed watermark")
	}
}

func TestInterleavedEpochsPruneOnlyEventPrefixAndProtectUnresolved(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ids := []string{strings.Repeat("7", 32), strings.Repeat("8", 32), strings.Repeat("9", 32)}
	if err := st.WriteAt(ctx, now, func(tx *store.Tx) error {
		for i, id := range ids {
			at := now.Add(time.Duration(i) * time.Minute)
			if i == 0 {
				at = now.Add(-3 * time.Hour)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO epochs(claim_id,token_hash,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,acquired_seq,ended_at,ended_seq,ended_recorded_at,end_reason,final_revision,checkpoint) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, strings.Repeat("a", 64), "agent", "session", id, "local-coordination", 1, at.UnixMicro(), 0, func() any {
				if i == 2 {
					return nil
				}
				return at.UnixMicro()
			}(), nil, func() any {
				if i == 1 {
					return now.UnixMicro()
				}
				return at.Add(-time.Hour).UnixMicro()
			}(), func() any {
				if i == 2 {
					return nil
				}
				return "released"
			}(), 1, nil); err != nil {
				return err
			}
			seq, err := tx.AppendEvent(store.Event{At: at, Kind: "acquired", ClaimID: id, Resources: []string{"shared"}, AgentID: "agent"})
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE epochs SET acquired_seq=?,ended_seq=CASE WHEN ended_at IS NULL THEN NULL ELSE ? END WHERE claim_id=?`, seq, seq, id); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO epoch_resources(claim_id,resource,position) VALUES(?,?,0)`, id, "shared"); err != nil {
				return err
			}
		}
		// The newest epoch is current and therefore explicitly active.
		if _, err := tx.ExecContext(ctx, `INSERT INTO claims(claim_id,token_hash,revision,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,ttl_us,heartbeat_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, ids[2], strings.Repeat("b", 64), 1, "agent", "session", ids[2], "local-coordination", 1, now.UnixMicro(), int64(time.Hour/time.Microsecond), now.UnixMicro(), now.Add(time.Hour).UnixMicro()); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO claim_resources(resource,claim_id,position) VALUES(?,?,0)`, "shared", ids[2])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	result, err := New(st).Collect(ctx, Request{Cutoff: now.Add(-time.Hour), Now: now, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Collected["epochs"].Count != 1 || countRows(t, st, "epochs") != 2 || result.Protected["activeClaims"].Count != 1 {
		t.Fatalf("interleaved result=%+v", result)
	}
	if result.PrunedThrough != "1" || watermark(t, st, "pruned_through_seq") != "1" {
		t.Fatalf("prefix watermark=%q", result.PrunedThrough)
	}
	cursorJSON, _ := json.Marshal(ledger.Cursor{Version: 1, AuthorityID: st.AuthorityID(), Feed: "events", Sequence: "0"})
	page, err := ledger.New(st).Events(ctx, base64.RawURLEncoding.EncodeToString(cursorJSON), 50)
	if err != nil || !page.Gap || len(page.Events) != 0 {
		t.Fatalf("continuation gap=%+v err=%v", page, err)
	}
	historyCursor, _ := json.Marshal(ledger.Cursor{Version: 1, AuthorityID: st.AuthorityID(), Feed: "history", Filter: "shared", Sequence: "0"})
	history, err := ledger.New(st).History(ctx, "shared", base64.RawURLEncoding.EncodeToString(historyCursor), 50, false)
	if err != nil || !history.Gap || len(history.Epochs) != 0 {
		t.Fatalf("history continuation gap=%+v err=%v", history, err)
	}
	var remainingEvents int
	if err := st.Read(ctx, func(tx *store.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM events WHERE kind='acquired'`).Scan(&remainingEvents)
	}); err != nil {
		t.Fatal(err)
	}
	if remainingEvents != 2 {
		t.Fatalf("middle event was pruned: %d", remainingEvents)
	}
}

func TestUnresolvedPredecessorIsProtected(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	id := strings.Repeat("e", 32)
	if err := st.WriteAt(ctx, now, func(tx *store.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO epochs(claim_id,token_hash,agent_id,session_id,work_key,guarantee,local_replace_allowed,acquired_at,acquired_seq,ended_at,ended_seq,ended_recorded_at,end_reason,final_revision) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, strings.Repeat("c", 64), "agent", "session", "r", "local-coordination", 1, now.Add(-3*time.Hour).UnixMicro(), 1, now.Add(-2*time.Hour).UnixMicro(), 1, now.Add(-2*time.Hour).UnixMicro(), "expired", 1); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO epoch_resources(claim_id,resource,position) VALUES(?,?,0)`, id, "r"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO operations(claim_id,operation_id,kind,request_hash,request_not_after,expected_revision,state,started_at,started_seq) VALUES(?,?,?,?,?,?,?,?,?)`, id, strings.Repeat("f", 32), "exec", strings.Repeat("d", 64), now.Add(-time.Hour).UnixMicro(), 1, "started", now.Add(-2*time.Hour).UnixMicro(), 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	result, err := New(st).Collect(ctx, Request{Cutoff: now.Add(-time.Hour), Now: now, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Protected["epochs"].Count != 1 || countRows(t, st, "epochs") != 1 {
		t.Fatalf("unresolved epoch was collected: %+v", result)
	}
}

func TestExpiredReplayIsRejectedWithoutRedispatch(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	clock := testkit.NewClock(now)
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, clock, nil, lease.Defaults{TTL: time.Minute})
	id, token := strings.Repeat("0", 32), strings.Repeat("4", 64)
	g, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: id, Token: token, Resources: []string{"replay"}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.BeginOperation(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: id, Token: token, Revision: g.Revision}, lease.OperationIntent{OperationID: strings.Repeat("1", 32), Kind: "exec", Request: map[string]any{"cwd": "/tmp"}, TTL: time.Minute, RequestNotAfter: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Minute)
	_, err = svc.BeginOperation(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: id, Token: token, Revision: g.Revision + 1}, lease.OperationIntent{OperationID: strings.Repeat("2", 32), Kind: "exec", Request: map[string]any{"cwd": "/tmp"}, TTL: time.Minute, RequestNotAfter: clock.Now().Add(-time.Second)})
	if e := reason.As(err); e == nil || e.Reason != reason.ReasonReplayExpired {
		t.Fatalf("replay error=%v", err)
	}
	if countRows(t, st, "operations") != 2 {
		t.Fatal("expired replay attempted a second dispatch")
	}
}

func TestConcurrentGCAcquireHeartbeatAndReconcileSerialize(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	clock := testkit.NewClock(now)
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, clock, nil, lease.Defaults{TTL: time.Minute})
	oldID, oldToken := strings.Repeat("a", 32), strings.Repeat("1", 64)
	old, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: oldID, Token: oldToken, Resources: []string{"shared"}, AgentID: "old", SessionID: "old", TTL: time.Second, RequestNotAfter: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	targetID := strings.Repeat("b", 32)
	started, err := svc.BeginOperation(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: oldID, Token: oldToken, Revision: old.Revision}, lease.OperationIntent{OperationID: targetID, Kind: "exec", Request: map[string]any{"cwd": "/tmp"}, TTL: time.Second, RequestNotAfter: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(2 * time.Second)
	newID, newToken := strings.Repeat("c", 32), strings.Repeat("2", 64)
	current, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: newID, Token: newToken, Resources: []string{"shared"}, AgentID: "new", SessionID: "new", TTL: time.Minute, RequestNotAfter: clock.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		_, e := New(st).Collect(ctx, Request{Cutoff: clock.Now().Add(-time.Hour), Now: clock.Now(), Apply: true})
		errs <- e
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		_, e := svc.Heartbeat(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: newID, Token: newToken, Revision: current.Revision}, lease.Renew{OperationID: strings.Repeat("d", 32), TTL: time.Minute, RequestNotAfter: clock.Now().Add(time.Hour)})
		errs <- e
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		_, e := svc.Reconcile(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: newID, Token: newToken, Revision: current.Revision}, lease.ReconcileRequest{OperationID: strings.Repeat("e", 32), TargetClaimID: oldID, TargetOperationID: targetID, ExpectedRequestSHA256: started.RequestHash, Outcome: "observed-success", Evidence: []byte(`{"executorStopped":true}`), TTL: time.Minute, RequestNotAfter: clock.Now().Add(time.Hour)})
		errs <- e
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		_, e := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: strings.Repeat("f", 32), Token: strings.Repeat("3", 64), Resources: []string{"free"}, AgentID: "other", SessionID: "other", TTL: time.Minute, RequestNotAfter: clock.Now().Add(time.Hour)})
		errs <- e
	}()
	close(start)
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil && reason.As(e) == nil {
			t.Fatalf("unexpected concurrent error: %v", e)
		}
	}
	var integrity string
	if err := st.Read(ctx, func(tx *store.Tx) error {
		return tx.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity)
	}); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity_check=%q", integrity)
	}
	if countRows(t, st, "claims") != 2 {
		t.Fatal("concurrent lifecycle lost a current claim")
	}
}

func TestReplayReceiptAndAuthenticationRetainedThroughDeadlineThenPruned(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	clock := testkit.NewClock(now)
	st, err := store.Open(ctx, t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, clock, nil, lease.Defaults{TTL: time.Minute})
	id, token := strings.Repeat("6", 32), strings.Repeat("e", 64)
	deadline := now.Add(time.Hour)
	req := lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: id, Token: token, Resources: []string{"replay"}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: deadline}
	first, err := svc.Acquire(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Release(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: id, Token: token, Revision: first.Revision}, lease.ReleaseRequest{OperationID: strings.Repeat("7", 32), RequestNotAfter: deadline}); err != nil {
		t.Fatal(err)
	}
	// Make the structural end old without changing the absolute replay deadline.
	if err := st.WriteAt(ctx, now, func(tx *store.Tx) error {
		_, e := tx.ExecContext(ctx, `UPDATE epochs SET ended_recorded_at=? WHERE claim_id=?`, now.Add(-2*time.Hour).UnixMicro(), id)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	kept, err := New(st).Collect(ctx, Request{Cutoff: now.Add(-time.Hour), Now: now, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if kept.Protected["epochs"].Count != 1 || countRows(t, st, "operations") != 2 {
		t.Fatalf("receipt was not retained: %+v", kept)
	}
	replay, err := svc.Acquire(ctx, req)
	if err != nil || replay.ClaimID != id || replay.Active {
		t.Fatalf("retained replay=%+v err=%v", replay, err)
	}
	clock.Advance(2 * time.Hour)
	pruned, err := New(st).Collect(ctx, Request{Cutoff: clock.Now().Add(-time.Hour), Now: clock.Now(), Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if pruned.Collected["epochs"].Count != 1 || countRows(t, st, "epochs") != 0 || countRows(t, st, "operations") != 0 {
		t.Fatalf("expired receipt was not pruned: %+v", pruned)
	}
	_, err = svc.Acquire(ctx, req)
	if e := reason.As(err); e == nil || e.Reason != reason.ReasonReplayExpired {
		t.Fatalf("post-prune replay error=%v", err)
	}
}

func snapshotAuthority(t *testing.T, st *store.Store) string {
	t.Helper()
	var result strings.Builder
	for _, table := range []string{"meta", "claims", "claim_resources", "epochs", "epoch_resources", "operations", "reconciliations", "events"} {
		if err := st.Read(context.Background(), func(tx *store.Tx) error {
			rows, err := tx.QueryContext(context.Background(), `SELECT * FROM `+table+` ORDER BY rowid`)
			if err != nil {
				return err
			}
			defer rows.Close()
			columns, err := rows.Columns()
			if err != nil {
				return err
			}
			result.WriteString(table + ":" + strings.Join(columns, ",") + "\n")
			for rows.Next() {
				values := make([]any, len(columns))
				pointers := make([]any, len(columns))
				for i := range values {
					pointers[i] = &values[i]
				}
				if err := rows.Scan(pointers...); err != nil {
					return err
				}
				for i, value := range values {
					if raw, ok := value.([]byte); ok {
						values[i] = string(raw)
					}
				}
				result.WriteString(fmt.Sprintf("%#v\n", values))
			}
			return rows.Err()
		}); err != nil {
			t.Fatal(err)
		}
	}
	return result.String()
}
func snapshotFilesystem(t *testing.T, root string) string {
	t.Helper()
	var result strings.Builder
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result.WriteString(rel + "|" + info.Mode().String() + "|" + fmt.Sprint(info.Size()) + "|" + info.ModTime().UTC().Format(time.RFC3339Nano))
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			result.WriteString("|" + hex.EncodeToString(sum[:]))
		}
		result.WriteByte('\n')
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return result.String()
}

func countRows(t *testing.T, st *store.Store, table string) int {
	t.Helper()
	var n int
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		return tx.QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}
func watermark(t *testing.T, st *store.Store, key string) string {
	t.Helper()
	var value string
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT value FROM meta WHERE key=?`, key).Scan(&value)
	}); err != nil {
		t.Fatal(err)
	}
	return value
}
