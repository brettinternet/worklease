package lease

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

const (
	maxReopenAttestation      = 128 * 1024
	maxEvidenceReferences     = 64
	maxEvidenceReferenceBytes = 2048
)

// RemoteActor is the trusted installation identity produced by the authenticated
// boundary. The lease service rechecks the current installation row, role, and
// authority incarnation in the same transaction as each operation.
type RemoteActor struct {
	InstallationID    string
	AuthorityID       string
	ExpectedRestoreID string
	// Credential is the installation bearer. It is accepted only at the
	// service boundary and is never persisted or included in a result.
	Credential string
}

// RemotePolicy is immutable for one Service process. Prefixes are exact,
// delimiter-terminated portable prefixes; MaxTTL and MaxHold bound new claims.
type RemotePolicy struct {
	Prefixes []string
	MaxTTL   time.Duration
	MaxHold  time.Duration
}

// ResponseContext supplies the fresh identity and authority time used by a
// transport envelope. Historical replay remains nested in the typed result.
type ResponseContext struct {
	AuthorityID   string    `json:"authorityId"`
	RestoreID     string    `json:"restoreId"`
	AuthorityTime time.Time `json:"authorityTime"`
}

// RemoteResponse is the domain-to-transport envelope input. The HTTP layer
// adds protocolVersion and ok/error while preserving this freshly sampled
// identity outside any retained result.
type RemoteResponse[T any] struct {
	ResponseContext
	Result T `json:"result"`
}

func NewRemote(st *store.Store, clock Clock, ids IDGenerator, defaults Defaults, policy RemotePolicy) (*Service, error) {
	if err := validateRemotePolicy(policy); err != nil {
		return nil, err
	}
	s := New(st, clock, ids, defaults)
	policy.Prefixes = append([]string(nil), policy.Prefixes...)
	s.remote = &policy
	return s, nil
}

func validateRemotePolicy(policy RemotePolicy) error {
	if len(policy.Prefixes) == 0 {
		return reason.Invalid("at least one admitted remote prefix is required")
	}
	if policy.MaxTTL < minTTL || policy.MaxTTL > maxTTL {
		return reason.Invalid("remote maximum TTL must be between 1 second and 1 hour")
	}
	if policy.MaxHold <= 0 {
		return reason.Invalid("remote maximum hold must be positive")
	}
	seen := map[string]bool{}
	for _, prefix := range policy.Prefixes {
		if !utf8.ValidString(prefix) || strings.TrimSpace(prefix) != prefix || !strings.HasSuffix(prefix, ":") {
			return reason.Invalid("remote prefixes must be delimiter-terminated UTF-8")
		}
		if reservedRemoteResource(prefix) {
			return reason.New(reason.ReasonResourceNotEnrolled, "host-local resource prefixes cannot be admitted remotely")
		}
		if seen[prefix] {
			return reason.Invalid("remote prefixes must be unique")
		}
		seen[prefix] = true
	}
	return nil
}

func reservedRemoteResource(resource string) bool {
	return strings.HasPrefix(resource, "path:") || strings.HasPrefix(resource, "backlog-md:") || strings.HasPrefix(resource, "markdown:")
}

func (s *Service) ResponseContext() ResponseContext {
	return ResponseContext{AuthorityID: s.st.AuthorityID(), RestoreID: s.st.RestoreID(), AuthorityTime: s.clock.Now().UTC()}
}

func WrapRemoteResponse[T any](s *Service, result T) RemoteResponse[T] {
	return RemoteResponse[T]{ResponseContext: s.ResponseContext(), Result: result}
}

func roleAllows(actual, required string) bool {
	rank := map[string]int{"read": 1, "write": 2, "admin": 3}
	return rank[actual] >= rank[required]
}

func (s *Service) authorizeRemote(tx *store.Tx, actor *RemoteActor, required string) error {
	return s.authorizeRemoteContext(context.Background(), tx, actor, required)
}

func (s *Service) authorizeRemoteContext(ctx context.Context, tx *store.Tx, actor *RemoteActor, required string) error {
	if actor == nil {
		return nil
	}
	if s.remote == nil {
		return reason.New(reason.ReasonAuthorizationDenied, "remote service policy is not configured")
	}
	// Authentication is deliberately performed from the bearer hash inside the
	// serialized transaction. Do not trust an installation id supplied beside
	// the bearer, and do not use SQL equality for the secret comparison.
	credential := actor.Credential
	if credential == "" {
		return reason.New(reason.ReasonAuthenticationRequired, "installation authentication is required")
	}
	if err := validateCredential(credential); err != nil {
		return reason.New(reason.ReasonAuthenticationRequired, "installation authentication is required")
	}
	hash := hashSecret(credential)
	var installation, role string
	var revokedAt any
	rows, err := tx.QueryContext(ctx, `SELECT installation_id,credential_hash,role,revoked_at FROM installations`)
	if err != nil {
		return storage(err)
	}
	matched := false
	for rows.Next() {
		var id, storedHash, candidateRole string
		var candidateRevoked any
		if err := rows.Scan(&id, &storedHash, &candidateRole, &candidateRevoked); err != nil {
			rows.Close()
			return storage(err)
		}
		match := subtle.ConstantTimeCompare([]byte(storedHash), []byte(hash))
		if match == 1 {
			installation, role, revokedAt, matched = id, candidateRole, candidateRevoked, true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return storage(err)
	}
	if err := rows.Close(); err != nil {
		return storage(err)
	}
	if !matched {
		return reason.New(reason.ReasonAuthenticationRequired, "installation authentication is required")
	}
	actor.InstallationID = installation
	if revokedAt != nil {
		return reason.New(reason.ReasonInstallationRevoked, "installation is revoked")
	}
	if !roleAllows(role, required) {
		return reason.New(reason.ReasonAuthorizationDenied, "installation role does not authorize this operation")
	}
	if actor.AuthorityID != s.st.AuthorityID() {
		return reason.New(reason.ReasonAuthorityMismatch, "authority identity does not match")
	}
	if actor.ExpectedRestoreID != s.st.RestoreID() {
		return reason.New(reason.ReasonAuthorityRestored, "authority was restored").With("restoreId", s.st.RestoreID())
	}
	return nil
}

func (s *Service) effectiveRemoteNow(tx *store.Tx, actor *RemoteActor, required string, now time.Time) (time.Time, error) {
	if err := s.authorizeRemote(tx, actor, required); err != nil {
		return time.Time{}, err
	}
	return s.effectiveNow(tx, now)
}

func (s *Service) remoteAdmissionLimits(ttl, maxHold time.Duration) error {
	if s.remote == nil {
		return reason.New(reason.ReasonAuthorizationDenied, "remote service policy is not configured")
	}
	if ttl > s.remote.MaxTTL {
		return reason.Invalid("requested TTL exceeds the admitted remote maximum")
	}
	if maxHold <= 0 || maxHold > s.remote.MaxHold {
		return reason.Invalid("requested maximum hold exceeds the admitted remote maximum")
	}
	return nil
}

func (s *Service) remoteAdmission(resources []string, ttl, maxHold time.Duration) (time.Duration, error) {
	if err := s.remoteAdmissionLimits(ttl, maxHold); err != nil {
		return 0, err
	}
	for _, resource := range resources {
		if reservedRemoteResource(resource) {
			return 0, reason.New(reason.ReasonResourceNotEnrolled, "host-local resource is not admitted remotely")
		}
		allowed := false
		for _, prefix := range s.remote.Prefixes {
			if strings.HasPrefix(resource, prefix) {
				allowed = true
				break
			}
		}
		if !allowed {
			return 0, reason.New(reason.ReasonResourceNotEnrolled, "resource prefix is not admitted remotely")
		}
	}
	return maxHold, nil
}

func recoveryMode(tx *store.Tx) (bool, int64, error) {
	var enabled int
	var revision int64
	if err := tx.QueryRowContext(context.Background(), `SELECT recovery_mode,recovery_revision FROM recovery_state WHERE singleton=1`).Scan(&enabled, &revision); err != nil {
		return false, 0, storage(err)
	}
	return enabled != 0, revision, nil
}

func extensionExpiry(row claimRow, now int64, ttl time.Duration, localHold time.Time) (int64, error) {
	if row.Remote {
		if row.AdmittedTTL <= 0 || ttl.Microseconds() > row.AdmittedTTL {
			return 0, reason.Invalid("requested TTL exceeds the claim's admitted maximum")
		}
		expires := now + ttl.Microseconds()
		if row.AdmittedHoldUntil <= now {
			return 0, reason.New(reason.ReasonClaimExpired, "lease hold deadline has passed")
		}
		if expires > row.AdmittedHoldUntil {
			expires = row.AdmittedHoldUntil
		}
		return expires, nil
	}
	expires := now + ttl.Microseconds()
	if !localHold.IsZero() && expires > localHold.UnixMicro() {
		expires = localHold.UnixMicro()
	}
	if expires <= now {
		return 0, reason.New(reason.ReasonClaimExpired, "lease hold deadline has passed")
	}
	return expires, nil
}

// RecoveryStatus is the typed administrative view of namespace recovery.
type RecoveryStatus struct {
	RecoveryMode          bool       `json:"recoveryMode"`
	RecoveryRevision      int64      `json:"recoveryRevision"`
	RestoredAt            *time.Time `json:"restoredAt,omitempty"`
	SelectedDurableCutoff *time.Time `json:"selectedDurableCutoff,omitempty"`
	LossIntervalStart     *time.Time `json:"lossIntervalStart,omitempty"`
	LossIntervalEnd       *time.Time `json:"lossIntervalEnd,omitempty"`
	CutoffKnown           bool       `json:"cutoffKnown"`
	UnresolvedOperations  []string   `json:"unresolvedOperations"`
	RetainedInstallations []string   `json:"retainedInstallations"`
}

func (s *Service) RemoteRecoveryStatus(ctx context.Context, actor RemoteActor) (RecoveryStatus, error) {
	var out RecoveryStatus
	err := s.st.Read(ctx, func(tx *store.Tx) error {
		if err := s.authorizeRemote(tx, &actor, "admin"); err != nil {
			return err
		}
		var mode, cutoffKnown int
		var restored, cutoff, start, end *int64
		if err := tx.QueryRowContext(ctx, `SELECT recovery_mode,recovery_revision,restored_at,selected_durable_cutoff,cutoff_known,loss_interval_start,loss_interval_end FROM recovery_state WHERE singleton=1`).Scan(&mode, &out.RecoveryRevision, &restored, &cutoff, &cutoffKnown, &start, &end); err != nil {
			return storage(err)
		}
		out.RecoveryMode, out.CutoffKnown = mode != 0, cutoffKnown != 0
		out.RestoredAt = microsPointer(restored)
		out.SelectedDurableCutoff = microsPointer(cutoff)
		out.LossIntervalStart = microsPointer(start)
		out.LossIntervalEnd = microsPointer(end)
		rows, err := tx.QueryContext(ctx, `SELECT operation_id FROM operations WHERE state='started' ORDER BY started_seq,claim_id,operation_id`)
		if err != nil {
			return storage(err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return storage(err)
			}
			out.UnresolvedOperations = append(out.UnresolvedOperations, id)
		}
		if err := rows.Close(); err != nil {
			return storage(err)
		}
		rows, err = tx.QueryContext(ctx, `SELECT installation_id FROM installations ORDER BY enrolled_at,installation_id`)
		if err != nil {
			return storage(err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return storage(err)
			}
			out.RetainedInstallations = append(out.RetainedInstallations, id)
		}
		return storage(rows.Err())
	})
	return out, err
}

func microsPointer(value *int64) *time.Time {
	if value == nil {
		return nil
	}
	t := time.UnixMicro(*value).UTC()
	return &t
}

type RevokeClaimRequest struct {
	OperationID     string
	ClaimID         string
	Reason          string
	RequestNotAfter time.Time
}

func (s *Service) RevokeClaim(ctx context.Context, actor RemoteActor, req RevokeClaimRequest) (Receipt, error) {
	if !validID(req.OperationID) || !validID(req.ClaimID) {
		return Receipt{}, reason.Invalid("operation and claim IDs must be 32 lowercase hex characters")
	}
	text := strings.TrimSpace(req.Reason)
	if text == "" {
		text = "revoked"
	}
	if err := validatePublicText("revocation reason", text); err != nil {
		return Receipt{}, err
	}
	now := s.clock.Now().UTC()
	var hash string
	var out Receipt
	err := s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		if err := s.authorizeRemoteContext(ctx, tx, &actor, "admin"); err != nil {
			return err
		}
		effective, err := s.effectiveNow(tx, now)
		if err != nil {
			return err
		}
		now = effective
		if err := validateRequestWindow(req.RequestNotAfter, now); err != nil {
			return err
		}
		hash = requestHash(map[string]any{"protocolVersion": "worklease-http/1", "kind": "claim-revoke", "authorityId": actor.AuthorityID, "expectedRestoreId": actor.ExpectedRestoreID, "installationId": actor.InstallationID, "operationId": req.OperationID, "claimId": req.ClaimID, "reason": text, "requestNotAfter": req.RequestNotAfter.UnixMicro()})
		var existing operationRow
		if found, err := readOperation(tx, req.ClaimID, req.OperationID, &existing); err != nil {
			return err
		} else if found {
			if err := replayOperation(existing, hash, effective); err != nil {
				return err
			}
			_, err = receiptFromOperation(existing, true, &out)
			return err
		}
		var row claimRow
		ok, err := readClaim(tx, req.ClaimID, &row)
		if err != nil {
			return err
		}
		if !ok {
			return reason.New(reason.ReasonStaleClaim, "claim is not current")
		}
		rev := row.Revision + 1
		seq, err := tx.AppendEvent(remoteEvent(row, effective, "revoked", req.OperationID, &rev, map[string]any{"reason": "revoked", "checkpointPresent": row.Checkpoint != ""}, actor.InstallationID, s.st.RestoreID()))
		if err != nil {
			return storage(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE epochs SET ended_at=?,ended_seq=?,ended_recorded_at=?,end_reason='revoked',final_revision=?,checkpoint=? WHERE claim_id=?`, effective.UnixMicro(), seq, effective.UnixMicro(), rev, nullString(row.Checkpoint), row.ClaimID); err != nil {
			return storage(err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM claims WHERE claim_id=?`, row.ClaimID); err != nil {
			return storage(err)
		}
		result := map[string]any{"revoked": true, "revision": rev, "reason": "revoked"}
		encoded, _ := json.Marshal(result)
		if _, err := tx.ExecContext(ctx, `INSERT INTO operations(claim_id,operation_id,kind,request_hash,request_not_after,expected_revision,state,receipt,started_at,started_seq,completed_at,completed_seq,installation_id,restore_id,remote) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,1)`, row.ClaimID, req.OperationID, "claim-revoke", hash, req.RequestNotAfter.UnixMicro(), row.Revision, "completed", string(encoded), effective.UnixMicro(), seq, effective.UnixMicro(), seq, actor.InstallationID, s.st.RestoreID()); err != nil {
			return storage(err)
		}
		out = Receipt{OperationID: req.OperationID, ClaimID: row.ClaimID, Kind: "claim-revoke", RequestHash: hash, Revision: rev, Committed: true, Result: result}
		return nil
	})
	return out, err
}

type ReopenAttestation struct {
	InventoryComplete             bool     `json:"inventoryComplete"`
	PendingSetsComplete           bool     `json:"pendingSetsComplete"`
	RetainedOutcomesComplete      bool     `json:"retainedOutcomesComplete"`
	NamespaceCessationEstablished bool     `json:"namespaceCessationEstablished"`
	EvidenceReferences            []string `json:"evidenceReferences"`
}

type ReopenRequest struct {
	OperationID              string
	RequestNotAfter          time.Time
	ExpectedRecoveryRevision int64
	Attestation              ReopenAttestation
}

type ReopenResult struct {
	RecoveryMode             bool      `json:"recoveryMode"`
	RecoveryRevision         int64     `json:"recoveryRevision"`
	ReopenedAt               time.Time `json:"reopenedAt"`
	ReopenedByInstallationID string    `json:"reopenedByInstallationId"`
	Idempotent               bool      `json:"idempotent"`
}

func validateReopenAttestation(att ReopenAttestation) ([]byte, error) {
	if len(att.EvidenceReferences) == 0 || len(att.EvidenceReferences) > maxEvidenceReferences {
		return nil, reason.Invalid("one to 64 recovery evidence references are required")
	}
	for _, ref := range att.EvidenceReferences {
		if !utf8.ValidString(ref) || strings.TrimSpace(ref) == "" || len([]byte(ref)) > maxEvidenceReferenceBytes {
			return nil, reason.Invalid("recovery evidence reference is invalid")
		}
	}
	encoded, err := json.Marshal(att)
	if err != nil || len(encoded) > maxReopenAttestation {
		return nil, reason.Invalid("recovery attestation exceeds 128 KiB")
	}
	return encoded, nil
}

type persistedRecoveryEvidence struct {
	Cutoff, LossStart, LossEnd        *int64
	CutoffKnown, StartKnown, EndKnown int
	CoverageGaps                      string
}

func validatePersistedRecoveryEvidence(value persistedRecoveryEvidence) error {
	if (value.Cutoff != nil) != (value.CutoffKnown != 0) || (value.LossStart != nil) != (value.StartKnown != 0) || (value.LossEnd != nil) != (value.EndKnown != 0) {
		return reason.New(reason.ReasonRecoveryRequired, "recovery cutoff and loss-bound knowledge is inconsistent")
	}
	if value.LossStart != nil && value.LossEnd != nil && *value.LossStart > *value.LossEnd {
		return reason.New(reason.ReasonRecoveryRequired, "recovery loss interval is invalid")
	}
	if value.Cutoff != nil && value.LossStart != nil && *value.Cutoff > *value.LossStart {
		return reason.New(reason.ReasonRecoveryRequired, "durable cutoff follows the loss interval")
	}
	var gaps []string
	if len(value.CoverageGaps) > maxReopenAttestation || json.Unmarshal([]byte(value.CoverageGaps), &gaps) != nil || len(gaps) > maxEvidenceReferences {
		return reason.New(reason.ReasonRecoveryRequired, "recovery coverage gaps are invalid")
	}
	for _, gap := range gaps {
		if !utf8.ValidString(gap) || strings.TrimSpace(gap) == "" || len([]byte(gap)) > maxEvidenceReferenceBytes {
			return reason.New(reason.ReasonRecoveryRequired, "recovery coverage gap is invalid")
		}
	}
	return nil
}

func validateReconciledRetainedRows(ctx context.Context, tx *store.Tx) error {
	var inconsistent int
	if err := tx.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM operations o WHERE o.state='reconciled' AND NOT EXISTS (SELECT 1 FROM reconciliations r WHERE r.claim_id=o.claim_id AND r.operation_id=o.operation_id)) +
		(SELECT count(*) FROM reconciliations r WHERE NOT EXISTS (SELECT 1 FROM operations o WHERE o.claim_id=r.claim_id AND o.operation_id=r.operation_id AND o.state='reconciled'))`).Scan(&inconsistent); err != nil {
		return storage(err)
	}
	if inconsistent != 0 {
		return reason.New(reason.ReasonRecoveryRequired, "retained reconciliation records are inconsistent")
	}
	return nil
}

func (s *Service) ReopenRecovery(ctx context.Context, actor RemoteActor, req ReopenRequest) (ReopenResult, error) {
	if !validID(req.OperationID) {
		return ReopenResult{}, reason.Invalid("operation ID must be 32 lowercase hex characters")
	}
	attestation, err := validateReopenAttestation(req.Attestation)
	if err != nil {
		return ReopenResult{}, err
	}
	now := s.clock.Now().UTC()
	var hash string
	var out ReopenResult
	err = s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		if err := s.authorizeRemoteContext(ctx, tx, &actor, "admin"); err != nil {
			return err
		}
		effective, err := s.effectiveNow(tx, now)
		if err != nil {
			return err
		}
		now = effective
		if err := validateRequestWindow(req.RequestNotAfter, now); err != nil {
			return err
		}
		hash = requestHash(map[string]any{"protocolVersion": "worklease-http/1", "kind": "recovery-reopen", "authorityId": actor.AuthorityID, "expectedRestoreId": actor.ExpectedRestoreID, "installationId": actor.InstallationID, "operationId": req.OperationID, "expectedRecoveryRevision": req.ExpectedRecoveryRevision, "attestation": json.RawMessage(attestation), "requestNotAfter": req.RequestNotAfter.UnixMicro()})
		var recordedHash string
		var deadline, reopened int64
		var revision int64
		var installation string
		err = tx.QueryRowContext(ctx, `SELECT request_hash,request_not_after,reopened_at,recovery_revision,reopened_by_installation_id FROM recovery_reopenings WHERE operation_id=?`, req.OperationID).Scan(&recordedHash, &deadline, &reopened, &revision, &installation)
		if err == nil {
			if recordedHash != hash {
				return reason.New(reason.ReasonOperationRequestMismatch, "request intent differs from the recorded operation")
			}
			if effective.UnixMicro() >= deadline {
				return reason.New(reason.ReasonReplayExpired, "request replay deadline has passed")
			}
			out = ReopenResult{RecoveryMode: false, RecoveryRevision: revision, ReopenedAt: time.UnixMicro(reopened).UTC(), ReopenedByInstallationID: installation, Idempotent: true}
			return nil
		}
		if !strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return storage(err)
		}
		if !req.Attestation.InventoryComplete || !req.Attestation.PendingSetsComplete || !req.Attestation.RetainedOutcomesComplete || !req.Attestation.NamespaceCessationEstablished {
			return reason.New(reason.ReasonRecoveryRequired, "complete recovery coverage is required before reopening")
		}
		mode, revision, err := recoveryMode(tx)
		if err != nil {
			return err
		}
		if !mode {
			return reason.New(reason.ReasonRecoveryClosed, "namespace recovery is not active")
		}
		if revision != req.ExpectedRecoveryRevision {
			return reason.New(reason.ReasonStaleRevision, "recovery revision is stale")
		}
		var unresolved int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM operations WHERE state='started'`).Scan(&unresolved); err != nil {
			return storage(err)
		}
		if unresolved != 0 {
			return reason.New(reason.ReasonRecoveryRequired, "retained operations must be reconciled before reopening").With("unresolvedOperations", unresolved)
		}
		if err := validateReconciledRetainedRows(ctx, tx); err != nil {
			return err
		}
		evidence := persistedRecoveryEvidence{}
		if err := tx.QueryRowContext(ctx, `SELECT selected_durable_cutoff,cutoff_known,loss_interval_start,loss_interval_end,loss_start_known,loss_end_known,coverage_gaps FROM recovery_state WHERE singleton=1`).Scan(&evidence.Cutoff, &evidence.CutoffKnown, &evidence.LossStart, &evidence.LossEnd, &evidence.StartKnown, &evidence.EndKnown, &evidence.CoverageGaps); err != nil {
			return storage(err)
		}
		if err := validatePersistedRecoveryEvidence(evidence); err != nil {
			return err
		}
		revision++
		references, _ := json.Marshal(req.Attestation.EvidenceReferences)
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_reopenings(operation_id,request_hash,request_not_after,restore_id,recovery_revision,reopened_at,reopened_by_installation_id,selected_durable_cutoff,cutoff_known,loss_interval_start,loss_interval_end,loss_start_known,loss_end_known,inventory_complete,pending_sets_complete,retained_outcomes_complete,namespace_cessation_established,coverage_gaps,attestation,evidence_references) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, req.OperationID, hash, req.RequestNotAfter.UnixMicro(), s.st.RestoreID(), revision, effective.UnixMicro(), actor.InstallationID, evidence.Cutoff, evidence.CutoffKnown, evidence.LossStart, evidence.LossEnd, evidence.StartKnown, evidence.EndKnown, 1, 1, 1, 1, evidence.CoverageGaps, string(attestation), string(references)); err != nil {
			return storage(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE recovery_state SET recovery_mode=0,recovery_revision=? WHERE singleton=1`, revision); err != nil {
			return storage(err)
		}
		out = ReopenResult{RecoveryMode: false, RecoveryRevision: revision, ReopenedAt: effective, ReopenedByInstallationID: actor.InstallationID}
		return nil
	})
	return out, err
}

func remoteEvent(row claimRow, at time.Time, kind, operation string, revision *int64, detail map[string]any, installation, restore string) store.Event {
	return store.Event{At: at, Kind: kind, ClaimID: row.ClaimID, Resources: row.Resources, OperationID: operation, Revision: revision, AgentID: row.AgentID, Detail: detail, InstallationID: installation, RestoreID: restore, Remote: true}
}
