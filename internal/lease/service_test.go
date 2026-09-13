package lease

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
	"github.com/brettinternet/worklease/internal/testkit"
)

type testIDs struct{ n int }

var currentTestAuthority string

func (g *testIDs) Generate() string { g.n++; return strings.Repeat("0", 31) + string(rune('0'+g.n%10)) }
func openLeaseTest(t *testing.T) (*Service, *store.Store, *testkit.Clock) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	clock := testkit.NewClock(now)
	st, err := store.Open(context.Background(), t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	currentTestAuthority = st.AuthorityID()
	return New(st, clock, &testIDs{}, Defaults{TTL: 5 * time.Second}), st, clock
}
func req(resource, id, token string) AcquireRequest {
	return AcquireRequest{AuthorityID: currentTestAuthority, Resources: []string{resource}, ClaimID: id, Token: token, AgentID: "agent", SessionID: "session", TTL: 2 * time.Second, RequestNotAfter: time.Now().Add(23 * time.Hour)}
}
func credentials(st *store.Store, id, token string, rev int64) Credentials {
	return Credentials{AuthorityID: st.AuthorityID(), ClaimID: id, Token: token, Revision: rev}
}

func TestLifecycleAtomicAcquireRenewCheckpointReleaseTransfer(t *testing.T) {
	svc, st, clock := openLeaseTest(t)
	ctx := context.Background()
	token := strings.Repeat("a", 64)
	g, err := svc.Acquire(ctx, req("resource", strings.Repeat("1", 32), token))
	if err != nil {
		t.Fatal(err)
	}
	if g.Revision != 1 || !g.Active {
		t.Fatalf("grant=%+v", g)
	}
	c := credentials(st, g.ClaimID, token, g.Revision)
	r, err := svc.Heartbeat(ctx, c, Renew{OperationID: strings.Repeat("2", 32), TTL: 2 * time.Second, RequestNotAfter: time.Now().Add(23 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if r.Revision != 2 {
		t.Fatalf("heartbeat=%+v", r)
	}
	c.Revision = r.Revision
	r, err = svc.Checkpoint(ctx, c, CheckpointRequest{OperationID: strings.Repeat("3", 32), Data: []byte(`{"step":1}`), TTL: 2 * time.Second, RequestNotAfter: time.Now().Add(23 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	c.Revision = r.Revision
	succID := strings.Repeat("4", 32)
	succToken := strings.Repeat("b", 64)
	ng, err := svc.Transfer(ctx, c, TransferRequest{OperationID: strings.Repeat("5", 32), SuccessorClaimID: succID, SuccessorToken: succToken, ToAgent: "next", ToSession: "next-session", TTL: 2 * time.Second, RequestNotAfter: time.Now().Add(23 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if ng.Revision != 1 {
		t.Fatalf("successor=%+v", ng)
	}
	if _, err := svc.Status(ctx, Selector{ClaimID: g.ClaimID}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(3 * time.Second)
	_, _ = svc.Release(ctx, credentials(st, succID, succToken, 1), ReleaseRequest{OperationID: strings.Repeat("6", 32), RequestNotAfter: time.Now().Add(23 * time.Hour)})
}

func TestAuthorizationOrderedAndNoFailedWriteSideEffects(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	id := strings.Repeat("1", 32)
	token := strings.Repeat("a", 64)
	g, err := svc.Acquire(context.Background(), req("r", id, token))
	if err != nil {
		t.Fatal(err)
	}
	bad := credentials(st, id, strings.Repeat("b", 64), 0)
	_, err = svc.Heartbeat(context.Background(), bad, Renew{OperationID: strings.Repeat("2", 32), RequestNotAfter: time.Now().Add(23 * time.Hour)})
	reasonErr := reason.As(err)
	if reasonErr == nil || reasonErr.Reason != reason.ReasonInvalidToken {
		t.Fatalf("err=%v", err)
	}
	v, err := svc.Status(context.Background(), Selector{ClaimID: id})
	if err != nil || v.Claim == nil || v.Claim.Revision != g.Revision {
		t.Fatalf("status=%+v err=%v", v, err)
	}
	if v.Claim.CheckpointPresent {
		t.Fatal("failed write changed checkpoint")
	}
	_, err = svc.Heartbeat(context.Background(), credentials(st, id, token, 0), Renew{OperationID: strings.Repeat("3", 32), RequestNotAfter: time.Now().Add(23 * time.Hour)})
	if e := reason.As(err); e == nil || e.Reason != reason.ReasonStaleRevision {
		t.Fatalf("missing revision=%v", err)
	}
}

func TestReplayAuthenticatesEndedEpochAndRejectsChangedIntent(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	deadline := time.Now().Add(23 * time.Hour)
	id := strings.Repeat("1", 32)
	token := strings.Repeat("a", 64)
	g, err := svc.Acquire(context.Background(), req("r", id, token))
	if err != nil {
		t.Fatal(err)
	}
	c := credentials(st, id, token, g.Revision)
	op := strings.Repeat("2", 32)
	first, err := svc.Heartbeat(context.Background(), c, Renew{OperationID: op, TTL: 2 * time.Second, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	c.Revision = first.Revision
	if _, err = svc.Release(context.Background(), c, ReleaseRequest{OperationID: strings.Repeat("3", 32), Reason: "done", RequestNotAfter: deadline}); err != nil {
		t.Fatal(err)
	}
	replay, err := svc.Heartbeat(context.Background(), credentials(st, id, token, 1), Renew{OperationID: op, TTL: 2 * time.Second, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Idempotent || replay.Revision != first.Revision {
		t.Fatalf("replay=%+v", replay)
	}
	_, err = svc.Heartbeat(context.Background(), credentials(st, id, token, 1), Renew{OperationID: op, TTL: 3 * time.Second, RequestNotAfter: deadline})
	e := reason.As(err)
	if e == nil || e.Reason != reason.ReasonOperationRequestMismatch {
		t.Fatalf("mismatch=%v", err)
	}
}

func TestHoldDeadlineIsPartOfAcquireHeartbeatAndCheckpointIntent(t *testing.T) {
	svc, st, clock := openLeaseTest(t)
	ctx := context.Background()
	deadline := clock.Now().Add(time.Hour)
	hold := clock.Now().Add(10 * time.Minute)
	changedHold := hold.Add(time.Minute)
	id := strings.Repeat("1", 32)
	token := strings.Repeat("a", 64)
	acquire := req("r", id, token)
	acquire.RequestNotAfter = deadline
	acquire.HoldUntil = hold
	grant, err := svc.Acquire(ctx, acquire)
	if err != nil {
		t.Fatal(err)
	}
	acquire.HoldUntil = changedHold
	if _, err = svc.Acquire(ctx, acquire); !isReason(err, reason.ReasonOperationRequestMismatch) {
		t.Fatalf("changed acquire hold=%v", err)
	}

	creds := credentials(st, id, token, grant.Revision)
	heartbeat := Renew{OperationID: strings.Repeat("2", 32), TTL: 2 * time.Second, RequestNotAfter: deadline, HoldUntil: hold}
	receipt, err := svc.Heartbeat(ctx, creds, heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	heartbeat.HoldUntil = changedHold
	if _, err = svc.Heartbeat(ctx, creds, heartbeat); !isReason(err, reason.ReasonOperationRequestMismatch) {
		t.Fatalf("changed heartbeat hold=%v", err)
	}

	creds.Revision = receipt.Revision
	checkpoint := CheckpointRequest{OperationID: strings.Repeat("3", 32), TTL: 2 * time.Second, Data: []byte(`{"step":1}`), RequestNotAfter: deadline, HoldUntil: hold}
	if _, err = svc.Checkpoint(ctx, creds, checkpoint); err != nil {
		t.Fatal(err)
	}
	checkpoint.HoldUntil = changedHold
	if _, err = svc.Checkpoint(ctx, creds, checkpoint); !isReason(err, reason.ReasonOperationRequestMismatch) {
		t.Fatalf("changed checkpoint hold=%v", err)
	}
}

func isReason(err error, want string) bool {
	e := reason.As(err)
	return e != nil && e.Reason == want
}

func TestLegacyLifecycleHashReplaysOnlyWhenExplicitlySupplied(t *testing.T) {
	svc, st, clock := openLeaseTest(t)
	ctx := context.Background()
	deadline := clock.Now().Add(time.Hour)
	id := strings.Repeat("1", 32)
	token := strings.Repeat("a", 64)
	acquire := req("r", id, token)
	acquire.RequestNotAfter = deadline
	grant, err := svc.Acquire(ctx, acquire)
	if err != nil {
		t.Fatal(err)
	}
	creds := credentials(st, id, token, grant.Revision)
	op := strings.Repeat("2", 32)
	legacy, err := svc.Heartbeat(ctx, creds, Renew{OperationID: op, TTL: 2 * time.Second, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	hold := clock.Now().Add(10 * time.Minute)
	if _, err = svc.Heartbeat(ctx, creds, Renew{OperationID: op, TTL: 2 * time.Second, RequestNotAfter: deadline, HoldUntil: hold}); !isReason(err, reason.ReasonOperationRequestMismatch) {
		t.Fatalf("unmarked legacy replay=%v", err)
	}
	replay, err := svc.Heartbeat(ctx, creds, Renew{OperationID: op, TTL: 2 * time.Second, RequestNotAfter: deadline, HoldUntil: hold, LegacyRequestHash: legacy.RequestHash})
	if err != nil || !replay.Idempotent || replay.RequestHash != legacy.RequestHash {
		t.Fatalf("explicit legacy replay=%+v err=%v", replay, err)
	}
}

func TestClockForwardExpirySmallRollbackAndLargeRegression(t *testing.T) {
	svc, _, clock := openLeaseTest(t)
	id := strings.Repeat("1", 32)
	token := strings.Repeat("a", 64)
	g, err := svc.Acquire(context.Background(), req("r", id, token))
	if err != nil {
		t.Fatal(err)
	}
	clock.SetWall(clock.Now().Add(-500 * time.Millisecond))
	if _, err = svc.Verify(context.Background(), Credentials{ClaimID: id, Token: token, Revision: g.Revision}, nil); err != nil {
		t.Fatalf("small rollback: %v", err)
	}
	clock.SetWall(clock.Now().Add(-2 * time.Second))
	_, err = svc.Heartbeat(context.Background(), Credentials{ClaimID: id, Token: token, Revision: g.Revision}, Renew{OperationID: strings.Repeat("2", 32), RequestNotAfter: time.Now().Add(23 * time.Hour)})
	e := reason.As(err)
	if e == nil || e.Reason != reason.ReasonClockRegression {
		t.Fatalf("large rollback=%v", err)
	}
}

func TestStartedOperationExclusivityAndCompletionRevision(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	id := strings.Repeat("1", 32)
	token := strings.Repeat("a", 64)
	g, err := svc.Acquire(context.Background(), req("r", id, token))
	if err != nil {
		t.Fatal(err)
	}
	c := credentials(st, id, token, g.Revision)
	started, err := svc.BeginOperation(context.Background(), c, OperationIntent{OperationID: strings.Repeat("2", 32), Kind: "exec", Request: map[string]any{"cwd": "/tmp", "maxDuration": 3}, RequestNotAfter: time.Now().Add(23 * time.Hour), TTL: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if started.Revision != 2 {
		t.Fatalf("started=%+v", started)
	}
	_, err = svc.BeginOperation(context.Background(), credentials(st, id, token, started.Revision), OperationIntent{OperationID: strings.Repeat("3", 32), Kind: "exec", RequestNotAfter: time.Now().Add(23 * time.Hour)})
	reasonErr := reason.As(err)
	if reasonErr == nil || reasonErr.Reason != reason.ReasonOperationInProgress {
		t.Fatalf("overlap=%v", err)
	}
	_, err = svc.Heartbeat(context.Background(), credentials(st, id, token, started.Revision), Renew{OperationID: strings.Repeat("4", 32), RequestNotAfter: time.Now().Add(23 * time.Hour)})
	if e := reason.As(err); e == nil || e.Reason != reason.ReasonOperationInProgress {
		t.Fatalf("external renewal=%v", err)
	}
	renewed, err := svc.RenewOperation(context.Background(), credentials(st, id, token, started.Revision), started.OperationID, 2*time.Second)
	if err != nil || renewed.Revision != 3 {
		t.Fatalf("internal renewal=%v", err)
	}
	done, err := svc.CompleteOperation(context.Background(), credentials(st, id, token, renewed.Revision), started.OperationID, map[string]any{"exitStatus": 0})
	if err != nil {
		t.Fatal(err)
	}
	if done.Revision != 4 {
		t.Fatalf("done=%+v", done)
	}
	if _, err = svc.Heartbeat(context.Background(), credentials(st, id, token, done.Revision), Renew{OperationID: strings.Repeat("5", 32), RequestNotAfter: time.Now().Add(23 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
}
