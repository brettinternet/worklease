package queue

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/testkit"
)

func beadsFixture(t *testing.T) (string, *BeadsAdapter, Source) {
	t.Helper()
	binary, err := exec.LookPath("bd")
	if err != nil {
		t.Skip("bd 1.3.0 is not installed")
	}
	version, err := exec.Command(binary, "version").Output()
	if err != nil || !strings.HasPrefix(string(version), "bd version 1.3.0 (") {
		t.Skip("bd 1.3.0 is required")
	}
	root := t.TempDir()
	git := testkit.GitCommand("-C", root, "init")
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	cmd := exec.Command(binary, "init", "--non-interactive", "--skip-hooks", "--skip-agents", "--prefix", "probe")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bd init: %v: %s", err, out)
	}
	a := NewBeadsAdapter()
	a.Binary = binary
	source, err := a.Resolve(context.Background(), map[string]string{"id": "fixture", "checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	return binary, a, source
}

func beadsCommand(t *testing.T, binary, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bd %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestBeadsAdapterConformance(t *testing.T) {
	t.Parallel()
	testkit.Home(t)
	binary, a, source := beadsFixture(t)
	first := beadsCommand(t, binary, source.Locator, "create", "First", "--silent")
	second := beadsCommand(t, binary, source.Locator, "create", "Second", "--description", "Full details", "--deps", "blocked-by:"+first, "--silent")
	beadsCommand(t, binary, source.Locator, "update", second, "--assignee", "alice", "--status", "in_progress")
	page, err := a.List(context.Background(), source, Query{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Coverage.State != CoverageComplete {
		t.Fatalf("page: %+v", page)
	}
	ref := Ref{SourceID: source.ID, ItemID: second}
	deps, err := a.ReadDependencies(context.Background(), source, ref, "", 100)
	if err != nil || len(deps.Edges) != 1 || deps.Edges[0].Type != HardPrerequisite || deps.Edges[0].To.ItemID != first {
		t.Fatalf("bulk edges: %+v", deps)
	}
	outcomes := a.ReadItems(context.Background(), source, []Ref{ref}, nil, 1)
	if outcomes[0].Kind != "found" || outcomes[0].Item.RawStatus != "in_progress" || outcomes[0].Item.AssignedTo[0] != "alice" || outcomes[0].Item.Claim.Active {
		t.Fatalf("detail: %+v", outcomes)
	}
	registry := &Registry{adapters: map[string]Adapter{"beads": a}}
	loader := NewLoader(registry)
	for range loader.Refresh(context.Background(), []Source{source}) {
	}
	if cached, found := loader.Store.Item(ref); !found || cached.Body != "" {
		t.Fatalf("brief list hydrated body unexpectedly: %+v", cached)
	}
	for range loader.HydrateDetail(context.Background(), source, ref) {
	}
	if selected, found := loader.Store.Item(ref); !found || selected.Body != "Full details" || selected.ReadOutcome != "found" {
		t.Fatalf("selected detail: %+v", selected)
	}
	capabilities, err := a.Capabilities(context.Background(), source, "alice", &ref)
	if err != nil || capabilities["dependencies"].Support != Supported || capabilities["native-claims"].Support != Unsupported {
		t.Fatalf("capabilities: %+v %v", capabilities, err)
	}
	if _, ok := NewRegistry().Get("beads"); !ok {
		t.Fatal("built-in adapter not registered")
	}
	if status := beadsCommand(t, binary, source.Locator, "dolt", "status", "--json"); !strings.Contains(status, `"server_running": false`) {
		t.Fatalf("unexpected daemon: %s", status)
	}
	if data, err := os.ReadFile(filepath.Join(source.Locator, ".beads", "metadata.json")); err != nil || len(data) == 0 {
		t.Fatal(err)
	}
}

func TestBeadsTypedEdgesAndCycleAreNotReady(t *testing.T) {
	t.Parallel()
	testkit.Home(t)
	a := NewBeadsAdapter()
	source := Source{ID: "s", Adapter: "beads"}
	a.terminal["s"] = "shipped"
	if !a.summary(source, beadsIssue{ID: "complete", Status: "shipped"}, nil).Terminal {
		t.Fatal("configured completion status was not terminal")
	}
	first := Ref{SourceID: "s", ItemID: "one"}
	second := Ref{SourceID: "s", ItemID: "two"}
	issues := []beadsIssue{{ID: "one", Title: "One", Status: "open", Dependencies: []beadsDependency{{IssueID: "one", DependsOnID: "two", Type: "blocks"}, {IssueID: "one", DependsOnID: "two", Type: "parent-child"}, {IssueID: "one", DependsOnID: "two", Type: "discovered-from"}}}, {ID: "two", Title: "Two", Status: "open", Dependencies: []beadsDependency{{IssueID: "two", DependsOnID: "one", Type: "blocks"}}}}
	items := map[string]Item{}
	for _, issue := range issues {
		ref := Ref{SourceID: "s", ItemID: issue.ID}
		edges := a.dependencyPage(source, ref, issue)
		if issue.ID == "one" && (len(edges.Edges) != 3 || edges.Edges[0].Type != HardPrerequisite || edges.Edges[1].Type != ParentChild || edges.Edges[2].Type != Related) {
			t.Fatalf("typed edges: %+v", edges)
		}
		item := Item{Summary: a.summary(source, issue, nil), TerminalKnown: true, DependenciesKnown: true, Closure: CoverageComplete, ReadPermission: Allowed, Relationships: edges.Edges}
		for _, edge := range edges.Edges {
			if edge.Type == HardPrerequisite {
				item.Dependencies = append(item.Dependencies, edge.To)
			}
		}
		items[ref.Key()] = item
	}
	evaluated := Recompute(items, CoverageComplete)
	if evaluated[first.Key()].Readiness.Status == Ready || evaluated[second.Key()].Readiness.Status == Ready {
		t.Fatalf("cycle became ready: %+v", evaluated)
	}
}

func TestBeadsWriteReadbackKeepsGitStage(t *testing.T) {
	t.Parallel()
	testkit.Home(t)
	binary, a, source := beadsFixture(t)
	id := beadsCommand(t, binary, source.Locator, "create", "Write target", "--silent")
	hookMarker := filepath.Join(source.Locator, "hook-ran")
	hook := filepath.Join(source.Locator, ".git", "hooks", "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf invoked > "+shellQuote(hookMarker)+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(source.Locator, "unrelated.txt")
	if err := os.WriteFile(staged, []byte("keep staged"), 0600); err != nil {
		t.Fatal(err)
	}
	git := testkit.GitCommand("-C", source.Locator, "add", "unrelated.txt")
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git stage: %v: %s", err, out)
	}
	git = testkit.GitCommand("-C", source.Locator, "diff", "--cached", "--name-only")
	stagedBefore, err := git.Output()
	if err != nil || !strings.Contains(string(stagedBefore), "unrelated.txt") {
		t.Fatalf("initial stage: %v %s", err, stagedBefore)
	}
	writer := &BeadsWriteAdapter{BeadsAdapter: a, Me: "alice"}
	ctx := context.Background()
	ref := Ref{SourceID: source.ID, ItemID: id}
	for _, action := range []Action{ActionRecordProgress, ActionAssignToMe, ActionReportBlocked} {
		intent := WriteIntent{OperationID: "aabbccddeeff00112233445566778899", Source: source, Ref: ref, Principal: "alice", Action: action}
		switch action {
		case ActionRecordProgress:
			intent.Append = "Progress"
			intent.Patch = map[string]string{"append": "comment"}
		case ActionReportBlocked:
			intent.Transition = "blocked"
			intent.Patch = map[string]string{"status": "blocked"}
		}
		prepared, _, err := writer.Prepare(ctx, intent)
		if err != nil {
			t.Fatalf("prepare %s: %v", action, err)
		}
		receipt, err := writer.Write(ctx, prepared)
		if err != nil {
			t.Fatalf("write %s: %v", action, err)
		}
		observed, err := writer.ReadReceipt(ctx, prepared, &receipt)
		if err != nil {
			t.Fatalf("readback %s: %v", action, err)
		}
		if got := checkWriteEvidence(prepared, &receipt, observed); got != WriteVerified {
			t.Fatalf("%s evidence: %s %+v", action, got, observed)
		}
	}
	beadsCommand(t, binary, source.Locator, "config", "set", "export.auto", "true")
	previewIntent := WriteIntent{OperationID: "bbccddeeff00112233445566778899aa", Source: source, Ref: ref, Principal: "alice", Action: ActionRecordProgress, Append: "Another note", Patch: map[string]string{"append": "comment"}}
	if _, preview, err := writer.Prepare(ctx, previewIntent); err != nil || !preview.AutoExport {
		t.Fatalf("auto-export disclosure: %+v %v", preview, err)
	}
	git = testkit.GitCommand("-C", source.Locator, "diff", "--cached", "--name-only")
	out, err := git.Output()
	if err != nil || string(out) != string(stagedBefore) {
		t.Fatalf("staged changed: %v before %q, after %q", err, stagedBefore, out)
	}
	if _, err := os.Stat(hookMarker); !os.IsNotExist(err) {
		t.Fatalf("Beads write ran Git hook: %v", err)
	}
}

func TestBeadsRejectsVersionDriftDuplicatesAndRemoteWithoutConsent(t *testing.T) {
	t.Parallel()
	testkit.Home(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".beads"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".beads", "metadata.json"), []byte(`{"dolt_mode":"embedded"}`), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(root, "bd")
	script := `#!/bin/sh
case " $* " in
  *' version '*) printf '%s\n' '{"version":"1.3.0"}' ;;
  *' sync.remote '*) printf '%s\n' '{"value":""}' ;;
  *' vc status '*) printf '%s\n' '{"commit":"abc"}' ;;
  *' --ready '*) printf '%s\n' '[]' ;;
  *' list '*) printf '%s\n' '[{"id":"probe-1","title":"One","status":"open"},{"id":"probe-1","title":"Two","status":"open"}]' ;;
esac
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	a := NewBeadsAdapter()
	a.Binary = binary
	source, err := a.Resolve(context.Background(), map[string]string{"id": "s", "checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.List(context.Background(), source, Query{}, ""); err == nil {
		t.Fatal("duplicate IDs accepted")
	}
	if err := os.WriteFile(binary, []byte(strings.Replace(script, `"version":"1.3.0"`, `"version":"1.4.0"`, 1)), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = a.List(context.Background(), source, Query{}, ""); err == nil || !strings.Contains(err.Error(), "unsupported-version") {
		t.Fatalf("version drift: %v", err)
	}
	if err := os.WriteFile(binary, []byte(strings.Replace(script, `[{"id":"probe-1","title":"One","status":"open"},{"id":"probe-1","title":"Two","status":"open"}]`, `{malformed`, 1)), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = a.List(context.Background(), source, Query{}, ""); err == nil || !strings.Contains(err.Error(), "schema-mismatch") {
		t.Fatalf("unparseable output: %v", err)
	}
	concurrent := strings.Replace(script, `*' vc status '*) printf '%s\n' '{"commit":"abc"}' ;;`, `*' vc status '*) if [ -f marker ]; then printf '%s\n' '{"commit":"def"}'; else touch marker; printf '%s\n' '{"commit":"abc"}'; fi ;;`, 1)
	concurrent = strings.Replace(concurrent, `[{"id":"probe-1","title":"One","status":"open"},{"id":"probe-1","title":"Two","status":"open"}]`, `[{"id":"probe-1","title":"One","status":"open"}]`, 1)
	if err := os.WriteFile(binary, []byte(concurrent), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = a.List(context.Background(), source, Query{}, ""); err == nil || !strings.Contains(err.Error(), "observation-invalidated") {
		t.Fatalf("concurrent Dolt commit: %v", err)
	}
	if _, ok := a.CachedEdges(source, Ref{SourceID: source.ID, ItemID: "probe-1"}); ok {
		t.Fatal("partial read published edges")
	}
	if err := os.WriteFile(binary, []byte(strings.Replace(script, `"value":""`, `"value":"git+ssh://example.invalid/repo"`, 1)), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = a.Resolve(context.Background(), map[string]string{"id": "remote", "checkout": root}); err == nil || !strings.Contains(err.Error(), "git-network-consent") {
		t.Fatalf("remote without consent: %v", err)
	}
}

func TestBeadsWritePipelineCheckpointAndRecovery(t *testing.T) {
	t.Parallel()
	binary, a, source := beadsFixture(t)
	id := beadsCommand(t, binary, source.Locator, "create", "Pipeline target", "--silent")
	writer := &BeadsWriteAdapter{BeadsAdapter: a, Me: "alice"}
	root := t.TempDir()
	journal, err := NewWriteJournal(filepath.Join(root, "recovery"), filepath.Join(root, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	claim := &writeFixture{}
	pipeline := WritePipeline{Adapter: writer, Claim: claim, Journal: journal}
	opID, err := NewWriteOperationID()
	if err != nil {
		t.Fatal(err)
	}
	intent := WriteIntent{OperationID: opID, Source: source, Ref: Ref{SourceID: source.ID, ItemID: id}, Principal: "alice", Action: ActionRecordProgress, Append: "Pipeline progress", Patch: map[string]string{"append": "comment"}, AuthorityID: "test-authority", ClaimID: "test-claim", ClaimRevision: 1, Resources: []string{"test-key"}, CheckpointTTL: time.Minute, CheckpointNotAfter: time.Now().Add(time.Hour)}
	prepared, _, err := writer.Prepare(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	result, err := pipeline.Start(context.Background(), prepared)
	if err != nil || result.Outcome != WriteVerified || claim.checkpointCalls != 1 {
		t.Fatalf("pipeline: %+v %v (checkpoints %d)", result, err, claim.checkpointCalls)
	}
	result, err = pipeline.Recover(context.Background(), opID)
	if err != nil || result.Outcome != WriteVerified || claim.checkpointCalls != 1 {
		t.Fatalf("recovery redispatched: %+v %v", result, err)
	}
	comments, err := writer.comments(context.Background(), prepared)
	if err != nil || len(comments) != 1 || !strings.Contains(comments[0].Text, prepared.Marker) {
		t.Fatalf("provider comment count: %+v %v", comments, err)
	}
}

func TestBeadsMissingAndVersionDrift(t *testing.T) {
	t.Parallel()
	testkit.Home(t)
	a := NewBeadsAdapter()
	a.Binary = "missing-bd-for-test"
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".beads"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".beads", "metadata.json"), []byte(`{"dolt_mode":"embedded"}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := a.Resolve(context.Background(), map[string]string{"checkout": root})
	if d, ok := err.(BeadsDiagnostic); !ok || d.Code != "cli-missing" {
		t.Fatalf("diagnostic: %v", err)
	}
}
