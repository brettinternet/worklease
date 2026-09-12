package lease

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
)

func TestCheckpointRejectsDuplicateAndCredentialFieldsWithoutMutation(t *testing.T) {
	svc, st, _ := openLeaseTest(t)
	id, token := strings.Repeat("1", 32), strings.Repeat("a", 64)
	grant, err := svc.Acquire(context.Background(), req("r", id, token))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Hour)
	for _, data := range []string{`{"x":1,"x":2}`, `{"token":"` + token + `"}`} {
		_, err = svc.Checkpoint(context.Background(), credentials(st, id, token, grant.Revision), CheckpointRequest{OperationID: strings.Repeat("2", 32), Data: []byte(data), RequestNotAfter: deadline})
		if e := reason.As(err); e == nil || e.Reason != reason.ReasonInvalidArgument {
			t.Fatalf("data=%s err=%v", data, err)
		}
	}
	status, err := svc.Status(context.Background(), Selector{ClaimID: id})
	if err != nil || status.Claim == nil || status.Claim.Revision != grant.Revision {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}
