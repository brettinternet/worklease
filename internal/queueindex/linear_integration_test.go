package queueindex_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueindex"
	"github.com/brettinternet/worklease/internal/testkit"
)

const (
	linearOrg    = "0bfebf80-70af-4eca-9e39-2029a01f5b77"
	linearTeam   = "d19193f6-0501-485b-93af-65e829c2039d"
	linearViewer = "72088203-6bc7-4a63-a71b-22048b88da64"
	linearFirst  = "075a1740-eeda-4b85-be0b-39755abf4c8c"
	linearSecond = "9f3b2707-b6d8-456d-9079-32f60cd33474"
)

func TestLinearSyncResumesMovingPagesAndReconcilesRemovedRelation(t *testing.T) {
	t.Parallel()
	testkit.Home(t)
	var mu sync.Mutex
	failed := false
	skipped := false
	moved := false
	relation := true
	failRelation := false
	missingRelation := false
	var listingCalls, incrementalCalls int
	issue := func(id string) map[string]any {
		return map[string]any{"id": id, "identifier": "TEST-1", "title": id, "updatedAt": "2026-09-25T12:00:00Z", "team": map[string]any{"id": linearTeam}, "state": map[string]any{"id": "state", "name": "Todo", "type": "unstarted"}}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string
			Variables map[string]json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var data any
		mu.Lock()
		switch {
		case strings.Contains(req.Query, "viewer { id organization"):
			data = map[string]any{"viewer": map[string]any{"id": linearViewer, "organization": map[string]any{"id": linearOrg}}, "team": map[string]any{"id": linearTeam}}
		case strings.Contains(req.Query, "issues(first:"):
			listingCalls++
			if strings.Contains(req.Query, "updatedAt:{gte:$since}") {
				incrementalCalls++
			}
			if failed && string(req.Variables["after"]) != "null" {
				mu.Unlock()
				w.WriteHeader(503)
				return
			}
			firstPage := string(req.Variables["after"]) == "null"
			nodes := []any{issue(linearFirst)}
			if !firstPage {
				nodes = []any{issue(linearSecond)}
				if skipped {
					nodes = nil // row moved ahead of the cursor between pages
				}
			} else if moved {
				nodes = []any{issue(linearSecond)} // unchanged first issue is outside the delta
			}
			data = map[string]any{"team": map[string]any{"id": linearTeam, "issues": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": firstPage && !moved, "endCursor": "page-two"}}}}
		case strings.Contains(req.Query, "inverseRelations("):
			var id string
			_ = json.Unmarshal(req.Variables["id"], &id)
			if missingRelation && id == linearFirst {
				data = map[string]any{"issue": nil}
				break
			}
			if failRelation && id == linearFirst {
				mu.Unlock()
				w.WriteHeader(503)
				return
			}
			nodes := []any{}
			if relation && id == linearFirst {
				nodes = append(nodes, map[string]any{"type": "blocks", "issue": map[string]any{"id": linearSecond, "team": map[string]any{"id": linearTeam}, "state": map[string]any{"type": "unstarted"}}})
			}
			data = map[string]any{"issue": map[string]any{"id": id, "team": map[string]any{"id": linearTeam}, "inverseRelations": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": false}}}}
		case strings.Contains(req.Query, "relations("):
			var id string
			_ = json.Unmarshal(req.Variables["id"], &id)
			data = map[string]any{"issue": map[string]any{"id": id, "team": map[string]any{"id": linearTeam}, "relations": map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false}}}}
		case strings.Contains(req.Query, "issue(id:"):
			var id string
			_ = json.Unmarshal(req.Variables["id"], &id)
			data = map[string]any{"issue": issue(id)}
		default:
			t.Errorf("unexpected query %s", req.Query)
		}
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	registry := queue.NewRegistry()
	builtin, _ := registry.Get("linear")
	adapter := builtin.(*queue.LinearAdapter)
	adapter.APIBase = server.URL
	ctx := context.Background()
	source, err := adapter.Resolve(ctx, map[string]string{"id": "linear-test", "organization": linearOrg, "team": linearTeam, "account": linearViewer, "credentialHelper": `["/bin/echo","fixture-token"]`})
	if err != nil {
		t.Fatal(err)
	}
	index, err := queueindex.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	store := queueindex.LinearSyncStore{Index: index, Registry: registry}
	loader := queue.NewLoader(registry)
	loader.Query.Budget = 1
	loader.LinearSync = store
	refresh := func() {
		for range loader.Refresh(ctx, []queue.Source{source}) {
		}
	}
	mu.Lock()
	failed = true
	mu.Unlock()
	refresh()
	checkpoint, err := store.LoadLinearSync(ctx, source)
	if err != nil || checkpoint.Cursor == "" || !checkpoint.CommittedWatermark.IsZero() {
		t.Fatalf("interrupted window: %+v %v", checkpoint, err)
	}
	mu.Lock()
	failed = false
	skipped = true
	mu.Unlock()
	refresh()
	checkpoint, err = store.LoadLinearSync(ctx, source)
	if err != nil || checkpoint.Cursor != "" || checkpoint.CommittedWatermark.IsZero() {
		t.Fatalf("completed window: %+v %v", checkpoint, err)
	}
	initial := checkpoint.CommittedWatermark
	if _, ok := loader.Store.Item(queue.Ref{SourceID: source.ID, ItemID: linearSecond}); ok {
		t.Fatal("moving page unexpectedly included skipped issue")
	}
	item, ok := loader.Store.Item(queue.Ref{SourceID: source.ID, ItemID: linearFirst})
	if !ok || !item.DependenciesKnown || len(item.Relationships) != 1 {
		t.Fatalf("initial relations: %+v %v", item, ok)
	}
	// Re-listing a known issue must not erase complete prior edge evidence
	// when its new inverse pagination fails.
	mu.Lock()
	skipped, failRelation = false, true
	mu.Unlock()
	refresh()
	item, ok = loader.Store.Item(queue.Ref{SourceID: source.ID, ItemID: linearFirst})
	if !ok || item.DependenciesKnown || len(item.Relationships) != 1 {
		t.Fatalf("listed issue lost prior edges on partial hydration: %+v %v", item, ok)
	}
	// A relation removal is not an updatedAt signal: the next incremental
	// traversal must refresh the closure even if the issue's timestamp stays put.
	mu.Lock()
	moved = true
	relation = false
	failRelation = true
	mu.Unlock()
	refresh()
	item, ok = loader.Store.Item(queue.Ref{SourceID: source.ID, ItemID: linearFirst})
	if !ok || item.DependenciesKnown || len(item.Relationships) != 1 {
		t.Fatalf("partial relation scan replaced complete edges: %+v %v", item, ok)
	}
	mu.Lock()
	failRelation = false
	mu.Unlock()
	refresh()
	checkpoint, err = store.LoadLinearSync(ctx, source)
	if err != nil || !checkpoint.CommittedWatermark.After(initial) {
		t.Fatalf("incremental watermark: %+v %v", checkpoint, err)
	}
	item, ok = loader.Store.Item(queue.Ref{SourceID: source.ID, ItemID: linearFirst})
	if !ok || !item.DependenciesKnown || len(item.Relationships) != 0 {
		t.Fatalf("removed relation persisted: %+v found=%v coverage=%+v", item, ok, loader.Store.Current().Sources[source.ID])
	}
	if _, ok := loader.Store.Item(queue.Ref{SourceID: source.ID, ItemID: linearSecond}); !ok {
		t.Fatal("overlapping incremental scan missed moved issue")
	}
	if loader.Store.Current().Sources[source.ID].State != queue.CoveragePartial {
		t.Fatal("moving traversal certified source membership")
	}
	mu.Lock()
	calls, windows := listingCalls, incrementalCalls
	mu.Unlock()
	if calls < 4 || windows == 0 {
		t.Fatalf("missing full and incremental pages: calls=%d windows=%d", calls, windows)
	}
	mu.Lock()
	missingRelation = true
	mu.Unlock()
	refresh()
	if _, ok := loader.Store.Item(queue.Ref{SourceID: source.ID, ItemID: linearSecond}); !ok || loader.Store.Current().Sources[source.ID].State == queue.CoverageUnknown {
		t.Fatal("one inaccessible issue purged the entire source")
	}
	mu.Lock()
	missingRelation = false
	mu.Unlock()
	// A new process has no live projection. It must not resume a full-list
	// cursor from disk as an incremental filter after the first bounded page.
	mu.Lock()
	moved, failed = false, true
	mu.Unlock()
	cold := queue.NewLoader(registry)
	cold.Query.Budget = 1
	cold.LinearSync = store
	for range cold.Refresh(ctx, []queue.Source{source}) {
	}
	checkpoint, err = store.LoadLinearSync(ctx, source)
	if err != nil || checkpoint.Cursor == "" || !checkpoint.CommittedWatermark.IsZero() {
		t.Fatalf("cold interrupted scan retained old window: %+v %v", checkpoint, err)
	}
	mu.Lock()
	failed = false
	beforeResume := incrementalCalls
	mu.Unlock()
	for range cold.Refresh(ctx, []queue.Source{source}) {
	}
	mu.Lock()
	resumedIncremental := incrementalCalls
	mu.Unlock()
	if resumedIncremental != beforeResume {
		t.Fatal("resumed full-list cursor with incremental filter")
	}
}

func TestLinearSupersededRefreshDoesNotPersistUnpublishedPage(t *testing.T) {
	t.Parallel()
	testkit.Home(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls int
	var mu sync.Mutex
	node := func(id string) map[string]any {
		return map[string]any{"id": id, "identifier": "TEST-1", "title": id, "updatedAt": "2026-09-25T12:00:00Z", "team": map[string]any{"id": linearTeam}, "state": map[string]any{"id": "state", "name": "Todo", "type": "unstarted"}}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string
			Variables map[string]json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var data any
		switch {
		case strings.Contains(req.Query, "viewer { id organization"):
			data = map[string]any{"viewer": map[string]any{"id": linearViewer, "organization": map[string]any{"id": linearOrg}}, "team": map[string]any{"id": linearTeam}}
		case strings.Contains(req.Query, "issues(first:"):
			mu.Lock()
			calls++
			firstCall := calls == 1
			mu.Unlock()
			if firstCall {
				close(entered)
				<-release
			}
			firstPage := string(req.Variables["after"]) == "null"
			id := linearSecond
			if firstPage {
				id = linearFirst
			}
			data = map[string]any{"team": map[string]any{"id": linearTeam, "issues": map[string]any{"nodes": []any{node(id)}, "pageInfo": map[string]any{"hasNextPage": firstPage, "endCursor": "page-two"}}}}
		case strings.Contains(req.Query, "relations(") || strings.Contains(req.Query, "inverseRelations("):
			var id string
			_ = json.Unmarshal(req.Variables["id"], &id)
			field := "relations"
			if strings.Contains(req.Query, "inverseRelations(") {
				field = "inverseRelations"
			}
			data = map[string]any{"issue": map[string]any{"id": id, "team": map[string]any{"id": linearTeam}, field: map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false}}}}
		case strings.Contains(req.Query, "issue(id:"):
			var id string
			_ = json.Unmarshal(req.Variables["id"], &id)
			data = map[string]any{"issue": node(id)}
		default:
			t.Errorf("unexpected query %s", req.Query)
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	registry := queue.NewRegistry()
	builtin, _ := registry.Get("linear")
	adapter := builtin.(*queue.LinearAdapter)
	adapter.APIBase = server.URL
	ctx := context.Background()
	source, err := adapter.Resolve(ctx, map[string]string{"id": "linear-interleaved", "organization": linearOrg, "team": linearTeam, "account": linearViewer, "credentialHelper": `["/bin/echo","fixture-token"]`})
	if err != nil {
		t.Fatal(err)
	}
	idx, err := queueindex.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	store := queueindex.LinearSyncStore{Index: idx, Registry: registry}
	if err := store.CommitLinearSyncPage(ctx, source, nil, "", time.Now().Add(-time.Minute), true); err != nil {
		t.Fatal(err)
	}
	loader := queue.NewLoader(registry)
	loader.Query.Budget = 1
	loader.LinearSync = store
	seed := queue.Ref{SourceID: source.ID, ItemID: linearSecond}
	loader.Store.SeedSnapshot(queue.Snapshot{Items: map[string]queue.Item{seed.Key(): {Summary: queue.Summary{Ref: seed}}}})
	firstDone := make(chan struct{})
	go func() {
		for range loader.Refresh(ctx, []queue.Source{source}) {
		}
		close(firstDone)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first page did not start")
	}
	second := loader.Refresh(ctx, []queue.Source{source}) // increments generation before old List returns
	close(release)
	for range second {
	}
	<-firstDone
	if _, ok := loader.Store.Item(queue.Ref{SourceID: source.ID, ItemID: linearFirst}); !ok {
		t.Fatal("superseded page was checkpointed without publication")
	}
	mu.Lock()
	count := calls
	mu.Unlock()
	if count < 3 {
		t.Fatalf("second refresh skipped first page: %d list calls", count)
	}
}
