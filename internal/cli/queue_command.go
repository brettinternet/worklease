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
	c := &urfave.Command{Name: "queue", Aliases: []string{"q"}, Usage: "browse and claim configured work", UsageText: "worklease queue [--view NAME]", Description: "Browse configured source snapshots; Claim for me acquires a Worklease coordination lease without provider writes.\n\nExamples:\n  worklease queue\n  worklease q -v Ready", Flags: []urfave.Flag{&urfave.StringFlag{Name: "view", Aliases: []string{"v"}, Usage: "configured queue view `NAME`"}}}
	c.Action = func(ctx context.Context, cmd *urfave.Command) error {
		if s.jsonRequested(cmd) {
			return s.handle(cmd, reason.Invalid("queue TUI is text-only; use queue query --json when available"))
		}
		return s.handle(cmd, runQueue(ctx, cmd, s))
	}
	return c
}
func queueMeBySource(cfg config.QueueConfig, source config.QueueSource) []string {
	if source.Adapter == "external" || source.Adapter == "linear" {
		if source.Account != "" {
			return []string{source.Account}
		}
		return nil
	}
	identityKey := "backlog-md"
	if source.Adapter == "beads" {
		identityKey = "beads"
	}
	if source.Adapter == "github" {
		identityKey = source.Host
	}
	identity, ok := cfg.Me[identityKey]
	if !ok {
		return nil
	}
	if source.Adapter == "github" || source.Adapter == "beads" {
		var account string
		if identity.Decode(&account) == nil && account != "" {
			return []string{account}
		}
		return nil
	}
	var names []string
	_ = identity.Decode(&names)
	return names
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
	queueSession, err := config.QueueSessionID(os.Getenv)
	if err != nil {
		return err
	}
	backend, authorityView, err := queueAuthorityForClaim(ctx, cmd, selected.Authority)
	if err != nil {
		return err
	}
	defer backend.Close()
	registry := queue.NewRegistry()
	cleanupExternal, err := queue.RegisterExternalSources(registry, cfg.Sources, os.Getenv)
	if err != nil {
		return err
	}
	defer cleanupExternal()
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
		adapterKey := queueAdapterRegistryKey(src)
		adapter, ok := registry.Get(adapterKey)
		if !ok {
			shownSources = append(shownSources, queue.Source{ID: src.ID, Name: src.ID, Adapter: adapterKey})
			sourceErrors[src.ID] = "adapter-unavailable"
			continue
		}
		options := map[string]string{"id": src.ID}
		if src.Adapter != "external" {
			options = queueSourceOptions(src)
		}
		resolved, err := adapter.Resolve(ctx, options)
		if err != nil {
			shownSources = append(shownSources, queue.Source{ID: src.ID, Name: src.ID, Adapter: adapterKey})
			sourceErrors[src.ID] = queueSourceFailure(err)
			if src.Adapter == "external" {
				sourceErrors[src.ID] = err.Error() // ExternalProcess bounds and redacts stderr.
			}
			continue
		}
		resolved.ID, resolved.Adapter = src.ID, adapterKey
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
	loader.LinearSync = queueindex.LinearSyncStore{Index: index, Registry: registry}
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
	model.Views = append(model.Views, queueui.RecoveryViewID)
	journal, err := queueRecoveryJournal()
	if err != nil {
		return err
	}
	model.LoadRecovery = func() tea.Cmd {
		return func() tea.Msg {
			entries, err := journal.Recovery(time.Now())
			return queueui.RecoveryMsg{Entries: entries, Err: err}
		}
	}
	model.MeBySource = make(map[string][]string)
	for _, src := range selected.Sources {
		model.MeBySource[src] = queueMeBySource(cfg, sourceByID[src])
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
	if model.Me == "" {
		for _, id := range selected.Sources {
			if source := sourceByID[id]; source.Adapter == "external" && source.Account != "" {
				model.Me = source.Account
				break
			}
		}
	}
	paths := config.UserProfilePaths(os.Getenv)
	claimInputs := queue.ClaimSources(cfg, sources)
	var claimOverlay sync.Map // ref key -> most recently observed claim and key inputs
	var authorityMu sync.Mutex
	var authorityVersion uint64 // bumped when late admission metadata replaces authorityView
	currentAuthority := func() (queue.ClaimAuthority, uint64) {
		authorityMu.Lock()
		defer authorityMu.Unlock()
		return authorityView, authorityVersion
	}
	guardClaims := func(snapshot queue.Snapshot) map[string]queue.ClaimSource {
		state, err := config.LoadQueueIdentities(os.Getenv)
		if err != nil {
			blocked := make(map[string]queue.ClaimSource, len(claimInputs))
			for id, source := range claimInputs {
				source.BlockReason, source.BlockDetail = "identity-unknown", "private identity record unavailable"
				blocked[id] = source
			}
			return blocked
		}
		selected, _ := currentAuthority()
		return queue.GuardClaimSources(ctx, claimInputs, registry, selected, state, snapshot)
	}
	var program *tea.Program
	var claimController *queueClaimController
	var workers sync.WaitGroup
	var workersMu sync.Mutex
	var liveMu sync.Mutex
	var liveCancel context.CancelFunc
	var liveDone chan struct{}
	var liveKeys map[string]string
	var blockedIdentity atomic.Bool
	var hydrationCancel context.CancelFunc
	closing := false
	resolvedByID := make(map[string]queue.Source, len(sources))
	for _, source := range sources {
		resolvedByID[source.ID] = source
	}
	claimController = &queueClaimController{backend: backend, registry: registry, sources: resolvedByID, claimSources: claimInputs, queueSession: queueSession, paths: paths, current: currentAuthority, blocked: func() bool { return blockedIdentity.Load() }, profile: backend.Profile, profileName: selected.Authority, home: backend.Config.Home}
	writeController := queueWriteController{backend: backend, registry: registry, current: currentAuthority, journal: journal, sources: resolvedByID, configured: sourceByID, me: model.MeBySource, session: queueSession, profile: selected.Authority}
	model.StateChoices = make(map[string][]queueui.StateChoice)
	model.StartTransitions = make(map[string]string)
	for id, source := range sourceByID {
		if source.Adapter == "backlog-md" || source.Adapter == "linear" || source.Adapter == "github" && source.GitHubProject != nil && source.GitHubProject.AllowWrites {
			model.StartTransitions[id] = source.Workflow["start"]
		}
		for _, step := range []struct {
			name   string
			action queue.Action
		}{{"start", queue.ActionStart}, {"blocked", queue.ActionReportBlocked}, {"review", queue.ActionRequestReview}, {"complete", queue.ActionComplete}, {"reopen", queue.ActionReopen}} {
			if transition := source.Workflow[step.name]; transition != "" {
				if source.Adapter == "github" && step.action != queue.ActionComplete && step.action != queue.ActionReopen && (source.GitHubProject == nil || !source.GitHubProject.AllowWrites) {
					continue
				}
				model.StateChoices[id] = append(model.StateChoices[id], queueui.StateChoice{Action: step.action, Label: step.name, Transition: transition})
			}
		}
	}
	model.PreviewWrite = func(item queue.Item, action queue.Action, transition, text string) tea.Cmd {
		return writeController.Preview(ctx, item, action, transition, text)
	}
	model.ConfirmWrite = func(preview queueui.WritePreview) tea.Cmd { return writeController.Confirm(ctx, preview) }
	model.RetryRecovery = func(entry queue.RecoveryEntry) tea.Cmd { return writeController.Recover(ctx, entry) }
	model.ReconcileRecovery = func(entry queue.RecoveryEntry, evidence string) tea.Cmd {
		return writeController.Reconcile(ctx, entry, evidence)
	}
	model.AttestCheckpointMissing = func(entry queue.RecoveryEntry, evidence string) tea.Cmd {
		return writeController.AttestCheckpointMissing(ctx, entry, evidence)
	}
	model.PreviewLaunch = func(item queue.Item) []queue.LaunchOption {
		selected, _ := currentAuthority()
		identities, identityErr := config.LoadQueueIdentities(os.Getenv)
		options := make([]queue.LaunchOption, 0, len(cfg.Launch))
		for _, action := range cfg.Launch {
			if identityErr != nil {
				options = append(options, queue.LaunchOption{Name: action.Name, Authority: selected.ID, Eligibility: queue.Eligibility{Reasons: []string{"identity-unknown"}, Outcome: "capability"}})
				continue
			}
			option, _ := queueLaunchOption(action, item, sourceByID[item.Ref.SourceID], selected, queueSession, identities.Sources[item.Ref.SourceID], os.Environ())
			options = append(options, option)
		}
		return options
	}
	model.Launch = func(item queue.Item, name string) tea.Cmd {
		return func() tea.Msg {
			result := queueui.LaunchResultMsg{Name: name}
			if err := claimController.profileIdentityCurrent(); err != nil {
				result.Err = err
				return result
			}
			selected, _ := currentAuthority()
			// Re-read the current claim immediately before starting the child. A
			// release-then-launch sequence does not reserve the resource.
			fresh, err := refreshQueueActionClosure(ctx, registry, resolvedByID, item)
			if err != nil {
				result.Err = err
				return result
			}
			observed := queue.OverlayClaims(ctx, []queue.Item{fresh}, guardClaims(loader.Store.Current()), selected, paths, os.Getenv)
			if len(observed) != 1 {
				result.Err = fmt.Errorf("claim observation unavailable")
				return result
			}
			identities, err := config.LoadQueueIdentities(os.Getenv)
			if err != nil {
				result.Err = err
				return result
			}
			for _, action := range cfg.Launch {
				if action.Name != name {
					continue
				}
				previous := identities.Sources[item.Ref.SourceID]
				option, handoff := queueLaunchOption(action, observed[0], sourceByID[item.Ref.SourceID], selected, queueSession, previous, os.Environ())
				if !option.Eligibility.Eligible {
					result.Err = fmt.Errorf("%s", strings.Join(option.Eligibility.Reasons, "; "))
					return result
				}
				claimSource, ok := claimInputs[item.Ref.SourceID]
				if !ok {
					result.Err = fmt.Errorf("claim source unavailable")
					return result
				}
				adapter, ok := registry.Get(claimSource.Source.Adapter)
				if !ok {
					result.Err = fmt.Errorf("claim source adapter unavailable")
					return result
				}
				keys, err := preAcquireQueueIdentity(ctx, claimSource, adapter, selected, observed[0])
				if err != nil {
					result.Err = err
					return result
				}
				previewKeys, err := queue.LaunchResources(observed[0], previous, selected)
				if err != nil || !sameResourceSelection(keys, previewKeys) {
					result.Err = fmt.Errorf("launch resources changed; reopen the picker")
					return result
				}
				child, err := queue.StartLaunch(ctx, handoff)
				if err == nil {
					detachLaunch(child) // the queue neither waits for nor owns the worker
				}
				result.Err = err
				return result
			}
			result.Err = fmt.Errorf("launch action no longer configured")
			return result
		}
	}
	startController := queueStartController{claim: claimController, write: writeController}
	model.PreviewStart = func(item queue.Item) tea.Cmd { return startController.Preview(ctx, item) }
	model.StartWork = func(item queue.Item, preview queueui.StartPreview) tea.Cmd {
		return startController.Start(ctx, item, preview)
	}
	model.PreviewClaim = func(item queue.Item) tea.Cmd { return claimController.Preview(ctx, item) }
	model.AcquireClaim = func(item queue.Item, preview queueui.ClaimPreview) tea.Cmd {
		return claimController.AcquireClaim(ctx, item, preview)
	}
	lifecycle := queueLifecycle{controller: claimController, now: time.Now}
	model.CancelClaim = func(path string) tea.Cmd {
		return func() tea.Msg {
			cancelCtx, stop := context.WithTimeout(ctx, 10*time.Second)
			defer stop()
			return queueui.CancelClaimMsg{Path: path, Err: lifecycle.Cancel(cancelCtx, path)}
		}
	}
	restartOverlay := func(base queue.Snapshot, claims map[string]queue.ClaimSource) {
		liveMu.Lock()
		defer liveMu.Unlock()
		if blockedIdentity.Load() {
			return
		}
		if claimController != nil {
			if identityErr := claimController.profileIdentityCurrent(); identityErr != nil {
				blockedIdentity.Store(true)
				stopped := base.Clone()
				for key, item := range stopped.Items {
					item.Claim = queue.ClaimObservation{AuthorityID: authorityView.ID, State: "unknown", NativeState: "not-exposed", Stale: true, Reason: "authority-mismatch", Detail: identityErr.Error(), ObservedAt: time.Now().UTC()}
					stopped.Items[key] = item
				}
				program.Send(queueui.ClaimOverlayMsg{Snapshot: stopped, Err: identityErr})
				return
			}
		}
		authorityMu.Lock()
		view := authorityView
		authorityMu.Unlock()
		keys := make(map[string]string, len(base.Items)+1)
		for key, item := range base.Items {
			keys[key] = strings.Join(item.Resources, "\x00") + "\x00" + item.Claim.Reason + "\x00" + item.Claim.Detail
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
			err := publishQueue(ctx, loader, sources, guardClaims, currentAuthority, paths, model, program, index, cachePartitions, &claimOverlay, restartOverlay)
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
			if adapter != "backlog-md" && adapter != "github" && adapter != "linear" {
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
				} else if adapter == "beads" {
					updates = loader.HydrateDetail(hydrationCtx, source, item.Ref)
				} else {
					updates = loader.HydrateEdges(hydrationCtx, source, []queue.Ref{item.Ref}, nil, false)
				}
				for snapshot := range updates {
					applyStoredClaims(&snapshot, &claimOverlay)
					program.Send(queueui.PrepareSnapshotForModel(snapshot, model))
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
	// Owned handles and recovery entries are known before the first keypress,
	// so an immediate quit still shows their exit consequences.
	ownedPaths, err := lifecycle.paths()
	if err != nil {
		return fmt.Errorf("queue-owned claim handles unavailable: %w", err)
	}
	for _, path := range ownedPaths {
		model.OwnedClaims[path] = queueui.OwnedClaimMsg{Path: path, LastResult: "verification pending"}
	}
	if entries, recoveryErr := journal.Recovery(time.Now()); recoveryErr != nil {
		model.RecoveryError = recoveryErr.Error()
	} else {
		model.Recovery = entries
	}
	// The model is handed to Bubble Tea before background producers start.
	program = tea.NewProgram(model, tea.WithOutput(s.writer), tea.WithContext(ctx))
	workers.Add(1)
	go func() {
		defer workers.Done()
		lifecycle.run(ctx, func(msg queueui.OwnedClaimMsg) { program.Send(msg) })
	}()
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
	if diagnostic, ok := err.(queue.BeadsDiagnostic); ok {
		return diagnostic.Code
	}
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

func publishQueue(ctx context.Context, loader *queue.Loader, sources []queue.Source, guard func(queue.Snapshot) map[string]queue.ClaimSource, authority func() (queue.ClaimAuthority, uint64), paths config.ProfilePaths, model queueui.Model, program *tea.Program, index *queueindex.Index, partitions map[string]queueindex.Partition, stored *sync.Map, onSnapshot func(queue.Snapshot, map[string]queue.ClaimSource)) error {
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
	claims := guard(cached)
	if len(cached.Items) > 0 {
		overlayCachedClaims(ctx, &cached, claims, authority, paths, stored)
		loader.Store.SeedSnapshot(cached)
		seeded := loader.Store.Current()
		program.Send(queueui.PrepareSnapshotForModel(seeded, model))
		onSnapshot(seeded, claims)
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
		program.Send(queueui.PrepareSnapshotForModel(snapshot, model))
		onSnapshot(snapshot, claims)
	}
	claims = guard(latest)
	if len(latest.Items) > 0 {
		items := make([]queue.Item, 0, len(latest.Items))
		for _, item := range latest.Items {
			items = append(items, item)
		}
		for _, item := range overlayCurrentClaims(ctx, items, claims, authority, paths) {
			latest.Items[item.Ref.Key()] = item
			stored.Store(item.Ref.Key(), item)
		}
		program.Send(queueui.PrepareSnapshotForModel(latest, model))
		onSnapshot(latest, claims)
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
		case "backlog-md", "beads":
			updates = loader.HydrateEdges(ctx, source, nil, visible, true)
		case "linear":
			updates = loader.HydrateEdges(ctx, source, visible, nil, false)
		default:
			continue
		}
		for snapshot := range updates {
			applyStoredClaims(&snapshot, stored)
			program.Send(queueui.PrepareSnapshotForModel(snapshot, model))
			onSnapshot(snapshot, claims)
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
