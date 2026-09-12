package guard

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/store"
)

func TestGuardHelperProcess(t *testing.T) {
	if os.Getenv("WORKLEASE_GO_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		os.Exit(2)
	}
	switch args[1] {
	case "argv":
		fmt.Fprint(os.Stdout, strings.Join(args[2:], "|"))
	case "invalid":
		_, _ = os.Stdout.Write([]byte{'a', 0xff, 'b'})
	case "large":
		_, _ = os.Stdout.Write([]byte(strings.Repeat("x", MaxCaptureBytes+257)))
	case "signal":
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}
	os.Exit(0)
}

func acquireExecTest(t *testing.T, resource, claim, token string) (*store.Store, *lease.Service, lease.Credentials) {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc := lease.New(st, nil, nil, lease.Defaults{TTL: 2 * time.Second})
	grant, err := svc.Acquire(context.Background(), lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Resources: []string{resource}, AgentID: "agent", SessionID: "session", TTL: time.Second, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	return st, svc, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Revision: grant.Revision}
}

func TestExecLiteralArgvBoundedInvalidUTF8SignalAndEnvironment(t *testing.T) {
	t.Setenv("WORKLEASE_GO_HELPER", "1")
	t.Setenv("WORKLEASE_TOKEN", "must-not-leak")
	t.Setenv("GIT_DIR", "must-not-leak")
	cases := []struct {
		name     string
		args     []string
		wantCode int
		check    func(*testing.T, ExecResult)
	}{
		{name: "literal-argv", args: []string{"argv", "a b", "$(false)", ";"}, check: func(t *testing.T, result ExecResult) {
			if got := result.Receipt.Result["stdout"]; got != "a b|$(false)|;" {
				t.Fatalf("stdout=%q", got)
			}
		}},
		{name: "invalid-utf8", args: []string{"invalid"}, check: func(t *testing.T, result ExecResult) {
			if got := result.Receipt.Result["stdout"]; got != "a\uFFFDb" {
				t.Fatalf("stdout=%q", got)
			}
		}},
		{name: "bounded", args: []string{"large"}, check: func(t *testing.T, result ExecResult) {
			if got := result.Receipt.Result["stdoutBytes"]; got != MaxCaptureBytes+257 {
				t.Fatalf("stdoutBytes=%v", got)
			}
			if got := result.Receipt.Result["stdoutTruncated"]; got != true {
				t.Fatalf("truncated=%v", got)
			}
		}},
		{name: "signal", args: []string{"signal"}, wantCode: 128 + int(syscall.SIGTERM), check: func(*testing.T, ExecResult) {}},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claim := fmt.Sprintf("%032x", i+10)
			token := fmt.Sprintf("%064x", i+10)
			st, svc, creds := acquireExecTest(t, tc.name, claim, token)
			defer st.Close()
			argv := append([]string{os.Args[0], "-test.run=TestGuardHelperProcess", "--"}, tc.args...)
			result, err := Exec(context.Background(), svc, creds, ExecRequest{OperationID: fmt.Sprintf("%032x", i+20), Argv: argv, CWD: t.TempDir(), MaxDuration: 5 * time.Second, TTL: time.Second, RequestNotAfter: time.Now().Add(time.Hour)})
			if err != nil {
				t.Fatal(err)
			}
			if result.ExitCode != tc.wantCode {
				t.Fatalf("exit=%d want=%d", result.ExitCode, tc.wantCode)
			}
			if strings.Contains(fmt.Sprint(result.Receipt.Result), "must-not-leak") {
				t.Fatal("credential or git environment leaked")
			}
			tc.check(t, result)
		})
	}
}

func TestGitPrimaryResolvesLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "primary")
	linked := filepath.Join(root, "linked")
	if err := os.Mkdir(primary, 0o700); err != nil {
		t.Fatal(err)
	}
	commands := [][]string{
		{"init", "-q", primary},
		{"-C", primary, "config", "user.email", "test@example.com"},
		{"-C", primary, "config", "user.name", "Test"},
		{"-C", primary, "commit", "--allow-empty", "-qm", "init"},
		{"-C", primary, "worktree", "add", "-q", "-b", "linked-test", linked},
	}
	for _, args := range commands {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	got, isolated, err := gitPrimary(linked)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(primary)
	if err != nil {
		t.Fatal(err)
	}
	if !isolated || got != want {
		t.Fatalf("primary=%q isolated=%v want=%q", got, isolated, want)
	}
}

func TestExecUsesRequestedCWDAndIsolatesGitEnvironment(t *testing.T) {
	requested, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st, svc, creds := acquireExecTest(t, "cwd", strings.Repeat("c", 32), strings.Repeat("d", 64))
	defer st.Close()
	t.Setenv("GIT_DIR", "must-not-leak")
	result, err := Exec(context.Background(), svc, creds, ExecRequest{OperationID: strings.Repeat("e", 32), Argv: []string{"env"}, CWD: requested, MaxDuration: time.Second, TTL: time.Second, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Receipt.Result["stdout"].(string), "GIT_DIR=") {
		t.Fatal("GIT_DIR leaked")
	}
	if got := result.Receipt.Result["executionDirectory"].(map[string]any)["path"]; got != requested {
		t.Fatalf("cwd=%v", got)
	}
}

func TestExecUsesArgvDevNullAndCompletesReceipt(t *testing.T) {
	st, err := store.Open(context.Background(), t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, nil, nil, lease.Defaults{TTL: 2 * time.Second})
	token := strings.Repeat("a", 64)
	claim := strings.Repeat("1", 32)
	g, err := svc.Acquire(context.Background(), lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Resources: []string{"exec-test"}, AgentID: "agent", SessionID: "session", TTL: time.Second, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Exec(context.Background(), svc, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Revision: g.Revision}, ExecRequest{OperationID: strings.Repeat("2", 32), Argv: []string{"sh", "-c", "printf '%s' \"$WORKLEASE_CLAIM_ID\"; read value; printf '%s' \"$value\" >&2"}, MaxDuration: time.Second, TTL: time.Second, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || result.Receipt.Result["stdout"] != claim {
		t.Fatalf("result=%+v", result)
	}
	if result.Receipt.Result["stderr"] != "" {
		t.Fatalf("stdin was not /dev/null: %+v", result.Receipt.Result)
	}
}

func TestExecLeaderExitReapsProcessGroupDescendant(t *testing.T) {
	st, err := store.Open(context.Background(), t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, nil, nil, lease.Defaults{TTL: 5 * time.Second})
	token, claim := strings.Repeat("c", 64), strings.Repeat("6", 32)
	g, err := svc.Acquire(context.Background(), lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Resources: []string{"group-test"}, AgentID: "agent", SessionID: "session", TTL: 5 * time.Second, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	marker := t.TempDir() + "/descendant-finished"
	_, err = Exec(context.Background(), svc, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Revision: g.Revision}, ExecRequest{OperationID: strings.Repeat("7", 32), Argv: []string{"sh", "-c", "(sleep 1; touch '" + marker + "') & exit 0"}, MaxDuration: time.Second, TTL: 5 * time.Second, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("process-group descendant survived leader exit")
	}
}

func TestExecTimeoutLeavesOperationUnresolved(t *testing.T) {
	st, err := store.Open(context.Background(), t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, nil, nil, lease.Defaults{TTL: 5 * time.Second})
	token := strings.Repeat("b", 64)
	claim := strings.Repeat("3", 32)
	g, err := svc.Acquire(context.Background(), lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Resources: []string{"timeout-test"}, AgentID: "agent", SessionID: "session", TTL: 5 * time.Second, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = Exec(context.Background(), svc, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Revision: g.Revision}, ExecRequest{OperationID: strings.Repeat("4", 32), Argv: []string{"sh", "-c", "sleep 10"}, MaxDuration: 30 * time.Millisecond, TTL: 5 * time.Second, RequestNotAfter: time.Now().Add(time.Hour)})
	if err == nil || err.Error() == "" {
		t.Fatal("timeout should fail")
	}
	if _, err = svc.BeginOperation(context.Background(), lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Revision: g.Revision + 1}, lease.OperationIntent{OperationID: strings.Repeat("5", 32), Kind: "exec", Request: map[string]any{"argv": []string{"true"}}, TTL: time.Second, RequestNotAfter: time.Now().Add(time.Hour)}); err == nil {
		t.Fatal("unresolved timeout should block a second operation")
	}
}
