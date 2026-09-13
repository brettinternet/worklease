package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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

func TestWriteTextErrorColorsReasonAndRecoveryHint(t *testing.T) {
	var out bytes.Buffer
	err := reason.New(reason.ReasonUnknownOutcome, "result is uncertain").With("recoveryHint", "inspect the operation")
	if writeErr := writeTextError(&out, err, true); writeErr != nil {
		t.Fatal(writeErr)
	}
	for _, want := range []string{"\x1b[31merror: unknown-outcome:\x1b[0m", "recoveryHint: \x1b[33minspect the operation\x1b[0m"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("colored error missing %q: %q", want, out.String())
		}
	}
	if ColorEnabled(&bytes.Buffer{}) {
		t.Fatal("non-terminal buffer enabled color")
	}
}

func TestWriteTextErrorSuggestsDoctorForUnsafeHome(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := WriteTextError(&out, reason.New(reason.ReasonHomeUnsafe, "authority home is unsafe")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "recoveryHint: run worklease doctor") {
		t.Fatalf("unsafe-home error missing recovery hint: %q", out.String())
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

func TestSuccessorHandlePreservesContextualPathButNotCredential(t *testing.T) {
	digest := strings.Repeat("a", 64)
	token := strings.Repeat("b", 64)
	projected := Redact(map[string]any{"successorHandle": "/tmp/ctx-" + digest + ".json", "token": token}).(map[string]any)
	if projected["successorHandle"] != "/tmp/ctx-"+digest+".json" || projected["token"] != "[REDACTED]" {
		t.Fatalf("projection=%#v", projected)
	}
}

func TestInvalidUTF8IsRedacted(t *testing.T) {
	t.Parallel()
	if got := RedactString(string([]byte{0xff})); got != "[REDACTED]" {
		t.Fatalf("got %q", got)
	}
}

func TestTypedPrivateAndPublicOutputPolicies(t *testing.T) {
	t.Parallel()
	type receipt struct {
		Token      string         `json:"token"`
		Argv       []string       `json:"argv"`
		Checkpoint map[string]any `json:"checkpoint"`
		Evidence   map[string]any `json:"evidence"`
		Output     string         `json:"output"`
		Revision   int64          `json:"revision"`
	}
	token := strings.Repeat("a", 64)
	value := receipt{
		Token: token, Argv: []string{"printf", "allowed"},
		Checkpoint: map[string]any{"phase": "allowed", token: "first", strings.Repeat("b", 64): "second"},
		Evidence:   map[string]any{"executorStopped": true}, Output: "allowed",
		Revision: 9007199254740993,
	}
	private := Redact(value).(map[string]any)
	if private["token"] != "[REDACTED]" || private["output"] != "allowed" {
		t.Fatalf("private policy = %#v", private)
	}
	if argv := private["argv"].([]any); argv[1] != "allowed" {
		t.Fatalf("private argv = %#v", argv)
	}
	if private["revision"].(json.Number).String() != "9007199254740993" {
		t.Fatalf("revision lost precision: %#v", private["revision"])
	}
	encoded, err := json.Marshal(private)
	if err != nil || strings.Contains(string(encoded), token) || !strings.Contains(string(encoded), `"[REDACTED]-2":"second"`) {
		t.Fatalf("map key redaction was unsafe or lossy: %s (%v)", encoded, err)
	}
	public := RedactPublic(value).(map[string]any)
	for _, key := range []string{"token", "argv", "checkpoint", "evidence", "output"} {
		if public[key] != "[REDACTED]" {
			t.Fatalf("public %s = %#v", key, public[key])
		}
	}
}

func TestJSONAndTextWritersApplySelectedPolicy(t *testing.T) {
	t.Parallel()
	fields := map[string]any{"checkpoint": map[string]string{"phase": "allowed"}}
	for _, test := range []struct {
		name   string
		write  func(io.Writer, string, map[string]any) error
		public bool
	}{
		{"private-json", WriteSuccess, false},
		{"private-text", WriteText, false},
		{"public-json", WritePublicSuccess, true},
		{"public-text", WritePublicText, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := test.write(&out, "test", fields); err != nil {
				t.Fatal(err)
			}
			contains := strings.Contains(out.String(), "allowed")
			if contains == test.public {
				t.Fatalf("public=%v output=%q", test.public, out.String())
			}
		})
	}
}

// Contextual handle paths embed a SHA-256 context ID and receipts carry
// SHA-256 request hashes. Neither is a credential; redacting them would
// destroy the recovery pointers the contract requires. A bare 64-hex value
// under any other key is still treated as a token.
func TestRedactKeepsHashAndPathFieldsButRedactsBareTokens(t *testing.T) {
	hex64 := strings.Repeat("ab", 32)
	redacted := Redact(map[string]any{
		"pendingPath":   "/home/handles/ctx-" + hex64 + ".json",
		"handlePath":    "/home/handles/ctx-" + hex64 + ".json",
		"requestSha256": hex64,
		"contentSha256": hex64,
		"note":          hex64,
	}).(map[string]any)
	for _, key := range []string{"pendingPath", "handlePath", "requestSha256", "contentSha256"} {
		if value, _ := redacted[key].(string); !strings.Contains(value, hex64) {
			t.Fatalf("%s was redacted: %v", key, redacted[key])
		}
	}
	if redacted["note"] != "[REDACTED]" {
		t.Fatalf("bare token survived under a neutral key: %v", redacted["note"])
	}
}
