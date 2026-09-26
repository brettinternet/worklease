package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/reason"
)

// queueWriteClaim reuses the queue's private handle and the ordinary CLI
// checkpoint transaction. The latter owns local pending-handle recovery and
// remote replay; provider writes never update Worklease handles directly.
type queueWriteClaim struct {
	backend *authorityContext
	path    string
	session string
}

// preservesHold reports whether the queue or MCP admitted this handle with a
// hold ceiling. A queue write checkpoint keeps it, so the normal renewal
// lifecycle continues within the original bound.
func (c queueWriteClaim) preservesHold(h handle.Handle) bool {
	return !h.HoldUntil.IsZero()
}

func (c queueWriteClaim) read(intent queue.WriteIntent) (handle.Handle, error) {
	h, err := handle.Read(c.path)
	if err != nil {
		return h, err
	}
	if h.AuthorityID != intent.AuthorityID || h.AuthorityID != c.backend.AuthorityID() || h.ClaimID != intent.ClaimID || !slices.Equal(h.Resources, intent.Resources) || c.session != "" && h.SessionID != c.session || c.backend.Profile != nil && h.RestoreID != c.backend.Profile.RestoreID {
		return h, reason.New(reason.ReasonAuthorityMismatch, "queue recovery handle does not match journaled claim and resources")
	}
	return h, nil
}

func (c queueWriteClaim) Verify(ctx context.Context, intent queue.WriteIntent) error {
	h, err := c.read(intent)
	if err != nil {
		return err
	}
	if h.State != "ready" || h.PendingRequest != nil || h.RecoveryRequest != nil {
		return reason.New(reason.ReasonRecoveryRequired, "queue handle has a pending request")
	}
	credentials := (queueLifecycle{controller: &queueClaimController{backend: c.backend}}).credentials(c.path, h)
	verified, err := c.backend.API.Verify(ctx, credentials, intent.Resources)
	if err != nil {
		return err
	}
	if !verified.Claim.Active || verified.Claim.ClaimID != h.ClaimID || verified.Claim.Revision != h.Revision || len(verified.UnknownOperations) > 0 || len(verified.Claim.UnknownOperations) > 0 {
		return reason.New(reason.ReasonRecoveryRequired, "queue write claim unverified or operation unresolved")
	}
	return nil
}

func queueCheckpointData(intent queue.WriteIntent, receipt queue.ProviderReceipt) (string, error) {
	data, err := json.Marshal(map[string]any{"providerOperationId": intent.OperationID, "providerReceipt": receipt})
	if err != nil {
		return "", err
	}
	if len(data) > 8*1024 {
		return "", fmt.Errorf("provider receipt exceeds checkpoint limit")
	}
	return string(data), nil
}

// CheckpointAuthorityTime is a conservative lower bound for the original
// authority, never the queue client's wall clock.
func (c queueWriteClaim) CheckpointAuthorityTime() (time.Time, error) {
	if c.backend.HTTP != nil {
		return c.backend.HTTP.Clock().LowerBound()
	}
	if c.backend.Local != nil {
		return c.backend.Local.AuthorityNow(), nil
	}
	return time.Time{}, fmt.Errorf("original authority time unavailable")
}

func (c queueWriteClaim) CheckpointStatus(ctx context.Context, intent queue.WriteIntent, receipt queue.ProviderReceipt) (queue.WriteVerification, error) {
	h, err := c.read(intent)
	if err != nil {
		return queue.WriteUnknown, err
	}
	operation, err := c.backend.API.Inspect(ctx, ledger.InspectRequest{OperationID: intent.OperationRef, ClaimID: intent.ClaimID, Full: true, HandlePath: c.path, Token: h.Token, CredentialPath: c.credentialPath()})
	if err != nil {
		if classified := reason.As(err); classified != nil && classified.Reason == reason.ReasonOperationNotFound {
			return queue.WriteUnknown, nil
		}
		return queue.WriteUnknown, err
	}
	if operation.Kind != "checkpoint" || operation.ClaimID != intent.ClaimID {
		return queue.WriteConflict, nil
	}
	if operation.State != "completed" {
		return queue.WriteUnknown, nil
	}
	if operation.RequestNotAfter == nil || operation.RequestNotAfter.UnixMicro() != intent.CheckpointNotAfter.UnixMicro() {
		return queue.WriteConflict, nil
	}
	data, err := queueCheckpointData(intent, receipt)
	if err != nil {
		return queue.WriteUnknown, err
	}
	var actor *lease.RemoteActor
	if c.backend.Remote {
		if operation.InstallationID == "" || operation.RestoreID == "" || c.backend.Profile == nil || operation.RestoreID != c.backend.Profile.RestoreID {
			return queue.WriteConflict, nil
		}
		actor = &lease.RemoteActor{InstallationID: operation.InstallationID, ExpectedRestoreID: operation.RestoreID}
	}
	hold := time.Time{}
	if !c.backend.Remote && c.preservesHold(h) {
		hold = h.HoldUntil
	}
	hash, err := lease.CheckpointRequestHash(intent.AuthorityID, intent.ClaimID, []byte(data), intent.CheckpointTTL, intent.CheckpointNotAfter, hold, actor)
	if err != nil {
		return queue.WriteUnknown, err
	}
	if operation.RequestSHA256 != hash {
		return queue.WriteConflict, nil
	}
	var checkpoint struct {
		Revision     int64     `json:"revision"`
		ExpiresAt    time.Time `json:"expiresAt"`
		Checkpointed bool      `json:"checkpointed"`
	}
	encoded, err := json.Marshal(operation.Receipt)
	if err == nil {
		err = json.Unmarshal(encoded, &checkpoint)
	}
	if err != nil || !checkpoint.Checkpointed || checkpoint.Revision < 1 || checkpoint.ExpiresAt.IsZero() {
		return queue.WriteUnknown, fmt.Errorf("checkpoint receipt incomplete")
	}
	if err := c.restoreCheckpointHandle(ctx, intent, checkpoint.Revision, checkpoint.ExpiresAt); err != nil {
		return queue.WriteUnknown, err
	}
	return queue.WriteVerified, nil
}

func (c queueWriteClaim) restoreCheckpointHandle(ctx context.Context, intent queue.WriteIntent, revision int64, expiry time.Time) error {
	lock, err := handle.AcquireLock(ctx, c.path+".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	h, err := lock.Read(c.path)
	if err != nil {
		return err
	}
	if h.AuthorityID != intent.AuthorityID || h.ClaimID != intent.ClaimID || !slices.Equal(h.Resources, intent.Resources) || c.session != "" && h.SessionID != c.session || h.RecoveryRequest != nil {
		return reason.New(reason.ReasonRecoveryRequired, "checkpoint handle identity changed")
	}
	if h.State == "ready" && h.PendingRequest == nil && h.Revision >= revision {
		return nil
	}
	if h.PendingRequest != nil && h.PendingRequest.OperationID != intent.OperationRef {
		return reason.New(reason.ReasonRecoveryRequired, "different handle operation is pending")
	}
	if h.State != "ready" && h.State != "pending" {
		return reason.New(reason.ReasonRecoveryRequired, "checkpoint handle is not recoverable")
	}
	h.State, h.PendingRequest = "ready", nil
	h.Revision, h.ExpiresAt = revision, expiry
	if !c.preservesHold(h) {
		h.HoldUntil = time.Time{}
	}
	return lock.Write(c.path, h)
}

func (c queueWriteClaim) credentialPath() string {
	if c.backend.Profile != nil {
		return c.backend.Profile.Credential.Path
	}
	return ""
}

// queuePreserveHold keeps a queue- or MCP-owned lease inside its admitted hold budget
// when the journaled write checkpoints through the ordinary CLI lifecycle.
type queuePreserveHold struct{}

func (c queueWriteClaim) Checkpoint(ctx context.Context, intent queue.WriteIntent, receipt queue.ProviderReceipt) error {
	h, err := c.read(intent)
	if err != nil {
		return err
	}
	if !c.backend.Remote && c.preservesHold(h) {
		ctx = context.WithValue(ctx, queuePreserveHold{}, true)
	}
	data, err := queueCheckpointData(intent, receipt)
	if err != nil {
		return err
	}
	args := []string{"worklease", "--home", c.backend.Config.Home}
	if c.backend.Profile != nil {
		args = append(args, "--profile", c.backend.ProfileName)
	} else {
		args = append(args, "--local")
	}
	args = append(args, "checkpoint", "--handle", c.path, "--operation-id", intent.OperationRef, "--ttl", intent.CheckpointTTL.String(), "--request-not-after", intent.CheckpointNotAfter.UTC().Format(time.RFC3339Nano), "--data", data)
	var stdout, stderr bytes.Buffer
	if err := Run(ctx, args, "", "", "", &stdout, &stderr); err != nil {
		return fmt.Errorf("queue checkpoint: %w: %s", err, stderr.String())
	}
	return nil
}
