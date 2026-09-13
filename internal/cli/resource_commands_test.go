package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/store"
)

func TestHandleCLIProcessHelper(t *testing.T) {
	if os.Getenv("WORKLEASE_HANDLE_TEST_HELPER") != "1" {
		return
	}
	separator := 0
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i + 1
			break
		}
	}
	if separator == 0 || separator >= len(os.Args) {
		os.Exit(64)
	}
	if err := Run(context.Background(), os.Args[separator:], "dev", "unknown", "unknown", os.Stdout, os.Stderr); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func runHandleProcess(t *testing.T, args []string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestHandleCLIProcessHelper$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "WORKLEASE_HANDLE_TEST_HELPER=1")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("helper timed out: %v", args)
	}
	if err == nil {
		return 0, string(output)
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), string(output)
	}
	t.Fatalf("start helper: %v", err)
	return -1, ""
}

func TestKeyAndPolicyCommandsReportContractMetadata(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "key", "--json", "--provider", "generic", "--source", "source", "--item", "item"}, "dev", "unknown", "unknown", &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	var key map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &key); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"resource", "provider", "capability", "scope", "identityScope", "localReplaceAllowed", "providerFencing"} {
		if _, ok := key[field]; !ok {
			t.Errorf("key missing %s", field)
		}
	}
	if key["providerFencing"] != false || key["localReplaceAllowed"] != false {
		t.Fatalf("metadata = %#v", key)
	}
	stdout.Reset()
	if err := Run(context.Background(), []string{"worklease", "key", "--json", "-r", "opaque"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var direct map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &direct); err != nil {
		t.Fatal(err)
	}
	if direct["localReplaceAllowed"] != true || direct["identityScope"] != "portable" {
		t.Fatalf("direct metadata = %#v", direct)
	}
	stdout.Reset()
	if err := Run(context.Background(), []string{"worklease", "key", "--json", "-r", "opaque", "--coordination-only"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var coordination map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &coordination); err != nil {
		t.Fatal(err)
	}
	if coordination["localReplaceAllowed"] != false || coordination["capability"] != "local-coordination" || coordination["providerFencing"] != false {
		t.Fatalf("coordination metadata = %#v", coordination)
	}
	stdout.Reset()
	err = Run(context.Background(), []string{"worklease", "policy", "list", "--json"}, "dev", "unknown", "unknown", &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	var policies map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &policies); err != nil {
		t.Fatal(err)
	}
	if len(policies["policies"].([]any)) != 6 {
		t.Fatalf("policies = %#v", policies)
	}
	for _, name := range []string{"backlog-md", "markdown", "github", "linear", "generic", "path"} {
		if !strings.Contains(stdout.String(), name) {
			t.Errorf("missing policy %s", name)
		}
	}
	stdout.Reset()
	if err := Run(context.Background(), []string{"worklease", "policy", "list"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"PROVIDER", "SCOPE", "CAPABILITY", "IDENTITY", "backlog-md", "path"} {
		if !strings.Contains(stdout.String(), field) {
			t.Errorf("text policy list missing %s", field)
		}
	}
	if strings.Contains(stdout.String(), "{\"") {
		t.Fatalf("text policy list contains raw JSON: %q", stdout.String())
	}
	stdout.Reset()
	if err := Run(context.Background(), []string{"worklease", "policy", "describe", "path", "--json"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var description map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &description); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"resource", "provider", "capability", "scope", "identityScope", "localReplaceAllowed", "providerFencing"} {
		if _, ok := description[field]; !ok {
			t.Errorf("JSON policy description missing %s", field)
		}
	}
	stdout.Reset()
	if err := Run(context.Background(), []string{"worklease", "policy", "describe", "path"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"policy path", "resource:", "scope:", "capability:", "identityScope:"} {
		if !strings.Contains(stdout.String(), field) {
			t.Errorf("text description missing %s: %q", field, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "localReplaceAllowed:") {
		t.Fatalf("compact policy description included full metadata: %q", stdout.String())
	}
	stdout.Reset()
	if err := Run(context.Background(), []string{"worklease", "policy", "describe", "path", "--full"}, "dev", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"localReplaceAllowed:", "providerFencing:", "contractVersion:", "keyPolicyVersion:"} {
		if !strings.Contains(stdout.String(), field) {
			t.Errorf("full text description missing %s: %q", field, stdout.String())
		}
	}
}

func TestResourceInputResolverRejectsDuplicateAndMixedModesBeforeAcquire(t *testing.T) {
	cases := [][]string{
		{"worklease", "acquire", "--json", "-r", "one", "-r", "one"},
		{"worklease", "acquire", "--json", "-r", "one", "--provider", "generic", "--source", "s", "--item", "i"},
		{"worklease", "acquire", "--json", "--provider", "generic", "--source", "s"},
	}
	for _, args := range cases {
		var stdout bytes.Buffer
		err := Run(context.Background(), args, "dev", "unknown", "unknown", &stdout, &bytes.Buffer{})
		if err == nil {
			t.Fatalf("%v unexpectedly succeeded", args)
		}
		var envelope map[string]any
		if e := json.Unmarshal(stdout.Bytes(), &envelope); e != nil {
			t.Fatalf("%v output: %v", args, e)
		}
		failure := envelope["error"].(map[string]any)
		if failure["reason"] != "resource-input-conflict" && failure["reason"] != "invalid-resource" {
			t.Fatalf("%v reason=%v", args, failure["reason"])
		}
	}
}

func TestLifecycleRejectsMixedCredentialSelection(t *testing.T) {
	home, tokenPath := t.TempDir(), filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenPath, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	args := []string{"worklease", "heartbeat", "--json", "--home", home, "--handle", "lease.json", "--claim-id", strings.Repeat("1", 32), "--token-file", tokenPath, "--revision", "1", "--operation-id", strings.Repeat("2", 32), "--request-not-after", time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}
	err := Run(context.Background(), args, "dev", "unknown", "unknown", &stdout, &bytes.Buffer{})
	if err == nil || !strings.Contains(stdout.String(), `"reason":"credential-source-conflict"`) {
		t.Fatalf("mixed selection err=%v output=%q", err, stdout.String())
	}
}

func TestHandlelessTransferIsRejected(t *testing.T) {
	home, credentialDir := t.TempDir(), t.TempDir()
	if err := os.Chmod(credentialDir, 0o700); err != nil {
		t.Fatal(err)
	}
	currentTokenPath := filepath.Join(credentialDir, "current")
	if err := os.WriteFile(currentTokenPath, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	claimID := strings.Repeat("1", 32)
	acquire := []string{"worklease", "acquire", "--json", "--home", home, "--resource", "r", "--no-handle", "--claim-id", claimID, "--session", "stateless-session", "--token-file", currentTokenPath, "--request-not-after", deadline}
	if err := Run(context.Background(), acquire, "dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	transfer := []string{"worklease", "transfer", "--json", "--home", home, "--claim-id", claimID, "--token-file", currentTokenPath, "--revision", "1", "--operation-id", strings.Repeat("3", 32), "--request-not-after", deadline, "--to-agent", "next", "--to-session", "next-session"}
	var transferOut bytes.Buffer
	if err := Run(context.Background(), transfer, "dev", "unknown", "unknown", &transferOut, &bytes.Buffer{}); err == nil || !strings.Contains(transferOut.String(), "successor-handle") {
		t.Fatalf("handle-less transfer err=%v output=%q", err, transferOut.String())
	}
	var statusOut bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "status", "--json", "--home", home}, "dev", "unknown", "unknown", &statusOut, &bytes.Buffer{}); err == nil || !strings.Contains(statusOut.String(), `"reason":"claim-selection-missing"`) {
		t.Fatalf("empty status err=%v output=%q", err, statusOut.String())
	}
}

func TestExplicitCredentialsTransferKeepsStableJSONEnvelope(t *testing.T) {
	home, credentialDir := t.TempDir(), t.TempDir()
	if err := os.Chmod(credentialDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(credentialDir, "current")
	if err := os.WriteFile(tokenPath, []byte(strings.Repeat("a", 64)), 0o600); err != nil {
		t.Fatal(err)
	}
	claimID := strings.Repeat("1", 32)
	deadline := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	if err := Run(context.Background(), []string{"worklease", "acquire", "--json", "--home", home, "--resource", "explicit-transfer", "--no-handle", "--claim-id", claimID, "--session", "stateless-session", "--token-file", tokenPath, "--request-not-after", deadline}, "dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	successor := filepath.Join(home, "handles", "ctx-"+strings.Repeat("d", 64)+".json")
	var out bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "transfer", "--json", "--home", home, "--claim-id", claimID, "--token-file", tokenPath, "--revision", "1", "--operation-id", strings.Repeat("2", 32), "--request-not-after", deadline, "--successor-handle", successor, "--to-agent", "next", "--to-session", "next-session"}, "dev", "unknown", "unknown", &out, &bytes.Buffer{})
	if err != nil || strings.Contains(out.String(), "token") {
		t.Fatalf("explicit transfer err=%v output=%q", err, out.String())
	}
	if strings.Contains(out.String(), `"successorHandle"`) || strings.Contains(out.String(), `"resources"`) {
		t.Fatalf("explicit transfer changed stable JSON fields: %q", out.String())
	}
	if _, err := os.Stat(successor); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireRejectsMixedHandleAndStatelessSelection(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "acquire", "--json", "--home", home, "--resource", "r", "--no-handle", "--handle", filepath.Join(home, "handles", "explicit.json"), "--claim-id", strings.Repeat("1", 32)}, "dev", "unknown", "unknown", &out, &bytes.Buffer{})
	if err == nil || !strings.Contains(out.String(), `"reason":"credential-source-conflict"`) {
		t.Fatalf("mixed acquire err=%v output=%q", err, out.String())
	}
}

func TestProcessesSerializeOneHandleAndConcurrentMutations(t *testing.T) {
	home := t.TempDir()
	handlePath := filepath.Join(home, "handles", "shared.json")
	args := []string{"worklease", "acquire", "--json", "--home", home, "--handle", handlePath, "--resource", "shared"}
	type result struct {
		code int
		out  string
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, out := runHandleProcess(t, args)
			results <- result{code, out}
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for result := range results {
		if result.code == 0 {
			successes++
		} else if !strings.Contains(result.out, "handle-in-use") && !strings.Contains(result.out, "operation-request-mismatch") {
			t.Fatalf("unexpected contender result: code=%d output=%q", result.code, result.out)
		}
	}
	if successes != 1 {
		t.Fatalf("acquire successes=%d want=1", successes)
	}
	mutation := []string{"worklease", "heartbeat", "--json", "--home", home, "--handle", handlePath}
	results = make(chan result, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, out := runHandleProcess(t, mutation)
			results <- result{code, out}
		}()
	}
	wg.Wait()
	close(results)
	for result := range results {
		if result.code != 0 {
			t.Fatalf("serialized mutation failed: code=%d output=%q", result.code, result.out)
		}
	}
	h, err := handle.Read(handlePath)
	if err != nil || h.Revision != 3 {
		t.Fatalf("final handle revision=%d err=%v", h.Revision, err)
	}
}

func TestReverseTransfersUseCanonicalLocksWithoutDeadlock(t *testing.T) {
	home := t.TempDir()
	first := filepath.Join(home, "handles", "a.json")
	second := filepath.Join(home, "handles", "b.json")
	for _, pair := range []struct{ path, resource, session string }{{first, "a", "one"}, {second, "b", "two"}} {
		if err := Run(context.Background(), []string{"worklease", "acquire", "--json", "--home", home, "--handle", pair.path, "--session", pair.session, "--resource", pair.resource}, "dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
	}
	commands := [][]string{
		{"worklease", "transfer", "--json", "--home", home, "--handle", first, "--successor-handle", second, "--to-agent", "next", "--to-session", "next-one"},
		{"worklease", "transfer", "--json", "--home", home, "--handle", second, "--successor-handle", first, "--to-agent", "next", "--to-session", "next-two"},
	}
	results := make(chan int, 2)
	for _, command := range commands {
		command := command
		wg := &sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, _ := runHandleProcess(t, command)
			results <- code
		}()
		go func() { wg.Wait() }()
	}
	for range 2 {
		if code := <-results; code == 0 {
			t.Fatal("reverse transfer unexpectedly overwrote an active destination")
		}
	}
	if _, err := handle.Read(first); err != nil {
		t.Fatal(err)
	}
	if _, err := handle.Read(second); err != nil {
		t.Fatal(err)
	}
}

func TestContextualTransferPersistsSuccessorAndSupportsGeneratedOperationIDs(t *testing.T) {
	home := t.TempDir()
	current := filepath.Join(home, "handles", "current.json")
	successor := filepath.Join(home, "handles", "successor.json")
	run := func(args ...string) string {
		var out bytes.Buffer
		if err := Run(context.Background(), append([]string{"worklease"}, args...), "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
			t.Fatalf("%v: %v output=%q", args, err, out.String())
		}
		return out.String()
	}
	run("acquire", "--json", "--home", home, "--handle", current, "--resource", "transfer-resource")
	transferred := run("transfer", "--home", home, "--handle", current, "--successor-handle", successor, "--to-agent", "next", "--to-session", "next-session")
	if strings.Contains(transferred, "token") {
		t.Fatal("transfer exposed a bearer token")
	}
	for _, want := range []string{"transferred ownership", "successorHandle: " + successor, "resources: transfer-resource", "agentId: next", "sessionId: next-session"} {
		if !strings.Contains(transferred, want) {
			t.Fatalf("transfer output missing %q: %q", want, transferred)
		}
	}
	if _, err := os.Stat(current); !os.IsNotExist(err) {
		t.Fatalf("predecessor handle still exists: %v", err)
	}
	run("heartbeat", "--json", "--home", home, "--handle", successor)
	run("release", "--json", "--home", home, "--handle", successor)
}

func TestContextualDefaultRunsCompleteLifecycle(t *testing.T) {
	home := t.TempDir()
	run := func(args ...string) string {
		var out bytes.Buffer
		if err := Run(context.Background(), append([]string{"worklease"}, args...), "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
			t.Fatalf("%v: %v output=%q", args, err, out.String())
		}
		return out.String()
	}
	acquired := run("acquire", "--json", "--home", home, "--resource", "contextual-resource")
	if strings.Contains(acquired, "token") {
		t.Fatal("contextual acquire exposed token")
	}
	run("heartbeat", "--json", "--home", home, "--operation-id", strings.Repeat("1", 32))
	checkpoint := run("checkpoint", "--json", "--home", home, "--operation-id", strings.Repeat("2", 32), "--data", `{"step":1}`)
	if strings.Contains(checkpoint, `"checkpointBytes"`) {
		t.Fatalf("checkpoint changed stable JSON fields: %q", checkpoint)
	}
	run("release", "--json", "--home", home, "--operation-id", strings.Repeat("3", 32))
}

func TestAcquireTextPreservesUnresolvedPredecessorRecoveryIDs(t *testing.T) {
	home := t.TempDir()
	st, err := store.Open(context.Background(), home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	svc := lease.New(st, nil, nil, lease.Defaults{})
	claimID, token, operationID := strings.Repeat("a", 32), strings.Repeat("b", 64), strings.Repeat("c", 32)
	grant, err := svc.Acquire(context.Background(), lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Resources: []string{"recovery-resource"}, AgentID: "old", SessionID: "old", TTL: time.Minute, RequestNotAfter: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BeginOperation(context.Background(), lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Revision: grant.Revision}, lease.OperationIntent{OperationID: operationID, Kind: "exec", Request: map[string]any{"argv": []string{"true"}}, TTL: time.Minute, RequestNotAfter: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(context.Background(), func(tx *store.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE claims SET expires_at=? WHERE claim_id=?`, time.Now().Add(-time.Second).UnixMicro(), claimID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "acquire", "--home", home, "--resource", "recovery-resource"}, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"unknownOperations: [\"" + operationID + "\"]", "claimId=" + claimID} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("acquire output missing %q: %q", want, out.String())
		}
	}
}

func TestLifecycleMutationsUseConciseTextSummaries(t *testing.T) {
	home := t.TempDir()
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "acquire", "--home", home, "--resource", "summary-resource"}, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.HasPrefix(got, "acquired 1 resource as claim ") || !strings.Contains(got, "\nrevision: 1\n") || strings.Contains(got, "receipt:") || strings.Contains(got, "map[") {
		t.Fatalf("acquire output=%q", got)
	}
	for _, test := range []struct {
		args   []string
		prefix string
		detail string
		extra  string
	}{
		{args: []string{"checkpoint", "--data", `{"step":1}`}, prefix: "checkpointed claim ", detail: "\nrevision: 2\nexpiresAt: ", extra: "\ncheckpointBytes: 10\n"},
		{args: []string{"heartbeat"}, prefix: "renewed claim ", detail: "\nrevision: 3\nexpiresAt: "},
		{args: []string{"release", "--reason", "completed"}, prefix: "released claim ", detail: "\nrevision: 4\nreason: completed\n"},
	} {
		out.Reset()
		args := append([]string{"worklease"}, test.args...)
		args = append(args, "--home", home)
		if err := Run(context.Background(), args, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if got := out.String(); !strings.HasPrefix(got, test.prefix) || !strings.Contains(got, test.detail) || test.extra != "" && !strings.Contains(got, test.extra) || strings.Contains(got, "receipt:") || strings.Contains(got, "map[") {
			t.Fatalf("%s output=%q", test.args[0], got)
		}
	}
}

func TestAcquireDerivesInputBeforeDispatch(t *testing.T) {
	var stdout bytes.Buffer
	home, tokenDir := t.TempDir(), t.TempDir()
	if err := os.Chmod(tokenDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(tokenDir, "token")
	if err := os.WriteFile(tokenPath, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"worklease", "acquire", "--json", "--home", home, "--provider", "generic", "--source", "s", "--item", "i", "--no-handle", "--claim-id", strings.Repeat("1", 32), "--session", "stateless-session", "--token-file", tokenPath, "--request-not-after", time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}
	err := Run(context.Background(), args, "dev", "unknown", "unknown", &stdout, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("acquire failed: %v output=%q", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"claimId"`) || strings.Contains(stdout.String(), `"token"`) {
		t.Fatalf("unexpected output=%q", stdout.String())
	}
	stdout.Reset()
	if err := Run(context.Background(), args, "dev", "unknown", "unknown", &stdout, &bytes.Buffer{}); err != nil {
		t.Fatalf("stateless replay failed: %v output=%q", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"idempotent":true`) {
		t.Fatalf("stateless replay was not idempotent: %q", stdout.String())
	}
}

func TestInterruptedCLITakeoverDoesNotRestoreMCPHold(t *testing.T) {
	home := t.TempDir()
	handlePath := filepath.Join(home, "handles", "takeover.json")
	run := func(args ...string) {
		var out bytes.Buffer
		if err := Run(context.Background(), append([]string{"worklease"}, args...), "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
			t.Fatalf("%v: %v output=%q", args, err, out.String())
		}
	}
	run("acquire", "--json", "--home", home, "--handle", handlePath, "--resource", "takeover")
	h, err := handle.Read(handlePath)
	if err != nil {
		t.Fatal(err)
	}
	formerHold := time.Now().Add(time.Minute).UTC()
	h.HoldUntil = formerHold
	deadline := time.Now().Add(time.Hour).UTC()
	inputs := map[string]any{"kind": "heartbeat", "authorityId": h.AuthorityID, "claimId": h.ClaimID, "ttl": int64((15 * time.Minute) / time.Microsecond), "requestNotAfter": deadline.UnixMicro()}
	if err := beginHandleMutation(handlePath, &h, "heartbeat", strings.Repeat("c", 32), deadline, inputs); err != nil {
		t.Fatal(err)
	}
	pending, err := handle.Read(handlePath)
	if err != nil {
		t.Fatal(err)
	}
	if !pending.HoldUntil.IsZero() {
		t.Fatalf("fresh CLI pending request retained MCP hold: %s", pending.HoldUntil)
	}
	run("heartbeat", "--json", "--home", home, "--handle", handlePath)
	recovered, err := handle.Read(handlePath)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered.HoldUntil.IsZero() || !recovered.ExpiresAt.After(formerHold) {
		t.Fatalf("CLI recovery restored MCP hold: expires=%s hold=%s former=%s", recovered.ExpiresAt, recovered.HoldUntil, formerHold)
	}
}

func TestPendingLifecycleRecoversBeforeAndAfterAuthorityDispatch(t *testing.T) {
	home := t.TempDir()
	handlePath := filepath.Join(home, "handles", "recovery.json")
	run := func(args ...string) {
		var out bytes.Buffer
		if err := Run(context.Background(), append([]string{"worklease"}, args...), "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err != nil {
			t.Fatalf("%v: %v output=%q", args, err, out.String())
		}
	}
	run("acquire", "--json", "--home", home, "--handle", handlePath, "--resource", "recover")
	h, err := handle.Read(handlePath)
	if err != nil {
		t.Fatal(err)
	}
	h.HoldUntil = time.Now().Add(time.Minute).UTC()
	deadline := time.Now().Add(time.Hour).UTC()
	firstOp := strings.Repeat("d", 32)
	firstInputs := map[string]any{"kind": "heartbeat", "authorityId": h.AuthorityID, "claimId": h.ClaimID, "ttl": int64((15 * time.Minute) / time.Microsecond), "requestNotAfter": deadline.UnixMicro()}
	h.State = "pending"
	h.PendingRequest = &handle.PendingRequest{OperationID: firstOp, Kind: "heartbeat", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: requestHashCLI(firstInputs), RequestNotAfter: deadline, Inputs: firstInputs}
	if err := handle.Write(handlePath, h); err != nil {
		t.Fatal(err)
	}
	run("heartbeat", "--json", "--home", home, "--handle", handlePath)
	h, err = handle.Read(handlePath)
	if err != nil || h.Revision != 2 || h.PendingRequest != nil || h.ExpiresAt.After(h.HoldUntil) {
		t.Fatalf("pre-dispatch legacy hold recovery=%#v err=%v", h, err)
	}
	secondOp := strings.Repeat("e", 32)
	secondDeadline := time.Now().Add(time.Hour).UTC()
	secondInputs := map[string]any{"kind": "heartbeat", "authorityId": h.AuthorityID, "claimId": h.ClaimID, "ttl": int64((15 * time.Minute) / time.Microsecond), "requestNotAfter": secondDeadline.UnixMicro()}
	h.State = "pending"
	h.PendingRequest = &handle.PendingRequest{OperationID: secondOp, Kind: "heartbeat", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: requestHashCLI(secondInputs), RequestNotAfter: secondDeadline, Inputs: secondInputs}
	if err := handle.Write(handlePath, h); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc := lease.New(st, nil, nil, lease.Defaults{})
	if _, err := svc.Heartbeat(context.Background(), lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision}, lease.Renew{OperationID: secondOp, TTL: 15 * time.Minute, RequestNotAfter: secondDeadline}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	run("heartbeat", "--json", "--home", home, "--handle", handlePath)
	h, err = handle.Read(handlePath)
	if err != nil || h.Revision != 3 || h.PendingRequest != nil {
		t.Fatalf("post-dispatch recovery=%#v err=%v", h, err)
	}
	if !h.ExpiresAt.After(h.HoldUntil) {
		t.Fatalf("explicit CLI takeover unexpectedly retained MCP hold: expires=%s hold=%s", h.ExpiresAt, h.HoldUntil)
	}
	releaseOp := strings.Repeat("f", 32)
	releaseDeadline := time.Now().Add(time.Hour).UTC()
	releaseInputs := map[string]any{"kind": "release", "authorityId": h.AuthorityID, "claimId": h.ClaimID, "reason": "released", "requestNotAfter": releaseDeadline.UnixMicro()}
	h.State = "pending"
	h.PendingRequest = &handle.PendingRequest{OperationID: releaseOp, Kind: "release", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: requestHashCLI(releaseInputs), RequestNotAfter: releaseDeadline, Inputs: releaseInputs}
	if err := handle.Write(handlePath, h); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(context.Background(), home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	svc = lease.New(st, nil, nil, lease.Defaults{})
	if _, err := svc.Release(context.Background(), lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision}, lease.ReleaseRequest{OperationID: releaseOp, Reason: "released", RequestNotAfter: releaseDeadline}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	run("release", "--json", "--home", home, "--handle", handlePath)
	if _, err := os.Stat(handlePath); !os.IsNotExist(err) {
		t.Fatalf("release recovery did not remove handle: %v", err)
	}
}

func TestHandleSynchronizationNeverRewindsRevision(t *testing.T) {
	home := t.TempDir()
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "claim.json")
	h := handle.Handle{SchemaVersion: 1, AuthorityID: strings.Repeat("a", 32), ClaimID: strings.Repeat("b", 32), Token: strings.Repeat("c", 64), Revision: 3, Resources: []string{"r"}, ExpiresAt: time.Now().Add(time.Hour), AgentID: "agent", SessionID: "session", State: "ready"}
	if err := handle.Write(path, h); err != nil {
		t.Fatal(err)
	}
	if err := finishHandleMutation(path, &h, lease.Receipt{Revision: 2, Result: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	got, err := handle.Read(path)
	if err != nil || got.Revision != 3 {
		t.Fatalf("handle revision rewound: %d err=%v", got.Revision, err)
	}
}

func TestStatusRejectsMixedPrivateAndPublicSelection(t *testing.T) {
	home := t.TempDir()
	handlePath := filepath.Join(home, "handles", "claim.json")
	if err := Run(context.Background(), []string{"worklease", "acquire", "--json", "--home", home, "--handle", handlePath, "--resource", "r"}, "dev", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, extra := range [][]string{{"--claim-id", strings.Repeat("1", 32)}, {"--resource", "r"}} {
		args := append([]string{"worklease", "status", "--json", "--home", home, "--handle", handlePath}, extra...)
		var out bytes.Buffer
		if err := Run(context.Background(), args, "dev", "unknown", "unknown", &out, &bytes.Buffer{}); err == nil || !strings.Contains(out.String(), `"reason":"credential-source-conflict"`) {
			t.Fatalf("mixed status %v err=%v output=%q", extra, err, out.String())
		}
	}
}
