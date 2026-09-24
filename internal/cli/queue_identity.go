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
	selected, authority, err := queueAuthorityForView(ctx, cmd, view.Authority)
	if err != nil {
		return err
	}
	defer selected.Close()
	registry := queue.NewRegistry()
	adapter, _ := registry.Get(configured.Adapter)
	resolved, err := adapter.Resolve(ctx, map[string]string{"id": configured.ID, "checkout": configured.Checkout, "host": configured.Host, "repository": configured.Repository, "account": configured.Account, "allowGitNetwork": fmt.Sprint(configured.AllowGitNetwork)})
	if err != nil {
		return err
	}
	resolved.ID, resolved.Adapter = configured.ID, configured.Adapter
	source, ok := queue.ClaimSources(cfg, []queue.Source{resolved})[sourceID]
	if !ok {
		return reason.Invalid("source claim inputs unavailable")
	}
	ids := make([]string, 0)
	seen := map[string]bool{}
	cursor := ""
	for {
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
	identities, err := config.LoadQueueIdentities(os.Getenv)
	if err != nil {
		return err
	}
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
	if previous.AuthorityID != "" && previous.AuthorityID != authority.ID {
		return reason.Invalid("prior authority differs; confirm old claims and operations in that authority first")
	}
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
	return config.SaveQueueIdentities(os.Getenv, identities)
}
