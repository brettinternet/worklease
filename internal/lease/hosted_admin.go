package lease

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

// BootstrapResult is the non-secret result of an offline bootstrap mutation.
type BootstrapResult struct {
	AuthorityID string    `json:"authorityId"`
	RestoreID   string    `json:"restoreId"`
	InviteID    string    `json:"inviteId"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type HostedRestoreRequest struct {
	SelectedCutoff time.Time
	LossStart      time.Time
	LossEnd        time.Time
	CutoffUnknown  bool
}

func composeOfflineBootstrap(ctx context.Context, tx *store.Tx, now time.Time, restoreID, secret string) (BootstrapResult, error) {
	inviteID, expiresAt, err := insertOfflineBootstrap(ctx, tx, now, restoreID, secret)
	if err != nil {
		return BootstrapResult{}, err
	}
	return BootstrapResult{InviteID: inviteID, RestoreID: restoreID, ExpiresAt: expiresAt}, nil
}

// HostedInitialize composes the first schema's bootstrap grant. The caller
// has already durably created the owner-private secret and acquired the lock.
func (s *Service) HostedInitialize(ctx context.Context, secret string) (BootstrapResult, error) {
	var result BootstrapResult
	now := s.clock.Now().UTC()
	err := s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM invites WHERE bootstrap=1 AND state='active'`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			var existingID, existingRestore, existingHash string
			var expires int64
			if err := tx.QueryRowContext(ctx, `SELECT invite_id,restore_id,invite_hash,expires_at FROM invites WHERE bootstrap=1 AND state='active' LIMIT 1`).Scan(&existingID, &existingRestore, &existingHash, &expires); err != nil {
				return err
			}
			if time.UnixMicro(expires).Before(now) || time.UnixMicro(expires).Equal(now) {
				// An unredeemed expired bootstrap is safe to replace in place:
				// it has never authenticated an installation and the owner file
				// is rewritten only after this transaction commits.
				if _, err := tx.ExecContext(ctx, `UPDATE invites SET state='revoked',revoked_at=? WHERE invite_id=? AND state='active'`, now.UnixMicro(), existingID); err != nil {
					return err
				}
			} else {
				if existingHash != HashSecret(secret) {
					return reason.Invalid("staged bootstrap secret differs from the recorded grant")
				}
				result = BootstrapResult{InviteID: existingID, RestoreID: existingRestore, ExpiresAt: time.UnixMicro(expires).UTC()}
				return nil
			}
		}
		var restore string
		if err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='restore_id'`).Scan(&restore); err != nil {
			return err
		}
		var err error
		result, err = composeOfflineBootstrap(ctx, tx, now, restore, secret)
		return err
	})
	if err != nil {
		return BootstrapResult{}, err
	}
	result.AuthorityID = s.st.AuthorityID()
	return result, nil
}

// HostedRestore runs after a validated owner-private backup has been installed
// and opened under the same lock. All lifecycle and recovery changes commit in
// one transaction; the secret was staged before this function was called.
func (s *Service) HostedRestore(ctx context.Context, req HostedRestoreRequest, secret string) (BootstrapResult, error) {
	var result BootstrapResult
	now := s.clock.Now().UTC().Truncate(time.Microsecond)
	newRestore := RandomIDs{}.Generate()
	err := s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		rows, err := tx.QueryContext(ctx, claimSelect)
		if err != nil {
			return err
		}
		var claims []claimRow
		for rows.Next() {
			var row claimRow
			if err := rows.Scan(row.scanArgs()...); err != nil {
				rows.Close()
				return err
			}
			if err := loadResources(tx, &row); err != nil {
				rows.Close()
				return err
			}
			claims = append(claims, row)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		for _, row := range claims {
			if err := endClaim(tx, row, now.UnixMicro(), "restored", "restored", nil); err != nil {
				return err
			}
		}
		if err := revokeAuthenticationForRestore(ctx, tx, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE meta SET value=? WHERE key='restore_id'`, newRestore); err != nil {
			return err
		}
		cutoff, cutoffKnown, start, end, startKnown, endKnown := nullableTime(req.SelectedCutoff), !req.CutoffUnknown, nullableTime(req.LossStart), nullableTime(req.LossEnd), !req.LossStart.IsZero(), !req.LossEnd.IsZero()
		if req.CutoffUnknown {
			cutoff = nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE recovery_state SET recovery_mode=1,recovery_revision=recovery_revision+1,restored_at=?,selected_durable_cutoff=?,cutoff_known=?,loss_interval_start=?,loss_interval_end=?,loss_start_known=?,loss_end_known=?,coverage_gaps='[]',bootstrap_invite_id=NULL,bootstrap_ready=0 WHERE singleton=1`, now.UnixMicro(), cutoff, boolInt(cutoffKnown), start, end, boolInt(startKnown), boolInt(endKnown)); err != nil {
			return err
		}
		result, err = composeOfflineBootstrap(ctx, tx, now, newRestore, secret)
		return err
	})
	if err != nil {
		return BootstrapResult{}, err
	}
	s.st.SetRestoreID(newRestore)
	result.AuthorityID, result.RestoreID = s.st.AuthorityID(), newRestore
	return result, nil
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Truncate(time.Microsecond).UnixMicro()
}

// HostedBootstrapReissue invalidates only the active bootstrap invite.
func (s *Service) HostedBootstrapReissue(ctx context.Context, secret string) (BootstrapResult, error) {
	var result BootstrapResult
	now := s.clock.Now().UTC().Truncate(time.Microsecond)
	err := s.st.WriteAt(ctx, now, func(tx *store.Tx) error {
		var restore string
		if err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='restore_id'`).Scan(&restore); err != nil {
			return err
		}
		var old sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT bootstrap_invite_id FROM recovery_state WHERE singleton=1 AND bootstrap_ready=1`).Scan(&old); err != nil {
			return reason.New(reason.ReasonInviteInvalid, "bootstrap invite is unavailable")
		}
		if old.Valid && old.String != "" {
			var existingHash, existingRestore, state string
			var existingExpiry int64
			err := tx.QueryRowContext(ctx, `SELECT invite_hash,restore_id,expires_at,state FROM invites WHERE invite_id=?`, old.String).Scan(&existingHash, &existingRestore, &existingExpiry, &state)
			if err == nil {
				restore = existingRestore
				if state == "active" && existingHash == HashSecret(secret) {
					result = BootstrapResult{InviteID: old.String, RestoreID: restore, ExpiresAt: time.UnixMicro(existingExpiry).UTC()}
					return nil
				}
				if state == "active" {
					if _, err := tx.ExecContext(ctx, `UPDATE invites SET state='revoked',revoked_at=? WHERE invite_id=? AND state='active'`, now.UnixMicro(), old.String); err != nil {
						return err
					}
				}
			} else if err != sql.ErrNoRows {
				return err
			}
		}
		var err error
		result, err = composeOfflineBootstrap(ctx, tx, now, restore, secret)
		return err
	})
	if err != nil {
		return BootstrapResult{}, err
	}
	result.AuthorityID, result.RestoreID = s.st.AuthorityID(), s.st.RestoreID()
	return result, nil
}

type ActiveClaimRecord struct {
	ClaimID   string    `json:"claimId"`
	AgentID   string    `json:"agentId"`
	SessionID string    `json:"sessionId"`
	WorkKey   string    `json:"workKey"`
	ExpiresAt time.Time `json:"expiresAt"`
	Resources []string  `json:"resources"`
}

type UnresolvedRecord struct {
	ClaimID     string    `json:"claimId"`
	OperationID string    `json:"operationId"`
	Kind        string    `json:"kind"`
	RequestHash string    `json:"requestSha256"`
	StartedAt   time.Time `json:"startedAt"`
	Resources   []string  `json:"resources"`
}
type RetirementStatus struct {
	ActiveClaims int
	Unresolved   int
}

func (s *Service) HostedRetirementStatus(ctx context.Context) (RetirementStatus, error) {
	var out RetirementStatus
	now := s.clock.Now().UTC().UnixMicro()
	err := s.st.Read(ctx, func(tx *store.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM claims WHERE expires_at>?`, now).Scan(&out.ActiveClaims); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT count(*) FROM operations WHERE state='started'`).Scan(&out.Unresolved)
	})
	return out, err
}

// WriteHostedRetirementExport streams bounded redacted records and a final
// count/hash manifest. No credential, request body, checkpoint, receipt, argv,
// reconciliation evidence, or evidence body is selected from SQLite.
func (s *Service) WriteHostedRetirementExport(ctx context.Context, writer io.Writer) (RetirementStatus, error) {
	var status RetirementStatus
	now := s.clock.Now().UTC().UnixMicro()
	err := s.st.Read(ctx, func(tx *store.Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM claims WHERE expires_at>?`, now).Scan(&status.ActiveClaims); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM operations WHERE state='started'`).Scan(&status.Unresolved); err != nil {
			return err
		}
		digest := sha256.New()
		writeRecord := func(value any, includeInManifest bool) error {
			encoded, err := json.Marshal(value)
			if err != nil {
				return err
			}
			if len(encoded) > 256*1024 {
				return reason.New(reason.ReasonStorageFailure, "retirement export record exceeds its bound")
			}
			encoded = append(encoded, '\n')
			if includeInManifest {
				if _, err := digest.Write(encoded); err != nil {
					return err
				}
			}
			_, err = writer.Write(encoded)
			return err
		}
		header := struct {
			RecordType               string `json:"recordType"`
			Version                  int    `json:"version"`
			RecordCount              int    `json:"recordCount"`
			ActiveClaimCount         int    `json:"activeClaimCount"`
			UnresolvedOperationCount int    `json:"unresolvedOperationCount"`
		}{"header", 1, status.ActiveClaims + status.Unresolved, status.ActiveClaims, status.Unresolved}
		if err := writeRecord(header, true); err != nil {
			return err
		}
		seenClaims := 0
		claimRows, err := tx.QueryContext(ctx, `SELECT claim_id,agent_id,session_id,work_key,expires_at FROM claims WHERE expires_at>? ORDER BY claim_id`, now)
		if err != nil {
			return err
		}
		for claimRows.Next() {
			var claim ActiveClaimRecord
			var expires int64
			if err := claimRows.Scan(&claim.ClaimID, &claim.AgentID, &claim.SessionID, &claim.WorkKey, &expires); err != nil {
				claimRows.Close()
				return err
			}
			claim.ExpiresAt = time.UnixMicro(expires).UTC()
			claim.Resources, err = resourcesForOperation(tx, claim.ClaimID)
			if err != nil {
				claimRows.Close()
				return err
			}
			if err := writeRecord(struct {
				RecordType string `json:"recordType"`
				ActiveClaimRecord
			}{"activeClaim", claim}, true); err != nil {
				claimRows.Close()
				return err
			}
			seenClaims++
		}
		if err := claimRows.Close(); err != nil {
			return err
		}
		seenOperations := 0
		operationRows, err := tx.QueryContext(ctx, `SELECT claim_id,operation_id,kind,request_hash,started_at FROM operations WHERE state='started' ORDER BY claim_id,operation_id`)
		if err != nil {
			return err
		}
		for operationRows.Next() {
			var operation UnresolvedRecord
			var started int64
			if err := operationRows.Scan(&operation.ClaimID, &operation.OperationID, &operation.Kind, &operation.RequestHash, &started); err != nil {
				operationRows.Close()
				return err
			}
			operation.StartedAt = time.UnixMicro(started).UTC()
			operation.Resources, err = resourcesForOperation(tx, operation.ClaimID)
			if err != nil {
				operationRows.Close()
				return err
			}
			if err := writeRecord(struct {
				RecordType string `json:"recordType"`
				UnresolvedRecord
			}{"unresolvedOperation", operation}, true); err != nil {
				operationRows.Close()
				return err
			}
			seenOperations++
		}
		if err := operationRows.Close(); err != nil {
			return err
		}
		if seenClaims != status.ActiveClaims || seenOperations != status.Unresolved {
			return reason.New(reason.ReasonStorageFailure, "retirement export record count changed")
		}
		manifest := struct {
			RecordType  string `json:"recordType"`
			RecordCount int    `json:"recordCount"`
			SHA256      string `json:"sha256"`
		}{"manifest", seenClaims + seenOperations, hex.EncodeToString(digest.Sum(nil))}
		return writeRecord(manifest, false)
	})
	return status, err
}

func resourcesForOperation(tx *store.Tx, claim string) ([]string, error) {
	rows, err := tx.QueryContext(context.Background(), `SELECT resource FROM epoch_resources WHERE claim_id=? ORDER BY position`, claim)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
