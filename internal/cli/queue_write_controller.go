package cli

import (
	"context"
	"encoding/json"
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
	case *queue.BeadsAdapter:
		me := c.me[source.ID]
		if len(me) != 1 || me[0] == "" {
			return nil, fmt.Errorf("beads identity not configured")
		}
		return &queue.BeadsWriteAdapter{BeadsAdapter: a, Me: me[0]}, nil
	case *queue.GitHubAdapter:
		configured := c.configured[source.ID]
		allowProjectWrites := configured.GitHubProject != nil && configured.GitHubProject.AllowWrites
		return &queue.GitHubWriteAdapter{GitHubAdapter: a, Interactive: true, AllowProjectWrites: allowProjectWrites}, nil
	case *queue.ExternalAdapter:
		configured, ok := c.configured[source.ID]
		if !ok || configured.Adapter != "external" || configured.Account == "" {
			return nil, fmt.Errorf("external provider identity not configured")
		}
		if err := config.CheckQueueAdapterApproval(os.Getenv, configured); err != nil {
			return nil, err
		}
		return queue.NewExternalWriteAdapter(a), nil
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
	if source.Adapter == "beads" {
		var liveActor string
		if entry, ok := cfg.Me["beads"]; ok {
			if err := entry.Decode(&liveActor); err != nil {
				return queueui.WritePreview{}, "", err
			}
		}
		if len(c.me[source.ID]) != 1 || c.me[source.ID][0] != liveActor {
			return queueui.WritePreview{}, "", fmt.Errorf("beads actor changed; reopen the write preview")
		}
	}
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
		if current.Adapter != configured.Adapter || current.Checkout != configured.Checkout || current.Repository != configured.Repository || current.Host != configured.Host || current.Account != configured.Account || action == queue.ActionStart && current.Workflow["start"] != configured.Workflow["start"] || configured.GitHubProject != nil && (!reflect.DeepEqual(current.GitHubProject, configured.GitHubProject) || !reflect.DeepEqual(current.Workflow, configured.Workflow)) {
			return queueui.WritePreview{}, "", fmt.Errorf("queue source binding changed; confirm migration before writing")
		}
		if configured.Adapter == "beads" && !reflect.DeepEqual(current.Workflow, configured.Workflow) {
			return queueui.WritePreview{}, "", fmt.Errorf("beads workflow changed; reopen the write preview")
		}
		if configured.Adapter == "external" {
			if !reflect.DeepEqual(current, configured) {
				return queueui.WritePreview{}, "", fmt.Errorf("external source binding changed; reopen the write preview")
			}
			if err := config.CheckQueueAdapterApproval(os.Getenv, current); err != nil {
				return queueui.WritePreview{}, "", err
			}
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
	if source.Adapter == "backlog-md" || source.Adapter == "beads" {
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
		if source.Adapter == "github" || source.Adapter == "beads" {
			kind = "comment"
		}
		intent.Patch = map[string]string{"append": kind}
	} else if action != queue.ActionAssignToMe && (source.Adapter == "backlog-md" || source.Adapter == "beads") {
		intent.Patch = map[string]string{"status": transition}
	} else if configured.GitHubProject != nil && source.Adapter == "github" && (action == queue.ActionStart || action == queue.ActionResume || action == queue.ActionReportBlocked || action == queue.ActionRequestReview) {
		intent.Patch = map[string]string{"projectOptionID": transition}
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
	case *queue.BeadsWriteAdapter:
		var detail queue.BeadsWritePreview
		intent, detail, err = a.Prepare(ctx, intent)
		preview.Effect = strings.Join(detail.Argv, " ")
		preview.SideEffects = []string{"local Dolt commit (may include pending Dolt changes)", "Dolt auto-push disabled; no Git commit or hooks"}
		if detail.AutoExport {
			preview.SideEffects = append(preview.SideEffects, "Git-tracked .beads/issues.jsonl may change (throttled auto-export; no Git commit)")
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
		if detail.Unconditional {
			preview.SideEffects = []string{"GitHub Projects v2 item status update"}
			preview.Races = append(preview.Races, "project status transition is unconditional; external project writers are not fenced")
		} else {
			preview.SideEffects = []string{"GitHub issue notification to watchers"}
		}
	case *queue.ExternalWriteAdapter:
		var detail queue.ExternalWritePreview
		intent, detail, err = a.Prepare(ctx, intent)
		if err == nil {
			err = applyExternalWritePreview(&preview, detail)
		}
	}
	if err != nil {
		return queueui.WritePreview{}, "", err
	}
	preview.Intent = intent
	return preview, path, nil
}

func applyExternalWritePreview(preview *queueui.WritePreview, detail queue.ExternalWritePreview) error {
	patch, err := json.Marshal(detail.Patch)
	if err != nil {
		return err
	}
	preview.Effect = detail.Operation + " " + string(patch)
	preview.SideEffects = []string{"external provider side effects and notifications are adapter-defined", "configured adapter executes with owner privileges and is not sandboxed"}
	if !detail.ConditionalWrite {
		preview.Races = append(preview.Races, "provider does not condition the write on the previewed version")
	}
	return nil
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
		var adapter queue.WriteAdapter
		var workflow map[string]string
		cleanup := func() {}
		// Checkpoint recovery reads only the authority; a removed or changed
		// source must not block it.
		if record.NeedsProviderReadback() {
			adapter, workflow, cleanup, err = c.recoveryAdapter(ctx, record.Intent)
			if err != nil {
				result.Err = err
				return result
			}
		}
		defer cleanup()
		recoverCtx, stop := context.WithTimeout(ctx, 30*time.Second)
		defer stop()
		pipeline := queue.WritePipeline{Adapter: adapter, Claim: queueWriteClaim{backend: c.backend, path: path, session: c.session}, Journal: c.journal, Workflow: workflow}
		result.Result, result.Err = pipeline.Recover(recoverCtx, entry.OperationID)
		return result
	}
}

func (c queueWriteController) recoveryAdapter(ctx context.Context, intent queue.WriteIntent) (queue.WriteAdapter, map[string]string, func(), error) {
	if intent.Source.Adapter == queue.ExternalSourceAdapterKey(intent.Source.ID) {
		adapter, workflow, cleanup, err := queueExternalRecoveryAdapter(ctx, intent)
		if err != nil {
			return nil, nil, nil, err
		}
		return adapter, workflow, cleanup, nil
	}
	read, ok := c.registry.Get(intent.Source.Adapter)
	if !ok {
		return nil, nil, nil, fmt.Errorf("write adapter unavailable")
	}
	switch a := read.(type) {
	case *queue.BacklogAdapter:
		return &queue.BacklogWriteAdapter{BacklogAdapter: a, Me: intent.Principal}, nil, func() {}, nil
	case *queue.BeadsAdapter:
		return &queue.BeadsWriteAdapter{BeadsAdapter: a, Me: intent.Principal}, nil, func() {}, nil
	case *queue.GitHubAdapter:
		configured := c.configured[intent.Source.ID]
		return &queue.GitHubWriteAdapter{GitHubAdapter: a, Interactive: true, AllowProjectWrites: configured.GitHubProject != nil && configured.GitHubProject.AllowWrites}, nil, func() {}, nil
	default:
		return nil, nil, nil, fmt.Errorf("write adapter unavailable")
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

func (c queueWriteController) AttestCheckpointMissing(ctx context.Context, entry queue.RecoveryEntry, evidence string) tea.Cmd {
	return func() tea.Msg {
		result := queueui.ReconcileResultMsg{OperationID: entry.OperationID, Status: "checkpoint-missing"}
		operator, err := user.Current()
		if err != nil || operator.Username == "" {
			result.Err = fmt.Errorf("operator identity unavailable")
			return result
		}
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
		if c.backend.AuthorityID() != record.Intent.AuthorityID {
			result.Err = fmt.Errorf("selected authority differs from recovery intent")
			return result
		}
		recoverCtx, stop := context.WithTimeout(ctx, 30*time.Second)
		defer stop()
		result.Err = (queue.WritePipeline{Journal: c.journal, Claim: queueWriteClaim{backend: c.backend, path: path, session: c.session}}).AttestCheckpointMissing(recoverCtx, entry.OperationID, operator.Username, evidence, true)
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
