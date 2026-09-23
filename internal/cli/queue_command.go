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
	"time"

	"github.com/brettinternet/worklease/internal/config"
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
	cacheDir, err := queueindex.CacheDir(os.Getenv, "")
	if err != nil {
		return err
	}
	index, err := queueindex.Open(ctx, cacheDir)
	if err != nil {
		return err
	}
	defer index.Close()
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
	var program *tea.Program
	var workers sync.WaitGroup
	var workersMu sync.Mutex
	closing := false
	start := func() {
		workersMu.Lock()
		if closing {
			workersMu.Unlock()
			return
		}
		workers.Add(1)
		workersMu.Unlock()
		authorityMu.Lock()
		selectedAuthority := authorityView
		authorityMu.Unlock()
		go func() {
			defer workers.Done()
			publishQueue(ctx, loader, sources, claims, selectedAuthority, paths, program, index, cachePartitions, &claimOverlay)
		}()
	}
	model.HydrateSelected = func(item queue.Item) tea.Cmd {
		return func() tea.Msg {
			if sourceByID[item.Ref.SourceID].Adapter != "backlog-md" {
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
				return nil
			}
			workersMu.Lock()
			if closing {
				workersMu.Unlock()
				return nil
			}
			workers.Add(1)
			workersMu.Unlock()
			go func() {
				defer workers.Done()
				for snapshot := range loader.HydrateEdges(ctx, source, []queue.Ref{item.Ref}, nil, false) {
					applyStoredClaims(&snapshot, &claimOverlay)
					program.Send(queueui.SnapshotMsg{Snapshot: snapshot})
				}
			}()
			return nil
		}
	}
	model.Refresh = func() tea.Cmd {
		return func() tea.Msg {
			start()
			return queueui.RefreshedMsg{}
		}
	}
	model.LoadHistory = func(item queue.Item, cursor string) tea.Cmd {
		return func() tea.Msg {
			page, err := backend.API.History(ctx, item.Resources[0], cursor, 20, false)
			return queueui.HistoryMsg{Identity: queueIdentity(item), Page: page, Err: err}
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
	start()
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
			authorityMu.Unlock()
			start()
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
				if watchErr := watcher.WatchChanges(ctx, source, start); watchErr != nil && ctx.Err() == nil {
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
	return err
}

// seedQueueIndex publishes the cached first frame before refresh starts.
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

func publishQueue(ctx context.Context, loader *queue.Loader, sources []queue.Source, claims map[string]queue.ClaimSource, selected queue.ClaimAuthority, paths config.ProfilePaths, program *tea.Program, index *queueindex.Index, partitions map[string]queueindex.Partition, stored *sync.Map) {
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
			program.Send(queueui.RefreshedMsg{Err: err})
			return
		}
		snapshot, observed, _, err := index.Read(ctx, partition, 0)
		if err != nil {
			release()
			for _, done := range releases {
				done()
			}
			program.Send(queueui.RefreshedMsg{Err: err})
			return
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
		loader.Store.SeedSnapshot(cached)
		program.Send(queueui.SnapshotMsg{Snapshot: loader.Store.Current()})
	}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	var latest queue.Snapshot
	for snapshot := range loader.Refresh(ctx, refreshSources) {
		latest = snapshot.Clone()
		items := make([]queue.Item, 0, len(snapshot.Items))
		for _, item := range snapshot.Items {
			items = append(items, item)
		}
		observed := queue.OverlayClaims(ctx, items, claims, selected, paths, os.Getenv)
		for _, item := range observed {
			snapshot.Items[item.Ref.Key()] = item
			stored.Store(item.Ref.Key(), item)
		}
		program.Send(queueui.SnapshotMsg{Snapshot: snapshot})
	}
	for _, source := range refreshSources {
		partition, ok := partitions[source.ID]
		if !ok {
			continue
		}
		if err := replaceQueueIndexSnapshot(ctx, index, partition, source.ID, latest); err != nil {
			program.Send(queueui.RefreshedMsg{Err: err})
		}
	}
	// First paint and index replacement have already completed. Slow per-task
	// Backlog views now hydrate visible rows, then the rest, without blocking UI.
	for _, source := range refreshSources {
		if source.Adapter != "backlog-md" || ctx.Err() != nil {
			continue
		}
		ordered := queue.EvaluateView(loader.Store.Current().Items, queue.View{SourceOrder: []string{source.ID}})
		visible := make([]queue.Ref, 0, min(35, len(ordered)))
		for _, item := range ordered[:min(35, len(ordered))] {
			visible = append(visible, item.Ref)
		}
		for snapshot := range loader.HydrateEdges(ctx, source, nil, visible, true) {
			applyStoredClaims(&snapshot, stored)
			program.Send(queueui.SnapshotMsg{Snapshot: snapshot})
		}
	}
}
