package queue

import (
	"context"
	"sync"
)

type Snapshot struct {
	Revision uint64              `json:"revision"`
	Items    map[string]Item     `json:"-"`
	Sources  map[string]Coverage `json:"sources"`
	Deleted  map[string]Ref      `json:"-"`
}

func (s Snapshot) Item(ref Ref) (Item, bool) {
	item, ok := s.Items[ref.Key()]
	return cloneItem(item), ok
}
func (s Snapshot) Clone() Snapshot {
	deleted := make(map[string]Ref, len(s.Deleted))
	for key, ref := range s.Deleted {
		deleted[key] = ref
	}
	return Snapshot{Revision: s.Revision, Items: cloneItems(s.Items), Sources: cloneCoverage(s.Sources), Deleted: deleted}
}
func cloneCoverage(in map[string]Coverage) map[string]Coverage {
	out := make(map[string]Coverage, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

type Store struct {
	mu      sync.RWMutex
	current Snapshot
	next    uint64
	subs    map[uint64]chan Snapshot
	subNext uint64
}

func NewStore() *Store {
	return &Store{current: Snapshot{Items: map[string]Item{}, Sources: map[string]Coverage{}, Deleted: map[string]Ref{}}, subs: map[uint64]chan Snapshot{}}
}
func (s *Store) Current() Snapshot { s.mu.RLock(); defer s.mu.RUnlock(); return s.current.Clone() }

// SeedSnapshot installs a previously observed cache snapshot before revalidation begins.
func (s *Store) SeedSnapshot(seed Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for source, coverage := range seed.Sources {
		if coverage.State == CoverageComplete {
			for key, item := range s.current.Items {
				if item.Ref.SourceID == source {
					delete(s.current.Items, key)
				}
			}
		}
		s.current.Sources[source] = coverage
	}
	for key, item := range seed.Items {
		s.current.Items[key] = cloneItem(item)
	}
	if s.current.Deleted == nil {
		s.current.Deleted = make(map[string]Ref)
	}
	for key, ref := range seed.Deleted {
		s.current.Deleted[key] = ref
	}
	s.current.Items = Recompute(s.current.Items, aggregateCoverage(s.current.Sources))
	s.next++
	s.current.Revision = s.next
	for _, ch := range s.subs {
		snap := s.current.Clone()
		select {
		case ch <- snap:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- snap:
			default:
			}
		}
	}
}
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

// Computation is performed outside the store lock and retried if another source publishes meanwhile.
func (s *Store) publishComputed(update func(*Snapshot), recompute bool, valid func() bool) (Snapshot, bool) {
	for {
		s.mu.RLock()
		base := s.current.Clone()
		revision := s.current.Revision
		s.mu.RUnlock()
		update(&base)
		if recompute {
			base.Items = Recompute(base.Items, aggregateCoverage(base.Sources))
		}
		s.mu.Lock()
		if valid != nil && !valid() {
			s.mu.Unlock()
			return Snapshot{}, false
		}
		if s.current.Revision != revision {
			s.mu.Unlock()
			continue
		}
		s.next++
		base.Revision = s.next
		s.current = base.Clone()
		for _, ch := range s.subs {
			snap := base.Clone()
			select {
			case ch <- snap:
			default:
				select {
				case <-ch:
				default:
				}
				select {
				case ch <- snap:
				default:
				}
			}
		}
		result := base.Clone()
		s.mu.Unlock()
		return result, true
	}
}

type Loader struct {
	Registry       *Registry
	Store          *Store
	Query          Query
	Fields         []string
	Budget         int
	HydrationLimit int
	mu             sync.Mutex
	generation     map[string]uint64
}

func NewLoader(registry *Registry) *Loader {
	return &Loader{Registry: registry, Store: NewStore(), Budget: 100, HydrationLimit: 4, generation: map[string]uint64{}}
}
func (l *Loader) begin(sourceID string) uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.generation[sourceID]++
	return l.generation[sourceID]
}
func (l *Loader) current(sourceID string, generation uint64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.generation[sourceID] == generation
}

// Refresh starts each source independently; each source publishes summaries before bounded detail hydration.
func (l *Loader) Refresh(ctx context.Context, sources []Source) <-chan Snapshot {
	updates := make(chan Snapshot, len(sources)*2+1)
	var wg sync.WaitGroup
	for _, source := range sources {
		source := source
		generation := l.begin(source.ID)
		wg.Add(1)
		go func() {
			defer wg.Done()
			adapter, ok := l.Registry.Get(source.Adapter)
			if !ok {
				l.failSource(ctx, source.ID, generation, "adapter-unavailable", updates)
				return
			}
			l.loadSource(ctx, adapter, source, generation, updates)
		}()
	}
	go func() { wg.Wait(); close(updates) }()
	return updates
}
func sendSnapshot(ctx context.Context, ch chan<- Snapshot, s Snapshot) {
	select {
	case ch <- s:
	case <-ctx.Done():
	}
}
func (l *Loader) publish(ctx context.Context, source string, generation uint64, update func(*Snapshot), out chan<- Snapshot) {
	snap, published := l.Store.publishComputed(update, true, func() bool { return l.current(source, generation) })
	if published {
		sendSnapshot(ctx, out, snap)
	}
}
func (l *Loader) failSource(ctx context.Context, source string, generation uint64, reason string, out chan<- Snapshot) {
	l.publish(ctx, source, generation, func(s *Snapshot) {
		for key, item := range s.Items {
			if item.Ref.SourceID == source {
				item.Fresh = false
				item.ReadOutcome = "stale"
				s.Items[key] = item
			}
		}
		s.Sources[source] = Coverage{State: CoverageUnknown, Reason: reason, TotalAccuracy: TotalUnknown}
	}, out)
}
func (l *Loader) loadSource(ctx context.Context, a Adapter, source Source, generation uint64, out chan<- Snapshot) {
	cursor := ""
	seenRefs := map[string]bool{}
	seenCursors := map[string]bool{}
	scanComplete := true
	limit := l.HydrationLimit
	if limit < 1 {
		limit = 1
	}
	type hydrationJob struct {
		ref  Ref
		item Item
	}
	jobs := make(chan hydrationJob, limit*4)
	var workers sync.WaitGroup
	for range limit {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobs {
				if ctx.Err() == nil {
					l.hydrateItem(ctx, a, source, generation, job.ref, job.item, out)
				}
			}
		}()
	}
	for {
		if ctx.Err() != nil {
			l.failSource(ctx, source.ID, generation, "refresh-cancelled", out)
			break
		}
		page, err := a.List(ctx, source, l.Query, cursor)
		if err != nil {
			l.failSource(ctx, source.ID, generation, "source-read-failed", out)
			break
		}
		if page.Coverage.State == CoverageUnknown {
			scanComplete = false
		}
		coverage := page.Coverage
		coverage.Cursor = page.NextCursor
		if page.NextCursor != "" {
			coverage.State = CoveragePartial
		}
		refs := make([]Ref, 0, len(page.Items))
		items := make(map[string]Item, len(page.Items))
		for _, summary := range page.Items {
			if summary.Ref.SourceID != source.ID || !validRef(summary.Ref) {
				continue
			}
			key := summary.Ref.Key()
			seenRefs[key] = true
			refs = append(refs, summary.Ref)
			items[key] = Item{Summary: summary, ReadOutcome: "summary-only", DependenciesKnown: false, Observation: page.Observation, Coverage: coverage}
		}
		// A new principal/config generation invalidates the old source partition before exposing new summaries.
		reset := false
		for _, item := range l.Store.Current().Items {
			if item.Ref.SourceID == source.ID && ((item.Observation.Principal != "" && item.Observation.Principal != page.Observation.Principal) || (item.Observation.ConfigurationGeneration != "" && item.Observation.ConfigurationGeneration != page.Observation.ConfigurationGeneration)) {
				reset = true
				break
			}
		}
		l.publish(ctx, source.ID, generation, func(s *Snapshot) {
			if cursor == "" {
				for key, item := range s.Items {
					if item.Ref.SourceID == source.ID {
						item.Fresh = false
						item.ReadOutcome = "stale"
						s.Items[key] = item
					}
				}
			}
			for key, ref := range s.Deleted {
				if ref.SourceID == source.ID {
					delete(s.Deleted, key)
				}
			}
			if reset {
				for key, item := range s.Items {
					if item.Ref.SourceID == source.ID {
						delete(s.Items, key)
					}
				}
			}
			for key, item := range items {
				s.Items[key] = item
			}
			s.Sources[source.ID] = coverage
		}, out)
		// Expensive per-item providers hydrate only explicitly requested details.
		_, onDemand := a.(interface{ OnDemandDetails() })
		if onDemand {
			refs = nil
		}
		// A slow item consumes one bounded slot, not the page or other slots.
		for _, ref := range refs {
			select {
			case jobs <- hydrationJob{ref: ref, item: items[ref.Key()]}:
			default: /* leave bounded, unhydrated summary as unknown */
			}
		}
		cursor = page.NextCursor
		if cursor == "" {
			complete := scanComplete && page.Coverage.State == CoverageComplete
			if complete {
				coverage.State = CoverageComplete
				coverage.Cursor = ""
			}
			if !complete {
				coverage.State = CoveragePartial
			}
			l.publish(ctx, source.ID, generation, func(s *Snapshot) {
				if complete {
					for key, item := range s.Items {
						if item.Ref.SourceID == source.ID && !seenRefs[key] {
							delete(s.Items, key)
						}
					}
				}
				s.Sources[source.ID] = coverage
			}, out)
			break
		}
		if seenCursors[cursor] {
			l.failSource(ctx, source.ID, generation, "cursor-loop", out)
			break
		}
		seenCursors[cursor] = true
	}
	close(jobs)
	workers.Wait()
}
func (l *Loader) hydrateItem(ctx context.Context, a Adapter, source Source, generation uint64, ref Ref, summary Item, out chan<- Snapshot) {
	outcomes := a.ReadItems(ctx, source, []Ref{ref}, l.Fields, l.Budget)
	var item *Item
	for _, outcome := range outcomes {
		if outcome.Ref.Key() != ref.Key() {
			continue
		}
		if outcome.Item != nil {
			copy := cloneItem(*outcome.Item)
			item = &copy
		}
		if outcome.Err != nil {
			summary.ReadOutcome = "failed"
			summary.Fresh = false
		} else if outcome.Item == nil {
			summary.ReadOutcome = outcome.Kind
			if summary.ReadOutcome == "" {
				summary.ReadOutcome = "missing"
			}
			if summary.ReadOutcome == "denied" || summary.ReadOutcome == "inaccessible" {
				summary.ReadPermission = Denied
			}
			summary.Fresh = false
		}
	}
	if item == nil {
		l.publish(ctx, source.ID, generation, func(s *Snapshot) {
			if summary.ReadOutcome == "inaccessible" || summary.ReadOutcome == "denied" {
				delete(s.Items, ref.Key())
				if s.Deleted == nil {
					s.Deleted = make(map[string]Ref)
				}
				s.Deleted[ref.Key()] = ref
				return
			}
			summary.DependenciesKnown = false
			summary.Closure = CoverageUnknown
			summary.Readiness = Readiness{Status: ReadinessUnknown, Reasons: []string{"item-read-" + summary.ReadOutcome}, Freshness: FreshnessUnknown}
			s.Items[ref.Key()] = summary
		}, out)
		return
	}
	if item.Ref.Key() != ref.Key() {
		return
	}
	if (item.Observation.Principal != "" && summary.Observation.Principal != "" && item.Observation.Principal != summary.Observation.Principal) || (item.Observation.ConfigurationGeneration != "" && summary.Observation.ConfigurationGeneration != "" && item.Observation.ConfigurationGeneration != summary.Observation.ConfigurationGeneration) {
		summary.Fresh = false
		summary.ReadOutcome = "identity-changed"
		summary.DependenciesKnown = false
		l.publish(ctx, source.ID, generation, func(s *Snapshot) { s.Items[ref.Key()] = summary }, out)
		return
	}
	if item.Title == "" {
		item.Title = summary.Title
	}
	if item.CanonicalID == "" {
		item.CanonicalID = summary.CanonicalID
	}
	if item.Observation.Principal == "" {
		item.Observation = summary.Observation
	}
	item.Coverage = summary.Coverage
	item.ReadOutcome = "found"
	depCursor := ""
	seenDependencyCursors := map[string]bool{}
	allComplete := true
	for {
		if ctx.Err() != nil {
			allComplete = false
			break
		}
		deps, err := a.ReadDependencies(ctx, source, ref, depCursor, l.Budget)
		if err != nil {
			allComplete = false
			break
		}
		if deps.Completeness != CoverageComplete {
			allComplete = false
		}
		if observationMismatch(item.Observation, deps.Observation) {
			l.publish(ctx, source.ID, generation, func(s *Snapshot) {
				for key, existing := range s.Items {
					if existing.Ref.SourceID == source.ID {
						delete(s.Items, key)
					}
				}
				s.Sources[source.ID] = Coverage{State: CoverageUnknown, Reason: "principal-changed-during-hydration", TotalAccuracy: TotalUnknown}
			}, out)
			return
		}
		if len(deps.Edges) > 0 || item.Relationships != nil {
			if item.Relationships == nil {
				item.Relationships = make([]Relationship, 0)
			}
			item.Relationships = append(item.Relationships, deps.Edges...)
		}
		if deps.Observation.ObservedAt.After(item.Observation.ObservedAt) || item.Observation.ObservedAt.IsZero() {
			item.Observation = deps.Observation
		}
		depCursor = deps.NextCursor
		if depCursor == "" {
			break
		}
		if seenDependencyCursors[depCursor] {
			allComplete = false
			break
		}
		seenDependencyCursors[depCursor] = true
	}
	item.DependenciesKnown = allComplete
	item.Closure = CoverageUnknown
	if allComplete {
		item.Closure = CoverageComplete
	}
	item.Fresh = item.Fresh && summary.Fresh
	l.publish(ctx, source.ID, generation, func(s *Snapshot) { s.Items[ref.Key()] = *item }, out)
}
func observationMismatch(a, b Observation) bool {
	return a.Principal != "" && b.Principal != "" && a.Principal != b.Principal ||
		a.ConfigurationGeneration != "" && b.ConfigurationGeneration != "" && a.ConfigurationGeneration != b.ConfigurationGeneration
}

func aggregateCoverage(sources map[string]Coverage) CoverageState {
	if len(sources) == 0 {
		return CoverageUnknown
	}
	for _, c := range sources {
		if c.State != CoverageComplete {
			return c.State
		}
	}
	return CoverageComplete
}
