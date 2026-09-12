package reason

import (
	"errors"
	"fmt"
	"testing"
)

func TestRegisteredReasonsCoverEveryExitFamily(t *testing.T) {
	t.Parallel()
	families := map[int]bool{}
	for _, name := range Names() {
		item := New(name, name)
		if !Registered(name) || item.Code != CodeFor(name) {
			t.Fatalf("reason %q is not stable", name)
		}
		families[item.Code] = true
	}
	for _, code := range []int{ExitInternal, ExitOwnership, ExitLedger, ExitInvalid, ExitAuthority, ExitChildTimeout, ExitInterrupted} {
		if !families[code] {
			t.Errorf("exit family %d has no registered reason", code)
		}
	}
}

func TestErrorSurvivesWrappingAndImplementsExitCoder(t *testing.T) {
	t.Parallel()
	original := New(ReasonStaleClaim, "stale")
	wrapped := fmt.Errorf("context: %w", original)
	if As(wrapped) != original || original.ExitCode() != ExitOwnership {
		t.Fatalf("classification lost: %#v", As(wrapped))
	}
}

func TestDefinitiveNoCommitAndOwnershipRenewalClassification(t *testing.T) {
	for _, name := range []string{ReasonStaleRevision, ReasonClaimExpired, ReasonAlreadyClaimed, ReasonInvalidArgument, ReasonOperationRequestMismatch} {
		if !DefinitiveNoCommit(New(name, "")) {
			t.Fatalf("%s must be a definitive no-commit failure", name)
		}
	}
	for _, name := range []string{ReasonUnknownOutcome, ReasonUnknownOutcomePending, ReasonReplayExpired, ReasonStorageFailure, ReasonHandleWriteFailed, ReasonOwnershipLost, ReasonChildTimeout, ReasonInterrupted} {
		if DefinitiveNoCommit(New(name, "")) {
			t.Fatalf("%s must remain uncertain", name)
		}
	}
	if DefinitiveNoCommit(errors.New("unclassified")) || DefinitiveNoCommit(nil) {
		t.Fatal("unclassified errors are never definitive")
	}
	for _, name := range []string{ReasonStaleClaim, ReasonInvalidToken, ReasonClaimExpired, ReasonStaleRevision, ReasonClockRegression, ReasonOperationNotFound} {
		if !OwnershipRenewalFailure(New(name, "")) {
			t.Fatalf("%s must terminate a supervised child", name)
		}
	}
	if OwnershipRenewalFailure(New(ReasonStorageFailure, "")) || OwnershipRenewalFailure(errors.New("busy")) {
		t.Fatal("transient renewal failures must not terminate the child")
	}
}
