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
	if _, err := a.ReadDependencies(ctx, source, Ref{SourceID: source.ID, ItemID: "-secret"}, "", 0); err == nil || strings.Contains(err.Error(), "secret\x1b") {
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
	if _, err := a.List(context.Background(), source, Query{}, ""); !diag(err, "cancelled") {
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
	if len(a.slots) != 0 || cap(a.slots) != 4 {
		t.Fatal("bounded slots leaked")
	}
}
func TestBacklogScratchCLI(t *testing.T) {
	if _, err := exec.LookPath("backlog"); err != nil {
		t.Skip("backlog CLI not installed")
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
