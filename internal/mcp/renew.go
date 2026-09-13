package mcp

import (
	"context"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
)

func (s *Server) startRenewal(ref string, ttl time.Duration, hold time.Time) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &runtimeLease{ref: ref, path: s.handlePath(ref), ttl: ttl, holdUntil: hold, ctx: ctx, cancel: cancel, status: "active"}
	s.mu.Lock()
	s.leases[ref] = r
	s.mu.Unlock()
	go s.renewLoop(r)
}
func stopRenewal(s *Server, ref string) {
	s.mu.Lock()
	r := s.leases[ref]
	if r != nil {
		r.status = "stopped"
		r.cancel()
		delete(s.leases, ref)
	}
	s.mu.Unlock()
}

// renewLoop renews one lease this server acquired until its hold deadline,
// expiry, an ownership failure, release, or shutdown. Every exit path marks the
// runtime status stopped so tool results never report a dead renewer as active.
func (s *Server) renewLoop(r *runtimeLease) {
	defer s.markRenewalStopped(r)
	for {
		h, err := handle.Read(r.path)
		if err != nil {
			return
		}
		now := time.Now()
		if !r.holdUntil.After(now) || !h.ExpiresAt.After(now) {
			return
		}
		wait := time.Until(h.ExpiresAt) / 2
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		t := time.NewTimer(wait)
		select {
		case <-r.ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		if !r.holdUntil.After(time.Now()) {
			return
		}
		remaining := time.Until(r.holdUntil)
		s.mu.Lock()
		ttl := r.ttl
		s.mu.Unlock()
		if ttl > remaining {
			ttl = remaining
		}
		if ttl < time.Second {
			return
		}
		b, err := s.open(r.ctx, true)
		if err != nil {
			return
		}
		lk, err := handle.AcquireLock(r.ctx, r.path+".lock")
		if err != nil {
			b.st.Close()
			return
		}
		h, err = lk.Read(r.path)
		if err == nil && h.State == "pending" {
			lk.Close()
			b.st.Close()
			return
		}
		if err == nil && h.State == "ready" && h.HoldUntil.After(time.Now()) {
			deadline := time.Now().UTC().Add(24 * time.Hour)
			id := opID()
			inputs := map[string]any{"kind": "heartbeat", "authorityId": h.AuthorityID, "claimId": h.ClaimID, "ttl": ttl.Microseconds(), "requestNotAfter": deadline.UnixMicro(), "holdUntil": h.HoldUntil.UTC().UnixMicro()}
			h.State = "pending"
			h.PendingRequest = &handle.PendingRequest{OperationID: id, Kind: "heartbeat", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: hashValue(inputs), RequestNotAfter: deadline, Inputs: inputs}
			if err = lk.Write(r.path, h); err == nil {
				rec, callErr := b.svc.Heartbeat(r.ctx, lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision}, lease.Renew{OperationID: id, TTL: ttl, RequestNotAfter: deadline, HoldUntil: h.HoldUntil})
				if callErr == nil {
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
					if writeErr := lk.Write(r.path, h); writeErr != nil {
						lk.Close()
						b.st.Close()
						return
					}
				} else {
					// A definitive failure (expired, stale, rejected) leaves the
					// ready credential usable for explicit calls; an uncertain
					// one keeps the exact pending request for recovery. Either
					// way automatic renewal stops until the client intervenes.
					if reason.DefinitiveNoCommit(callErr) {
						_ = lk.ClearPending(r.path, &h)
					}
					lk.Close()
					b.st.Close()
					return
				}
			}
		}
		lk.Close()
		b.st.Close()
		if err != nil {
			return
		}
	}
}
func (s *Server) markRenewalStopped(r *runtimeLease) {
	s.mu.Lock()
	r.status = "stopped"
	s.mu.Unlock()
}

func (s *Server) stopAllRenewals() {
	s.mu.Lock()
	for _, r := range s.leases {
		r.status = "stopped"
		r.cancel()
	}
	s.leases = map[string]*runtimeLease{}
	s.mu.Unlock()
}
