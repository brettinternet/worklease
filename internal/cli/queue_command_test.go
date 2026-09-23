package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestQueueCommandRejectsJSONWithoutEnteringTerminal(t *testing.T) {
	var out, errs bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "--json", "queue"}, "test", "unknown", "unknown", &out, &errs)
	if err == nil || !strings.Contains(out.String(), "text-only") {
		t.Fatalf("err=%v output=%s", err, out.String())
	}
}
func TestQueueSourceFailureLabels(t *testing.T) {
	for _, tt := range []struct{ message, want string }{{"API rate limit reached", "rate-limited"}, {"permission denied", "permission denied"}, {"connection refused", "offline/unavailable"}} {
		if got := queueSourceFailure(errString(tt.message)); got != tt.want {
			t.Errorf("%s: %s", tt.message, got)
		}
	}
	if !sourceSubset([]string{"a"}, []string{"a", "b"}) || sourceSubset([]string{"c"}, []string{"a", "b"}) {
		t.Fatal("view source subset mismatch")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
