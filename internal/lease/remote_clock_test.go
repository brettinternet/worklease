package lease

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
)

func TestRemoteAuthMutationUsesEffectiveAuthorityClock(t *testing.T) {
	svc, _, clock, actor := openRemoteLeaseTest(t)
	clock.Advance(time.Second)
	clock.SetWall(clock.Now().Add(-2 * time.Second))
	invite, _ := GenerateInviteCode()
	_, err := svc.IssueInvite(context.Background(), actor, IssueInviteRequest{InviteID: strings.Repeat("1", 32), OperationID: strings.Repeat("2", 32), RequestNotAfter: clock.Now().Add(time.Hour), Role: "read", Label: "clock", InviteSha256: HashSecret(invite)})
	if !isReason(err, reason.ReasonClockRegression) {
		t.Fatalf("clock regression=%v", err)
	}
}
