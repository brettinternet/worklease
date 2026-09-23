package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
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
	backend, authorityView, err := queueAuthorityForView(ctx, cmd, selected.Authority)
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
	var program *tea.Program
	var workers sync.WaitGroup
	var workersMu sync.Mutex
	var liveMu sync.Mutex
	var liveCancel context.CancelFunc
	var liveDone chan struct{}
	var liveKeys map[string]bool
	var blockedIdentity atomic.Bool
	closing := false
	restartOverlay := func(base queue.Snapshot) {
		liveMu.Lock()
		defer liveMu.Unlock()
		if blockedIdentity.Load() {
			return
		}
		keys := make(map[string]bool, len(base.Items))
		for key := range base.Items {
			keys[key] = true
		}
		if len(keys) == len(liveKeys) && liveKeys != nil {
			same := true
			for key := range keys {
				if !liveKeys[key] {
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
			_ = queue.RunClaimOverlay(liveCtx, items, claims, authorityView, paths, os.Getenv, func(observed []queue.Item, rebuilding bool, err error) {
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
	start := func() {
		workersMu.Lock()
		if closing {
			workersMu.Unlock()
			return
		}
		workers.Add(1)
		workersMu.Unlock()
		go func() {
			defer workers.Done()
			publishQueue(ctx, loader, sources, claims, authorityView, paths, program, func(snapshot queue.Snapshot) {
				if len(snapshot.Items) > 0 {
					restartOverlay(snapshot)
				}
			})
		}()
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
func publishQueue(ctx context.Context, loader *queue.Loader, sources []queue.Source, claims map[string]queue.ClaimSource, selected queue.ClaimAuthority, paths config.ProfilePaths, program *tea.Program, onSnapshot func(queue.Snapshot)) {
	for snapshot := range loader.Refresh(ctx, sources) {
		items := make([]queue.Item, 0, len(snapshot.Items))
		for _, item := range snapshot.Items {
			items = append(items, item)
		}
		observed := queue.OverlayClaims(ctx, items, claims, selected, paths, os.Getenv)
		for _, item := range observed {
			snapshot.Items[item.Ref.Key()] = item
		}
		program.Send(queueui.SnapshotMsg{Snapshot: snapshot})
		onSnapshot(snapshot)
	}
}
