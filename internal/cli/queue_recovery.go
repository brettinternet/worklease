package cli

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueindex"
	"github.com/brettinternet/worklease/internal/reason"
	urfave "github.com/urfave/cli/v3"
)

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
		Name: "retry", Usage: "read back one journaled write without redispatching it",
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
			registry := queue.NewRegistry()
			read, ok := registry.Get(record.Intent.Source.Adapter)
			if !ok {
				return s.handle(cmd, reason.Invalid("write adapter unavailable"))
			}
			var adapter queue.WriteAdapter
			switch source := read.(type) {
			case *queue.BacklogAdapter:
				adapter = &queue.BacklogWriteAdapter{BacklogAdapter: source, Me: record.Intent.Principal}
			case *queue.GitHubAdapter:
				adapter = queue.NewGitHubWriteAdapter(source, true)
			default:
				return s.handle(cmd, reason.Invalid("write adapter unavailable"))
			}
			pipeline := queue.WritePipeline{Journal: journal, Adapter: adapter, Claim: queueWriteClaim{backend: backend, path: cmd.String("handle")}}
			result, err := pipeline.Recover(ctx, record.Intent.OperationID)
			if err != nil {
				return s.handle(cmd, reason.New(reason.ReasonRecoveryRequired, fmt.Sprintf("read-back %s; claim held %t; operation %s: %v", result.Outcome, result.ClaimHeld, record.Intent.OperationID, err)).With("result", result).With("operationId", record.Intent.OperationID))
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
