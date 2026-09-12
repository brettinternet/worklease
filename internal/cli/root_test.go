package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/reason"
)

func TestVersionTextAndJSON(t *testing.T) {
	for _, args := range [][]string{{"worklease", "version", "--json"}, {"worklease", "--json", "version"}, {"worklease", "--version", "--json"}} {
		var stdout, stderr bytes.Buffer
		if err := Run(context.Background(), args, "1.2.3", "abc123", "2026-09-12T00:00:00Z", &stdout, &stderr); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		var result map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("%v: %v (%q)", args, err, stdout.String())
		}
		if result["schemaVersion"] != float64(2) || result["operation"] != "version" || result["version"] != "1.2.3" || result["commit"] != "abc123" || result["buildTime"] == nil || result["goVersion"] == nil {
			t.Fatalf("%v: %#v", args, result)
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q", stderr.String())
		}
	}
	var text bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "version"}, "dev", "unknown", "unknown", &text, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"worklease dev", "commit: unknown", "schemaVersion: 2"} {
		if !strings.Contains(text.String(), part) {
			t.Errorf("text output missing %q: %q", part, text.String())
		}
	}
}

func TestParserFailuresKeepOneJSONEnvelopeAndRedact(t *testing.T) {
	token := strings.Repeat("a", 64)
	for _, args := range [][]string{{"worklease", "--json", "--unknown=" + token}, {"worklease", "version", "--json", "--unknown=" + token}, {"worklease", "--json", string([]byte{0xff})}} {
		var stdout, stderr bytes.Buffer
		err := Run(context.Background(), args, "dev", "unknown", "unknown", &stdout, &stderr)
		if err == nil {
			t.Fatalf("%v: expected error", args)
		}
		if strings.Contains(stdout.String(), token) || strings.Contains(stderr.String(), token) {
			t.Fatalf("%v leaked credential", args)
		}
		if strings.Count(stdout.String(), "\n") != 1 {
			t.Fatalf("%v emitted multiple documents: %q", args, stdout.String())
		}
		var result map[string]any
		if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
			t.Fatalf("%v: %v (%q)", args, decodeErr, stdout.String())
		}
		if result["ok"] != false {
			t.Fatalf("%v: %#v", args, result)
		}
	}
}

func TestCommandTreeRegistrationHelpAndShortOptions(t *testing.T) {
	root := NewRootCommand("dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	want := []string{"version", "key", "policy", "acquire", "status", "list", "heartbeat", "checkpoint", "release", "transfer", "verify", "exec", "replace-file", "op", "history", "events", "watch", "gc", "doctor", "instructions", "setup", "mcp"}
	got := map[string]bool{}
	for _, command := range root.Commands {
		got[command.Name] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("command %q is not registered", name)
		}
	}
	if err := validateCLICommandTree(root); err != nil {
		t.Fatal(err)
	}
	for _, command := range root.Commands {
		if command.Usage == "" {
			t.Errorf("%s has no usage", command.Name)
		}
		if !strings.Contains(command.Description, "Examples:") {
			t.Errorf("%s has no help example", command.Name)
		}
	}
	for _, part := range []string{"[$WORKLEASE_HOME]", "[$WORKLEASE_CONFIG]"} {
		found := false
		for _, flag := range root.Flags {
			if strings.Contains(flag.String(), part) {
				found = true
			}
		}
		if !found {
			t.Errorf("root help does not reference %s", part)
		}
	}
}

func TestRunHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Run(ctx, []string{"worklease", "version"}, "dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
}

func TestExclusiveSelectionAndResourceBoundaries(t *testing.T) {
	revision, fd := int64(1), 3
	if _, err := Select(SelectionInput{Handle: "a", Lease: "b"}, true); reason.As(err).Reason != reason.ReasonCredentialSourceConflict {
		t.Fatalf("handle/lease conflict = %v", err)
	}
	if _, err := Select(SelectionInput{ClaimID: "id", TokenFile: "token", TokenFD: &fd, Revision: &revision}, true); reason.As(err).Reason != reason.ReasonCredentialSourceConflict {
		t.Fatalf("token conflict = %v", err)
	}
	if _, err := Select(SelectionInput{ClaimID: "id", TokenFile: "token"}, true); reason.As(err).Reason != reason.ReasonInvalidArgument {
		t.Fatalf("missing revision = %v", err)
	}
	if selected, err := Select(SelectionInput{Session: "stable"}, true); err != nil || selected.Mode != "context" || selected.Session != "stable" {
		t.Fatalf("context selection = %#v, %v", selected, err)
	}
}
