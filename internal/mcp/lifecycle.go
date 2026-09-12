package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/instructions"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/resource"
	watchpkg "github.com/brettinternet/worklease/internal/watch"
)

func (s *Server) status(ctx context.Context, a map[string]any) (any, error) {
	b, e := s.open(ctx, false)
	if e != nil {
		return nil, e
	}
	defer b.st.Close()
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
		if h.AuthorityID != b.st.AuthorityID() {
			return nil, reason.New(reason.ReasonAuthorityMismatch, "lease authority does not match")
		}
		v, e := b.svc.Status(ctx, lease.Selector{ClaimID: h.ClaimID})
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
	v, e := b.svc.Status(ctx, lease.Selector{Resources: rs})
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
	defer b.st.Close()
	filter, _ := argString(a, "resource")
	v, e := b.svc.List(ctx, filter)
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
	b, e := s.open(ctx, true)
	if e != nil {
		return nil, e
	}
	defer b.st.Close()
	if h.AuthorityID != b.st.AuthorityID() {
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
	ttlv, e := argNumber(a, "ttl", b.svc.DefaultTTL().Seconds())
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
		inputs = map[string]any{"kind": kind, "authorityId": h.AuthorityID, "claimId": h.ClaimID, "ttl": ttlDuration(ttlv).Microseconds(), "requestNotAfter": deadline.UnixMicro()}
	case "checkpoint":
		inputs = map[string]any{"kind": kind, "authorityId": h.AuthorityID, "claimId": h.ClaimID, "ttl": ttlDuration(ttlv).Microseconds(), "checkpoint": json.RawMessage(checkpointRaw), "requestNotAfter": deadline.UnixMicro()}
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
		receipt, e = b.svc.Heartbeat(ctx, lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision}, lease.Renew{OperationID: id, TTL: ttlDuration(ttlv), RequestNotAfter: deadline, HoldUntil: h.HoldUntil})
	case "checkpoint":
		receipt, e = b.svc.Checkpoint(ctx, lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision}, lease.CheckpointRequest{OperationID: id, TTL: ttlDuration(ttlv), Data: checkpointRaw, RequestNotAfter: deadline, HoldUntil: h.HoldUntil})
	case "release":
		r, _ := argString(a, "reason")
		if strings.TrimSpace(r) == "" {
			r = "released"
		}
		receipt, e = b.svc.Release(ctx, lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision}, lease.ReleaseRequest{OperationID: id, Reason: r, RequestNotAfter: deadline})
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
	var rec lease.Receipt
	var err error
	creds := lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision}
	switch kind {
	case "heartbeat":
		rec, err = b.svc.Heartbeat(ctx, creds, lease.Renew{OperationID: p.OperationID, TTL: ttl, RequestNotAfter: p.RequestNotAfter, HoldUntil: h.HoldUntil})
	case "checkpoint":
		raw, _ := json.Marshal(p.Inputs["checkpoint"])
		rec, err = b.svc.Checkpoint(ctx, creds, lease.CheckpointRequest{OperationID: p.OperationID, TTL: ttl, Data: raw, RequestNotAfter: p.RequestNotAfter, HoldUntil: h.HoldUntil})
	case "release":
		rec, err = b.svc.Release(ctx, creds, lease.ReleaseRequest{OperationID: p.OperationID, Reason: pendingString(p.Inputs, "reason"), RequestNotAfter: p.RequestNotAfter})
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
	defer b.st.Close()
	if h.AuthorityID != b.st.AuthorityID() {
		return nil, reason.New(reason.ReasonAuthorityMismatch, "lease authority does not match")
	}
	expected := []string{}
	if v, ok := a["resources"]; ok {
		expected, e = stringList(v)
		if e != nil {
			return nil, e
		}
	}
	v, e := b.svc.Verify(ctx, lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision}, expected)
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
	defer b.st.Close()
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
	p, e := ledger.New(b.st).Events(ctx, cursor, limit)
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
	defer b.st.Close()
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
	r, e := watchpkg.Wait(ctx, b.st, watchpkg.Request{Cursor: cursor, Resources: resources, Until: until, Timeout: time.Duration(timeout * float64(time.Second)), PollInterval: s.options.PollInterval})
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
		return nil, reason.Invalid("topic must be loop or safety")
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
