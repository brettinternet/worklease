package lease

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

const (
	inviteLifetime       = 15 * time.Minute
	inviteReplayLifetime = 24 * time.Hour
	minimumSecretBytes   = 32
)

// GenerateSecret returns a client-side 256-bit bearer encoded as lowercase hex.
// The caller must durably save it before sending the associated request.
func GenerateSecret() (string, error) {
	var secret [minimumSecretBytes]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", reason.New(reason.ReasonStorageFailure, "generate secret")
	}
	return hex.EncodeToString(secret[:]), nil
}

// GenerateInviteCode creates the plaintext invite. It is intentionally a
// client-side helper; authorities receive only HashSecret(code).
func GenerateInviteCode() (string, error) { return GenerateSecret() }

// GenerateInstallationCredential creates the client installation bearer.
func GenerateInstallationCredential() (string, error) { return GenerateSecret() }

// HashSecret is the stable storage representation for invite and installation
// bearers. Plaintext secrets must never be stored in an authority database.
func HashSecret(secret string) string {
	return hashSecret(secret)
}

func hashSecret(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

func validateCredential(secret string) error {
	if len(secret) != minimumSecretBytes*2 {
		return reason.New(reason.ReasonAuthenticationRequired, "installation authentication is required")
	}
	if decoded, err := hex.DecodeString(secret); err != nil || len(decoded) != minimumSecretBytes || secret != strings.ToLower(secret) {
		return reason.New(reason.ReasonAuthenticationRequired, "installation authentication is required")
	}
	return nil
}

func validateSecretHash(hash string, invalid string) error {
	if len(hash) != sha256.Size*2 || hash != strings.ToLower(hash) {
		return reason.New(invalid, "secret hash is invalid")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return reason.New(invalid, "secret hash is invalid")
	}
	return nil
}

// insertOfflineBootstrap is the authentication-owned primitive used by offline
// initialization, restore, and reissue after the plaintext secret is durable.
func insertOfflineBootstrap(ctx context.Context, tx *store.Tx, now time.Time, restoreID, secret string) (string, time.Time, error) {
	if err := validateCredential(secret); err != nil {
		return "", time.Time{}, err
	}
	inviteID, operationID := RandomIDs{}.Generate(), RandomIDs{}.Generate()
	expires := now.Add(inviteLifetime)
	inviteHash := HashSecret(secret)
	requestHash := HashSecret(operationID + restoreID + inviteHash)
	if _, err := tx.ExecContext(ctx, `INSERT INTO invites(invite_id,invite_hash,role,label,issued_at,expires_at,restore_id,issued_by_installation_id,issue_operation_id,issue_request_hash,request_not_after,bootstrap,state) VALUES(?,?,?,?,?,?,?,?,?,?,?,?, 'active')`, inviteID, inviteHash, "admin", "bootstrap", now.UnixMicro(), expires.UnixMicro(), restoreID, nil, operationID, requestHash, now.Add(inviteReplayLifetime).UnixMicro(), 1); err != nil {
		return "", time.Time{}, storage(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE recovery_state SET bootstrap_invite_id=?,bootstrap_ready=1 WHERE singleton=1`, inviteID); err != nil {
		return "", time.Time{}, storage(err)
	}
	return inviteID, expires.UTC(), nil
}

// revokeAuthenticationForRestore invalidates every retained usable bearer in
// the caller's restore transaction while preserving authentication history.
func revokeAuthenticationForRestore(ctx context.Context, tx *store.Tx, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `UPDATE installations SET revoked_at=?,revoke_reason='authority-restored' WHERE revoked_at IS NULL`, now.UnixMicro()); err != nil {
		return storage(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE invites SET state='revoked',revoked_at=?,revoked_by_installation_id=NULL WHERE state='active'`, now.UnixMicro()); err != nil {
		return storage(err)
	}
	return nil
}

// IssueInviteRequest contains only the hash of the client-generated invite.
type IssueInviteRequest struct {
	OperationID     string
	RequestNotAfter time.Time
	InviteID        string
	Role            string
	Label           string
	ExpiresAt       time.Time
	InviteSha256    string
}

type IssueInviteResult struct {
	InviteID               string    `json:"inviteId"`
	Role                   string    `json:"role"`
	Label                  string    `json:"label"`
	ExpiresAt              time.Time `json:"expiresAt"`
	IssuedAt               time.Time `json:"issuedAt"`
	IssuedByInstallationID string    `json:"issuedByInstallationId"`
}

func (s *Service) IssueInvite(ctx context.Context, actor RemoteActor, req IssueInviteRequest) (IssueInviteResult, error) {
	now := s.clock.Now().UTC()
	var out IssueInviteResult
	err := s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		// Authentication is first in the mutation transaction, including for
		// malformed or conflicting invite input.
		if err := s.authorizeRemoteContext(ctx, tx, &actor, "admin"); err != nil {
			return err
		}
		effective, err := s.effectiveNow(tx, now)
		if err != nil {
			return err
		}
		now = effective
		if err := validateOperationID(req.OperationID); err != nil {
			return err
		}
		if err := validateRequestWindow(req.RequestNotAfter, now); err != nil {
			return err
		}
		if !validID(req.InviteID) {
			return reason.Invalid("invite ID must be 32 lowercase hex characters")
		}
		if req.Role != "read" && req.Role != "write" && req.Role != "admin" {
			return reason.Invalid("invite role must be read, write, or admin")
		}
		if err := validatePublicText("invite label", strings.TrimSpace(req.Label)); err != nil {
			return err
		}
		hash := req.InviteSha256
		if err := validateSecretHash(hash, reason.ReasonInviteInvalid); err != nil {
			return err
		}
		expires := req.ExpiresAt.UTC()
		var expiryInput any
		if expires.IsZero() {
			expires = now.Add(inviteLifetime)
		} else {
			expires = expires.Truncate(time.Microsecond)
			expiryInput = expires.UnixMicro()
		}
		// Hash the omitted-vs-explicit input, not the resolved default. This
		// keeps an omitted expiry's exact retry identity stable while replaying
		// the originally resolved expiry.
		requestHash := requestHash(map[string]any{"protocolVersion": "worklease-http/1", "kind": "invite-issue", "authorityId": s.st.AuthorityID(), "expectedRestoreId": actor.ExpectedRestoreID, "installationId": actor.InstallationID, "operationId": req.OperationID, "inviteId": req.InviteID, "role": req.Role, "label": strings.TrimSpace(req.Label), "expiresAt": expiryInput, "inviteSha256": hash, "requestNotAfter": req.RequestNotAfter.UnixMicro()})
		var existing struct {
			id, recordedHash, role, label, issuer string
			expiry, issued                        int64
		}
		err = tx.QueryRowContext(ctx, `SELECT invite_id,issue_request_hash,role,label,expires_at,issued_at,issued_by_installation_id FROM invites WHERE issued_by_installation_id=? AND issue_operation_id=?`, actor.InstallationID, req.OperationID).Scan(&existing.id, &existing.recordedHash, &existing.role, &existing.label, &existing.expiry, &existing.issued, &existing.issuer)
		if err == nil {
			if existing.recordedHash != requestHash {
				return reason.New(reason.ReasonOperationRequestMismatch, "request intent differs from the recorded invite")
			}
			if now.UnixMicro() >= req.RequestNotAfter.UnixMicro() {
				return reason.New(reason.ReasonReplayExpired, "request replay deadline has passed")
			}
			out = IssueInviteResult{InviteID: existing.id, Role: existing.role, Label: existing.label, ExpiresAt: time.UnixMicro(existing.expiry).UTC(), IssuedAt: time.UnixMicro(existing.issued).UTC(), IssuedByInstallationID: existing.issuer}
			return nil
		}
		if !strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return storage(err)
		}
		if !req.ExpiresAt.IsZero() && (expires.Before(now.Add(time.Minute)) || expires.After(now.Add(24*time.Hour))) {
			return reason.Invalid("invite expiry must be 1 minute to 24 hours from authority time")
		}
		mode, _, err := recoveryMode(tx)
		if err != nil {
			return err
		}
		if mode {
			return reason.New(reason.ReasonRecoveryClosed, "invite issuance is closed during recovery")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO invites(invite_id,invite_hash,role,label,issued_at,expires_at,restore_id,issued_by_installation_id,issue_operation_id,issue_request_hash,request_not_after,bootstrap,state) VALUES(?,?,?,?,?,?,?,?,?,?,?,?, 'active')`, req.InviteID, hash, req.Role, strings.TrimSpace(req.Label), now.UnixMicro(), expires.UnixMicro(), s.st.RestoreID(), actor.InstallationID, req.OperationID, requestHash, req.RequestNotAfter.UnixMicro(), 0)
		if err != nil {
			return storage(err)
		}
		out = IssueInviteResult{InviteID: req.InviteID, Role: req.Role, Label: strings.TrimSpace(req.Label), ExpiresAt: expires, IssuedAt: now, IssuedByInstallationID: actor.InstallationID}
		return nil
	})
	return out, err
}

// EnrollRequest carries both client-generated plaintext secrets only in memory.
type EnrollRequest struct {
	AuthorityID       string
	ExpectedRestoreID string
	RequestID         string
	RequestNotAfter   time.Time
	InstallationID    string
	Invite            string
	Credential        string
	Label             string
}

type EnrollResult struct {
	InstallationID string    `json:"installationId"`
	Role           string    `json:"role"`
	Label          string    `json:"label"`
	EnrolledAt     time.Time `json:"enrolledAt"`
}

func (s *Service) Enroll(ctx context.Context, req EnrollRequest) (EnrollResult, error) {
	credential := req.Credential
	invite := req.Invite
	now := s.clock.Now().UTC()
	var out EnrollResult
	err := s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		if err := validateCredential(invite); err != nil {
			return reason.New(reason.ReasonInviteInvalid, "invite is invalid")
		}
		inviteHash := hashSecret(invite)
		var inviteID, role, inviteLabel, restoreID, state, issuer string
		var expiry int64
		var bootstrap int
		err := tx.QueryRowContext(ctx, `SELECT invite_id,role,label,restore_id,state,expires_at,bootstrap,coalesce(issued_by_installation_id,'') FROM invites WHERE invite_hash=?`, inviteHash).Scan(&inviteID, &role, &inviteLabel, &restoreID, &state, &expiry, &bootstrap, &issuer)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "no rows") {
				return reason.New(reason.ReasonInviteInvalid, "invite is invalid")
			}
			return storage(err)
		}
		var redemption struct {
			recordedHash, installation, storedCredential, result string
			replayUntil                                          int64
		}
		err = tx.QueryRowContext(ctx, `SELECT request_hash,installation_id,credential_hash,result,replay_until FROM invite_redemptions WHERE invite_id=?`, inviteID).Scan(&redemption.recordedHash, &redemption.installation, &redemption.storedCredential, &redemption.result, &redemption.replayUntil)
		redemptionFound := err == nil
		if err != nil && !strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return storage(err)
		}
		if !redemptionFound && state != "active" {
			return reason.New(reason.ReasonInviteUsed, "invite has already been redeemed")
		}
		if redemptionFound {
			var revokedAt any
			if err := tx.QueryRowContext(ctx, `SELECT revoked_at FROM installations WHERE installation_id=?`, redemption.installation).Scan(&revokedAt); err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "no rows") {
					return reason.New(reason.ReasonAuthenticationRequired, "installation authentication is required")
				}
				return storage(err)
			}
			if revokedAt != nil {
				return reason.New(reason.ReasonInstallationRevoked, "installation is revoked")
			}
		}
		if req.AuthorityID != s.st.AuthorityID() {
			return reason.New(reason.ReasonAuthorityMismatch, "authority identity does not match")
		}
		if req.ExpectedRestoreID != s.st.RestoreID() {
			return reason.New(reason.ReasonAuthorityRestored, "authority was restored").With("restoreId", s.st.RestoreID())
		}
		effective, err := s.effectiveNow(tx, now)
		if err != nil {
			return err
		}
		now = effective
		if err := validateOperationID(req.RequestID); err != nil {
			return err
		}
		if err := validateRequestWindow(req.RequestNotAfter, now); err != nil {
			return err
		}
		if !validID(req.InstallationID) {
			return reason.Invalid("installation ID must be 32 lowercase hex characters")
		}
		if err := validateCredential(credential); err != nil {
			return reason.New(reason.ReasonInviteInvalid, "installation credential is invalid")
		}
		label := strings.TrimSpace(req.Label)
		if err := validatePublicText("installation label", label); err != nil {
			return err
		}
		credentialHash := hashSecret(credential)
		requestHash := requestHash(map[string]any{"protocolVersion": "worklease-http/1", "kind": "enroll", "authorityId": req.AuthorityID, "expectedRestoreId": req.ExpectedRestoreID, "requestId": req.RequestID, "installationId": req.InstallationID, "inviteSha256": inviteHash, "credentialSha256": credentialHash, "label": label, "requestNotAfter": req.RequestNotAfter.UnixMicro()})
		if redemptionFound {
			if subtle.ConstantTimeCompare([]byte(redemption.storedCredential), []byte(credentialHash)) != 1 || redemption.recordedHash != requestHash {
				return reason.New(reason.ReasonOperationRequestMismatch, "request intent differs from the recorded enrollment")
			}
			if now.UnixMicro() >= redemption.replayUntil {
				return reason.New(reason.ReasonReplayExpired, "enrollment replay has expired")
			}
			if json.Unmarshal([]byte(redemption.result), &out) != nil {
				return reason.New(reason.ReasonStorageFailure, "stored enrollment result is invalid")
			}
			return nil
		}
		if restoreID != s.st.RestoreID() {
			return reason.New(reason.ReasonAuthorityRestored, "invite belongs to an earlier authority incarnation").With("restoreId", s.st.RestoreID())
		}
		if now.UnixMicro() >= expiry {
			return reason.New(reason.ReasonInviteExpired, "invite has expired")
		}
		mode, _, err := recoveryMode(tx)
		if err != nil {
			return err
		}
		if mode && (bootstrap == 0 || role != "admin") {
			return reason.New(reason.ReasonRecoveryClosed, "ordinary enrollment is closed during recovery")
		}
		if bootstrap != 0 {
			if role != "admin" {
				return reason.New(reason.ReasonInviteInvalid, "bootstrap invite is not an admin invite")
			}
			var bootstrapID sql.NullString
			var ready int
			if err := tx.QueryRowContext(ctx, `SELECT bootstrap_invite_id,bootstrap_ready FROM recovery_state WHERE singleton=1`).Scan(&bootstrapID, &ready); err != nil {
				return storage(err)
			}
			if ready == 0 || !bootstrapID.Valid || bootstrapID.String != inviteID {
				return reason.New(reason.ReasonInviteInvalid, "bootstrap invite is not current")
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO installations(installation_id,credential_hash,role,label,enrolled_at,restore_id,enrolled_by_invite_id,issuer_installation_id,request_id,request_hash) VALUES(?,?,?,?,?,?,?,?,?,?)`, req.InstallationID, credentialHash, role, label, now.UnixMicro(), s.st.RestoreID(), inviteID, nullString(issuer), req.RequestID, requestHash); err != nil {
			return storage(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE invites SET state='used',used_at=?,used_by_installation_id=? WHERE invite_id=? AND state='active'`, now.UnixMicro(), req.InstallationID, inviteID); err != nil {
			return storage(err)
		}
		out = EnrollResult{InstallationID: req.InstallationID, Role: role, Label: label, EnrolledAt: now}
		encoded, _ := json.Marshal(out)
		replayUntil := now.Add(inviteReplayLifetime)
		if replayUntil.After(req.RequestNotAfter) {
			replayUntil = req.RequestNotAfter
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO invite_redemptions(invite_id,request_id,request_hash,request_not_after,expected_restore_id,installation_id,credential_hash,result,redeemed_at,replay_until) VALUES(?,?,?,?,?,?,?,?,?,?)`, inviteID, req.RequestID, requestHash, req.RequestNotAfter.UnixMicro(), req.ExpectedRestoreID, req.InstallationID, credentialHash, string(encoded), now.UnixMicro(), replayUntil.UnixMicro())
		return storage(err)
	})
	return out, err
}

// InstallationView is the redacted administrative installation projection.
type InstallationView struct {
	InstallationID          string     `json:"installationId"`
	Role                    string     `json:"role"`
	Label                   string     `json:"label"`
	EnrolledAt              time.Time  `json:"enrolledAt"`
	RevokedAt               *time.Time `json:"revokedAt,omitempty"`
	RevokedByInstallationID string     `json:"revokedByInstallationId,omitempty"`
}

func (s *Service) ListInstallations(ctx context.Context, actor RemoteActor, includeRevoked bool) ([]InstallationView, error) {
	var out []InstallationView
	err := s.st.Read(ctx, func(tx *store.Tx) error {
		if err := s.authorizeRemoteContext(ctx, tx, &actor, "admin"); err != nil {
			return err
		}
		q := `SELECT installation_id,role,label,enrolled_at,revoked_at,coalesce(revoked_by_installation_id,'') FROM installations`
		if !includeRevoked {
			q += ` WHERE revoked_at IS NULL`
		}
		q += ` ORDER BY enrolled_at,installation_id`
		rows, err := tx.QueryContext(ctx, q)
		if err != nil {
			return storage(err)
		}
		defer rows.Close()
		for rows.Next() {
			var id, role, label, revokedBy string
			var enrolled int64
			var revokedAt *int64
			if err := rows.Scan(&id, &role, &label, &enrolled, &revokedAt, &revokedBy); err != nil {
				return storage(err)
			}
			view := InstallationView{InstallationID: id, Role: role, Label: label, EnrolledAt: time.UnixMicro(enrolled).UTC(), RevokedByInstallationID: revokedBy}
			if revokedAt != nil {
				v := time.UnixMicro(*revokedAt).UTC()
				view.RevokedAt = &v
			}
			out = append(out, view)
		}
		return storage(rows.Err())
	})
	return out, err
}

type RevokeInstallationRequest struct {
	InstallationID  string
	Reason          string
	OperationID     string
	RequestNotAfter time.Time
}

type RevokeInstallationResult struct {
	InstallationID          string    `json:"installationId"`
	RevokedAt               time.Time `json:"revokedAt"`
	RevokedByInstallationID string    `json:"revokedByInstallationId"`
}

func (s *Service) RevokeInstallation(ctx context.Context, actor RemoteActor, req RevokeInstallationRequest) (RevokeInstallationResult, error) {
	var out RevokeInstallationResult
	now := s.clock.Now().UTC()
	text := strings.TrimSpace(req.Reason)
	if text == "" {
		text = "revoked"
	}
	err := s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		if err := s.authorizeRemoteContext(ctx, tx, &actor, "admin"); err != nil {
			return err
		}
		effective, err := s.effectiveNow(tx, now)
		if err != nil {
			return err
		}
		now = effective
		requestHash := requestHash(map[string]any{"protocolVersion": "worklease-http/1", "kind": "installation-revoke", "authorityId": actor.AuthorityID, "expectedRestoreId": actor.ExpectedRestoreID, "installationId": actor.InstallationID, "targetInstallationId": req.InstallationID, "operationId": req.OperationID, "reason": text, "requestNotAfter": req.RequestNotAfter.UnixMicro()})
		if err := validateOperationID(req.OperationID); err != nil {
			return err
		}
		if err := validateRequestWindow(req.RequestNotAfter, now); err != nil {
			return err
		}
		if !validID(req.InstallationID) {
			return reason.Invalid("installation ID must be 32 lowercase hex characters")
		}
		if req.InstallationID == actor.InstallationID {
			return reason.Invalid("an installation cannot revoke itself")
		}
		if err := validatePublicText("revocation reason", text); err != nil {
			return err
		}
		var replayHash, replayResult string
		var replayDeadline int64
		err = tx.QueryRowContext(ctx, `SELECT request_hash,request_not_after,result FROM admin_operation_replays WHERE actor_installation_id=? AND operation_id=?`, actor.InstallationID, req.OperationID).Scan(&replayHash, &replayDeadline, &replayResult)
		if err == nil {
			if replayHash != requestHash {
				return reason.New(reason.ReasonOperationRequestMismatch, "request intent differs from the recorded installation revocation")
			}
			if now.UnixMicro() >= replayDeadline {
				return reason.New(reason.ReasonReplayExpired, "request replay deadline has passed")
			}
			if err := json.Unmarshal([]byte(replayResult), &out); err != nil {
				return storage(err)
			}
			return nil
		}
		if !strings.Contains(strings.ToLower(err.Error()), "no rows") {
			return storage(err)
		}
		var revokedAt *int64
		var revokedBy string
		err = tx.QueryRowContext(ctx, `SELECT revoked_at,coalesce(revoked_by_installation_id,'') FROM installations WHERE installation_id=?`, req.InstallationID).Scan(&revokedAt, &revokedBy)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "no rows") {
				return reason.New(reason.ReasonAuthenticationRequired, "installation authentication is required")
			}
			return storage(err)
		}
		if revokedAt != nil {
			out = RevokeInstallationResult{InstallationID: req.InstallationID, RevokedAt: time.UnixMicro(*revokedAt).UTC(), RevokedByInstallationID: revokedBy}
			encoded, _ := json.Marshal(out)
			if _, err := tx.ExecContext(ctx, `INSERT INTO admin_operation_replays(actor_installation_id,operation_id,request_hash,request_not_after,result) VALUES(?,?,?,?,?)`, actor.InstallationID, req.OperationID, requestHash, req.RequestNotAfter.UnixMicro(), string(encoded)); err != nil {
				return storage(err)
			}
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE installations SET revoked_at=?,revoked_by_installation_id=?,revoke_reason=? WHERE installation_id=? AND revoked_at IS NULL`, now.UnixMicro(), actor.InstallationID, text, req.InstallationID); err != nil {
			return storage(err)
		}
		out = RevokeInstallationResult{InstallationID: req.InstallationID, RevokedAt: now, RevokedByInstallationID: actor.InstallationID}
		encoded, _ := json.Marshal(out)
		if _, err := tx.ExecContext(ctx, `INSERT INTO admin_operation_replays(actor_installation_id,operation_id,request_hash,request_not_after,result) VALUES(?,?,?,?,?)`, actor.InstallationID, req.OperationID, requestHash, req.RequestNotAfter.UnixMicro(), string(encoded)); err != nil {
			return storage(err)
		}
		return nil
	})
	return out, err
}

// GCAuthentication removes only expired redemption replay rows. The invite
// remains burned, so a post-GC redemption can never enroll again.
func (s *Service) GCAuthentication(ctx context.Context) (int64, error) {
	now := s.clock.Now().UTC().UnixMicro()
	var removed int64
	err := s.st.WriteAt(ctx, time.UnixMicro(now).UTC(), func(tx *store.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM invite_redemptions WHERE replay_until<=?`, now)
		if err != nil {
			return storage(err)
		}
		removed, err = result.RowsAffected()
		if err != nil {
			return storage(err)
		}
		result, err = tx.ExecContext(ctx, `DELETE FROM admin_operation_replays WHERE request_not_after<=?`, now)
		if err != nil {
			return storage(err)
		}
		count, err := result.RowsAffected()
		removed += count
		return err
	})
	return removed, err
}
