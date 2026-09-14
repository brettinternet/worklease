// Package remoteadmin composes authenticated administrative operations whose
// domain implementation lives outside the lease package.
package remoteadmin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/gc"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

type GCRequest struct {
	OperationID     string
	RequestNotAfter time.Time
	Cutoff          time.Time
	RetentionDays   float64
}

// GC authenticates, resolves exact replay, enforces recovery mode, applies
// retention, and records replay in one serialized authority transaction.
func GC(ctx context.Context, st *store.Store, service *lease.Service, actor lease.RemoteActor, req GCRequest) (gc.Result, error) {
	var result gc.Result
	now := time.Now().UTC()
	err := st.WriteAt(ctx, now, func(tx *store.Tx) error {
		check := service.RemoteActorTransactionCheck(&actor, "admin")
		if err := check(ctx, tx); err != nil {
			return err
		}
		var observedRaw string
		if err := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='last_observed_at'`).Scan(&observedRaw); err != nil {
			return reason.New(reason.ReasonStorageFailure, "read authority time")
		}
		observed, err := strconv.ParseInt(observedRaw, 10, 64)
		if err != nil {
			return reason.New(reason.ReasonSchemaCorrupt, "authority clock watermark is invalid")
		}
		if now.UnixMicro() < observed-1_000_000 {
			return reason.New(reason.ReasonClockRegression, "authority clock moved backwards")
		}
		if now.UnixMicro() < observed {
			now = time.UnixMicro(observed).UTC()
		}
		if !validID(req.OperationID) {
			return reason.Invalid("operation ID must be 32 lowercase hex characters")
		}
		if !req.RequestNotAfter.After(now) || req.RequestNotAfter.After(now.Add(24*time.Hour)) {
			return reason.Invalid("request replay deadline must be after authority time and at most 24 hours ahead")
		}
		requestHash, err := hashRequest(actor, req)
		if err != nil {
			return reason.New(reason.ReasonInternal, "retention request hashing failed")
		}
		var recordedHash, encoded string
		var deadline int64
		err = tx.QueryRowContext(ctx, `SELECT request_hash,request_not_after,result FROM admin_operation_replays WHERE actor_installation_id=? AND operation_id=?`, actor.InstallationID, req.OperationID).Scan(&recordedHash, &deadline, &encoded)
		if err == nil {
			if recordedHash != requestHash {
				return reason.New(reason.ReasonOperationRequestMismatch, "request intent differs from the recorded retention operation")
			}
			if now.UnixMicro() >= deadline {
				return reason.New(reason.ReasonReplayExpired, "request replay deadline has passed")
			}
			if err := json.Unmarshal([]byte(encoded), &result); err != nil {
				return reason.New(reason.ReasonStorageFailure, "stored retention result is invalid")
			}
			return nil
		}
		if !errorsIsNoRows(err) {
			return reason.New(reason.ReasonStorageFailure, "read retention replay")
		}
		var recoveryMode int
		if err := tx.QueryRowContext(ctx, `SELECT recovery_mode FROM recovery_state WHERE singleton=1`).Scan(&recoveryMode); err != nil {
			return reason.New(reason.ReasonStorageFailure, "read recovery state")
		}
		if recoveryMode != 0 {
			return reason.New(reason.ReasonRecoveryClosed, "retention is closed during recovery")
		}
		result, err = gc.NewTransaction(tx).Collect(ctx, gc.Request{Cutoff: req.Cutoff, RetentionDays: req.RetentionDays, Apply: true, Now: now})
		if err != nil {
			return err
		}
		encodedResult, err := json.Marshal(result)
		if err != nil {
			return reason.New(reason.ReasonInternal, "retention result encoding failed")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO admin_operation_replays(actor_installation_id,operation_id,request_hash,request_not_after,result) VALUES(?,?,?,?,?)`, actor.InstallationID, req.OperationID, requestHash, req.RequestNotAfter.UnixMicro(), string(encodedResult)); err != nil {
			return reason.New(reason.ReasonStorageFailure, "record retention replay")
		}
		return nil
	})
	return result, err
}

func hashRequest(actor lease.RemoteActor, req GCRequest) (string, error) {
	var cutoff any
	if !req.Cutoff.IsZero() {
		cutoff = req.Cutoff.UTC().UnixMicro()
	}
	encoded, err := json.Marshal(map[string]any{"protocolVersion": "worklease-http/1", "kind": "gc", "authorityId": actor.AuthorityID, "expectedRestoreId": actor.ExpectedRestoreID, "installationId": actor.InstallationID, "operationId": req.OperationID, "requestNotAfter": req.RequestNotAfter.UnixMicro(), "cutoff": cutoff, "retentionDays": req.RetentionDays, "apply": true})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func validID(value string) bool {
	if len(value) != 32 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func errorsIsNoRows(err error) bool { return err == sql.ErrNoRows }
