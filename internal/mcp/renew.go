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
	r := &runtimeLease{ref: ref, path: s.handlePath(ref), ttl: ttl, holdUntil: hold, ctx: ctx, cancel: cancel, stop: make(chan struct{}), done: make(chan struct{}), status: "active"}
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
		close(r.stop)
		delete(s.leases, ref)
	}
	s.mu.Unlock()
	if r != nil {
		<-r.done
	}
}

// renewLoop renews one lease this server acquired until its hold deadline,
// expiry, an ownership failure, release, or shutdown. Every exit path marks the
// runtime status stopped so tool results never report a dead renewer as active.
func (s *Server) renewLoop(r *runtimeLease) {
	defer close(r.done)
	defer s.markRenewalStopped(r)
	for {
		h, err := handle.Read(r.path)
		if err != nil {
			return
		}
		now := time.Now()
		if s.remote != nil {
			var nowErr error
			now, nowErr = s.remoteUpperNow(r.ctx)
			if nowErr != nil {
				return
			}
		}
		if !r.holdUntil.After(now) || !h.ExpiresAt.After(now) {
			return
		}
		wait := h.ExpiresAt.Sub(now) / 2
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		t := time.NewTimer(wait)
		select {
		case <-r.stop:
			t.Stop()
			return
		case <-r.ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		s.mu.Lock()
		ttl := r.ttl
		s.mu.Unlock()
		if s.remote != nil {
			if !s.renewRemote(r, ttl) {
				return
			}
			continue
		}
		if !r.holdUntil.After(time.Now()) {
			return
		}
		remaining := time.Until(r.holdUntil)
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
			b.Close()
			return
		}
		h, err = lk.Read(r.path)
		if err == nil && h.State == "pending" {
			lk.Close()
			b.Close()
			return
		}
		if err == nil && h.State == "ready" && h.HoldUntil.After(time.Now()) {
			deadline := time.Now().UTC().Add(24 * time.Hour)
			id := opID()
			inputs := map[string]any{"kind": "heartbeat", "authorityId": h.AuthorityID, "claimId": h.ClaimID, "ttl": ttl.Microseconds(), "requestNotAfter": deadline.UnixMicro(), "holdUntil": h.HoldUntil.UTC().UnixMicro()}
			h.State = "pending"
			h.PendingRequest = &handle.PendingRequest{OperationID: id, Kind: "heartbeat", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: hashValue(inputs), RequestNotAfter: deadline, Inputs: inputs}
			if err = lk.Write(r.path, h); err == nil {
				rec, callErr := b.authority.Heartbeat(r.ctx, lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision}, lease.Renew{OperationID: id, TTL: ttl, RequestNotAfter: deadline, HoldUntil: h.HoldUntil})
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
						b.Close()
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
					b.Close()
					return
				}
			}
		}
		lk.Close()
		b.Close()
		if err != nil {
			return
		}
	}
}
func (s *Server) renewRemote(r *runtimeLease, ttl time.Duration) bool {
	if s.ensureRemoteCredential() != nil {
		return false
	}
	h, err := handle.Read(r.path)
	if err != nil || h.State != "ready" || h.PendingRequest != nil || s.profile == nil || h.AuthorityID != s.profile.AuthorityID {
		return false
	}
	authorityNow, err := s.remoteUpperNow(r.ctx)
	if err != nil || !h.HoldUntil.After(authorityNow) {
		return false
	}
	remaining := h.HoldUntil.Sub(authorityNow)
	if ttl > remaining {
		ttl = remaining
	}
	if ttl < time.Second {
		return false
	}
	id := opID()
	creds := lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision, HandlePath: r.path, CredentialPath: s.profile.Credential.Path}
	receipt, err := s.remote.Heartbeat(r.ctx, creds, lease.Renew{OperationID: id, TTL: ttl})
	if err != nil {
		if reason.DefinitiveNoCommit(err) {
			clearRemotePending(r.path)
		}
		return false
	}
	lock, err := handle.AcquireLock(r.ctx, r.path+".lock")
	if err != nil {
		return false
	}
	defer lock.Close()
	current, err := lock.Read(r.path)
	if err != nil {
		return false
	}
	if raw, ok := receipt.Result["expiresAt"].(string); ok {
		current.ExpiresAt, _ = time.Parse(time.RFC3339Nano, raw)
	}
	if current.ExpiresAt.After(current.HoldUntil) {
		current.ExpiresAt = current.HoldUntil
	}
	return lock.Write(r.path, current) == nil
}

func (s *Server) markRenewalStopped(r *runtimeLease) {
	s.mu.Lock()
	r.status = "stopped"
	s.mu.Unlock()
}

func (s *Server) stopAllRenewals() {
	s.mu.Lock()
	leases := make([]*runtimeLease, 0, len(s.leases))
	for _, r := range s.leases {
		r.status = "stopped"
		close(r.stop)
		r.cancel()
		leases = append(leases, r)
	}
	s.leases = map[string]*runtimeLease{}
	s.mu.Unlock()
	for _, r := range leases {
		<-r.done
	}
}
