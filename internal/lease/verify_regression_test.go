package lease

import (
	"context"
	"testing"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

func TestVerifyEmptyReadOnlyAuthorityFailsClosed(t *testing.T) {
	home := t.TempDir() + "/missing"
	st, err := store.Open(context.Background(), home, store.Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := New(st, nil, nil, Defaults{})
	verification, err := svc.Verify(context.Background(), Credentials{ClaimID: "claim", Token: "token"}, nil)
	if err == nil || verification.Claim.ClaimID != "" {
		t.Fatalf("verify unexpectedly succeeded: verification=%+v err=%v", verification, err)
	}
	if got := reason.As(err); got == nil || got.Reason != reason.ReasonStorageFailure {
		t.Fatalf("reason=%v, want storage-failure", err)
	}
}
