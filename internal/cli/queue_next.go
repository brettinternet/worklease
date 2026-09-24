package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/reason"
	urfavecli "github.com/urfave/cli/v3"
)

func queueNextAction(s *boundary) func(context.Context, *urfavecli.Command) error {
	return queueQueryActionWithSelection(s, queue.NewRegistry, queue.NewLoader, func(cmd *urfavecli.Command, cfg config.QueueConfig, view *config.QueueView, scoped, visible []queue.Item, sources []queueSourceJSON, authority queueAuthorityJSON, incomplete bool) error {
		limit := cmd.Int("group")
		if limit < 1 || limit > 32 {
			return s.handle(cmd, reason.Invalid("--group must be between 1 and 32"))
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
		result := queue.SelectWave(scoped, visible, view.Sources, selectors, !incomplete, limit, func(item queue.Item, owner string) bool {
			return isQueueMe(cfg, item, owner)
		})
		candidates := make([]queueQueryItem, 0, len(result.Candidates))
		for _, item := range result.Candidates {
			candidates = append(candidates, queueQueryItem{Item: item, DisplayID: item.Ref.ItemID, Resources: item.Resources, Actions: map[string]queue.Eligibility{string(queue.ActionClaim): queue.ClaimActions(item)[queue.ActionClaim]}})
		}
		// Reuse query's public-digest projection so generic resource keys stay
		// exact without allowing arbitrary caller-supplied strings through redaction.
		projected := normalizedQueueEnvelope(queueQueryEnvelope{Items: candidates})
		payload := map[string]any{
			"schemaVersion": 1,
			"view":          view.Name,
			"result":        result.Result,
			"authority":     authority,
			"sources":       sources,
			"candidates":    projected["items"],
			"excluded":      result.Excluded,
			"acquired":      false,
			"hint":          "Selection does not acquire. For agent loops use queue next --claim (TASK-130.5); manually claim the exact resources and re-query on contention.",
		}
		if cmd.Bool("json") {
			return output.WriteSuccess(s.writer, "queue-next", map[string]any{"next": payload})
		}
		if _, err := fmt.Fprintf(s.writer, "%s (observation only; no claim acquired; use --claim for agent loops)\n", result.Result); err != nil {
			return err
		}
		for _, item := range candidates {
			if _, err := fmt.Fprintf(s.writer, "%s\t%s\t%s\n", safeQueueCell(item.Ref.String()), safeQueueCell(item.Title), safeQueueCell(strings.Join(item.Resources, ","))); err != nil {
				return err
			}
		}
		return nil
	})
}
