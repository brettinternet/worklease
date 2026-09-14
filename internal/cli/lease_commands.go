package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	urfave "github.com/urfave/cli/v3"
)

var writeHandleFile func(string, handle.Handle) error

func writeHandle(lock *handle.Lock, path string, h handle.Handle) error {
	if writeHandleFile != nil {
		return writeHandleFile(path, h)
	}
	if lock != nil {
		return lock.Write(path, h)
	}
	return handle.Write(path, h)
}

func tokenFromCommand(cmd *urfave.Command, fileFlag, fdFlag string) (string, error) {
	path := strings.TrimSpace(cmd.String(fileFlag))
	fdSet := fdFlag != "" && cmd.IsSet(fdFlag)
	if (path != "") == fdSet {
		return "", reason.New(reason.ReasonCredentialSourceConflict, "exactly one token source is required")
	}
	if path != "" {
		return handle.ReadCredential(path)
	} else {
		fd := cmd.Int(fdFlag)
		if fd < 0 {
			return "", reason.New(reason.ReasonCredentialUnsafe, "token descriptor is invalid")
		}
		return handle.ReadCredentialFD(fd)
	}
}
func requestDeadlineCLI(cmd *urfave.Command) (time.Time, error) {
	value := strings.TrimSpace(cmd.String("request-not-after"))
	if value == "" {
		if strings.TrimSpace(cmd.String("handle")) != "" || strings.TrimSpace(os.Getenv("WORKLEASE_HANDLE")) != "" || (strings.TrimSpace(cmd.String("claim-id")) == "" && strings.TrimSpace(cmd.String("token-file")) == "" && !cmd.IsSet("token-fd") && !cmd.IsSet("revision")) {
			return time.Now().UTC().Add(24 * time.Hour), nil
		}
		return time.Time{}, reason.New(reason.ReasonReplayExpired, "request-not-after is required for stateless requests")
	}
	deadline, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, reason.Invalid("request-not-after must be RFC3339")
	}
	now := time.Now()
	if !deadline.After(now) || deadline.After(now.Add(24*time.Hour)) {
		return time.Time{}, reason.New(reason.ReasonReplayExpired, "request replay deadline must be within 24 hours")
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
		if operation == "status" || operation == "list" {
			return output.WritePublicSuccess(s.writer, operation, fields)
		}
		jsonFields := fields
		if operation == "checkpoint" || operation == "transfer" {
			jsonFields = make(map[string]any, len(fields))
			for key, value := range fields {
				jsonFields[key] = value
			}
			delete(jsonFields, "checkpointBytes")
			if operation == "transfer" {
				delete(jsonFields, "resources")
				delete(jsonFields, "successorHandle")
			}
		}
		return output.WriteSuccess(s.writer, operation, jsonFields)
	}
	switch operation {
	case "acquire":
		return writeAcquireText(s.writer, fields)
	case "status":
		return writeStatusText(s.writer, lease.Status{Claim: fields["claim"].(*lease.ClaimView), Resources: fields["resources"].([]lease.ResourceStatus)}, cmd.Bool("full"), output.ColorEnabled(s.writer))
	case "list":
		return writeListText(s.writer, fields["claims"].([]lease.ClaimView), cmd.Bool("full"), output.ColorEnabled(s.writer))
	case "heartbeat", "checkpoint", "release":
		return writeReceiptText(s.writer, fields["receipt"].(lease.Receipt), fields)
	case "transfer":
		return writeTransferText(s.writer, fields)
	case "verify":
		claim, _ := fields["claim"].(lease.ClaimView)
		unknown, _ := fields["unknownOperations"].([]string)
		return writeVerificationText(s.writer, &claim, unknown)
	default:
		return output.WriteText(s.writer, operation, fields)
	}
}
func acquireActionReal(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		in, err := ResolveResourceInput(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		explicitHandle := strings.TrimSpace(cmd.String("handle")) != "" || strings.TrimSpace(os.Getenv("WORKLEASE_HANDLE")) != ""
		if cmd.Bool("no-handle") && explicitHandle {
			return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "--no-handle cannot be mixed with a handle"))
		}
		backend, err := authorityFor(ctx, cmd, true)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer backend.Close()
		svc, st, cfg := backend.Local, backend.Store, backend.Config
		resources := make([]string, 0, len(in.Keys))
		for _, key := range in.Keys {
			resources = append(resources, key.Resource)
		}
		if backend.Remote {
			return remoteAcquire(ctx, s, cmd, backend, resources, in.Keys[0].LocalReplaceAllowed)
		}
		// --no-handle is the deliberately explicit stateless escape hatch.
		if cmd.Bool("no-handle") {
			if strings.TrimSpace(cmd.String("claim-id")) == "" {
				return s.handle(cmd, reason.Invalid("stateless acquire requires --claim-id"))
			}
			if strings.TrimSpace(cfg.SessionID) == "" {
				return s.handle(cmd, reason.Invalid("stateless acquire requires --session so retries preserve identity"))
			}
			token, e := tokenFromCommand(cmd, "token-file", "token-fd")
			if e != nil {
				return s.handle(cmd, e)
			}
			deadline, e := requestDeadlineCLI(cmd)
			if e != nil {
				return s.handle(cmd, e)
			}
			g, e := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), Resources: resources, Token: token, ClaimID: cmd.String("claim-id"), AgentID: cfg.AgentID, SessionID: cfg.SessionID, WorkKey: cmd.String("work-key"), TTL: cmd.Duration("ttl"), Wait: cmd.Duration("wait"), PollInterval: cmd.Duration("poll-interval"), CoordinationOnly: cmd.Bool("coordination-only"), LocalReplaceAllowed: in.Keys[0].LocalReplaceAllowed, RequestNotAfter: deadline})
			if e != nil {
				return s.handle(cmd, e)
			}
			return writeLeaseResult(s, cmd, "acquire", acquireFields(g))
		}
		if strings.TrimSpace(cmd.String("claim-id")) != "" || strings.TrimSpace(cmd.String("token-file")) != "" || cmd.IsSet("token-fd") {
			return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "explicit credentials require --no-handle"))
		}
		path, e := acquireHandlePath(cmd, cfg)
		if e != nil {
			return s.handle(cmd, e)
		}
		lock, e := handle.AcquireLock(ctx, path+".lock")
		if e != nil {
			return s.handle(cmd, e)
		}
		defer lock.Close()
		var h handle.Handle
		existing, re := lock.Read(path)
		if re == nil {
			h = existing
			if h.AuthorityID != st.AuthorityID() {
				return s.handle(cmd, reason.New(reason.ReasonAuthorityMismatch, "handle authority does not match"))
			}
		}
		claimID, token, session := h.ClaimID, h.Token, h.SessionID
		if h.State == "ready" && !h.ExpiresAt.After(time.Now()) {
			current, statusErr := svc.Status(ctx, lease.Selector{ClaimID: h.ClaimID})
			if statusErr != nil {
				return s.handle(cmd, statusErr)
			}
			if current.Claim != nil && current.Claim.Active {
				h.Revision, h.ExpiresAt = current.Claim.Revision, current.Claim.ExpiresAt
				if writeErr := writeHandle(lock, path, h); writeErr != nil {
					return s.handle(cmd, writeErr)
				}
				return s.handle(cmd, reason.New(reason.ReasonHandleInUse, "active handle is in use"))
			}
			claimID, token, session = "", "", ""
		}
		if claimID == "" {
			claimID = randomHex(16)
			token = randomHex(32)
			session = cfg.SessionID
			if session == "" {
				session = randomHex(16)
			}
		}
		if err := validateTokenCLI(token); err != nil {
			return s.handle(cmd, err)
		}
		ttl := cmd.Duration("ttl")
		if ttl == 0 {
			ttl = cfg.TTL
		}
		if ttl < time.Second || ttl > time.Hour {
			return s.handle(cmd, reason.Invalid("ttl must be between 1s and 1h"))
		}
		wait, poll := cmd.Duration("wait"), cmd.Duration("poll-interval")
		if wait < 0 || wait > time.Minute {
			return s.handle(cmd, reason.Invalid("wait must be between 0 and 60s"))
		}
		if poll == 0 {
			poll = cfg.PollInterval
		}
		if poll < 10*time.Millisecond || poll > 30*time.Second {
			return s.handle(cmd, reason.Invalid("poll interval is invalid"))
		}
		if err := validatePublicCLI("work key", cmd.String("work-key"), 1024, false); err != nil {
			return s.handle(cmd, err)
		}
		deadline, deadlineErr := requestDeadlineCLI(cmd)
		if deadlineErr != nil {
			return s.handle(cmd, deadlineErr)
		}
		if h.State == "pending" && h.PendingRequest != nil && !cmd.IsSet("request-not-after") {
			deadline = h.PendingRequest.RequestNotAfter
		}
		workKey := strings.TrimSpace(cmd.String("work-key"))
		if workKey == "" {
			workKey = strings.Join(resources, ",")
		}
		inputs := acquireInputs(st.AuthorityID(), claimID, resources, cfg.AgentID, session, workKey, ttl, wait, poll, cmd.Bool("coordination-only"), in.Keys[0].LocalReplaceAllowed, deadline)
		if h.State == "pending" {
			bindPendingHold(inputs, h.PendingRequest)
		}
		requestHash := acquireRequestHash(inputs)
		if h.State == "ready" && h.ExpiresAt.After(time.Now()) {
			return s.handle(cmd, reason.New(reason.ReasonHandleInUse, "active handle is in use"))
		}
		if h.RecoveryRequest != nil {
			return s.handle(cmd, reason.New(reason.ReasonHandleInUse, "pending recovery requires reconciliation"))
		}
		if h.State == "pending" {
			p := h.PendingRequest
			if p == nil || p.Kind != "acquire" || p.RequestHash != requestHash {
				return s.handle(cmd, reason.New(reason.ReasonOperationRequestMismatch, "pending acquire request differs"))
			}
			holdUntil, legacyHash := pendingHoldUntilCLI(p, &h)
			g, replayErr := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), Resources: h.Resources, Token: h.Token, ClaimID: h.ClaimID, AgentID: h.AgentID, SessionID: h.SessionID, WorkKey: inputString(p.Inputs, "workKey"), TTL: inputDuration(p.Inputs, "ttl", ttl), Wait: inputDuration(p.Inputs, "wait", 0), PollInterval: inputDuration(p.Inputs, "pollInterval", poll), CoordinationOnly: inputBool(p.Inputs, "coordinationOnly"), LocalReplaceAllowed: h.LocalReplaceAllowed, RequestNotAfter: p.RequestNotAfter, HoldUntil: holdUntil, LegacyRequestHash: legacyHash})
			if replayErr != nil {
				if isDefinitiveNoCommit(replayErr) {
					_ = lock.Remove(path)
				}
				return s.handle(cmd, mutationFailure(replayErr, h.ClaimID, p.OperationID, path))
			}
			ready := h
			ready.State, ready.Revision, ready.ExpiresAt = "ready", g.Revision, g.ExpiresAt
			ready.PendingRequest = nil
			if e := writeHandle(lock, path, ready); e != nil {
				return s.handle(cmd, committedHandleFailure(e, g.Receipt, path, h.ClaimID, p.OperationID))
			}
			return writeLeaseResult(s, cmd, "acquire", acquireFields(g))
		}
		pending := handle.Handle{SchemaVersion: 1, AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Resources: resources, AgentID: cfg.AgentID, SessionID: session, LocalReplaceAllowed: in.Keys[0].LocalReplaceAllowed, State: "pending", PendingRequest: &handle.PendingRequest{OperationID: claimID, Kind: "acquire", AuthorityID: st.AuthorityID(), ClaimID: claimID, RequestHash: requestHash, RequestNotAfter: deadline, Inputs: inputs}}
		if err := writeHandle(lock, path, pending); err != nil {
			return s.handle(cmd, err)
		}
		g, e := svc.Acquire(ctx, lease.AcquireRequest{AuthorityID: st.AuthorityID(), Resources: resources, Token: token, ClaimID: claimID, AgentID: cfg.AgentID, SessionID: session, WorkKey: workKey, TTL: ttl, Wait: wait, PollInterval: poll, CoordinationOnly: cmd.Bool("coordination-only"), LocalReplaceAllowed: in.Keys[0].LocalReplaceAllowed, RequestNotAfter: deadline})
		if e != nil {
			if isDefinitiveNoCommit(e) {
				_ = lock.Remove(path)
			}
			return s.handle(cmd, mutationFailure(e, claimID, claimID, path))
		}
		ready := pending
		ready.State = "ready"
		ready.Revision = g.Revision
		ready.ExpiresAt = g.ExpiresAt
		ready.PendingRequest = nil
		if err := writeHandle(lock, path, ready); err != nil {
			return s.handle(cmd, committedHandleFailure(err, g.Receipt, path, claimID, claimID))
		}
		return writeLeaseResult(s, cmd, "acquire", acquireFields(g))
	}
}
func acquireFields(g lease.Grant) map[string]any {
	return map[string]any{"claimId": g.ClaimID, "resources": g.Resources, "agentId": g.AgentID, "sessionId": g.SessionID, "workKey": g.WorkKey, "revision": g.Revision, "expiresAt": g.ExpiresAt, "authorityId": g.AuthorityID, "guarantee": g.Guarantee, "localReplaceAllowed": g.LocalReplaceAllowed, "receipt": g.Receipt, "recovery": g.Recovery, "unknownOperations": g.UnknownOperations}
}
func transferFields(g lease.Grant, successorHandle string) map[string]any {
	return map[string]any{"claimId": g.ClaimID, "resources": g.Resources, "agentId": g.AgentID, "sessionId": g.SessionID, "revision": g.Revision, "expiresAt": g.ExpiresAt, "authorityId": g.AuthorityID, "guarantee": g.Guarantee, "successorHandle": successorHandle}
}
func randomHex(n int) string {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		panic("secure random unavailable")
	}
	return hex.EncodeToString(b)
}
func acquireHandlePath(cmd *urfave.Command, cfg config.Config) (string, error) {
	if p := strings.TrimSpace(cmd.String("handle")); p != "" {
		return filepath.Clean(p), nil
	}
	if p := strings.TrimSpace(os.Getenv("WORKLEASE_HANDLE")); p != "" {
		return filepath.Clean(p), nil
	}
	root, e := handle.ContextRoot(mustGetwd(), nil)
	if e != nil {
		return "", e
	}
	return handle.ContextualPath(cfg.Home, root, cfg.SessionID), nil
}
func mustGetwd() string {
	v, e := os.Getwd()
	if e != nil {
		return "."
	}
	return v
}
func statusActionReal(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		claimID, resources := strings.TrimSpace(cmd.String("claim-id")), cmd.StringSlice("resource")
		selectedHandle := strings.TrimSpace(cmd.String("handle"))
		if selectedHandle == "" {
			selectedHandle = strings.TrimSpace(os.Getenv("WORKLEASE_HANDLE"))
		}
		if strings.TrimSpace(cmd.String("lease")) != "" || strings.TrimSpace(cmd.String("token-file")) != "" || cmd.IsSet("token-fd") || cmd.IsSet("revision") {
			return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "status public selection cannot include private credentials"))
		}
		if claimID != "" && len(resources) > 0 {
			return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "claim-id and resource status selection are exclusive"))
		}
		if selectedHandle != "" && (claimID != "" || len(resources) > 0) {
			return s.handle(cmd, reason.New(reason.ReasonCredentialSourceConflict, "handle and public status selection are exclusive"))
		}
		backend, err := authorityFor(ctx, cmd, false)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer backend.Close()
		cfg := backend.Config
		if selectedHandle != "" || (claimID == "" && len(resources) == 0) {
			path := selectedHandle
			if path == "" {
				path, err = acquireHandlePath(cmd, cfg)
				if err != nil {
					return s.handle(cmd, err)
				}
			}
			h, e := handle.Read(path)
			if e != nil {
				return s.handle(cmd, reason.New(reason.ReasonClaimSelectionMissing, "selected handle is unavailable"))
			}
			if h.AuthorityID != backend.AuthorityID() {
				return s.handle(cmd, reason.New(reason.ReasonAuthorityMismatch, "handle authority does not match"))
			}
			claimID = h.ClaimID
		}
		if claimID == "" && len(resources) == 0 {
			return s.handle(cmd, reason.New(reason.ReasonClaimSelectionMissing, "status requires claim-id or resource selection"))
		}
		v, err := backend.API.Status(ctx, lease.Selector{ClaimID: claimID, Resources: resources})
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeLeaseResult(s, cmd, "status", map[string]any{"claim": v.Claim, "claims": v.Claims, "resources": v.Resources})
	}
}
func listActionReal(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		resources := cmd.StringSlice("resource")
		if len(resources) > 1 {
			return s.handle(cmd, reason.Invalid("list accepts at most one resource"))
		}
		filter := ""
		if len(resources) == 1 {
			filter = resources[0]
		}
		backend, err := authorityFor(ctx, cmd, false)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer backend.Close()
		v, err := backend.API.List(ctx, filter, nil)
		if err != nil {
			return s.handle(cmd, err)
		}
		return writeLeaseResult(s, cmd, "list", map[string]any{"claims": v})
	}
}
func credsCLI(ctx context.Context, cmd *urfave.Command) (lease.Credentials, commandAuthority, io.Closer, *handle.Lock, *handle.Handle, string, error) {
	if err := ValidateSelection(cmd, true); err != nil {
		return lease.Credentials{}, nil, nil, nil, nil, "", err
	}
	backend, err := authorityFor(ctx, cmd, true)
	if err != nil {
		return lease.Credentials{}, nil, nil, nil, nil, "", err
	}
	cfg := backend.Config
	// Explicit credentials are the only stateless lifecycle mode. With no
	// explicit credential source, ordinary commands select the contextual file.
	if strings.TrimSpace(cmd.String("claim-id")) != "" || strings.TrimSpace(cmd.String("token-file")) != "" || cmd.IsSet("token-fd") || cmd.IsSet("revision") {
		token, e := tokenFromCommand(cmd, "token-file", "token-fd")
		if e != nil {
			backend.Close()
			return lease.Credentials{}, nil, nil, nil, nil, "", e
		}
		return lease.Credentials{AuthorityID: backend.AuthorityID(), ClaimID: cmd.String("claim-id"), Token: token, Revision: cmd.Int64("revision")}, backend.API, backend, nil, nil, "", nil
	}
	if strings.TrimSpace(cmd.String("lease")) != "" {
		backend.Close()
		return lease.Credentials{}, nil, nil, nil, nil, "", reason.New(reason.ReasonInvalidArgument, "private lease references are only available through MCP")
	}
	path := strings.TrimSpace(cmd.String("handle"))
	var e error
	if path == "" {
		path, e = acquireHandlePath(cmd, cfg)
		if e != nil {
			backend.Close()
			return lease.Credentials{}, nil, nil, nil, nil, "", e
		}
	}
	if backend.Remote {
		h, e := handle.Read(path)
		if e != nil {
			backend.Close()
			return lease.Credentials{}, nil, nil, nil, nil, "", e
		}
		if h.AuthorityID != backend.AuthorityID() {
			backend.Close()
			return lease.Credentials{}, nil, nil, nil, nil, "", reason.New(reason.ReasonAuthorityMismatch, "handle authority does not match")
		}
		return lease.Credentials{AuthorityID: backend.AuthorityID(), ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision, HandlePath: path, CredentialPath: backend.Profile.Credential.Path}, backend.API, backend, nil, &h, path, nil
	}
	lk, e := handle.AcquireLock(ctx, path+".lock")
	if e != nil {
		backend.Close()
		return lease.Credentials{}, nil, nil, nil, nil, "", e
	}
	h, e := lk.Read(path)
	if e != nil {
		lk.Close()
		backend.Close()
		return lease.Credentials{}, nil, nil, nil, nil, "", e
	}
	if h.AuthorityID != backend.AuthorityID() {
		lk.Close()
		backend.Close()
		return lease.Credentials{}, nil, nil, nil, nil, "", reason.New(reason.ReasonAuthorityMismatch, "handle authority does not match")
	}
	return lease.Credentials{AuthorityID: backend.AuthorityID(), ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision, HandlePath: path}, backend.API, backend, lk, &h, path, nil
}
func beginHandleMutation(path string, h *handle.Handle, kind, op string, deadline time.Time, inputs map[string]any, locks ...*handle.Lock) error {
	if h == nil || h.SchemaVersion == handle.RemoteSchemaVersion {
		return nil
	}
	if h.PendingRequest != nil || h.RecoveryRequest != nil {
		return reason.New(reason.ReasonHandleInUse, "pending request requires recovery")
	}
	if !validHexID(op) {
		return reason.Invalid("operation ID must be 32 lowercase hex characters")
	}
	if deadline.IsZero() {
		return reason.New(reason.ReasonReplayExpired, "request-not-after is required")
	}
	// Starting a fresh explicit CLI mutation takes the handle outside any MCP
	// automatic hold budget. Clear the old ceiling before the pending write so
	// interrupted CLI dispatch recovers the same unbounded intent.
	h.HoldUntil = time.Time{}
	h.State = "pending"
	h.PendingRequest = &handle.PendingRequest{OperationID: op, Kind: kind, AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: requestHashCLI(inputs), RequestNotAfter: deadline, Inputs: inputs}
	if len(locks) > 0 && locks[0] != nil {
		return locks[0].Write(path, *h)
	}
	return handle.Write(path, *h)
}
func operationID(cmd *urfave.Command, pending *handle.PendingRequest) (string, error) {
	value := strings.TrimSpace(cmd.String("operation-id"))
	if pending != nil {
		if value == "" {
			return pending.OperationID, nil
		}
		if value != pending.OperationID {
			return "", reason.New(reason.ReasonOperationRequestMismatch, "pending request differs")
		}
		return value, nil
	}
	if value == "" {
		return randomHex(16), nil
	}
	if !validHexID(value) {
		return "", reason.Invalid("operation ID must be 32 lowercase hex characters")
	}
	return value, nil
}
func validHexID(v string) bool {
	if len(v) != 32 {
		return false
	}
	b, err := hex.DecodeString(v)
	return err == nil && hex.EncodeToString(b) == v
}
func validatePublicCLI(name, value string, max int, required bool) error {
	if !required && value == "" {
		return nil
	}
	if value == "" || len([]byte(value)) > max || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n") {
		return reason.Invalid(name + " is invalid")
	}
	return nil
}
func acquireInputs(authority, claim string, resources []string, agent, session, work string, ttl, wait, poll time.Duration, coordination, local bool, deadline time.Time) map[string]any {
	return map[string]any{"kind": "acquire", "authorityId": authority, "claimId": claim, "resources": resources, "agentId": agent, "sessionId": session, "workKey": work, "ttl": ttl.Microseconds(), "wait": wait.Microseconds(), "pollInterval": poll.Microseconds(), "requestNotAfter": deadline.UTC().UnixMicro(), "localReplaceAllowed": local, "coordinationOnly": coordination}
}
func acquireRequestHash(in map[string]any) string {
	intent := map[string]any{"kind": in["kind"], "authorityId": in["authorityId"], "claimId": in["claimId"], "resources": in["resources"], "agentId": in["agentId"], "sessionId": in["sessionId"], "workKey": in["workKey"], "ttl": in["ttl"], "requestNotAfter": in["requestNotAfter"], "localReplaceAllowed": in["localReplaceAllowed"], "coordinationOnly": in["coordinationOnly"]}
	if holdUntil := inputInt64(in, "holdUntil"); holdUntil != 0 {
		intent["holdUntil"] = holdUntil
	}
	return requestHashCLI(intent)
}
func inputString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
func inputBool(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}
func inputInt64(m map[string]any, key string) int64 {
	if v, ok := m[key].(float64); ok {
		return int64(v)
	}
	if v, ok := m[key].(json.Number); ok {
		n, _ := v.Int64()
		return n
	}
	if v, ok := m[key].(int64); ok {
		return v
	}
	return 0
}
func inputDuration(m map[string]any, key string, fallback time.Duration) time.Duration {
	if v, ok := m[key].(float64); ok {
		return time.Duration(int64(v)) * time.Microsecond
	}
	if v, ok := m[key].(json.Number); ok {
		n, _ := v.Int64()
		return time.Duration(n) * time.Microsecond
	}
	if v, ok := m[key].(int64); ok {
		return time.Duration(v) * time.Microsecond
	}
	return fallback
}
func bindPendingHold(inputs map[string]any, pending *handle.PendingRequest) {
	if pending == nil {
		return
	}
	if holdUntil := inputInt64(pending.Inputs, "holdUntil"); holdUntil != 0 {
		inputs["holdUntil"] = holdUntil
	}
}

func pendingHoldUntilCLI(p *handle.PendingRequest, h *handle.Handle) (time.Time, string) {
	if holdUntil := inputInt64(p.Inputs, "holdUntil"); holdUntil != 0 {
		return time.UnixMicro(holdUntil).UTC(), ""
	}
	if h != nil && !h.HoldUntil.IsZero() {
		return h.HoldUntil, p.RequestHash
	}
	return time.Time{}, ""
}

func inputBytes(m map[string]any, key string) []byte {
	if v, ok := m[key].(string); ok {
		return []byte(v)
	}
	if v, ok := m[key]; ok {
		b, _ := json.Marshal(v)
		return b
	}
	return nil
}
func mutationFailure(err error, claim, op, path string) error {
	if err == nil {
		return nil
	}
	if e := reason.As(err); e != nil {
		e.With("claimId", claim).With("operationId", op).With("pendingPath", path)
		// A guard that already committed its started intent reports its own
		// commit state; only classify errors that have not been classified.
		if _, classified := e.Details["commitState"]; !classified {
			state := "not-committed"
			if !isDefinitiveNoCommit(err) {
				state = "unknown"
			}
			e.With("commitState", state)
		}
	}
	return err
}
func isDefinitiveNoCommit(err error) bool { return reason.DefinitiveNoCommit(err) }
func committedHandleFailure(err error, receipt lease.Receipt, path, claim, op string) error {
	e := reason.New(reason.ReasonHandleWriteFailed, "claim committed but handle could not be updated").With("claimId", claim).With("operationId", op).With("commitState", "committed").With("pendingPath", path)
	if receipt.OperationID != "" {
		e.With("receipt", receipt)
	}
	return e
}
func clearPending(path string, h *handle.Handle, locks ...*handle.Lock) {
	if h != nil && h.SchemaVersion == handle.RemoteSchemaVersion {
		return
	}
	if len(locks) > 0 && locks[0] != nil {
		_ = locks[0].ClearPending(path, h)
		return
	}
	_ = handle.ClearPending(path, h)
}

// recoverPendingMutation replays only the exact request retained in a handle.
// It never constructs a new operation or deadline.
func recoverPendingMutation(ctx context.Context, svc commandAuthority, c lease.Credentials, path string, h *handle.Handle, kind string, currentHash string, locks ...*handle.Lock) (lease.Receipt, bool, error) {
	if h == nil || h.SchemaVersion == handle.RemoteSchemaVersion || h.State != "pending" {
		return lease.Receipt{}, false, nil
	}
	p := h.PendingRequest
	if p == nil || p.Kind != kind {
		return lease.Receipt{}, true, reason.New(reason.ReasonOperationRequestMismatch, "pending request differs")
	}
	if p.RequestHash != currentHash {
		return lease.Receipt{}, true, reason.New(reason.ReasonOperationRequestMismatch, "pending request differs")
	}
	var r lease.Receipt
	var err error
	switch kind {
	case "heartbeat":
		holdUntil, legacyHash := pendingHoldUntilCLI(p, h)
		r, err = svc.Heartbeat(ctx, c, lease.Renew{OperationID: p.OperationID, TTL: inputDuration(p.Inputs, "ttl", 0), RequestNotAfter: p.RequestNotAfter, HoldUntil: holdUntil, LegacyRequestHash: legacyHash})
	case "checkpoint":
		holdUntil, legacyHash := pendingHoldUntilCLI(p, h)
		r, err = svc.Checkpoint(ctx, c, lease.CheckpointRequest{OperationID: p.OperationID, TTL: inputDuration(p.Inputs, "ttl", 0), Data: inputBytes(p.Inputs, "checkpoint"), RequestNotAfter: p.RequestNotAfter, HoldUntil: holdUntil, LegacyRequestHash: legacyHash})
	case "release":
		r, err = svc.Release(ctx, c, lease.ReleaseRequest{OperationID: p.OperationID, Reason: inputString(p.Inputs, "reason"), RequestNotAfter: p.RequestNotAfter})
	default:
		return lease.Receipt{}, true, reason.New(reason.ReasonOperationRequestMismatch, "pending request cannot be replayed here")
	}
	if err != nil {
		if isDefinitiveNoCommit(err) {
			clearPending(path, h, locks...)
		}
		return lease.Receipt{}, true, mutationFailure(err, c.ClaimID, p.OperationID, path)
	}
	return r, true, nil
}
func requestHashCLI(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func finishHandleMutation(path string, h *handle.Handle, r lease.Receipt, locks ...*handle.Lock) error {
	if h == nil || h.SchemaVersion == handle.RemoteSchemaVersion {
		return nil
	}
	h.State = "ready"
	h.PendingRequest = nil
	if r.Revision > h.Revision {
		h.Revision = r.Revision
	}
	if v, ok := r.Result["expiresAt"].(string); ok {
		if t, e := time.Parse(time.RFC3339Nano, v); e == nil {
			h.ExpiresAt = t
		}
	}
	if len(locks) > 0 && locks[0] != nil {
		return locks[0].Write(path, *h)
	}
	return handle.Write(path, *h)
}
func heartbeatActionReal(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		c, svc, st, lk, h, hp, err := credsCLI(ctx, cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		defer func() {
			if lk != nil {
				lk.Close()
			}
		}()
		if h != nil && h.RecoveryRequest != nil {
			return s.handle(cmd, reason.New(reason.ReasonHandleInUse, "pending recovery requires reconciliation"))
		}
		deadline, err := requestDeadlineCLI(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		if h != nil && h.PendingRequest != nil && !cmd.IsSet("request-not-after") {
			deadline = h.PendingRequest.RequestNotAfter
		}
		ttl := cmd.Duration("ttl")
		if ttl == 0 {
			ttl = svc.DefaultTTL()
		}
		if ttl < time.Second || ttl > time.Hour {
			return s.handle(cmd, reason.Invalid("ttl must be between 1s and 1h"))
		}
		var pending *handle.PendingRequest
		if h != nil {
			pending = h.PendingRequest
		}
		if h == nil && c.HandlePath == "" && strings.TrimSpace(cmd.String("operation-id")) == "" {
			return s.handle(cmd, reason.Invalid("stateless mutations require --operation-id"))
		}
		op, err := operationID(cmd, pending)
		if err != nil {
			return s.handle(cmd, err)
		}
		inputs := map[string]any{"kind": "heartbeat", "authorityId": c.AuthorityID, "claimId": c.ClaimID, "ttl": ttl.Microseconds(), "requestNotAfter": deadline.UTC().UnixMicro()}
		bindPendingHold(inputs, pending)
		if r, recovering, e := recoverPendingMutation(ctx, svc, c, hp, h, "heartbeat", requestHashCLI(inputs), lk); recovering {
			if e != nil {
				return s.handle(cmd, e)
			}
			if e = finishHandleMutation(hp, h, r, lk); e != nil {
				return s.handle(cmd, committedHandleFailure(e, r, hp, c.ClaimID, r.OperationID))
			}
			return writeLeaseResult(s, cmd, "heartbeat", map[string]any{"receipt": r})
		}
		if err := beginHandleMutation(hp, h, "heartbeat", op, deadline, inputs, lk); err != nil {
			return s.handle(cmd, err)
		}
		r, err := svc.Heartbeat(ctx, c, lease.Renew{OperationID: op, TTL: ttl, RequestNotAfter: deadline})
		if err != nil {
			if isDefinitiveNoCommit(err) {
				clearPending(hp, h, lk)
			}
			return s.handle(cmd, mutationFailure(err, c.ClaimID, op, hp))
		}
		if err := finishHandleMutation(hp, h, r, lk); err != nil {
			return s.handle(cmd, committedHandleFailure(err, r, hp, c.ClaimID, op))
		}
		return writeLeaseResult(s, cmd, "heartbeat", map[string]any{"receipt": r})
	}
}
func checkpointActionReal(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		c, svc, st, lk, h, hp, err := credsCLI(ctx, cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		defer func() {
			if lk != nil {
				lk.Close()
			}
		}()
		if h != nil && h.RecoveryRequest != nil {
			return s.handle(cmd, reason.New(reason.ReasonHandleInUse, "pending recovery requires reconciliation"))
		}
		data := []byte(cmd.String("data"))
		if p := cmd.String("data-file"); p != "" {
			data, err = os.ReadFile(p)
			if err != nil {
				return s.handle(cmd, reason.New(reason.ReasonInvalidArgument, "checkpoint data file cannot be read"))
			}
		}
		data, normalizeErr := lease.NormalizeCheckpoint(data)
		if normalizeErr != nil || len(data) == 0 || len(data) > 8*1024 || strings.Contains(string(data), c.Token) {
			return s.handle(cmd, reason.Invalid("checkpoint must be canonical JSON no larger than 8 KiB"))
		}
		deadline, err := requestDeadlineCLI(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		if h != nil && h.PendingRequest != nil && !cmd.IsSet("request-not-after") {
			deadline = h.PendingRequest.RequestNotAfter
		}
		ttl := cmd.Duration("ttl")
		if ttl == 0 {
			ttl = svc.DefaultTTL()
		}
		if ttl < time.Second || ttl > time.Hour {
			return s.handle(cmd, reason.Invalid("ttl must be between 1s and 1h"))
		}
		var pending *handle.PendingRequest
		if h != nil {
			pending = h.PendingRequest
		}
		if h == nil && c.HandlePath == "" && strings.TrimSpace(cmd.String("operation-id")) == "" {
			return s.handle(cmd, reason.Invalid("stateless mutations require --operation-id"))
		}
		op, err := operationID(cmd, pending)
		if err != nil {
			return s.handle(cmd, err)
		}
		inputs := map[string]any{"kind": "checkpoint", "authorityId": c.AuthorityID, "claimId": c.ClaimID, "ttl": ttl.Microseconds(), "checkpoint": json.RawMessage(data), "requestNotAfter": deadline.UTC().UnixMicro()}
		bindPendingHold(inputs, pending)
		if r, recovering, e := recoverPendingMutation(ctx, svc, c, hp, h, "checkpoint", requestHashCLI(inputs), lk); recovering {
			if e != nil {
				return s.handle(cmd, e)
			}
			if e = finishHandleMutation(hp, h, r, lk); e != nil {
				return s.handle(cmd, committedHandleFailure(e, r, hp, c.ClaimID, r.OperationID))
			}
			return writeLeaseResult(s, cmd, "checkpoint", map[string]any{"receipt": r, "checkpointBytes": len(data)})
		}
		if err := beginHandleMutation(hp, h, "checkpoint", op, deadline, inputs, lk); err != nil {
			return s.handle(cmd, err)
		}
		r, err := svc.Checkpoint(ctx, c, lease.CheckpointRequest{OperationID: op, TTL: ttl, Data: data, RequestNotAfter: deadline})
		if err != nil {
			if isDefinitiveNoCommit(err) {
				clearPending(hp, h, lk)
			}
			return s.handle(cmd, mutationFailure(err, c.ClaimID, op, hp))
		}
		if err := finishHandleMutation(hp, h, r, lk); err != nil {
			return s.handle(cmd, committedHandleFailure(err, r, hp, c.ClaimID, op))
		}
		return writeLeaseResult(s, cmd, "checkpoint", map[string]any{"receipt": r, "checkpointBytes": len(data)})
	}
}
func releaseActionReal(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		c, svc, st, lk, h, hp, err := credsCLI(ctx, cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer st.Close()
		defer func() {
			if lk != nil {
				lk.Close()
			}
		}()
		if h != nil && h.RecoveryRequest != nil {
			return s.handle(cmd, reason.New(reason.ReasonHandleInUse, "pending recovery requires reconciliation"))
		}
		deadline, err := requestDeadlineCLI(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		if h != nil && h.PendingRequest != nil && !cmd.IsSet("request-not-after") {
			deadline = h.PendingRequest.RequestNotAfter
		}
		reasonText := strings.TrimSpace(cmd.String("reason"))
		if reasonText == "" {
			reasonText = "released"
		}
		if err := validatePublicCLI("release reason", reasonText, 1024, true); err != nil || strings.Contains(reasonText, c.Token) {
			if err == nil {
				err = reason.Invalid("release reason is invalid")
			}
			return s.handle(cmd, err)
		}
		var pending *handle.PendingRequest
		if h != nil {
			pending = h.PendingRequest
		}
		if h == nil && c.HandlePath == "" && strings.TrimSpace(cmd.String("operation-id")) == "" {
			return s.handle(cmd, reason.Invalid("stateless mutations require --operation-id"))
		}
		op, err := operationID(cmd, pending)
		if err != nil {
			return s.handle(cmd, err)
		}
		inputs := map[string]any{"kind": "release", "authorityId": c.AuthorityID, "claimId": c.ClaimID, "reason": reasonText, "requestNotAfter": deadline.UTC().UnixMicro()}
		if r, recovering, e := recoverPendingMutation(ctx, svc, c, hp, h, "release", requestHashCLI(inputs), lk); recovering {
			if e != nil {
				return s.handle(cmd, e)
			}
			if e = lk.Remove(hp); e != nil {
				return s.handle(cmd, committedHandleFailure(e, r, hp, c.ClaimID, r.OperationID))
			}
			return writeLeaseResult(s, cmd, "release", map[string]any{"receipt": r})
		}
		if err := beginHandleMutation(hp, h, "release", op, deadline, inputs, lk); err != nil {
			return s.handle(cmd, err)
		}
		r, err := svc.Release(ctx, c, lease.ReleaseRequest{OperationID: op, Reason: reasonText, RequestNotAfter: deadline})
		if err != nil {
			if isDefinitiveNoCommit(err) {
				clearPending(hp, h, lk)
			}
			return s.handle(cmd, mutationFailure(err, c.ClaimID, op, hp))
		}
		if h != nil {
			var removeErr error
			if lk != nil {
				removeErr = lk.Remove(hp)
			} else {
				removeErr = handle.Remove(hp)
			}
			if removeErr != nil {
				return s.handle(cmd, committedHandleFailure(removeErr, r, hp, c.ClaimID, op))
			}
		}
		return writeLeaseResult(s, cmd, "release", map[string]any{"receipt": r})
	}
}
func lockForPath(locks []*handle.Lock, path string) *handle.Lock {
	for _, lock := range locks {
		if lock != nil && lock.Matches(path) {
			return lock
		}
	}
	return nil
}

func transferActionReal(s *boundary) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		if strings.TrimSpace(cmd.String("successor-handle")) == "" {
			return s.handle(cmd, reason.Invalid("transfer requires --successor-handle"))
		}
		if err := ValidateSelection(cmd, true); err != nil {
			return s.handle(cmd, err)
		}
		deadline, err := requestDeadlineCLI(cmd)
		if err != nil {
			return s.handle(cmd, err)
		}
		backend, err := authorityFor(ctx, cmd, true)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer backend.Close()
		svc, st, cfg := backend.Local, backend.Store, backend.Config
		successorPath := filepath.Clean(strings.TrimSpace(cmd.String("successor-handle")))
		if backend.Remote {
			return remoteTransfer(ctx, s, cmd, backend, successorPath, deadline)
		}
		explicit := strings.TrimSpace(cmd.String("claim-id")) != "" || strings.TrimSpace(cmd.String("token-file")) != "" || cmd.IsSet("token-fd") || cmd.IsSet("revision")
		predecessorPath := ""
		if !explicit {
			predecessorPath, err = acquireHandlePath(cmd, cfg)
			if err != nil {
				return s.handle(cmd, err)
			}
			if filepath.Clean(predecessorPath) == successorPath {
				return s.handle(cmd, reason.Invalid("successor handle must differ from predecessor"))
			}
		}
		lockPaths := []string{successorPath + ".lock"}
		if predecessorPath != "" {
			lockPaths = append(lockPaths, predecessorPath+".lock")
		}
		locks, err := handle.AcquireLocks(ctx, lockPaths...)
		if err != nil {
			return s.handle(cmd, err)
		}
		defer func() {
			for _, l := range locks {
				_ = l.Close()
			}
		}()
		predecessorLock := lockForPath(locks, predecessorPath)
		successorLock := lockForPath(locks, successorPath)
		var h *handle.Handle
		hp := predecessorPath
		var c lease.Credentials
		if explicit {
			token, e := tokenFromCommand(cmd, "token-file", "token-fd")
			if e != nil {
				return s.handle(cmd, e)
			}
			c = lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: cmd.String("claim-id"), Token: token, Revision: cmd.Int64("revision")}
			if successor, readErr := successorLock.Read(successorPath); readErr == nil && successor.State == "pending" && successor.PendingRequest != nil && successor.PendingRequest.Kind == "transfer" && inputString(successor.PendingRequest.Inputs, "claimId") == c.ClaimID {
				if successor.AuthorityID != st.AuthorityID() {
					return s.handle(cmd, reason.New(reason.ReasonAuthorityMismatch, "successor handle authority does not match"))
				}
				if successor.RecoveryRequest != nil {
					return s.handle(cmd, reason.New(reason.ReasonHandleInUse, "pending recovery requires reconciliation"))
				}
				h = &handle.Handle{SchemaVersion: 1, AuthorityID: c.AuthorityID, ClaimID: c.ClaimID, Token: c.Token, Revision: c.Revision, Resources: append([]string(nil), successor.Resources...), ExpiresAt: time.Now().Add(time.Second), AgentID: "stateless", SessionID: "stateless", LocalReplaceAllowed: successor.LocalReplaceAllowed, State: "ready"}
			} else {
				status, statusErr := svc.Status(ctx, lease.Selector{ClaimID: c.ClaimID})
				if statusErr != nil || status.Claim == nil {
					return s.handle(cmd, reason.New(reason.ReasonStaleClaim, "claim is not current"))
				}
				h = &handle.Handle{SchemaVersion: 1, AuthorityID: c.AuthorityID, ClaimID: c.ClaimID, Token: c.Token, Revision: c.Revision, Resources: append([]string(nil), status.Claim.Resources...), ExpiresAt: status.Claim.ExpiresAt, AgentID: status.Claim.AgentID, SessionID: status.Claim.SessionID, LocalReplaceAllowed: status.Claim.LocalReplaceAllowed, State: "ready"}
			}
			hp = ""
		} else {
			loaded, e := predecessorLock.Read(predecessorPath)
			if e != nil {
				return s.handle(cmd, e)
			}
			if loaded.AuthorityID != st.AuthorityID() {
				return s.handle(cmd, reason.New(reason.ReasonAuthorityMismatch, "handle authority does not match"))
			}
			h = &loaded
			c = lease.Credentials{AuthorityID: loaded.AuthorityID, ClaimID: loaded.ClaimID, Token: loaded.Token, Revision: loaded.Revision}
		}
		if h != nil && h.RecoveryRequest != nil {
			return s.handle(cmd, reason.New(reason.ReasonHandleInUse, "pending recovery requires reconciliation"))
		}
		if h != nil && h.PendingRequest != nil && h.PendingRequest.Kind != "transfer" {
			return s.handle(cmd, reason.New(reason.ReasonHandleInUse, "pending request requires recovery"))
		}
		if h != nil && h.PendingRequest != nil && h.PendingRequest.Kind == "transfer" {
			p := h.PendingRequest
			if _, e := operationID(cmd, p); e != nil {
				return s.handle(cmd, e)
			}
			successorID := inputString(p.Inputs, "successorClaimId")
			sh, readErr := successorLock.Read(successorPath)
			if readErr != nil {
				if classified := reason.As(readErr); classified == nil || classified.Reason != reason.ReasonHandleMalformed {
					return s.handle(cmd, readErr)
				}
				sh = handle.Handle{SchemaVersion: 1, AuthorityID: h.AuthorityID, ClaimID: successorID, Token: p.SuccessorToken, Resources: append([]string(nil), h.Resources...), AgentID: inputString(p.Inputs, "toAgent"), SessionID: inputString(p.Inputs, "toSession"), State: "pending", LocalReplaceAllowed: h.LocalReplaceAllowed, PendingRequest: &handle.PendingRequest{OperationID: p.OperationID, Kind: "transfer", AuthorityID: h.AuthorityID, ClaimID: successorID, RequestHash: p.RequestHash, RequestNotAfter: p.RequestNotAfter, Inputs: p.Inputs, SuccessorToken: p.SuccessorToken}}
				if e := successorLock.Write(successorPath, sh); e != nil {
					return s.handle(cmd, e)
				}
			}
			if sh.RecoveryRequest != nil {
				return s.handle(cmd, reason.New(reason.ReasonHandleInUse, "pending recovery requires reconciliation"))
			}
			if (sh.State != "pending" && sh.State != "ready") || sh.ClaimID != successorID || sh.PendingRequest != nil && sh.PendingRequest.OperationID != p.OperationID || sh.AuthorityID != st.AuthorityID() || sh.Token != p.SuccessorToken {
				return s.handle(cmd, reason.New(reason.ReasonOperationRequestMismatch, "pending transfer recovery requires its successor handle"))
			}
			if filepath.Clean(inputString(p.Inputs, "successorHandle")) != successorPath ||
				(cmd.String("to-agent") != "" && cmd.String("to-agent") != sh.AgentID) ||
				(cmd.String("to-session") != "" && cmd.String("to-session") != sh.SessionID) ||
				(cmd.IsSet("to-work-key") && cmd.String("to-work-key") != inputString(p.Inputs, "toWorkKey")) ||
				(cmd.IsSet("ttl") && cmd.Duration("ttl").Microseconds() != int64(inputDuration(p.Inputs, "ttl", 0)/time.Microsecond)) ||
				(cmd.IsSet("request-not-after") && !deadline.Equal(p.RequestNotAfter)) {
				return s.handle(cmd, reason.New(reason.ReasonOperationRequestMismatch, "pending transfer request differs"))
			}
			g, replayErr := svc.Transfer(ctx, c, lease.TransferRequest{OperationID: p.OperationID, SuccessorClaimID: sh.ClaimID, SuccessorToken: sh.Token, ToAgent: sh.AgentID, ToSession: sh.SessionID, ToWorkKey: inputString(p.Inputs, "toWorkKey"), TTL: inputDuration(p.Inputs, "ttl", svc.DefaultTTL()), RequestNotAfter: p.RequestNotAfter})
			if replayErr != nil {
				if isDefinitiveNoCommit(replayErr) {
					clearPending(hp, h, predecessorLock)
					_ = successorLock.Remove(successorPath)
				}
				return s.handle(cmd, mutationFailure(replayErr, c.ClaimID, p.OperationID, hp))
			}
			sh.State, sh.ExpiresAt, sh.PendingRequest = "ready", g.ExpiresAt, nil
			if g.Revision > sh.Revision {
				sh.Revision = g.Revision
			}
			if e := successorLock.Write(successorPath, sh); e != nil {
				return s.handle(cmd, committedHandleFailure(e, g.Receipt, successorPath, c.ClaimID, p.OperationID))
			}
			if e := predecessorLock.Remove(hp); e != nil {
				return s.handle(cmd, committedHandleFailure(e, g.Receipt, hp, c.ClaimID, p.OperationID))
			}
			return writeLeaseResult(s, cmd, "transfer", transferFields(g, successorPath))
		}
		if explicit {
			if _, statErr := os.Lstat(successorPath); statErr == nil {
				sh, readErr := successorLock.Read(successorPath)
				if readErr != nil {
					return s.handle(cmd, readErr)
				}
				p := sh.PendingRequest
				if sh.AuthorityID != st.AuthorityID() {
					return s.handle(cmd, reason.New(reason.ReasonAuthorityMismatch, "successor handle authority does not match"))
				}
				if sh.RecoveryRequest != nil {
					return s.handle(cmd, reason.New(reason.ReasonHandleInUse, "pending recovery requires reconciliation"))
				}
				if sh.State != "pending" || p == nil || p.Kind != "transfer" || inputString(p.Inputs, "claimId") != c.ClaimID || filepath.Clean(inputString(p.Inputs, "successorHandle")) != successorPath {
					return s.handle(cmd, reason.New(reason.ReasonHandleInUse, "successor handle is already in use"))
				}
				if _, e := operationID(cmd, p); e != nil {
					return s.handle(cmd, e)
				}
				if (cmd.String("to-agent") != "" && cmd.String("to-agent") != sh.AgentID) || (cmd.String("to-session") != "" && cmd.String("to-session") != sh.SessionID) || (cmd.IsSet("to-work-key") && cmd.String("to-work-key") != inputString(p.Inputs, "toWorkKey")) || (cmd.IsSet("ttl") && cmd.Duration("ttl") != inputDuration(p.Inputs, "ttl", 0)) || (cmd.IsSet("request-not-after") && !deadline.Equal(p.RequestNotAfter)) {
					return s.handle(cmd, reason.New(reason.ReasonOperationRequestMismatch, "pending transfer request differs"))
				}
				g, replayErr := svc.Transfer(ctx, c, lease.TransferRequest{OperationID: p.OperationID, SuccessorClaimID: sh.ClaimID, SuccessorToken: sh.Token, ToAgent: sh.AgentID, ToSession: sh.SessionID, ToWorkKey: inputString(p.Inputs, "toWorkKey"), TTL: inputDuration(p.Inputs, "ttl", svc.DefaultTTL()), RequestNotAfter: p.RequestNotAfter})
				if replayErr != nil {
					if isDefinitiveNoCommit(replayErr) {
						_ = successorLock.Remove(successorPath)
					}
					return s.handle(cmd, mutationFailure(replayErr, c.ClaimID, p.OperationID, successorPath))
				}
				sh.State, sh.ExpiresAt, sh.PendingRequest = "ready", g.ExpiresAt, nil
				if g.Revision > sh.Revision {
					sh.Revision = g.Revision
				}
				if e := successorLock.Write(successorPath, sh); e != nil {
					return s.handle(cmd, committedHandleFailure(e, g.Receipt, successorPath, c.ClaimID, p.OperationID))
				}
				return writeLeaseResult(s, cmd, "transfer", transferFields(g, successorPath))
			} else if !os.IsNotExist(statErr) {
				return s.handle(cmd, reason.New(reason.ReasonHandleUnsafe, "successor handle path is unsafe"))
			}
		}
		successorID, successor := randomHex(16), randomHex(32)
		if explicit && strings.TrimSpace(cmd.String("operation-id")) == "" {
			return s.handle(cmd, reason.Invalid("stateless mutations require --operation-id"))
		}
		op, err := operationID(cmd, nil)
		if err != nil {
			return s.handle(cmd, err)
		}
		if strings.TrimSpace(cmd.String("to-agent")) == "" || strings.TrimSpace(cmd.String("to-session")) == "" {
			return s.handle(cmd, reason.Invalid("successor identity is required"))
		}
		ttl := cmd.Duration("ttl")
		if ttl == 0 {
			ttl = svc.DefaultTTL()
		}
		if ttl < time.Second || ttl > time.Hour {
			return s.handle(cmd, reason.Invalid("ttl must be between 1s and 1h"))
		}
		toWorkKey := strings.TrimSpace(cmd.String("to-work-key"))
		if toWorkKey == "" {
			toWorkKey = strings.Join(h.Resources, ",")
		}
		inputs := map[string]any{"kind": "transfer", "authorityId": c.AuthorityID, "claimId": c.ClaimID, "successorClaimId": successorID, "successorHandle": successorPath, "toAgent": cmd.String("to-agent"), "toSession": cmd.String("to-session"), "toWorkKey": toWorkKey, "ttl": ttl.Microseconds(), "requestNotAfter": deadline.UTC().UnixMicro()}
		transferHash := requestHashCLI(inputs)
		sh := handle.Handle{SchemaVersion: 1, AuthorityID: c.AuthorityID, ClaimID: successorID, Token: successor, Resources: h.Resources, AgentID: cmd.String("to-agent"), SessionID: cmd.String("to-session"), State: "pending", LocalReplaceAllowed: h.LocalReplaceAllowed, PendingRequest: &handle.PendingRequest{OperationID: op, Kind: "transfer", AuthorityID: c.AuthorityID, ClaimID: successorID, RequestHash: transferHash, RequestNotAfter: deadline, Inputs: inputs, SuccessorToken: successor}}
		if _, e := os.Lstat(successorPath); e == nil {
			if _, readErr := successorLock.Read(successorPath); readErr != nil {
				return s.handle(cmd, readErr)
			}
			return s.handle(cmd, reason.New(reason.ReasonHandleInUse, "successor handle is already in use"))
		} else if !os.IsNotExist(e) {
			return s.handle(cmd, reason.New(reason.ReasonHandleUnsafe, "successor handle path is unsafe"))
		}
		predecessorReady := *h
		if hp != "" {
			h.State = "pending"
			h.PendingRequest = &handle.PendingRequest{OperationID: op, Kind: "transfer", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: transferHash, RequestNotAfter: deadline, Inputs: inputs, SuccessorToken: successor}
			if err := predecessorLock.Write(hp, *h); err != nil {
				return s.handle(cmd, err)
			}
		}
		if err := successorLock.Write(successorPath, sh); err != nil {
			if hp != "" {
				_ = predecessorLock.Write(hp, predecessorReady)
			}
			return s.handle(cmd, err)
		}
		g, err := svc.Transfer(ctx, c, lease.TransferRequest{OperationID: op, SuccessorClaimID: successorID, SuccessorToken: successor, ToAgent: cmd.String("to-agent"), ToSession: cmd.String("to-session"), ToWorkKey: toWorkKey, TTL: ttl, RequestNotAfter: deadline})
		if err != nil {
			if isDefinitiveNoCommit(err) {
				if hp != "" {
					clearPending(hp, h, predecessorLock)
				}
				_ = successorLock.Remove(successorPath)
			}
			pendingPath := hp
			if pendingPath == "" {
				pendingPath = successorPath
			}
			return s.handle(cmd, mutationFailure(err, c.ClaimID, op, pendingPath))
		}
		sh.State, sh.ExpiresAt, sh.PendingRequest = "ready", g.ExpiresAt, nil
		if g.Revision > sh.Revision {
			sh.Revision = g.Revision
		}
		if err := successorLock.Write(successorPath, sh); err != nil {
			return s.handle(cmd, committedHandleFailure(err, g.Receipt, successorPath, c.ClaimID, op))
		}
		if hp != "" {
			if err := predecessorLock.Remove(hp); err != nil {
				return s.handle(cmd, committedHandleFailure(err, g.Receipt, hp, c.ClaimID, op))
			}
		}
		return writeLeaseResult(s, cmd, "transfer", transferFields(g, successorPath))
	}
}
