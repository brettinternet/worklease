package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
	urfave "github.com/urfave/cli/v3"
)

func writeLedgerResult(s *boundary, cmd *urfave.Command, operation string, fields map[string]any) error {
	if s.jsonRequested(cmd) {
		return output.WriteSuccess(s.writer, operation, fields)
	}
	return output.WriteText(s.writer, operation, fields)
}

func eventsAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		cursor := strings.TrimSpace(cmd.String("cursor"))
		if err := ledger.ValidateCursor(cursor, "events", ""); err != nil {
			return s.handle(cmd, err)
		}
		_, st, _, err := serviceFor(ctx, cmd, false)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		page, err := ledger.New(st).Events(ctx, cursor, cmd.Int("limit"))
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeLedgerResult(s, cmd, "events", map[string]any{"authorityId": page.AuthorityID, "events": page.Events, "nextCursor": page.NextCursor, "gap": page.Gap})
	}
}
func historyAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		resources := cmd.StringSlice("resource")
		if len(resources) != 1 {
			return s.handle(cmd, reason.Invalid("history requires exactly one resource"))
		}
		resource := resources[0]
		cursor := strings.TrimSpace(cmd.String("cursor"))
		if err := ledger.ValidateCursor(cursor, "history", resource); err != nil {
			return s.handle(cmd, err)
		}
		_, st, _, err := serviceFor(ctx, cmd, false)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		page, err := ledger.New(st).History(ctx, resource, cursor, cmd.Int("limit"), cmd.Bool("full"))
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeLedgerResult(s, cmd, "history", map[string]any{"authorityId": page.AuthorityID, "resource": page.Resource, "epochs": page.Epochs, "nextCursor": page.NextCursor, "gap": page.Gap})
	}
}

func inspectAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		if cmd.Bool("full") {
			if err := ValidateSelection(cmd, false); err != nil {
				return s.handle(cmd, err)
			}
		}
		cfg, err := config.Load(config.Input{Flags: map[string]string{"home": cmd.String("home"), "session": cmd.String("session"), "config": cmd.String("config")}})
		if err != nil {
			return s.handle(cmd, err)
		}
		st, err := store.Open(ctx, cfg.Home, store.Options{ReadOnly: true})
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		req := ledger.InspectRequest{OperationID: strings.TrimSpace(cmd.String("operation-id")), ClaimID: strings.TrimSpace(cmd.String("claim-id")), Full: cmd.Bool("full")}
		resources := cmd.StringSlice("resource")
		if len(resources) > 1 {
			return s.handle(cmd, reason.Invalid("operation inspection accepts one resource"))
		}
		if len(resources) == 1 {
			req.Resource = resources[0]
		}
		selectedHandle := strings.TrimSpace(cmd.String("handle"))
		if selectedHandle == "" {
			selectedHandle = strings.TrimSpace(os.Getenv("WORKLEASE_HANDLE"))
		}
		if !req.Full {
			if strings.TrimSpace(cmd.String("token-file")) != "" || cmd.IsSet("token-fd") || cmd.IsSet("revision") || strings.TrimSpace(cmd.String("lease")) != "" {
				return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "public inspection does not accept private credentials"))
			}
			if req.ClaimID != "" && req.Resource != "" || selectedHandle != "" && (req.ClaimID != "" || req.Resource != "") {
				return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "operation inspection selectors are exclusive"))
			}
			if req.ClaimID == "" && req.Resource == "" {
				path := selectedHandle
				if path == "" {
					path, err = acquireHandlePath(cmd, cfg)
					if err != nil {
						return s.handle(cmd, err)
					}
				}
				h, e := handle.Read(filepath.Clean(path))
				if e != nil {
					return s.handle(cmd, reason.New(reason.ReasonClaimSelectionMissing, "operation inspection requires a claim or resource"))
				}
				if h.AuthorityID != st.AuthorityID() {
					return s.handle(cmd, reason.New(reason.ReasonAuthorityMismatch, "handle authority does not match"))
				}
				req.ClaimID = h.ClaimID
			}
		}
		if req.Full {
			if strings.TrimSpace(cmd.String("token-file")) != "" || cmd.IsSet("token-fd") {
				req.Token, err = tokenFromCommand(cmd, "token-file", "token-fd")
				if err != nil {
					return s.handle(cmd, err)
				}
			} else {
				path := strings.TrimSpace(cmd.String("handle"))
				if path == "" {
					if strings.TrimSpace(cmd.String("lease")) != "" {
						return s.handle(cmd, reason.Invalid("private lease references are only available through MCP"))
					}
					path, err = acquireHandlePath(cmd, cfg)
					if err != nil {
						return s.handle(cmd, err)
					}
				}
				h, e := handle.Read(filepath.Clean(path))
				if e != nil {
					return s.handle(cmd, e)
				}
				if h.AuthorityID != st.AuthorityID() {
					return s.handle(cmd, reason.New(reason.ReasonAuthorityMismatch, "handle authority does not match"))
				}
				req.Token = h.Token
				if req.ClaimID == "" {
					req.ClaimID = h.ClaimID
				}
			}
		}
		view, err := ledger.New(st).Inspect(ctx, req)
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeLedgerResult(s, cmd, "inspect", map[string]any{"inspection": view})
	}
}

func reconcileAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		creds, svc, st, lock, h, path, err := credsCLI(ctx, cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		if lock != nil {
			defer lock.Close()
		}
		deadline, err := requestDeadlineCLI(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		var recovery *handle.RecoveryRequest
		if h != nil {
			recovery = h.RecoveryRequest
		}
		op, err := operationID(cmd, nil)
		if recovery != nil {
			if strings.TrimSpace(cmd.String("operation-id")) != "" && cmd.String("operation-id") != recovery.OperationID {
				return s.handle(cmd, reason.New(reason.ReasonOperationRequestMismatch, "pending reconciliation request differs"))
			}
			op = recovery.OperationID
			deadline = recovery.RequestNotAfter
		}
		if err != nil {
			return s.handle(cmd, err)
		}
		evidence := json.RawMessage(cmd.String("evidence"))
		targetClaim, targetOp, outcome, expected := cmd.String("target-claim-id"), cmd.String("target-operation-id"), cmd.String("outcome"), strings.ToLower(cmd.String("expected-request-sha256"))
		ttl := cmd.Duration("ttl")
		if ttl == 0 {
			ttl = svc.DefaultTTL()
		}
		inputs := map[string]any{"kind": "reconcile", "authorityId": creds.AuthorityID, "claimId": creds.ClaimID, "targetClaimId": targetClaim, "targetOperationId": targetOp, "expectedRequestSha256": expected, "outcome": outcome, "evidence": evidence, "ttl": ttl.Microseconds(), "requestNotAfter": deadline.UTC().UnixMicro()}
		hash := requestHashCLI(inputs)
		if recovery != nil {
			if recovery.TargetClaimID != targetClaim || recovery.TargetOperationID != targetOp || recovery.Outcome != outcome || recovery.RequestHash != hash || string(recovery.Evidence) != string(evidence) {
				return s.handle(cmd, reason.New(reason.ReasonOperationRequestMismatch, "pending reconciliation request differs"))
			}
		} else if h != nil {
			h.RecoveryRequest = &handle.RecoveryRequest{OperationID: op, TargetClaimID: targetClaim, TargetOperationID: targetOp, RequestHash: hash, RequestNotAfter: deadline, Outcome: outcome, Evidence: evidence}
			if err := handle.Write(path, *h); err != nil {
				return s.handle(cmd, err)
			}
		}
		r, err := svc.Reconcile(ctx, creds, lease.ReconcileRequest{OperationID: op, TargetClaimID: targetClaim, TargetOperationID: targetOp, ExpectedRequestSHA256: expected, Outcome: outcome, Evidence: evidence, TTL: ttl, RequestNotAfter: deadline})
		if err != nil {
			if h != nil && isDefinitiveNoCommit(err) {
				h.RecoveryRequest = nil
				_ = handle.Write(path, *h)
			}
			return s.handle(cmd, mutationFailure(err, creds.ClaimID, op, path))
		}
		if h != nil {
			h.State = "ready"
			h.PendingRequest = nil
			h.RecoveryRequest = nil
			h.Revision = r.Revision
			h.ExpiresAt = r.ExpiresAt
			if err := handle.Write(path, *h); err != nil {
				return s.handle(cmd, committedHandleFailure(err, lease.Receipt{OperationID: r.OperationID, ClaimID: r.ResolverClaimID, Kind: "reconcile", Revision: r.Revision, Committed: true}, path, creds.ClaimID, op))
			}
		}
		return writeLedgerResult(s, cmd, "reconcile", map[string]any{"receipt": r})
	}
}
