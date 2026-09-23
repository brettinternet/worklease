package queue

import (
	"context"
	"sync"
	"time"
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
func (s *Store) Item(ref Ref) (Item, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.current.Items[ref.Key()]
	return cloneItem(item), ok
}

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
			for source, coverage := range base.Sources {
				coverage.ObservedEdges = 0
				for _, item := range base.Items {
					if item.Ref.SourceID == source && item.DependenciesKnown && item.Closure == CoverageComplete && item.Fresh {
						coverage.ObservedEdges++
					}
				}
				base.Sources[source] = coverage
			}
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

type hydrationJob struct {
	ref  Ref
	item Item
}

const githubPagesPerRefresh = 5

type Loader struct {
	Registry       *Registry
	Store          *Store
	GitHubSync     GitHubSyncStore
	Query          Query
	Fields         []string
	Budget         int
	HydrationLimit int
	// DeferDetails lets interactive views hydrate only their visible rows.
	DeferDetails bool
	mu           sync.Mutex
	generation   map[string]uint64
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

// HydrateVisible batches the explicitly visible GitHub rows by node ID.
// Off-screen summaries remain unhydrated until the view requests them.
func (l *Loader) HydrateVisible(ctx context.Context, source Source, refs []Ref) <-chan Snapshot {
	out := make(chan Snapshot, 8)
	a, ok := l.Registry.Get(source.Adapter)
	if !ok || !isBatchHydrator(a) {
		close(out)
		return out
	}
	l.mu.Lock()
	generation := l.generation[source.ID]
	l.mu.Unlock()
	go func() {
		defer close(out)
		for start := 0; start < len(refs) && ctx.Err() == nil; start += 100 {
			end := min(start+100, len(refs))
			jobs := make([]hydrationJob, 0, end-start)
			for _, ref := range refs[start:end] {
				if ref.SourceID != source.ID {
					continue
				}
				if item, found := l.Store.Item(ref); found && (item.ReadOutcome == "summary-only" || !item.Fresh) {
					jobs = append(jobs, hydrationJob{ref: ref, item: item})
				}
			}
			if len(jobs) > 0 && l.current(source.ID, generation) {
				l.hydrateBatch(ctx, a, source, generation, jobs, out)
			}
		}
	}()
	return out
}

// HydrateEdges fills a Backlog source after summary publication. The selected
// closure runs first, followed by visible rows and then remaining summaries.
// Every provider view is scheduled by quotaScheduler, never by an unbounded
// subprocess fan-out. The returned stream must be drained until closed.
func (l *Loader) HydrateEdges(ctx context.Context, source Source, selected, visible []Ref, background bool) <-chan Snapshot {
	out := make(chan Snapshot, 8)
	a, ok := l.Registry.Get(source.Adapter)
	if !ok {
		close(out)
		return out
	}
	l.mu.Lock()
	generation := l.generation[source.ID]
	l.mu.Unlock()
	go func() {
		defer close(out)
		seen := map[string]bool{}
		// Selected closure is discovered incrementally: a cold selected view may
		// reveal edges that were absent from the initial summary snapshot.
		pending := append([]Ref(nil), selected...)
		for len(pending) > 0 && ctx.Err() == nil && l.current(source.ID, generation) {
			ref := pending[0]
			pending = pending[1:]
			if ref.SourceID != source.ID || seen[ref.Key()] {
				continue
			}
			seen[ref.Key()] = true
			item, ok := l.Store.Item(ref)
			if !ok {
				continue
			}
			if !item.DependenciesKnown || item.Closure != CoverageComplete || !item.Fresh {
				l.hydrateItem(context.WithValue(ctx, backlogPriorityKey{}, PriorityAction), a, source, generation, ref, item, out)
				item, _ = l.Store.Item(ref)
			}
			for _, edge := range item.Relationships {
				if edge.From == ref && edge.Type == HardPrerequisite {
					pending = append(pending, edge.To)
				}
			}
		}
		if ctx.Err() != nil || !l.current(source.ID, generation) {
			return
		}
		type request struct {
			ref      Ref
			priority RequestPriority
		}
		requests := make([]request, 0)
		appendRef := func(ref Ref, priority RequestPriority) {
			if ref.SourceID != source.ID || seen[ref.Key()] {
				return
			}
			seen[ref.Key()] = true
			if item, ok := l.Store.Item(ref); ok && (!item.DependenciesKnown || item.Closure != CoverageComplete || !item.Fresh) {
				requests = append(requests, request{ref, priority})
			}
		}
		for _, ref := range visible {
			appendRef(ref, PriorityVisible)
		}
		if background {
			for _, item := range l.Store.Current().Items {
				appendRef(item.Ref, PriorityBackground)
			}
		}
		limit := l.HydrationLimit
		if limit < 1 {
			limit = 1
		}
		jobs := make(chan request)
		var workers sync.WaitGroup
		for range limit {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for job := range jobs {
					item, ok := l.Store.Item(job.ref)
					if ok && ctx.Err() == nil && l.current(source.ID, generation) {
						l.hydrateItem(context.WithValue(ctx, backlogPriorityKey{}, job.priority), a, source, generation, job.ref, item, out)
					}
				}
			}()
		}
		for _, job := range requests {
			if !l.current(source.ID, generation) {
				break
			}
			select {
			case jobs <- job:
			case <-ctx.Done():
				close(jobs)
				workers.Wait()
				return
			}
		}
		close(jobs)
		workers.Wait()
	}()
	return out
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
func (l *Loader) withholdSource(ctx context.Context, source Source, generation uint64, reason string, out chan<- Snapshot) {
	if l.GitHubSync != nil {
		if err := l.GitHubSync.WithholdGitHubSource(ctx, source); err != nil {
			reason = "github-access-unverified-payload-purge-failed"
		} else if err := l.GitHubSync.RestartGitHubSync(ctx, source, true); err != nil {
			reason = "github-reconciliation-restart-failed"
		}
	}
	l.publish(ctx, source.ID, generation, func(s *Snapshot) {
		for key, item := range s.Items {
			if item.Ref.SourceID == source.ID {
				delete(s.Items, key)
			}
		}
		s.Sources[source.ID] = Coverage{State: CoverageUnknown, Reason: reason, TotalAccuracy: TotalUnknown}
	}, out)
}
func (l *Loader) loadSource(ctx context.Context, a Adapter, source Source, generation uint64, out chan<- Snapshot) {
	if source.Adapter == "github" && l.GitHubSync != nil {
		release, err := l.GitHubSync.LockGitHubSync(ctx, source)
		if err != nil {
			l.failSource(ctx, source.ID, generation, "sync-lock-unavailable", out)
			return
		}
		defer release()
	}
	cursor := ""
	var committedWatermark, scanWatermark time.Time
	var checkpoint SyncCheckpoint
	var reconcile bool
	incrementalAdapter, supportsIncremental := a.(IncrementalListAdapter)
	useIncremental := false
	if source.Adapter == "github" && supportsIncremental && l.GitHubSync != nil {
		var err error
		checkpoint, err = l.GitHubSync.LoadGitHubSync(ctx, source)
		if err != nil {
			l.failSource(ctx, source.ID, generation, "sync-state-unavailable", out)
			return
		}
		checkpoint, reconcile, err = l.GitHubSync.StartGitHubReconciliation(ctx, source, time.Now().UTC())
		if err != nil {
			l.failSource(ctx, source.ID, generation, "reconciliation-state-unavailable", out)
			return
		}
		// GitHub payloads are not seeded from the disk index before a live
		// authorization check. A fresh process cannot build a complete visible
		// projection from a since-only window; finish a full bounded scan first.
		if !reconcile && !checkpoint.CommittedWatermark.IsZero() {
			current := l.Store.Current()
			hasProjection := current.Sources[source.ID].State == CoverageComplete
			for _, item := range current.Items {
				if item.Ref.SourceID == source.ID {
					hasProjection = true
					break
				}
			}
			if !hasProjection {
				if err = l.GitHubSync.RestartGitHubSync(ctx, source, true); err == nil {
					checkpoint, reconcile, err = l.GitHubSync.StartGitHubReconciliation(ctx, source, time.Now().UTC())
				}
				if err != nil {
					l.failSource(ctx, source.ID, generation, "reconciliation-state-unavailable", out)
					return
				}
			}
		}
		committedWatermark = checkpoint.CommittedWatermark
		if github, ok := a.(*GitHubAdapter); ok && !committedWatermark.IsZero() {
			github.pollNewestHint(ctx, source)
		}
		scanWatermark = checkpoint.ScanWatermark
		if reconcile {
			cursor = checkpoint.ReconciliationCursor
			if cursor == "@start" {
				cursor = ""
			}
		} else {
			cursor = checkpoint.Cursor
			useIncremental = !committedWatermark.IsZero()
		}
		if scanWatermark.IsZero() || cursor == "" {
			scanWatermark = time.Now().UTC()
		}
	}
	seenRefs := map[string]bool{}
	seenCursors := map[string]bool{}
	pagesRead := 0
	scanComplete := true
	limit := l.HydrationLimit
	if limit < 1 {
		limit = 1
	}
	type hydrationTask struct{ batch []hydrationJob }
	jobs := make(chan hydrationTask, limit*4)
	var workers sync.WaitGroup
	for range limit {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for task := range jobs {
				if ctx.Err() != nil {
					continue
				}
				if len(task.batch) > 1 || (len(task.batch) == 1 && isBatchHydrator(a)) {
					l.hydrateBatch(ctx, a, source, generation, task.batch, out)
				} else if len(task.batch) == 1 {
					job := task.batch[0]
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
		var page SummaryPage
		var err error
		if reconcile {
			page, err = a.List(ctx, source, l.Query, cursor)
			page.Reconciliation = true
		} else if useIncremental {
			page, err = incrementalAdapter.ListIncremental(ctx, source, l.Query, cursor, committedWatermark, scanWatermark)
		} else {
			page, err = a.List(ctx, source, l.Query, cursor)
		}
		if err != nil {
			if diagnostic, ok := err.(GitHubDiagnostic); ok && diagnostic.Code == "invalid-cursor" && source.Adapter == "github" && l.GitHubSync != nil {
				_ = l.GitHubSync.RestartGitHubSync(ctx, source, reconcile)
			}
			if diagnostic, ok := err.(GitHubDiagnostic); ok && (diagnostic.Code == "not-found-or-inaccessible" || diagnostic.Code == "permission-denied" || diagnostic.Code == "saml-sso" || diagnostic.Code == "identity-changed" || diagnostic.Code == "authentication") {
				l.withholdSource(ctx, source, generation, "github-access-unverified", out)
				if diagnostic.Code == "permission-denied" || diagnostic.Code == "saml-sso" || diagnostic.Code == "authentication" {
					if marker, ok := l.GitHubSync.(interface {
						MarkGitHubInaccessible(context.Context, Source) error
					}); ok {
						_ = marker.MarkGitHubInaccessible(ctx, source)
					}
				}
			} else {
				l.failSource(ctx, source.ID, generation, "source-read-failed", out)
			}
			break
		}
		pagesRead++
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
			item := Item{Summary: summary, ReadOutcome: "summary-only", DependenciesKnown: false, Observation: page.Observation, Coverage: coverage}
			if cached, ok := a.(interface {
				CachedEdges(Source, Ref) (DependencyPage, bool)
			}); ok {
				item.TerminalKnown = true // list status is independent of edge hydration
				item.ReadPermission = Allowed
				if deps, valid := cached.CachedEdges(source, summary.Ref); valid {
					item.ReadOutcome = "found"
					item.ReadPermission = Allowed
					item.TerminalKnown = true // this status came from the current complete list
					item.Relationships = deps.Edges
					item.DependenciesKnown = deps.Completeness == CoverageComplete
					item.Closure = deps.Completeness
				}
			}
			items[key] = item
		}
		var retired []Ref
		if source.Adapter == "github" && l.GitHubSync != nil {
			pageItems := make([]Item, 0, len(items))
			for _, item := range items {
				pageItems = append(pageItems, item)
			}
			if reconcile {
				reconCursor := page.NextCursor
				completeRecon := reconCursor == "" && page.Coverage.State == CoverageComplete
				if !completeRecon && reconCursor == "" {
					reconCursor = "@start"
				}
				var commitErr error
				retired, commitErr = l.GitHubSync.CommitGitHubReconciliationPage(ctx, source, pageItems, reconCursor, checkpoint, completeRecon)
				if commitErr != nil {
					l.failSource(ctx, source.ID, generation, "reconciliation-write-failed", out)
					break
				}
				checkpoint.ReconciliationCursor = reconCursor
			} else {
				syncComplete := page.NextCursor == "" && page.Coverage.State == CoverageComplete
				if err := l.GitHubSync.CommitGitHubSyncPage(ctx, source, pageItems, page.NextCursor, scanWatermark, syncComplete); err != nil {
					l.failSource(ctx, source.ID, generation, "sync-state-write-failed", out)
					break
				}
				if syncComplete && !useIncremental {
					committedWatermark = scanWatermark
					useIncremental = true
				}
			}
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
			// A transfer or renumbering can change the ref while retaining the
			// immutable GitHub node ID. Never display both identities at once.
			if source.Adapter == "github" {
				byNode := make(map[string]string, len(items))
				for key, item := range items {
					if item.CanonicalID != "" {
						byNode[item.CanonicalID] = key
					}
				}
				for key, existing := range s.Items {
					if existing.Ref.SourceID == source.ID && existing.CanonicalID != "" {
						if newKey, ok := byNode[existing.CanonicalID]; ok && newKey != key {
							delete(s.Items, key)
						}
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
		batchHydration := isBatchHydrator(a)
		if onDemand && (!batchHydration || l.DeferDetails) {
			refs = nil
		}
		// A slow item consumes one bounded slot, not the page or other slots.
		for start := 0; start < len(refs); {
			end := start + 1
			if batchHydration {
				end = start + 100
				if end > len(refs) {
					end = len(refs)
				}
			}
			batch := make([]hydrationJob, 0, end-start)
			for _, ref := range refs[start:end] {
				batch = append(batch, hydrationJob{ref: ref, item: items[ref.Key()]})
			}
			select {
			case jobs <- hydrationTask{batch: batch}:
			default: /* leave bounded, unhydrated summary as unknown */
			}
			start = end
		}
		cursor = page.NextCursor
		if cursor == "" {
			complete := scanComplete && page.Coverage.State == CoverageComplete
			if complete && (!page.Incremental || page.Reconciliation) {
				coverage.State = CoverageComplete
				coverage.Cursor = ""
			} else {
				coverage.State = CoveragePartial
				if page.Incremental {
					coverage.Reason = "incremental-window-complete; reconciliation-pending"
				} else {
					coverage.Reason = "reconciliation-incomplete"
				}
			}
			l.publish(ctx, source.ID, generation, func(s *Snapshot) {
				if page.Reconciliation && complete {
					for _, ref := range retired {
						delete(s.Items, ref.Key())
					}
				} else if complete && !page.Incremental {
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
		if source.Adapter == "github" && pagesRead >= githubPagesPerRefresh {
			coverage.State = CoveragePartial
			coverage.Reason = "github-page-budget-reached"
			l.publish(ctx, source.ID, generation, func(s *Snapshot) { s.Sources[source.ID] = coverage }, out)
			break
		}
	}
	close(jobs)
	workers.Wait()
}
func isBatchHydrator(adapter Adapter) bool {
	_, ok := adapter.(interface{ BatchHydration() })
	return ok
}

func (l *Loader) hydrateBatch(ctx context.Context, a Adapter, source Source, generation uint64, jobs []hydrationJob, out chan<- Snapshot) {
	refs := make([]Ref, 0, len(jobs))
	for _, job := range jobs {
		refs = append(refs, job.ref)
	}
	outcomes := a.ReadItems(ctx, source, refs, l.Fields, l.Budget)
	byRef := make(map[string]ItemOutcome, len(outcomes))
	for _, outcome := range outcomes {
		byRef[outcome.Ref.Key()] = outcome
	}
	for _, job := range jobs {
		outcome, ok := byRef[job.ref.Key()]
		summary := job.item
		if !ok {
			summary.ReadOutcome = "failed"
			summary.Fresh = false
		}
		if outcome.Err != nil {
			summary.ReadOutcome = "failed"
			summary.Fresh = false
		}
		if outcome.Item == nil {
			if outcome.Err == nil && outcome.Kind != "" {
				summary.ReadOutcome = outcome.Kind
			}
			if summary.ReadOutcome == "withheld" && l.GitHubSync != nil {
				_ = l.GitHubSync.WithholdGitHubItem(ctx, source, job.ref)
			}
			l.publish(ctx, source.ID, generation, func(s *Snapshot) {
				if summary.ReadOutcome == "withheld" {
					delete(s.Items, job.ref.Key())
					return
				}
				summary.DependenciesKnown = false
				summary.Closure = CoverageUnknown
				summary.Readiness = Readiness{Status: ReadinessUnknown, Reasons: []string{"item-read-" + summary.ReadOutcome}, Freshness: FreshnessUnknown}
				s.Items[job.ref.Key()] = summary
			}, out)
			continue
		}
		item := cloneItem(*outcome.Item)
		if item.Ref.Key() != job.ref.Key() {
			continue
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
		complete := true
		withheld := false
		cursor := ""
		for {
			if ctx.Err() != nil {
				complete = false
				break
			}
			page, err := a.ReadDependencies(ctx, source, job.ref, cursor, l.Budget)
			if err != nil {
				if githubAccessLost(err) {
					if l.GitHubSync != nil {
						_ = l.GitHubSync.WithholdGitHubItem(ctx, source, job.ref)
					}
					l.publish(ctx, source.ID, generation, func(s *Snapshot) { delete(s.Items, job.ref.Key()) }, out)
					complete = false
					withheld = true
					break
				}
				complete = false
				break
			}
			if page.Completeness != CoverageComplete && page.NextCursor == "" {
				complete = false
			}
			item.Relationships = append(item.Relationships, page.Edges...)
			if page.Observation.ObservedAt.After(item.Observation.ObservedAt) {
				item.Observation = page.Observation
			}
			cursor = page.NextCursor
			if cursor == "" {
				break
			}
		}
		if withheld {
			continue
		}
		item.DependenciesKnown = complete
		item.Closure = CoverageUnknown
		if complete {
			item.Closure = CoverageComplete
		}
		item.Fresh = item.Fresh && summary.Fresh
		l.publish(ctx, source.ID, generation, func(s *Snapshot) { s.Items[job.ref.Key()] = item }, out)
	}
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
		if summary.ReadOutcome == "withheld" && l.GitHubSync != nil {
			_ = l.GitHubSync.WithholdGitHubItem(ctx, source, ref)
		}
		l.publish(ctx, source.ID, generation, func(s *Snapshot) {
			if summary.ReadOutcome == "withheld" {
				delete(s.Items, ref.Key())
				return
			}
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
			if githubAccessLost(err) {
				if l.GitHubSync != nil {
					_ = l.GitHubSync.WithholdGitHubItem(ctx, source, ref)
				}
				l.publish(ctx, source.ID, generation, func(s *Snapshot) { delete(s.Items, ref.Key()) }, out)
				return
			}
			allComplete = false
			break
		}
		if deps.Completeness != CoverageComplete {
			allComplete = false
		}
		if observationMismatch(item.Observation, deps.Observation) {
			mismatchGeneration := l.begin(source.ID)
			l.publish(ctx, source.ID, mismatchGeneration, func(s *Snapshot) {
				for key, existing := range s.Items {
					if existing.Ref.SourceID == source.ID {
						delete(s.Items, key)
					}
				}
				s.Sources[source.ID] = Coverage{State: CoverageUnknown, Reason: "principal-changed-during-hydration", TotalAccuracy: TotalUnknown}
			}, out)
			// Stop later pages and sibling hydration workers from republishing
			// observations from the invalidated principal generation.
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
func githubAccessLost(err error) bool {
	diagnostic, ok := err.(GitHubDiagnostic)
	if !ok {
		return false
	}
	switch diagnostic.Code {
	case "not-found-or-inaccessible", "permission-denied", "saml-sso", "authentication", "identity-changed":
		return true
	}
	return false
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
