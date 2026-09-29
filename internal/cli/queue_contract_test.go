package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestQueueContractUsesEnrolledMainCheckoutAndOverridesCopy(t *testing.T) {
	if isolateCLIProcess(t) {
		return
	}
	h := newQueueQueryHarness(t)
	linked := filepath.Join(filepath.Dir(h.checkout), "linked")
	if output, err := testkit.GitCommand("-C", h.checkout, "worktree", "add", "-b", "contract-linked", linked).CombinedOutput(); err != nil {
		t.Fatalf("create linked worktree: %v: %s", err, output)
	}
	writeTestQueueContract(t, h.checkout, "version: 1\nsources:\n  - id: shared\n    adapter: backlog-md\n    workflow: {start: Main}\n    claims: {policy: generic, source: team/main}\n")
	writeTestQueueContract(t, linked, "version: 1\nsources:\n  - id: shared\n    adapter: backlog-md\n    workflow: {start: Linked}\n    claims: {policy: generic, source: team/linked}\n")
	copyRoot := t.TempDir()
	writeTestQueueContract(t, copyRoot, "version: 1\nsources: [{id: bad, adapter: unknown}]\n")
	cfg := config.QueueConfig{
		Sources: []config.QueueSource{
			{ID: "copied", Adapter: "backlog-md", Checkout: h.checkout, ContractCheckout: linked, Claims: &config.QueueClaims{Policy: "generic", Source: "private/copy"}, Workflow: map[string]string{"start": "Private"}},
			{ID: "legacy", Adapter: "backlog-md", Checkout: copyRoot, Claims: &config.QueueClaims{Policy: "generic", Source: "private/legacy"}},
		},
		Views: []config.QueueView{{Name: "Ready", Sources: []string{"copied", "legacy"}}},
	}
	resolved, diagnostics := applyQueueContracts(context.Background(), cfg)
	if len(diagnostics) != 0 {
		t.Fatalf("contract diagnostics: %v", diagnostics)
	}
	if len(resolved.Sources) != 2 || resolved.Sources[0].ID != "shared" || resolved.Sources[0].Adapter != "backlog-md" || resolved.Sources[0].Workflow["start"] != "Main" || resolved.Sources[0].Claims == nil || resolved.Sources[0].Claims.Source != "team/main" {
		t.Fatalf("enrolled contract did not override its copied values: %+v", resolved.Sources)
	}
	if resolved.Sources[0].Checkout != h.checkout || resolved.Sources[0].ContractCheckout != linked {
		t.Fatalf("owner-private runtime paths changed: %+v", resolved.Sources[0])
	}
	if resolved.Sources[1].Claims == nil || resolved.Sources[1].Claims.Source != "private/legacy" || resolved.Sources[1].Workflow["start"] != "" {
		t.Fatalf("unenrolled source was changed or its malformed contract was read: %+v", resolved.Sources[1])
	}
	if len(resolved.Views) != 1 || len(resolved.Views[0].Sources) != 2 || resolved.Views[0].Sources[0] != "shared" {
		t.Fatalf("view source IDs did not follow the runtime contract: %+v", resolved.Views)
	}
	_, mainRoot, err := handle.BindingRoots(linked, nil)
	canonicalCheckout, canonicalErr := filepath.EvalSymlinks(h.checkout)
	if err != nil || canonicalErr != nil || mainRoot != canonicalCheckout {
		t.Fatalf("linked worktree root=%q err=%v; want enrolled main %q", mainRoot, err, h.checkout)
	}
}

func TestQueueContractDriftBlocksClaimsButKeepsReadsAndBadContractIsIncomplete(t *testing.T) {
	if isolateCLIProcess(t) {
		return
	}
	h := newQueueQueryHarness(t)
	t.Setenv("WORKLEASE_HOME", h.state)
	h.setTasks(`[{"id":"TASK-1","title":"Contract task","status":"Open","ordinal":1,"dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true}]`)
	writeTestQueueContract(t, h.checkout, "version: 1\nsources:\n  - id: local\n    adapter: backlog-md\n    workflow: {start: In Progress}\n    claims: {policy: generic, source: shared/old}\n")
	queueText := fmt.Sprintf("version: 1\nme: {}\nsources:\n  - id: local\n    adapter: backlog-md\n    checkout: %s\n    contractCheckout: %s\n    claims: {policy: generic, source: private/copy}\nviews:\n  - name: Ready\n    authority: local\n    sources: [local]\n    filter: {assigned: [nobody]}\n", h.checkout, h.checkout)
	h.writeQueueConfig(queueText)

	cfg, diagnostics, err := loadQueueRuntimeConfig(context.Background(), os.Getenv)
	if err != nil || len(diagnostics) != 0 || cfg.Sources[0].Claims == nil || cfg.Sources[0].Claims.Source != "shared/old" {
		t.Fatalf("initial enrolled contract: cfg=%+v diagnostics=%v err=%v", cfg, diagnostics, err)
	}
	confirmedSource := cfg.Sources[0]
	if err := validateCurrentQueueContract(context.Background(), confirmedSource); err != nil {
		t.Fatalf("unchanged contract rejected: %v", err)
	}
	if _, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge", "--json"); err != nil {
		t.Fatalf("confirm initial contract identity: %v", err)
	}

	writeTestQueueContract(t, h.checkout, "version: 1\nsources:\n  - id: local\n    adapter: backlog-md\n    workflow: {start: Doing}\n    claims: {policy: generic, source: shared/new}\n")
	if err := validateCurrentQueueContract(context.Background(), confirmedSource); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonBindingMigrationRequired {
		t.Fatalf("changed contract did not stop stale claim path: %v", err)
	}
	updated, diagnostics, err := loadQueueRuntimeConfig(context.Background(), os.Getenv)
	if err != nil || len(diagnostics) != 0 || updated.Sources[0].Claims == nil || updated.Sources[0].Claims.Source != "shared/new" {
		t.Fatalf("changed contract did not remain readable: cfg=%+v diagnostics=%v err=%v", updated, diagnostics, err)
	}
	data, err := h.run("queue", "query", "--view", "Ready", "--json")
	if err != nil {
		t.Fatalf("query after claim-input drift: %v: %s", err, data)
	}
	var response map[string]any
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	query := response["query"].(map[string]any)
	if query["incomplete"] != false || len(query["items"].([]any)) != 1 {
		t.Fatalf("claim-domain drift stopped provider reads: %s", data)
	}
	next, err := h.run("queue", "next", "--view", "Ready", "--claim", "--session", "contract-test", "--json")
	if err != nil {
		t.Fatalf("queue next with changed claim inputs: %v: %s", err, next)
	}
	if !strings.Contains(string(next), `"acquired":false`) {
		t.Fatalf("stale claim domain acquired a lease: %s", next)
	}

	if _, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge", "--json"); err != nil {
		t.Fatalf("confirm changed contract identity: %v", err)
	}
	next, err = h.run("queue", "next", "--view", "Ready", "--claim", "--session", "contract-test", "--json")
	if err != nil || !strings.Contains(string(next), `"acquired":true`) {
		t.Fatalf("confirmed contract cannot be claimed: %v: %s", err, next)
	}
	if _, err := h.run("release", "--session", "contract-test", "--reason", "test complete", "--json"); err != nil {
		t.Fatal(err)
	}

	// A bad enrolled project must not take a separate, copied project down.
	queueText = strings.Replace(queueText, "views:\n", "  - id: healthy\n    adapter: backlog-md\n    checkout: "+h.checkout+"\nviews:\n", 1)
	queueText = strings.Replace(queueText, "sources: [local]", "sources: [local, healthy]", 1)
	h.writeQueueConfig(queueText)
	writeTestQueueContract(t, h.checkout, "version: 1\nsources:\n  - id: local\n    adapter: backlog-md\n    claims: {policy: generic, source: shared/bad, checkout: /forbidden}\n")
	data, err = h.run("queue", "query", "--view", "Ready", "--json")
	if err != nil {
		t.Fatalf("malformed enrolled contract should be a project-local diagnostic: %v: %s", err, data)
	}
	if !strings.Contains(string(data), `"incomplete":true`) || !strings.Contains(string(data), queueContractInvalid) || !strings.Contains(string(data), "claims.checkout") {
		t.Fatalf("malformed contract did not mark the enrolled source incomplete: %s", data)
	}
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	items := response["query"].(map[string]any)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["ref"].(map[string]any)["sourceId"] != "healthy" {
		t.Fatalf("malformed project affected healthy project or fell back to its copied domain: %s", data)
	}
	// Move rather than delete the test file to exercise missing-contract handling.
	path := filepath.Join(h.checkout, queueProposalFile)
	if err := os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	data, err = h.run("queue", "query", "--view", "Ready", "--json")
	if err != nil || !strings.Contains(string(data), queueContractMissing) || !strings.Contains(string(data), `"sourceId":"healthy"`) {
		t.Fatalf("missing contract did not isolate the affected project: %v: %s", err, data)
	}
}

func TestQueueInitContractEnrollmentIsPreviewedAndExplicit(t *testing.T) {
	if isolateCLIProcess(t) {
		return
	}
	h := newInitHarness(t)
	h.proposal("version: 1\nsources:\n  - id: shared\n    adapter: backlog-md\n    workflow: {start: In Progress}\n    claims: {policy: generic, source: shared/team}\n")
	adopted := h.invoke("--json")
	if adopted.Err != nil {
		t.Fatalf("initial copied adoption: %v %s", adopted.Err, adopted.Stdout)
	}
	before, err := os.ReadFile(h.configPath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalCheckout, err := filepath.EvalSymlinks(h.checkout)
	if err != nil {
		t.Fatal(err)
	}
	h.proposal("version: 1\nsources:\n  - id: renamed\n    adapter: backlog-md\n    workflow: {start: In Progress}\n    claims: {policy: generic, source: shared/team}\n")
	preview := h.invoke("--enroll-contract", "--dry-run", "--json")
	if preview.Err != nil || !strings.Contains(string(preview.Stdout), `contractCheckout: `+canonicalCheckout) || !strings.Contains(string(preview.Stdout), `"identity":"pending"`) || !strings.Contains(string(preview.Stdout), "--enroll-contract") {
		t.Fatalf("contract enrollment preview: %v %s", preview.Err, preview.Stdout)
	}
	afterPreview, err := os.ReadFile(h.configPath)
	if err != nil || string(afterPreview) != string(before) {
		t.Fatalf("dry-run changed owner-private configuration: %v", err)
	}
	applied := h.invoke("--enroll-contract", "--json")
	if applied.Err != nil || !strings.Contains(string(applied.Stdout), `"identity":"confirmation-required"`) || !strings.Contains(string(applied.Stdout), "identity confirm --source renamed --acknowledge") {
		t.Fatalf("contract enrollment apply: %v %s", applied.Err, applied.Stdout)
	}
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil || len(cfg.Sources) != 1 || cfg.Sources[0].ContractCheckout != canonicalCheckout {
		t.Fatalf("owner-private enrollment was not applied: %+v %v", cfg, err)
	}
	loaded, diagnostics, err := loadQueueRuntimeConfig(context.Background(), os.Getenv)
	if err != nil || len(diagnostics) != 0 || loaded.Sources[0].ID != "renamed" || loaded.Sources[0].Claims == nil || loaded.Sources[0].Claims.Source != "shared/team" {
		t.Fatalf("runtime contract did not replace the adopted copy: cfg=%+v diagnostics=%v err=%v", loaded, diagnostics, err)
	}
}

func TestQueueContractConflictingRenamesKeepSourceIDsUnique(t *testing.T) {
	if isolateCLIProcess(t) {
		return
	}
	h := newQueueQueryHarness(t)
	other := t.TempDir()
	if output, err := testkit.GitCommand("init", "-b", "main", other).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	writeTestQueueContract(t, h.checkout, "version: 1\nsources: [{id: second, adapter: backlog-md}]\n")
	writeTestQueueContract(t, other, "version: 1\nsources: [{id: copied, adapter: backlog-md}]\n")
	cfg := config.QueueConfig{Sources: []config.QueueSource{
		{ID: "first", Adapter: "backlog-md", ContractCheckout: h.checkout},
		{ID: "second", Adapter: "backlog-md", ContractCheckout: other},
		{ID: "copied", Adapter: "backlog-md"},
	}}
	resolved, diagnostics := applyQueueContracts(context.Background(), cfg)
	if diagnostics["first"] != queueContractIDConflict || diagnostics["second"] != queueContractIDConflict || diagnostics["copied"] != "" {
		t.Fatalf("conflicting rename diagnostics: %v", diagnostics)
	}
	for i, id := range []string{"first", "second", "copied"} {
		if resolved.Sources[i].ID != id {
			t.Fatalf("conflicting contract replaced private source %s: %+v", id, resolved.Sources)
		}
	}
}

func writeTestQueueContract(t *testing.T, root, text string) {
	t.Helper()
	path := filepath.Join(root, queueProposalFile)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
