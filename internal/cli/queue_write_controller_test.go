package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/resource"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestLinearRecoveryRejectsChangedWorkflowAndSourceBinding(t *testing.T) {
	t.Parallel()
	configured := config.QueueSource{
		ID: "linear", Adapter: "linear", Organization: "0bfebf80-70af-4eca-9e39-2029a01f5b77",
		Team: "d19193f6-0501-485b-93af-65e829c2039d", Project: "9f3b2707-b6d8-456d-9079-32f60cd33474",
		Account: "72088203-6bc7-4a63-a71b-22048b88da64", CredentialHelper: []string{"/usr/bin/helper", "profile-a"},
		Workflow: map[string]string{"start": "075a1740-eeda-4b85-be0b-39755abf4c8c"},
	}
	intent := queue.WriteIntent{
		Source: queue.Source{ID: configured.ID, Adapter: configured.Adapter, Locator: configured.Organization},
		Action: queue.ActionStart, Transition: configured.Workflow["start"],
	}
	if err := validateLinearRecoveryConfig(intent, configured); err != nil {
		t.Fatalf("unchanged Linear recovery binding rejected: %v", err)
	}
	changedWorkflow := configured
	changedWorkflow.Workflow = map[string]string{"start": "9f3b2707-b6d8-456d-9079-32f60cd33474"}
	if err := validateLinearRecoveryConfig(intent, changedWorkflow); err == nil {
		t.Fatal("Linear recovery accepted a changed transition mapping")
	}
	changedHelper := configured
	changedHelper.CredentialHelper = []string{"/usr/bin/helper", "profile-b"}
	if sameLinearQueueBinding(configured, changedHelper) {
		t.Fatal("Linear recovery binding ignored changed credential-helper argv")
	}
	changedScope := configured
	changedScope.Project = "432032b3-d574-4147-ae02-278d03f99c9e"
	if sameLinearQueueBinding(configured, changedScope) {
		t.Fatal("Linear recovery binding ignored changed project scope")
	}
}

func TestQueueWriteControllerExternalCrashRecoveryIsReadOnly(t *testing.T) {
	h := newQueueQueryHarness(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	logPath := filepath.Join(t.TempDir(), "external-requests.jsonl")
	executable := writeQueueExternalWriteExecutable(t, "planning", logPath)
	queueConfig := fmt.Sprintf("version: 1\nme: {}\nsources:\n  - id: planning\n    adapter: external\n    executable: %q\n    expectedAdapterId: example.write\n    expectedVersion: 1.0.0\n    config: {}\n    account: alice\n    workflow: {start: Doing}\n    claims: {policy: generic, source: acme/planning}\nviews:\n  - name: Ready\n    authority: local\n    sources: [planning]\n    filter: {}\n", executable)
	h.writeQueueConfig(queueConfig)
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	configured := cfg.Sources[0]
	if err := config.ApproveQueueAdapter(context.Background(), os.Getenv, configured); err != nil {
		t.Fatal(err)
	}
	registry := queue.NewRegistry()
	cleanup, err := queue.RegisterExternalSources(registry, cfg.Sources, os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	read, ok := registry.Get(queue.ExternalSourceAdapterKey("planning"))
	if !ok {
		t.Fatal("external source did not register")
	}
	source, err := read.Resolve(context.Background(), map[string]string{"id": "planning"})
	if err != nil {
		t.Fatal(err)
	}
	controller := queueWriteController{registry: registry, configured: map[string]config.QueueSource{"planning": configured}}
	writer, err := controller.adapter(source)
	if err != nil {
		t.Fatal(err)
	}
	externalWriter, ok := writer.(*queue.ExternalWriteAdapter)
	if !ok {
		t.Fatalf("controller selected %T, want external writer", writer)
	}
	operationID := strings.Repeat("b", 32)
	intent := queue.WriteIntent{
		OperationID: operationID, Source: source, Ref: queue.Ref{SourceID: source.ID, ItemID: "item-1"}, Principal: "alice",
		AuthorityID: "authority-private", ClaimID: "claim-private", ClaimRevision: 1, Action: queue.ActionRecordProgress,
		Patch: map[string]string{"append": "notes"}, Append: "evidence", CheckpointTTL: time.Minute, CheckpointNotAfter: time.Now().Add(time.Hour),
	}
	claimKey, err := resource.Resolve(resource.Input{Provider: "generic", Source: configured.Claims.Source, Item: intent.Ref.ItemID})
	if err != nil {
		t.Fatal(err)
	}
	intent.Resources = []string{claimKey.Resource}
	prepared, detail, err := externalWriter.Prepare(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	preview := queueui.WritePreview{Races: []string{"external provider writers are not fenced by this claim"}}
	if err := applyExternalWritePreview(&preview, detail); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(preview.Effect, "recordProgress {") || !strings.Contains(preview.Effect, `"content":"evidence"`) || len(preview.SideEffects) != 2 || len(preview.Races) != 2 {
		t.Fatalf("external preview omits its safe effect, side effects, or race: %+v", preview)
	}
	journal, err := queue.NewWriteJournal(config.QueueRecoveryDir(os.Getenv), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	claim := &externalControllerTestClaim{held: true}
	workflow := configured.Workflow
	pipeline := queue.WritePipeline{Adapter: writer, Claim: claim, Journal: journal, Workflow: workflow}
	result, err := pipeline.Start(context.Background(), prepared)
	if err == nil || result.Outcome != queue.WriteUnknown || !result.ClaimHeld || !claim.held {
		t.Fatalf("crashed provider write did not remain unknown with its claim held: %+v %v held=%t", result, err, claim.held)
	}
	record, err := journal.Read(operationID)
	if err != nil || record.Status != "unknown" || record.Receipt != nil {
		t.Fatalf("journal after lost provider response = %+v, %v", record, err)
	}
	if len(claim.checkpoints) != 0 {
		t.Fatal("unknown provider write was checkpointed before read-back")
	}
	recoveryAdapter, recoveryWorkflow, closeAdapter, err := queueExternalRecoveryAdapter(context.Background(), record.Intent)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAdapter()
	result, err = (queue.WritePipeline{Adapter: recoveryAdapter, Claim: claim, Journal: journal, Workflow: recoveryWorkflow}).Recover(context.Background(), operationID)
	if err != nil || result.Outcome != queue.WriteVerified || !result.ClaimHeld || !claim.held || len(claim.checkpoints) != 1 {
		t.Fatalf("readReceipt recovery did not verify while retaining claim: %+v %v held=%t checkpoints=%d", result, err, claim.held, len(claim.checkpoints))
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	dispatches := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var request struct {
			Method string `json:"method"`
		}
		if json.Unmarshal([]byte(line), &request) == nil && request.Method == "recordProgress" {
			dispatches++
		}
	}
	if dispatches != 1 {
		t.Fatalf("recovery redispatched provider mutation %d times", dispatches)
	}
	for _, secret := range []string{"authority-private", "claim-private", claimKey.Resource} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("Worklease claim authority material was sent to external adapter: %s", secret)
		}
	}
	h.writeQueueConfig(strings.Replace(queueConfig, "account: alice", "account: bob", 1))
	if _, _, closeDrift, driftErr := queueExternalRecoveryAdapter(context.Background(), record.Intent); driftErr == nil {
		closeDrift()
		t.Fatal("external recovery accepted a changed account binding")
	}
}

type externalControllerTestClaim struct {
	held        bool
	checkpoints []string
}

func (c *externalControllerTestClaim) Verify(context.Context, queue.WriteIntent) error { return nil }
func (c *externalControllerTestClaim) Checkpoint(_ context.Context, intent queue.WriteIntent, _ queue.ProviderReceipt) error {
	c.checkpoints = append(c.checkpoints, intent.OperationRef)
	return nil
}
func (c *externalControllerTestClaim) CheckpointStatus(context.Context, queue.WriteIntent, queue.ProviderReceipt) (queue.WriteVerification, error) {
	return queue.WriteUnknown, nil
}

func writeQueueExternalWriteExecutable(t *testing.T, sourceID, logPath string) string {
	t.Helper()
	binary, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "external-write-adapter")
	script := fmt.Sprintf("#!/bin/sh\nexec %s -test.run='^TestQueueExternalWriteProcessHelper$' -- %s %s\n", shellQuote(binary), shellQuote(sourceID), shellQuote(logPath))
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func TestQueueExternalWriteProcessHelper(t *testing.T) {
	separator := -1
	for index, arg := range os.Args {
		if arg == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+2 >= len(os.Args) {
		return
	}
	runQueueExternalWriteProcessHelper(os.Args[separator+1], os.Args[separator+2])
}

func runQueueExternalWriteProcessHelper(sourceID, logPath string) {
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var request struct {
			ID     string                     `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &request) != nil {
			os.Exit(31)
		}
		if file, openErr := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); openErr == nil {
			_, _ = file.Write(line)
			_ = file.Close()
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "manifest": map[string]any{
				"id": "example.write", "version": "1.0.0", "protocol": map[string]int{"minMajor": 1, "maxMajor": 1},
				"configSchema": map[string]any{"type": "object"}, "authentication": []string{}, "resourcePolicy": "generic",
				"capabilities": []string{"mutation", "progress"}, "requiredFeatures": []string{},
			}}
		case "resolve":
			result = map[string]any{"context": externalControllerContext(sourceID, nil), "source": map[string]string{"id": sourceID, "name": sourceID, "locator": "memory://" + sourceID}}
		case "capabilities":
			var principal string
			_ = json.Unmarshal(request.Params["principal"], &principal)
			result = map[string]any{"context": externalControllerContext(sourceID, &principal), "capabilities": map[string]any{
				"mutation": map[string]any{"support": "supported", "permission": "allowed", "availability": "available"},
				"progress": map[string]any{"support": "supported", "permission": "allowed", "availability": "available"},
			}}
		case "readItem":
			var ref queue.Ref
			_ = json.Unmarshal(request.Params["ref"], &ref)
			result = map[string]any{"context": externalControllerContext(sourceID, nil), "outcome": map[string]any{"ref": ref, "status": "found", "item": map[string]any{
				"ref": ref, "title": "Write fixture", "rawStatus": "Open", "state": "open", "order": "1", "priority": 0,
				"canonicalId": sourceID + ":" + ref.ItemID, "providerReady": true, "assignedTo": []string{}, "nativeClaim": "none",
				"updatedAt": time.Now().UTC(), "body": "", "terminal": false, "providerBlocked": false,
			}}}
		case "readDependencies":
			var ref queue.Ref
			_ = json.Unmarshal(request.Params["ref"], &ref)
			result = map[string]any{"context": externalControllerContext(sourceID, nil), "edges": []any{}, "nextCursor": nil, "completeness": "complete"}
		case "recordProgress":
			os.Exit(23)
		case "readReceipt":
			var target queue.Ref
			var operationID string
			var intent struct {
				Payload map[string]any `json:"payload"`
			}
			_ = json.Unmarshal(request.Params["target"], &target)
			_ = json.Unmarshal(request.Params["operationId"], &operationID)
			_ = json.Unmarshal(request.Params["intent"], &intent)
			content, _ := intent.Payload["content"].(string)
			result = map[string]any{"context": externalControllerContext(sourceID, nil), "verification": "verified", "evidence": map[string]any{
				"sourceId": sourceID, "itemId": target.ItemID, "precondition": "provider-v1", "patch": map[string]string{"append": "notes"},
				"markerCount": 1, "appendContent": content, "appendProof": true, "receiptId": "receipt://" + operationID,
				"operationId": operationID, "actor": "alice",
			}}
		default:
			os.Exit(33)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			os.Exit(34)
		}
		response, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": json.RawMessage(encoded)})
		if err != nil {
			os.Exit(35)
		}
		_, _ = fmt.Fprintln(os.Stdout, string(response))
	}
}

func externalControllerContext(sourceID string, principal *string) map[string]any {
	version := "provider-v1"
	return map[string]any{"principal": principal, "configurationGeneration": "controller-test", "observedAt": time.Now().UTC(), "providerVersion": &version,
		"coverage": map[string]any{"state": "complete", "scope": sourceID, "cursor": nil}}
}

func TestQueueWriteControllerPreviewsAndVerifiesBacklogMutation(t *testing.T) {
	if isolateCLIProcess(t) {
		return
	}
	binary, err := exec.LookPath("backlog")
	if err != nil {
		t.Skip("backlog CLI unavailable")
	}
	if _, err := exec.Command(binary, "--version").Output(); err != nil {
		t.Skip("backlog CLI cannot run")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	claimItem := queueClaimItem("tasks", "TASK-1")
	claimController, backend, fixture := newLocalQueueClaimController(t, config.DefaultTTL, claimItem)
	claimPreview := claimController.Preview(context.Background(), claimItem)().(queueui.ClaimPreviewMsg)
	if claimPreview.Err != nil {
		t.Fatal(claimPreview.Err)
	}
	if result := claimController.AcquireClaim(context.Background(), claimItem, *claimPreview.Preview)().(queueui.ClaimResultMsg); result.Err != nil {
		t.Fatal(result.Err)
	}
	originalPath, err := queueClaimHandlePath(backend.Config.Home, claimController.queueSession, config.LocalProfileName, fixture.source, claimItem.Ref)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	configText := "project_name: scratch\nstatuses: [To Do, In Progress, Done]\nbacklog_directory: docs/backlog\nremote_operations: false\ncheck_active_branches: false\nauto_commit: false\nbypass_git_hooks: false\n"
	if err := os.WriteFile(filepath.Join(root, "backlog.config.yml"), []byte(configText), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial"}} {
		cmd := testkit.GitCommand(args...)
		cmd.Dir = root
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %v %s", err, data)
		}
	}
	create := exec.Command(binary, "task", "create", "Scratch write", "--assignee", "@alice", "--ac", "criterion", "--no-dod-defaults")
	create.Dir = root
	if data, err := create.CombinedOutput(); err != nil {
		t.Fatalf("backlog fixture: %v %s", err, data)
	}
	registry := queue.NewRegistry()
	read, ok := registry.Get("backlog-md")
	if !ok {
		t.Fatal("backlog adapter unavailable")
	}
	source, err := read.Resolve(context.Background(), map[string]string{"id": "tasks", "checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	original, err := handle.Read(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	ref := queue.Ref{SourceID: source.ID, ItemID: "TASK-1"}
	path, err := queueClaimHandlePath(backend.Config.Home, claimController.queueSession, config.LocalProfileName, source, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(originalPath, path); err != nil {
		t.Fatal(err)
	}
	// The write controller is under test here; lifecycle renewal has separate
	// tests. Keep the single moved handle and production TTL so a provider CLI
	// call cannot race an unrelated heartbeat in this test.
	queueConfig := fmt.Sprintf("version: 1\nme:\n  backlog-md: ['@bob']\nsources:\n  - id: tasks\n    adapter: backlog-md\n    checkout: %q\n    claims:\n      policy: generic\n      source: portable\n    workflow:\n      start: In Progress\nviews:\n  - name: Ready\n    authority: local\n    sources: [tasks]\n    filter:\n      readiness: ready\n", root)
	if err := handle.WriteOwnerPrivate(config.QueuePath(os.Getenv), []byte(queueConfig), 1<<20); err != nil {
		t.Fatal(err)
	}
	confirmed := config.QueueIdentity{Adapter: source.Adapter, Locator: source.Locator, Policy: "generic", Source: "portable", AuthorityID: backend.AuthorityID()}
	if err := config.SaveQueueIdentities(os.Getenv, config.QueueIdentities{Version: 1, Sources: map[string]config.QueueIdentity{source.ID: confirmed}}); err != nil {
		t.Fatal(err)
	}
	journal, err := queueRecoveryJournal()
	if err != nil {
		t.Fatal(err)
	}
	current := func() (queue.ClaimAuthority, uint64) { return claimController.current() }
	configured := config.QueueSource{ID: "tasks", Adapter: "backlog-md", Checkout: root, Workflow: map[string]string{"start": "In Progress"}}
	controller := queueWriteController{backend: backend, registry: registry, current: current, journal: journal, sources: map[string]queue.Source{source.ID: source}, configured: map[string]config.QueueSource{source.ID: configured}, me: map[string][]string{source.ID: {"@bob"}}, session: claimController.queueSession, profile: config.LocalProfileName}
	item := queue.Item{Summary: queue.Summary{Ref: ref}}
	for _, tc := range []struct {
		action                   queue.Action
		transition, text, effect string
	}{{queue.ActionStart, "In Progress", "", "--status In Progress"}, {queue.ActionRecordProgress, "", "evidence", "--append-notes"}, {queue.ActionAssignToMe, "", "", "--assignee"}} {
		preview := controller.Preview(context.Background(), item, tc.action, tc.transition, tc.text)().(queueui.WritePreviewMsg)
		if preview.Err != nil {
			t.Fatalf("%s preview: %v", tc.action, preview.Err)
		}
		if !strings.Contains(preview.Preview.Effect, tc.effect) || preview.Preview.Intent.ClaimID != original.ClaimID || len(preview.Preview.Races) == 0 || len(preview.Preview.SideEffects) == 0 || preview.Preview.Intent.AuthorityID != backend.AuthorityID() {
			t.Fatalf("%s missing effect, claim, or races: %+v", tc.action, preview.Preview)
		}
	}
	if entries, err := journal.Recovery(time.Now()); err != nil || len(entries) != 0 {
		t.Fatalf("preview dispatched: %+v %v", entries, err)
	}
	preview := controller.Preview(context.Background(), item, queue.ActionStart, "In Progress", "")().(queueui.WritePreviewMsg)
	if preview.Err != nil {
		t.Fatal(preview.Err)
	}
	result := controller.Confirm(context.Background(), *preview.Preview)().(queueui.WriteResultMsg)
	if result.Err != nil || result.Result.Outcome != queue.WriteVerified || !result.Result.ClaimHeld {
		t.Fatalf("confirmed state not verified and held: %+v", result)
	}
	oldDomain := controller.Preview(context.Background(), item, queue.ActionAssignToMe, "", "")().(queueui.WritePreviewMsg)
	if oldDomain.Err != nil {
		t.Fatal(oldDomain.Err)
	}
	changedConfig := strings.Replace(queueConfig, "source: portable", "source: different", 1)
	if err := handle.WriteOwnerPrivate(config.QueuePath(os.Getenv), []byte(changedConfig), 1<<20); err != nil {
		t.Fatal(err)
	}
	if attempted := controller.Confirm(context.Background(), *oldDomain.Preview)().(queueui.WriteResultMsg); attempted.Err == nil {
		t.Fatal("old-domain handle authorized provider write after unconfirmed policy change")
	}
	if err := handle.WriteOwnerPrivate(config.QueuePath(os.Getenv), []byte(queueConfig), 1<<20); err != nil {
		t.Fatal(err)
	}
	stale := controller.Preview(context.Background(), item, queue.ActionAssignToMe, "", "")().(queueui.WritePreviewMsg)
	if stale.Err != nil {
		t.Fatal(stale.Err)
	}
	external := exec.Command(binary, "task", "edit", "TASK-1", "--append-notes", "external change")
	external.Dir = root
	if data, err := external.CombinedOutput(); err != nil {
		t.Fatalf("external change: %v %s", err, data)
	}
	rejected := controller.Confirm(context.Background(), *stale.Preview)().(queueui.WriteResultMsg)
	if rejected.Err == nil || rejected.Result.Outcome == queue.WriteVerified {
		t.Fatalf("stale preview dispatched: %+v", rejected)
	}
	if entries, err := journal.Recovery(time.Now()); err != nil || len(entries) != 0 {
		t.Fatalf("preview wrote without confirmation or stale intent dispatched: %+v %v", entries, err)
	}
}
