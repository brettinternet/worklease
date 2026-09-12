package cli

import (
	"context"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
	urfave "github.com/urfave/cli/v3"
)

func serviceFor(ctx context.Context, cmd *urfave.Command, write bool) (*lease.Service, *store.Store, config.Config, error) {
	cfg, err := config.Load(config.Input{Flags: map[string]string{"home": cmd.String("home"), "agent": cmd.String("agent"), "session": cmd.String("session"), "ttl": cmd.String("ttl"), "poll_interval": cmd.String("poll-interval"), "config": cmd.String("config")}})
	if err != nil {
		return nil, nil, cfg, err
	}
	st, err := store.Open(ctx, cfg.Home, store.Options{ReadOnly: !write})
	if err != nil {
		return nil, nil, cfg, err
	}
	return lease.New(st, nil, nil, lease.Defaults{TTL: cfg.TTL, PollInterval: cfg.PollInterval}), st, cfg, nil
}
func tokenFromCommand(cmd *urfave.Command, fileFlag, fdFlag string) (string, error) {
	path := strings.TrimSpace(cmd.String(fileFlag))
	fdSet := fdFlag != "" && cmd.IsSet(fdFlag)
	if (path != "") == fdSet {
		return "", reason.New(reason.ReasonCredentialSourceConflict, "exactly one token source is required")
	}
	var data []byte
	var err error
	if path != "" {
		file, openErr := os.Open(path)
		if openErr != nil {
			return "", reason.New(reason.ReasonCredentialUnsafe, "token source cannot be read safely")
		}
		data, err = io.ReadAll(io.LimitReader(file, 4097))
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
	} else {
		fd := cmd.Int(fdFlag)
		if fd < 0 {
			return "", reason.New(reason.ReasonCredentialUnsafe, "token descriptor is invalid")
		}
		file := os.NewFile(uintptr(fd), "credential")
		if file == nil {
			return "", reason.New(reason.ReasonCredentialUnsafe, "token descriptor is invalid")
		}
		data, err = io.ReadAll(io.LimitReader(file, 4097))
	}
	if err != nil || len(data) > 4096 {
		return "", reason.New(reason.ReasonCredentialUnsafe, "token source cannot be read safely")
	}
	value := strings.TrimSuffix(string(data), "\n")
	if err := validateTokenCLI(value); err != nil {
		return "", err
	}
	return value, nil
}
func requestDeadlineCLI(cmd *urfave.Command) (time.Time, error) {
	value := strings.TrimSpace(cmd.String("request-not-after"))
	if value == "" {
		return time.Time{}, reason.New(reason.ReasonReplayExpired, "request-not-after is required until handle-backed requests are available")
	}
	deadline, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, reason.Invalid("request-not-after must be RFC3339")
	}
	return deadline, nil
}
func validateTokenCLI(v string) error {
	if len(v) != 64 {
		return reason.New(reason.ReasonCredentialMalformed, "credential must be 64 lowercase hexadecimal characters")
	}
	if _, err := hex.DecodeString(v); err != nil || v != strings.ToLower(v) {
		return reason.New(reason.ReasonCredentialMalformed, "credential must be 64 lowercase hexadecimal characters")
	}
	return nil
}
func writeLeaseResult(s *boundary, cmd *urfave.Command, operation string, fields map[string]any) error {
	if s.jsonRequested(cmd) {
		return output.WriteSuccess(s.writer, operation, fields)
	}
	return output.WriteText(s.writer, operation, fields)
}
func acquireActionReal(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		in, err := ResolveResourceInput(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		if !cmd.Bool("no-handle") {
			return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "stateless acquire requires --no-handle until handles are available"))
		}
		svc, st, cfg, err := serviceFor(ctx, cmd, true)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		if strings.TrimSpace(cmd.String("handle")) != "" || strings.TrimSpace(os.Getenv("WORKLEASE_HANDLE")) != "" {
			return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "handle-backed acquire is unavailable until handle support is implemented"))
		}
		if strings.TrimSpace(cmd.String("claim-id")) == "" {
			return s.handle(cmd, reason.Invalid("stateless acquire requires --claim-id"))
		}
		token, err := tokenFromCommand(cmd, "token-file", "token-fd")
		if err != nil {
			return s.handle(cmd, err)
		}
		deadline, err := requestDeadlineCLI(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		resources := make([]string, 0, len(in.Keys))
		for _, key := range in.Keys {
			resources = append(resources, key.Resource)
		}
		g, err := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), Resources: resources, Token: token, ClaimID: cmd.String("claim-id"), AgentID: cfg.AgentID, SessionID: cfg.SessionID, WorkKey: cmd.String("work-key"), TTL: cmd.Duration("ttl"), Wait: cmd.Duration("wait"), PollInterval: cmd.Duration("poll-interval"), CoordinationOnly: cmd.Bool("coordination-only"), LocalReplaceAllowed: in.Keys[0].LocalReplaceAllowed, RequestNotAfter: deadline})
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeLeaseResult(s, cmd, "acquire", map[string]any{"claimId": g.ClaimID, "resources": g.Resources, "agentId": g.AgentID, "sessionId": g.SessionID, "workKey": g.WorkKey, "revision": g.Revision, "expiresAt": g.ExpiresAt, "authorityId": g.AuthorityID, "guarantee": g.Guarantee, "localReplaceAllowed": g.LocalReplaceAllowed, "receipt": g.Receipt, "recovery": g.Recovery, "unknownOperations": g.UnknownOperations})
	}
}
func statusActionReal(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		if strings.TrimSpace(cmd.String("handle")) != "" || strings.TrimSpace(cmd.String("lease")) != "" || strings.TrimSpace(os.Getenv("WORKLEASE_HANDLE")) != "" || strings.TrimSpace(cmd.String("token-file")) != "" || cmd.IsSet("token-fd") || cmd.IsSet("revision") {
			return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "status currently supports only public claim-id or resource selection"))
		}
		claimID, resources := strings.TrimSpace(cmd.String("claim-id")), cmd.StringSlice("resource")
		if claimID != "" && len(resources) > 0 {
			return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "claim-id and resource status selection are exclusive"))
		}
		if claimID == "" && len(resources) == 0 {
			return s.handle(cmd, reason.New(reason.ReasonClaimSelectionMissing, "status requires claim-id or resource selection"))
		}
		svc, st, _, err := serviceFor(ctx, cmd, false)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		v, err := svc.Status(ctx, lease.Selector{ClaimID: cmd.String("claim-id"), Resources: cmd.StringSlice("resource")})
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeLeaseResult(s, cmd, "status", map[string]any{"claim": v.Claim, "claims": v.Claims, "resources": v.Resources})
	}
}
func listActionReal(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		svc, st, _, err := serviceFor(ctx, cmd, false)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		v, err := svc.List(ctx, cmd.String("resource"))
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeLeaseResult(s, cmd, "list", map[string]any{"claims": v})
	}
}
func credsCLI(ctx context.Context, cmd *urfave.Command) (lease.Credentials, *lease.Service, *store.Store, error) {
	if err := ValidateSelection(cmd, true); err != nil {
		return lease.Credentials{}, nil, nil, err
	}
	if strings.TrimSpace(cmd.String("handle")) != "" || strings.TrimSpace(cmd.String("lease")) != "" || strings.TrimSpace(os.Getenv("WORKLEASE_HANDLE")) != "" {
		return lease.Credentials{}, nil, nil, reason.New(reason.ReasonCredentialSourceConflict, "handle and lease selection are unavailable until handle support is implemented")
	}
	token, err := tokenFromCommand(cmd, "token-file", "token-fd")
	if err != nil {
		return lease.Credentials{}, nil, nil, err
	}
	svc, st, _, err := serviceFor(ctx, cmd, true)
	if err != nil {
		return lease.Credentials{}, nil, nil, err
	}
	return lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: cmd.String("claim-id"), Token: token, Revision: cmd.Int64("revision")}, svc, st, nil
}
func heartbeatActionReal(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		c, svc, st, err := credsCLI(ctx, cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		deadline, err := requestDeadlineCLI(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		r, err := svc.Heartbeat(ctx, c, lease.Renew{OperationID: cmd.String("operation-id"), TTL: cmd.Duration("ttl"), RequestNotAfter: deadline})
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeLeaseResult(s, cmd, "heartbeat", map[string]any{"receipt": r})
	}
}
func checkpointActionReal(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		c, svc, st, err := credsCLI(ctx, cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		data := []byte(cmd.String("data"))
		if p := cmd.String("data-file"); p != "" {
			data, err = os.ReadFile(p)
			if err != nil {
				return s.handle(cmd, reason.New(reason.ReasonInvalidArgument, "checkpoint data file cannot be read"))
			}
		}
		deadline, err := requestDeadlineCLI(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		r, err := svc.Checkpoint(ctx, c, lease.CheckpointRequest{OperationID: cmd.String("operation-id"), TTL: cmd.Duration("ttl"), Data: data, RequestNotAfter: deadline})
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeLeaseResult(s, cmd, "checkpoint", map[string]any{"receipt": r})
	}
}
func releaseActionReal(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		c, svc, st, err := credsCLI(ctx, cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		deadline, err := requestDeadlineCLI(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		r, err := svc.Release(ctx, c, lease.ReleaseRequest{OperationID: cmd.String("operation-id"), Reason: cmd.String("reason"), RequestNotAfter: deadline})
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeLeaseResult(s, cmd, "release", map[string]any{"receipt": r})
	}
}
func transferActionReal(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		c, svc, st, err := credsCLI(ctx, cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		deadline, err := requestDeadlineCLI(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		if strings.TrimSpace(cmd.String("successor-handle")) != "" {
			return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "successor handles are unavailable until handle support is implemented"))
		}
		successor, err := tokenFromCommand(cmd, "successor-token-file", "")
		if err != nil {
			return s.handle(cmd, err)
		}
		successorID := strings.TrimSpace(cmd.String("successor-claim-id"))
		if successorID == "" {
			return s.handle(cmd, reason.Invalid("successor-claim-id is required"))
		}
		g, err := svc.Transfer(ctx, c, lease.TransferRequest{OperationID: cmd.String("operation-id"), SuccessorClaimID: successorID, SuccessorToken: successor, ToAgent: cmd.String("to-agent"), ToSession: cmd.String("to-session"), ToWorkKey: cmd.String("to-work-key"), TTL: cmd.Duration("ttl"), RequestNotAfter: deadline})
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeLeaseResult(s, cmd, "transfer", map[string]any{"claimId": g.ClaimID, "agentId": g.AgentID, "sessionId": g.SessionID, "revision": g.Revision, "expiresAt": g.ExpiresAt, "authorityId": g.AuthorityID, "guarantee": g.Guarantee})
	}
}
