package mcp

import (
	"context"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
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
func (s *Server) renewLoop(r *runtimeLease) {
	for {
		h, err := handle.Read(r.path)
		if err != nil {
			s.markRenewalStopped(r)
			return
		}
		now := time.Now()
		if !r.holdUntil.After(now) || !h.ExpiresAt.After(now) {
			s.mu.Lock()
			r.status = "stopped"
			s.mu.Unlock()
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
			s.mu.Lock()
			r.status = "stopped"
			s.mu.Unlock()
			return
		}
		remaining := time.Until(r.holdUntil)
		ttl := r.ttl
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
		h, err = handle.Read(r.path)
		if err == nil && h.State == "pending" {
			s.mu.Lock()
			r.status = "stopped"
			s.mu.Unlock()
			lk.Close()
			b.st.Close()
			return
		}
		if err == nil && h.State == "ready" && h.HoldUntil.After(time.Now()) {
			deadline := time.Now().UTC().Add(24 * time.Hour)
			id := opID()
			inputs := map[string]any{"kind": "heartbeat", "authorityId": h.AuthorityID, "claimId": h.ClaimID, "ttl": ttl.Microseconds(), "requestNotAfter": deadline.UnixMicro()}
			h.State = "pending"
			h.PendingRequest = &handle.PendingRequest{OperationID: id, Kind: "heartbeat", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: hashValue(inputs), RequestNotAfter: deadline, Inputs: inputs}
			if err = handle.Write(r.path, h); err == nil {
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
					if writeErr := handle.Write(r.path, h); writeErr != nil {
						s.markRenewalStopped(r)
						lk.Close()
						b.st.Close()
						return
					}
				} else {
					s.mu.Lock()
					r.status = "stopped"
					s.mu.Unlock()
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
