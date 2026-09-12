package main

import (
	"context"
	"testing"

	"github.com/brettinternet/worklease/internal/reason"
)

func TestExitCodeUsesStableReasonFamily(t *testing.T) {
	t.Parallel()
	if got := exitCode(reason.New(reason.ReasonConfigInvalid, "bad config")); got != reason.ExitInvalid {
		t.Fatalf("exit code = %d", got)
	}
	if got := exitCode(context.Canceled); got != reason.ExitInterrupted {
		t.Fatalf("cancellation exit code = %d", got)
	}
}
