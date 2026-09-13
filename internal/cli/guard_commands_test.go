package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/resource"
)

func TestInvokingExecReturnsArgvAndOutputInJSONAndText(t *testing.T) {
	home := t.TempDir()
	handlePath := filepath.Join(home, "handles", "exec.json")
	if err := Run(context.Background(), []string{"worklease", "acquire", "--home", home, "--handle", handlePath, "--resource", "exec-output"}, "dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		json bool
	}{
		{"json", true},
		{"text", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := []string{"worklease", "exec", "--home", home, "--handle", handlePath}
			if test.json {
				args = append(args, "--json")
			}
			args = append(args, "--", "printf", "allowed-output")
			var out bytes.Buffer
			if err := Run(context.Background(), args, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"printf", "allowed-output"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("output missing %q: %s", want, out.String())
				}
			}
		})
	}
}

func TestHookTargetsAcceptNativeFileEditorsAndRejectBash(t *testing.T) {
	tests := []struct {
		tool  string
		field string
		want  string
		ok    bool
	}{
		{tool: "Edit", field: "file_path", want: "edit.go", ok: true},
		{tool: "Write", field: "file_path", want: "write.go", ok: true},
		{tool: "MultiEdit", field: "file_path", want: "multi.go", ok: true},
		{tool: "NotebookEdit", field: "notebook_path", want: "book.ipynb", ok: true},
		{tool: "Bash", field: "command", want: "worklease status", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.tool, func(t *testing.T) {
			raw, err := json.Marshal(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			targets, err := hookTargets(hookEvent{ToolName: tc.tool, ToolInput: map[string]json.RawMessage{tc.field: raw}})
			if tc.ok {
				if err != nil || len(targets) != 1 || targets[0] != tc.want {
					t.Fatalf("targets=%v err=%v", targets, err)
				}
			} else if err == nil {
				t.Fatalf("unsupported tool accepted: %v", targets)
			}
		})
	}
}

func TestHookTargetsRejectMalformedInput(t *testing.T) {
	for _, event := range []hookEvent{
		{ToolName: "Edit", ToolInput: map[string]json.RawMessage{}},
		{ToolName: "Write", ToolInput: map[string]json.RawMessage{"file_path": json.RawMessage(`123`)}},
		{ToolName: "NotebookEdit", ToolInput: map[string]json.RawMessage{"notebook_path": json.RawMessage(`""`)}},
	} {
		if targets, err := hookTargets(event); err == nil {
			t.Fatalf("malformed event accepted: %v", targets)
		}
	}
}

func TestRealClaudeHookClaimAndPathCoverage(t *testing.T) {
	home := filepath.Join(t.TempDir(), "authority")
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	target, unrelated := filepath.Join(workspace, "owned.txt"), filepath.Join(workspace, "other.txt")
	pathKey, err := resource.Resolve(resource.Input{Path: target, WorkingDir: workspace})
	if err != nil {
		t.Fatal(err)
	}
	handlePath := filepath.Join(home, "handles", "claim.json")
	if err := Run(context.Background(), []string{"worklease", "--home", home, "acquire", "--handle", handlePath, "--resource", pathKey.Resource}, "dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, coverage, path string
		allow                bool
	}{
		{"claim", "claim", unrelated, true},
		{"owned-path", "path", target, true},
		{"unrelated-path", "path", unrelated, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			event, _ := json.Marshal(hookEvent{CWD: workspace, ToolName: "Edit", ToolInput: map[string]json.RawMessage{"file_path": json.RawMessage(strconvQuote(test.path))}})
			err := runWithStdin(t, event, []string{"worklease", "--home", home, "verify", "--handle", handlePath, "--hook", "claude-code", "--coverage", test.coverage})
			if test.allow && err != nil {
				t.Fatalf("blocked: %v", err)
			}
			if !test.allow && err == nil {
				t.Fatal("unrelated path allowed")
			}
		})
	}
}

func TestRealClaudeHookBlocksMissingPendingAndExpiredClaims(t *testing.T) {
	home, workspace := filepath.Join(t.TempDir(), "authority"), t.TempDir()
	event, _ := json.Marshal(hookEvent{CWD: workspace, ToolName: "Write", ToolInput: map[string]json.RawMessage{"file_path": json.RawMessage(strconvQuote(filepath.Join(workspace, "file.txt")))}})
	missing := filepath.Join(t.TempDir(), "missing.json")
	if err := runWithStdin(t, event, []string{"worklease", "--home", home, "verify", "--handle", missing, "--hook", "claude-code"}); err == nil {
		t.Fatal("missing claim allowed")
	}

	pending := filepath.Join(home, "handles", "pending.json")
	if err := Run(context.Background(), []string{"worklease", "--home", home, "acquire", "--handle", pending, "--resource", "pending"}, "dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	h, err := handle.Read(pending)
	if err != nil {
		t.Fatal(err)
	}
	h.PendingRequest = &handle.PendingRequest{OperationID: "pending", Kind: "exec", RequestNotAfter: time.Now().Add(time.Hour)}
	encoded, err := json.Marshal(h)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pending, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runWithStdin(t, event, []string{"worklease", "--home", home, "verify", "--handle", pending, "--hook", "claude-code"}); err == nil {
		t.Fatal("pending claim allowed")
	}

	expired := filepath.Join(home, "handles", "expired.json")
	if err := Run(context.Background(), []string{"worklease", "--home", home, "acquire", "--handle", expired, "--resource", "expired", "--ttl", "1s"}, "dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if err := runWithStdin(t, event, []string{"worklease", "--home", home, "verify", "--handle", expired, "--hook", "claude-code"}); err == nil {
		t.Fatal("expired claim allowed")
	}
}

func runWithStdin(t *testing.T, data []byte, args []string) error {
	t.Helper()
	input := filepath.Join(t.TempDir(), "stdin.json")
	if err := os.WriteFile(input, data, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = file
	defer func() { os.Stdin = previous; _ = file.Close() }()
	return Run(context.Background(), args, "dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
}

func strconvQuote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

// After the authority commits a started intent, the pending request is the
// only exact record of that operation. A completion-stage failure whose reason
// would otherwise prove no commit (stale-revision) must not clear it.
func TestGuardLifecycleKeepsPendingRequestAfterStart(t *testing.T) {
	home := t.TempDir()
	handles := filepath.Join(home, "handles")
	if err := os.Mkdir(handles, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(handles, "ctx-"+strings.Repeat("0", 64)+".json")
	authority, claim := strings.Repeat("a", 32), strings.Repeat("b", 32)
	h := handle.Handle{SchemaVersion: 1, AuthorityID: authority, ClaimID: claim, Token: strings.Repeat("c", 64), Revision: 3, Resources: []string{"guarded"}, ExpiresAt: time.Now().Add(time.Hour), AgentID: "agent", SessionID: "session", LocalReplaceAllowed: true, State: "pending", PendingRequest: &handle.PendingRequest{OperationID: strings.Repeat("d", 32), Kind: "exec", AuthorityID: authority, ClaimID: claim, RequestHash: strings.Repeat("e", 64), RequestNotAfter: time.Now().Add(time.Hour), Inputs: map[string]any{"argv": []any{"true"}}}}
	if err := handle.Write(path, h); err != nil {
		t.Fatal(err)
	}
	lifecycle := guardLifecycle(path, &h)
	lifecycle.Failure(reason.New(reason.ReasonStaleRevision, "stale"), true)
	after, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != "pending" || after.PendingRequest == nil {
		t.Fatalf("post-start failure cleared pending request: state=%s", after.State)
	}
	lifecycle.Failure(reason.New(reason.ReasonStaleRevision, "stale"), false)
	after, err = handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != "ready" || after.PendingRequest != nil || after.Revision != 3 {
		t.Fatalf("pre-start definitive failure must restore the ready handle: state=%s revision=%d", after.State, after.Revision)
	}
}

// mutationFailure must not overwrite a commit state the guard already
// established after its started intent committed.
func TestMutationFailurePreservesGuardCommitState(t *testing.T) {
	err := mutationFailure(reason.New(reason.ReasonStaleRevision, "stale").With("commitState", "unknown"), "claim", "op", "/p")
	if got := reason.As(err).Details["commitState"]; got != "unknown" {
		t.Fatalf("commitState=%v", got)
	}
	err = mutationFailure(reason.New(reason.ReasonStaleRevision, "stale"), "claim", "op", "/p")
	if got := reason.As(err).Details["commitState"]; got != "not-committed" {
		t.Fatalf("unclassified stale-revision commitState=%v", got)
	}
}
