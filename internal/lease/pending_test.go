package lease

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
)

func TestExpiredPredecessorUnknownOperationBlocksVerification(t *testing.T) {
	svc, st, clock := openLeaseTest(t)
	id, successor := strings.Repeat("1", 32), strings.Repeat("2", 32)
	token := strings.Repeat("a", 64)
	grant, err := svc.Acquire(context.Background(), req("resource", id, token))
	if err != nil {
		t.Fatal(err)
	}
	started, err := svc.BeginOperation(context.Background(), credentials(st, id, token, grant.Revision), OperationIntent{OperationID: strings.Repeat("3", 32), Kind: "exec", RequestNotAfter: time.Now().Add(23 * time.Hour), TTL: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(3 * time.Second)
	newToken := strings.Repeat("b", 64)
	next, err := svc.Acquire(context.Background(), req("resource", successor, newToken))
	if err != nil {
		t.Fatal(err)
	}
	if len(next.UnknownOperations) != 1 || next.UnknownOperations[0] != started.OperationID {
		t.Fatalf("unknown=%v", next.UnknownOperations)
	}
	_, err = svc.Verify(context.Background(), credentials(st, successor, newToken, next.Revision), nil)
	if e := reason.As(err); e == nil || e.Reason != reason.ReasonUnknownOutcomePending {
		t.Fatalf("verify=%v", err)
	}
	_, err = svc.BeginOperation(context.Background(), credentials(st, successor, newToken, next.Revision), OperationIntent{OperationID: strings.Repeat("4", 32), Kind: "exec", RequestNotAfter: time.Now().Add(23 * time.Hour), TTL: 2 * time.Second})
	if e := reason.As(err); e == nil || e.Reason != reason.ReasonUnknownOutcomePending {
		t.Fatalf("successor begin=%v", err)
	}
}
