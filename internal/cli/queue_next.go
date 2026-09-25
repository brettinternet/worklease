package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/resource"
	urfavecli "github.com/urfave/cli/v3"
)

func queueNextAction(s *boundary) func(context.Context, *urfavecli.Command) error {
	return queueQueryActionWithSelection(s, queue.NewRegistry, queue.NewLoader, func(ctx context.Context, cmd *urfavecli.Command, cfg config.QueueConfig, view *config.QueueView, registry *queue.Registry, sources []queue.Source, claimSources map[string]queue.ClaimSource, backend *authorityContext, auth queue.ClaimAuthority, scoped, visible []queue.Item, sourceRows []queueSourceJSON, incomplete bool) error {
		if cmd.Bool("start") && !cmd.Bool("claim") {
			return s.handle(cmd, reason.Invalid("--start requires --claim"))
		}
		limit := cmd.Int("group")
		if limit < 1 || limit > 32 || cmd.Bool("claim") && (cmd.IsSet("group") || cmd.IsSet("max-age") || cmd.IsSet("cursor")) {
			return s.handle(cmd, reason.Invalid("--claim requires an unpaginated fresh snapshot and cannot be combined with --group, --max-age, or --cursor"))
		}
		selectors := make([]queue.Ref, 0, len(cmd.StringSlice("item")))
		known := make(map[string]bool, len(scoped))
		for _, item := range scoped {
			known[item.Ref.Key()] = true
		}
		for _, value := range cmd.StringSlice("item") {
			source, id, ok := strings.Cut(value, ":")
			ref := queue.Ref{SourceID: source, ItemID: id}
			if !ok || source == "" || id == "" || !known[ref.Key()] && !incomplete {
				return s.handle(cmd, reason.Invalid("--item must be a source-qualified ref present in the view"))
			}
			selectors = append(selectors, ref)
		}
		selectionLimit := limit
		if cmd.Bool("claim") {
			selectionLimit = len(scoped)
			if selectionLimit == 0 {
				selectionLimit = 1
			}
		}
		completeSelection := !incomplete || linearKnownItemSelection(cfg, sourceRows, scoped)
		result := queue.SelectWave(scoped, visible, view.Sources, selectors, completeSelection, selectionLimit, func(item queue.Item, owner string) bool {
			return isQueueMe(cfg, item, owner)
		})
		selected := result.Candidates
		if bridge, ok := ctx.Value(queueMCPAcquireKey{}).(queueMCPAcquire); ok && cmd.Bool("claim") && (auth.ID != bridge.authorityID || auth.Profile != bridge.profile || (backend.Profile == nil) != (bridge.pinnedProfile == nil) || backend.Profile != nil && *backend.Profile != *bridge.pinnedProfile) {
			return s.handle(cmd, reason.New(reason.ReasonAuthorityMismatch, "queue view authority differs from the MCP lease authority"))
		}
		skipped := make([]map[string]any, 0)
		if cmd.Bool("claim") && result.Result != "incomplete" {
			visibleRefs := make(map[string]bool, len(visible))
			for _, item := range visible {
				visibleRefs[item.Ref.Key()] = true
			}
			for _, item := range scoped {
				selectedRef := len(selectors) == 0
				for _, ref := range selectors {
					selectedRef = selectedRef || ref == item.Ref
				}
				if selectedRef && visibleRefs[item.Ref.Key()] && item.Readiness.Status == queue.Ready && item.Claim.Active && item.Claim.Known {
					skipped = append(skipped, map[string]any{"ref": item.Ref, "holder": item.Claim.AgentID, "expiresAt": item.Claim.ExpiresAt})
				}
			}
		}
		var grant map[string]any
		var startHandle string
		if cmd.Bool("claim") && result.Result == "ready" {
			if backend.Config.SessionID == "" {
				return s.handle(cmd, reason.Invalid("--claim requires --session or WORKLEASE_SESSION_ID"))
			}
			selected = nil
			eligibilityChanged := false
			for _, candidate := range result.Candidates {
				source, ok := claimSources[candidate.Ref.SourceID]
				if !ok || source.BlockReason != "" {
					return s.handle(cmd, reason.New("identity-unknown", "claim source is unavailable"))
				}
				adapter, ok := registry.Get(source.Source.Adapter)
				if !ok {
					return s.handle(cmd, reason.New("identity-unknown", "claim adapter is unavailable"))
				}
				sourceMap := make(map[string]queue.Source, len(sources))
				for _, src := range sources {
					sourceMap[src.ID] = src
				}
				fresh, err := refreshQueueActionClosure(ctx, registry, sourceMap, candidate)
				if err != nil {
					return s.handle(cmd, err)
				}
				fresh.Claim = queue.ClaimObservation{}
				if eligibility := queue.EvaluateAction(fresh, queue.ActionStart); !eligibility.Eligible || !queueNextAssignmentEligible(cfg, view, fresh, selectors) {
					if fresh.Readiness.Status == queue.ReadinessUnknown {
						return s.handle(cmd, reason.New(reason.ReasonQueueIncomplete, "candidate prerequisite evidence is unknown; query again").With("ref", candidate.Ref.String()))
					}
					eligibilityChanged = true
					skipped = append(skipped, map[string]any{"ref": candidate.Ref, "reason": "eligibility-changed"})
					continue
				}
				inputs := queue.IdentityInputs(source, auth.ID)
				key, err := resource.Resolve(resource.Input{Provider: inputs.Policy, Source: inputs.Source, Item: fresh.Ref.ItemID})
				if err != nil || !containsResource(candidate.Resources, key.Resource) {
					return s.handle(cmd, reason.New("identity-changed", "candidate claim resource changed; query again"))
				}
				keys, err := preAcquireQueueIdentity(ctx, source, adapter, auth, fresh)
				if err != nil {
					return s.handle(cmd, err)
				}
				observed := queue.OverlayClaims(ctx, []queue.Item{fresh}, map[string]queue.ClaimSource{source.Source.ID: source}, auth, config.UserProfilePaths(os.Getenv), os.Getenv)[0]
				if observed.Claim.Active && observed.Claim.Known && observed.Claim.AuthorityID == auth.ID {
					skipped = append(skipped, map[string]any{"ref": candidate.Ref, "holder": observed.Claim.AgentID, "expiresAt": observed.Claim.ExpiresAt})
					continue
				}
				if !observed.Claim.Known || observed.Claim.Stale || !observed.Claim.Available || observed.Claim.Reason != "" || observed.Claim.AuthorityID != auth.ID {
					return s.handle(cmd, reason.New("claim-unknown", "claim authority observation is unavailable"))
				}
				workerHandle := ""
				if cmd.Bool("start") {
					if _, mcp := ctx.Value(queueMCPAcquireKey{}).(queueMCPAcquire); !mcp {
						workerHandle, err = acquireHandlePath(ctx, cmd, backend.Config, auth.ID, true)
						if err != nil {
							return s.handle(cmd, err)
						}
					}
				}
				grant, err = acquireQueueWorker(ctx, cmd, backend, auth, keys, workerHandle)
				if err != nil {
					if failure := reason.As(err); failure != nil && failure.Reason == reason.ReasonAlreadyClaimed {
						row := map[string]any{"ref": candidate.Ref}
						if holder, ok := failure.Details["holder"].(map[string]any); ok {
							row["holder"], row["expiresAt"] = holder["agentId"], holder["expiresAt"]
						} else {
							row["holder"] = failure.Details["holder"]
						}
						skipped = append(skipped, row)
						continue
					}
					if JSONErrorHandled(err) {
						return s.handle(cmd, reason.As(err))
					}
					return s.handle(cmd, err)
				}
				startHandle = workerHandle
				if bridge, ok := ctx.Value(queueMCPAcquireKey{}).(queueMCPAcquire); ok && cmd.Bool("start") {
					startHandle = bridge.handlePath(grant)
				}
				fresh.Resources = keys
				fresh.KeyInputs = candidate.KeyInputs
				fresh.Claim = observed.Claim
				selected = []queue.Item{fresh}
				break
			}
			if grant == nil {
				result.Result = "active-claims"
				if eligibilityChanged {
					result.Result = "ineligible"
				}
			}
		}
		if incomplete && completeSelection && grant == nil && len(selected) == 0 {
			// A finished Linear scan still cannot prove that other issues do
			// not exist; only a successful claim is a definitive result.
			result.Result = "incomplete"
		}
		var transition map[string]any
		if cmd.Bool("start") {
			transition = map[string]any{"outcome": "not attempted", "reason": "no claim acquired"}
			if grant != nil {
				transition = queueNextStart(ctx, cfg, selected[0], registry, sources, backend, auth, backend.Config.SessionID, startHandle)
			}
		}
		candidates := make([]queueQueryItem, 0, len(selected))
		for _, item := range selected {
			candidates = append(candidates, queueQueryItem{Item: item, DisplayID: item.Ref.ItemID, Resources: item.Resources, Actions: map[string]queue.Eligibility{string(queue.ActionClaim): queue.ClaimActions(item)[queue.ActionClaim]}})
		}
		projected := normalizedQueueEnvelope(queueQueryEnvelope{Items: candidates})
		payload := map[string]any{
			"schemaVersion": 1,
			"view":          view.Name,
			"result":        result.Result,
			"authority":     queueAuthorityJSON{Profile: auth.Profile, ID: auth.ID, Scope: scopeLabel(auth.Remote)},
			"sources":       sourceRows,
			"candidates":    projected["items"],
			"excluded":      result.Excluded,
			"skipped":       skipped,
			"acquired":      grant != nil,
		}
		if incomplete && completeSelection {
			payload["selectionScope"] = "known-linear-items"
		}
		if cmd.Bool("start") {
			payload["claimOutcome"] = "not attempted"
			if grant != nil {
				payload["claimOutcome"] = "applied"
			}
			payload["transition"] = transition
		}
		if grant != nil {
			payload["claim"] = grant
		} else if !cmd.Bool("claim") {
			payload["hint"] = "Selection does not acquire. For agent loops use queue next --claim; manually claim the exact resources and re-query on contention."
		}
		if cmd.Bool("json") {
			return output.WriteSuccess(s.writer, "queue-next", map[string]any{"next": payload})
		}
		label := "no claim acquired; use --claim for agent loops"
		if grant != nil {
			label = "claim acquired; worker must heartbeat and release"
			if transition != nil && transition["outcome"] == "rejected" {
				label = "Claim acquired; status unchanged"
			} else if transition != nil && transition["outcome"] == "not attempted" {
				label = "Claim acquired; transition not attempted"
			} else if transition != nil && transition["outcome"] == "unknown" {
				label = "Claim acquired; provider outcome requires recovery"
			}
		}
		if _, err := fmt.Fprintf(s.writer, "%s (%s)\n", result.Result, label); err != nil {
			return err
		}
		if transition != nil {
			if _, err := fmt.Fprintf(s.writer, "claim: %s; transition: %s", payload["claimOutcome"], transition["outcome"]); err != nil {
				return err
			}
			if detail, ok := transition["reason"].(string); ok && detail != "" {
				if _, err := fmt.Fprintf(s.writer, " (%s)", safeQueueCell(detail)); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(s.writer); err != nil {
				return err
			}
		}
		for _, item := range candidates {
			if _, err := fmt.Fprintf(s.writer, "%s\t%s\t%s\n", safeQueueCell(item.Ref.String()), safeQueueCell(item.Title), safeQueueCell(strings.Join(item.Resources, ","))); err != nil {
				return err
			}
		}
		return nil
	})
}

// Linear cannot certify complete source membership from moving-cursor scans.
// A finished traversal may select only its observed items: every observed
// closure must be complete, and the exact candidate is re-read before acquire.
// Any interrupted scan, stale item, or other partial source still fails closed.
func linearKnownItemSelection(cfg config.QueueConfig, sources []queueSourceJSON, items []queue.Item) bool {
	partial := false
	for _, row := range sources {
		if row.Coverage.State == queue.CoverageComplete {
			if row.Freshness != "fresh" {
				return false
			}
			continue
		}
		configured := queueSourceByID(cfg.Sources, row.ID)
		if configured.Adapter != "linear" || row.Coverage.State != queue.CoveragePartial || row.Coverage.Reason != "reconciliation-incomplete" || row.Freshness == "stale" {
			return false
		}
		partial = true
	}
	if !partial || len(items) == 0 {
		return false
	}
	for _, item := range items {
		if !item.Fresh || !item.DependenciesKnown || item.Closure != queue.CoverageComplete {
			return false
		}
	}
	return true
}

func queueNextAssignmentEligible(cfg config.QueueConfig, view *config.QueueView, item queue.Item, selectors []queue.Ref) bool {
	for _, ref := range selectors {
		if ref == item.Ref {
			return true // An explicit selection overrides advisory assignment and its view filter.
		}
	}
	if len(item.AssignedTo) > 0 {
		mine := false
		for _, owner := range item.AssignedTo {
			mine = mine || isQueueMe(cfg, item, owner)
		}
		if !mine {
			return false
		}
	}
	if len(view.Filter.Assigned) == 0 {
		return true
	}
	for _, filter := range view.Filter.Assigned {
		if strings.EqualFold(filter, "nobody") && len(item.AssignedTo) == 0 {
			return true
		}
		for _, owner := range item.AssignedTo {
			if strings.EqualFold(filter, owner) || strings.EqualFold(filter, "me") && isQueueMe(cfg, item, owner) {
				return true
			}
		}
	}
	return false
}

type queueMCPAcquireKey struct{}
type queueMCPAcquire struct {
	authorityID, profile string
	pinnedProfile        *config.Profile
	acquire              func(context.Context, []string) (map[string]any, error)
	handlePath           func(map[string]any) string
}

type queueAcquirePinKey struct{}
type queueAcquirePin struct {
	authorityID string
	profile     *config.Profile
}

// Invoke the ordinary acquire command rather than maintaining a second handle,
// replay, admission, and remote pending-request implementation in the queue.
func acquireQueueWorker(ctx context.Context, cmd *urfavecli.Command, backend *authorityContext, auth queue.ClaimAuthority, keys []string, workerHandle string) (map[string]any, error) {
	if bridge, ok := ctx.Value(queueMCPAcquireKey{}).(queueMCPAcquire); ok {
		return bridge.acquire(ctx, keys)
	}
	args := []string{"worklease", "--json", "--home", backend.Config.Home}
	if auth.Remote {
		args = append(args, "--profile", auth.Profile)
	} else {
		args = append(args, "--profile", config.LocalProfileName)
	}
	if cmd.IsSet("config") {
		args = append(args, "--config", cmd.String("config"))
	}
	args = append(args, "acquire", "--session", backend.Config.SessionID, "--agent", backend.Config.AgentID, "--ttl", backend.Config.TTL.String(), "--coordination-only")
	if cmd.IsSet("ttl") {
		args[len(args)-2] = cmd.Duration("ttl").String()
	}
	if workerHandle == "" {
		workerHandle = strings.TrimSpace(cmd.String("handle"))
	}
	if workerHandle != "" {
		args = append(args, "--handle", workerHandle)
	}
	for _, key := range keys {
		args = append(args, "--resource", key)
	}
	var result bytes.Buffer
	ctx = context.WithValue(ctx, queueAcquirePinKey{}, queueAcquirePin{authorityID: auth.ID, profile: backend.Profile})
	err := Run(ctx, args, "", "", "", &result, &result)
	if err != nil {
		return nil, err
	}
	var grant map[string]any
	if err := json.Unmarshal(result.Bytes(), &grant); err != nil || grant["claimId"] == nil {
		return nil, reason.New(reason.ReasonUnknownOutcome, "acquire receipt could not be decoded; inspect the contextual handle before retrying").With("commitState", "unknown")
	}
	return grant, nil
}
