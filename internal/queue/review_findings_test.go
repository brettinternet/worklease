package queue

import (
	"context"
	"testing"
	"time"
)

type reviewAdapter struct {
	pages []SummaryPage
	deps  []DependencyPage
}

func (a *reviewAdapter) Resolve(context.Context, map[string]string) (Source, error) {
	return Source{}, nil
}
func (a *reviewAdapter) Capabilities(context.Context, Source, string, *Ref) (CapabilitySet, error) {
	return nil, nil
}
func (a *reviewAdapter) List(_ context.Context, _ Source, _ Query, cursor string) (SummaryPage, error) {
	index := 0
	if cursor != "" {
		for i, page := range a.pages {
			if i > 0 && a.pages[i-1].NextCursor == cursor {
				index = i
				break
			}
			if page.NextCursor == cursor && i+1 < len(a.pages) {
				index = i + 1
				break
			}
		}
	}
	return a.pages[index], nil
}
func (a *reviewAdapter) ReadItems(_ context.Context, _ Source, refs []Ref, _ []string, _ int) []ItemOutcome {
	out := make([]ItemOutcome, 0, len(refs))
	for _, ref := range refs {
		out = append(out, itemOutcome(ref))
	}
	return out
}
func (a *reviewAdapter) ReadDependencies(_ context.Context, _ Source, _ Ref, cursor string, _ int) (DependencyPage, error) {
	index := 0
	if cursor != "" {
		index = 1
	}
	if index >= len(a.deps) {
		return DependencyPage{Completeness: CoverageComplete}, nil
	}
	return a.deps[index], nil
}

type stalledReviewAdapter struct {
	*reviewAdapter
	secondPageStarted chan struct{}
}

func (a *stalledReviewAdapter) List(ctx context.Context, source Source, query Query, cursor string) (SummaryPage, error) {
	if cursor != "" {
		close(a.secondPageStarted)
		<-ctx.Done()
		return SummaryPage{}, ctx.Err()
	}
	return a.reviewAdapter.List(ctx, source, query, cursor)
}

func runReviewAdapter(t *testing.T, adapter Adapter, hydrationLimit int) Snapshot {
	t.Helper()
	registry := NewRegistry()
	if err := registry.Register("review", adapter); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(registry)
	loader.HydrationLimit = hydrationLimit
	for range loader.Refresh(context.Background(), []Source{{ID: "s", Adapter: "review"}}) {
	}
	return loader.Store.Current()
}

type failedListAdapter struct {
	reviewAdapter
	err error
}

func (a *failedListAdapter) List(context.Context, Source, Query, string) (SummaryPage, error) {
	return SummaryPage{}, a.err
}

func TestSourceListFailurePreservesGitHubClassificationAndRetry(t *testing.T) {
	retryAt := time.Now().Add(time.Minute).Truncate(time.Second)
	for _, test := range []struct {
		name    string
		err     error
		code    string
		retryAt time.Time
	}{
		{name: "429", err: GitHubRateDiagnostic{GitHubDiagnostic{Code: "rate-limited"}, retryAt}, code: "rate-limited", retryAt: retryAt},
		// Access loss is withheld as unverified (TASK-129.3), still distinct from 429 and offline.
		{name: "403", err: GitHubDiagnostic{Code: "permission-denied"}, code: "github-access-unverified"},
		{name: "connection", err: GitHubDiagnostic{Code: "offline"}, code: "offline"},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := NewRegistry()
			if err := registry.Register("failed", &failedListAdapter{err: test.err}); err != nil {
				t.Fatal(err)
			}
			loader := NewLoader(registry)
			for range loader.Refresh(context.Background(), []Source{{ID: "s", Adapter: "failed"}}) {
			}
			coverage := loader.Store.Current().Sources["s"]
			if coverage.Reason != test.code || !coverage.RetryAt.Equal(test.retryAt) {
				t.Fatalf("coverage lost diagnostic: %+v", coverage)
			}
		})
	}
}

func TestReadinessCyclePropagatesReachableBlocker(t *testing.T) {
	a, b, d := Ref{"s", "a"}, Ref{"s", "b"}, Ref{"s", "d"}
	items := map[string]Item{
		a.Key(): {Summary: Summary{Ref: a, Fresh: true}, DependenciesKnown: true, Relationships: []Relationship{{Type: HardPrerequisite, From: a, To: b, Condition: "terminal", Fresh: true, Support: Supported}, {Type: HardPrerequisite, From: a, To: d, Condition: "terminal", Fresh: true, Support: Supported}}},
		b.Key(): {Summary: Summary{Ref: b, Fresh: true}, DependenciesKnown: true, Relationships: []Relationship{{Type: HardPrerequisite, From: b, To: a, Condition: "terminal", Fresh: true, Support: Supported}}},
		d.Key(): {Summary: Summary{Ref: d, Fresh: true}, TerminalKnown: true, DependenciesKnown: true, Closure: CoverageComplete},
	}
	for i := 0; i < 100; i++ {
		got := Recompute(items, CoverageComplete)
		if got[a.Key()].Readiness.Status != Blocked || got[b.Key()].Readiness.Status != Blocked {
			t.Fatalf("iteration %d statuses A=%s B=%s", i, got[a.Key()].Readiness.Status, got[b.Key()].Readiness.Status)
		}
	}
}

func TestSeedSnapshotRecomputesStaleReadinessAndNotifies(t *testing.T) {
	store := NewStore()
	updates, cancel := store.Subscribe(2)
	defer cancel()
	<-updates
	ref := Ref{"s", "a"}
	store.SeedSnapshot(Snapshot{Items: map[string]Item{ref.Key(): {Summary: Summary{Ref: ref, Fresh: false}, DependenciesKnown: true, Closure: CoverageComplete, Readiness: Readiness{Status: Ready}}}, Sources: map[string]Coverage{"s": {State: CoverageComplete}}})
	got := store.Current().Items[ref.Key()]
	if got.Readiness.Status != ReadinessUnknown || got.Readiness.Freshness != Stale {
		t.Fatalf("seed readiness=%+v", got.Readiness)
	}
	select {
	case update := <-updates:
		if update.Items[ref.Key()].Readiness.Status != ReadinessUnknown {
			t.Fatal("subscriber received stale readiness")
		}
	default:
		t.Fatal("seed did not notify subscribers")
	}
}

func TestDependencyUnknownPageStaysIncomplete(t *testing.T) {
	ref := Ref{"s", "a"}
	adapter := &reviewAdapter{pages: []SummaryPage{{Items: []Summary{{Ref: ref, Fresh: true}}, Coverage: Coverage{State: CoverageComplete, TotalAccuracy: TotalExact}}}, deps: []DependencyPage{{Completeness: CoverageUnknown, NextCursor: "next"}, {Completeness: CoverageComplete}}}
	got := runReviewAdapter(t, adapter, 1).Items[ref.Key()]
	if got.DependenciesKnown || got.Readiness.Status != ReadinessUnknown {
		t.Fatalf("unknown page published complete closure: %+v", got)
	}
}

func TestDependencyRepeatedCursorBecomesUnknown(t *testing.T) {
	ref := Ref{"s", "a"}
	adapter := &reviewAdapter{pages: []SummaryPage{{Items: []Summary{{Ref: ref, Fresh: true}}, Coverage: Coverage{State: CoverageComplete, TotalAccuracy: TotalExact}}}, deps: []DependencyPage{{Completeness: CoverageComplete, NextCursor: "again"}, {Completeness: CoverageComplete, NextCursor: "again"}}}
	got := runReviewAdapter(t, adapter, 1).Items[ref.Key()]
	if got.DependenciesKnown {
		t.Fatalf("repeated dependency cursor marked complete: %+v", got)
	}
}

func TestDependencyPrincipalChangePurgesSourceProjection(t *testing.T) {
	ref := Ref{"s", "a"}
	adapter := &reviewAdapter{pages: []SummaryPage{{Items: []Summary{{Ref: ref, Fresh: true}}, Observation: Observation{Principal: "alice", ConfigurationGeneration: "g1"}, Coverage: Coverage{State: CoverageComplete, TotalAccuracy: TotalExact}}}, deps: []DependencyPage{{Completeness: CoverageComplete, Observation: Observation{Principal: "bob", ConfigurationGeneration: "g1"}}}}
	got := runReviewAdapter(t, adapter, 1)
	if _, ok := got.Items[ref.Key()]; ok {
		t.Fatal("item survived principal change during hydration")
	}
	if got.Sources["s"].State != CoverageUnknown {
		t.Fatalf("source coverage=%+v", got.Sources["s"])
	}
}

func TestIncompletePaginatedRefreshStalesUnvisitedRows(t *testing.T) {
	a, b := Ref{"s", "a"}, Ref{"s", "b"}
	fake := newFake()
	fake.pages["s"] = []SummaryPage{{Items: []Summary{{Ref: a, Fresh: true}, {Ref: b, Fresh: true}}, Coverage: Coverage{State: CoverageComplete, TotalAccuracy: TotalExact}}}
	fake.outcomes[a.Key()] = []ItemOutcome{itemOutcome(a)}
	fake.outcomes[b.Key()] = []ItemOutcome{itemOutcome(b)}
	registry := NewRegistry()
	_ = registry.Register("fake", fake)
	loader := NewLoader(registry)
	source := Source{ID: "s", Adapter: "fake"}
	for range loader.Refresh(context.Background(), []Source{source}) {
	}

	stalled := &stalledReviewAdapter{reviewAdapter: &reviewAdapter{pages: []SummaryPage{{Items: []Summary{{Ref: a, Fresh: true}}, NextCursor: "next", Coverage: Coverage{State: CoveragePartial}}}}, secondPageStarted: make(chan struct{})}
	_ = registry.Register("review", stalled)
	ctx, cancel := context.WithCancel(context.Background())
	updates := loader.Refresh(ctx, []Source{{ID: "s", Adapter: "review"}})
	<-stalled.secondPageStarted
	cancel()
	for range updates {
	}
	retained, ok := loader.Store.Current().Items[b.Key()]
	if !ok || retained.Fresh || retained.Readiness.Freshness != Stale {
		t.Fatalf("unvisited retained row=%+v present=%v", retained, ok)
	}
}
