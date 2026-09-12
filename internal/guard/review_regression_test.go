package guard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/resource"
	"github.com/brettinternet/worklease/internal/store"
)

// A child that exits while the guard's own renewal is in flight must still
// complete with its real exit status. Before the fix, completion raced the
// renewal's revision bump and failed stale-revision, stranding the finished
// child as an unresolved operation.
func TestExecCompletesWhenChildExitsDuringRenewal(t *testing.T) {
	st, err := store.Open(context.Background(), t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, nil, nil, lease.Defaults{TTL: 2 * time.Second})
	token := strings.Repeat("a", 64)
	claim := strings.Repeat("1", 32)
	g, err := svc.Acquire(context.Background(), lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Resources: []string{"exec-renew-race"}, AgentID: "agent", SessionID: "session", TTL: 2 * time.Second, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	creds := lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Revision: g.Revision}
	for i, opID := range []string{strings.Repeat("2", 32), strings.Repeat("3", 32)} {
		// TTL 2s renews at 1s; the child exits within the renewal window.
		result, err := Exec(context.Background(), svc, creds, ExecRequest{OperationID: opID, Argv: []string{"sh", "-c", "sleep 0.995"}, MaxDuration: 10 * time.Second, TTL: 2 * time.Second, RequestNotAfter: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatalf("iteration %d: exec failed after child completed: %v", i, err)
		}
		if result.ExitCode != 0 || !result.Receipt.Committed {
			t.Fatalf("iteration %d: exit=%d committed=%v", i, result.ExitCode, result.Receipt.Committed)
		}
		creds.Revision = result.Receipt.Revision
		status, err := svc.Status(context.Background(), lease.Selector{ClaimID: claim})
		if err != nil || status.Claim == nil {
			t.Fatalf("status: %v", err)
		}
		if len(status.Claim.UnknownOperations) != 0 {
			t.Fatalf("iteration %d: finished child left unresolved operations %v", i, status.Claim.UnknownOperations)
		}
	}
}

// Once the started intent is committed, every failure reports commitState
// unknown and tells the lifecycle the operation started, so adapters retain
// the exact pending request instead of clearing it.
func TestExecPostStartFailureReportsUnknownAndStartedLifecycle(t *testing.T) {
	st, svc, creds := acquireExecTest(t, "exec-post-start", strings.Repeat("5", 32), strings.Repeat("b", 64))
	defer st.Close()
	var failures []struct {
		err     error
		started bool
	}
	lifecycle := &OperationLifecycle{Failure: func(err error, started bool) {
		failures = append(failures, struct {
			err     error
			started bool
		}{err, started})
	}}
	_, err := Exec(context.Background(), svc, creds, ExecRequest{OperationID: strings.Repeat("6", 32), Argv: []string{"sh", "-c", "sleep 10"}, MaxDuration: 30 * time.Millisecond, TTL: 5 * time.Second, RequestNotAfter: time.Now().Add(time.Hour), Lifecycle: lifecycle})
	classified := reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonChildTimeout {
		t.Fatalf("timeout error=%v", err)
	}
	if classified.Details["commitState"] != "unknown" || classified.Details["operationId"] != strings.Repeat("6", 32) {
		t.Fatalf("post-start failure details=%v", classified.Details)
	}
	if len(failures) != 1 || !failures[0].started {
		t.Fatalf("lifecycle failures=%+v", failures)
	}

	// A pre-start failure (stale revision before dispatch) reports started=false.
	failures = nil
	stale := creds
	stale.Revision = 999
	_, err = Exec(context.Background(), svc, stale, ExecRequest{OperationID: strings.Repeat("7", 32), Argv: []string{"true"}, MaxDuration: time.Second, TTL: time.Second, RequestNotAfter: time.Now().Add(time.Hour), Lifecycle: lifecycle})
	if got := reason.As(err); got == nil || got.Reason != reason.ReasonOperationInProgress && got.Reason != reason.ReasonStaleRevision {
		t.Fatalf("pre-start error=%v", err)
	}
	if len(failures) != 1 || failures[0].started {
		t.Fatalf("pre-start lifecycle failures=%+v", failures)
	}
}

// A temporary-file failure before rename is a proven no-effect failure. It
// must record a completed failure that frees the started slot rather than
// leaving the operation unresolved.
func TestReplaceFileTempCreationFailureRecordsCompletedFailure(t *testing.T) {
	root := t.TempDir()
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	targetDir := filepath.Join(root, "locked")
	if err := os.Mkdir(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "target.txt")
	content := filepath.Join(root, "content.txt")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(content, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := resource.Resolve(resource.Input{Path: target})
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), filepath.Join(root, "state"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, nil, nil, lease.Defaults{TTL: time.Minute})
	token := strings.Repeat("c", 64)
	claim := strings.Repeat("8", 32)
	grant, err := svc.Acquire(context.Background(), lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Resources: []string{key.Resource}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour), LocalReplaceAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(targetDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(targetDir, 0o700) })
	sum := sha256.Sum256([]byte("old"))
	var started bool
	lifecycle := &OperationLifecycle{Failure: func(err error, wasStarted bool) { started = wasStarted }}
	_, err = ReplaceFile(context.Background(), svc, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Revision: grant.Revision}, ReplaceRequest{
		OperationID: strings.Repeat("9", 32), Path: target, ExpectedSHA256: hex.EncodeToString(sum[:]), ContentFile: content,
		TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour), Lifecycle: lifecycle,
	})
	if got := reason.As(err); got == nil || got.Reason != reason.ReasonInvalidPath {
		t.Fatalf("temp creation error=%v", err)
	}
	if started {
		t.Fatal("completed failure must not report a lingering started operation")
	}
	status, err := svc.Status(context.Background(), lease.Selector{ClaimID: claim})
	if err != nil || status.Claim == nil {
		t.Fatalf("status: %v", err)
	}
	if len(status.Claim.UnknownOperations) != 0 {
		t.Fatalf("no-effect failure left unresolved operations %v", status.Claim.UnknownOperations)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "old" {
		t.Fatalf("target changed: %q %v", got, err)
	}
}
