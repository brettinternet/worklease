package authority

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
)

var errHandlePending = errors.New("handle has a different pending request")

type handleReplacement struct {
	ClaimID   string
	Token     string
	Revision  int64
	ExpiresAt time.Time
}

// sameResourceSet compares resource membership. The protocol does not define
// resource order as grant identity, so an authority that canonicalizes order
// must not permanently wedge a committed pending acquire.
func sameResourceSet(request, grant []string) bool {
	if len(request) != len(grant) {
		return false
	}
	left, right := slices.Clone(request), slices.Clone(grant)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
}

// persistHandleRequest uses the existing owner-private handle slot for named
// claim mutations. A different unresolved request can never replace it.
func persistHandleRequest(path, claimID string, p PendingRequest, newToken string, replacement handleReplacement) error {
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	lock, err := handle.AcquireLock(context.Background(), path+".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	h, err := lock.Read(path)
	var replaced *handle.Handle
	var request struct {
		ClaimID, AgentID, SessionID string
		Resources                   []string
	}
	if p.Kind == "acquire" {
		if decodeErr := json.Unmarshal(p.Request, &request); decodeErr != nil || request.ClaimID != claimID {
			return fmt.Errorf("pending acquire request is malformed")
		}
	}
	if err != nil {
		present, metadataErr := handle.ValidateMetadata(path)
		if p.Kind != "acquire" || metadataErr != nil || present {
			return err
		}
		h = handle.Handle{SchemaVersion: handle.RemoteSchemaVersion, AuthorityID: p.AuthorityID, RestoreID: p.ExpectedRestoreID, ClaimID: claimID, Token: newToken, Resources: request.Resources, AgentID: request.AgentID, SessionID: request.SessionID, State: "pending"}
	} else if h.ClaimID != claimID {
		if p.Kind != "acquire" || replacement.ClaimID == "" || h.AuthorityID != p.AuthorityID || h.SchemaVersion != handle.RemoteSchemaVersion || h.State != "ready" || h.PendingRequest != nil || h.RecoveryRequest != nil || h.ClaimID != replacement.ClaimID || h.Token != replacement.Token || h.Revision != replacement.Revision || !h.ExpiresAt.Equal(replacement.ExpiresAt) {
			return fmt.Errorf("handle claim does not match request")
		}
		old := h
		replaced = &old
		h = handle.Handle{SchemaVersion: handle.RemoteSchemaVersion, AuthorityID: p.AuthorityID, RestoreID: p.ExpectedRestoreID, ClaimID: claimID, Token: newToken, Resources: request.Resources, AgentID: request.AgentID, SessionID: request.SessionID, State: "pending"}
	}
	if h.PendingRequest != nil {
		existing := h.PendingRequest
		if existing.RequestHash != p.RequestSHA256 || existing.OperationID != p.OperationID {
			return errHandlePending
		}
		return nil
	}
	h.SchemaVersion = handle.RemoteSchemaVersion
	h.RestoreID = p.ExpectedRestoreID
	h.State = "pending"
	kind := p.Kind
	if kind == "operations/begin" {
		kind = "exec"
	}
	h.PendingRequest = &handle.PendingRequest{OperationID: p.OperationID, Kind: kind, AuthorityID: p.AuthorityID, Endpoint: p.Endpoint, CertificateSHA256: p.CertificateSHA256, ClaimID: h.ClaimID, RequestHash: p.RequestSHA256, RequestNotAfter: p.RequestNotAfter, ExpectedRestoreID: p.ExpectedRestoreID, Request: append([]byte(nil), p.Request...), ParentRequestID: p.ParentRequestID, EffectEvidence: append([]byte(nil), p.EffectEvidence...), Inputs: map[string]any{"request": string(p.Request), "expectedRestoreId": p.ExpectedRestoreID, "parentRequestId": p.ParentRequestID, "newClaimHandleRef": p.NewClaimHandleRef}}
	if kind == "transfer" {
		h.PendingRequest.SuccessorToken = newToken
	}
	if len(p.EffectEvidence) > 0 {
		h.PendingRequest.Inputs["effectEvidence"] = json.RawMessage(p.EffectEvidence)
	}
	if replaced != nil {
		return lock.ReplaceReady(path, *replaced, h)
	}
	return lock.Write(path, h)
}
func setHandleAutoRenewOwner(path, owner string) error {
	lock, err := handle.AcquireLock(context.Background(), path+".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	h, err := lock.Read(path)
	if err != nil {
		return err
	}
	if h.PendingRequest == nil || h.PendingRequest.Kind != "acquire" {
		return errHandlePending
	}
	h.AutoRenewOwner = owner
	return lock.Write(path, h)
}

func activateTransferHandle(predecessorPath, successorPath string, grant lease.Grant) error {
	if predecessorPath == "" || successorPath == "" || predecessorPath == successorPath {
		return fmt.Errorf("transfer handle paths are invalid")
	}
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(successorPath)); err != nil {
		return err
	}
	locks, err := handle.AcquireLocks(context.Background(), predecessorPath+".lock", successorPath+".lock")
	if err != nil {
		return err
	}
	defer func() {
		for _, lock := range locks {
			_ = lock.Close()
		}
	}()
	var predecessorLock, successorLock *handle.Lock
	for _, lock := range locks {
		if lock.Matches(predecessorPath) {
			predecessorLock = lock
		}
		if lock.Matches(successorPath) {
			successorLock = lock
		}
	}
	if predecessorLock == nil || successorLock == nil {
		return fmt.Errorf("transfer handle locks are unavailable")
	}
	predecessor, err := predecessorLock.Read(predecessorPath)
	if err != nil || predecessor.PendingRequest == nil || predecessor.PendingRequest.Kind != "transfer" {
		return errHandlePending
	}
	successor := handle.Handle{SchemaVersion: handle.RemoteSchemaVersion, AuthorityID: grant.AuthorityID, ClaimID: grant.ClaimID, Token: predecessor.PendingRequest.SuccessorToken, Revision: grant.Revision, Resources: append([]string(nil), grant.Resources...), ExpiresAt: grant.ExpiresAt, AgentID: grant.AgentID, SessionID: grant.SessionID, State: "ready"}
	if err := successorLock.Write(successorPath, successor); err != nil {
		return err
	}
	return predecessorLock.Remove(predecessorPath)
}

func activateGrantHandle(path string, grant lease.Grant) error {
	lock, err := handle.AcquireLock(context.Background(), path+".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	h, err := lock.Read(path)
	if err != nil {
		return err
	}
	if h.ClaimID != grant.ClaimID || h.PendingRequest == nil || h.PendingRequest.OperationID != grant.ClaimID || h.PendingRequest.Kind != "acquire" {
		return errHandlePending
	}
	var request struct {
		AuthorityID, ClaimID, AgentID, SessionID, WorkKey string
		Resources                                         []string
	}
	if err := json.Unmarshal(h.PendingRequest.Request, &request); err != nil || request.AuthorityID != grant.AuthorityID || request.ClaimID != grant.ClaimID || request.AgentID != grant.AgentID || request.SessionID != grant.SessionID || request.WorkKey != grant.WorkKey || !sameResourceSet(request.Resources, grant.Resources) {
		return fmt.Errorf("remote grant does not match pending acquire")
	}
	h.Revision, h.ExpiresAt, h.State, h.PendingRequest = grant.Revision, grant.ExpiresAt, "ready", nil
	restoreID := grant.RestoreID
	if restoreID == "" {
		restoreID = h.RestoreID
	}
	if restoreID == "" && h.PendingRequest != nil {
		restoreID = h.PendingRequest.ExpectedRestoreID
	}
	h.AuthorityID, h.RestoreID, h.Resources, h.AgentID, h.SessionID, h.LocalReplaceAllowed = grant.AuthorityID, restoreID, append([]string(nil), grant.Resources...), grant.AgentID, grant.SessionID, grant.LocalReplaceAllowed
	return lock.Write(path, h)
}

func updateHandleRevision(path string, revision int64) error {
	if path == "" || revision == 0 {
		return nil
	}
	lock, err := handle.AcquireLock(context.Background(), path+".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	h, err := lock.Read(path)
	if err != nil {
		return err
	}
	if revision > h.Revision {
		h.Revision = revision
	}
	return lock.Write(path, h)
}

func clearHandleRequest(path, requestID string) error {
	lock, err := handle.AcquireLock(context.Background(), path+".lock")
	if err != nil {
		return err
	}
	defer lock.Close()
	h, err := lock.Read(path)
	if err != nil {
		return err
	}
	if h.PendingRequest == nil || h.PendingRequest.OperationID != requestID {
		return errHandlePending
	}
	return lock.ClearPending(path, &h)
}
