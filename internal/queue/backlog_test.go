package queue

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/testkit"
)

func backlogFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func fakeBacklog(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "backlog.config.yml"), []byte("project_name: test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"backlog-list.json", "backlog-view.json"} {
		if err := os.WriteFile(filepath.Join(root, name), backlogFixture(t, name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(root, "backlog")
	content := "#!/bin/sh\ncase \"$*\" in\n  '--version') echo 1.52.0;;\n  'config get remoteOperations'|'config get checkActiveBranches'|'config get autoCommit'|'config get bypassGitHooks') echo false;;\n  'task list --json') /bin/cat backlog-list.json;;\n  'task view TASK-2 --json') /bin/cat backlog-view.json;;\n  *) echo 'secret\033[31m' >&2; exit 1;;\nesac\n"
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	return root, script
}
func TestBacklogCacheIdentityChangesWithCheckoutInstanceAndConfig(t *testing.T) {
	parent := t.TempDir()
	checkout := filepath.Join(parent, "checkout")
	if err := os.MkdirAll(checkout, 0700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-b", "main", checkout}, {"-C", checkout, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial"}} {
		if output, err := testkit.GitCommand(args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	a := NewBacklogAdapter()
	source := Source{ID: "s", Locator: checkout}
	_, _, original, ok := a.QueueCacheIdentity(source)
	if !ok {
		t.Fatal("initial cache identity unavailable")
	}
	if output, err := testkit.GitCommand("-C", checkout, "checkout", "-b", "other").CombinedOutput(); err != nil {
		t.Fatalf("git branch switch: %v: %s", err, output)
	}
	_, _, branched, ok := a.QueueCacheIdentity(source)
	if !ok || branched == original {
		t.Fatal("branch switch reused cache identity")
	}
	original = branched
	if err := os.WriteFile(filepath.Join(checkout, "backlog.config.yml"), []byte("project_name: changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, configured, ok := a.QueueCacheIdentity(source)
	if !ok || configured == original {
		t.Fatal("configuration change reused partition")
	}
	if err := os.Rename(checkout, filepath.Join(parent, "previous")); err != nil {
		t.Fatal(err)
	}
	if output, err := testkit.GitCommand("init", "-b", "main", checkout).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if output, err := testkit.GitCommand("-C", checkout, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "replacement").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	_, _, replaced, ok := a.QueueCacheIdentity(source)
	if !ok || replaced == original {
		t.Fatal("replacement checkout reused partition")
	}
}

func TestBacklogGoldenAndDiagnostics(t *testing.T) {
	root, binary := fakeBacklog(t)
	a := NewBacklogAdapter("Done")
	a.Binary = binary
	ctx := context.Background()
	source, err := a.Resolve(ctx, map[string]string{"id": "fixture", "checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	page, err := a.List(ctx, source, Query{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Coverage.Total != 2 || page.Items[1].State != StateComplete || *page.Items[0].ProviderReady {
		t.Fatalf("unexpected page: %+v", page)
	}
	ref := Ref{SourceID: source.ID, ItemID: "TASK-2"}
	result := a.ReadItems(ctx, source, []Ref{ref}, nil, 0)
	if len(result) != 1 || result[0].Err != nil || result[0].Item.Body != "Body" {
		t.Fatalf("unexpected detail: %+v", result)
	}
	deps, err := a.ReadDependencies(ctx, source, ref, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if deps.Completeness != CoveragePartial || len(deps.Edges) != 3 || deps.Edges[2].Type != ParentChild || deps.Edges[0].Condition != "terminal" {
		t.Fatalf("unexpected dependencies: %+v", deps)
	}
	if _, err := a.ReadDependencies(ctx, source, Ref{SourceID: source.ID, ItemID: "TASK-SECRET"}, "", 0); err == nil || strings.Contains(err.Error(), "secret\x1b") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("stderr escaped: %v", err)
	}
	caps, _ := a.Capabilities(ctx, source, "", nil)
	if caps["native-claims"].Support != Unsupported || caps["mutation"].Permission != Denied {
		t.Fatal(caps)
	}
	if _, ok := NewRegistry().Get("backlog-md"); !ok {
		t.Fatal("adapter unregistered")
	}
	registry := NewRegistry()
	registry.adapters["backlog-md"] = a
	loader := NewLoader(registry)
	for range loader.Refresh(ctx, []Source{source}) {
	}
	if item, ok := loader.Store.Current().Item(ref); !ok || item.ReadOutcome != "summary-only" {
		t.Fatalf("list triggered detail reads: %+v %t", item, ok)
	}
	for _, kind := range []string{"task-view", "task-list"} {
		var out struct {
			Tasks []backlogTask `json:"tasks"`
		}
		if err := decodeBacklog([]byte(`{"kind":"`+kind+`","schemaVersion":2,"tasks":[]}`), "task-list", &out); err == nil {
			t.Fatal("accepted wrong schema")
		}
	}
	// Duplicate IDs are source diagnostics, not silently resolved to distinct keys.
	duplicate := []byte(`{"kind":"task-list","schemaVersion":1,"tasks":[{"id":"X"},{"id":"X"}]}`)
	if err := os.WriteFile(filepath.Join(root, "backlog-list.json"), duplicate, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.List(ctx, source, Query{}, ""); err != nil {
		t.Fatal(err)
	}
	if ids := a.Diagnostics(source).DuplicateIDs; len(ids) != 1 || ids[0] != "X" {
		t.Fatal(ids)
	}
}
func TestBacklogEffectsVersionAndBounds(t *testing.T) {
	root, binary := fakeBacklog(t)
	a := NewBacklogAdapter()
	a.Binary = binary
	if _, err := a.Resolve(context.Background(), map[string]string{"checkout": t.TempDir()}); !diag(err, "not-backlog-project") {
		t.Fatal(err)
	}
	a.Binary = filepath.Join(root, "absent")
	if _, err := a.Resolve(context.Background(), map[string]string{"checkout": root}); !diag(err, "cli-missing") {
		t.Fatal(err)
	}
	a.Binary = binary
	script, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte(strings.Replace(string(script), "echo 1.52.0", "echo 2.0.0", 1)), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Resolve(context.Background(), map[string]string{"checkout": root}); !diag(err, "unsupported-version") {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte(strings.Replace(string(script), "'config get remoteOperations'|'config get checkActiveBranches'|'config get autoCommit'|'config get bypassGitHooks') echo false", "'config get remoteOperations') echo true;;\n  'config get checkActiveBranches'|'config get autoCommit'|'config get bypassGitHooks') echo false", 1)), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Resolve(context.Background(), map[string]string{"checkout": root}); !diag(err, "git-network-consent") {
		t.Fatal(err)
	}
	// A source already resolved without consent must also stop if settings change.
	if err := os.WriteFile(binary, script, 0700); err != nil {
		t.Fatal(err)
	}
	drySource, err := a.Resolve(context.Background(), map[string]string{"id": "dry", "checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte(strings.Replace(string(script), "'config get remoteOperations'|'config get checkActiveBranches'|'config get autoCommit'|'config get bypassGitHooks') echo false", "'config get remoteOperations') echo true;;\n  'config get checkActiveBranches'|'config get autoCommit'|'config get bypassGitHooks') echo false", 1)), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := a.List(context.Background(), drySource, Query{}, ""); !diag(err, "git-network-consent") {
		t.Fatal(err)
	}
	source, err := a.Resolve(context.Background(), map[string]string{"checkout": root, "allowGitNetwork": "true"})
	if err != nil {
		t.Fatal(err)
	}
	if !a.Diagnostics(source).NetworkEffects {
		t.Fatal("missing network effect")
	}
	if err := os.WriteFile(binary, []byte(strings.Replace(string(script), "/bin/cat backlog-list.json", "sleep 5", 1)), 0700); err != nil {
		t.Fatal(err)
	}
	a.Timeout = 20 * time.Millisecond
	if _, err := a.List(context.Background(), source, Query{}, ""); !diag(err, "timed-out") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.List(ctx, source, Query{}, ""); !diag(err, "cancelled") {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte(strings.Replace(string(script), "/bin/cat backlog-list.json", "head -c 9000000 /dev/zero", 1)), 0700); err != nil {
		t.Fatal(err)
	}
	a.Timeout = time.Second
	if data, err := a.run(context.Background(), root, binary, "task", "list", "--json"); !diag(err, "output-limit") {
		t.Fatalf("len=%d err=%v", len(data), err)
	}
}
func diag(err error, code string) bool {
	var d BacklogDiagnostic
	return errors.As(err, &d) && d.Code == code
}

// scratchGit runs git in a scratch repository without inherited GIT_*
// variables, which Git hooks set and which would redirect it to this repository.
func scratchGit(root string, args ...string) *exec.Cmd {
	cmd := testkit.GitCommand(args...)
	cmd.Dir = root
	return cmd
}

func TestBacklogGitFreshness(t *testing.T) {
	root, binary := fakeBacklog(t)
	for _, args := range [][]string{{"init", "-b", "main"}, {"add", "."}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "seed"}} {
		cmd := scratchGit(root, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	a := NewBacklogAdapter("Done")
	a.Binary = binary
	source, err := a.Resolve(context.Background(), map[string]string{"checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	initial := a.Diagnostics(source)
	if initial.Branch != "main" || len(initial.Head) != 40 || initial.Dirty {
		t.Fatalf("initial git freshness: %+v", initial)
	}
	if err := os.WriteFile(filepath.Join(root, "new-file"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.List(context.Background(), source, Query{}, ""); err != nil {
		t.Fatal(err)
	}
	if !a.Diagnostics(source).Dirty {
		t.Fatal("list did not refresh dirty state")
	}
}

func TestBacklogReadItemsMoreThanSchedulerCapacity(t *testing.T) {
	root, binary := fakeBacklog(t)
	a := NewBacklogAdapter()
	a.Binary = binary
	source := Source{ID: "fixture", Locator: root}
	a.consent[source.ID] = true
	refs := make([]Ref, 140)
	for i := range refs {
		refs[i] = Ref{SourceID: source.ID, ItemID: "TASK-2"}
	}
	outcomes := a.ReadItems(context.Background(), source, refs, nil, 0)
	if len(outcomes) != len(refs) {
		t.Fatalf("got %d outcomes", len(outcomes))
	}
	for i, outcome := range outcomes {
		if outcome.Kind != "found" || outcome.Err != nil {
			t.Fatalf("outcome %d: %+v", i, outcome)
		}
	}
}

func TestBacklogReadConcurrency(t *testing.T) {
	root, binary := fakeBacklog(t)
	a := NewBacklogAdapter()
	a.Binary = binary
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = a.run(context.Background(), root, binary, "--version") }()
	}
	wg.Wait()
	gate := quotaScheduler("backlog:"+root, 4)
	gate.mu.Lock()
	defer gate.mu.Unlock()
	if gate.running != 0 || gate.limit != 4 {
		t.Fatal("bounded slots leaked")
	}
}
func TestBacklogScratchCLI(t *testing.T) {
	t.Parallel()
	binary, err := exec.LookPath("backlog")
	if err != nil {
		t.Skipf("backlog CLI not installed: %v", err)
	}
	probeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(probeCtx, binary, "--version").CombinedOutput(); err != nil {
		t.Skipf("backlog --version probe failed: %v: %s", err, out)
	}
	root := t.TempDir()
	cmd := exec.Command("backlog", "init", "Scratch", "--defaults", "--no-git", "--config-location", "root", "--integration-mode", "none")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scratch init: %s: %v", out, err)
	}
	cmd = exec.Command("backlog", "task", "create", "Example")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("scratch create: %s: %v", out, err)
	}
	a := NewBacklogAdapter("Done")
	source, err := a.Resolve(context.Background(), map[string]string{"checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	page, err := a.List(context.Background(), source, Query{}, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("scratch list: %+v %v", page, err)
	}
	result := a.ReadItems(context.Background(), source, []Ref{page.Items[0].Ref}, nil, 0)
	if len(result) != 1 || result[0].Err != nil {
		t.Fatalf("scratch view: %+v", result)
	}
}
