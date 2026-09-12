package lease

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

func requireReason(t *testing.T, err error, want string) {
	t.Helper()
	got := reason.As(err)
	if got == nil || got.Reason != want {
		t.Fatalf("reason=%v want=%s", err, want)
	}
}

func TestGuardReplayUsesOriginalEpochBeforeCurrentAuthorization(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	deadline := svc.clock.Now().Add(time.Hour)
	claimID, token, operationID := strings.Repeat("1", 32), strings.Repeat("a", 64), strings.Repeat("2", 32)
	grant, err := svc.Acquire(context.Background(), AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Resources: []string{"r"}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	intent := OperationIntent{OperationID: operationID, Kind: "exec", TTL: time.Minute, RequestNotAfter: deadline, Request: map[string]any{"cwd": "/tmp", "maxDuration": 10}}
	started, err := svc.BeginOperation(context.Background(), credentials(st, claimID, token, grant.Revision), intent)
	if err != nil {
		t.Fatal(err)
	}
	changedMax := intent
	changedMax.Request = map[string]any{"cwd": "/tmp", "maxDuration": 11}
	_, err = svc.BeginOperation(context.Background(), credentials(st, claimID, token, grant.Revision), changedMax)
	requireReason(t, err, reason.ReasonOperationRequestMismatch)
	_, err = svc.BeginOperation(context.Background(), credentials(st, claimID, token, grant.Revision), intent)
	requireReason(t, err, reason.ReasonUnknownOutcome)
	completed, err := svc.CompleteOperation(context.Background(), credentials(st, claimID, token, started.Revision), operationID, map[string]any{"exitStatus": 0})
	if err != nil {
		t.Fatal(err)
	}
	if completed.Revision != 3 {
		t.Fatalf("completion revision=%d", completed.Revision)
	}
	replayed, err := svc.CompleteOperation(context.Background(), credentials(st, claimID, token, grant.Revision), operationID, nil)
	if err != nil || !replayed.Idempotent || replayed.Revision != completed.Revision {
		t.Fatalf("completion replay=%+v err=%v", replayed, err)
	}
	if _, err := svc.Release(context.Background(), credentials(st, claimID, token, completed.Revision), ReleaseRequest{OperationID: strings.Repeat("3", 32), RequestNotAfter: deadline}); err != nil {
		t.Fatal(err)
	}
	beginReplay, err := svc.BeginOperation(context.Background(), credentials(st, claimID, token, grant.Revision), intent)
	if err != nil || !beginReplay.Completed || beginReplay.Receipt == nil || beginReplay.Receipt.Revision != completed.Revision {
		t.Fatalf("begin replay=%+v err=%v", beginReplay, err)
	}
}

func TestAcquireReplayReportsOriginalReceiptAndFinalEpochState(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	deadline := svc.clock.Now().Add(time.Hour)
	request := AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: strings.Repeat("1", 32), Token: strings.Repeat("a", 64), Resources: []string{"r"}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: deadline}
	grant, err := svc.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := svc.Heartbeat(context.Background(), credentials(st, grant.ClaimID, request.Token, grant.Revision), Renew{OperationID: strings.Repeat("2", 32), TTL: time.Minute, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	released, err := svc.Release(context.Background(), credentials(st, grant.ClaimID, request.Token, renewed.Revision), ReleaseRequest{OperationID: strings.Repeat("3", 32), RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := svc.Acquire(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Active || replay.Revision != released.Revision || replay.Receipt.Revision != 1 || replay.Receipt.RequestHash == "" || !replay.Receipt.Idempotent {
		t.Fatalf("acquire replay=%+v", replay)
	}
}

func TestSuppliedGuardHashMustMatchCanonicalIntent(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	deadline := svc.clock.Now().Add(time.Hour)
	claimID, token := strings.Repeat("1", 32), strings.Repeat("a", 64)
	grant, err := svc.Acquire(context.Background(), AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Resources: []string{"r"}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.BeginOperation(context.Background(), credentials(st, claimID, token, grant.Revision), OperationIntent{OperationID: strings.Repeat("2", 32), Kind: "exec", TTL: time.Minute, RequestNotAfter: deadline, RequestHash: strings.Repeat("f", 64), Request: map[string]any{"cwd": "/changed"}})
	requireReason(t, err, reason.ReasonOperationRequestMismatch)
	replace := OperationIntent{OperationID: strings.Repeat("3", 32), Kind: "replace-file", TTL: time.Minute, RequestNotAfter: deadline, Request: map[string]any{"target": "/tmp/file", "contentDigest": "one"}}
	if _, err := svc.BeginOperation(context.Background(), credentials(st, claimID, token, grant.Revision), replace); err != nil {
		t.Fatal(err)
	}
	replace.Request = map[string]any{"target": "/tmp/file", "contentDigest": "two"}
	_, err = svc.BeginOperation(context.Background(), credentials(st, claimID, token, grant.Revision), replace)
	requireReason(t, err, reason.ReasonOperationRequestMismatch)
}

func TestReplayDeadlineIsExclusiveAndWaitCapsSleep(t *testing.T) {
	svc, st, clock := openLeaseTest(t)
	claimID, token := strings.Repeat("1", 32), strings.Repeat("a", 64)
	acquireDeadline := clock.Now().Add(2 * time.Hour)
	grant, err := svc.Acquire(context.Background(), AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Resources: []string{"r"}, AgentID: "agent", SessionID: "session", TTL: time.Hour, RequestNotAfter: acquireDeadline})
	if err != nil {
		t.Fatal(err)
	}
	replayDeadline := clock.Now().Add(time.Minute)
	renew := Renew{OperationID: strings.Repeat("2", 32), TTL: time.Hour, RequestNotAfter: replayDeadline}
	if _, err := svc.Heartbeat(context.Background(), credentials(st, claimID, token, grant.Revision), renew); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute)
	_, err = svc.Heartbeat(context.Background(), credentials(st, claimID, token, grant.Revision), renew)
	requireReason(t, err, reason.ReasonReplayExpired)

	home := t.TempDir()
	realStore, err := store.Open(context.Background(), home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer realStore.Close()
	realService := New(realStore, nil, nil, Defaults{})
	now := time.Now()
	first := AcquireRequest{AuthorityID: realStore.AuthorityID(), ClaimID: strings.Repeat("4", 32), Token: strings.Repeat("b", 64), Resources: []string{"busy"}, AgentID: "one", SessionID: "one", TTL: time.Minute, RequestNotAfter: now.Add(time.Hour)}
	if _, err := realService.Acquire(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := AcquireRequest{AuthorityID: realStore.AuthorityID(), ClaimID: strings.Repeat("5", 32), Token: strings.Repeat("c", 64), Resources: []string{"busy"}, AgentID: "two", SessionID: "two", TTL: time.Minute, RequestNotAfter: now.Add(time.Hour), Wait: 50 * time.Millisecond, PollInterval: time.Second}
	startedAt := time.Now()
	_, err = realService.Acquire(context.Background(), second)
	requireReason(t, err, reason.ReasonWaitTimeout)
	if elapsed := time.Since(startedAt); elapsed > 300*time.Millisecond {
		t.Fatalf("wait exceeded bound: %s", elapsed)
	}
}

func TestInvalidInputsAndSecretsDoNotMutate(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	deadline := svc.clock.Now().Add(time.Hour)
	base := AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: strings.Repeat("1", 32), Token: strings.Repeat("a", 64), Resources: []string{"r"}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: deadline}
	missingAuthority := base
	missingAuthority.AuthorityID = ""
	_, err := svc.Acquire(context.Background(), missingAuthority)
	requireReason(t, err, reason.ReasonInvalidArgument)
	badIdentity := base
	badIdentity.AgentID = "bad\nagent"
	_, err = svc.Acquire(context.Background(), badIdentity)
	requireReason(t, err, reason.ReasonInvalidArgument)
	badResource := base
	badResource.Resources = []string{string([]byte{0xff})}
	_, err = svc.Acquire(context.Background(), badResource)
	requireReason(t, err, reason.ReasonInvalidResource)
	grant, err := svc.Acquire(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	creds := credentials(st, grant.ClaimID, base.Token, grant.Revision)
	_, err = svc.Release(context.Background(), creds, ReleaseRequest{OperationID: strings.Repeat("2", 32), Reason: base.Token, RequestNotAfter: deadline})
	requireReason(t, err, reason.ReasonInvalidArgument)
	_, err = svc.BeginOperation(context.Background(), creds, OperationIntent{OperationID: strings.Repeat("3", 32), Kind: "exec", TTL: time.Minute, RequestNotAfter: deadline, Request: map[string]any{"maxDuration": math.NaN()}})
	requireReason(t, err, reason.ReasonInvalidArgument)
	status, err := svc.Status(context.Background(), Selector{ClaimID: grant.ClaimID})
	if err != nil || status.Claim == nil || status.Claim.Revision != grant.Revision {
		t.Fatalf("invalid input changed claim: %+v err=%v", status, err)
	}
}

func TestForwardClockStepExpiresClaim(t *testing.T) {
	svc, st, clock := openLeaseTest(t)
	deadline := clock.Now().Add(time.Hour)
	claimID, token := strings.Repeat("1", 32), strings.Repeat("a", 64)
	grant, err := svc.Acquire(context.Background(), AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Resources: []string{"r"}, AgentID: "agent", SessionID: "session", TTL: 2 * time.Second, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(3 * time.Second)
	_, err = svc.Verify(context.Background(), credentials(st, claimID, token, grant.Revision), nil)
	if e := reason.As(err); e == nil || e.Reason != reason.ReasonVerifyFailed || e.Details["cause"] != reason.ReasonClaimExpired {
		t.Fatalf("forward expiry=%v", err)
	}
}

func TestTransferReplayReportsActualExpiredSuccessor(t *testing.T) {
	svc, st, clock := openLeaseTest(t)
	deadline := clock.Now().Add(time.Hour)
	claimID, token := strings.Repeat("1", 32), strings.Repeat("a", 64)
	grant, err := svc.Acquire(context.Background(), AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Resources: []string{"r"}, AgentID: "agent", SessionID: "session", TTL: 2 * time.Second, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	request := TransferRequest{OperationID: strings.Repeat("2", 32), SuccessorClaimID: strings.Repeat("3", 32), SuccessorToken: strings.Repeat("b", 64), ToAgent: "next", ToSession: "next-session", TTL: 2 * time.Second, RequestNotAfter: deadline}
	if _, err := svc.Transfer(context.Background(), credentials(st, claimID, token, grant.Revision), request); err != nil {
		t.Fatal(err)
	}
	clock.Advance(3 * time.Second)
	replayed, err := svc.Transfer(context.Background(), credentials(st, claimID, token, grant.Revision), request)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Active || replayed.ExpiresAt.After(clock.Now()) {
		t.Fatalf("replayed successor should be expired: %+v", replayed)
	}
}
