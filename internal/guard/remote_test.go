package guard

import (
	"context"
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
