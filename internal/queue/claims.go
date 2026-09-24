package queue

import (
	"context"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/resource"
)

// ClaimSource retains the configured identity inputs, rather than deriving keys
// from a display ID or from the checkout running the queue.
type ClaimSource struct {
	Source                   Source
	Policy, ClaimSource      string
	BlockReason, BlockDetail string
}

// ClaimSources carries queue.yaml's exact key inputs into the overlay. Without
// an explicit binding, keys use the same default sources CLI callers document.
// A Backlog.md source whose directory cannot be resolved is omitted.
func ClaimSources(cfg config.QueueConfig, resolved []Source) map[string]ClaimSource {
	byID := make(map[string]Source, len(resolved))
	for _, source := range resolved {
		byID[source.ID] = source
	}
	out := make(map[string]ClaimSource, len(cfg.Sources))
	for _, configured := range cfg.Sources {
		source, ok := byID[configured.ID]
		if !ok {
			continue
		}
		input := ClaimSource{Source: source}
		if configured.Claims != nil {
			input.Policy, input.ClaimSource = configured.Claims.Policy, configured.Claims.Source
		} else if configured.Adapter == "github" {
			input.ClaimSource = configured.Repository
			if configured.Host != "github.com" {
				input.ClaimSource = configured.Host + "/" + configured.Repository
			}
		} else if configured.Adapter == "backlog-md" {
			dir, err := BacklogDirectory(source.Locator)
			if err != nil {
				// An unresolvable key source must not fall back to the checkout root.
				continue
			}
			input.ClaimSource = dir
		}
		out[configured.ID] = input
	}
	return out
}

// ClaimStatusReader is the only authority capability available to the queue core.
// Mutation methods are deliberately absent from this boundary.
type ClaimStatusReader interface {
	Status(context.Context, lease.Selector) (lease.Status, error)
}

type ClaimAuthority struct {
	API     ClaimStatusReader
	LiveAPI LiveClaimReader
	Now     func() (time.Time, error)
	ID      string
	Profile string
	Remote  bool
	// Nil means the remote server did not advertise its admission policy.
	AdmittedPrefixes        *[]string
	LocalDefaultAuthorityID string
}

// OverlayClaims observes one selected authority. A failed read never converts
// an unknown claim to a free claim; no authority mutation is performed.
func OverlayClaims(ctx context.Context, items []Item, sources map[string]ClaimSource, selected ClaimAuthority, paths config.ProfilePaths, env func(string) string) []Item {
	out := make([]Item, len(items))
	resources := make([]string, 0, len(items))
	indexes := make(map[string][]int)
	for i, input := range items {
		item := cloneItem(input)
		item.NativeClaim = "not-exposed"
		item.Claim = ClaimObservation{AuthorityID: selected.ID, State: "unknown", NativeState: "not-exposed", Stale: true, ObservedAt: time.Now().UTC()}
		source, ok := sources[item.Ref.SourceID]
		if !ok {
			item.Claim.Reason = "source-unavailable"
		} else {
			item.Claim.Reason, item.Claim.Detail = source.BlockReason, source.BlockDetail
			policy := source.Policy
			keySource := source.ClaimSource
			if policy == "" {
				policy = source.Source.Adapter
			}
			if keySource == "" {
				keySource = source.Source.Locator
			}
			key, err := resource.Resolve(resource.Input{Provider: policy, Source: keySource, Item: item.Ref.ItemID})
			if err != nil {
				item.Claim.Reason = "invalid-resource"
			} else {
				item.Resources = []string{key.Resource}
				item.KeyInputs = &resource.Input{Provider: key.Provider, Source: key.Source, Item: key.Item}
				switch {
				case selected.API == nil || selected.Remote && selected.ID == "":
					item.Claim.Reason = "authority-unavailable"
				case selected.Remote && selected.AdmittedPrefixes == nil:
					item.Claim.Reason = "admission-unknown"
				case selected.Remote && !lease.ResourceAdmitted(*selected.AdmittedPrefixes, key.Resource):
					item.Claim.Reason = "resource-not-admitted"
				default:
					if source.Source.Adapter == "backlog-md" && !matchingCheckoutAuthority(source.Source.Locator, selected, paths, env) {
						item.Claim.Reason = "authority-mismatch"
					}
					indexes[key.Resource] = append(indexes[key.Resource], i)
					if len(indexes[key.Resource]) == 1 {
						resources = append(resources, key.Resource)
					}
				}
			}
		}
		out[i] = item
	}
	// The request is bounded well below 1 MiB, including protocol framing.
	// Thirty-two entries also leave room for a 4 MiB response even when each
	// claim contains the maximum 32 resources near the key size limit.
	for start := 0; start < len(resources); {
		end, bytes := start, 256
		for end < len(resources) && end-start < 32 && bytes+len(resources[end])+128 < 512*1024 {
			bytes += len(resources[end]) + 128
			end++
		}
		if end == start {
			end++
		}
		overlayBatch(ctx, out, indexes, resources[start:end], selected)
		start = end
	}
	return out
}

func matchingCheckoutAuthority(checkout string, selected ClaimAuthority, paths config.ProfilePaths, env func(string) string) bool {
	root, err := handle.ContextRoot(checkout, nil)
	if err != nil {
		return false
	}
	// The worker's own explicit --profile is not the queue's view flag. The
	// worker may still select one later; this checks the checkout default.
	choice, err := config.SelectProfile(nil, env, root, paths)
	if err != nil {
		return false
	}
	if choice.Profile != nil {
		return selected.Remote && choice.Profile.AuthorityID == selected.ID
	}
	return !selected.Remote && selected.Profile == config.LocalProfileName && selected.LocalDefaultAuthorityID != "" && selected.ID == selected.LocalDefaultAuthorityID
}

func overlayBatch(ctx context.Context, items []Item, indexes map[string][]int, keys []string, selected ClaimAuthority) {
	if len(keys) == 0 {
		return
	}
	status, err := selected.API.Status(ctx, lease.Selector{AuthorityID: selected.ID, Resources: keys})
	if err != nil {
		if classified := reason.As(err); classified != nil && classified.Reason == reason.ReasonResponseTooLarge && len(keys) > 1 {
			middle := len(keys) / 2
			overlayBatch(ctx, items, indexes, keys[:middle], selected)
			overlayBatch(ctx, items, indexes, keys[middle:], selected)
		}
		return
	}
	observed := time.Now().UTC()
	byResource := make(map[string]lease.ResourceStatus, len(status.Resources))
	for _, entry := range status.Resources {
		byResource[entry.Resource] = entry
	}
	for _, key := range keys {
		entry, ok := byResource[key]
		if !ok {
			continue
		}
		observation := ClaimObservation{AuthorityID: selected.ID, State: "unknown", NativeState: "not-exposed", Stale: true, ObservedAt: observed}
		switch entry.State {
		case "free":
			if entry.Claim == nil {
				observation.State, observation.Known, observation.Stale = "free", true, false
			}
		case "active", "expired":
			if entry.Claim != nil && entry.Claim.AuthorityID == selected.ID {
				observation.Known, observation.Stale = true, false
				observation.State = "expired"
				if entry.State == "active" && entry.Claim.Active {
					observation.State, observation.Active = "held", true
				}
				observation.AgentID, observation.SessionID = entry.Claim.AgentID, entry.Claim.SessionID
				observation.AcquiredAt, observation.ExpiresAt = entry.Claim.AcquiredAt, entry.Claim.ExpiresAt
			}
		}
		observation.Available = observation.Known && !observation.Active
		for _, index := range indexes[key] {
			reason, detail := items[index].Claim.Reason, items[index].Claim.Detail
			items[index].Claim = observation
			items[index].Claim.Reason, items[index].Claim.Detail = reason, detail
		}
	}
}

// ClaimActions applies authority-specific restrictions after generic readiness.
func ClaimActions(item Item) map[Action]Eligibility {
	actions := make(map[Action]Eligibility)
	for _, action := range []Action{ActionStart, ActionResume, ActionClaim, ActionLaunch, ActionReportBlocked, ActionRecordProgress, ActionComplete} {
		eligibility := EvaluateAction(item, action)
		if action == ActionClaim || action == ActionLaunch {
			eligibility = EvaluateAction(item, ActionStart)
		}
		if action == ActionStart || action == ActionResume || action == ActionClaim || action == ActionLaunch {
			switch {
			case item.Claim.Reason != "":
				eligibility = Eligibility{Reasons: []string{item.Claim.Reason}, Outcome: "capability"}
			case !item.Claim.Known || item.Claim.Stale:
				eligibility = Eligibility{Reasons: []string{"claim-unknown"}, Outcome: "capability"}
			}
		}
		if action == ActionLaunch && eligibility.Eligible {
			eligibility = Eligibility{Reasons: []string{"read-only-slice"}, Outcome: "capability"}
		}
		actions[action] = eligibility
	}
	return actions
}
