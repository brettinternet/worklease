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

func TestHistoryWithoutResourceShowsRecentEventsAndTextOmitsCursor(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	st, err := store.Open(ctx, home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc := lease.New(st, nil, nil, lease.Defaults{TTL: time.Minute})
	if _, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: strings.Repeat("1", 32), Token: strings.Repeat("a", 64), Resources: []string{"r"}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	run := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		if err := Run(ctx, append([]string{"worklease"}, args...), "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	eventsText := run("events", "--home", home)
	if historyText := run("history", "--home", home); historyText != eventsText {
		t.Fatalf("history=%q events=%q", historyText, eventsText)
	}
	if strings.Contains(eventsText, "nextCursor:") {
		t.Fatalf("text exposed opaque cursor: %q", eventsText)
	}
	eventsJSON := run("events", "--json", "--home", home)
	historyJSON := run("history", "--json", "--home", home)
	if historyJSON != eventsJSON {
		t.Fatalf("history=%q events=%q", historyJSON, eventsJSON)
	}
	var historyEnvelope map[string]any
	if err := json.Unmarshal([]byte(historyJSON), &historyEnvelope); err != nil {
		t.Fatal(err)
	}
	if historyEnvelope["operation"] != "events" || historyEnvelope["events"] == nil || historyEnvelope["epochs"] != nil {
		t.Fatalf("history alias envelope=%#v", historyEnvelope)
	}
	if !strings.Contains(eventsJSON, `"nextCursor":"`) {
		t.Fatalf("JSON omitted cursor: %q", eventsJSON)
	}
	if resourceHistory := run("history", "--json", "--home", home, "--resource", "r"); !strings.Contains(resourceHistory, `"operation":"history"`) || !strings.Contains(resourceHistory, `"epochs":[`) {
		t.Fatalf("resource history=%q", resourceHistory)
	}
}

func TestSameHandleReconciliationAdoptsCurrentRevisionAndRestoresLifecycle(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	st, err := store.Open(ctx, home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc := lease.New(st, nil, nil, lease.Defaults{TTL: time.Minute})
	claimID, token := strings.Repeat("1", 32), strings.Repeat("a", 64)
	grant, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Resources: []string{"r"}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	targetID := strings.Repeat("2", 32)
	deadline := time.Now().Add(time.Hour).UTC()
	started, err := svc.BeginOperation(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Revision: grant.Revision}, lease.OperationIntent{OperationID: targetID, Kind: "exec", Request: map[string]any{"argv": []any{"sleep", "5"}}, TTL: time.Minute, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	handlePath := filepath.Join(home, "handles", "same-claim.json")
	h := handle.Handle{SchemaVersion: 1, AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Revision: grant.Revision, Resources: []string{"r"}, ExpiresAt: grant.ExpiresAt, AgentID: "agent", SessionID: "session", LocalReplaceAllowed: true, State: "pending", PendingRequest: &handle.PendingRequest{OperationID: targetID, Kind: "exec", AuthorityID: st.AuthorityID(), ClaimID: claimID, RequestHash: started.RequestHash, RequestNotAfter: deadline, Inputs: map[string]any{"argv": []any{"sleep", "5"}}}}
	if err := handle.Write(handlePath, h); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reconcileID := strings.Repeat("3", 32)
	reconcile := func(expected, evidence string) error {
		var out bytes.Buffer
		args := []string{"worklease", "op", "reconcile", "--json", "--home", home, "--handle", handlePath, "--operation-id", reconcileID, "--request-not-after", deadline.Format(time.RFC3339Nano), "--ttl", "1m", "--target-claim-id", claimID, "--target-operation-id", targetID, "--outcome", "observed-failure", "--evidence", evidence, "--expected-request-sha256", expected}
		return Run(ctx, args, "dev", "unknown", "unknown", &out, &bytes.Buffer{})
	}
	if err := reconcile(strings.Repeat("f", 64), `{"outcome":"observed-failure","executorStopped":true}`); err == nil {
		t.Fatal("wrong request hash succeeded")
	}
	// Simulate a reconciliation commit followed by a crash before the pending
	// handle is updated, then a concurrent valid renewal before exact recovery.
	st, err = store.Open(ctx, home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc = lease.New(st, nil, nil, lease.Defaults{TTL: time.Minute})
	reconciled, err := svc.ReconcileAtCurrentRevision(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Revision: grant.Revision}, lease.ReconcileRequest{OperationID: reconcileID, TargetClaimID: claimID, TargetOperationID: targetID, ExpectedRequestSHA256: started.RequestHash, Outcome: "observed-failure", Evidence: json.RawMessage(`{"outcome":"observed-failure","executorStopped":true}`), TTL: time.Minute, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := svc.Heartbeat(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Revision: reconciled.Revision}, lease.Renew{OperationID: strings.Repeat("4", 32), TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reconcile(started.RequestHash, `{"outcome":"observed-failure","executorStopped":true}`); err != nil {
		t.Fatal(err)
	}
	after, err := handle.Read(handlePath)
	if err != nil || after.State != "ready" || after.PendingRequest != nil || after.RecoveryRequest != nil || after.Revision != renewed.Revision {
		t.Fatalf("reconciled handle=%+v err=%v", after, err)
	}
	for _, args := range [][]string{
		{"worklease", "heartbeat", "--json", "--home", home, "--handle", handlePath},
		{"worklease", "checkpoint", "--json", "--home", home, "--handle", handlePath, "--data", `{"phase":"recovered"}`},
		{"worklease", "exec", "--json", "--home", home, "--handle", handlePath, "--", "true"},
	} {
		if err := Run(ctx, args, "dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if err := reconcile(started.RequestHash, `{"outcome":"observed-failure","executorStopped":true,"changed":true}`); err == nil {
		t.Fatal("changed evidence replay succeeded")
	}

	// A committed replay cannot make an ended claim ready, but it must remain
	// classified as committed and retain the exact recovery request.
	after, err = handle.Read(handlePath)
	if err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(ctx, home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc = lease.New(st, nil, nil, lease.Defaults{TTL: time.Minute})
	if _, err := svc.Release(ctx, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Revision: after.Revision}, lease.ReleaseRequest{OperationID: strings.Repeat("5", 32), Reason: "ended before replay", RequestNotAfter: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	after.State = "pending"
	after.PendingRequest = h.PendingRequest
	after.RecoveryRequest = nil
	if err := handle.Write(handlePath, after); err != nil {
		t.Fatal(err)
	}
	var replayOut bytes.Buffer
	replayArgs := []string{"worklease", "op", "reconcile", "--json", "--home", home, "--handle", handlePath, "--operation-id", reconcileID, "--request-not-after", deadline.Format(time.RFC3339Nano), "--ttl", "1m", "--target-claim-id", claimID, "--target-operation-id", targetID, "--outcome", "observed-failure", "--evidence", `{"outcome":"observed-failure","executorStopped":true}`, "--expected-request-sha256", started.RequestHash}
	if err := Run(ctx, replayArgs, "dev", "unknown", "unknown", &replayOut, &bytes.Buffer{}); err == nil || !strings.Contains(replayOut.String(), `"commitState":"committed"`) {
		t.Fatalf("ended replay out=%s err=%v", replayOut.String(), err)
	}
	after, err = handle.Read(handlePath)
	if err != nil || after.State != "pending" || after.PendingRequest == nil || after.RecoveryRequest == nil {
		t.Fatalf("ended replay handle=%+v err=%v", after, err)
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
	if err := st.Write(ctx, func(tx *store.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE claims SET expires_at=? WHERE claim_id=?`, time.Now().Add(-time.Second).UnixMicro(), oldID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
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
	tokenDir := t.TempDir()
	if err := os.Chmod(tokenDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(tokenDir, "token")
	if err := os.WriteFile(tokenPath, []byte(oldToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"worklease", "op", "inspect", "--json", "--home", home, "--claim-id", oldID, "--operation-id", target, "--token-file", tokenPath, "--full"},
		{"worklease", "op", "inspect", "--home", home, "--claim-id", oldID, "--operation-id", target, "--token-file", tokenPath, "--full"},
	} {
		var privateInspection bytes.Buffer
		if err := Run(ctx, args, "dev", "unknown", "unknown", &privateInspection, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(privateInspection.String(), "executorStopped") || strings.Contains(privateInspection.String(), oldToken) {
			t.Fatalf("private inspection=%s", privateInspection.String())
		}
	}
	var events bytes.Buffer
	if err := Run(ctx, []string{"worklease", "events", "--json", "--home", home, "--limit", "10"}, "dev", "unknown", "unknown", &events, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(events.String(), "reconciled") || strings.Contains(events.String(), "executorStopped") {
		t.Fatalf("events=%s", events.String())
	}
}
