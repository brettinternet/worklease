package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
)

// queueNextStart is a separate outcome after acquisition. The caller keeps its
// claim even when the provider rejects or cannot establish the write outcome.
func queueNextStart(ctx context.Context, cfg config.QueueConfig, item queue.Item, registry *queue.Registry, sources []queue.Source, backend *authorityContext, auth queue.ClaimAuthority, session, path string) map[string]any {
	outcome := map[string]any{"outcome": "not attempted", "message": "Claim acquired; transition not attempted"}
	configured := make(map[string]config.QueueSource, len(cfg.Sources))
	for _, source := range cfg.Sources {
		configured[source.ID] = source
	}
	mapping := configured[item.Ref.SourceID]
	if mapping.Adapter != "backlog-md" || mapping.Workflow["start"] == "" {
		return map[string]any{"outcome": "not attempted", "reason": "no supported Start work mapping for source"}
	}
	if path == "" {
		return map[string]any{"outcome": "unknown", "reason": "acquired private handle unavailable; inspect claim before recovery"}
	}
	byID := make(map[string]queue.Source, len(sources))
	for _, source := range sources {
		byID[source.ID] = source
	}
	fresh, err := refreshQueueActionClosure(ctx, registry, byID, item)
	if err != nil {
		outcome["reason"] = err.Error()
		return outcome
	}
	if eligibility := queue.EvaluateAction(fresh, queue.ActionStart); !eligibility.Eligible {
		outcome["outcome"] = "rejected"
		outcome["message"] = "Claim acquired; status unchanged"
		outcome["reason"] = "start eligibility changed: " + strings.Join(eligibility.Reasons, "; ")
		return outcome
	}
	var actor []string
	if entry, ok := cfg.Me["backlog-md"]; ok {
		if err := entry.Decode(&actor); err != nil {
			outcome["reason"] = err.Error()
			return outcome
		}
	}
	journal, err := queueRecoveryJournal()
	if err != nil {
		outcome["reason"] = err.Error()
		return outcome
	}
	profile := auth.Profile
	if profile == "" {
		profile = config.LocalProfileName
	}
	controller := queueWriteController{
		backend: backend, registry: registry, current: func() (queue.ClaimAuthority, uint64) { return auth, 0 },
		journal: journal, sources: byID, configured: configured,
		me: map[string][]string{item.Ref.SourceID: actor}, session: session, profile: profile, handlePath: path,
	}
	preview, _, err := controller.prepare(ctx, fresh, queue.ActionStart, mapping.Workflow["start"], "", "", "")
	if err != nil {
		outcome["reason"] = err.Error()
		return outcome
	}
	written := controller.Confirm(ctx, preview)().(queueui.WriteResultMsg)
	if written.OperationID != "" {
		outcome["operationId"] = written.OperationID
	}
	switch {
	case written.Err == nil && written.Result.Outcome == queue.WriteVerified:
		return map[string]any{"outcome": "applied", "operationId": written.OperationID}
	case written.Result.Outcome == queue.WriteConflict && written.Result.SourceUnchanged:
		outcome["outcome"] = "rejected"
		outcome["message"] = "Claim acquired; status unchanged"
		if written.Err != nil {
			outcome["reason"] = written.Err.Error()
		}
	default:
		outcome["outcome"] = "unknown"
		outcome["message"] = "Claim acquired; provider outcome requires recovery"
		outcome["reason"] = fmt.Sprint(written.Result.Detail)
		if written.Err != nil {
			outcome["reason"] = written.Err.Error()
		}
	}
	return outcome
}
