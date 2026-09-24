package queue

import (
	"context"
	"fmt"
	"sort"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/resource"
)

// IdentityInputs are the configured claim inputs, not a provider-resolved node ID.
func IdentityInputs(source ClaimSource, authorityID string) config.QueueIdentity {
	policy := source.Policy
	if policy == "" {
		policy = source.Source.Adapter
	}
	keySource := source.ClaimSource
	if keySource == "" {
		keySource = source.Source.Locator
	}
	return config.QueueIdentity{Adapter: source.Source.Adapter, Locator: source.Source.Locator, Policy: policy, Source: keySource, AuthorityID: authorityID}
}

func SameIdentity(a, b config.QueueIdentity) bool {
	return a.Adapter == b.Adapter && a.Locator == b.Locator && a.Policy == b.Policy && a.Source == b.Source && a.AuthorityID == b.AuthorityID
}

const MigrationChecklist = "Stop old workers; resolve old claims and pending operations; update CLI, skill, and launch callers to the new key; edit queue.yaml to rebind. Claims in other authorities cannot be detected."

// IdentityGate checks the source's observed IDs and prior claim domain. A
// missing or unreadable observation is never interpreted as permission to claim.
// Calling again immediately before acquisition supplies a fresh provider list.
func IdentityGate(ctx context.Context, source ClaimSource, adapter Adapter, authority ClaimAuthority, previous config.QueueIdentity, observed []Item, deleted []Ref) (string, string) {
	current := IdentityInputs(source, authority.ID)
	if adapter == nil {
		return "identity-unknown", "source adapter unavailable"
	}
	freshIDs := map[string]bool(nil)
	if source.Source.Adapter == "backlog-md" && current.Policy == "generic" {
		page, err := adapter.List(ctx, source.Source, Query{}, "")
		if err != nil {
			return "identity-unknown", "fresh Backlog.md list unavailable"
		}
		freshIDs = make(map[string]bool, len(page.Items))
		for _, summary := range page.Items {
			if freshIDs[summary.Ref.ItemID] {
				return "duplicate-item-id", "Backlog.md list contains repeated task IDs; resolve duplicates and old claims before rebinding"
			}
			freshIDs[summary.Ref.ItemID] = true
		}
		if page.Coverage.State != CoverageComplete {
			return "identity-unknown", "Backlog.md list is incomplete"
		}
	}
	for _, item := range observed {
		if item.Ref.SourceID == source.Source.ID && item.ReadOutcome == "identity-changed" {
			return "identity-changed", identityDetail(adapter, source.Source)
		}
	}
	if previous.Policy == "" {
		return "binding-migration-required", "Confirm the initial claim domain. " + MigrationChecklist
	} else if !SameIdentity(previous, current) {
		return "binding-migration-required", fmt.Sprintf("claim inputs changed: %s/%s -> %s/%s. %s", previous.Policy, previous.Source, current.Policy, current.Source, MigrationChecklist)
	}
	// The completed refresh may have removed an old ref; ask the same authority
	// whether it still owns the old key before offering any new key.
	ids := freshIDs
	if ids == nil {
		ids = make(map[string]bool)
		for _, item := range observed {
			if item.Ref.SourceID == source.Source.ID {
				ids[item.Ref.ItemID] = true
			}
		}
	}
	old := append([]string(nil), previous.ItemIDs...)
	if previous.Policy == "" {
		previous = current
	}
	for _, ref := range deleted {
		if ref.SourceID == source.Source.ID {
			old = append(old, ref.ItemID)
		}
	}
	missing := map[string]bool{}
	for _, id := range old {
		if !ids[id] {
			missing[id] = true
		}
	}
	if len(missing) == 0 {
		return "", ""
	}
	if authority.API == nil || authority.ID == "" {
		return "identity-unknown", "cannot check claims on missing task IDs"
	}
	keys := make([]string, 0, len(missing))
	for id := range missing {
		key, err := resource.Resolve(resource.Input{Provider: previous.Policy, Source: previous.Source, Item: id})
		if err != nil {
			return "identity-unknown", "cannot derive prior task key"
		}
		keys = append(keys, key.Resource)
	}
	sort.Strings(keys)
	for start := 0; start < len(keys); start += 32 {
		end := start + 32
		if end > len(keys) {
			end = len(keys)
		}
		status, err := authority.API.Status(ctx, lease.Selector{AuthorityID: authority.ID, Resources: keys[start:end]})
		if err != nil || len(status.Resources) != end-start {
			return "identity-unknown", "old-key claim status unavailable"
		}
		expected := map[string]bool{}
		for _, key := range keys[start:end] {
			expected[key] = true
		}
		for _, entry := range status.Resources {
			if !expected[entry.Resource] {
				return "identity-unknown", "old-key claim status mismatched"
			}
			delete(expected, entry.Resource)
			if entry.State == "active" {
				return "identity-changed", "task ID disappeared while its old key is held: " + entry.Resource + "; " + MigrationChecklist
			}
			if entry.State != "free" && entry.State != "expired" {
				return "identity-unknown", "old-key claim status ambiguous"
			}
		}
		if len(expected) != 0 {
			return "identity-unknown", "old-key claim status incomplete"
		}
	}
	return "", ""
}

// PreAcquireIdentity repeats the fresh gate and returns all keys that must be
// acquired atomically. An old caller claiming a retired key then conflicts with
// the new caller even if it starts after the status observation.
func PreAcquireIdentity(ctx context.Context, source ClaimSource, adapter Adapter, authority ClaimAuthority, previous config.QueueIdentity, item Item) ([]string, error) {
	if item.Ref.SourceID != source.Source.ID {
		return nil, fmt.Errorf("item is outside claim source")
	}
	if code, detail := IdentityGate(ctx, source, adapter, authority, previous, []Item{item}, nil); code != "" {
		return nil, fmt.Errorf("%s: %s", code, detail)
	}
	if source.Source.Adapter == "backlog-md" && IdentityInputs(source, authority.ID).Policy == "generic" {
		page, err := adapter.List(ctx, source.Source, Query{}, "")
		if err != nil || page.Coverage.State != CoverageComplete {
			return nil, fmt.Errorf("fresh source enumeration required before acquisition")
		}
		found := false
		seen := make(map[string]bool, len(page.Items))
		for _, summary := range page.Items {
			if seen[summary.Ref.ItemID] {
				return nil, fmt.Errorf("duplicate-item-id: fresh list contains repeated IDs")
			}
			seen[summary.Ref.ItemID] = true
			found = found || summary.Ref.ItemID == item.Ref.ItemID
		}
		if !found {
			return nil, fmt.Errorf("identity-changed: item is absent from fresh list")
		}
	}
	current := IdentityInputs(source, authority.ID)
	inputs := []resource.Input{{Provider: current.Policy, Source: current.Source, Item: item.Ref.ItemID}}
	for _, domain := range previous.Retired {
		input := resource.Input{Provider: domain.Policy, Source: domain.Source, Item: item.Ref.ItemID}
		key, err := resource.Resolve(input)
		if err != nil {
			return nil, err
		}
		// A host-local former key cannot be admitted by a portable remote
		// authority. Only the operator's explicit migration acknowledgement
		// can retire that exclusion domain; it cannot be fenced remotely.
		if authority.Remote && (authority.AdmittedPrefixes == nil || !lease.ResourceAdmitted(*authority.AdmittedPrefixes, key.Resource)) {
			continue
		}
		old := config.QueueIdentity{Policy: domain.Policy, Source: domain.Source}
		if err := PreviousKeys(ctx, authority, old, []string{item.Ref.ItemID}); err != nil {
			return nil, fmt.Errorf("prior claim domain is held or unknown: %w", err)
		}
		inputs = append(inputs, input)
	}
	keys := make([]string, 0, len(inputs))
	seen := map[string]bool{}
	for _, input := range inputs {
		key, err := resource.Resolve(input)
		if err != nil {
			return nil, err
		}
		if authority.Remote && (authority.AdmittedPrefixes == nil || !lease.ResourceAdmitted(*authority.AdmittedPrefixes, key.Resource)) {
			return nil, fmt.Errorf("claim domain is not admitted by view authority")
		}
		if !seen[key.Resource] {
			seen[key.Resource] = true
			keys = append(keys, key.Resource)
		}
	}
	if len(keys) > 32 {
		return nil, fmt.Errorf("claim domain migration exceeds atomic claim limit")
	}
	return keys, nil
}

// GuardClaimSources applies the same gate to all actionable items in a view.
// The source index is not proof of identity: portable Backlog.md bindings
// always re-list from the provider, including when a cached snapshot is used.
func GuardClaimSources(ctx context.Context, sources map[string]ClaimSource, registry *Registry, authority ClaimAuthority, state config.QueueIdentities, snapshot Snapshot) map[string]ClaimSource {
	out := make(map[string]ClaimSource, len(sources))
	items := make([]Item, 0, len(snapshot.Items))
	for _, item := range snapshot.Items {
		items = append(items, item)
	}
	deleted := make([]Ref, 0, len(snapshot.Deleted))
	for _, ref := range snapshot.Deleted {
		deleted = append(deleted, ref)
	}
	for id, source := range sources {
		adapter, _ := registry.Get(source.Source.Adapter)
		source.BlockReason, source.BlockDetail = IdentityGate(ctx, source, adapter, authority, state.Sources[id], items, deleted)
		if snapshot.Sources[id].Reason == "identity-changed" {
			source.BlockReason = "identity-changed"
			source.BlockDetail = identityDetail(adapter, source.Source)
		}
		out[id] = source
	}
	return out
}

func identityDetail(adapter Adapter, source Source) string {
	locator := source.Locator
	if reporter, ok := adapter.(interface{ IdentityChange(Source) string }); ok {
		if change := reporter.IdentityChange(source); change != "" {
			locator = change
		}
	}
	return "provider identity changed: " + locator + "; " + MigrationChecklist
}

// PreviousKeys builds old-domain keys for the explicit migration confirmation.
// Refuse an uncertain status rather than treating it as a free key.
func PreviousKeys(ctx context.Context, authority ClaimAuthority, previous config.QueueIdentity, ids []string) error {
	if authority.API == nil || authority.ID == "" {
		return fmt.Errorf("claim authority unavailable")
	}
	keys := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		key, err := resource.Resolve(resource.Input{Provider: previous.Policy, Source: previous.Source, Item: id})
		if err != nil {
			return err
		}
		keys = append(keys, key.Resource)
	}
	sort.Strings(keys)
	for start := 0; start < len(keys); start += 32 {
		end := start + 32
		if end > len(keys) {
			end = len(keys)
		}
		status, err := authority.API.Status(ctx, lease.Selector{AuthorityID: authority.ID, Resources: keys[start:end]})
		if err != nil || len(status.Resources) != end-start {
			return fmt.Errorf("old-key claim status unavailable")
		}
		found := map[string]bool{}
		expected := map[string]bool{}
		for _, key := range keys[start:end] {
			expected[key] = true
		}
		for _, entry := range status.Resources {
			if found[entry.Resource] || !expected[entry.Resource] {
				return fmt.Errorf("old-key claim status ambiguous")
			}
			found[entry.Resource] = true
			if entry.State == "active" || entry.State != "free" && entry.State != "expired" {
				return fmt.Errorf("old-key claim held or unknown: %s", entry.Resource)
			}
		}
	}
	return nil
}
