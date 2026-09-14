package guard

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
)

type losingExecAuthority struct{}

func (losingExecAuthority) BeginOperation(context.Context, lease.Credentials, lease.OperationIntent) (lease.Started, error) {
	return lease.Started{OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ClaimID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Kind: "exec", Revision: 2, RequestHash: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}, nil
}
func (losingExecAuthority) RenewOperation(context.Context, lease.Credentials, string, time.Duration) (lease.Receipt, error) {
	return lease.Receipt{}, reason.New(reason.ReasonInstallationRevoked, "installation revoked")
}
func (losingExecAuthority) CompleteOperation(context.Context, lease.Credentials, string, map[string]any) (lease.Receipt, error) {
	return lease.Receipt{}, nil
}

type replayingRenewAuthority struct {
	renewals int
}

func (a *replayingRenewAuthority) BeginOperation(context.Context, lease.Credentials, lease.OperationIntent) (lease.Started, error) {
	return lease.Started{OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ClaimID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Kind: "exec", Revision: 2, RequestHash: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}, nil
}
func (a *replayingRenewAuthority) RenewOperation(context.Context, lease.Credentials, string, time.Duration) (lease.Receipt, error) {
	a.renewals++
	if a.renewals == 1 {
		return lease.Receipt{}, reason.New(reason.ReasonUnknownOutcome, "lost renewal response")
	}
	return lease.Receipt{Revision: 3, Result: map[string]any{"expiresAt": time.Now().Add(time.Second).Format(time.RFC3339Nano)}}, nil
}
func (*replayingRenewAuthority) CompleteOperation(context.Context, lease.Credentials, string, map[string]any) (lease.Receipt, error) {
	return lease.Receipt{OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ClaimID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Kind: "exec", Revision: 4, Committed: true, Result: map[string]any{"returncode": 0}}, nil
}

func TestReplayedReceiptExitAcceptsJSONNumber(t *testing.T) {
	if got := receiptExit(lease.Receipt{Result: map[string]any{"returncode": json.Number("0")}}); got != 0 {
		t.Fatalf("exit=%d", got)
	}
}

func TestTransientRenewalFailureRetriesExactRequestBeforeExpiry(t *testing.T) {
	authority := &replayingRenewAuthority{}
	result, err := Exec(context.Background(), authority, lease.Credentials{AuthorityID: "dddddddddddddddddddddddddddddddd", ClaimID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, ExecRequest{
		OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Argv:        []string{"sleep", "0.18"}, TTL: 200 * time.Millisecond, MaxDuration: time.Second,
		RequestNotAfter: time.Now().Add(time.Hour),
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if authority.renewals != 2 {
		t.Fatalf("renewals=%d want immediate exact replay", authority.renewals)
	}
}

type finishingDuringRenewAuthority struct {
	renewals           int
	completionRevision int64
}

func (a *finishingDuringRenewAuthority) BeginOperation(context.Context, lease.Credentials, lease.OperationIntent) (lease.Started, error) {
	return lease.Started{OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ClaimID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Kind: "exec", Revision: 2}, nil
}
func (a *finishingDuringRenewAuthority) RenewOperation(context.Context, lease.Credentials, string, time.Duration) (lease.Receipt, error) {
	a.renewals++
	if a.renewals == 1 {
		time.Sleep(80 * time.Millisecond)
		return lease.Receipt{}, reason.New(reason.ReasonUnknownOutcome, "lost renewal response")
	}
	return lease.Receipt{Revision: 3, Result: map[string]any{"expiresAt": time.Now().Add(time.Second).Format(time.RFC3339Nano)}}, nil
}
func (a *finishingDuringRenewAuthority) CompleteOperation(_ context.Context, credentials lease.Credentials, _ string, _ map[string]any) (lease.Receipt, error) {
	a.completionRevision = credentials.Revision
	return lease.Receipt{Result: map[string]any{"returncode": 0}}, nil
}

func TestFinishedChildReplaysInflightRenewalBeforeCompletion(t *testing.T) {
	authority := &finishingDuringRenewAuthority{}
	_, err := Exec(context.Background(), authority, lease.Credentials{ClaimID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, ExecRequest{
		OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Argv: []string{"sleep", "0.08"},
		TTL: 100 * time.Millisecond, MaxDuration: time.Second, RequestNotAfter: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if authority.renewals != 2 || authority.completionRevision != 3 {
		t.Fatalf("renewals=%d completionRevision=%d", authority.renewals, authority.completionRevision)
	}
}

func TestRemoteOwnershipFailureTerminatesProcessGroupImmediately(t *testing.T) {
	started := time.Now()
	_, err := Exec(context.Background(), losingExecAuthority{}, lease.Credentials{AuthorityID: "dddddddddddddddddddddddddddddddd", ClaimID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, ExecRequest{
		OperationID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Argv:        []string{"sleep", "5"}, TTL: 200 * time.Millisecond, MaxDuration: 5 * time.Second,
		RequestNotAfter: time.Now().Add(time.Hour),
	})
	if classified := reason.As(err); classified == nil || classified.Reason != reason.ReasonOwnershipLost {
		t.Fatalf("error=%v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("ownership loss took %s to terminate child", elapsed)
	}
}
