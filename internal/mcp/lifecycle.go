package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/instructions"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/resource"
	watchpkg "github.com/brettinternet/worklease/internal/watch"
)

func (s *Server) status(ctx context.Context, a map[string]any) (any, error) {
	b, e := s.open(ctx, false)
	if e != nil {
		return nil, e
	}
	defer b.Close()
	ref, _ := argString(a, "lease")
	resources, has := a["resources"]
	if ref != "" && has {
		return nil, reason.New(reason.ReasonCredentialSourceConflict, "lease and resources are exclusive")
	}
	if ref != "" {
		h, _, lk, e := s.readLease(ctx, ref, false)
		if e != nil {
			return nil, e
		}
		defer lk.Close()
		if h.AuthorityID != b.id {
			return nil, reason.New(reason.ReasonAuthorityMismatch, "lease authority does not match")
		}
		v, e := b.authority.Status(ctx, lease.Selector{ClaimID: h.ClaimID})
		if e != nil {
			return nil, e
		}
		return map[string]any{"lease": ref, "claim": v.Claim, "unknownOperations": claimUnknown(v)}, nil
	}
	var rs []string
	if has {
		rs, e = stringList(resources)
		if e != nil {
			return nil, e
		}
	} else {
		return nil, reason.Invalid("status requires lease or resources")
	}
	v, e := b.authority.Status(ctx, lease.Selector{Resources: rs})
	if e != nil {
		return nil, e
	}
	return map[string]any{"resources": v.Resources, "claims": v.Claims, "unknownOperations": statusUnknown(v)}, nil
}
func claimUnknown(v lease.Status) []string {
	if v.Claim != nil {
		return v.Claim.UnknownOperations
	}
	return nil
}
func statusUnknown(v lease.Status) []string {
	var out []string
	for _, c := range v.Claims {
		out = append(out, c.UnknownOperations...)
	}
	return out
}
func (s *Server) list(ctx context.Context, a map[string]any) (any, error) {
	b, e := s.open(ctx, false)
	if e != nil {
		return nil, e
	}
	defer b.Close()
	filter, _ := argString(a, "resource")
	v, e := b.authority.List(ctx, filter, nil)
	if e != nil {
		return nil, e
	}
	return map[string]any{"claims": v}, nil
}

func (s *Server) mutation(ctx context.Context, a map[string]any, kind string) (any, error) {
	ref, e := argString(a, "lease")
	if e != nil || ref == "" {
		return nil, reason.Invalid("lease reference is required")
	}
	h, path, lk, e := s.readLease(ctx, ref, true)
	if e != nil {
		return nil, e
	}
	defer lk.Close()
	if h.SchemaVersion == handle.RemoteSchemaVersion {
		_ = lk.Close()
		return s.remoteMutation(ctx, a, kind, ref, path, h)
	}
	b, e := s.open(ctx, true)
	if e != nil {
		return nil, e
	}
	defer b.Close()
	if h.AuthorityID != b.id {
		return nil, reason.New(reason.ReasonAuthorityMismatch, "lease authority does not match")
	}
	if h.State == "pending" {
		return s.recoverPending(ctx, ref, h, path, lk, b, kind)
	}
	if h.State != "ready" {
		return nil, reason.New(reason.ReasonHandleInUse, "lease has a pending request")
	}
	if !h.HoldUntil.IsZero() && !time.Now().Before(h.HoldUntil) && kind != "release" {
		return nil, reason.New(reason.ReasonClaimExpired, "automatic hold deadline has passed")
	}
	ttlv, e := argNumber(a, "ttl", s.options.TTL.Seconds())
	if e != nil || ttlv <= 0 || ttlv > 3600 {
		return nil, reason.Invalid("ttl must be between 1s and 1h")
	}
	if !h.HoldUntil.IsZero() {
		remaining := time.Until(h.HoldUntil).Seconds()
		if remaining <= 0 && kind != "release" {
			return nil, reason.New(reason.ReasonClaimExpired, "automatic hold deadline has passed")
		}
		if ttlv > remaining {
			ttlv = remaining
		}
		if ttlv < 1 && kind != "release" {
			return nil, reason.New(reason.ReasonClaimExpired, "automatic hold deadline has passed")
		}
	}
	var checkpointRaw []byte
	if kind == "checkpoint" {
		data, ok := a["data"]
		if !ok {
			return nil, reason.Invalid("data is required")
		}
		raw, marshalErr := json.Marshal(data)
		if marshalErr != nil {
			return nil, reason.Invalid("checkpoint must be JSON")
		}
		checkpointRaw, e = lease.ValidateCheckpoint(raw, h.Token)
		if e != nil {
			return nil, e
		}
	}
	if kind == "release" {
		releaseReason, _ := argString(a, "reason")
		if h.Token != "" && strings.Contains(releaseReason, h.Token) {
			return nil, reason.Invalid("release reason must not contain the active credential")
		}
	}
	id := opID()
	deadline := s.deadline()
	var inputs map[string]any
	switch kind {
	case "heartbeat":
		inputs = map[string]any{"kind": kind, "authorityId": h.AuthorityID, "claimId": h.ClaimID, "ttl": ttlDuration(ttlv).Microseconds(), "requestNotAfter": deadline.UnixMicro(), "holdUntil": h.HoldUntil.UTC().UnixMicro()}
	case "checkpoint":
		inputs = map[string]any{"kind": kind, "authorityId": h.AuthorityID, "claimId": h.ClaimID, "ttl": ttlDuration(ttlv).Microseconds(), "checkpoint": json.RawMessage(checkpointRaw), "requestNotAfter": deadline.UnixMicro(), "holdUntil": h.HoldUntil.UTC().UnixMicro()}
	case "release":
		r, _ := argString(a, "reason")
		if strings.TrimSpace(r) == "" {
			r = "released"
		}
		inputs = map[string]any{"kind": kind, "authorityId": h.AuthorityID, "claimId": h.ClaimID, "reason": r, "requestNotAfter": deadline.UnixMicro()}
	}
	pending := &handle.PendingRequest{OperationID: id, Kind: kind, AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: hashValue(inputs), RequestNotAfter: deadline, Inputs: inputs}
	h.State = "pending"
	h.PendingRequest = pending
	if e = lk.Write(path, h); e != nil {
		return nil, e
	}
	var receipt lease.Receipt
	switch kind {
	case "heartbeat":
		receipt, e = b.authority.Heartbeat(ctx, lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision}, lease.Renew{OperationID: id, TTL: ttlDuration(ttlv), RequestNotAfter: deadline, HoldUntil: h.HoldUntil})
	case "checkpoint":
		receipt, e = b.authority.Checkpoint(ctx, lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision}, lease.CheckpointRequest{OperationID: id, TTL: ttlDuration(ttlv), Data: checkpointRaw, RequestNotAfter: deadline, HoldUntil: h.HoldUntil})
	case "release":
		r, _ := argString(a, "reason")
		if strings.TrimSpace(r) == "" {
			r = "released"
		}
		receipt, e = b.authority.Release(ctx, lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision}, lease.ReleaseRequest{OperationID: id, Reason: r, RequestNotAfter: deadline})
	}
	if e != nil {
		if reason.DefinitiveNoCommit(e) {
			// The request provably did not commit: restore the usable ready
			// credential so later calls are not wedged behind this request.
			_ = lk.ClearPending(path, &h)
		}
		return nil, mutationError(e, h.ClaimID, id, path)
	}
	if kind == "release" {
		stopRenewal(s, ref)
		if e = lk.Remove(path); e != nil {
			return nil, reason.New(reason.ReasonHandleWriteFailed, "lease handle could not be removed").With("claimId", h.ClaimID).With("operationId", id).With("commitState", "committed")
		}
		return map[string]any{"receipt": receipt, "lease": ref, "autoHeartbeat": "stopped"}, nil
	}
	h.State = "ready"
	h.PendingRequest = nil
	if receipt.Revision > h.Revision {
		h.Revision = receipt.Revision
	}
	if raw, ok := receipt.Result["expiresAt"].(string); ok {
		if t, er := time.Parse(time.RFC3339Nano, raw); er == nil {
			h.ExpiresAt = t
		}
	}
	if !h.HoldUntil.IsZero() && h.ExpiresAt.After(h.HoldUntil) {
		h.ExpiresAt = h.HoldUntil
	}
	if e = lk.Write(path, h); e != nil {
		return nil, reason.New(reason.ReasonHandleWriteFailed, "lease handle could not be updated").With("claimId", h.ClaimID).With("operationId", id).With("commitState", "committed")
	}
	state := "stopped"
	s.mu.Lock()
	if r := s.leases[ref]; r != nil {
		r.ttl = ttlDuration(ttlv)
		state = r.status
	}
	s.mu.Unlock()
	return map[string]any{"receipt": receipt, "lease": ref, "autoHeartbeat": state, "holdUntil": h.HoldUntil}, nil
}
func (s *Server) recoverPending(ctx context.Context, ref string, h handle.Handle, path string, lk *handle.Lock, b serviceBundle, kind string) (any, error) {
	p := h.PendingRequest
	if p == nil || p.Kind != kind {
		return nil, reason.New(reason.ReasonOperationRequestMismatch, "pending request differs")
	}
	ttl := time.Duration(pendingInt(p.Inputs, "ttl")) * time.Microsecond
	holdUntil, legacyHash := pendingHoldUntil(p, h.HoldUntil)
	var rec lease.Receipt
	var err error
	creds := lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision}
	switch kind {
	case "heartbeat":
		rec, err = b.authority.Heartbeat(ctx, creds, lease.Renew{OperationID: p.OperationID, TTL: ttl, RequestNotAfter: p.RequestNotAfter, HoldUntil: holdUntil, LegacyRequestHash: legacyHash})
	case "checkpoint":
		raw, _ := json.Marshal(p.Inputs["checkpoint"])
		rec, err = b.authority.Checkpoint(ctx, creds, lease.CheckpointRequest{OperationID: p.OperationID, TTL: ttl, Data: raw, RequestNotAfter: p.RequestNotAfter, HoldUntil: holdUntil, LegacyRequestHash: legacyHash})
	case "release":
		rec, err = b.authority.Release(ctx, creds, lease.ReleaseRequest{OperationID: p.OperationID, Reason: pendingString(p.Inputs, "reason"), RequestNotAfter: p.RequestNotAfter})
	default:
		return nil, reason.New(reason.ReasonOperationRequestMismatch, "pending request differs")
	}
	if err != nil {
		if reason.DefinitiveNoCommit(err) {
			_ = lk.ClearPending(path, &h)
		}
		return nil, mutationError(err, h.ClaimID, p.OperationID, path)
	}
	if kind == "release" {
		stopRenewal(s, ref)
		if e := lk.Remove(path); e != nil {
			return nil, e
		}
		return map[string]any{"lease": ref, "receipt": rec, "autoHeartbeat": "stopped"}, nil
	}
	h.State = "ready"
	h.PendingRequest = nil
	if rec.Revision > h.Revision {
		h.Revision = rec.Revision
	}
	if exp, ok := rec.Result["expiresAt"].(string); ok {
		h.ExpiresAt, _ = time.Parse(time.RFC3339Nano, exp)
	}
	if h.ExpiresAt.After(h.HoldUntil) {
		h.ExpiresAt = h.HoldUntil
	}
	if e := lk.Write(path, h); e != nil {
		return nil, e
	}
	s.mu.Lock()
	status := "stopped"
	if r := s.leases[ref]; r != nil {
		status = r.status
	}
	s.mu.Unlock()
	return map[string]any{"lease": ref, "receipt": rec, "autoHeartbeat": status, "holdUntil": h.HoldUntil}, nil
}
func clearRemotePending(path string) {
	current, err := handle.Read(path)
	if err == nil {
		_ = handle.ClearPending(path, &current)
	}
}

func (s *Server) remoteMutation(ctx context.Context, a map[string]any, kind, ref, path string, h handle.Handle) (any, error) {
	if s.remote == nil || s.profile == nil || h.AuthorityID != s.profile.AuthorityID {
		return nil, reason.New(reason.ReasonAuthorityMismatch, "lease authority does not match")
	}
	if err := s.ensureRemoteCredential(); err != nil {
		return nil, err
	}
	if h.PendingRequest != nil {
		if h.PendingRequest.Kind != kind {
			return nil, reason.New(reason.ReasonOperationRequestMismatch, "pending request differs")
		}
		response, err := s.remoteClient.ReplayHandle(ctx, path)
		if err != nil {
			if reason.DefinitiveNoCommit(err) {
				clearRemotePending(path)
			}
			return nil, mutationError(err, h.ClaimID, h.PendingRequest.OperationID, path)
		}
		var receipt lease.Receipt
		if err := json.Unmarshal(response.Result, &receipt); err != nil {
			return nil, reason.Invalid("remote mutation result is invalid")
		}
		return s.finishRemoteMutation(ref, path, h, kind, receipt)
	}
	if h.State != "ready" {
		return nil, reason.New(reason.ReasonHandleInUse, "lease has a pending request")
	}
	authorityNow, err := s.remoteUpperNow(ctx)
	if err != nil {
		return nil, err
	}
	if !h.HoldUntil.IsZero() && !authorityNow.Before(h.HoldUntil) && kind != "release" {
		return nil, reason.New(reason.ReasonClaimExpired, "automatic hold deadline has passed")
	}
	ttlv, err := argNumber(a, "ttl", s.options.TTL.Seconds())
	if err != nil || ttlv <= 0 || ttlv > 3600 {
		return nil, reason.Invalid("ttl must be between 1s and 1h")
	}
	if !h.HoldUntil.IsZero() {
		remaining := h.HoldUntil.Sub(authorityNow).Seconds()
		if ttlv > remaining {
			ttlv = remaining
		}
		if ttlv < 1 && kind != "release" {
			return nil, reason.New(reason.ReasonClaimExpired, "automatic hold deadline has passed")
		}
	}
	var checkpointRaw []byte
	if kind == "checkpoint" {
		data, ok := a["data"]
		if !ok {
			return nil, reason.Invalid("data is required")
		}
		checkpointRaw, err = json.Marshal(data)
		if err == nil {
			checkpointRaw, err = lease.ValidateCheckpoint(checkpointRaw, h.Token)
		}
		if err != nil {
			return nil, err
		}
	}
	releaseReason, _ := argString(a, "reason")
	if kind == "release" {
		if strings.TrimSpace(releaseReason) == "" {
			releaseReason = "released"
		}
		if strings.Contains(releaseReason, h.Token) {
			return nil, reason.Invalid("release reason must not contain the active credential")
		}
	}
	id := opID()
	creds := lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision, HandlePath: path, CredentialPath: s.profile.Credential.Path}
	var receipt lease.Receipt
	switch kind {
	case "heartbeat":
		receipt, err = s.remote.Heartbeat(ctx, creds, lease.Renew{OperationID: id, TTL: ttlDuration(ttlv)})
	case "checkpoint":
		receipt, err = s.remote.Checkpoint(ctx, creds, lease.CheckpointRequest{OperationID: id, TTL: ttlDuration(ttlv), Data: checkpointRaw})
	case "release":
		receipt, err = s.remote.Release(ctx, creds, lease.ReleaseRequest{OperationID: id, Reason: releaseReason})
	}
	if err != nil {
		if reason.DefinitiveNoCommit(err) {
			clearRemotePending(path)
		}
		return nil, mutationError(err, h.ClaimID, id, path)
	}
	return s.finishRemoteMutation(ref, path, h, kind, receipt)
}

func (s *Server) finishRemoteMutation(ref, path string, h handle.Handle, kind string, receipt lease.Receipt) (any, error) {
	if kind == "release" {
		stopRenewal(s, ref)
		if err := handle.Remove(path); err != nil {
			return nil, reason.New(reason.ReasonHandleWriteFailed, "lease handle could not be removed").With("claimId", h.ClaimID).With("operationId", receipt.OperationID).With("commitState", "committed")
		}
		return map[string]any{"receipt": receipt, "lease": ref, "autoHeartbeat": "stopped"}, nil
	}
	lock, err := handle.AcquireLock(context.Background(), path+".lock")
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	current, err := lock.Read(path)
	if err != nil {
		return nil, err
	}
	if raw, ok := receipt.Result["expiresAt"].(string); ok {
		if expires, parseErr := time.Parse(time.RFC3339Nano, raw); parseErr == nil {
			current.ExpiresAt = expires
		}
	}
	if !current.HoldUntil.IsZero() && current.ExpiresAt.After(current.HoldUntil) {
		current.ExpiresAt = current.HoldUntil
	}
	if err := lock.Write(path, current); err != nil {
		return nil, reason.New(reason.ReasonHandleWriteFailed, "lease handle could not be updated").With("claimId", h.ClaimID).With("operationId", receipt.OperationID).With("commitState", "committed")
	}
	status := "stopped"
	s.mu.Lock()
	if runtime := s.leases[ref]; runtime != nil {
		status = runtime.status
	}
	s.mu.Unlock()
	return map[string]any{"receipt": receipt, "lease": ref, "autoHeartbeat": status, "holdUntil": current.HoldUntil}, nil
}

func pendingHoldUntil(p *handle.PendingRequest, fallback time.Time) (time.Time, string) {
	if micros := pendingInt(p.Inputs, "holdUntil"); micros != 0 {
		return time.UnixMicro(micros).UTC(), ""
	}
	if !fallback.IsZero() {
		return fallback, p.RequestHash
	}
	return time.Time{}, ""
}

func pendingInt(m map[string]any, k string) int64 {
	switch v := m[k].(type) {
	case json.Number:
		n, _ := v.Int64()
		return n
	case float64:
		return int64(v)
	case int64:
		return v
	}
	return 0
}
func pendingString(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func mutationError(e error, claim, id, path string) error {
	if x := reason.As(e); x != nil {
		state := "not-committed"
		if !reason.DefinitiveNoCommit(e) {
			state = "unknown"
		}
		x.With("claimId", claim).With("operationId", id).With("pendingPath", path).With("commitState", state)
	}
	return e
}

func (s *Server) verify(ctx context.Context, a map[string]any) (any, error) {
	ref, e := argString(a, "lease")
	if e != nil || ref == "" {
		return nil, reason.Invalid("lease reference is required")
	}
	h, _, lk, e := s.readLease(ctx, ref, false)
	if e != nil {
		return nil, e
	}
	defer lk.Close()
	b, e := s.open(ctx, false)
	if e != nil {
		return nil, e
	}
	defer b.Close()
	if h.AuthorityID != b.id {
		return nil, reason.New(reason.ReasonAuthorityMismatch, "lease authority does not match")
	}
	expected := []string{}
	if v, ok := a["resources"]; ok {
		expected, e = stringList(v)
		if e != nil {
			return nil, e
		}
	}
	v, e := b.authority.Verify(ctx, lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision}, expected)
	if e != nil {
		return nil, e
	}
	return map[string]any{"lease": ref, "claim": v.Claim, "unknownOperations": v.UnknownOperations}, nil
}
func (s *Server) events(ctx context.Context, a map[string]any) (any, error) {
	b, e := s.open(ctx, false)
	if e != nil {
		return nil, e
	}
	defer b.Close()
	cursor, _ := argString(a, "cursor")
	limit := 0
	if v, ok := a["limit"]; ok {
		switch n := v.(type) {
		case json.Number:
			i, er := n.Int64()
			if er != nil {
				return nil, reason.Invalid("limit must be an integer")
			}
			limit = int(i)
		case float64:
			if n != float64(int(n)) {
				return nil, reason.Invalid("limit must be an integer")
			}
			limit = int(n)
		default:
			return nil, reason.Invalid("limit must be an integer")
		}
	}
	p, e := b.authority.Events(ctx, cursor, limit)
	if e != nil {
		return nil, e
	}
	return map[string]any{"authorityId": p.AuthorityID, "events": p.Events, "nextCursor": p.NextCursor, "gap": p.Gap}, nil
}
func (s *Server) watch(ctx context.Context, a map[string]any) (any, error) {
	b, e := s.open(ctx, false)
	if e != nil {
		return nil, e
	}
	defer b.Close()
	cursor, _ := argString(a, "cursor")
	until, _ := argString(a, "until")
	resources := []string{}
	if v, ok := a["resources"]; ok {
		resources, e = stringList(v)
		if e != nil {
			return nil, e
		}
	}
	timeout, e := argNumber(a, "timeout", 30)
	if e != nil || timeout < 0 || timeout > 60 {
		return nil, reason.Invalid("timeout must be between 0 and 60s")
	}
	totalTimeout := time.Duration(timeout * float64(time.Second))
	request := watchpkg.Request{Cursor: cursor, Resources: resources, Until: until, Timeout: totalTimeout, PollInterval: s.options.PollInterval}
	if b.remote && request.Timeout > 30*time.Second {
		request.Timeout = 30 * time.Second
	}
	r, e := b.authority.Watch(ctx, request)
	if b.remote && totalTimeout > 30*time.Second {
		deadline := time.Now().Add(totalTimeout - request.Timeout)
		for e == nil && r.TimedOut && !r.Gap {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				break
			}
			if remaining > 30*time.Second {
				remaining = 30 * time.Second
			}
			request.Timeout = remaining
			if r.NextCursor != "" {
				request.Cursor = r.NextCursor
			}
			r, e = b.authority.Watch(ctx, request)
		}
	}
	if e != nil {
		return nil, e
	}
	return map[string]any{"authorityId": r.AuthorityID, "cursor": r.Cursor, "nextCursor": r.NextCursor, "event": r.Event, "timedOut": r.TimedOut, "gap": r.Gap, "resetCursor": r.ResetCursor, "free": r.Free, "changed": r.Changed, "resources": r.Resources, "unresolvedPredecessor": r.UnresolvedPredecessor, "unresolvedOperations": r.UnresolvedOperations}, nil
}
func (s *Server) instructions(a map[string]any) (any, error) {
	topic, e := argString(a, "topic")
	if e != nil {
		return nil, e
	}
	lines, e := instructions.For(topic)
	if e != nil {
		return nil, reason.Invalid(e.Error())
	}
	return map[string]any{"lines": lines}, nil
}
func stringList(v any) ([]string, error) {
	var arr []any
	switch x := v.(type) {
	case []any:
		arr = x
	case []string:
		for _, s := range x {
			arr = append(arr, s)
		}
	default:
		return nil, reason.Invalid("resources must be an array")
	}
	if len(arr) == 0 || len(arr) > 32 {
		return nil, reason.Invalid("resources must contain 1 to 32 strings")
	}
	out := make([]string, len(arr))
	for i, x := range arr {
		var ok bool
		out[i], ok = x.(string)
		if !ok || strings.TrimSpace(out[i]) != out[i] || out[i] == "" {
			return nil, reason.Invalid("resources must contain valid strings")
		}
		if _, e := resource.Direct(out[i], false); e != nil {
			return nil, e
		}
	}
	return out, nil
}
