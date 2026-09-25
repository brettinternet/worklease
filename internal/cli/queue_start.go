package cli

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
	tea "github.com/charmbracelet/bubbletea"
)

// Start work composes a normal queue claim with a separate journaled provider
// write. Neither system treats the other operation as atomic.
type queueStartController struct {
	claim *queueClaimController
	write queueWriteController
}

func (c queueStartController) prepare(ctx context.Context, item queue.Item) (queueui.StartPreview, queueClaimPlan, error) {
	cfg, ok := c.write.configured[item.Ref.SourceID]
	projectStart := cfg.Adapter == "github" && cfg.GitHubProject != nil && cfg.GitHubProject.AllowWrites
	linearStart := cfg.Adapter == "linear"
	if !ok || cfg.Workflow["start"] == "" || cfg.Adapter != "backlog-md" && !projectStart && !linearStart {
		return queueui.StartPreview{}, queueClaimPlan{}, fmt.Errorf("Start work has no supported provider mapping; Claim only")
	}
	plan, err := c.claim.prepare(ctx, item)
	if err != nil {
		return queueui.StartPreview{}, queueClaimPlan{}, err
	}
	live, err := config.LoadQueue(os.Getenv)
	if err != nil {
		return queueui.StartPreview{}, queueClaimPlan{}, err
	}
	current := false
	for _, source := range live.Sources {
		if source.ID == cfg.ID {
			current = source.Adapter == cfg.Adapter && source.Checkout == cfg.Checkout && source.Repository == cfg.Repository && source.Host == cfg.Host && source.Account == cfg.Account && source.Workflow["start"] == cfg.Workflow["start"]
			if projectStart {
				current = current && reflect.DeepEqual(source.GitHubProject, cfg.GitHubProject) && reflect.DeepEqual(source.Workflow, cfg.Workflow)
			}
			if linearStart {
				current = current && source.Organization == cfg.Organization && source.Team == cfg.Team && source.Project == cfg.Project && slices.Equal(source.CredentialHelper, cfg.CredentialHelper) && reflect.DeepEqual(source.Workflow, cfg.Workflow)
			}
			break
		}
	}
	currentClaims, claimsFound := queue.ClaimSources(live, []queue.Source{plan.source.Source})[cfg.ID]
	if !current || !claimsFound || currentClaims.Policy != plan.source.Policy || currentClaims.ClaimSource != plan.source.ClaimSource {
		return queueui.StartPreview{}, queueClaimPlan{}, fmt.Errorf("Start work source, claim binding, or transition changed; reopen the queue")
	}
	transition := cfg.Workflow["start"]
	adapter, err := c.write.adapter(plan.source.Source)
	if err != nil {
		return queueui.StartPreview{}, queueClaimPlan{}, err
	}
	if backlog, ok := adapter.(*queue.BacklogWriteAdapter); ok {
		actor := c.write.me[plan.source.Source.ID]
		var liveActor []string
		if entry, ok := live.Me["backlog-md"]; ok {
			if err := entry.Decode(&liveActor); err != nil {
				return queueui.StartPreview{}, queueClaimPlan{}, err
			}
		}
		if len(actor) == 0 || actor[0] == "" || !slices.Equal(actor, liveActor) {
			return queueui.StartPreview{}, queueClaimPlan{}, fmt.Errorf("provider actor changed or is not configured; reopen the queue")
		}
		intent, detail, err := backlog.Prepare(ctx, queue.WriteIntent{OperationID: randomHex(16), Source: plan.source.Source, Ref: plan.item.Ref, Principal: actor[0], Action: queue.ActionStart, Transition: transition, Patch: map[string]string{"status": transition}})
		if err != nil {
			return queueui.StartPreview{}, queueClaimPlan{}, err
		}
		preflight, err := backlog.Inspect(ctx, intent)
		if err != nil {
			return queueui.StartPreview{}, queueClaimPlan{}, err
		}
		if !preflight.Capability || !preflight.Authorized || !preflight.InScope || !preflight.Fresh || !preflight.NativeAvailable || !preflight.Ready || preflight.Precondition != intent.Precondition {
			return queueui.StartPreview{}, queueClaimPlan{}, fmt.Errorf("Start work provider permission or readiness unavailable")
		}
		effects := []string{"provider status change", "local Backlog.md watchers may refresh"}
		if detail.CreatesCommit {
			effects = append(effects, "Git commit")
		}
		if detail.RunsHooks {
			effects = append(effects, "Git hooks")
		}
		return queueui.StartPreview{Claim: plan.preview, Source: plan.source.Source.ID + " (" + plan.source.Source.Locator + ")", Actor: actor[0], Transition: transition, TransitionValue: transition, RequiredFields: "status=" + transition, Effect: strings.Join(detail.Argv, " "), SideEffects: effects}, plan, nil
	}
	if writer, ok := adapter.(*queue.LinearWriteAdapter); ok && linearStart {
		actor := cfg.Account
		intent, detail, err := writer.Prepare(ctx, queue.WriteIntent{OperationID: strings.Repeat("0", 32), Source: plan.source.Source, Ref: plan.item.Ref, Principal: actor, Action: queue.ActionStart, Transition: transition, Patch: map[string]string{"stateId": transition}})
		if err != nil {
			return queueui.StartPreview{}, queueClaimPlan{}, err
		}
		preflight, err := writer.Inspect(ctx, intent)
		if err != nil {
			return queueui.StartPreview{}, queueClaimPlan{}, err
		}
		if !preflight.Capability || !preflight.Authorized || !preflight.InScope || !preflight.Fresh || !preflight.NativeAvailable || !preflight.Ready || preflight.Precondition != intent.Precondition {
			return queueui.StartPreview{}, queueClaimPlan{}, fmt.Errorf("Start work provider permission or readiness unavailable")
		}
		return queueui.StartPreview{Claim: plan.preview, Source: plan.source.Source.ID + " (" + plan.source.Source.Locator + ")", Actor: actor, Transition: detail.Operation, TransitionValue: transition, RequiredFields: "stateId=" + transition, Effect: detail.Operation, SideEffects: []string{"Linear issue workflow state update"}}, plan, nil
	}
	if writer, ok := adapter.(*queue.GitHubWriteAdapter); ok && projectStart {
		actor := cfg.Account
		if actor == "" {
			return queueui.StartPreview{}, queueClaimPlan{}, fmt.Errorf("GitHub account is not configured")
		}
		intent, detail, err := writer.Prepare(ctx, queue.WriteIntent{OperationID: randomHex(16), Source: plan.source.Source, Ref: plan.item.Ref, Principal: actor, Action: queue.ActionStart, Transition: transition, Patch: map[string]string{"projectOptionID": transition}})
		if err != nil {
			return queueui.StartPreview{}, queueClaimPlan{}, err
		}
		preflight, err := writer.Inspect(ctx, intent)
		if err != nil {
			return queueui.StartPreview{}, queueClaimPlan{}, err
		}
		if !preflight.Capability || !preflight.Authorized || !preflight.InScope || !preflight.Fresh || !preflight.NativeAvailable || !preflight.Ready || preflight.Precondition != intent.Precondition {
			return queueui.StartPreview{}, queueClaimPlan{}, fmt.Errorf("Start work provider permission or readiness unavailable")
		}
		optionName := strings.TrimPrefix(detail.Operation, "set-project-status-unconditionally: ")
		return queueui.StartPreview{Claim: plan.preview, Source: plan.source.Source.ID + " (" + plan.source.Source.Locator + ")", Actor: actor, Transition: optionName, TransitionValue: transition, RequiredFields: "project status=" + optionName, Effect: detail.Operation, SideEffects: []string{"GitHub Projects v2 item status update; transition is unconditional"}}, plan, nil
	}
	return queueui.StartPreview{}, queueClaimPlan{}, fmt.Errorf("Start work has no supported provider mapping; Claim only")
}

func (c queueStartController) Preview(ctx context.Context, item queue.Item) tea.Cmd {
	return func() tea.Msg {
		preview, plan, err := c.prepare(ctx, item)
		if err != nil {
			return queueui.StartPreviewMsg{Identity: queueIdentity(item), Err: err}
		}
		return queueui.StartPreviewMsg{Identity: queueIdentity(item), Item: plan.item, Preview: &preview}
	}
}

func (c queueStartController) Start(ctx context.Context, item queue.Item, preview queueui.StartPreview) tea.Cmd {
	return func() tea.Msg {
		result := queueui.StartResultMsg{Claim: queueui.ClaimResultMsg{Identity: queueIdentity(item), Err: fmt.Errorf("start revalidation failed")}, ClaimStep: "rejected", TransitionStep: "not attempted"}
		fresh, plan, err := c.prepare(ctx, item)
		if err == nil && !reflect.DeepEqual(fresh, preview) {
			err = fmt.Errorf("Start work preview changed; reopen it")
		}
		if err != nil {
			result.Claim.Err = err
			return result
		}
		grant, err := c.claim.acquire(ctx, plan)
		if err != nil {
			result.Claim.Err = err
			if !isDefinitiveNoCommit(err) {
				result.ClaimStep = "unknown"
			}
			return result
		}
		result.Claim = queueClaimResult(plan, grant)
		result.ClaimStep = "applied"
		// The provider may change between claim and transition. Refresh the
		// closure and verify this exact private handle before any write.
		observed, err := refreshQueueActionClosure(ctx, c.claim.registry, c.claim.sources, plan.item)
		if err == nil {
			eligibility := queue.EvaluateAction(observed, queue.ActionStart)
			if !eligibility.Eligible {
				err = fmt.Errorf("start eligibility changed: %s", strings.Join(eligibility.Reasons, "; "))
			}
		}
		if err == nil {
			transition := preview.TransitionValue
			if transition == "" {
				transition = preview.Transition
			}
			writePreview, _, prepareErr := c.write.prepare(ctx, observed, queue.ActionStart, transition, "", "", "")
			err = prepareErr
			if err == nil {
				written := c.write.Confirm(ctx, writePreview)().(queueui.WriteResultMsg)
				result.Write = &written
				if written.Err == nil && written.Result.Outcome == queue.WriteVerified {
					result.TransitionStep = "applied"
				} else if written.Result.Outcome == queue.WriteConflict && written.Result.SourceUnchanged {
					result.TransitionStep = "rejected"
				} else {
					result.TransitionStep = "unknown"
				}
				return result
			}
		}
		result.TransitionStep = "rejected"
		result.Err = err
		return result
	}
}
