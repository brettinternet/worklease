package queue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/testkit"
	"github.com/fsnotify/fsnotify"
)

func gitForBacklogEdges(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := testkit.GitCommand(args...)
	cmd.Dir = root
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

func TestBacklogInvalidatedInFlightListCannotRestoreStaleEdges(t *testing.T) {
	t.Parallel()
	root, binary := fakeBacklog(t)
	oldList := []byte(`{"kind":"task-list","schemaVersion":1,"tasks":[{"id":"TASK-2","dependencies":["TASK-1"],"readiness":{"missingDependencies":[]}}]}`)
	newList := []byte(`{"kind":"task-list","schemaVersion":1,"tasks":[{"id":"TASK-2","dependencies":["TASK-3"],"readiness":{"missingDependencies":[]}}]}`)
	if err := os.WriteFile(filepath.Join(root, "backlog-list.json"), oldList, 0600); err != nil {
		t.Fatal(err)
	}
	a := NewBacklogAdapter("Done")
	a.Binary = binary
	source, err := a.Resolve(context.Background(), map[string]string{"checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	ref := Ref{SourceID: source.ID, ItemID: "TASK-2"}
	if _, err := a.List(context.Background(), source, Query{}, ""); err != nil {
		t.Fatal(err)
	}
	if deps, ok := a.CachedEdges(source, ref); !ok || len(deps.Edges) != 1 || deps.Edges[0].To.ItemID != "TASK-1" {
		t.Fatalf("initial edge cache: %+v, valid=%t", deps, ok)
	}
	if err := os.WriteFile(filepath.Join(root, "backlog-list-held.json"), oldList, 0600); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	releasePath := filepath.Join(root, "backlog-list-release")
	if err := syscall.Mkfifo(releasePath, 0600); err != nil {
		t.Fatal(err)
	}
	blocking := `  'task list --json') if [ ! -e backlog-list-started ]; then : > backlog-list-started; /bin/cat backlog-list-release >/dev/null; /bin/cat backlog-list-held.json; else /bin/cat backlog-list.json; fi;;`
	updated := strings.Replace(string(original), "  'task list --json') /bin/cat backlog-list.json;;", blocking, 1)
	if updated == string(original) {
		t.Fatal("fake backlog list command not found")
	}
	if err := os.WriteFile(binary, []byte(updated), 0700); err != nil {
		t.Fatal(err)
	}
	oldDone := make(chan error, 1)
	oldReceived := false
	started := false
	released := false
	releaseOldList := func() error {
		writer, err := os.OpenFile(releasePath, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		return writer.Close()
	}
	go func() {
		_, listErr := a.List(context.Background(), source, Query{}, "")
		oldDone <- listErr
	}()
	t.Cleanup(func() {
		if started && !released {
			if err := releaseOldList(); err != nil {
				t.Errorf("release in-flight list: %v", err)
			}
		}
		if !oldReceived {
			select {
			case <-oldDone:
			case <-time.After(5 * time.Second):
				t.Error("in-flight list did not finish after release")
			}
		}
	})
	deadline := time.NewTimer(3 * time.Second)
	poll := time.NewTicker(10 * time.Millisecond)
	defer deadline.Stop()
	defer poll.Stop()
	for {
		if _, err := os.Stat(filepath.Join(root, "backlog-list-started")); err == nil {
			started = true
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("pre-invalidation list did not start")
		case <-poll.C:
		}
	}
	a.InvalidateEdges(source)
	if err := os.WriteFile(filepath.Join(root, "backlog-list.json"), newList, 0600); err != nil {
		t.Fatal(err)
	}
	newDone := make(chan error, 1)
	go func() {
		_, listErr := a.List(context.Background(), source, Query{}, "")
		newDone <- listErr
	}()
	select {
	case err := <-newDone:
		if err != nil {
			t.Fatalf("post-invalidation list: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("post-invalidation list joined the held pre-invalidation read")
	}
	if deps, ok := a.CachedEdges(source, ref); !ok || len(deps.Edges) != 1 || deps.Edges[0].To.ItemID != "TASK-3" {
		t.Fatalf("post-invalidation edge cache: %+v, valid=%t", deps, ok)
	}
	if err := releaseOldList(); err != nil {
		t.Fatal(err)
	}
	released = true
	oldErr := <-oldDone
	oldReceived = true
	if !diag(oldErr, "observation-invalidated") {
		t.Fatalf("pre-invalidation list was not rejected: %v", oldErr)
	}
	if deps, ok := a.CachedEdges(source, ref); !ok || len(deps.Edges) != 1 || deps.Edges[0].To.ItemID != "TASK-3" {
		t.Fatalf("stale list replaced current edge cache: %+v, valid=%t", deps, ok)
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
	dependent := `{"kind":"task-view","schemaVersion":1,"task":{"id":"TASK-2","status":"To Do","dependencies":["TASK-1"],"readiness":{"missingDependencies":[]}}}`
	prerequisite := `{"kind":"task-view","schemaVersion":1,"task":{"id":"TASK-1","status":"Done","dependencies":[],"readiness":{"missingDependencies":[]}}}`
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

type manualBacklogWatchTicker struct{ ticks chan time.Time }

func (t *manualBacklogWatchTicker) C() <-chan time.Time { return t.ticks }
func (*manualBacklogWatchTicker) Stop()                 {}

type recordingBacklogWatcher struct {
	backlogWatcher
	added chan<- string
}

func (w recordingBacklogWatcher) Add(path string) error {
	if err := w.backlogWatcher.Add(path); err != nil {
		return err
	}
	select {
	case w.added <- path:
	default:
	}
	return nil
}

func TestBacklogTaskDirectoryCreatedAfterWatcherStartupIsObserved(t *testing.T) {
	t.Parallel()
	root, binary := fakeBacklog(t)
	if err := os.WriteFile(filepath.Join(root, "backlog.config.yml"), []byte("backlog_directory: records\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := NewBacklogAdapter("Done")
	a.Binary = binary
	a.reconcileInterval = time.Hour
	added := make(chan string, 32)
	tickers := make(chan *manualBacklogWatchTicker, 1)
	a.watcherFactory = func() (backlogWatcher, error) {
		watcher, err := newNativeBacklogWatcher()
		if err != nil {
			return nil, err
		}
		return recordingBacklogWatcher{backlogWatcher: watcher, added: added}, nil
	}
	a.tickerFactory = func(time.Duration) backlogWatchTicker {
		ticker := &manualBacklogWatchTicker{ticks: make(chan time.Time, 1)}
		tickers <- ticker
		return ticker
	}
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
	select {
	case <-tickers:
	case err := <-finished:
		t.Fatalf("watch stopped before startup: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not finish registration")
	}
	tasks := filepath.Join(source.Locator, "records", "tasks")
	if err := os.MkdirAll(tasks, 0700); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	var observed []string
	for {
		select {
		case path := <-added:
			observed = append(observed, path)
			if path == tasks {
				goto tasksWatched
			}
		case err := <-finished:
			t.Fatalf("watch stopped before task directory registration: %v", err)
		case <-deadline.C:
			t.Fatalf("new task directory was not registered; watched: %v", observed)
		}
	}
tasksWatched:
	taskPath := filepath.Join(tasks, "created.md")
	if err := os.WriteFile(taskPath, []byte("initial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(taskPath, []byte("edited"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case err := <-finished:
		t.Fatalf("watch stopped before task edit: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("task edit was missed before reconciliation")
	}
	cancel()
	if err := <-finished; err != context.Canceled {
		t.Fatalf("watch shutdown: %v", err)
	}
}

type failingBacklogWatcher struct {
	added  chan<- string
	events chan fsnotify.Event
	errors chan error
}

func (w failingBacklogWatcher) Add(path string) error {
	select {
	case w.added <- path:
	default:
	}
	return errors.New("injected watch registration failure")
}
func (w failingBacklogWatcher) Close() error                  { return nil }
func (w failingBacklogWatcher) Events() <-chan fsnotify.Event { return w.events }
func (w failingBacklogWatcher) Errors() <-chan error          { return w.errors }

func TestBacklogWatchRegistrationFailureInvalidatesAndReconciles(t *testing.T) {
	t.Parallel()
	root, binary := fakeBacklog(t)
	bulk := []byte(`{"kind":"task-list","schemaVersion":1,"tasks":[{"id":"TASK-2","dependencies":["TASK-1"],"readiness":{"missingDependencies":[]}}]}`)
	if err := os.WriteFile(filepath.Join(root, "backlog-list.json"), bulk, 0600); err != nil {
		t.Fatal(err)
	}
	a := NewBacklogAdapter("Done")
	a.Binary = binary
	source, err := a.Resolve(context.Background(), map[string]string{"checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	ref := Ref{SourceID: source.ID, ItemID: "TASK-2"}
	if _, err := a.List(context.Background(), source, Query{}, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.CachedEdges(source, ref); !ok {
		t.Fatal("test setup did not cache edges")
	}
	added := make(chan string, 16)
	a.watcherFactory = func() (backlogWatcher, error) {
		return failingBacklogWatcher{added: added, events: make(chan fsnotify.Event), errors: make(chan error)}, nil
	}
	tickers := make(chan *manualBacklogWatchTicker, 1)
	a.tickerFactory = func(time.Duration) backlogWatchTicker {
		ticker := &manualBacklogWatchTicker{ticks: make(chan time.Time, 1)}
		tickers <- ticker
		return ticker
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notified := make(chan struct{}, 1)
	finished := make(chan error, 1)
	go func() {
		finished <- a.WatchChanges(ctx, source, func() {
			select {
			case notified <- struct{}{}:
			default:
			}
		})
	}()
	var ticker *manualBacklogWatchTicker
	select {
	case ticker = <-tickers:
	case err := <-finished:
		t.Fatalf("watch registration failure stopped fallback: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("reconciliation ticker was not started after registration failure")
	}
	if _, ok := a.CachedEdges(source, ref); ok {
		t.Fatal("watch registration failure retained cached edges")
	}
	ticker.ticks <- time.Time{}
	select {
	case <-notified:
	case err := <-finished:
		t.Fatalf("watch stopped before fallback reconciliation: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("periodic fallback did not notify a reconciliation")
	}
	cancel()
	if err := <-finished; err != context.Canceled {
		t.Fatalf("watch shutdown: %v", err)
	}
}

func TestBacklogPeriodicReconciliationRelistsWithoutEvents(t *testing.T) {
	root, binary := fakeBacklog(t)
	if err := os.WriteFile(filepath.Join(root, "backlog.config.yml"), []byte("backlog_directory: records\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "records", "tasks"), 0700); err != nil {
		t.Fatal(err)
	}
	a := NewBacklogAdapter("Done")
	a.Binary = binary
	a.reconcileInterval = 25 * time.Millisecond
	source, err := a.Resolve(context.Background(), map[string]string{"checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	observed := make(chan error, 1)
	finished := make(chan error, 1)
	go func() {
		finished <- a.WatchChanges(ctx, source, func() {
			page, readErr := a.List(ctx, source, Query{}, "")
			if readErr == nil && len(page.Items) != 2 {
				readErr = BacklogDiagnostic{"incomplete", "periodic re-list omitted tasks"}
			}
			select {
			case observed <- readErr:
			default:
			}
		})
	}()
	select {
	case err := <-observed:
		if err != nil {
			t.Fatal(err)
		}
	case err := <-finished:
		t.Fatalf("watch stopped before reconciliation: %v", err)
	case <-ctx.Done():
		t.Fatal("periodic reconciliation did not re-list")
	}
	cancel()
	<-finished
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

type scriptedBacklogWatcher struct {
	events chan fsnotify.Event
	errors chan error
}

func (w scriptedBacklogWatcher) Add(string) error              { return nil }
func (w scriptedBacklogWatcher) Close() error                  { return nil }
func (w scriptedBacklogWatcher) Events() <-chan fsnotify.Event { return w.events }
func (w scriptedBacklogWatcher) Errors() <-chan error          { return w.errors }

func TestBacklogWatchIgnoresMetadataAndGitLockEvents(t *testing.T) {
	t.Parallel()
	root, binary := fakeBacklog(t)
	bulk := []byte(`{"kind":"task-list","schemaVersion":1,"tasks":[{"id":"TASK-2","dependencies":["TASK-1"],"readiness":{"missingDependencies":[]}}]}`)
	if err := os.WriteFile(filepath.Join(root, "backlog-list.json"), bulk, 0600); err != nil {
		t.Fatal(err)
	}
	a := NewBacklogAdapter("Done")
	a.Binary = binary
	source, err := a.Resolve(context.Background(), map[string]string{"checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	ref := Ref{SourceID: source.ID, ItemID: "TASK-2"}
	if _, err := a.List(context.Background(), source, Query{}, ""); err != nil {
		t.Fatal(err)
	}
	watcher := scriptedBacklogWatcher{events: make(chan fsnotify.Event), errors: make(chan error)}
	a.watcherFactory = func() (backlogWatcher, error) { return watcher, nil }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- a.WatchChanges(ctx, source, func() {}) }()
	// Each unbuffered send returns only after the previous event was handled.
	send := func(event fsnotify.Event) {
		t.Helper()
		select {
		case watcher.events <- event:
		case err := <-finished:
			t.Fatalf("watch stopped: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("watch did not receive event")
		}
	}
	task := filepath.Join(root, "backlog", "tasks", "task-2.md")
	lock := fsnotify.Event{Name: filepath.Join(root, ".git", "index.lock"), Op: fsnotify.Remove}
	send(fsnotify.Event{Name: task, Op: fsnotify.Chmod})
	send(lock)
	send(lock)
	if _, ok := a.CachedEdges(source, ref); !ok {
		t.Fatal("metadata or Git lock event invalidated cached edges")
	}
	send(fsnotify.Event{Name: task, Op: fsnotify.Write})
	send(lock)
	if _, ok := a.CachedEdges(source, ref); ok {
		t.Fatal("task write did not invalidate cached edges")
	}
}
