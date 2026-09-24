package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueindex"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/reason"
)

// Queue lifecycle traffic never enters the provider refresh/index scheduler.
// The clock is supplied by the runner so suspend and deadline behavior can be tested.
type queueLifecycle struct {
	controller *queueClaimController
	now        func() time.Time
}

func (l queueLifecycle) directory() string {
	profile := l.controller.profileName
	if profile == "" {
		profile = "local"
	}
	return queueClaimHandleDir(l.controller.home, l.controller.queueSession, profile)
}

func (l queueLifecycle) paths() ([]string, error) {
	names, err := handle.ListOwnerPrivateNames(l.directory())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasPrefix(name, "claim-") && strings.HasSuffix(name, ".json") {
			paths = append(paths, filepath.Join(l.directory(), name))
		}
	}
	return paths, nil
}

func (l queueLifecycle) credentials(path string, h handle.Handle) lease.Credentials {
	c := lease.Credentials{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, Token: h.Token, Revision: h.Revision, HandlePath: path}
	if l.controller.backend.Profile != nil {
		c.CredentialPath = l.controller.backend.Profile.Credential.Path
	}
	return c
}

func (l queueLifecycle) authorityNow(selected queue.ClaimAuthority) (time.Time, error) {
	if selected.Remote {
		if selected.Now == nil {
			return time.Time{}, reason.New(reason.ReasonClockRegression, "remote authority time unavailable")
		}
		return selected.Now()
	}
	return l.now(), nil
}

func renewalMargin(ttl time.Duration) time.Duration {
	margin := ttl / 4
	if margin > 30*time.Second {
		margin = 30 * time.Second
	}
	return margin
}

func (l queueLifecycle) inspect(ctx context.Context, path string, renew bool) queueui.OwnedClaimMsg {
	msg := queueui.OwnedClaimMsg{Path: path, LastResult: "verification pending"}
	selected, err := l.controller.selectedAuthority()
	if err == nil {
		err = l.controller.profileIdentityCurrent()
	}
	if err != nil {
		msg.LastResult = "authority unavailable: " + err.Error()
		return msg
	}
	var lock *handle.Lock
	if !selected.Remote {
		lock, err = handle.AcquireLock(ctx, path+".lock")
		if err != nil {
			msg.LastResult = "handle unavailable: " + err.Error()
			return msg
		}
		defer lock.Close()
	}
	var h handle.Handle
	if lock != nil {
		h, err = lock.Read(path)
	} else {
		h, err = handle.Read(path)
	}
	if err != nil {
		msg.LastResult = "handle unavailable: " + err.Error()
		return msg
	}
	msg.Resources, msg.ClaimID, msg.ExpiresAt = append([]string(nil), h.Resources...), h.ClaimID, h.ExpiresAt
	if h.AuthorityID != selected.ID || h.SessionID != l.controller.queueSession || selected.Remote && (l.controller.profile == nil || h.RestoreID != l.controller.profile.RestoreID) {
		msg.LastResult = "authority identity changed; recovery required"
		return msg
	}
	if h.State != "ready" || h.PendingRequest != nil || h.RecoveryRequest != nil {
		msg.LastResult = "pending operation; exact recovery required"
		return msg
	}
	verification, err := l.controller.backend.API.Verify(ctx, l.credentials(path, h), h.Resources)
	if err != nil {
		msg.LastResult = "ownership unverified: " + err.Error()
		if classified := reason.As(err); classified != nil && (classified.Reason == reason.ReasonStaleClaim || classified.Reason == reason.ReasonClaimExpired || classified.Reason == reason.ReasonVerifyFailed && (classified.Details["cause"] == reason.ReasonStaleClaim || classified.Details["cause"] == reason.ReasonClaimExpired)) {
			msg.Lost = true
		}
		return msg
	}
	if !verification.Claim.Active || verification.Claim.ClaimID != h.ClaimID || verification.Claim.Revision != h.Revision {
		msg.Lost = true
		msg.LastResult = "claim lost or revision changed"
		return msg
	}
	if len(verification.UnknownOperations) > 0 || len(verification.Claim.UnknownOperations) > 0 {
		msg.LastResult = "unresolved operation; exact recovery required"
		return msg
	}
	msg.Verified = true
	msg.ExpiresAt = verification.Claim.ExpiresAt
	ttl := h.ExpiresAt.Sub(verification.Claim.HeartbeatAt)
	if ttl <= 0 {
		ttl = l.controller.backend.Config.TTL
	}
	msg.NextRenewal = nextRenewal(verification.Claim.HeartbeatAt, msg.ExpiresAt, h.ClaimID)
	msg.LastResult = "verified"
	currentTime, clockErr := l.authorityNow(selected)
	if clockErr != nil {
		msg.Verified = false
		msg.LastResult = "authority clock unavailable: " + clockErr.Error()
		return msg
	}
	if !renew || currentTime.Before(msg.NextRenewal) {
		return msg
	}
	if !currentTime.Add(renewalMargin(ttl)).Before(msg.ExpiresAt) {
		msg.Verified = false
		msg.LastResult = "renewal deadline missed; verify before further actions"
		return msg
	}
	op, deadline := randomHex(16), currentTime.Add(time.Hour)
	inputs := map[string]any{"kind": "heartbeat", "authorityId": h.AuthorityID, "claimId": h.ClaimID, "ttl": ttl.Microseconds(), "requestNotAfter": deadline.UTC().UnixMicro()}
	if !selected.Remote {
		if h.HoldUntil.IsZero() {
			msg.Verified = false
			msg.LastResult = "local hold deadline missing; recovery required"
			return msg
		}
		inputs["holdUntil"] = h.HoldUntil.UTC().UnixMicro()
		h.State = "pending"
		h.PendingRequest = &handle.PendingRequest{OperationID: op, Kind: "heartbeat", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: requestHashCLI(inputs), RequestNotAfter: deadline, Inputs: inputs}
		err = lock.Write(path, h)
	} else {
		err = beginHandleMutation(path, &h, "heartbeat", op, deadline, inputs)
	}
	if err != nil {
		msg.Verified = false
		msg.LastResult = "renewal preparation failed: " + err.Error()
		return msg
	}
	receipt, err := l.controller.backend.API.Heartbeat(ctx, l.credentials(path, h), lease.Renew{OperationID: op, TTL: ttl, RequestNotAfter: deadline, HoldUntil: h.HoldUntil})
	if err != nil {
		if isDefinitiveNoCommit(err) {
			clearPending(path, &h, lock)
		}
		msg.Verified = false
		msg.LastResult = "renewal uncertain or failed: " + err.Error()
		return msg
	}
	if selected.Remote {
		// The remote client persists the new revision; record the new expiry too.
		// Otherwise a reopened queue would schedule against an old deadline.
		expires, ok := receipt.Result["expiresAt"].(string)
		var expiry time.Time
		if ok {
			expiry, err = time.Parse(time.RFC3339Nano, expires)
		}
		if !ok || err != nil {
			msg.Verified = false
			msg.LastResult = "renewed but expiry unavailable; recovery required"
			return msg
		}
		remoteLock, lockErr := handle.AcquireLock(ctx, path+".lock")
		if lockErr != nil {
			msg.Verified = false
			msg.LastResult = "renewed but handle unavailable: " + lockErr.Error()
			return msg
		}
		current, readErr := remoteLock.Read(path)
		if readErr == nil && current.State == "ready" && current.ClaimID == h.ClaimID && current.Revision == receipt.Revision {
			current.ExpiresAt = expiry
			readErr = remoteLock.Write(path, current)
		}
		remoteLock.Close()
		if readErr != nil || current.Revision != receipt.Revision || current.State != "ready" {
			msg.Verified = false
			msg.LastResult = "renewed but handle needs recovery"
			return msg
		}
		h = current
	} else if err = finishHandleMutation(path, &h, receipt, lock); err != nil {
		msg.Verified = false
		msg.LastResult = "renewed but handle needs recovery: " + err.Error()
		return msg
	}
	msg.ExpiresAt = h.ExpiresAt
	msg.NextRenewal = nextRenewal(currentTime, h.ExpiresAt, h.ClaimID)
	msg.LastResult = "renewed at " + currentTime.UTC().Format(time.RFC3339)
	return msg
}

func nextRenewal(last, expiry time.Time, id string) time.Time {
	ttl := expiry.Sub(last)
	if ttl <= 0 {
		return last
	}
	jitter := time.Duration(id[0]%11) * ttl / 200
	return last.Add(ttl*2/5 - jitter)
}

// Cancel permits only a demonstrably no-effect epoch. Checkpoints alone are
// not provider receipts; journaled provider intents disallow cancellation.
func (l queueLifecycle) Cancel(ctx context.Context, path string) error {
	selected, err := l.controller.selectedAuthority()
	if err == nil {
		err = l.controller.profileIdentityCurrent()
	}
	if err != nil {
		return err
	}
	// Peek only to choose the lock. Re-read under both locks before any effect.
	peek, err := handle.Read(path)
	if err != nil {
		return err
	}
	cacheDir, err := queueindex.CacheDir(os.Getenv, os.Getenv("HOME"))
	if err != nil {
		return reason.New(reason.ReasonRecoveryRequired, "queue cache location unavailable; cancellation refused")
	}
	journal, err := queue.NewWriteJournal(config.QueueRecoveryDir(os.Getenv), cacheDir)
	if err != nil {
		return reason.New(reason.ReasonRecoveryRequired, "queue recovery location unsafe; cancellation refused")
	}
	claimLock, err := journal.ClaimLock(ctx, peek.AuthorityID, peek.ClaimID)
	if err != nil {
		return err
	}
	defer claimLock.Close()
	var lock *handle.Lock
	if !selected.Remote {
		lock, err = handle.AcquireLock(ctx, path+".lock")
		if err != nil {
			return err
		}
		defer lock.Close()
	}
	var h handle.Handle
	if lock != nil {
		h, err = lock.Read(path)
	} else {
		h, err = handle.Read(path)
	}
	if err != nil {
		return err
	}
	if h.ClaimID != peek.ClaimID || h.AuthorityID != peek.AuthorityID || h.AuthorityID != selected.ID || h.SessionID != l.controller.queueSession || h.State != "ready" || h.PendingRequest != nil || h.RecoveryRequest != nil {
		return reason.New(reason.ReasonRecoveryRequired, "queue claim identity or pending request requires recovery")
	}
	verified, err := l.controller.backend.API.Verify(ctx, l.credentials(path, h), h.Resources)
	if err != nil {
		return err
	}
	if !verified.Claim.Active || verified.Claim.ClaimID != h.ClaimID || verified.Claim.Revision != h.Revision || len(verified.UnknownOperations) > 0 || len(verified.Claim.UnknownOperations) > 0 {
		return reason.New(reason.ReasonRecoveryRequired, "claim is lost or an operation is unresolved")
	}
	canCancel, err := journal.CanCancel(h.ClaimID)
	if err != nil {
		return reason.New(reason.ReasonRecoveryRequired, "provider write history unavailable; cancellation refused")
	}
	if !canCancel {
		return reason.New(reason.ReasonRecoveryRequired, "provider write intent journaled; cancellation refused")
	}
	// An incomplete/pruned history cannot establish the no-effect exception.
	for _, resource := range h.Resources {
		page, e := l.controller.backend.API.History(ctx, resource, "", 100, false)
		if e != nil {
			return e
		}
		if page.Gap {
			return reason.New(reason.ReasonRecoveryRequired, "claim history incomplete; cancellation refused")
		}
		found := false
		for _, epoch := range page.Epochs {
			if epoch.ClaimID != h.ClaimID {
				continue
			}
			found = true
			for _, operation := range epoch.Operations {
				switch operation.Kind {
				case "acquire", "heartbeat", "checkpoint": // claim maintenance is not a guarded or provider operation
				default:
					return reason.New(reason.ReasonRecoveryRequired, "guarded operation started; cancellation refused")
				}
			}
		}
		if !found {
			return reason.New(reason.ReasonRecoveryRequired, "claim history unavailable; cancellation refused")
		}
	}
	currentTime, clockErr := l.authorityNow(selected)
	if clockErr != nil {
		return clockErr
	}
	op, deadline := randomHex(16), currentTime.Add(time.Hour)
	reasonText := "cancelled: no guarded operation or provider write dispatched by queue"
	inputs := map[string]any{"kind": "release", "authorityId": h.AuthorityID, "claimId": h.ClaimID, "reason": reasonText, "requestNotAfter": deadline.UTC().UnixMicro()}
	if err = beginHandleMutation(path, &h, "release", op, deadline, inputs, lock); err != nil {
		return err
	}
	receipt, err := l.controller.backend.API.Release(ctx, l.credentials(path, h), lease.ReleaseRequest{OperationID: op, Reason: reasonText, RequestNotAfter: deadline})
	if err != nil {
		if isDefinitiveNoCommit(err) {
			clearPending(path, &h, lock)
		}
		return mutationFailure(err, h.ClaimID, op, path)
	}
	if lock != nil {
		err = lock.Remove(path)
	} else {
		err = removeReleasedQueueHandle(ctx, path, h.ClaimID, receipt.Revision)
	}
	if err != nil {
		return committedHandleFailure(err, receipt, path, h.ClaimID, op)
	}
	return nil
}

// A remote release may race a new acquisition by another queue process using
// the same persisted session. Never remove a successor's only credential.
func removeReleasedQueueHandle(ctx context.Context, path, claimID string, revision int64) error {
	lock, err := handle.AcquireLock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	current, err := lock.Read(path)
	if err != nil {
		return err
	}
	if current.ClaimID != claimID {
		return nil
	}
	if current.State != "ready" || current.PendingRequest != nil || current.RecoveryRequest != nil || current.Revision != revision {
		return reason.New(reason.ReasonRecoveryRequired, "released claim handle changed; preserve recovery record")
	}
	return lock.Remove(path)
}

func (l queueLifecycle) run(ctx context.Context, send func(queueui.OwnedClaimMsg)) {
	// Control operations run independently of providers/indexing and independently
	// of each other. One slow authority request cannot consume another lease's margin.
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	type result struct {
		path string
		msg  queueui.OwnedClaimMsg
	}
	results := make(chan result, 16)
	active := make(map[string]bool)
	nextCheck := make(map[string]time.Time)
	var workers sync.WaitGroup
	defer workers.Wait()
	for {
		paths, err := l.paths()
		if err != nil {
			for path := range nextCheck {
				send(queueui.OwnedClaimMsg{Path: path, LastResult: fmt.Sprintf("handle directory unavailable: %v", err)})
			}
		} else {
			present := make(map[string]bool, len(paths))
			for _, path := range paths {
				present[path] = true
			}
			for path := range nextCheck {
				if !present[path] && !active[path] {
					delete(nextCheck, path)
					send(queueui.OwnedClaimMsg{Path: path, Gone: true})
				}
			}
			for _, path := range paths {
				if active[path] || l.now().Before(nextCheck[path]) || len(active) >= 16 {
					continue
				}
				active[path] = true
				workers.Add(1)
				go func(path string) {
					defer workers.Done()
					checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					msg := l.inspect(checkCtx, path, true)
					cancel()
					select {
					case results <- result{path, msg}:
					case <-ctx.Done():
					}
				}(path)
			}
		}
		select {
		case <-ctx.Done():
			return
		case item := <-results:
			delete(active, item.path)
			interval := 10 * time.Second
			if !item.msg.Verified {
				interval = time.Second
			} else if !item.msg.NextRenewal.IsZero() {
				selected, _ := l.controller.current()
				authorityTime, clockErr := l.authorityNow(selected)
				if clockErr != nil {
					item.msg.Verified = false
					item.msg.LastResult = "authority clock unavailable: " + clockErr.Error()
					interval = time.Second
				} else {
					until := item.msg.NextRenewal.Sub(authorityTime)
					if until > 0 && until < interval {
						interval = until
					}
				}
			}
			nextCheck[item.path] = l.now().Add(interval)
			send(item.msg)
		case <-ticker.C:
		}
	}
}
