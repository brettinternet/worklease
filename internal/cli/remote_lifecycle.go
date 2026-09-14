package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
	watchpkg "github.com/brettinternet/worklease/internal/watch"
	urfave "github.com/urfave/cli/v3"
)

func remoteTransfer(ctx context.Context, s *boundary, cmd *urfave.Command, backend *authorityContext, successorPath string, deadline time.Time) error {
	if strings.TrimSpace(cmd.String("claim-id")) != "" || strings.TrimSpace(cmd.String("token-file")) != "" || cmd.IsSet("token-fd") || cmd.IsSet("revision") {
		return s.handle(cmd, reason.Invalid("remote transfer requires a named predecessor handle"))
	}
	predecessorPath, err := acquireHandlePath(cmd, backend.Config)
	if err != nil {
		return s.handle(cmd, err)
	}
	if predecessorPath == successorPath {
		return s.handle(cmd, reason.Invalid("successor handle must differ from predecessor"))
	}
	h, err := handle.Read(predecessorPath)
	if err != nil {
		return s.handle(cmd, err)
	}
	if h.AuthorityID != backend.AuthorityID() {
		return s.handle(cmd, reason.New(reason.ReasonAuthorityMismatch, "handle authority does not match"))
	}
	toAgent, toSession := strings.TrimSpace(cmd.String("to-agent")), strings.TrimSpace(cmd.String("to-session"))
	if toAgent == "" || toSession == "" {
		return s.handle(cmd, reason.Invalid("successor identity is required"))
	}
	ttl := cmd.Duration("ttl")
	if ttl == 0 {
		ttl = backend.Config.TTL
	}
	request := lease.TransferRequest{OperationID: strings.TrimSpace(cmd.String("operation-id")), SuccessorHandlePath: successorPath, ToAgent: toAgent, ToSession: toSession, ToWorkKey: strings.TrimSpace(cmd.String("to-work-key")), TTL: ttl, RequestNotAfter: deadline}
	if request.ToWorkKey == "" {
		request.ToWorkKey = strings.Join(h.Resources, ",")
	}
	if h.PendingRequest != nil {
		if h.PendingRequest.Kind != "transfer" {
			return s.handle(cmd, reason.New(reason.ReasonHandleInUse, "pending request requires recovery"))
		}
		var saved struct {
			OperationID, SuccessorClaimID, ToAgent, ToSession, ToWorkKey string
			TTLMicros                                                    int64
		}
		if json.Unmarshal(h.PendingRequest.Request, &saved) != nil {
			return s.handle(cmd, reason.New(reason.ReasonOperationRequestMismatch, "pending transfer request is invalid"))
		}
		request.OperationID, request.SuccessorClaimID = saved.OperationID, saved.SuccessorClaimID
		request.SuccessorToken = h.PendingRequest.SuccessorToken
		request.ToAgent, request.ToSession, request.ToWorkKey = saved.ToAgent, saved.ToSession, saved.ToWorkKey
		request.TTL, request.RequestNotAfter = time.Duration(saved.TTLMicros)*time.Microsecond, h.PendingRequest.RequestNotAfter
	} else {
		if request.OperationID == "" {
			request.OperationID = randomHex(16)
		}
		request.SuccessorClaimID, request.SuccessorToken = randomHex(16), randomHex(32)
	}
	creds := lease.Credentials{AuthorityID: backend.AuthorityID(), ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision, HandlePath: predecessorPath, CredentialPath: backend.Profile.Credential.Path}
	request.SuccessorCredentialPath = backend.Profile.Credential.Path
	grant, err := backend.API.Transfer(ctx, creds, request)
	if err != nil {
		return s.handle(cmd, mutationFailure(err, h.ClaimID, request.OperationID, predecessorPath))
	}
	return writeLeaseResult(s, cmd, "transfer", transferFields(grant, successorPath))
}

func remoteAcquire(ctx context.Context, s *boundary, cmd *urfave.Command, backend *authorityContext, resources []string, localReplaceAllowed bool) error {
	if cmd.IsSet("poll-interval") {
		return s.handle(cmd, reason.Invalid("poll-interval is local-only"))
	}
	wait := cmd.Duration("wait")
	if wait < 0 || wait > time.Minute {
		return s.handle(cmd, reason.Invalid("wait must be between 0 and 60s"))
	}
	cfg := backend.Config
	ttl := cmd.Duration("ttl")
	if ttl == 0 {
		ttl = cfg.TTL
	}
	if ttl < time.Second || ttl > time.Hour {
		return s.handle(cmd, reason.Invalid("ttl must be between 1s and 1h"))
	}
	deadline, err := requestDeadlineCLI(cmd)
	if err != nil {
		return s.handle(cmd, err)
	}
	workKey := strings.TrimSpace(cmd.String("work-key"))
	if workKey == "" {
		workKey = strings.Join(resources, ",")
	}
	request := lease.AcquireRequest{AuthorityID: backend.AuthorityID(), Resources: resources, AgentID: cfg.AgentID, SessionID: cfg.SessionID, WorkKey: workKey, TTL: ttl, MaxHold: time.Hour, CoordinationOnly: cmd.Bool("coordination-only"), LocalReplaceAllowed: localReplaceAllowed, RequestNotAfter: deadline}
	if cmd.Bool("no-handle") {
		if strings.TrimSpace(cmd.String("claim-id")) == "" || strings.TrimSpace(cfg.SessionID) == "" {
			return s.handle(cmd, reason.Invalid("stateless acquire requires --claim-id and --session"))
		}
		token, err := tokenFromCommand(cmd, "token-file", "token-fd")
		if err != nil {
			return s.handle(cmd, err)
		}
		request.ClaimID, request.Token = cmd.String("claim-id"), token
	} else {
		if strings.TrimSpace(cmd.String("claim-id")) != "" || strings.TrimSpace(cmd.String("token-file")) != "" || cmd.IsSet("token-fd") {
			return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "explicit credentials require --no-handle"))
		}
		path, err := acquireHandlePath(cmd, cfg)
		if err != nil {
			return s.handle(cmd, err)
		}
		request.HandlePath = path
		if existing, readErr := handle.Read(path); readErr == nil {
			if existing.AuthorityID != backend.AuthorityID() {
				return s.handle(cmd, reason.New(reason.ReasonAuthorityMismatch, "handle authority does not match"))
			}
			if existing.State == "ready" && existing.ExpiresAt.After(time.Now()) {
				return s.handle(cmd, reason.New(reason.ReasonHandleInUse, "active handle is in use"))
			}
			request.ClaimID, request.Token = existing.ClaimID, existing.Token
			request.AgentID, request.SessionID = existing.AgentID, existing.SessionID
			request.Resources = append([]string(nil), existing.Resources...)
			if existing.PendingRequest != nil {
				request.RequestNotAfter = existing.PendingRequest.RequestNotAfter
			}
		} else if !errors.Is(readErr, os.ErrNotExist) && reason.As(readErr) == nil {
			return s.handle(cmd, readErr)
		}
		if request.ClaimID == "" {
			request.ClaimID, request.Token = randomHex(16), randomHex(32)
			if request.SessionID == "" {
				request.SessionID = randomHex(16)
			}
		}
	}
	waitUntil := time.Now().Add(wait)
	for {
		grant, err := backend.API.Acquire(ctx, request)
		if err == nil {
			return writeLeaseResult(s, cmd, "acquire", acquireFields(grant))
		}
		classified := reason.As(err)
		if wait == 0 || classified == nil || classified.Reason != reason.ReasonAlreadyClaimed || !time.Now().Before(waitUntil) {
			return s.handle(cmd, mutationFailure(err, request.ClaimID, request.ClaimID, request.HandlePath))
		}
		remaining := time.Until(waitUntil)
		if remaining > 30*time.Second {
			remaining = 30 * time.Second
		}
		if remaining <= 0 {
			return s.handle(cmd, mutationFailure(err, request.ClaimID, request.ClaimID, request.HandlePath))
		}
		if _, watchErr := backend.API.Watch(ctx, watchpkg.Request{Resources: resources, Until: "free", Timeout: remaining}); watchErr != nil {
			return s.handle(cmd, watchErr)
		}
	}

}
