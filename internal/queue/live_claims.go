package queue

import (
	"context"
	"errors"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/watch"
)

// LiveClaimReader is the read-only capability needed by a live overlay.
type LiveClaimReader interface {
	ClaimStatusReader
	Events(context.Context, string, int) (ledger.EventsPage, error)
	Watch(context.Context, watch.Request) (watch.Result, error)
}

// RunClaimOverlay maintains a claim projection from a baseline cursor. publish
// receives complete projections after baselines and targeted updates; a history
// gap causes a visible rebuilding transition before any new projection appears.
func RunClaimOverlay(ctx context.Context, items []Item, sources map[string]ClaimSource, selected ClaimAuthority, paths config.ProfilePaths, env func(string) string, publish func([]Item, bool, error)) error {
	if selected.LiveAPI == nil {
		return errors.New("live claim watch is unavailable")
	}
	current := cloneItemsByList(items)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		head, err := selected.LiveAPI.Events(ctx, "", 1)
		if err != nil {
			markClaimStale(current, claimFailureReason(err))
			publish(current, false, err)
			if claimFailureReason(err) == "authority-mismatch" {
				return err
			}
			if waitForClaimRetry(ctx) != nil {
				return ctx.Err()
			}
			continue
		}
		if head.AuthorityID != selected.ID || head.NextCursor == "" {
			err = errors.New("claim authority identity or cursor changed")
			markClaimStale(current, "authority-mismatch")
			publish(current, false, err)
			return err
		}
		current = OverlayClaims(ctx, current, sources, selected, paths, env)
		cursor := head.NextCursor
		publish(current, false, nil)
		if retryableClaims(current) {
			if waitForClaimRetry(ctx) != nil {
				return ctx.Err()
			}
			continue
		}
		pendingExpiry := make(map[string]bool)
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			wait := 500 * time.Millisecond
			if expiry, ok := nearestClaimExpiry(current); ok {
				if selected.Now == nil {
					return errors.New("authority time is unavailable")
				}
				now, clockErr := selected.Now()
				if clockErr != nil {
					markClaimStale(current, "authority-time-unavailable")
					publish(current, false, clockErr)
					return clockErr
				}
				if delay := expiry.Sub(now); delay > 0 && delay < wait {
					wait = delay
				} else if delay <= 0 {
					for resource := range expiredClaimResources(current, now) {
						pendingExpiry[resource] = true
					}
					current = statusResources(ctx, current, pendingExpiry, sources, selected, paths, env)
					pendingExpiry = unresolvedClaimResources(current, pendingExpiry)
					publish(current, false, nil)
					wait = 50 * time.Millisecond
				}
			}
			result, watchErr := selected.LiveAPI.Watch(ctx, watch.Request{Cursor: cursor, Timeout: wait})
			if watchErr != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				markClaimStale(current, claimFailureReason(watchErr))
				publish(current, false, watchErr)
				if claimFailureReason(watchErr) == "authority-mismatch" {
					return watchErr
				}
				if waitForClaimRetry(ctx) != nil {
					return ctx.Err()
				}
				break
			}
			if result.AuthorityID != selected.ID {
				watchErr = errors.New("claim authority identity changed")
				markClaimStale(current, "authority-mismatch")
				publish(current, false, watchErr)
				return watchErr
			}
			if result.Gap {
				current = unknownClaims(current, selected.ID, "history-gap")
				publish(current, true, nil)
				break
			}
			if result.Event != nil {
				touched := make(map[string]bool, len(result.Event.Resources))
				for _, resource := range result.Event.Resources {
					touched[resource] = true
				}
				current = statusResources(ctx, current, touched, sources, selected, paths, env)
				publish(current, false, nil)
				if retryableTouchedClaims(current, touched) {
					if waitForClaimRetry(ctx) != nil {
						return ctx.Err()
					}
					continue // Replay the event; never advance past an unobserved state.
				}
			}
			cursor = result.NextCursor
			if result.TimedOut && selected.Now != nil {
				now, clockErr := selected.Now()
				if clockErr != nil {
					markClaimStale(current, "authority-time-unavailable")
					publish(current, false, clockErr)
					return clockErr
				}
				for resource := range expiredClaimResources(current, now) {
					pendingExpiry[resource] = true
				}
				if len(pendingExpiry) > 0 {
					current = statusResources(ctx, current, pendingExpiry, sources, selected, paths, env)
					pendingExpiry = unresolvedClaimResources(current, pendingExpiry)
					publish(current, false, nil)
				}
			}
		}
	}
}

func waitForClaimRetry(ctx context.Context) error {
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryableClaims(items []Item) bool {
	for _, item := range items {
		if len(item.Resources) > 0 && item.Claim.Reason == "" && !item.Claim.Known {
			return true
		}
	}
	return false
}

func retryableTouchedClaims(items []Item, touched map[string]bool) bool {
	for _, item := range items {
		if item.Claim.Known || item.Claim.Reason != "" {
			continue
		}
		for _, resource := range item.Resources {
			if touched[resource] {
				return true
			}
		}
	}
	return false
}

func expiredClaimResources(items []Item, now time.Time) map[string]bool {
	resources := make(map[string]bool)
	for _, item := range items {
		if item.Claim.Active && !item.Claim.ExpiresAt.After(now) {
			for _, resource := range item.Resources {
				resources[resource] = true
			}
		}
	}
	return resources
}

func unresolvedClaimResources(items []Item, resources map[string]bool) map[string]bool {
	unresolved := make(map[string]bool)
	for _, item := range items {
		if item.Claim.Known || item.Claim.Reason != "" {
			continue
		}
		for _, resource := range item.Resources {
			if resources[resource] {
				unresolved[resource] = true
			}
		}
	}
	return unresolved
}

func claimFailureReason(err error) string {
	if classified := reason.As(err); classified != nil {
		if classified.Reason == reason.ReasonAuthorityRestored || classified.Reason == reason.ReasonAuthorityMismatch {
			return "authority-mismatch"
		}
	}
	return "authority-unavailable"
}

func cloneItemsByList(items []Item) []Item {
	out := make([]Item, len(items))
	for i, item := range items {
		out[i] = cloneItem(item)
	}
	return out
}

func nearestClaimExpiry(items []Item) (time.Time, bool) {
	var nearest time.Time
	for _, item := range items {
		if item.Claim.Active && !item.Claim.ExpiresAt.IsZero() && (nearest.IsZero() || item.Claim.ExpiresAt.Before(nearest)) {
			nearest = item.Claim.ExpiresAt
		}
	}
	return nearest, !nearest.IsZero()
}

func unknownClaims(items []Item, authorityID, reason string) []Item {
	for i := range items {
		observedAt := items[i].Claim.ObservedAt
		items[i].Claim = ClaimObservation{AuthorityID: authorityID, State: "unknown", NativeState: "not-exposed", Stale: true, Reason: reason, ObservedAt: observedAt}
	}
	return items
}

func markClaimStale(items []Item, reason string) {
	for i := range items {
		items[i].Claim.Known = false
		items[i].Claim.Active = false
		items[i].Claim.Available = false
		items[i].Claim.Stale = true
		items[i].Claim.Reason = reason
	}
}

func statusResources(ctx context.Context, items []Item, resources map[string]bool, sources map[string]ClaimSource, selected ClaimAuthority, paths config.ProfilePaths, env func(string) string) []Item {
	if len(resources) == 0 {
		return items
	}
	var subset []Item
	indexes := make([]int, 0)
	for i, item := range items {
		match := false
		for _, resource := range item.Resources {
			if resources[resource] {
				match = true
			}
		}
		if match {
			subset = append(subset, item)
			indexes = append(indexes, i)
		}
	}
	observed := OverlayClaims(ctx, subset, sources, selected, paths, env)
	for j, item := range observed {
		items[indexes[j]] = item
	}
	return items
}
