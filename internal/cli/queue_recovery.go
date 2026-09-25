package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"slices"
	"strings"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueindex"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/resource"
	urfave "github.com/urfave/cli/v3"
)

func queueExternalRecoveryAdapter(ctx context.Context, intent queue.WriteIntent) (*queue.ExternalWriteAdapter, map[string]string, func(), error) {
	if intent.Source.ID == "" || intent.Source.ID != intent.Ref.SourceID || intent.Source.Adapter != queue.ExternalSourceAdapterKey(intent.Source.ID) {
		return nil, nil, nil, fmt.Errorf("journaled external source identity is invalid")
	}
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil {
		return nil, nil, nil, err
	}
	var configured *config.QueueSource
	for i := range cfg.Sources {
		if cfg.Sources[i].ID == intent.Source.ID {
			configured = &cfg.Sources[i]
			break
		}
	}
	if configured == nil || configured.Adapter != "external" {
		return nil, nil, nil, fmt.Errorf("journaled external source is no longer configured")
	}
	if err := config.CheckQueueAdapterApproval(os.Getenv, *configured); err != nil {
		return nil, nil, nil, err
	}
	if configured.Account == "" || configured.Account != intent.Principal {
		return nil, nil, nil, fmt.Errorf("external provider identity changed since the journaled write")
	}
	if configured.Claims == nil || configured.Claims.Policy != "generic" {
		return nil, nil, nil, fmt.Errorf("external claim binding is unavailable")
	}
	key, err := resource.Resolve(resource.Input{Provider: configured.Claims.Policy, Source: configured.Claims.Source, Item: intent.Ref.ItemID})
	if err != nil || !slices.Contains(intent.Resources, key.Resource) {
		return nil, nil, nil, fmt.Errorf("external claim binding changed since the journaled write")
	}
	if mapping := externalRecoveryWorkflowKey(intent.Action); mapping != "" && (configured.Workflow[mapping] == "" || configured.Workflow[mapping] != intent.Transition) {
		return nil, nil, nil, fmt.Errorf("external workflow mapping changed since the journaled write")
	}
	registry := queue.NewRegistry()
	cleanup, err := queue.RegisterExternalSources(registry, []config.QueueSource{*configured}, os.Getenv)
	if err != nil {
		return nil, nil, nil, err
	}
	read, ok := registry.Get(queue.ExternalSourceAdapterKey(configured.ID))
	if !ok {
		cleanup()
		return nil, nil, nil, fmt.Errorf("external adapter unavailable")
	}
	resolved, err := read.Resolve(ctx, map[string]string{"id": configured.ID})
	if err != nil {
		cleanup()
		return nil, nil, nil, err
	}
	if resolved != intent.Source {
		cleanup()
		return nil, nil, nil, fmt.Errorf("external source resolution drifted from journaled source")
	}
	adapter, ok := read.(*queue.ExternalAdapter)
	if !ok {
		cleanup()
		return nil, nil, nil, fmt.Errorf("external write adapter unavailable")
	}
	return queue.NewExternalWriteAdapter(adapter), configured.Workflow, cleanup, nil
}

// queueBuiltinRecoveryAdapter resolves the journaled source afresh, because a
// new process has no resolved binding and read-back must not run against a
// drifted source.
func queueBuiltinRecoveryAdapter(ctx context.Context, intent queue.WriteIntent) (queue.WriteAdapter, error) {
	if intent.Source.ID == "" || intent.Source.ID != intent.Ref.SourceID {
		return nil, fmt.Errorf("journaled source identity is invalid")
	}
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil {
		return nil, err
	}
	var configured *config.QueueSource
	for i := range cfg.Sources {
		if cfg.Sources[i].ID == intent.Source.ID {
			configured = &cfg.Sources[i]
			break
		}
	}
	if configured == nil || configured.Adapter != intent.Source.Adapter {
		return nil, fmt.Errorf("journaled source is no longer configured")
	}
	read, ok := queue.NewRegistry().Get(configured.Adapter)
	if !ok {
		return nil, fmt.Errorf("write adapter unavailable")
	}
	resolved, err := read.Resolve(ctx, map[string]string{"id": configured.ID, "checkout": configured.Checkout, "host": configured.Host, "repository": configured.Repository, "account": configured.Account, "allowGitNetwork": fmt.Sprint(configured.AllowGitNetwork)})
	if err != nil {
		return nil, err
	}
	resolved.ID, resolved.Adapter = configured.ID, configured.Adapter
	if resolved != intent.Source {
		return nil, fmt.Errorf("source resolution drifted from journaled source")
	}
	switch a := read.(type) {
	case *queue.BacklogAdapter:
		return &queue.BacklogWriteAdapter{BacklogAdapter: a, Me: intent.Principal}, nil
	case *queue.GitHubAdapter:
		return queue.NewGitHubWriteAdapter(a, true), nil
	default:
		return nil, fmt.Errorf("write adapter unavailable")
	}
}

func externalRecoveryWorkflowKey(action queue.Action) string {
	switch action {
	case queue.ActionStart, queue.ActionResume:
		return "start"
	case queue.ActionReportBlocked:
		return "blocked"
	case queue.ActionRequestReview:
		return "review"
	case queue.ActionComplete:
		return "complete"
	case queue.ActionReopen:
		return "reopen"
	default:
		return ""
	}
}

func queueRecoveryJournal() (queue.WriteJournal, error) {
	cacheDir, err := queueindex.CacheDir(os.Getenv, "")
	if err != nil {
		return queue.WriteJournal{}, err
	}
	return queue.NewWriteJournal(config.QueueRecoveryDir(os.Getenv), cacheDir)
}

func queueRecoveryCommand(s *boundary) *urfave.Command {
	command := &urfave.Command{Name: "recovery", Usage: "list unresolved provider writes", UsageText: "worklease queue recovery [--json]"}
	command.Commands = []*urfave.Command{{
		Name: "retry", Usage: "read back a write or check an expired checkpoint without redispatching",
		Flags: []urfave.Flag{&urfave.StringFlag{Name: "operation-id", Usage: "exact journaled operation ID"}, &urfave.StringFlag{Name: "handle", Usage: "private handle for the original claim"}},
		Action: func(ctx context.Context, cmd *urfave.Command) error {
			if cmd.String("operation-id") == "" || cmd.String("handle") == "" {
				return s.handle(cmd, reason.Invalid("retry requires --operation-id and the original --handle"))
			}
			journal, err := queueRecoveryJournal()
			if err != nil {
				return s.handle(cmd, err)
			}
			record, err := journal.Read(cmd.String("operation-id"))
			if errors.Is(err, os.ErrNotExist) {
				return s.handle(cmd, reason.New(reason.ReasonOperationNotFound, "recovery operation not found; check the operation ID with queue recovery").With("commitState", "unknown"))
			}
			if err != nil {
				return s.handle(cmd, err)
			}
			backend, err := authorityFor(ctx, cmd, true)
			if err != nil {
				return s.handle(cmd, err)
			}
			defer backend.Close()
			if backend.AuthorityID() != record.Intent.AuthorityID {
				return s.handle(cmd, reason.New(reason.ReasonAuthorityMismatch, "selected authority differs from recovery intent"))
			}
			var adapter queue.WriteAdapter
			var workflow map[string]string
			cleanup := func() {}
			if record.Intent.Source.Adapter == queue.ExternalSourceAdapterKey(record.Intent.Source.ID) {
				adapter, workflow, cleanup, err = queueExternalRecoveryAdapter(ctx, record.Intent)
			} else {
				adapter, err = queueBuiltinRecoveryAdapter(ctx, record.Intent)
			}
			result := queue.WriteResult{Outcome: queue.WriteUnknown, ClaimHeld: true}
			if err != nil {
				err = fmt.Errorf("recovery source is unavailable: %w", err)
			} else {
				defer cleanup()
				pipeline := queue.WritePipeline{Journal: journal, Adapter: adapter, Claim: queueWriteClaim{backend: backend, path: cmd.String("handle")}, Workflow: workflow}
				result, err = pipeline.Recover(ctx, record.Intent.OperationID)
			}
			if err != nil {
				return s.handle(cmd, reason.New(reason.ReasonRecoveryRequired, fmt.Sprintf("read-back %s; claim held %t; operation %s: %v", result.Outcome, result.ClaimHeld, record.Intent.OperationID, err)).With("result", result).With("operationId", record.Intent.OperationID).With("commitState", "unknown"))
			}
			if cmd.Bool("json") {
				return output.WriteSuccess(s.writer, "queue-recovery-retry", map[string]any{"result": result, "operationId": record.Intent.OperationID})
			}
			_, err = fmt.Fprintf(s.writer, "%s: %s (claim held: %t)\n", result.Outcome, result.Detail, result.ClaimHeld)
			return err
		},
	}, {
		Name: "reconcile", Usage: "record proof that an unknown write did not commit and cannot still execute",
		Flags: []urfave.Flag{
			&urfave.StringFlag{Name: "operation-id", Usage: "exact journaled operation ID"},
			&urfave.StringFlag{Name: "evidence", Usage: "typed evidence that the provider write did not commit"},
			&urfave.BoolFlag{Name: "no-commit", Usage: "attest that the write did not commit"},
			&urfave.BoolFlag{Name: "executor-ceased", Usage: "attest that no executor can still perform the write"},
		},
		Action: func(ctx context.Context, cmd *urfave.Command) error {
			if strings.TrimSpace(cmd.String("operation-id")) == "" || strings.TrimSpace(cmd.String("evidence")) == "" || !cmd.Bool("no-commit") || !cmd.Bool("executor-ceased") {
				return s.handle(cmd, reason.Invalid("reconciliation requires an operation ID, typed no-commit evidence, --no-commit, and --executor-ceased"))
			}
			operator, err := user.Current()
			if err != nil || operator.Username == "" {
				return s.handle(cmd, reason.Invalid("operator identity unavailable"))
			}
			journal, err := queueRecoveryJournal()
			if err != nil {
				return s.handle(cmd, err)
			}
			pipeline := queue.WritePipeline{Journal: journal}
			if err := pipeline.Reconcile(ctx, cmd.String("operation-id"), operator.Username, cmd.String("evidence"), true, true); err != nil {
				return s.handle(cmd, err)
			}
			if cmd.Bool("json") {
				return output.WriteSuccess(s.writer, "queue-recovery-reconcile", map[string]any{"operationId": cmd.String("operation-id"), "operator": operator.Username, "status": "reconciled", "claimHeld": true})
			}
			_, err = fmt.Fprintf(s.writer, "Reconciled %s; claim remains held\n", cmd.String("operation-id"))
			return err
		},
	}, {
		Name: "checkpoint-missing", Usage: "attest verified provider effect with no committed checkpoint after the deadline",
		Flags: []urfave.Flag{
			&urfave.StringFlag{Name: "operation-id", Usage: "exact journaled operation ID"},
			&urfave.StringFlag{Name: "handle", Usage: "private handle for the original claim"},
			&urfave.StringFlag{Name: "evidence", Usage: "typed provider and authority audit evidence"},
			&urfave.BoolFlag{Name: "provider-verified", Usage: "attest that the provider effect occurred"},
			&urfave.BoolFlag{Name: "checkpoint-absent", Usage: "attest that the original checkpoint did not commit"},
			&urfave.BoolFlag{Name: "executor-ceased", Usage: "attest that no executor can still commit the checkpoint"},
		},
		Action: func(ctx context.Context, cmd *urfave.Command) error {
			if cmd.String("operation-id") == "" || cmd.String("handle") == "" || strings.TrimSpace(cmd.String("evidence")) == "" || !cmd.Bool("provider-verified") || !cmd.Bool("checkpoint-absent") || !cmd.Bool("executor-ceased") {
				return s.handle(cmd, reason.Invalid("checkpoint-missing requires --operation-id, --handle, --evidence, --provider-verified, --checkpoint-absent and --executor-ceased"))
			}
			operator, err := user.Current()
			if err != nil || operator.Username == "" {
				return s.handle(cmd, reason.Invalid("operator identity unavailable"))
			}
			journal, err := queueRecoveryJournal()
			if err != nil {
				return s.handle(cmd, err)
			}
			record, err := journal.Read(cmd.String("operation-id"))
			if err != nil {
				return s.handle(cmd, err)
			}
			backend, err := authorityFor(ctx, cmd, true)
			if err != nil {
				return s.handle(cmd, err)
			}
			defer backend.Close()
			if backend.AuthorityID() != record.Intent.AuthorityID {
				return s.handle(cmd, reason.New(reason.ReasonAuthorityMismatch, "selected authority differs from recovery intent"))
			}
			pipeline := queue.WritePipeline{Journal: journal, Claim: queueWriteClaim{backend: backend, path: cmd.String("handle")}}
			if err := pipeline.AttestCheckpointMissing(ctx, record.Intent.OperationID, operator.Username, cmd.String("evidence"), true); err != nil {
				return s.handle(cmd, err)
			}
			if cmd.Bool("json") {
				return output.WriteSuccess(s.writer, "queue-recovery-checkpoint-missing", map[string]any{"operationId": record.Intent.OperationID, "operator": operator.Username, "status": "checkpoint-missing", "claimHeld": true})
			}
			_, err = fmt.Fprintf(s.writer, "Provider verified, checkpoint missing for %s; claim remains held\n", record.Intent.OperationID)
			return err
		},
	}}
	command.Action = func(_ context.Context, cmd *urfave.Command) error {
		journal, err := queueRecoveryJournal()
		if err != nil {
			return s.handle(cmd, err)
		}
		entries, err := journal.Recovery()
		if err != nil {
			return s.handle(cmd, err)
		}
		if cmd.Bool("json") {
			return output.WriteSuccess(s.writer, "queue-recovery", map[string]any{"recovery": entries})
		}
		for _, entry := range entries {
			if _, err := fmt.Fprintf(s.writer, "%s\t%s\t%s\t%s\t%s\n", entry.OperationID, entry.Ref.String(), entry.Status, entry.Readback, entry.Effect); err != nil {
				return err
			}
		}
		return nil
	}
	return command
}
