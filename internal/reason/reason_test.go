package reason

import (
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
