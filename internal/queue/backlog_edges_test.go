package queue

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func gitForBacklogEdges(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}

func TestBacklogEdgeInvalidationAndIndependentPrerequisite(t *testing.T) {
	root, binary := fakeBacklog(t)
	gitForBacklogEdges(t, root, "init", "-b", "main")
	gitForBacklogEdges(t, root, "add", ".")
	gitForBacklogEdges(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "seed")
	path := filepath.Join(root, "task.md")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	view := strings.Replace(string(backlogFixture(t, "backlog-view.json")), `"description":"Body"`, `"path":"task.md","description":"Body"`, 1)
	if err := os.WriteFile(filepath.Join(root, "backlog-view.json"), []byte(view), 0600); err != nil {
		t.Fatal(err)
	}
	a := NewBacklogAdapter("Done")
	a.Binary = binary
	ctx := context.Background()
	source, err := a.Resolve(ctx, map[string]string{"checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	ref := Ref{source.ID, "TASK-2"}
	list := func() {
		t.Helper()
		if _, err := a.List(ctx, source, Query{}, ""); err != nil {
			t.Fatal(err)
		}
	}
	list()
	if _, err := a.RefreshClosure(ctx, source, ref); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.CachedEdges(source, ref); !ok {
		_, _, generation, valid := a.QueueCacheIdentity(source)
		t.Fatalf("expected observed edge: partition=%q generation=%q valid=%t edges=%+v diagnostics=%+v", a.partitions[source.ID], generation, valid, a.edges, a.Diagnostics(source))
	}
	// Minute-resolution updatedAt stays unchanged; file metadata is only a
	// signal to invalidate, not proof of an unchanged dependency set.
	if err := os.WriteFile(path, []byte("new contents"), 0600); err != nil {
		t.Fatal(err)
	}
	list()
	if _, ok := a.CachedEdges(source, ref); ok {
		t.Fatal("same-minute file change retained edge")
	}
	if _, err := a.RefreshClosure(ctx, source, ref); err != nil {
		t.Fatal(err)
	}
	// Changing a prerequisite does not change the dependent file; its own
	// list observation must still determine whether the edge is satisfied.
	changed := strings.Replace(string(backlogFixture(t, "backlog-list.json")), `"status":"Done"`, `"status":"To Do"`, 1)
	if err := os.WriteFile(filepath.Join(root, "backlog-list.json"), []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	list()
	if _, ok := a.CachedEdges(source, ref); !ok {
		t.Fatal("unmodified dependent edge observation lost")
	}
	registry := NewRegistry()
	registry.adapters["backlog-md"] = a
	loader := NewLoader(registry)
	for range loader.Refresh(ctx, []Source{source}) {
	}
	item, _ := loader.Store.Current().Item(ref)
	if item.Readiness.Status != Blocked {
		t.Fatalf("changed prerequisite should block dependent: %+v", item.Readiness)
	}
	gitForBacklogEdges(t, root, "switch", "-c", "other")
	list()
	if _, ok := a.CachedEdges(source, ref); ok {
		t.Fatal("branch switch retained edges")
	}
	if _, err := a.RefreshClosure(ctx, source, ref); err != nil {
		t.Fatal(err)
	}
	// An unreadable Git identity cannot retain the previous HEAD partition.
	t.Setenv("PATH", root)
	list()
	if _, ok := a.CachedEdges(source, ref); ok {
		t.Fatal("failed Git probe retained previous edge partition")
	}
	a.InvalidateEdges(source) // a lost watch or overflow has the same effect
	if _, ok := a.CachedEdges(source, ref); ok {
		t.Fatal("watch failure retained edges")
	}
}

func TestInFlightBacklogViewCannotRestoreInvalidatedEdges(t *testing.T) {
	root, binary := fakeBacklog(t)
	original, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	script := strings.Replace(string(original), "/bin/cat backlog-view.json;;", "printf ready > read-started; /bin/cat backlog-view.json; sleep 0.3;;", 1)
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	gitForBacklogEdges(t, root, "init", "-b", "main")
	gitForBacklogEdges(t, root, "add", ".")
	gitForBacklogEdges(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "seed")
	a := NewBacklogAdapter("Done")
	a.Binary = binary
	source, err := a.Resolve(context.Background(), map[string]string{"checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.List(context.Background(), source, Query{}, ""); err != nil {
		t.Fatal(err)
	}
	ref := Ref{source.ID, "TASK-2"}
	result := make(chan error, 1)
	go func() { _, err := a.RefreshClosure(context.Background(), source, ref); result <- err }()
	deadline := time.After(2 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "read-started")); err == nil {
			break
		}
		select {
		case <-deadline:
			t.Fatal("view never started")
		case <-time.After(10 * time.Millisecond):
		}
	}
	a.InvalidateEdges(source)
	if err := <-result; !diag(err, "observation-invalidated") {
		t.Fatalf("in-flight view restored invalidated edges: %v", err)
	}
	if _, ok := a.CachedEdges(source, ref); ok {
		t.Fatal("invalidated view populated cache")
	}
}

func TestBacklogActionClosureRereadsEveryPrerequisite(t *testing.T) {
	root, binary := fakeBacklog(t)
	original, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	script := strings.Replace(string(original), "  'task view TASK-2 --json')", "  'task view TASK-1 --json') /bin/cat prerequisite.json;;\n  'task view TASK-2 --json')", 1)
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	dependent := `{"kind":"task-view","schemaVersion":1,"task":{"id":"TASK-2","status":"To Do","dependencies":["TASK-1"]}}`
	prerequisite := `{"kind":"task-view","schemaVersion":1,"task":{"id":"TASK-1","status":"Done","dependencies":[]}}`
	if err := os.WriteFile(filepath.Join(root, "backlog-view.json"), []byte(dependent), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prerequisite.json"), []byte(prerequisite), 0600); err != nil {
		t.Fatal(err)
	}
	a := NewBacklogAdapter("Done")
	a.Binary = binary
	source, err := a.Resolve(context.Background(), map[string]string{"checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	ref := Ref{source.ID, "TASK-2"}
	first, err := a.RefreshActionClosure(context.Background(), source, ref)
	if err != nil || first[ref.Key()].Readiness.Status != Ready {
		t.Fatalf("first action closure: %v %+v", err, first)
	}
	prerequisite = strings.Replace(prerequisite, `"status":"Done"`, `"status":"To Do"`, 1)
	if err := os.WriteFile(filepath.Join(root, "prerequisite.json"), []byte(prerequisite), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := a.RefreshActionClosure(context.Background(), source, ref)
	if err != nil || second[ref.Key()].Readiness.Status != Blocked {
		t.Fatalf("cached prerequisite leaked into action check: %v %+v", err, second)
	}
}

func TestBacklogFilesystemWatchInvalidatesSameMinuteEdit(t *testing.T) {
	root, binary := fakeBacklog(t)
	if err := os.WriteFile(filepath.Join(root, "backlog.config.yml"), []byte("backlog_directory: records\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tasks := filepath.Join(root, "records", "tasks")
	if err := os.MkdirAll(tasks, 0700); err != nil {
		t.Fatal(err)
	}
	a := NewBacklogAdapter("Done")
	a.Binary = binary
	source, err := a.Resolve(context.Background(), map[string]string{"checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changed := make(chan struct{}, 1)
	finished := make(chan error, 1)
	go func() {
		finished <- a.WatchChanges(ctx, source, func() {
			select {
			case changed <- struct{}{}:
			default:
			}
		})
	}()
	// Wait for watcher registration without a fixed sleep; a config change in
	// the root directory is observable once registration completes.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("filesystem watch did not observe a change")
		case err := <-finished:
			t.Fatalf("filesystem watch stopped: %v", err)
		case <-changed:
			cancel()
			if err := <-finished; err != context.Canceled {
				t.Fatalf("watch shutdown: %v", err)
			}
			return
		default:
			// Rename in the watched task directory is the same signal as a task
			// edit; no updatedAt granularity is assumed.
			if err := os.WriteFile(filepath.Join(tasks, "changed.md"), []byte("edge"), 0600); err != nil {
				t.Fatal(err)
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
}

type recordingHydration struct {
	*fakeAdapter
	mu    sync.Mutex
	calls []string
}

func (r *recordingHydration) ReadItems(ctx context.Context, source Source, refs []Ref, fields []string, budget int) []ItemOutcome {
	r.mu.Lock()
	r.calls = append(r.calls, refs[0].ItemID)
	r.mu.Unlock()
	return r.fakeAdapter.ReadItems(ctx, source, refs, fields, budget)
}

func TestEdgeHydrationOrdersSelectedClosureBeforeVisibleAndBackground(t *testing.T) {
	source := Source{ID: "s", Adapter: "recorded"}
	refs := []Ref{{"s", "selected"}, {"s", "prerequisite"}, {"s", "visible"}, {"s", "background"}}
	adapter := &recordingHydration{fakeAdapter: newFake()}
	registry := NewRegistry()
	if err := registry.Register("recorded", adapter); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(registry)
	loader.HydrationLimit = 1
	items := map[string]Item{}
	for _, ref := range refs {
		item := Item{Summary: Summary{Ref: ref, Fresh: true, Terminal: true}, TerminalKnown: true, DependenciesKnown: false, Closure: CoverageUnknown}
		items[ref.Key()] = item
		adapter.outcomes[ref.Key()] = []ItemOutcome{{Ref: ref, Kind: "found", Item: &item}}
	}
	adapter.edges[refs[0].Key()] = []DependencyPage{{Edges: []Relationship{{Type: HardPrerequisite, From: refs[0], To: refs[1], Condition: "terminal", Fresh: true, Support: Supported}}, Completeness: CoverageComplete}}
	loader.Store.SeedSnapshot(Snapshot{Items: items, Sources: map[string]Coverage{"s": {State: CoverageComplete, Total: 4}}})
	for range loader.HydrateEdges(context.Background(), source, refs[:1], refs[2:3], true) {
	}
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if strings.Join(adapter.calls, ",") != "selected,prerequisite,visible,background" {
		t.Fatalf("hydration order: %v", adapter.calls)
	}
}

func TestSelectedClosureTraversesKnownEdgeToUnknownPrerequisite(t *testing.T) {
	source := Source{ID: "s", Adapter: "recorded"}
	selected := Ref{"s", "selected"}
	prerequisite := Ref{"s", "prerequisite"}
	adapter := &recordingHydration{fakeAdapter: newFake()}
	registry := NewRegistry()
	if err := registry.Register("recorded", adapter); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(registry)
	known := Item{Summary: Summary{Ref: selected, Fresh: true}, DependenciesKnown: true, Closure: CoverageComplete,
		Relationships: []Relationship{{Type: HardPrerequisite, From: selected, To: prerequisite, Condition: "terminal", Fresh: true, Support: Supported}}}
	unknown := Item{Summary: Summary{Ref: prerequisite, Fresh: true, Terminal: true}, TerminalKnown: true, Closure: CoverageUnknown}
	adapter.outcomes[prerequisite.Key()] = []ItemOutcome{{Ref: prerequisite, Kind: "found", Item: &unknown}}
	loader.Store.SeedSnapshot(Snapshot{Items: map[string]Item{selected.Key(): known, prerequisite.Key(): unknown}, Sources: map[string]Coverage{"s": {State: CoverageComplete}}})
	for range loader.HydrateEdges(context.Background(), source, []Ref{selected}, nil, false) {
	}
	if strings.Join(adapter.calls, ",") != "prerequisite" {
		t.Fatalf("known selected edge did not hydrate unknown prerequisite: %v", adapter.calls)
	}
}

func TestBacklogBulkEdgesAndPartialCoverage(t *testing.T) {
	root, binary := fakeBacklog(t)
	// Bulk fields are optional: an empty array is a complete zero-edge set.
	bulk := `{"kind":"task-list","schemaVersion":1,"tasks":[{"id":"TASK-2","title":"Second","status":"To Do","updatedAt":"2026-09-23T12:00:00Z","dependencies":["TASK-1"]},{"id":"TASK-1","title":"First","status":"To Do","updatedAt":"2026-09-23T12:00:00Z","dependencies":[]}]}`
	if err := os.WriteFile(filepath.Join(root, "backlog-list.json"), []byte(bulk), 0600); err != nil {
		t.Fatal(err)
	}
	a := NewBacklogAdapter("Done")
	a.Binary = binary
	source, err := a.Resolve(context.Background(), map[string]string{"checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.adapters["backlog-md"] = a
	loader := NewLoader(registry)
	for range loader.Refresh(context.Background(), []Source{source}) {
	}
	snapshot := loader.Store.Current()
	ref := Ref{source.ID, "TASK-2"}
	item, _ := snapshot.Item(ref)
	if item.ReadOutcome != "found" || item.Readiness.Status != Blocked || snapshot.Sources[source.ID].ObservedEdges != 2 {
		t.Fatalf("bulk edge and blocker lost: %+v, %+v", item, snapshot.Sources[source.ID])
	}
	for range loader.HydrateEdges(context.Background(), source, []Ref{ref}, nil, true) {
		t.Fatal("bulk dependency fields triggered a per-task background view")
	}
	// Incomplete coverage without a known blocker is unknown; a known blocker
	// wins even if the remainder of the graph is not observed.
	item.DependenciesKnown = false
	item.Closure = CoverageUnknown
	items := snapshot.Items
	items[ref.Key()] = item
	if got := Recompute(items, CoverageComplete)[ref.Key()].Readiness.Status; got != Blocked {
		t.Fatalf("known blocker lost under partial coverage: %s", got)
	}
	prereq := Ref{source.ID, "TASK-1"}
	p := items[prereq.Key()]
	p.Terminal = true
	items[prereq.Key()] = p
	if got := Recompute(items, CoverageComplete)[ref.Key()].Readiness.Status; got != ReadinessUnknown {
		t.Fatalf("incomplete unblocked closure should be unknown: %s", got)
	}
	// Explicit checks always issue a provider read, not the bulk/cache result.
	a.Timeout = time.Second
	if _, err := a.RefreshClosure(context.Background(), source, ref); err != nil {
		t.Fatal(err)
	}
}
