package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/reason"
)

func TestWriteSuccessProducesOneVersionedEnvelope(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := WriteSuccess(&out, "version", map[string]any{"version": "dev"}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("output is not one line: %q", out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["schemaVersion"] != float64(2) || got["operation"] != "version" || got["ok"] != true {
		t.Fatalf("unexpected envelope: %#v", got)
	}
}

func TestClassifyPreservesCommittedAndUnknownState(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"committed", "unknown"} {
		failure := Classify(reason.New(reason.ReasonHandleWriteFailed, "write failed").With("commitState", state))
		if failure.Details["commitState"] != state {
			t.Fatalf("state = %#v", failure.Details)
		}
	}
}

func TestErrorsAndNestedValuesAreRedacted(t *testing.T) {
	t.Parallel()
	token := strings.Repeat("a", 64)
	err := reason.Invalid("rejected --token="+token).With("token", token).With("nested", map[string]any{"token_hash": token})
	var out bytes.Buffer
	if writeErr := WriteError(&out, "worklease", err); writeErr != nil {
		t.Fatal(writeErr)
	}
	if strings.Contains(out.String(), token) || !strings.Contains(out.String(), "[REDACTED]") {
		t.Fatalf("credential leaked: %s", out.String())
	}
	if strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("multiple envelopes: %q", out.String())
	}
}

func TestWriteTextErrorIncludesSafeHolderMetadata(t *testing.T) {
	var out bytes.Buffer
	err := reason.New(reason.ReasonAlreadyClaimed, "resource is already claimed").With("resource", "task").With("holder", map[string]any{
		"agentId": "other", "claimId": "claim", "expiresAt": "2026-09-12T00:00:00.000000Z",
	})
	if writeErr := WriteTextError(&out, err); writeErr != nil {
		t.Fatal(writeErr)
	}
	text := out.String()
	for _, phrase := range []string{"error: already-claimed:", "holder:", "agentId", "expiresAt"} {
		if !strings.Contains(text, phrase) {
			t.Fatalf("text error missing %q: %q", phrase, text)
		}
	}
}

func TestClassifyUnexpectedErrorIsSafe(t *testing.T) {
	t.Parallel()
	failure := Classify(errors.New("private implementation detail"))
	if failure.Reason != reason.ReasonInternal || strings.Contains(failure.Message, "private") || failure.Details["commitState"] != "not-committed" {
		t.Fatalf("unsafe classification: %#v", failure)
	}
}

func TestInvalidUTF8IsRedacted(t *testing.T) {
	t.Parallel()
	if got := RedactString(string([]byte{0xff})); got != "[REDACTED]" {
		t.Fatalf("got %q", got)
	}
}
