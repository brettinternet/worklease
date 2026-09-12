package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/store"
)

func TestEventsCLIRejectsMalformedCursorBeforeOpeningStorage(t *testing.T) {
	home := filepath.Join(t.TempDir(), "not-created")
	var out bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "events", "--json", "--home", home, "--cursor", "bad"}, "dev", "unknown", "unknown", &out, &bytes.Buffer{})
	if err == nil || !strings.Contains(out.String(), `"reason":"cursor-invalid"`) {
		t.Fatalf("out=%s err=%v", out.String(), err)
	}
	if _, statErr := os.Stat(home); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("storage opened: %v", statErr)
	}
}

func TestLedgerCLIJSONAndPendingHandleReconciliationRecovery(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	st, err := store.Open(ctx, home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc := lease.New(st, nil, nil, lease.Defaults{TTL: time.Minute})
	oldID, oldToken := strings.Repeat("1", 32), strings.Repeat("a", 64)
	old, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: oldID, Token: oldToken, Resources: []string{"r"}, AgentID: "old", SessionID: "old", TTL: time.Second, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	target := strings.Repeat("2", 32)
	started, err := svc.BeginOperation(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: oldID, Token: oldToken, Revision: old.Revision}, lease.OperationIntent{OperationID: target, Kind: "exec", Request: map[string]any{"cwd": "/tmp"}, TTL: time.Second, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	newID, newToken := strings.Repeat("3", 32), strings.Repeat("b", 64)
	current, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: newID, Token: newToken, Resources: []string{"r"}, AgentID: "new", SessionID: "new", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	handlePath := filepath.Join(home, "handles", "recovery.json")
	pendingDeadline := time.Now().Add(time.Hour)
	h := handle.Handle{SchemaVersion: 1, AuthorityID: current.AuthorityID, ClaimID: newID, Token: newToken, Revision: current.Revision, Resources: []string{"r"}, ExpiresAt: current.ExpiresAt, AgentID: "new", SessionID: "new", LocalReplaceAllowed: true, State: "pending", PendingRequest: &handle.PendingRequest{OperationID: strings.Repeat("5", 32), Kind: "exec", AuthorityID: current.AuthorityID, ClaimID: newID, RequestHash: strings.Repeat("c", 64), RequestNotAfter: pendingDeadline, Inputs: map[string]any{"kind": "exec"}}}
	if err := handle.Write(handlePath, h); err != nil {
		t.Fatal(err)
	}
	run := func(expected string) (map[string]any, error) {
		var out, stderr bytes.Buffer
		args := []string{"worklease", "op", "reconcile", "--json", "--home", home, "--handle", handlePath, "--target-claim-id", oldID, "--target-operation-id", target, "--outcome", "observed-success", "--evidence", `{"outcome":"observed-success","executorStopped":true}`, "--expected-request-sha256", expected}
		err := Run(ctx, args, "dev", "unknown", "unknown", &out, &stderr)
		var result map[string]any
		if json.Unmarshal(out.Bytes(), &result) != nil {
			t.Fatalf("out=%s stderr=%s", out.String(), stderr.String())
		}
		return result, err
	}
	if result, err := run(strings.Repeat("f", 64)); err == nil || result["ok"] != false {
		t.Fatalf("wrong hash result=%v err=%v", result, err)
	}
	after, err := handle.Read(handlePath)
	if err != nil {
		t.Fatal(err)
	}
	if after.PendingRequest == nil || after.RecoveryRequest != nil {
		t.Fatalf("wrong hash clearing=%+v", after)
	}
	result, err := run(started.RequestHash)
	if err != nil || result["ok"] != true {
		t.Fatalf("result=%v err=%v", result, err)
	}
	after, err = handle.Read(handlePath)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != "ready" || after.PendingRequest != nil || after.RecoveryRequest != nil || after.Revision != current.Revision+1 {
		t.Fatalf("after=%+v", after)
	}
	var inspection bytes.Buffer
	if err := Run(ctx, []string{"worklease", "op", "inspect", "--json", "--home", home, "--claim-id", oldID, "--operation-id", target}, "dev", "unknown", "unknown", &inspection, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inspection.String(), `"inspection"`) || strings.Contains(inspection.String(), "executorStopped") {
		t.Fatalf("inspection=%s", inspection.String())
	}
	var events bytes.Buffer
	if err := Run(ctx, []string{"worklease", "events", "--json", "--home", home, "--limit", "10"}, "dev", "unknown", "unknown", &events, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(events.String(), "reconciled") || strings.Contains(events.String(), "executorStopped") {
		t.Fatalf("events=%s", events.String())
	}
}
