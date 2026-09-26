package cli

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/reason"
	urfave "github.com/urfave/cli/v3"
)

func queueIdentityCommand(s *boundary) *urfave.Command {
	return &urfave.Command{
		Name: "identity", Usage: "confirm a queue source claim-domain migration",
		Commands: []*urfave.Command{{
			Name: "confirm", Usage: "confirm the source identity after completing the migration checklist",
			Flags: []urfave.Flag{
				&urfave.StringFlag{Name: "source", Usage: "configured source ID"},
				&urfave.BoolFlag{Name: "acknowledge", Usage: "confirm old workers stopped, old claims and operations resolved, and all callers updated"},
			},
			Action: func(ctx context.Context, cmd *urfave.Command) error {
				if !cmd.Bool("acknowledge") || cmd.String("view") == "" || cmd.String("source") == "" {
					return s.handle(cmd, reason.Invalid("--view, --source and --acknowledge are required; "+queue.MigrationChecklist))
				}
				return s.handle(cmd, confirmQueueIdentity(ctx, cmd, cmd.String("view"), cmd.String("source")))
			},
		}},
	}
}

func confirmQueueIdentity(ctx context.Context, cmd *urfave.Command, viewName, sourceID string) error {
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil {
		return err
	}
	var view *config.QueueView
	for i := range cfg.Views {
		if cfg.Views[i].Name == viewName {
			view = &cfg.Views[i]
			break
		}
	}
	if view == nil {
		return reason.Invalid("unknown queue view")
	}
	included := false
	for _, id := range view.Sources {
		included = included || id == sourceID
	}
	if !included {
		return reason.Invalid("source is not in view")
	}
	var configured *config.QueueSource
	for i := range cfg.Sources {
		if cfg.Sources[i].ID == sourceID {
			configured = &cfg.Sources[i]
			break
		}
	}
	if configured == nil {
		return reason.Invalid("unknown queue source")
	}
	selected, authority, err := queueAuthorityForClaim(ctx, cmd, view.Authority)
	if err != nil {
		return err
	}
	defer selected.Close()
	registry := queue.NewRegistry()
	cleanupExternal, err := queue.RegisterExternalSources(registry, []config.QueueSource{*configured}, os.Getenv)
	if err != nil {
		return err
	}
	defer cleanupExternal()
	adapter, ok := registry.Get(queueAdapterRegistryKey(*configured))
	if !ok {
		return reason.Invalid("source adapter unavailable")
	}
	options := queueSourceOptions(*configured)
	if configured.Adapter == "external" {
		options = map[string]string{"id": configured.ID}
	}
	resolved, err := adapter.Resolve(ctx, options)
	if err != nil {
		return err
	}
	resolved.ID, resolved.Adapter = configured.ID, queueAdapterRegistryKey(*configured)
	source, ok := queue.ClaimSources(cfg, []queue.Source{resolved})[sourceID]
	if !ok {
		return reason.Invalid("source claim inputs unavailable")
	}
	// Linear's stable issue UUIDs do not need an enumerated ID ledger.
	// Its visible list is intentionally partial until reconciliation; requiring
	// complete enumeration would make the initial claim domain unconfirmable.
	ids := make([]string, 0)
	seen := map[string]bool{}
	cursor := ""
	for configured.Adapter != "linear" {
		page, e := adapter.List(ctx, resolved, queue.Query{Budget: 100}, cursor)
		if e != nil {
			return reason.Invalid("complete fresh source enumeration required before confirmation")
		}
		for _, item := range page.Items {
			if seen[item.Ref.ItemID] {
				return reason.Invalid("duplicate-item-id: cannot confirm migration")
			}
			seen[item.Ref.ItemID] = true
			ids = append(ids, item.Ref.ItemID)
		}
		if page.Coverage.State == queue.CoverageUnknown {
			return reason.Invalid("complete fresh source enumeration required before confirmation")
		}
		if page.NextCursor == "" {
			if page.Coverage.State != queue.CoverageComplete {
				return reason.Invalid("complete fresh source enumeration required before confirmation")
			}
			break
		}
		if page.NextCursor == cursor || len(ids) > 10000 {
			return reason.Invalid("source enumeration did not complete")
		}
		cursor = page.NextCursor
	}
	return config.UpdateQueueIdentities(ctx, os.Getenv, func(identities *config.QueueIdentities) error {
		return confirmQueueSourceIdentity(ctx, identities, sourceID, configured, resolved, source, authority, ids)
	})
}

// confirmQueueSourceIdentity checks old keys in the view authority and records
// the new claim domain. Claims in a former, different authority cannot be seen
// here; the operator's acknowledgement covers them.
func confirmQueueSourceIdentity(ctx context.Context, identities *config.QueueIdentities, sourceID string, configured *config.QueueSource, resolved queue.Source, source queue.ClaimSource, authority queue.ClaimAuthority, ids []string) error {
	previous := identities.Sources[sourceID]
	if previous.Policy == "" {
		previous = queue.IdentityInputs(source, authority.ID)
		if configured.Adapter == "backlog-md" && configured.Claims != nil {
			directory, e := queue.BacklogDirectory(resolved.Locator)
			if e != nil {
				return e
			}
			previous.Policy, previous.Source = "backlog-md", directory
		}
	}
	oldIDs := append(append([]string(nil), previous.ItemIDs...), ids...)
	if err := queue.PreviousKeys(ctx, authority, previous, oldIDs); err != nil {
		return fmt.Errorf("binding-migration-required: %w", err)
	}
	for _, domain := range previous.Retired {
		if err := queue.PreviousKeys(ctx, authority, config.QueueIdentity{Policy: domain.Policy, Source: domain.Source}, oldIDs); err != nil {
			return fmt.Errorf("binding-migration-required: %w", err)
		}
	}
	current := queue.IdentityInputs(source, authority.ID)
	if !queue.SameIdentity(previous, current) {
		if err := queue.PreviousKeys(ctx, authority, current, ids); err != nil {
			return fmt.Errorf("binding-migration-required: %w", err)
		}
	}
	sort.Strings(ids)
	current.ItemIDs = ids
	current.Retired = append([]config.ClaimDomain(nil), previous.Retired...)
	if !queue.SameIdentity(previous, current) && (previous.Policy != current.Policy || previous.Source != current.Source) {
		current.Retired = append(current.Retired, config.ClaimDomain{Policy: previous.Policy, Source: previous.Source})
	}
	if len(current.Retired) >= 32 {
		return reason.Invalid("too many former claim domains for one atomic claim")
	}
	identities.Sources[sourceID] = current
	return nil
}

// preAcquireQueueIdentity runs the final identity gate and, for sources whose
// item IDs are tracked, records the item before acquisition so a later
// renumber is still checked against its old key.
func preAcquireQueueIdentity(ctx context.Context, source queue.ClaimSource, adapter queue.Adapter, authority queue.ClaimAuthority, item queue.Item) ([]string, error) {
	identities, err := config.LoadQueueIdentities(os.Getenv)
	if err != nil {
		return nil, reason.New("identity-unknown", "private queue identity record unavailable")
	}
	previous := identities.Sources[source.Source.ID]
	keys, err := queue.PreAcquireIdentity(ctx, source, adapter, authority, previous, item)
	if err != nil {
		return nil, err
	}
	if queue.TracksItemIDs(source, authority.ID) {
		if err := config.RecordQueueClaimItem(ctx, os.Getenv, source.Source.ID, previous, item.Ref.ItemID); err != nil {
			return nil, reason.New("identity-unknown", "claim item could not be recorded in the confirmed claim domain: "+err.Error())
		}
	}
	return keys, nil
}
