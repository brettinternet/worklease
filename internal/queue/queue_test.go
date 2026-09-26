package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

func (s *Store) Subscribe(buffer int) (<-chan Snapshot, func()) {
	if buffer < 1 {
		buffer = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subNext++
	id := s.subNext
	ch := make(chan Snapshot, buffer)
	ch <- s.current.Clone()
	s.subs[id] = ch
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			if c, ok := s.subs[id]; ok {
				delete(s.subs, id)
				close(c)
			}
		})
	}
}

func (s *Store) publish(update func(*Snapshot)) Snapshot {
	result, _ := s.publishComputed(update, false, nil)
	return result
}

type fixture struct {
	SchemaVersion int `json:"schemaVersion"`
	Cases         []struct {
		Name  string `json:"name"`
		Graph struct {
			Coverage string `json:"coverage"`
			Items    []struct {
				Ref           []string   `json:"ref"`
				Terminal      *bool      `json:"terminal"`
				OwnerVerified bool       `json:"ownerVerified"`
				State         string     `json:"state"`
				Dependencies  [][]string `json:"dependencies"`
			} `json:"items"`
			Edges []struct {
				Type           string   `json:"type"`
				From           []string `json:"from"`
				To             []string `json:"to"`
				Condition      string   `json:"condition"`
				Outcome        string   `json:"outcome"`
				Interpretation string   `json:"interpretation"`
				Fresh          bool     `json:"fresh"`
				Support        string   `json:"support"`
				Provenance     string   `json:"provenance"`
			} `json:"edges"`
		} `json:"graph"`
		Candidate []string `json:"candidate"`
		Action    string   `json:"action"`
		Expected  struct {
			Readiness string   `json:"readiness"`
			Eligible  bool     `json:"eligible"`
			Reasons   []string `json:"reasons"`
			Outcome   string   `json:"outcome"`
			Requires  []string `json:"requires"`
		} `json:"expected"`
	} `json:"cases"`
}

func TestDependencyEligibilityFixture(t *testing.T) {
	data, err := os.ReadFile("../../skills/worklease-workflow/examples/dependency-eligibility-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	for _, tc := range f.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			items := map[string]Item{}
			for _, entry := range tc.Graph.Items {
				ref := Ref{SourceID: entry.Ref[0], ItemID: entry.Ref[1]}
				item := Item{Summary: Summary{Ref: ref, Fresh: true}, TerminalKnown: entry.Terminal != nil, DependenciesKnown: true, Closure: CoverageState(tc.Graph.Coverage)}
				if entry.Terminal != nil {
					item.Terminal = *entry.Terminal
				}
				item.State = StateOpen
				if entry.State != "" {
					item.State = StateCategory(entry.State)
				}
				for _, d := range entry.Dependencies {
					item.Dependencies = append(item.Dependencies, Ref{SourceID: d[0], ItemID: d[1]})
				}
				if entry.OwnerVerified {
					item.Claim = ClaimObservation{Known: true, Active: true, OwnerVerified: true}
				}
				items[ref.Key()] = item
			}
			for _, e := range tc.Graph.Edges {
				edge := Relationship{Type: RelationshipType(e.Type), From: Ref{SourceID: e.From[0], ItemID: e.From[1]}, To: Ref{SourceID: e.To[0], ItemID: e.To[1]}, Condition: e.Condition, RawOutcome: e.Outcome, Interpretation: e.Interpretation, Fresh: e.Fresh, Support: Support(e.Support), Provenance: e.Provenance, Direction: DependentToPrerequisite}
				item := items[edge.From.Key()]
				item.Relationships = append(item.Relationships, edge)
				items[edge.From.Key()] = item
			}
			coverage := CoverageState(tc.Graph.Coverage)
			items = Recompute(items, coverage)
			candidate := Ref{SourceID: tc.Candidate[0], ItemID: tc.Candidate[1]}
			item, ok := items[candidate.Key()]
			if !ok {
				t.Fatal("missing candidate")
			}
			if string(item.Readiness.Status) != tc.Expected.Readiness {
				t.Errorf("readiness = %s, want %s (%v)", item.Readiness.Status, tc.Expected.Readiness, item.Readiness.Reasons)
			}
			elig := EvaluateAction(item, Action(tc.Action))
			if elig.Eligible != tc.Expected.Eligible {
				t.Errorf("eligible = %v, want %v", elig.Eligible, tc.Expected.Eligible)
			}
			if tc.Expected.Outcome != "" && elig.Outcome != tc.Expected.Outcome {
				t.Errorf("outcome = %q, want %q", elig.Outcome, tc.Expected.Outcome)
			}
			if !reflect.DeepEqual(elig.Requires, tc.Expected.Requires) {
				t.Errorf("requires = %v, want %v", elig.Requires, tc.Expected.Requires)
			}
			for _, r := range tc.Expected.Reasons {
				if !containsString(item.Readiness.Reasons, r) && !containsString(elig.Reasons, r) {
					t.Errorf("reason %q missing: readiness=%v eligibility=%v", r, item.Readiness.Reasons, elig.Reasons)
				}
			}
		})
	}
}
func containsString(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestRecomputeUsesCurrentTransitiveGraph(t *testing.T) {
	a, b, c := Ref{"s", "a"}, Ref{"s", "b"}, Ref{"s", "c"}
	items := map[string]Item{a.Key(): {Summary: Summary{Ref: a, Fresh: true}, TerminalKnown: true, DependenciesKnown: true, Relationships: []Relationship{{Type: HardPrerequisite, From: a, To: b, Condition: "terminal", Interpretation: "satisfied", Fresh: true, Support: Supported}}}, b.Key(): {Summary: Summary{Ref: b, Fresh: true, Terminal: true}, TerminalKnown: true, DependenciesKnown: true, Relationships: []Relationship{{Type: HardPrerequisite, From: b, To: c, Condition: "terminal", Fresh: true, Support: Supported}}}, c.Key(): {Summary: Summary{Ref: c, Fresh: true, Terminal: true}, TerminalKnown: true, DependenciesKnown: true}}
	if got := Recompute(items, CoverageComplete)[a.Key()].Readiness.Status; got != Ready {
		t.Fatalf("initial readiness=%s", got)
	}
	changed := cloneItems(items)
	prereq := changed[c.Key()]
	prereq.Terminal = false
	changed[c.Key()] = prereq
	if got := Recompute(changed, CoverageComplete)[a.Key()].Readiness.Status; got != Blocked {
		t.Fatalf("after prerequisite reopen readiness=%s", got)
	}
}
func TestRecomputeKeepsLastKnownReadinessOnlyWhileRevalidating(t *testing.T) {
	t.Parallel()
	a, b := Ref{"s", "a"}, Ref{"s", "b"}
	found := func(ref Ref, edges ...Relationship) Item {
		return Item{Summary: Summary{Ref: ref, Fresh: true, Terminal: ref == b}, ReadOutcome: "found", ReadPermission: Allowed, TerminalKnown: true, DependenciesKnown: true, Closure: CoverageComplete, Relationships: edges}
	}
	items := Recompute(map[string]Item{
		a.Key(): found(a, Relationship{Type: HardPrerequisite, From: a, To: b, Condition: "terminal", Fresh: true, Support: Supported}),
		b.Key(): found(b),
	}, CoverageComplete)
	if got := items[a.Key()].Readiness; got.Status != Ready || got.LastKnown != "" {
		t.Fatalf("initial readiness=%+v", got)
	}

	// A relisted prerequisite leaves its dependent unknown, but the dependent
	// keeps its last known readiness until the prerequisite is reread.
	prereq := items[b.Key()]
	prereq.ReadOutcome = "summary-only"
	items[b.Key()] = prereq
	items = Recompute(items, CoverageComplete)
	if got := items[a.Key()].Readiness; got.Status != ReadinessUnknown || got.LastKnown != Ready {
		t.Fatalf("revalidating readiness=%+v", got)
	}
	items = Recompute(items, CoverageComplete)
	if got := items[a.Key()].Readiness.LastKnown; got != Ready {
		t.Fatalf("repeated recompute lost last known readiness: %q", got)
	}

	missing := items[b.Key()]
	missing.ReadOutcome, missing.Fresh = "missing", false
	definitive := cloneItems(items)
	definitive[b.Key()] = missing
	if got := Recompute(definitive, CoverageComplete)[a.Key()].Readiness; got.Status != ReadinessUnknown || got.LastKnown != "" {
		t.Fatalf("definitive read kept last known readiness: %+v", got)
	}

	items[b.Key()] = found(b)
	items = Recompute(items, CoverageComplete)
	if got := items[a.Key()].Readiness; got.Status != Ready || got.LastKnown != "" {
		t.Fatalf("reread readiness=%+v", got)
	}
}

func TestStoreRetainsDetailsWhileItemIsRelisted(t *testing.T) {
	t.Parallel()
	ref := Ref{"s", "a"}
	observation := Observation{Principal: "alice", AccessScope: "scope"}
	edge := Relationship{Type: ParentChild, From: Ref{"s", "parent"}, To: ref, Fresh: true, Support: Supported}
	updated := time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	hydrated := Item{Summary: Summary{Ref: ref, CanonicalID: "node-a", Fresh: true, UpdatedAt: updated}, Body: "description", ReadOutcome: "found", ReadPermission: Allowed, TerminalKnown: true, DependenciesKnown: true, Closure: CoverageComplete, Relationships: []Relationship{edge}, Observation: observation}
	summary := Item{Summary: hydrated.Summary, ReadOutcome: "summary-only", Observation: observation}
	summary.Observation.AccessScope = ""
	for _, tc := range []struct {
		name   string
		change func(*Item)
		retain bool
	}{
		{name: "same owner", change: func(*Item) {}, retain: true},
		{name: "principal changed", change: func(i *Item) { i.Observation.Principal = "bob" }},
		{name: "identity changed", change: func(i *Item) { i.CanonicalID = "node-b" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := NewStore()
			publish := func(item Item) Item {
				snapshot, _ := store.publishComputed(func(s *Snapshot) {
					s.Items[ref.Key()] = item
					s.Sources["s"] = Coverage{State: CoverageComplete}
				}, true, nil)
				return snapshot.Items[ref.Key()]
			}
			if got := publish(hydrated).Readiness.Status; got != Ready {
				t.Fatalf("hydrated readiness=%s", got)
			}
			relisted := summary
			tc.change(&relisted)
			got := publish(relisted)
			retained := got.Body == hydrated.Body && len(got.Relationships) == 1 && got.Readiness.LastKnown == Ready
			if retained != tc.retain || got.Readiness.Status != ReadinessUnknown || EvaluateAction(got, ActionStart).Eligible {
				t.Fatalf("relisted item retain=%t: %+v", tc.retain, got)
			}
			if tc.retain && got.Relationships[0].Fresh {
				t.Fatal("retained relationship was presented as fresh")
			}
		})
	}

	// A listing rebuilt from cached edges is found without a body. It keeps
	// the previous body only while the provider update time is unchanged.
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprintf("cached edges changed=%t", changed), func(t *testing.T) {
			t.Parallel()
			store := NewStore()
			cached := hydrated
			cached.Body = ""
			if changed {
				cached.UpdatedAt = updated.Add(time.Minute)
			}
			var got Item
			for _, item := range []Item{hydrated, cached} {
				snapshot, _ := store.publishComputed(func(s *Snapshot) { s.Items[ref.Key()] = item }, true, nil)
				got = snapshot.Items[ref.Key()]
			}
			if (got.Body == hydrated.Body) == changed || got.Readiness.Status != Ready {
				t.Fatalf("cached-edge listing: %+v", got)
			}
		})
	}
}

func TestMissingReadDropsRetainedDetails(t *testing.T) {
	t.Parallel()
	source := Source{ID: "s", Adapter: "fake"}
	ref := Ref{"s", "1"}
	adapter := newFake()
	adapter.outcomes[ref.Key()] = []ItemOutcome{{Ref: ref, Kind: "missing"}}
	registry := NewRegistry()
	if err := registry.Register("fake", adapter); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(registry)
	relisted := Item{Summary: Summary{Ref: ref, Fresh: true}, Body: "retained description", ReadOutcome: "summary-only",
		Relationships: []Relationship{{Type: ParentChild, From: Ref{"s", "parent"}, To: ref}}}
	loader.Store.SeedSnapshot(Snapshot{Items: map[string]Item{ref.Key(): relisted}, Sources: map[string]Coverage{"s": {State: CoverageComplete}}})
	for range loader.HydrateEdges(context.Background(), source, []Ref{ref}, nil, false) {
	}
	if item, _ := loader.Store.Item(ref); item.ReadOutcome != "missing" || item.Body != "" || item.Relationships != nil {
		t.Fatalf("missing item kept retained details: %+v", item)
	}
}

func TestViewOrderFilteringAndCanonicalDedup(t *testing.T) {
	items := map[string]Item{"z": {Summary: Summary{Ref: Ref{"a", "2"}, CanonicalID: "same", Order: "1", State: StateOpen}}, "a": {Summary: Summary{Ref: Ref{"a", "1"}, CanonicalID: "same", Order: "1", State: StateOpen}}, "c": {Summary: Summary{Ref: Ref{"a", "3"}, Order: "2", State: StateBlocked}}}
	got := EvaluateView(items, View{SourceOrder: []string{"a", "b"}, Filters: Filters{States: []StateCategory{StateOpen}}})
	if len(got) != 1 || got[0].Ref.SourceID != "a" {
		t.Fatalf("unexpected view: %+v", got)
	}
}
func TestViewTextFilterMatchesItemID(t *testing.T) {
	t.Parallel()
	items := map[string]Item{
		"a": {Summary: Summary{Ref: Ref{"a", "TASK-128"}, Title: "Read-only slice", Order: "1"}},
		"b": {Summary: Summary{Ref: Ref{"a", "TASK-7"}, Title: "Other", Order: "2"}},
	}
	got := EvaluateView(items, View{SourceOrder: []string{"a"}, Filters: Filters{Text: "task-128"}})
	if len(got) != 1 || got[0].Ref.ItemID != "TASK-128" {
		t.Fatalf("ID filter: %+v", got)
	}
}
func TestSnapshotImmutability(t *testing.T) {
	store := NewStore()
	item := Item{Summary: Summary{Ref: Ref{"s", "i"}, AssignedTo: []string{"one"}}, Readiness: Readiness{Reasons: []string{"x"}}}
	store.publish(func(s *Snapshot) { s.Items[item.Ref.Key()] = item })
	snapshot := store.Current()
	copy, ok := snapshot.Item(item.Ref)
	if !ok {
		t.Fatal("item missing")
	}
	copy.AssignedTo[0] = "changed"
	copy.Readiness.Reasons[0] = "changed"
	snapshot.Items[item.Ref.Key()] = copy
	if got := store.Current().Items[item.Ref.Key()]; got.AssignedTo[0] != "one" || got.Readiness.Reasons[0] != "x" {
		t.Fatalf("snapshot mutated: %+v", got)
	}
}
func TestBacklogReadFailureKeepsSafeDiagnosticCode(t *testing.T) {
	t.Parallel()
	loader := NewLoader(NewRegistry())
	updates := make(chan Snapshot, 1)
	generation := loader.begin("tasks")
	loader.failSourceError(context.Background(), "tasks", generation, BacklogDiagnostic{Code: "provider-failed", Detail: "provider command failed"}, updates)
	if got := (<-updates).Sources["tasks"]; got.State != CoverageUnknown || got.Reason != "provider-failed" {
		t.Fatalf("Backlog failure lost its diagnostic code: %+v", got)
	}
}

type changingBacklogList struct {
	*fakeAdapter
	pages []SummaryPage
	calls int
	errAt int
}

func (a *changingBacklogList) OnDemandDetails() {}
func (a *changingBacklogList) List(_ context.Context, _ Source, _ Query, _ string) (SummaryPage, error) {
	return a.nextPage()
}
func (a *changingBacklogList) ConfirmBacklogList(_ context.Context, _ Source) (SummaryPage, error) {
	return a.nextPage()
}
func (a *changingBacklogList) nextPage() (SummaryPage, error) {
	a.calls++
	if a.calls == a.errAt {
		return SummaryPage{}, BacklogDiagnostic{Code: "observation-invalidated"}
	}
	return a.pages[min(a.calls-1, len(a.pages)-1)], nil
}

func TestBacklogRefreshConfirmsMissingTasksBeforeRetiring(t *testing.T) {
	t.Parallel()
	one := Ref{SourceID: "tasks", ItemID: "TASK-1"}
	two := Ref{SourceID: "tasks", ItemID: "TASK-2"}
	page := func(refs ...Ref) SummaryPage {
		items := make([]Summary, 0, len(refs))
		for _, ref := range refs {
			items = append(items, Summary{Ref: ref, Fresh: true})
		}
		return SummaryPage{Items: items, Coverage: Coverage{State: CoverageComplete, Total: len(refs), TotalAccuracy: TotalExact}}
	}
	for _, tc := range []struct {
		name    string
		second  SummaryPage
		errAt   int
		wantMin int
	}{
		{name: "transient gap", second: page(one, two), wantMin: 2},
		{name: "confirmed removal", second: page(one), wantMin: 1},
		{name: "changing omissions", second: page(two), wantMin: 2},
		{name: "failed confirmation", errAt: 2, wantMin: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &changingBacklogList{fakeAdapter: newFake(), pages: []SummaryPage{page(one), tc.second}, errAt: tc.errAt}
			registry := NewRegistry()
			registry.adapters["backlog-md"] = adapter
			loader := NewLoader(registry)
			loader.DeferDetails = true
			loader.Store.SeedSnapshot(Snapshot{Items: map[string]Item{
				one.Key(): {Summary: Summary{Ref: one, Fresh: true}, DependenciesKnown: true, Closure: CoverageComplete},
				two.Key(): {Summary: Summary{Ref: two, Fresh: true}, DependenciesKnown: true, Closure: CoverageComplete},
			}, Sources: map[string]Coverage{"tasks": {State: CoverageComplete, Total: 2}}})
			minItems := 2
			for snapshot := range loader.Refresh(context.Background(), []Source{{ID: "tasks", Adapter: "backlog-md"}}) {
				minItems = min(minItems, len(snapshot.Items))
			}
			final := loader.Store.Current()
			if adapter.calls != 2 || minItems != tc.wantMin || len(final.Items) != tc.wantMin {
				t.Fatalf("list calls=%d minimum rows=%d final rows=%d, want 2 calls and %d rows", adapter.calls, minItems, len(final.Items), tc.wantMin)
			}
			want := CoverageUnknown
			if tc.name == "confirmed removal" {
				want = CoverageComplete
				if _, present := final.Items[two.Key()]; present {
					t.Fatal("confirmed removal retained old task")
				}
			}
			if got := final.Sources["tasks"].State; got != want {
				t.Fatalf("coverage = %s, want %s", got, want)
			}
		})
	}
}

type interruptedBacklogDetail struct{ *changingBacklogList }

func (a *interruptedBacklogDetail) ReadItems(_ context.Context, _ Source, refs []Ref, _ []string, _ int) []ItemOutcome {
	return []ItemOutcome{{Ref: refs[0], Kind: "failed", Err: BacklogDiagnostic{Code: "observation-invalidated"}}}
}

func TestBacklogInterruptedDetailKeepsListedSummary(t *testing.T) {
	t.Parallel()
	ref := Ref{SourceID: "tasks", ItemID: "TASK-1"}
	registry := NewRegistry()
	registry.adapters["backlog-md"] = &interruptedBacklogDetail{&changingBacklogList{fakeAdapter: newFake()}}
	loader := NewLoader(registry)
	loader.Store.SeedSnapshot(Snapshot{Items: map[string]Item{
		ref.Key(): {Summary: Summary{Ref: ref, Fresh: true}, ReadOutcome: "summary-only"},
	}, Sources: map[string]Coverage{"tasks": {State: CoverageComplete}}})
	for range loader.HydrateDetail(context.Background(), Source{ID: "tasks", Adapter: "backlog-md"}, ref) {
	}
	item, _ := loader.Store.Item(ref)
	if !item.Fresh || item.ReadOutcome != "summary-only" {
		t.Fatalf("interrupted detail overwrote listed summary: %+v", item)
	}
}

func TestIndependentSourceRefreshPublishesHealthyBeforeSlowSource(t *testing.T) {
	fake := newFake()
	fake.pages["healthy"] = []SummaryPage{{Items: []Summary{{Ref: Ref{"healthy", "a"}, Title: "A", Fresh: true}}, Coverage: Coverage{State: CoverageComplete, TotalAccuracy: TotalExact}}}
	fake.pages["slow"] = []SummaryPage{{Items: []Summary{{Ref: Ref{"slow", "b"}, Fresh: true}}, Coverage: Coverage{State: CoverageComplete}}}
	fake.delays["slow"] = time.Second
	registry := NewRegistry()
	if err := registry.Register("fake", fake); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(registry)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	updates := loader.Refresh(ctx, []Source{{ID: "slow", Adapter: "fake"}, {ID: "healthy", Adapter: "fake"}})
	found := false
	for !found {
		select {
		case snap, ok := <-updates:
			if !ok {
				t.Fatal("updates closed before healthy source")
			}
			if _, ok := snap.Items[(Ref{"healthy", "a"}).Key()]; ok {
				found = true
			}
		case <-ctx.Done():
			t.Fatal("healthy source starved by slow source")
		}
	}
}
func TestFakeAdapterPaginationErrorsCapabilitiesAndEdges(t *testing.T) {
	fake := newFake()
	fake.pages["s"] = []SummaryPage{{Items: []Summary{{Ref: Ref{"s", "1"}}}}, {Items: []Summary{{Ref: Ref{"s", "2"}}}}}
	first, err := fake.List(context.Background(), Source{ID: "s"}, Query{}, "")
	if err != nil || first.NextCursor != "page-1" {
		t.Fatalf("first page=%+v err=%v", first, err)
	}
	second, err := fake.List(context.Background(), Source{ID: "s"}, Query{}, first.NextCursor)
	if err != nil || len(second.Items) != 1 || second.Items[0].Ref.ItemID != "2" {
		t.Fatalf("second page=%+v err=%v", second, err)
	}
	fake.errors["s"] = context.DeadlineExceeded
	if _, err = fake.List(context.Background(), Source{ID: "s"}, Query{}, ""); err == nil {
		t.Fatal("list error was hidden")
	}
	delete(fake.errors, "s")
	fake.denied["s"] = true
	caps, _ := fake.Capabilities(context.Background(), Source{ID: "s"}, "", nil)
	if caps["list"].Permission != Denied {
		t.Fatalf("capability denial lost: %+v", caps)
	}
	edge := Relationship{Type: HardPrerequisite, From: Ref{"s", "1"}, To: Ref{"s", "2"}, Condition: "terminal", Fresh: true}
	fake.edges[edge.From.Key()] = []DependencyPage{{Edges: []Relationship{edge}, Completeness: CoverageComplete}}
	deps, err := fake.ReadDependencies(context.Background(), Source{ID: "s"}, edge.From, "", 10)
	if err != nil || len(deps.Edges) != 1 {
		t.Fatalf("dependency edge=%+v err=%v", deps, err)
	}
}

func TestExplicitCompleteClosureRemainsReadyInPartialSource(t *testing.T) {
	ref := Ref{"s", "a"}
	item := Item{Summary: Summary{Ref: ref, Fresh: true}, DependenciesKnown: true, Closure: CoverageComplete}
	if got := Recompute(map[string]Item{ref.Key(): item}, CoveragePartial)[ref.Key()].Readiness.Status; got != Ready {
		t.Fatalf("complete explicit closure readiness=%s", got)
	}
}

func TestReadinessProviderReadyIsNotProofAndReadinessIndependentOfClaim(t *testing.T) {
	r := Ref{"s", "a"}
	item := Item{Summary: Summary{Ref: r, Fresh: true, ProviderReady: boolPtr(true)}, DependenciesKnown: false, Claim: ClaimObservation{Known: true, Active: true}}
	got := Recompute(map[string]Item{r.Key(): item}, CoveragePartial)[r.Key()]
	if got.Readiness.Status != ReadinessUnknown {
		t.Fatalf("provider readiness proved %s", got.Readiness.Status)
	}
	if EvaluateAction(got, ActionStart).Eligible {
		t.Fatal("active claim eligible")
	}
	if reflect.DeepEqual(got.Readiness.Reasons, []string{"provider-ready"}) {
		t.Fatal("provider readiness leaked")
	}
}
func boolPtr(v bool) *bool { return &v }

func TestInterruptedDetailReadKeepsListedSummary(t *testing.T) {
	t.Parallel()
	source := Source{ID: "s", Adapter: "fake"}
	ref := Ref{"s", "1"}
	adapter := newFake()
	adapter.outcomes[ref.Key()] = []ItemOutcome{{Ref: ref, Err: BacklogDiagnostic{"cancelled", "provider read cancelled"}}}
	registry := NewRegistry()
	if err := registry.Register("fake", adapter); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(registry)
	listed := Item{Summary: Summary{Ref: ref, Fresh: true}, TerminalKnown: true, ReadOutcome: "summary-only"}
	loader.Store.SeedSnapshot(Snapshot{Items: map[string]Item{ref.Key(): listed}, Sources: map[string]Coverage{"s": {State: CoverageComplete, Total: 1}}})
	for range loader.HydrateEdges(context.Background(), source, []Ref{ref}, nil, false) {
	}
	if item, _ := loader.Store.Item(ref); !item.Fresh || item.ReadOutcome != "summary-only" {
		t.Fatalf("interrupted read changed listed item: fresh %v outcome %q", item.Fresh, item.ReadOutcome)
	}
}

func TestSourceFailureKeepsAdapterDiagnosticCode(t *testing.T) {
	t.Parallel()
	adapter := newFake()
	adapter.errors["s"] = BacklogDiagnostic{"observation-invalidated", "provider changed during task list"}
	registry := NewRegistry()
	if err := registry.Register("fake", adapter); err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(registry)
	for range loader.Refresh(context.Background(), []Source{{ID: "s", Adapter: "fake"}}) {
	}
	if got := loader.Store.Current().Sources["s"].Reason; got != "observation-invalidated" {
		t.Fatalf("source reason = %q", got)
	}
}
