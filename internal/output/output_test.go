package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

func TestErrorRenderersPreserveSafeRecoveryPathAndRedaction(t *testing.T) {
	hex64 := strings.Repeat("ab", 32)
	secret := strings.Repeat("c", 64)
	pendingPath := "/home/handles/ctx-" + hex64 + ".json"
	err := reason.New(reason.ReasonUnknownOutcomePending, "request "+secret+" is pending").
		With("pendingPath", pendingPath).
		With("resource", secret).
		With("recoveryHint", "retry\nwith "+secret).
		With("holder", map[string]any{
			"token":      secret,
			"note":       secret,
			"checkpoint": map[string]any{"pendingPath": pendingPath},
		}).
		With("handlePath", "/private/ctx-"+hex64+".json")

	var jsonOut bytes.Buffer
	if writeErr := WriteError(&jsonOut, "release", err); writeErr != nil {
		t.Fatal(writeErr)
	}
	var envelope Envelope
	if decodeErr := json.Unmarshal(jsonOut.Bytes(), &envelope); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if envelope.Error.Details["pendingPath"] != pendingPath {
		t.Fatalf("JSON pendingPath = %#v", envelope.Error.Details["pendingPath"])
	}
	if holder, ok := envelope.Error.Details["holder"].(map[string]any); !ok || len(holder) != 0 {
		t.Fatalf("JSON holder was not safely projected: %#v", envelope.Error.Details["holder"])
	}
	if strings.Contains(jsonOut.String(), secret) {
		t.Fatalf("JSON leaked credential: %q", jsonOut.String())
	}

	for _, color := range []bool{false, true} {
		t.Run(fmt.Sprintf("color=%v", color), func(t *testing.T) {
			var textOut bytes.Buffer
			if writeErr := writeTextError(&textOut, err, color); writeErr != nil {
				t.Fatal(writeErr)
			}
			text := textOut.String()
			if !strings.Contains(text, "pendingPath: "+pendingPath) {
				t.Fatalf("text pendingPath was not preserved: %q", text)
			}
			for _, disallowed := range []string{secret, "handlePath:", "checkpoint: map"} {
				if strings.Contains(text, disallowed) {
					t.Fatalf("text exposed %q: %q", disallowed, text)
				}
			}
			for _, want := range []string{"request [REDACTED] is pending", "resource: [REDACTED]", "recoveryHint:", `retry\\nwith [REDACTED]`, "holder: map[]"} {
				if !strings.Contains(text, want) {
					t.Fatalf("text missing %q: %q", want, text)
				}
			}
			if color != strings.Contains(text, "\x1b[") {
				t.Fatalf("color=%v output=%q", color, text)
			}
		})
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

func TestHolderProjectionKeepsOnlyBoundedPublicFields(t *testing.T) {
	t.Parallel()
	claimID := strings.Repeat("a", 32)
	err := reason.New(reason.ReasonAlreadyClaimed, "resource is already claimed").With("resource", "coordination:test").With("holder", map[string]any{
		"claimId": claimID, "agentId": "agent", "workKey": "work", "expiresAt": "2026-09-12T00:00:00.000000Z",
		"token": strings.Repeat("b", 64), "checkpoint": map[string]any{"secret": "private"}, "unexpected": "drop",
	})
	failure := Classify(err)
	holder, ok := failure.Details["holder"].(map[string]any)
	if !ok || len(holder) != 4 || holder["claimId"] != claimID || holder["agentId"] != "agent" || holder["workKey"] != "work" {
		t.Fatalf("holder projection=%#v", failure.Details["holder"])
	}
	if _, present := holder["unexpected"]; present {
		t.Fatal("unexpected holder field survived")
	}
	var out bytes.Buffer
	if err := WriteTextError(&out, reason.New(reason.ReasonAlreadyClaimed, "resource is already claimed").With("holder", map[string]any{"agentId": "agent\n-injected", "workKey": strings.Repeat("x", 1025)})); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "injected") || strings.Contains(out.String(), "workKey") {
		t.Fatalf("malformed holder field rendered: %q", out.String())
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
