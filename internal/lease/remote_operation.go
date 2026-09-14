package lease

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

// RemoteOperationRenewRequest gives each renewal of a started remote operation
// its own exact-replay identity.
type RemoteOperationRenewRequest struct {
	RenewalID       string
	OperationID     string
	TTL             time.Duration
	RequestNotAfter time.Time
}

func (s *Service) RemoteRenewOperation(ctx context.Context, creds Credentials, req RemoteOperationRenewRequest) (Receipt, error) {
	if creds.Actor == nil {
		return Receipt{}, reason.Invalid("remote actor is required")
	}
	if !validID(req.RenewalID) || !validID(req.OperationID) {
		return Receipt{}, reason.Invalid("renewal and operation IDs must be 32 lowercase hex characters")
	}
	ttl := req.TTL
	if ttl == 0 {
		ttl = s.defaults.TTL
	}
	if err := validateTTL(ttl); err != nil {
		return Receipt{}, err
	}
	now := s.clock.Now().UTC()
	var hash string
	var out Receipt
	err := s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		if err := s.authorizeRemoteContext(ctx, tx, creds.Actor, "write"); err != nil {
			return err
		}
		effective, err := s.effectiveNow(tx, now)
		if err != nil {
			return err
		}
		now = effective
		if err := validateRequestWindow(req.RequestNotAfter, effective); err != nil {
			return err
		}
		hash = requestHash(map[string]any{"protocolVersion": "worklease-http/1", "kind": "operation-renew", "authorityId": creds.Actor.AuthorityID, "expectedRestoreId": creds.Actor.ExpectedRestoreID, "installationId": creds.Actor.InstallationID, "claimId": creds.ClaimID, "operationId": req.OperationID, "renewalId": req.RenewalID, "ttl": ttl.Microseconds(), "requestNotAfter": req.RequestNotAfter.UnixMicro()})
		var target operationRow
		found, err := readOperation(tx, creds.ClaimID, req.OperationID, &target)
		if err != nil {
			return err
		}
		if !found {
			return reason.New(reason.ReasonOperationNotFound, "started operation was not found")
		}
		if subtle.ConstantTimeCompare([]byte(target.TokenHash), []byte(hashToken(creds.Token))) != 1 {
			return reason.New(reason.ReasonInvalidToken, "credential is invalid")
		}
		if !target.Remote || target.InstallationID != creds.Actor.InstallationID {
			return reason.New(reason.ReasonAuthorizationDenied, "operation belongs to another installation")
		}
		var recordedHash, encoded string
		var deadline int64
		err = tx.QueryRowContext(ctx, `SELECT request_hash,request_not_after,receipt FROM operation_renewals WHERE claim_id=? AND operation_id=? AND renewal_id=?`, creds.ClaimID, req.OperationID, req.RenewalID).Scan(&recordedHash, &deadline, &encoded)
		if err == nil {
			if recordedHash != hash {
				return reason.New(reason.ReasonOperationRequestMismatch, "request intent differs from the recorded operation renewal")
			}
			if effective.UnixMicro() >= deadline {
				return reason.New(reason.ReasonReplayExpired, "request replay deadline has passed")
			}
			if err := json.Unmarshal([]byte(encoded), &out); err != nil {
				return storage(err)
			}
			out.Idempotent = true
			return nil
		}
		if !strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return storage(err)
		}
		var row claimRow
		current, err := readClaim(tx, creds.ClaimID, &row)
		if err != nil {
			return err
		}
		if !current {
			return reason.New(reason.ReasonStaleClaim, "claim is not current")
		}
		if err := s.authorize(row, creds, effective.UnixMicro(), true); err != nil {
			return err
		}
		if target.State != "started" {
			return reason.New(reason.ReasonOperationNotFound, "started operation was not found")
		}
		revision := row.Revision + 1
		expires, err := extensionExpiry(row, effective.UnixMicro(), ttl, time.Time{})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE claims SET revision=?,ttl_us=?,heartbeat_at=?,expires_at=? WHERE claim_id=?`, revision, ttl.Microseconds(), effective.UnixMicro(), expires, row.ClaimID); err != nil {
			return storage(err)
		}
		event := remoteEvent(row, effective, "renewed", req.OperationID, &revision, nil, creds.Actor.InstallationID, s.st.RestoreID())
		seq, err := tx.AppendEvent(event)
		if err != nil {
			return storage(err)
		}
		out = Receipt{OperationID: req.RenewalID, ClaimID: row.ClaimID, Kind: "operation-renew", RequestHash: hash, Revision: revision, Committed: true, Result: map[string]any{"operationId": req.OperationID, "revision": revision, "expiresAt": formatMicros(expires)}}
		encodedReceipt, _ := json.Marshal(out)
		if _, err := tx.ExecContext(ctx, `INSERT INTO operation_renewals(claim_id,operation_id,renewal_id,request_hash,request_not_after,expected_revision,ttl_us,receipt,renewed_at,renewed_seq,installation_id,restore_id,remote) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,1)`, row.ClaimID, req.OperationID, req.RenewalID, hash, req.RequestNotAfter.UnixMicro(), row.Revision, ttl.Microseconds(), string(encodedReceipt), effective.UnixMicro(), seq, creds.Actor.InstallationID, s.st.RestoreID()); err != nil {
			return storage(err)
		}
		return nil
	})
	return out, err
}
