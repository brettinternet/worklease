package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func itemOutcome(ref Ref) ItemOutcome {
	item := Item{Summary: Summary{Ref: ref, Fresh: true}, TerminalKnown: true, DependenciesKnown: true, Closure: CoverageComplete}
	return ItemOutcome{Ref: ref, Item: &item, Kind: "found"}
}
func drainRefresh(ch <-chan Snapshot) {
	for range ch {
	}
}

func TestExplicitInaccessibleOutcomePurgesProjection(t *testing.T) {
	fake := newFake()
	ref := Ref{SourceID: "s", ItemID: "gone"}
	fake.pages["s"] = []SummaryPage{{Items: []Summary{{Ref: ref, Title: "secret cached title", Fresh: true}}, Coverage: Coverage{State: CoverageComplete, TotalAccuracy: TotalExact}, Observation: Observation{Principal: "alice", ConfigurationGeneration: "g"}}}
	fake.outcomes[ref.Key()] = []ItemOutcome{{Ref: ref, Kind: "inaccessible"}}
	registry := NewRegistry()
	if err := registry.Register("fake", fake); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(registry)
	drainRefresh(loader.Refresh(context.Background(), []Source{{ID: "s", Adapter: "fake"}}))
	snapshot := loader.Store.Current()
	if _, ok := snapshot.Items[ref.Key()]; ok {
		t.Fatal("inaccessible item remained in source projection")
	}
	if snapshot.Deleted[ref.Key()] != ref {
		t.Fatalf("explicit deletion evidence missing: %+v", snapshot.Deleted)
	}
}

func TestProviderReadyPointerIsClonedAcrossSnapshots(t *testing.T) {
	store := NewStore()
	ready := true
	item := Item{Summary: Summary{Ref: Ref{"s", "i"}, ProviderReady: &ready}}
	store.publish(func(s *Snapshot) { s.Items[item.Ref.Key()] = item })
	snap := store.Current()
	copy, _ := snap.Item(item.Ref)
	*copy.ProviderReady = false
	if *store.Current().Items[item.Ref.Key()].ProviderReady != true {
		t.Fatal("snapshot provider-readiness pointer was shared")
	}
}

func TestMaintenanceRequiresVerifiedOwner(t *testing.T) {
	item := Item{Claim: ClaimObservation{Known: true, Active: true}, Readiness: Readiness{Status: Blocked, Reasons: []string{"hard-condition-unsatisfied"}}}
	if got := EvaluateAction(item, ActionReportBlocked); got.Eligible {
		t.Fatal("unverified active claim allowed maintenance")
	}
	item.Claim.OwnerVerified = true
	if got := EvaluateAction(item, ActionReportBlocked); !got.Eligible {
		t.Fatalf("verified owner denied maintenance: %+v", got)
	}
}

// An explicit empty relationship list means no observed edges, so legacy
// dependencies conflict with it and must not be silently used or dropped.
func TestEmptyTypedRelationshipsConflictWithLegacyDependencies(t *testing.T) {
	a, b := Ref{"s", "a"}, Ref{"s", "b"}
	items := map[string]Item{a.Key(): {Summary: Summary{Ref: a, Fresh: true}, Dependencies: []Ref{b}, Relationships: []Relationship{}, DependenciesKnown: true, Closure: CoverageComplete}, b.Key(): {Summary: Summary{Ref: b, Fresh: true, Terminal: true}, TerminalKnown: true, DependenciesKnown: true, Closure: CoverageComplete}}
	got := Recompute(items, CoverageComplete)[a.Key()].Readiness
	if got.Status != ReadinessUnknown || !containsString(got.Reasons, "ambiguous-dependency-projection") {
		t.Fatalf("conflicting projection readiness: %+v", got)
	}
}
func TestPrerequisitePermissionChangeRecomputesDependents(t *testing.T) {
	a, b := Ref{"s", "a"}, Ref{"s", "b"}
	items := map[string]Item{
		a.Key(): {Summary: Summary{Ref: a, Fresh: true}, Dependencies: []Ref{b}, DependenciesKnown: true, Closure: CoverageComplete},
		b.Key(): {Summary: Summary{Ref: b, Fresh: true, Terminal: true}, TerminalKnown: true, DependenciesKnown: true, Closure: CoverageComplete, ReadPermission: Allowed},
	}
	if got := Recompute(items, CoverageComplete)[a.Key()].Readiness.Status; got != Ready {
		t.Fatalf("accessible prerequisite readiness=%s", got)
	}
	prerequisite := items[b.Key()]
	prerequisite.ReadPermission = Denied
	items[b.Key()] = prerequisite
	if got := Recompute(items, CoverageComplete)[a.Key()].Readiness.Status; got != ReadinessUnknown {
		t.Fatalf("inaccessible prerequisite incorrectly proved %s", got)
	}
	prerequisite.ReadPermission = Allowed
	items[b.Key()] = prerequisite
	if got := Recompute(items, CoverageComplete)[a.Key()].Readiness.Status; got != Ready {
		t.Fatalf("restored permission readiness=%s", got)
	}
}

func TestTypedTerminalConditionUsesCurrentPrerequisiteAndStaleEdgeDoesNotBlock(t *testing.T) {
	a, b := Ref{"s", "a"}, Ref{"s", "b"}
	edge := Relationship{Type: HardPrerequisite, From: a, To: b, Condition: "terminal", Interpretation: "satisfied", Fresh: true, Support: Supported}
	items := map[string]Item{a.Key(): {Summary: Summary{Ref: a, Fresh: true}, DependenciesKnown: true, Closure: CoverageComplete, Relationships: []Relationship{edge}}, b.Key(): {Summary: Summary{Ref: b, Fresh: true, Terminal: true}, TerminalKnown: true, DependenciesKnown: true, Closure: CoverageComplete}}
	if got := Recompute(items, CoverageComplete)[a.Key()].Readiness.Status; got != Ready {
		t.Fatalf("terminal prerequisite not ready: %s", got)
	}
	changed := cloneItems(items)
	p := changed[b.Key()]
	p.Terminal = false
	changed[b.Key()] = p
	if got := Recompute(changed, CoverageComplete)[a.Key()].Readiness.Status; got != Blocked {
		t.Fatalf("reopened prerequisite readiness=%s", got)
	}
	stale := edge
	stale.Fresh = false
	stale.Interpretation = "unsatisfied"
	changed[a.Key()] = Item{Summary: Summary{Ref: a, Fresh: true}, DependenciesKnown: true, Closure: CoverageComplete, Relationships: []Relationship{stale}}
	if got := Recompute(changed, CoverageComplete)[a.Key()].Readiness.Status; got != ReadinessUnknown {
		t.Fatalf("stale unsatisfied relationship proved %s", got)
	}
}
func TestFailedItemReadAndTerminalItemCannotStart(t *testing.T) {
	ref := Ref{"s", "i"}
	missing := Item{Summary: Summary{Ref: ref, Fresh: false}, ReadOutcome: "missing", DependenciesKnown: true, Closure: CoverageComplete}
	missing = Recompute(map[string]Item{ref.Key(): missing}, CoverageComplete)[ref.Key()]
	if missing.Readiness.Status != ReadinessUnknown {
		t.Fatalf("missing item readiness=%s", missing.Readiness.Status)
	}
	terminal := Item{Summary: Summary{Ref: ref, Fresh: true, Terminal: true}, Readiness: Readiness{Status: Ready}}
	if got := EvaluateAction(terminal, ActionStart); got.Eligible || got.Outcome != "complete" {
		t.Fatalf("terminal item start=%+v", got)
	}
}
func TestViewOnlyIncludesConfiguredSourcesAndDeduplicatesCanonicalIdentity(t *testing.T) {
	items := map[string]Item{
		"a": {Summary: Summary{Ref: Ref{"first", "1"}, CanonicalID: "github.com/acme/api#1"}},
		"b": {Summary: Summary{Ref: Ref{"second", "1"}, CanonicalID: "github.com/acme/api#1"}},
		"c": {Summary: Summary{Ref: Ref{"excluded", "2"}}},
	}
	got := EvaluateView(items, View{SourceOrder: []string{"second", "first"}})
	if len(got) != 1 || got[0].Ref.SourceID != "second" {
		t.Fatalf("view leaked an unconfigured source or duplicated an item: %+v", got)
	}
}

func TestViewEqualOrderUsesReferenceTieBreak(t *testing.T) {
	items := map[string]Item{"z": {Summary: Summary{Ref: Ref{"s", "z"}, Order: "same"}}, "a": {Summary: Summary{Ref: Ref{"s", "a"}, Order: "same"}}}
	for i := 0; i < 10; i++ {
		got := EvaluateView(items, View{SourceOrder: []string{"s"}})
		if got[0].Ref.ItemID != "a" || got[1].Ref.ItemID != "z" {
			t.Fatalf("unstable equal-order result: %+v", got)
		}
	}
}
func TestFailedRefreshStalesPartitionAndCursorIsPartial(t *testing.T) {
	fake := newFake()
	ref := Ref{"s", "i"}
	fake.pages["s"] = []SummaryPage{{Items: []Summary{{Ref: ref, Fresh: true}}, Coverage: Coverage{State: CoverageComplete, TotalAccuracy: TotalExact}}, {Coverage: Coverage{State: CoverageComplete, TotalAccuracy: TotalExact}}}
	fake.outcomes[ref.Key()] = []ItemOutcome{itemOutcome(ref)}
	registry := NewRegistry()
	_ = registry.Register("fake", fake)
	loader := NewLoader(registry)
	updates := loader.Refresh(context.Background(), []Source{{ID: "s", Adapter: "fake"}})
	partialCursorSeen := false
	for snap := range updates {
		if _, ok := snap.Items[ref.Key()]; ok && snap.Sources["s"].State == CoveragePartial && snap.Sources["s"].Cursor != "" {
			partialCursorSeen = true
		}
	}
	if !partialCursorSeen {
		t.Fatal("page cursor did not publish partial coverage")
	}
	snap := loader.Store.Current()
	if snap.Sources["s"].State != CoverageComplete {
		t.Fatalf("completed scan did not restore coverage: %+v", snap.Sources["s"])
	}
	fake.errors["s"] = errors.New("offline")
	drainRefresh(loader.Refresh(context.Background(), []Source{{ID: "s", Adapter: "fake"}}))
	snap = loader.Store.Current()
	if snap.Items[ref.Key()].Fresh || snap.Items[ref.Key()].Readiness.Status != ReadinessUnknown {
		t.Fatalf("failed refresh retained fresh item: %+v", snap.Items[ref.Key()])
	}
}
func TestPrincipalGenerationResetAndCompleteScanRetirement(t *testing.T) {
	fake := newFake()
	oldRef, newRef := Ref{"s", "old"}, Ref{"s", "new"}
	fake.pages["s"] = []SummaryPage{{Items: []Summary{{Ref: oldRef, Fresh: true}}, Observation: Observation{Principal: "one", ConfigurationGeneration: "g1"}, Coverage: Coverage{State: CoverageComplete, TotalAccuracy: TotalExact}}}
	fake.outcomes[oldRef.Key()] = []ItemOutcome{itemOutcome(oldRef)}
	registry := NewRegistry()
	_ = registry.Register("fake", fake)
	loader := NewLoader(registry)
	source := Source{ID: "s", Adapter: "fake"}
	drainRefresh(loader.Refresh(context.Background(), []Source{source}))
	fake.pages["s"] = []SummaryPage{{Items: []Summary{{Ref: newRef, Fresh: true}}, Observation: Observation{Principal: "two", ConfigurationGeneration: "g2"}, Coverage: Coverage{State: CoverageComplete, TotalAccuracy: TotalExact}}}
	fake.outcomes[newRef.Key()] = []ItemOutcome{itemOutcome(newRef)}
	drainRefresh(loader.Refresh(context.Background(), []Source{source}))
	snap := loader.Store.Current()
	if _, ok := snap.Items[oldRef.Key()]; ok {
		t.Fatal("principal/config change retained old partition")
	}
	if _, ok := snap.Items[newRef.Key()]; !ok {
		t.Fatal("new principal item missing")
	}
	fake.pages["s"] = []SummaryPage{{Coverage: Coverage{State: CoverageComplete, TotalAccuracy: TotalExact}, Observation: Observation{Principal: "two", ConfigurationGeneration: "g2"}}}
	drainRefresh(loader.Refresh(context.Background(), []Source{source}))
	if len(loader.Store.Current().Items) != 0 {
		t.Fatal("complete empty scan did not retire absent source items")
	}
}

type supersedeAdapter struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
}

func (a *supersedeAdapter) Resolve(context.Context, map[string]string) (Source, error) {
	return Source{}, nil
}
func (a *supersedeAdapter) Capabilities(context.Context, Source, string, *Ref) (CapabilitySet, error) {
	return nil, nil
}
func (a *supersedeAdapter) List(ctx context.Context, s Source, q Query, cursor string) (SummaryPage, error) {
	a.mu.Lock()
	a.calls++
	call := a.calls
	a.mu.Unlock()
	if call == 1 {
		close(a.started)
		<-a.release
		return SummaryPage{Items: []Summary{{Ref: Ref{s.ID, "old"}, Fresh: true}}, Coverage: Coverage{State: CoverageComplete, TotalAccuracy: TotalExact}}, nil
	}
	return SummaryPage{Items: []Summary{{Ref: Ref{s.ID, "new"}, Fresh: true}}, Coverage: Coverage{State: CoverageComplete, TotalAccuracy: TotalExact}}, nil
}
func (a *supersedeAdapter) ReadItems(ctx context.Context, s Source, refs []Ref, _ []string, _ int) []ItemOutcome {
	out := make([]ItemOutcome, 0, len(refs))
	for _, r := range refs {
		out = append(out, itemOutcome(r))
	}
	return out
}
func (a *supersedeAdapter) ReadDependencies(context.Context, Source, Ref, string, int) (DependencyPage, error) {
	return DependencyPage{Completeness: CoverageComplete}, nil
}
func TestSupersededRefreshCannotPublish(t *testing.T) {
	adapter := &supersedeAdapter{started: make(chan struct{}), release: make(chan struct{})}
	registry := NewRegistry()
	_ = registry.Register("supersede", adapter)
	loader := NewLoader(registry)
	source := Source{ID: "s", Adapter: "supersede"}
	first := loader.Refresh(context.Background(), []Source{source})
	<-adapter.started
	drainRefresh(loader.Refresh(context.Background(), []Source{source}))
	close(adapter.release)
	drainRefresh(first)
	snap := loader.Store.Current()
	if _, ok := snap.Items[(Ref{"s", "old"}).Key()]; ok {
		t.Fatal("superseded refresh published stale item")
	}
	if _, ok := snap.Items[(Ref{"s", "new"}).Key()]; !ok {
		t.Fatal("current refresh item missing")
	}
}

type hydrationAdapter struct {
	started chan string
	release chan struct{}
}

func (a *hydrationAdapter) Resolve(context.Context, map[string]string) (Source, error) {
	return Source{}, nil
}
func (a *hydrationAdapter) Capabilities(context.Context, Source, string, *Ref) (CapabilitySet, error) {
	return nil, nil
}
func (a *hydrationAdapter) List(context.Context, Source, Query, string) (SummaryPage, error) {
	return SummaryPage{Items: []Summary{{Ref: Ref{"s", "slow"}, Fresh: true}, {Ref: Ref{"s", "fast"}, Fresh: true}}, Coverage: Coverage{State: CoverageComplete, TotalAccuracy: TotalExact}}, nil
}
func (a *hydrationAdapter) ReadItems(ctx context.Context, s Source, refs []Ref, _ []string, _ int) []ItemOutcome {
	for _, r := range refs {
		if r.ItemID == "slow" {
			a.started <- "slow"
			<-a.release
		}
	}
	out := make([]ItemOutcome, 0, len(refs))
	for _, r := range refs {
		out = append(out, itemOutcome(r))
	}
	return out
}
func (a *hydrationAdapter) ReadDependencies(context.Context, Source, Ref, string, int) (DependencyPage, error) {
	return DependencyPage{Completeness: CoverageComplete}, nil
}
func TestSummaryPublishesBeforeBoundedHydrationAndFastItemDoesNotWait(t *testing.T) {
	adapter := &hydrationAdapter{started: make(chan string, 2), release: make(chan struct{})}
	registry := NewRegistry()
	_ = registry.Register("hydration", adapter)
	loader := NewLoader(registry)
	loader.HydrationLimit = 2
	updates := loader.Refresh(context.Background(), []Source{{ID: "s", Adapter: "hydration"}})
	deadline := time.After(time.Second)
	summarySeen, fastSeen := false, false
	for !summarySeen || !fastSeen {
		select {
		case snap, ok := <-updates:
			if !ok {
				t.Fatal("refresh closed before hydration")
			}
			if _, ok := snap.Items[(Ref{"s", "slow"}).Key()]; ok {
				summarySeen = true
			}
			fast, ok := snap.Items[(Ref{"s", "fast"}).Key()]
			if ok && fast.ReadOutcome == "found" {
				fastSeen = true
			}
		case <-deadline:
			t.Fatal("summary or fast item starved by slow hydration")
		}
	}
	close(adapter.release)
	for range updates {
	}
}
func TestRecomputeMemoizedLargeClosure(t *testing.T) {
	const n = 1200
	items := make(map[string]Item, n)
	for i := 0; i < n; i++ {
		ref := Ref{"s", itoa(i)}
		item := Item{Summary: Summary{Ref: ref, Fresh: true, Terminal: true}, TerminalKnown: true, DependenciesKnown: true, Closure: CoverageComplete}
		if i+1 < n {
			next := Ref{"s", itoa(i + 1)}
			item.Relationships = []Relationship{{Type: HardPrerequisite, From: ref, To: next, Condition: "terminal", Fresh: true, Support: Supported, Interpretation: "satisfied"}}
		}
		items[ref.Key()] = item
	}
	if got := Recompute(items, CoverageComplete)[(Ref{"s", "0"}).Key()].Readiness.Status; got != Ready {
		t.Fatalf("large closure readiness=%s", got)
	}
}

func TestMatchingDependencyProjectionIsNotAmbiguous(t *testing.T) {
	a, b := Ref{"s", "a"}, Ref{"s", "b"}
	edge := Relationship{Type: HardPrerequisite, From: a, To: b, Condition: "terminal", Fresh: true, Support: Supported}
	items := map[string]Item{a.Key(): {Summary: Summary{Ref: a, Fresh: true}, Dependencies: []Ref{b}, Relationships: []Relationship{edge}, DependenciesKnown: true, Closure: CoverageComplete}, b.Key(): {Summary: Summary{Ref: b, Fresh: true, Terminal: true}, TerminalKnown: true, DependenciesKnown: true, Closure: CoverageComplete}}
	if got := Recompute(items, CoverageComplete)[a.Key()].Readiness; got.Status != Ready {
		t.Fatalf("matching projection readiness: %+v", got)
	}
}
