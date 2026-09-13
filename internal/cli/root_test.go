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
	for _, test := range []struct {
		name, version, commit, buildTime, want string
	}{
		{name: "release", version: "1.0.0", commit: "ca97d51936e447ef33d1eb01610543c32a345dbf", buildTime: "2026-09-13T01:08:03Z", want: "worklease 1.0.0 (ca97d51, built 2026-09-13T01:08:03Z)\n"},
		{name: "development", version: "dev", commit: "unknown", buildTime: "unknown", want: "worklease dev\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var text bytes.Buffer
			if err := Run(context.Background(), []string{"worklease", "version"}, test.version, test.commit, test.buildTime, &text, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if text.String() != test.want {
				t.Fatalf("text output = %q, want %q", text.String(), test.want)
			}
		})
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
	future := []string{}
	got := map[string]bool{}
	for _, command := range root.Commands {
		got[command.Name] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("command %q is not registered", name)
		}
	}
	for _, name := range future {
		if command := root.Command(name); command != nil && (command.Usage == "" || !strings.Contains(command.Description, "Examples:")) {
			t.Errorf("future command %q has incomplete staged help", name)
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

func TestHistoryHelpDocumentsBothProjections(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "history", "--help"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"worklease history\n", "worklease history --resource RESOURCE", "global lifecycle event feed", "retained claim epochs for exactly one resource"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("history help missing %q: %q", want, stdout.String())
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestAffectedCommandHelpDescribesTextViewsAndColor(t *testing.T) {
	root := NewRootCommand("dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	for _, path := range []string{"key", "status", "history", "events", "watch", "gc", "policy describe"} {
		command := root
		for _, name := range strings.Fields(path) {
			command = command.Command(name)
		}
		if command == nil {
			t.Fatalf("missing %s command", path)
		}
		for _, want := range []string{"Output:", "ANSI color", "NO_COLOR", "TERM=dumb", "redirection", "--json"} {
			if !strings.Contains(command.Description, want) {
				t.Errorf("%s help missing %q: %q", path, want, command.Description)
			}
		}
	}
	for _, path := range []string{"status", "history", "events", "policy describe"} {
		command := root
		for _, name := range strings.Fields(path) {
			command = command.Command(name)
		}
		if !strings.Contains(command.Description, "--full") {
			t.Errorf("%s help does not explain --full: %q", path, command.Description)
		}
	}
}

type canonicalHelpCase struct {
	path, example string
	flags         []string
}

func (test canonicalHelpCase) pathUsage() string {
	return "worklease " + test.path
}

func TestCanonicalCommandHelpPathsFlagsAndExamples(t *testing.T) {
	root := NewRootCommand("dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	tests := []canonicalHelpCase{
		{path: "version", example: "worklease version --json", flags: []string{}},
		{path: "key", example: "worklease key --path README.md", flags: []string{"resource", "provider", "source", "item", "path", "coordination-only"}},
		{path: "acquire", example: "worklease acquire --path README.md", flags: []string{"resource", "provider", "source", "item", "path", "coordination-only", "ttl", "wait", "poll-interval", "agent", "work-key", "session", "handle", "claim-id", "token-file", "token-fd", "request-not-after", "no-handle"}},
		{path: "status", example: "worklease status", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "resource", "full"}},
		{path: "list", example: "worklease list", flags: []string{"resource", "full"}},
		{path: "heartbeat", example: "worklease heartbeat", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "ttl", "operation-id", "request-not-after"}},
		{path: "checkpoint", example: "worklease checkpoint --data '{}'", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "ttl", "operation-id", "request-not-after", "data", "data-file"}},
		{path: "release", example: "worklease release", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "ttl", "operation-id", "request-not-after", "reason"}},
		{path: "exec", example: "worklease exec -- git status", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "ttl", "operation-id", "request-not-after", "max-duration", "cwd", "git-primary"}},
		{path: "transfer", example: "worklease transfer --successor-handle PATH --to-agent AGENT --to-session SESSION", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "ttl", "operation-id", "request-not-after", "to-agent", "to-session", "to-work-key", "successor-handle"}},
		{path: "replace-file", example: "worklease replace-file --path FILE --expected-sha256 SHA256 --content-file CONTENT", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "ttl", "operation-id", "request-not-after", "path", "expected-sha256", "content-file"}},
		{path: "verify", example: "worklease verify", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "resource", "hook", "coverage"}},
		{path: "history", example: "worklease history", flags: []string{"resource", "cursor", "limit", "full"}},
		{path: "events", example: "worklease events", flags: []string{"cursor", "limit", "full"}},
		{path: "watch", example: "worklease watch --resource RESOURCE --until free", flags: []string{"resource", "cursor", "until", "timeout"}},
		{path: "gc", example: "worklease gc --retention-days 30", flags: []string{"retention-days", "cutoff", "apply"}},
		{path: "doctor", example: "worklease doctor", flags: []string{}},
		{path: "policy list", example: "worklease policy list", flags: []string{"full"}},
		{path: "policy describe", example: "worklease policy describe path", flags: []string{"full"}},
		{path: "op inspect", example: "worklease op inspect --operation-id ID", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "resource", "operation-id", "full"}},
		{path: "op reconcile", example: "worklease op reconcile --target-operation-id ID --outcome observed-success --evidence '{\"outcome\":\"observed-success\",\"executorStopped\":true}'", flags: []string{"handle", "lease", "claim-id", "token-file", "token-fd", "revision", "session", "ttl", "operation-id", "request-not-after", "target-claim-id", "target-operation-id", "outcome", "evidence", "expected-request-sha256"}},
		{path: "instructions loop", example: "worklease instructions loop", flags: []string{}},
		{path: "instructions safety", example: "worklease instructions safety", flags: []string{}},
		{path: "setup mcp", example: "worklease setup mcp --client claude-code --scope project", flags: []string{"client", "scope", "agent", "apply", "remove"}},
		{path: "setup guard", example: "worklease setup guard --client claude-code --coverage claim", flags: []string{"client", "scope", "coverage", "session", "handle", "lease", "apply", "remove"}},
		{path: "setup instructions", example: "worklease setup instructions", flags: []string{}},
		{path: "mcp", example: "worklease mcp", flags: []string{}},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			command := root
			for _, name := range strings.Fields(test.path) {
				command = command.Command(name)
				if command == nil {
					t.Fatalf("missing command")
				}
			}
			if command.UsageText != test.pathUsage() {
				t.Fatalf("usage=%q want=%q", command.UsageText, test.pathUsage())
			}
			if !strings.Contains(command.Description, test.example) {
				t.Fatalf("description lacks executable example %q: %q", test.example, command.Description)
			}
			got := make([]string, 0, len(command.Flags))
			for _, flag := range command.Flags {
				got = append(got, flag.Names()[0])
			}
			if strings.Join(got, "\x00") != strings.Join(test.flags, "\x00") {
				t.Fatalf("flags=%v want=%v", got, test.flags)
			}
			aliases := map[string]string{"resource": "r", "provider": "p", "source": "s", "item": "i", "coordination-only": "C", "ttl": "T", "wait": "W", "agent": "a", "work-key": "w", "claim-id": "c", "token-file": "F", "token-fd": "D", "revision": "R", "operation-id": "o", "reason": "m", "max-duration": "M", "full": "f"}
			for _, flag := range command.Flags {
				wantNames := flag.Names()[0]
				if alias := aliases[wantNames]; alias != "" {
					wantNames += "\x00" + alias
				}
				if strings.Join(flag.Names(), "\x00") != wantNames {
					t.Fatalf("flag %q names=%v want=%q", flag.Names()[0], flag.Names(), wantNames)
				}
			}
		})
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
