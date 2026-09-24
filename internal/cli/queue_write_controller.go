package cli

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
	tea "github.com/charmbracelet/bubbletea"
)

type queueWriteController struct {
	backend          *authorityContext
	registry         *queue.Registry
	current          func() (queue.ClaimAuthority, uint64)
	journal          queue.WriteJournal
	sources          map[string]queue.Source
	configured       map[string]config.QueueSource
	me               map[string][]string
	session, profile string
	// handlePath is the worker's acquired CLI or MCP handle for queue next --start.
	// Interactive queue writes continue to use their item-scoped handle.
	handlePath string
}

func (c queueWriteController) adapter(source queue.Source) (queue.WriteAdapter, error) {
	read, ok := c.registry.Get(source.Adapter)
	if !ok {
		return nil, fmt.Errorf("write adapter unavailable")
	}
	switch a := read.(type) {
	case *queue.BacklogAdapter:
		me := c.me[source.ID]
		if len(me) == 0 {
			return nil, fmt.Errorf("backlog.md identity not configured")
		}
		return &queue.BacklogWriteAdapter{BacklogAdapter: a, Me: me[0]}, nil
	case *queue.GitHubAdapter:
		return queue.NewGitHubWriteAdapter(a, true), nil
	default:
		return nil, fmt.Errorf("source does not support writes")
	}
}

func (c queueWriteController) prepare(ctx context.Context, item queue.Item, action queue.Action, transition, text, operationID, checkpointRef string) (queueui.WritePreview, string, error) {
	source, ok := c.sources[item.Ref.SourceID]
	if !ok {
		return queueui.WritePreview{}, "", fmt.Errorf("source unavailable")
	}
	// An old live handle cannot authorize writes under a changed claim binding.
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil {
		return queueui.WritePreview{}, "", err
	}
	configured := c.configured[source.ID]
	if action == queue.ActionStart && source.Adapter == "backlog-md" {
		var liveActor []string
		if entry, ok := cfg.Me["backlog-md"]; ok {
			if err := entry.Decode(&liveActor); err != nil {
				return queueui.WritePreview{}, "", err
			}
		}
		if !slices.Equal(liveActor, c.me[source.ID]) {
			return queueui.WritePreview{}, "", fmt.Errorf("provider actor changed; reopen Start work before writing")
		}
	}
	found := false
	for _, current := range cfg.Sources {
		if current.ID != source.ID {
			continue
		}
		found = true
		if current.Adapter != configured.Adapter || current.Checkout != configured.Checkout || current.Repository != configured.Repository || current.Host != configured.Host || current.Account != configured.Account || action == queue.ActionStart && current.Workflow["start"] != configured.Workflow["start"] {
			return queueui.WritePreview{}, "", fmt.Errorf("queue source binding changed; confirm migration before writing")
		}
	}
	if !found {
		return queueui.WritePreview{}, "", fmt.Errorf("queue source no longer configured")
	}
	claimSource, ok := queue.ClaimSources(cfg, []queue.Source{source})[source.ID]
	if !ok {
		return queueui.WritePreview{}, "", fmt.Errorf("queue claim source unavailable")
	}
	identities, err := config.LoadQueueIdentities(os.Getenv)
	if err != nil {
		return queueui.WritePreview{}, "", err
	}
	if c.current == nil {
		return queueui.WritePreview{}, "", fmt.Errorf("claim authority unavailable")
	}
	authority, _ := c.current()
	read, ok := c.registry.Get(source.Adapter)
	if !ok || authority.ID != c.backend.AuthorityID() || authority.API == nil {
		return queueui.WritePreview{}, "", fmt.Errorf("claim authority or source unavailable")
	}
	keys, err := queue.PreAcquireIdentity(ctx, claimSource, read, authority, identities.Sources[source.ID], item)
	if err != nil {
		return queueui.WritePreview{}, "", err
	}
	path := c.handlePath
	if path == "" {
		path, err = queueClaimHandlePath(c.backend.Config.Home, c.session, c.profile, source, item.Ref)
		if err != nil {
			return queueui.WritePreview{}, "", err
		}
	}
	h, err := handle.Read(path)
	if err != nil {
		return queueui.WritePreview{}, "", fmt.Errorf("claim for selected item unavailable: %w", err)
	}
	if !slices.Equal(keys, h.Resources) {
		return queueui.WritePreview{}, "", fmt.Errorf("claim resources differ from confirmed source identity")
	}
	principal := c.configured[source.ID].Account
	if source.Adapter == "backlog-md" {
		if names := c.me[source.ID]; len(names) > 0 {
			principal = names[0]
		}
	}
	if principal == "" {
		return queueui.WritePreview{}, "", fmt.Errorf("provider identity not configured")
	}
	if operationID == "" {
		operationID, err = queue.NewWriteOperationID()
		if err != nil {
			return queueui.WritePreview{}, "", err
		}
	}
	if checkpointRef == "" {
		checkpointRef, err = queue.NewWriteOperationID()
		if err != nil {
			return queueui.WritePreview{}, "", err
		}
	}
	intent := queue.WriteIntent{OperationID: operationID, OperationRef: checkpointRef, Source: source, Ref: item.Ref, Principal: principal, AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, ClaimRevision: h.Revision, Resources: append([]string(nil), h.Resources...), CheckpointTTL: c.backend.Config.TTL, CheckpointNotAfter: time.Now().Add(time.Hour), Action: action, Transition: transition, Append: text}
	if intent.CheckpointTTL <= 0 {
		intent.CheckpointTTL = 10 * time.Minute
	}
	if action == queue.ActionRecordProgress {
		kind := "notes"
		if source.Adapter == "github" {
			kind = "comment"
		}
		intent.Patch = map[string]string{"append": kind}
	} else if action != queue.ActionAssignToMe && source.Adapter == "backlog-md" {
		intent.Patch = map[string]string{"status": transition}
	}
	claim := queueWriteClaim{backend: c.backend, path: path, session: c.session}
	if err := claim.Verify(ctx, intent); err != nil {
		return queueui.WritePreview{}, "", err
	}
	adapter, err := c.adapter(source)
	if err != nil {
		return queueui.WritePreview{}, "", err
	}
	scope := "local"
	if c.backend.Remote {
		scope = "remote"
	}
	preview := queueui.WritePreview{Identity: queueIdentity(item), AuthorityProfile: c.profile, Scope: scope, Intent: intent, Races: []string{"external provider writers are not fenced by this claim"}}
	switch a := adapter.(type) {
	case *queue.BacklogWriteAdapter:
		var detail queue.BacklogWritePreview
		intent, detail, err = a.Prepare(ctx, intent)
		preview.Effect = strings.Join(detail.Argv, " ")
		preview.SideEffects = []string{"provider task edit", "local Backlog.md watchers may refresh"}
		if detail.CreatesCommit {
			preview.SideEffects = append(preview.SideEffects, "Git commit")
		}
		if detail.RunsHooks {
			preview.SideEffects = append(preview.SideEffects, "Git hooks")
		}
		if detail.AssignmentRace {
			preview.Races = append(preview.Races, "assignment read-modify-write can overwrite concurrent assignees")
		}
	case *queue.GitHubWriteAdapter:
		var detail queue.GitHubWritePreview
		intent, detail, err = a.Prepare(ctx, intent)
		preview.Effect = detail.Operation
		if intent.Append != "" {
			preview.Effect += ": " + intent.Append
		}
		if intent.Action == queue.ActionAssignToMe {
			preview.Effect += ": " + intent.Principal
		}
		if intent.Action == queue.ActionComplete || intent.Action == queue.ActionReopen {
			preview.Effect += ": " + intent.Transition
		}
		preview.SideEffects = []string{"GitHub issue notification to watchers"}
	}
	if err != nil {
		return queueui.WritePreview{}, "", err
	}
	preview.Intent = intent
	return preview, path, nil
}

func (c queueWriteController) Preview(ctx context.Context, item queue.Item, action queue.Action, transition, text string) tea.Cmd {
	return func() tea.Msg {
		preview, _, err := c.prepare(ctx, item, action, transition, text, "", "")
		return queueui.WritePreviewMsg{Preview: &preview, Err: err}
	}
}

func (c queueWriteController) Recover(ctx context.Context, entry queue.RecoveryEntry) tea.Cmd {
	return func() tea.Msg {
		result := queueui.WriteResultMsg{OperationID: entry.OperationID, Result: queue.WriteResult{Outcome: queue.WriteUnknown, ClaimHeld: true}}
		record, err := c.journal.Read(entry.OperationID)
		if err != nil {
			result.Err = err
			return result
		}
		path, err := queueClaimHandlePath(c.backend.Config.Home, c.session, c.profile, record.Intent.Source, record.Intent.Ref)
		if err != nil {
			result.Err = err
			return result
		}
		read, ok := c.registry.Get(record.Intent.Source.Adapter)
		if !ok {
			result.Err = fmt.Errorf("write adapter unavailable")
			return result
		}
		var adapter queue.WriteAdapter
		switch a := read.(type) {
		case *queue.BacklogAdapter:
			adapter = &queue.BacklogWriteAdapter{BacklogAdapter: a, Me: record.Intent.Principal}
		case *queue.GitHubAdapter:
			adapter = queue.NewGitHubWriteAdapter(a, true)
		default:
			result.Err = fmt.Errorf("write adapter unavailable")
			return result
		}
		recoverCtx, stop := context.WithTimeout(ctx, 30*time.Second)
		defer stop()
		pipeline := queue.WritePipeline{Adapter: adapter, Claim: queueWriteClaim{backend: c.backend, path: path, session: c.session}, Journal: c.journal}
		result.Result, result.Err = pipeline.Recover(recoverCtx, entry.OperationID)
		return result
	}
}

func (c queueWriteController) Reconcile(ctx context.Context, entry queue.RecoveryEntry, evidence string) tea.Cmd {
	return func() tea.Msg {
		result := queueui.ReconcileResultMsg{OperationID: entry.OperationID}
		operator, err := user.Current()
		if err != nil || operator.Username == "" {
			result.Err = fmt.Errorf("operator identity unavailable")
			return result
		}
		result.Err = (queue.WritePipeline{Journal: c.journal}).Reconcile(ctx, entry.OperationID, operator.Username, evidence, true, true)
		return result
	}
}

func (c queueWriteController) Confirm(ctx context.Context, preview queueui.WritePreview) tea.Cmd {
	return func() tea.Msg {
		id := preview.Intent.OperationID
		result := queueui.WriteResultMsg{OperationID: id, Result: queue.WriteResult{Outcome: queue.WriteConflict, ClaimHeld: true, SourceUnchanged: true}}
		item := queue.Item{Summary: queue.Summary{Ref: preview.Intent.Ref, CanonicalID: preview.Identity}}
		fresh, path, err := c.prepare(ctx, item, preview.Intent.Action, preview.Intent.Transition, preview.Intent.Append, id, preview.Intent.OperationRef)
		if err != nil {
			result.Err = err
			return result
		}
		// Renewal may change only the claim revision and checkpoint deadline; an
		// altered provider intent needs a fresh, explicitly approved preview.
		compare := fresh.Intent
		compare.ClaimRevision, compare.CheckpointNotAfter = preview.Intent.ClaimRevision, preview.Intent.CheckpointNotAfter
		if !reflect.DeepEqual(compare, preview.Intent) || fresh.Effect != preview.Effect || !reflect.DeepEqual(fresh.SideEffects, preview.SideEffects) {
			result.Err = fmt.Errorf("provider write preview changed; reopen it")
			return result
		}
		fresh.Intent.CheckpointNotAfter = preview.Intent.CheckpointNotAfter
		adapter, err := c.adapter(fresh.Intent.Source)
		if err != nil {
			result.Err = err
			return result
		}
		pipeline := queue.WritePipeline{Adapter: adapter, Claim: queueWriteClaim{backend: c.backend, path: path, session: c.session}, Journal: c.journal, Workflow: c.configured[fresh.Intent.Source.ID].Workflow}
		result.Result, result.Err = pipeline.Start(ctx, fresh.Intent)
		return result
	}
}
