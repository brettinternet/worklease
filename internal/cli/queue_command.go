package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueindex"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/reason"
	tea "github.com/charmbracelet/bubbletea"
	urfave "github.com/urfave/cli/v3"
)

func queueCommand(s *boundary) *urfave.Command {
	c := &urfave.Command{Name: "queue", Usage: "browse configured work in a read-only terminal", UsageText: "worklease queue [--view NAME]", Description: "Browse configured source snapshots without provider writes or claims.\n\nExamples:\n  worklease queue\n  worklease queue --view Ready", Flags: []urfave.Flag{&urfave.StringFlag{Name: "view", Usage: "configured queue view `NAME`"}}}
	c.Action = func(ctx context.Context, cmd *urfave.Command) error {
		if s.jsonRequested(cmd) {
			return s.handle(cmd, reason.Invalid("queue TUI is text-only; use queue query --json when available"))
		}
		return s.handle(cmd, runQueue(ctx, cmd, s))
	}
	return c
}
func runQueue(ctx context.Context, cmd *urfave.Command, s *boundary) error {
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil {
		return err
	}
	if len(cfg.Views) == 0 {
		return reason.Invalid("queue.yaml has no views")
	}
	selected := cfg.Views[0]
	if name := cmd.String("view"); name != "" {
		found := false
		for _, v := range cfg.Views {
			if v.Name == name {
				selected = v
				found = true
				break
			}
		}
		if !found {
			return reason.Invalid("unknown queue view: " + name)
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	backend, authorityView, err := queueAuthorityForViewWithMetadata(ctx, cmd, selected.Authority, false)
	if err != nil {
		return err
	}
	defer backend.Close()
	registry := queue.NewRegistry()
	sources := make([]queue.Source, 0, len(selected.Sources))
	shownSources := make([]queue.Source, 0, len(selected.Sources))
	sourceErrors := make(map[string]string)
	sourceByID := map[string]config.QueueSource{}
	for _, src := range cfg.Sources {
		sourceByID[src.ID] = src
	}
	for _, id := range selected.Sources {
		src, ok := sourceByID[id]
		if !ok {
			return reason.Invalid("unknown queue source: " + id)
		}
		adapter, ok := registry.Get(src.Adapter)
		if !ok {
			return reason.Invalid("unknown adapter: " + src.Adapter)
		}
		options := map[string]string{"id": src.ID, "checkout": src.Checkout, "host": src.Host, "repository": src.Repository, "account": src.Account}
		if src.AllowGitNetwork {
			options["allowGitNetwork"] = "true"
		}
		resolved, err := adapter.Resolve(ctx, options)
		if err != nil {
			shownSources = append(shownSources, queue.Source{ID: src.ID, Name: src.ID, Adapter: src.Adapter})
			sourceErrors[src.ID] = queueSourceFailure(err)
			continue
		}
		sources = append(sources, resolved)
		shownSources = append(shownSources, resolved)
	}
	loader := queue.NewLoader(registry)
	loader.DeferDetails = true
	cacheDir, err := queueindex.CacheDir(os.Getenv, "")
	if err != nil {
		return err
	}
	index, err := queueindex.Open(ctx, cacheDir)
	if err != nil {
		return err
	}
	defer index.Close()
	loader.GitHubSync = queueindex.GitHubSyncStore{Index: index, Registry: registry}
	cachePartitions, err := seedQueueIndex(ctx, index, registry, sources, loader)
	if err != nil {
		return err
	}
	model := queueui.New(loader.Store.Current())
	model.Sources = shownSources
	model.SourceErrors = sourceErrors
	model.ViewName = selected.Name
	model.Views = nil
	model.ViewFilters = make(map[string]queue.Filters)
	model.ViewRules = make(map[string]queueui.ViewRule)
	for _, v := range cfg.Views {
		if v.Authority != selected.Authority || !sourceSubset(v.Sources, selected.Sources) {
			continue
		}
		model.Views = append(model.Views, v.Name)
		filters := queue.Filters{SourceIDs: append([]string(nil), v.Sources...)}
		model.ViewFilters[v.Name] = filters
		model.ViewRules[v.Name] = queueui.ViewRule{Readiness: v.Filter.Readiness, Claim: v.Filter.Claim, Assigned: v.Filter.Assigned}
	}
	model.MeBySource = make(map[string][]string)
	for _, src := range selected.Sources {
		configured := sourceByID[src]
		identityKey := "backlog-md"
		if configured.Adapter == "github" {
			identityKey = configured.Host
		}
		if identity, ok := cfg.Me[identityKey]; ok {
			var names []string
			if configured.Adapter == "github" {
				var name string
				if identity.Decode(&name) == nil && name != "" {
					names = []string{name}
				}
			} else {
				_ = identity.Decode(&names)
			}
			model.MeBySource[src] = names
		}
	}
	model.Authority = authorityView.Profile
	model.Scope = "local"
	if authorityView.Remote {
		model.Scope = "remote"
	}
	model.Authority = fmt.Sprintf("%s %s", authorityView.Profile, authorityView.ID)
	if me, ok := cfg.Me["backlog-md"]; ok {
		var names []string
		if me.Decode(&names) == nil && len(names) > 0 {
			model.Me = names[0]
		}
	}
	if model.Me == "" {
		for _, v := range cfg.Me {
			model.Me = v.Value
			break
		}
	}
	paths := config.UserProfilePaths(os.Getenv)
	claims := queue.ClaimSources(cfg, sources)
	var claimOverlay sync.Map // ref key -> most recently observed claim and key inputs
	var authorityMu sync.Mutex
	var authorityVersion uint64 // bumped when late admission metadata replaces authorityView
	currentAuthority := func() (queue.ClaimAuthority, uint64) {
		authorityMu.Lock()
		defer authorityMu.Unlock()
		return authorityView, authorityVersion
	}
	var program *tea.Program
	var workers sync.WaitGroup
	var workersMu sync.Mutex
	var liveMu sync.Mutex
	var liveCancel context.CancelFunc
	var liveDone chan struct{}
	var liveKeys map[string]string
	var blockedIdentity atomic.Bool
	var hydrationCancel context.CancelFunc
	closing := false
	restartOverlay := func(base queue.Snapshot) {
		liveMu.Lock()
		defer liveMu.Unlock()
		if blockedIdentity.Load() {
			return
		}
		authorityMu.Lock()
		view := authorityView
		authorityMu.Unlock()
		keys := make(map[string]string, len(base.Items)+1)
		for key, item := range base.Items {
			keys[key] = strings.Join(item.Resources, "\x00")
		}
		// Late admission metadata changes action eligibility, so it restarts the overlay.
		keys["\x00admitted"] = "unknown"
		if view.AdmittedPrefixes != nil {
			keys["\x00admitted"] = "known\x00" + strings.Join(*view.AdmittedPrefixes, "\x00")
		}
		if len(keys) == len(liveKeys) && liveKeys != nil {
			same := true
			for key, resources := range keys {
				if liveKeys[key] != resources {
					same = false
					break
				}
			}
			if same {
				return
			}
		}
		liveKeys = keys
		if liveCancel != nil {
			liveCancel()
			<-liveDone
		}
		if ctx.Err() != nil {
			return
		}
		liveCtx, stop := context.WithCancel(ctx)
		liveCancel = stop
		liveDone = make(chan struct{})
		go func(done chan struct{}) {
			defer close(done)
			items := make([]queue.Item, 0, len(base.Items))
			for _, item := range base.Items {
				items = append(items, item)
			}
			_ = queue.RunClaimOverlay(liveCtx, items, claims, view, paths, os.Getenv, func(observed []queue.Item, rebuilding bool, err error) {
				updated := base.Clone()
				for _, item := range observed {
					updated.Items[item.Ref.Key()] = item
					if err != nil && item.Claim.Reason == "authority-mismatch" {
						blockedIdentity.Store(true)
					}
				}
				program.Send(queueui.ClaimOverlayMsg{Snapshot: updated, Rebuilding: rebuilding, Err: err})
			})
		}(liveDone)
	}
	start := func(notifyFailure bool) <-chan error {
		done := make(chan error, 1)
		workersMu.Lock()
		if closing {
			workersMu.Unlock()
			done <- context.Canceled
			close(done)
			return done
		}
		workers.Add(1)
		workersMu.Unlock()
		go func() {
			defer workers.Done()
			err := publishQueue(ctx, loader, sources, claims, currentAuthority, paths, program, index, cachePartitions, &claimOverlay, restartOverlay)
			if notifyFailure && err != nil && ctx.Err() == nil {
				program.Send(queueui.RefreshedMsg{Err: err})
			}
			done <- err
			close(done)
		}()
		return done
	}
	model.HydrateSelected = func(item queue.Item) tea.Cmd {
		// Supersede on selection (Update runs synchronously), not when the command
		// executes: Bubble Tea may run an older selection's command after a newer one.
		workersMu.Lock()
		if hydrationCancel != nil {
			hydrationCancel()
			hydrationCancel = nil
		}
		if closing {
			workersMu.Unlock()
			return nil
		}
		hydrationCtx, cancel := context.WithCancel(ctx)
		hydrationCancel = cancel
		workersMu.Unlock()
		return func() tea.Msg {
			adapter := sourceByID[item.Ref.SourceID].Adapter
			if adapter != "backlog-md" && adapter != "github" {
				cancel()
				return nil
			}
			var source queue.Source
			for _, candidate := range sources {
				if candidate.ID == item.Ref.SourceID {
					source = candidate
					break
				}
			}
			if source.ID == "" {
				cancel()
				return nil
			}
			workersMu.Lock()
			if closing || hydrationCtx.Err() != nil {
				workersMu.Unlock()
				cancel()
				return nil
			}
			workers.Add(1)
			workersMu.Unlock()
			go func() {
				defer workers.Done()
				defer cancel()
				var updates <-chan queue.Snapshot
				if adapter == "github" {
					updates = loader.HydrateVisible(hydrationCtx, source, []queue.Ref{item.Ref})
				} else {
					updates = loader.HydrateEdges(hydrationCtx, source, []queue.Ref{item.Ref}, nil, false)
				}
				for snapshot := range updates {
					applyStoredClaims(&snapshot, &claimOverlay)
					program.Send(queueui.SnapshotMsg{Snapshot: snapshot})
				}
			}()
			return nil
		}
	}
	model.Refresh = func() tea.Cmd {
		return refreshCompletionCmd(func() <-chan error { return start(false) })
	}
	model.LoadComments = func(item queue.Item, cursor string) tea.Cmd {
		return func() tea.Msg {
			if sourceByID[item.Ref.SourceID].Adapter != "github" {
				return queueui.CommentsMsg{Identity: queueIdentity(item), Err: fmt.Errorf("comments not available for this source")}
			}
			adapter, ok := registry.Get("github")
			if !ok {
				return queueui.CommentsMsg{Identity: queueIdentity(item), Err: fmt.Errorf("GitHub adapter unavailable")}
			}
			for _, source := range sources {
				if source.ID == item.Ref.SourceID {
					comments, next, err := adapter.(*queue.GitHubAdapter).ReadComments(ctx, source, item.Ref, cursor, 100)
					return queueui.CommentsMsg{Identity: queueIdentity(item), Comments: comments, Cursor: next, Err: err}
				}
			}
			return queueui.CommentsMsg{Identity: queueIdentity(item), Err: fmt.Errorf("GitHub source unavailable")}
		}
	}
	model.LoadHistory = func(item queue.Item, cursor string, before bool) tea.Cmd {
		return func() tea.Msg {
			var page ledger.HistoryPage
			var err error
			if before {
				page, err = backend.API.HistoryBefore(ctx, item.Resources[0], cursor, 20, false)
			} else {
				page, err = backend.API.History(ctx, item.Resources[0], cursor, 20, false)
			}
			return queueui.HistoryMsg{Identity: queueIdentity(item), Page: page, Before: before, Err: err}
		}
	}
	model.OpenURL = func(item queue.Item) tea.Cmd {
		return func() tea.Msg {
			src := sourceByID[item.Ref.SourceID]
			if src.Adapter != "github" {
				return queueui.RefreshedMsg{Err: fmt.Errorf("provider URL not available for %s", src.Adapter)}
			}
			if _, err := strconv.Atoi(item.Ref.ItemID); err != nil {
				return queueui.RefreshedMsg{Err: fmt.Errorf("invalid issue number")}
			}
			url := "https://" + src.Host + "/" + src.Repository + "/issues/" + item.Ref.ItemID
			if !strings.HasPrefix(url, "https://") {
				return queueui.RefreshedMsg{Err: fmt.Errorf("invalid provider URL")}
			}
			binary := "open"
			if runtime.GOOS == "linux" {
				binary = "xdg-open"
			}
			return queueui.RefreshedMsg{Err: exec.CommandContext(ctx, binary, url).Run()}
		}
	}
	// The model is handed to Bubble Tea before background producers start.
	program = tea.NewProgram(model, tea.WithOutput(s.writer), tea.WithContext(ctx))
	start(true)
	if backend.HTTP != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			response, metadataErr := backend.HTTP.Metadata(ctx)
			if metadataErr != nil || response.Metadata == nil || ctx.Err() != nil {
				return
			}
			authorityMu.Lock()
			authorityView.AdmittedPrefixes = response.Metadata.AdmittedPrefixes
			authorityVersion++
			authorityMu.Unlock()
			start(true)
		}()
	}
	for _, source := range sources {
		adapter, ok := registry.Get(source.Adapter)
		watcher, okWatch := adapter.(interface {
			WatchChanges(context.Context, queue.Source, func()) error
		})
		if !ok || !okWatch {
			continue
		}
		workers.Add(1)
		go func(source queue.Source) {
			defer workers.Done()
			for ctx.Err() == nil {
				if watchErr := watcher.WatchChanges(ctx, source, func() { start(true) }); watchErr != nil && ctx.Err() == nil {
					program.Send(queueui.RefreshedMsg{Err: watchErr})
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(10 * time.Second): // failed watch: retry after a bounded backoff
				}
			}
		}(source)
	}
	_, err = program.Run()
	workersMu.Lock()
	closing = true
	cancel()
	workersMu.Unlock()
	workers.Wait()
	liveMu.Lock()
	if liveCancel != nil {
		liveCancel()
		<-liveDone
	}
	liveMu.Unlock()
	return err
}

// seedQueueIndex publishes the cached first frame before refresh starts.
func refreshCompletionCmd(start func() <-chan error) tea.Cmd {
	return func() tea.Msg {
		return queueui.RefreshedMsg{Err: <-start()}
	}
}

func replaceQueueIndexSnapshot(ctx context.Context, index *queueindex.Index, partition queueindex.Partition, sourceID string, snapshot queue.Snapshot) error {
	items := make([]queue.Item, 0)
	for _, item := range snapshot.Items {
		if item.Ref.SourceID == sourceID {
			items = append(items, item)
		}
	}
	deleted := make([]queue.Ref, 0)
	for _, ref := range snapshot.Deleted {
		if ref.SourceID == sourceID {
			deleted = append(deleted, ref)
		}
	}
	return index.ReplaceWithDeletes(ctx, partition, items, deleted, snapshot.Sources[sourceID].State == queue.CoverageComplete)
}

func seedQueueIndex(ctx context.Context, index *queueindex.Index, registry *queue.Registry, sources []queue.Source, loader *queue.Loader) (map[string]queueindex.Partition, error) {
	partitions := make(map[string]queueindex.Partition)
	cached := queue.Snapshot{Items: map[string]queue.Item{}, Sources: map[string]queue.Coverage{}}
	for _, source := range sources {
		adapter, _ := registry.Get(source.Adapter)
		partition, ok := queueindex.ForSource(adapter, source)
		if !ok {
			continue
		}
		partitions[source.ID] = partition
		snapshot, _, _, err := index.Read(ctx, partition, -1)
		if err != nil {
			return nil, err
		}
		for key, item := range snapshot.Items {
			cached.Items[key] = item
		}
		for id, coverage := range snapshot.Sources {
			cached.Sources[id] = coverage
		}
	}
	loader.Store.SeedSnapshot(cached)
	return partitions, nil
}

func queueSourceFailure(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "rate"):
		return "rate-limited"
	case strings.Contains(message, "permission"), strings.Contains(message, "denied"), strings.Contains(message, "auth"):
		return "permission denied"
	default:
		return "offline/unavailable"
	}
}
func sourceSubset(wanted, available []string) bool {
	for _, id := range wanted {
		found := false
		for _, other := range available {
			if id == other {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func queueIdentity(item queue.Item) string {
	if item.CanonicalID != "" {
		return item.CanonicalID
	}
	return item.Ref.Key()
}
func applyStoredClaims(snapshot *queue.Snapshot, stored *sync.Map) {
	for key, item := range snapshot.Items {
		if value, ok := stored.Load(key); ok {
			prior := value.(queue.Item)
			item.Claim, item.Resources, item.KeyInputs, item.NativeClaim = prior.Claim, prior.Resources, prior.KeyInputs, prior.NativeClaim
			snapshot.Items[key] = item
		}
	}
}

// overlayCurrentClaims recomputes the overlay if admission metadata changed
// while it ran, so a frame never publishes claims from a superseded authority view.
func overlayCurrentClaims(ctx context.Context, items []queue.Item, claims map[string]queue.ClaimSource, authority func() (queue.ClaimAuthority, uint64), paths config.ProfilePaths) []queue.Item {
	for {
		selected, version := authority()
		observed := queue.OverlayClaims(ctx, items, claims, selected, paths, os.Getenv)
		if _, current := authority(); current == version || ctx.Err() != nil {
			return observed
		}
	}
}

func overlayCachedClaims(ctx context.Context, cached *queue.Snapshot, claims map[string]queue.ClaimSource, authority func() (queue.ClaimAuthority, uint64), paths config.ProfilePaths, stored *sync.Map) {
	items := make([]queue.Item, 0, len(cached.Items))
	for _, item := range cached.Items {
		items = append(items, item)
	}
	for _, item := range overlayCurrentClaims(ctx, items, claims, authority, paths) {
		cached.Items[item.Ref.Key()] = item
		stored.Store(item.Ref.Key(), item)
	}
}

func publishQueue(ctx context.Context, loader *queue.Loader, sources []queue.Source, claims map[string]queue.ClaimSource, authority func() (queue.ClaimAuthority, uint64), paths config.ProfilePaths, program *tea.Program, index *queueindex.Index, partitions map[string]queueindex.Partition, stored *sync.Map, onSnapshot func(queue.Snapshot)) error {
	refreshStarted := time.Now()
	stored.Range(func(key, _ any) bool { stored.Delete(key); return true }) // rebind after source/config refresh
	var releases []func()
	refreshSources := make([]queue.Source, 0, len(sources))
	cached := queue.Snapshot{Items: map[string]queue.Item{}, Sources: map[string]queue.Coverage{}}
	lockOrder := append([]queue.Source(nil), sources...)
	sort.Slice(lockOrder, func(i, j int) bool { return lockOrder[i].ID < lockOrder[j].ID })
	for _, source := range lockOrder {
		partition, cacheable := partitions[source.ID]
		if !cacheable {
			refreshSources = append(refreshSources, source)
			continue
		}
		release, err := index.WaitRefreshLock(ctx, partition)
		if err != nil {
			for _, done := range releases {
				done()
			}
			return err
		}
		snapshot, observed, _, err := index.Read(ctx, partition, 0)
		if err != nil {
			release()
			for _, done := range releases {
				done()
			}
			return err
		}
		if snapshot.Sources[source.ID].State == queue.CoverageComplete && !observed.Before(refreshStarted) {
			release()
			for key, item := range snapshot.Items {
				cached.Items[key] = item
			}
			for id, coverage := range snapshot.Sources {
				cached.Sources[id] = coverage
			}
			continue
		}
		releases = append(releases, release)
		refreshSources = append(refreshSources, source)
	}
	if len(cached.Items) > 0 {
		overlayCachedClaims(ctx, &cached, claims, authority, paths, stored)
		loader.Store.SeedSnapshot(cached)
		seeded := loader.Store.Current()
		program.Send(queueui.SnapshotMsg{Snapshot: seeded})
		onSnapshot(seeded)
	}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	var latest queue.Snapshot
	var refreshErr error
	for snapshot := range loader.Refresh(ctx, refreshSources) {
		latest = snapshot.Clone()
		items := make([]queue.Item, 0, len(snapshot.Items))
		for _, item := range snapshot.Items {
			items = append(items, item)
		}
		observed := overlayCurrentClaims(ctx, items, claims, authority, paths)
		for _, item := range observed {
			snapshot.Items[item.Ref.Key()] = item
			stored.Store(item.Ref.Key(), item)
		}
		program.Send(queueui.SnapshotMsg{Snapshot: snapshot})
		onSnapshot(snapshot)
	}
	for _, source := range refreshSources {
		partition, ok := partitions[source.ID]
		if !ok {
			continue
		}
		if err := replaceQueueIndexSnapshot(ctx, index, partition, source.ID, latest); err != nil && refreshErr == nil {
			refreshErr = err
		}
	}
	// First paint and index replacement have already completed. Hydrate only
	// visible GitHub rows in batches; Backlog views run visible-first, then
	// background, without blocking the initial UI.
	for _, source := range refreshSources {
		if ctx.Err() != nil {
			continue
		}
		ordered := queue.EvaluateView(loader.Store.Current().Items, queue.View{SourceOrder: []string{source.ID}})
		visible := make([]queue.Ref, 0, min(35, len(ordered)))
		for _, item := range ordered[:min(35, len(ordered))] {
			visible = append(visible, item.Ref)
		}
		var updates <-chan queue.Snapshot
		switch source.Adapter {
		case "github":
			updates = loader.HydrateVisible(ctx, source, visible)
		case "backlog-md":
			updates = loader.HydrateEdges(ctx, source, nil, visible, true)
		default:
			continue
		}
		for snapshot := range updates {
			applyStoredClaims(&snapshot, stored)
			program.Send(queueui.SnapshotMsg{Snapshot: snapshot})
			onSnapshot(snapshot)
		}
	}
	current := loader.Store.Current()
	for _, source := range refreshSources {
		coverage := current.Sources[source.ID]
		if coverage.State == queue.CoverageUnknown && coverage.Reason != "" && refreshErr == nil {
			refreshErr = fmt.Errorf("%s: %s", source.ID, coverage.Reason)
		}
	}
	if refreshErr != nil {
		return refreshErr
	}
	return ctx.Err()
}
