package lease

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

const maxEvidence = 8 * 1024

// beforeReconciliationCommit is test-only fault injection proving that the
// target ledger, resolver revision, audit row, and event roll back together.
var beforeReconciliationCommit func() error

type ReconcileRequest struct {
	OperationID           string
	TargetClaimID         string
	TargetOperationID     string
	ExpectedRequestSHA256 string
	Outcome               string
	Evidence              json.RawMessage
	TTL                   time.Duration
	RequestNotAfter       time.Time
}

type ReconciliationReceipt struct {
	OperationID       string    `json:"operationId"`
	TargetClaimID     string    `json:"targetClaimId"`
	TargetOperationID string    `json:"targetOperationId"`
	ResolverClaimID   string    `json:"resolverClaimId"`
	Outcome           string    `json:"outcome"`
	RequestSHA256     string    `json:"requestSha256"`
	Revision          int64     `json:"revision"`
	ReconciledAt      time.Time `json:"reconciledAt"`
	ExpiresAt         time.Time `json:"expiresAt"`
	Idempotent        bool      `json:"idempotent"`
	Committed         bool      `json:"committed"`
}

// Reconcile records a caller-attested result for one started operation. It
// advances only the current resolver claim and never revives the target epoch.
func (s *Service) Reconcile(ctx context.Context, creds Credentials, req ReconcileRequest) (ReconciliationReceipt, error) {
	if err := validateOperationID(req.OperationID); err != nil {
		return ReconciliationReceipt{}, err
	}
	if !validID(req.TargetClaimID) || !validID(req.TargetOperationID) {
		return ReconciliationReceipt{}, reason.Invalid("target claim and operation IDs must be 32 lowercase hex characters")
	}
	if req.OperationID == req.TargetOperationID {
		return ReconciliationReceipt{}, reason.Invalid("reconciliation and target operation IDs must differ")
	}
	if decoded, err := hex.DecodeString(req.ExpectedRequestSHA256); err != nil || len(decoded) != 32 {
		return ReconciliationReceipt{}, reason.Invalid("expected request SHA-256 must be 64 hexadecimal characters")
	}
	req.ExpectedRequestSHA256 = strings.ToLower(req.ExpectedRequestSHA256)
	if req.Outcome != "observed-success" && req.Outcome != "observed-failure" {
		return ReconciliationReceipt{}, reason.Invalid("outcome must be observed-success or observed-failure")
	}
	canonical, err := strictCheckpoint(req.Evidence)
	if err != nil || len(canonical) > maxEvidence {
		return ReconciliationReceipt{}, reason.Invalid("evidence must be strict JSON no larger than 8 KiB")
	}
	var attestation map[string]any
	if json.Unmarshal(canonical, &attestation) != nil || attestation["executorStopped"] != true || attestation["outcome"] != req.Outcome {
		return ReconciliationReceipt{}, reason.Invalid("evidence must attest the outcome and executorStopped=true")
	}
	ttl := req.TTL
	if ttl == 0 {
		ttl = s.defaults.TTL
	}
	if err := validateTTL(ttl); err != nil {
		return ReconciliationReceipt{}, err
	}
	now := s.clock.Now().UTC()
	if err := validateRequestWindow(req.RequestNotAfter, now); err != nil {
		return ReconciliationReceipt{}, err
	}
	intent := map[string]any{"kind": "reconcile", "authorityId": s.st.AuthorityID(), "claimId": creds.ClaimID, "targetClaimId": req.TargetClaimID, "targetOperationId": req.TargetOperationID, "expectedRequestSha256": req.ExpectedRequestSHA256, "outcome": req.Outcome, "evidence": json.RawMessage(canonical), "ttl": ttl.Microseconds(), "requestNotAfter": req.RequestNotAfter.UTC().UnixMicro()}
	hash, err := checkedRequestHash(intent)
	if err != nil {
		return ReconciliationReceipt{}, err
	}
	var result ReconciliationReceipt
	err = s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		effective, e := s.effectiveNow(tx, now)
		if e != nil {
			return e
		}
		// An equal resolver operation is an authenticated idempotent replay.
		var replay operationRow
		if found, e := readOperation(tx, creds.ClaimID, req.OperationID, &replay); e != nil {
			return e
		} else if found {
			if subtle.ConstantTimeCompare([]byte(replay.TokenHash), []byte(hashToken(creds.Token))) != 1 {
				return reason.New(reason.ReasonInvalidToken, "credential is invalid")
			}
			if replay.Kind != "reconcile" || replay.RequestHash != hash {
				return reason.New(reason.ReasonReconciliationConflict, "reconciliation operation ID was used for different intent")
			}
			if replay.State == "started" {
				return reason.New(reason.ReasonUnknownOutcome, "reconciliation outcome is unresolved")
			}
			if effective.UnixMicro() >= replay.RequestNotAfter {
				return reason.New(reason.ReasonReplayExpired, "request replay deadline has passed")
			}
			if replay.Receipt == "" || json.Unmarshal([]byte(replay.Receipt), &result) != nil {
				return storage(errors.New("invalid reconciliation receipt"))
			}
			result.Idempotent = true
			return nil
		}
		var resolver claimRow
		ok, e := readClaim(tx, creds.ClaimID, &resolver)
		if e != nil {
			return e
		}
		if !ok {
			return reason.New(reason.ReasonStaleClaim, "resolver claim is not current")
		}
		if e = s.authorize(resolver, creds, effective.UnixMicro(), true); e != nil {
			return e
		}
		var target operationRow
		found, e := readOperation(tx, req.TargetClaimID, req.TargetOperationID, &target)
		if e != nil {
			return e
		}
		if !found {
			return reason.New(reason.ReasonOperationNotFound, "target operation was not found")
		}
		if target.State != "started" {
			return reason.New(reason.ReasonReconciliationConflict, "target operation is not unresolved").With("state", target.State)
		}
		if subtle.ConstantTimeCompare([]byte(target.RequestHash), []byte(req.ExpectedRequestSHA256)) != 1 {
			return reason.New(reason.ReasonExpectedHashMismatch, "target request SHA-256 does not match")
		}
		targetResources, e := epochResourcesForReconciliation(ctx, tx, req.TargetClaimID)
		if e != nil {
			return e
		}
		if !coversResources(resolver.Resources, targetResources) {
			return reason.New(reason.ReasonReconciliationConflict, "resolver claim does not cover every target resource").With("requiredResources", targetResources)
		}
		var existing string
		if e := tx.QueryRowContext(ctx, `SELECT reconcile_operation_id FROM reconciliations WHERE claim_id=? AND operation_id=?`, req.TargetClaimID, req.TargetOperationID).Scan(&existing); e == nil {
			return reason.New(reason.ReasonReconciliationConflict, "target operation was already reconciled").With("reconcileOperationId", existing)
		} else if !strings.Contains(strings.ToLower(e.Error()), "no rows") {
			return storage(e)
		}
		revision := resolver.Revision + 1
		expires := effective.Add(ttl).UnixMicro()
		targetReceipt := map[string]any{"reconciled": true, "outcome": req.Outcome, "resolverClaimId": resolver.ClaimID, "revision": revision}
		targetJSON, _ := json.Marshal(targetReceipt)
		seq, e := tx.AppendEvent(store.Event{At: effective, Kind: "reconciled", ClaimID: req.TargetClaimID, Resources: targetResources, OperationID: req.TargetOperationID, Revision: &revision, AgentID: resolver.AgentID, Detail: map[string]any{"outcome": req.Outcome}})
		if e != nil {
			return storage(e)
		}
		if _, e = tx.ExecContext(ctx, `UPDATE operations SET state='reconciled',receipt=?,completed_at=?,completed_seq=? WHERE claim_id=? AND operation_id=? AND state='started'`, string(targetJSON), effective.UnixMicro(), seq, req.TargetClaimID, req.TargetOperationID); e != nil {
			return storage(e)
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO reconciliations(claim_id,operation_id,outcome,evidence,request_hash,reconcile_operation_id,resolver_claim_id,resolver_agent_id,resolver_session_id,recorded_at,recorded_seq) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, req.TargetClaimID, req.TargetOperationID, req.Outcome, string(canonical), req.ExpectedRequestSHA256, req.OperationID, resolver.ClaimID, resolver.AgentID, resolver.SessionID, effective.UnixMicro(), seq); e != nil {
			return storage(e)
		}
		if _, e = tx.ExecContext(ctx, `UPDATE claims SET revision=?,ttl_us=?,heartbeat_at=?,expires_at=? WHERE claim_id=?`, revision, ttl.Microseconds(), effective.UnixMicro(), expires, resolver.ClaimID); e != nil {
			return storage(e)
		}
		result = ReconciliationReceipt{OperationID: req.OperationID, TargetClaimID: req.TargetClaimID, TargetOperationID: req.TargetOperationID, ResolverClaimID: resolver.ClaimID, Outcome: req.Outcome, RequestSHA256: req.ExpectedRequestSHA256, Revision: revision, ReconciledAt: effective, ExpiresAt: time.UnixMicro(expires).UTC(), Committed: true}
		if beforeReconciliationCommit != nil {
			if e := beforeReconciliationCommit(); e != nil {
				return storage(e)
			}
		}
		receiptJSON, _ := json.Marshal(result)
		if _, e = tx.ExecContext(ctx, `INSERT INTO operations(claim_id,operation_id,kind,request_hash,request_not_after,expected_revision,state,receipt,started_at,started_seq,completed_at,completed_seq) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, resolver.ClaimID, req.OperationID, "reconcile", hash, req.RequestNotAfter.UnixMicro(), resolver.Revision, "completed", string(receiptJSON), effective.UnixMicro(), seq, effective.UnixMicro(), seq); e != nil {
			return storage(e)
		}
		return nil
	})
	return result, err
}

func epochResourcesForReconciliation(ctx context.Context, tx *store.Tx, claim string) ([]string, error) {
	rows, e := tx.QueryContext(ctx, `SELECT resource FROM epoch_resources WHERE claim_id=? ORDER BY position`, claim)
	if e != nil {
		return nil, storage(e)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if e := rows.Scan(&r); e != nil {
			return nil, storage(e)
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return nil, reason.New(reason.ReasonOperationNotFound, "target epoch was not found")
	}
	return out, rows.Err()
}
func coversResources(have, want []string) bool {
	set := map[string]bool{}
	for _, r := range have {
		set[r] = true
	}
	for _, r := range want {
		if !set[r] {
			return false
		}
	}
	return true
}
