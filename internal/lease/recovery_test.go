package lease

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestAcquireReturnsPredecessorCheckpointAfterRelease(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	oldID, newID := strings.Repeat("1", 32), strings.Repeat("2", 32)
	token := strings.Repeat("a", 64)
	deadline := time.Now().Add(time.Hour)
	g, err := svc.Acquire(context.Background(), AcquireRequest{AuthorityID: st.AuthorityID(), Resources: []string{"r"}, ClaimID: oldID, Token: token, AgentID: "a", SessionID: "s", TTL: 2 * time.Second, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	r, err := svc.Checkpoint(context.Background(), credentials(st, oldID, token, g.Revision), CheckpointRequest{OperationID: strings.Repeat("3", 32), Data: []byte(`{"cursor":7}`), RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Release(context.Background(), credentials(st, oldID, token, r.Revision), ReleaseRequest{OperationID: strings.Repeat("4", 32), RequestNotAfter: deadline}); err != nil {
		t.Fatal(err)
	}
	next, err := svc.Acquire(context.Background(), AcquireRequest{AuthorityID: st.AuthorityID(), Resources: []string{"r"}, ClaimID: newID, Token: strings.Repeat("b", 64), AgentID: "b", SessionID: "s2", TTL: 2 * time.Second, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Recovery) != 1 || !next.Recovery[0].CheckpointPresent || next.Recovery[0].ClaimID != oldID {
		t.Fatalf("recovery=%+v", next.Recovery)
	}
}
