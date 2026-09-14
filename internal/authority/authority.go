package authority

import (
	"context"
	"encoding/json"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
	"github.com/brettinternet/worklease/internal/watch"
)

// Authority is the one consumer-facing contract. Consumers do not select a
// backend or open a store; construction chooses LocalAuthority or
// RemoteAuthority. Execute is available for protocol endpoints not yet routed
// by a command consumer.
type Authority interface {
	// Execute covers frozen protocol endpoints while command-specific routing
	// remains deferred to the lifecycle adapters.
	Execute(context.Context, RequestSpec) (Response, error)
	Acquire(context.Context, lease.AcquireRequest) (lease.Grant, error)
	Status(context.Context, lease.Selector) (lease.Status, error)
	List(context.Context, string, *lease.RemoteActor) ([]lease.ClaimView, error)
	Heartbeat(context.Context, lease.Credentials, lease.Renew) (lease.Receipt, error)
	Checkpoint(context.Context, lease.Credentials, lease.CheckpointRequest) (lease.Receipt, error)
	Release(context.Context, lease.Credentials, lease.ReleaseRequest) (lease.Receipt, error)
	Transfer(context.Context, lease.Credentials, lease.TransferRequest) (lease.Grant, error)
	Verify(context.Context, lease.Credentials, []string) (lease.Verification, error)
	BeginOperation(context.Context, lease.Credentials, lease.OperationIntent) (lease.Started, error)
	RenewOperation(context.Context, lease.Credentials, string, time.Duration) (lease.Receipt, error)
	CompleteOperation(context.Context, lease.Credentials, string, map[string]any) (lease.Receipt, error)
	Inspect(context.Context, ledger.InspectRequest) (ledger.Operation, error)
	Reconcile(context.Context, lease.Credentials, lease.ReconcileRequest) (lease.ReconciliationReceipt, error)
	Events(context.Context, string, int) (ledger.EventsPage, error)
	History(context.Context, string, string, int, bool) (ledger.HistoryPage, error)
	Watch(context.Context, watch.Request) (watch.Result, error)
}

type LocalAuthority struct {
	Service      *lease.Service
	Store        *store.Store
	PollInterval time.Duration
}

func NewLocalAuthority(s *lease.Service, st *store.Store, poll time.Duration) (*LocalAuthority, error) {
	if s == nil || st == nil {
		return nil, reason.Invalid("local authority service and store are required")
	}
	return &LocalAuthority{Service: s, Store: st, PollInterval: poll}, nil
}
func (a *LocalAuthority) Execute(context.Context, RequestSpec) (Response, error) {
	return Response{}, reason.New(reason.ReasonInvalidArgument, "raw execution is remote-only")
}
func (a *LocalAuthority) Events(ctx context.Context, cursor string, limit int) (ledger.EventsPage, error) {
	if a.Store == nil {
		return ledger.EventsPage{}, reason.Invalid("local event store is unavailable")
	}
	return ledger.New(a.Store).Events(ctx, cursor, limit)
}
func (a *LocalAuthority) History(ctx context.Context, resource, cursor string, limit int, full bool) (ledger.HistoryPage, error) {
	if a.Store == nil {
		return ledger.HistoryPage{}, reason.Invalid("local history store is unavailable")
	}
	return ledger.New(a.Store).History(ctx, resource, cursor, limit, full)
}
func (a *LocalAuthority) Watch(ctx context.Context, r watch.Request) (watch.Result, error) {
	if a.Store == nil {
		return watch.Result{}, reason.Invalid("local watch store is unavailable")
	}
	r.PollInterval = a.PollInterval
	return watch.Wait(ctx, a.Store, r)
}
func (a *LocalAuthority) Acquire(c context.Context, r lease.AcquireRequest) (lease.Grant, error) {
	return a.Service.Acquire(c, r)
}
func (a *LocalAuthority) Status(c context.Context, r lease.Selector) (lease.Status, error) {
	return a.Service.Status(c, r)
}
func (a *LocalAuthority) List(c context.Context, r string, x *lease.RemoteActor) ([]lease.ClaimView, error) {
	return a.Service.List(c, r, x)
}
func (a *LocalAuthority) Heartbeat(c context.Context, x lease.Credentials, r lease.Renew) (lease.Receipt, error) {
	return a.Service.Heartbeat(c, x, r)
}
func (a *LocalAuthority) Checkpoint(c context.Context, x lease.Credentials, r lease.CheckpointRequest) (lease.Receipt, error) {
	return a.Service.Checkpoint(c, x, r)
}
func (a *LocalAuthority) Release(c context.Context, x lease.Credentials, r lease.ReleaseRequest) (lease.Receipt, error) {
	return a.Service.Release(c, x, r)
}
func (a *LocalAuthority) Transfer(c context.Context, x lease.Credentials, r lease.TransferRequest) (lease.Grant, error) {
	return a.Service.Transfer(c, x, r)
}
func (a *LocalAuthority) Verify(c context.Context, x lease.Credentials, r []string) (lease.Verification, error) {
	return a.Service.Verify(c, x, r)
}
func (a *LocalAuthority) BeginOperation(c context.Context, x lease.Credentials, r lease.OperationIntent) (lease.Started, error) {
	return a.Service.BeginOperation(c, x, r)
}
func (a *LocalAuthority) RenewOperation(c context.Context, x lease.Credentials, id string, ttl time.Duration) (lease.Receipt, error) {
	return a.Service.RenewOperation(c, x, id, ttl)
}
func (a *LocalAuthority) CompleteOperation(c context.Context, x lease.Credentials, id string, r map[string]any) (lease.Receipt, error) {
	return a.Service.CompleteOperation(c, x, id, r)
}
func (a *LocalAuthority) Inspect(c context.Context, r ledger.InspectRequest) (ledger.Operation, error) {
	if a.Store == nil {
		return ledger.Operation{}, reason.Invalid("local operation store is unavailable")
	}
	return ledger.New(a.Store).Inspect(c, r)
}
func (a *LocalAuthority) Reconcile(c context.Context, x lease.Credentials, r lease.ReconcileRequest) (lease.ReconciliationReceipt, error) {
	return a.Service.Reconcile(c, x, r)
}

// RemoteAuthority adapts the frozen HTTP routes to Authority. Its only durable
// state is profile credentials and pending records owned by HTTPClient.
type RemoteAuthority struct{ Client *HTTPClient }

func NewRemoteAuthority(c *HTTPClient) (*RemoteAuthority, error) {
	if c == nil {
		return nil, reason.Invalid("remote HTTP client is required")
	}
	return &RemoteAuthority{Client: c}, nil
}
func (a *RemoteAuthority) request(ctx context.Context, path, kind string, id string, body any, mutating bool, terminal bool, claim string, newClaim string, refs ...string) (json.RawMessage, error) {
	if mutating && a.Client.clock.ResampleNeeded() {
		if _, e := a.Client.Metadata(ctx); e != nil {
			return nil, e
		}
	}
	if m, ok := body.(map[string]any); ok && mutating {
		d, e := a.Client.clock.RequestNotAfter()
		if e != nil {
			return nil, reason.New(reason.ReasonClockRegression, "authority time is unsampled")
		}
		m["requestNotAfter"] = d
	}
	b, e := json.Marshal(body)
	if e != nil {
		return nil, reason.Invalid("remote request encoding failed")
	}
	spec := RequestSpec{Path: path, Kind: kind, RequestID: id, Body: b, Mutating: mutating, Terminal: terminal, ClaimCredential: claim, NewClaimCredential: newClaim}
	if m, ok := body.(map[string]any); ok {
		spec.ClaimID, _ = m["claimId"].(string)
	}
	if len(refs) > 0 {
		spec.ClaimHandlePath = refs[0]
		if mutating {
			spec.HandlePath = refs[0]
		}
	}
	if len(refs) > 1 {
		spec.NewClaimHandlePath = refs[1]
	}
	if len(refs) > 2 {
		spec.TargetOperationID = refs[2]
	}
	if len(refs) > 3 {
		spec.ClaimCredentialPath = refs[3]
	}
	if len(refs) > 4 {
		spec.NewClaimCredentialPath = refs[4]
	}
	if len(refs) > 5 {
		spec.TargetHandlePath = refs[5]
	}
	r, e := a.Client.Call(ctx, spec)
	if e != nil {
		return nil, e
	}
	return r.Result, nil
}
func decodeResult(b json.RawMessage, v any) error {
	if len(b) == 0 {
		return reason.Invalid("remote result is missing")
	}
	if e := decodeStrict(b, v); e != nil {
		return reason.Invalid("remote result is invalid")
	}
	return nil
}
func finish(c *HTTPClient, id, handlePath, expectedOperation, expectedClaim string, b json.RawMessage, out any) error {
	if err := decodeResult(b, out); err != nil {
		return err
	}
	if err := validateResult(out); err != nil {
		return err
	}
	switch x := out.(type) {
	case *lease.Receipt:
		if x.OperationID != expectedOperation || (expectedClaim != "" && x.ClaimID != expectedClaim) {
			return reason.Invalid("remote receipt does not match request")
		}
	case *lease.Grant:
		if x.ClaimID != expectedClaim || x.Receipt.OperationID != expectedOperation || x.Receipt.ClaimID != expectedClaim {
			return reason.Invalid("remote grant does not match request")
		}
	}
	return c.finalize(id, handlePath)
}
func validateResult(v any) error {
	switch x := v.(type) {
	case *lease.Receipt:
		if !validID(x.OperationID) || !validID(x.ClaimID) || x.Kind == "" || !validHash(x.RequestHash) || !x.Committed {
			return reason.Invalid("remote receipt is invalid")
		}
	case *lease.Grant:
		if !validID(x.ClaimID) || !validID(x.AuthorityID) || !x.Receipt.Committed {
			return reason.Invalid("remote grant is invalid")
		}
	case *lease.Started:
		if !validID(x.OperationID) || !validID(x.ClaimID) || x.Kind == "" || !validHash(x.RequestHash) {
			return reason.Invalid("remote operation result is invalid")
		}
	}
	return nil
}
func validHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func common(c *HTTPClient, id string) map[string]any {
	return map[string]any{"protocolVersion": protocolVersion, "authorityId": c.profile.AuthorityID, "expectedRestoreId": c.profile.RestoreID, "operationId": id, "requestNotAfter": nil}
}
func (a *RemoteAuthority) Execute(ctx context.Context, s RequestSpec) (Response, error) {
	response, err := a.Client.Call(ctx, s)
	if err != nil || !s.Mutating || s.Path == "/v1/operations/begin" {
		return response, err
	}
	var result map[string]json.RawMessage
	if err := decodeStrict(response.Result, &result); err != nil || result == nil {
		return Response{}, reason.Invalid("remote mutation result is invalid")
	}
	if err := a.Client.finalize(s.RequestID, s.HandlePath); err != nil {
		return Response{}, err
	}
	return response, nil
}
func (a *RemoteAuthority) Acquire(ctx context.Context, r lease.AcquireRequest) (lease.Grant, error) {
	id := r.ClaimID
	if id == "" {
		return lease.Grant{}, reason.Invalid("claim ID is required")
	}
	q := common(a.Client, id)
	q["claimId"], q["resources"], q["agentId"], q["sessionId"], q["workKey"], q["ttlMicros"], q["maxHoldMicros"], q["coordinationOnly"] = r.ClaimID, r.Resources, r.AgentID, r.SessionID, r.WorkKey, r.TTL.Microseconds(), r.MaxHold.Microseconds(), r.CoordinationOnly
	b, e := a.request(ctx, "/v1/claims/acquire", "acquire", id, q, true, false, "", r.Token, r.HandlePath, r.HandlePath, "", "", r.CredentialPath)
	if e != nil {
		return lease.Grant{}, e
	}
	var out lease.Grant
	if e == nil {
		e = decodeResult(b, &out)
	}
	if e == nil {
		e = validateResult(&out)
	}
	if e == nil && (out.ClaimID != id || out.Receipt.OperationID != id || out.Receipt.ClaimID != id) {
		e = reason.Invalid("remote grant does not match request")
	}
	if e == nil && r.HandlePath != "" {
		e = activateGrantHandle(r.HandlePath, out)
	} else if e == nil {
		e = a.Client.finalize(id, "")
	}
	return out, e
}
func (a *RemoteAuthority) Status(ctx context.Context, r lease.Selector) (lease.Status, error) {
	q := map[string]any{"protocolVersion": protocolVersion, "authorityId": a.Client.profile.AuthorityID, "expectedRestoreId": a.Client.profile.RestoreID}
	q["claimId"], q["resources"] = r.ClaimID, r.Resources
	b, e := a.request(ctx, "/v1/claims/status", "status", "00000000000000000000000000000000", q, false, false, "", "")
	if e != nil {
		return lease.Status{}, e
	}
	var out lease.Status
	e = decodeResult(b, &out)
	return out, e
}
func (a *RemoteAuthority) List(ctx context.Context, r string, x *lease.RemoteActor) ([]lease.ClaimView, error) {
	q := map[string]any{"protocolVersion": protocolVersion, "authorityId": a.Client.profile.AuthorityID, "expectedRestoreId": a.Client.profile.RestoreID}
	q["resource"] = r
	b, e := a.request(ctx, "/v1/claims/list", "list", "00000000000000000000000000000000", q, false, false, "", "")
	if e != nil {
		return nil, e
	}
	var v struct {
		Claims []lease.ClaimView `json:"claims"`
	}
	e = decodeResult(b, &v)
	return v.Claims, e
}
func (a *RemoteAuthority) Heartbeat(ctx context.Context, c lease.Credentials, r lease.Renew) (lease.Receipt, error) {
	q := common(a.Client, r.OperationID)
	q["claimId"], q["revision"], q["ttlMicros"] = c.ClaimID, c.Revision, r.TTL.Microseconds()
	b, e := a.request(ctx, "/v1/claims/heartbeat", "heartbeat", r.OperationID, q, true, false, c.Token, "", c.HandlePath, "", "", c.CredentialPath)
	var out lease.Receipt
	if e == nil {
		e = finish(a.Client, r.OperationID, c.HandlePath, r.OperationID, c.ClaimID, b, &out)
	}
	return out, e
}
func (a *RemoteAuthority) Checkpoint(ctx context.Context, c lease.Credentials, r lease.CheckpointRequest) (lease.Receipt, error) {
	q := common(a.Client, r.OperationID)
	q["claimId"], q["revision"], q["ttlMicros"], q["data"] = c.ClaimID, c.Revision, r.TTL.Microseconds(), r.Data
	b, e := a.request(ctx, "/v1/claims/checkpoint", "checkpoint", r.OperationID, q, true, false, c.Token, "", c.HandlePath, "", "", c.CredentialPath)
	var out lease.Receipt
	if e == nil {
		e = finish(a.Client, r.OperationID, c.HandlePath, r.OperationID, c.ClaimID, b, &out)
	}
	return out, e
}
func (a *RemoteAuthority) Release(ctx context.Context, c lease.Credentials, r lease.ReleaseRequest) (lease.Receipt, error) {
	q := common(a.Client, r.OperationID)
	q["claimId"], q["revision"], q["reason"] = c.ClaimID, c.Revision, r.Reason
	b, e := a.request(ctx, "/v1/claims/release", "release", r.OperationID, q, true, false, c.Token, "", c.HandlePath, "", "", c.CredentialPath)
	var out lease.Receipt
	if e == nil {
		e = finish(a.Client, r.OperationID, c.HandlePath, r.OperationID, c.ClaimID, b, &out)
	}
	return out, e
}
func (a *RemoteAuthority) Transfer(ctx context.Context, c lease.Credentials, r lease.TransferRequest) (lease.Grant, error) {
	if c.HandlePath != "" && r.SuccessorHandlePath == "" {
		return lease.Grant{}, reason.Invalid("remote named transfer requires a successor handle path")
	}
	q := common(a.Client, r.OperationID)
	q["claimId"], q["revision"], q["successorClaimId"], q["toAgent"], q["toSession"], q["toWorkKey"], q["ttlMicros"] = c.ClaimID, c.Revision, r.SuccessorClaimID, r.ToAgent, r.ToSession, r.ToWorkKey, r.TTL.Microseconds()
	b, e := a.request(ctx, "/v1/claims/transfer", "transfer", r.OperationID, q, true, false, c.Token, r.SuccessorToken, c.HandlePath, r.SuccessorHandlePath, "", c.CredentialPath, r.SuccessorCredentialPath)
	var out lease.Grant
	if e == nil {
		e = decodeResult(b, &out)
	}
	if e == nil {
		e = validateResult(&out)
	}
	if e == nil && (out.ClaimID != r.SuccessorClaimID || out.Receipt.OperationID != r.OperationID || out.Receipt.ClaimID != c.ClaimID) {
		e = reason.Invalid("remote transfer does not match request")
	}
	if e == nil && c.HandlePath != "" {
		e = activateTransferHandle(c.HandlePath, r.SuccessorHandlePath, out)
	} else if e == nil {
		e = a.Client.finalize(r.OperationID, "")
	}
	return out, e
}
func (a *RemoteAuthority) Verify(ctx context.Context, c lease.Credentials, expected []string) (lease.Verification, error) {
	q := map[string]any{"protocolVersion": protocolVersion, "authorityId": a.Client.profile.AuthorityID, "expectedRestoreId": a.Client.profile.RestoreID}
	q["claimId"], q["revision"], q["expectedResources"] = c.ClaimID, c.Revision, expected
	b, e := a.request(ctx, "/v1/claims/verify", "verify", "00000000000000000000000000000000", q, false, false, c.Token, "")
	var out lease.Verification
	if e == nil {
		e = decodeResult(b, &out)
	}
	return out, e
}
func (a *RemoteAuthority) BeginOperation(ctx context.Context, c lease.Credentials, r lease.OperationIntent) (lease.Started, error) {
	q := common(a.Client, r.OperationID)
	q["claimId"], q["revision"], q["kind"], q["request"], q["requestSha256"], q["ttlMicros"] = c.ClaimID, c.Revision, r.Kind, r.Request, r.RequestHash, r.TTL.Microseconds()
	b, e := a.request(ctx, "/v1/operations/begin", "operations/begin", r.OperationID, q, true, false, c.Token, "", c.HandlePath, "", "", c.CredentialPath)
	var out lease.Started
	if e == nil {
		e = decodeResult(b, &out)
	}
	if e == nil {
		e = validateResult(&out)
	}
	if e == nil && (out.OperationID != r.OperationID || out.ClaimID != c.ClaimID || out.Kind != r.Kind || out.RequestHash != r.RequestHash) {
		e = reason.Invalid("remote operation result does not match request")
	}
	return out, e
}
func (a *RemoteAuthority) RenewOperation(ctx context.Context, c lease.Credentials, id string, ttl time.Duration) (lease.Receipt, error) {
	renewalID, e := newID()
	if e != nil {
		return lease.Receipt{}, e
	}
	q := common(a.Client, id)
	q["claimId"], q["revision"], q["renewalId"], q["ttlMicros"] = c.ClaimID, c.Revision, renewalID, ttl.Microseconds()
	b, e := a.request(ctx, "/v1/operations/renew", "operations/renew", renewalID, q, true, false, c.Token, "", c.HandlePath, "", id, c.CredentialPath)
	var out lease.Receipt
	if e == nil {
		e = finish(a.Client, renewalID, c.HandlePath, renewalID, c.ClaimID, b, &out)
	}
	return out, e
}
func (a *RemoteAuthority) CompleteOperation(ctx context.Context, c lease.Credentials, id string, r map[string]any) (lease.Receipt, error) {
	completionID, e := newID()
	if e != nil {
		return lease.Receipt{}, e
	}
	q := common(a.Client, id)
	q["claimId"], q["revision"], q["receipt"] = c.ClaimID, c.Revision, r
	b, e := a.request(ctx, "/v1/operations/complete", "operations/complete", completionID, q, true, false, c.Token, "", c.HandlePath, "", id, c.CredentialPath)
	var out lease.Receipt
	if e == nil {
		e = finish(a.Client, completionID, c.HandlePath, id, c.ClaimID, b, &out)
	}
	if e == nil {
		e = a.Client.finalize(id, c.HandlePath)
	} // terminal completion reconciles the retained begin effect
	return out, e
}

func (a *RemoteAuthority) Inspect(ctx context.Context, r ledger.InspectRequest) (ledger.Operation, error) {
	q := map[string]any{"protocolVersion": protocolVersion, "authorityId": a.Client.profile.AuthorityID, "expectedRestoreId": a.Client.profile.RestoreID, "operationId": r.OperationID, "claimId": r.ClaimID, "resource": r.Resource, "view": "public"}
	if r.Full {
		q["view"] = "private"
	}
	b, err := a.request(ctx, "/v1/operations/inspect", "operations/inspect", "00000000000000000000000000000000", q, false, false, r.Token, "", r.HandlePath, "", "", r.CredentialPath)
	var out ledger.Operation
	if err == nil {
		err = decodeResult(b, &out)
	}
	if err == nil && (out.OperationID != r.OperationID || (r.ClaimID != "" && out.ClaimID != r.ClaimID)) {
		err = reason.Invalid("remote operation does not match request")
	}
	return out, err
}

func (a *RemoteAuthority) Reconcile(ctx context.Context, c lease.Credentials, r lease.ReconcileRequest) (lease.ReconciliationReceipt, error) {
	q := common(a.Client, r.OperationID)
	q["claimId"], q["revision"], q["targetClaimId"], q["targetOperationId"], q["expectedRequestSha256"], q["outcome"], q["evidence"], q["ttlMicros"] = c.ClaimID, c.Revision, r.TargetClaimID, r.TargetOperationID, r.ExpectedRequestSHA256, r.Outcome, r.Evidence, r.TTL.Microseconds()
	b, err := a.request(ctx, "/v1/operations/reconcile", "operations/reconcile", r.OperationID, q, true, false, c.Token, "", c.HandlePath, "", r.TargetOperationID, c.CredentialPath, "", r.TargetHandlePath)
	var out lease.ReconciliationReceipt
	if err == nil {
		err = decodeResult(b, &out)
	}
	if err == nil && (out.OperationID != r.OperationID || out.ResolverClaimID != c.ClaimID || out.TargetClaimID != r.TargetClaimID || out.TargetOperationID != r.TargetOperationID || !out.Committed) {
		err = reason.Invalid("remote reconciliation does not match request")
	}
	if err == nil {
		err = a.Client.finalize(r.OperationID, c.HandlePath)
	}
	if err == nil {
		handlePath := r.TargetHandlePath
		if handlePath == "" && r.TargetClaimID == c.ClaimID {
			handlePath = c.HandlePath
		}
		err = a.Client.finalize(r.TargetOperationID, handlePath)
	}
	return out, err
}

func (a *RemoteAuthority) Events(ctx context.Context, cursor string, limit int) (ledger.EventsPage, error) {
	q := map[string]any{"protocolVersion": protocolVersion, "authorityId": a.Client.profile.AuthorityID, "expectedRestoreId": a.Client.profile.RestoreID, "cursor": cursor, "limit": limit}
	b, e := a.request(ctx, "/v1/events", "events", "00000000000000000000000000000000", q, false, false, "", "")
	var out ledger.EventsPage
	if e == nil {
		e = decodeResult(b, &out)
	}
	return out, e
}
func (a *RemoteAuthority) History(ctx context.Context, resource, cursor string, limit int, full bool) (ledger.HistoryPage, error) {
	q := map[string]any{"protocolVersion": protocolVersion, "authorityId": a.Client.profile.AuthorityID, "expectedRestoreId": a.Client.profile.RestoreID, "resource": resource, "cursor": cursor, "limit": limit, "full": full}
	b, e := a.request(ctx, "/v1/history", "history", "00000000000000000000000000000000", q, false, false, "", "")
	var out ledger.HistoryPage
	if e == nil {
		e = decodeResult(b, &out)
	}
	return out, e
}
func (a *RemoteAuthority) Watch(ctx context.Context, r watch.Request) (watch.Result, error) {
	q := map[string]any{"protocolVersion": protocolVersion, "authorityId": a.Client.profile.AuthorityID, "expectedRestoreId": a.Client.profile.RestoreID, "cursor": r.Cursor, "resources": r.Resources, "until": r.Until, "timeoutMicros": r.Timeout.Microseconds()}
	b, e := a.request(ctx, "/v1/watch", "watch", "00000000000000000000000000000000", q, false, false, "", "")
	var out watch.Result
	if e == nil {
		e = decodeResult(b, &out)
	}
	return out, e
}

// FakeAuthority is a deterministic contract fake for consumers and tests. It
// records calls but never contacts a server or local SQLite.
type FakeAuthority struct {
	Calls         []string
	AcquireResult lease.Grant
	AcquireError  error
}

func (f *FakeAuthority) record(s string) { f.Calls = append(f.Calls, s) }
func (f *FakeAuthority) Execute(context.Context, RequestSpec) (Response, error) {
	f.record("execute")
	return Response{}, nil
}
func (f *FakeAuthority) Acquire(context.Context, lease.AcquireRequest) (lease.Grant, error) {
	f.record("acquire")
	return f.AcquireResult, f.AcquireError
}
func (f *FakeAuthority) Status(context.Context, lease.Selector) (lease.Status, error) {
	f.record("status")
	return lease.Status{}, nil
}
func (f *FakeAuthority) List(context.Context, string, *lease.RemoteActor) ([]lease.ClaimView, error) {
	f.record("list")
	return nil, nil
}
func (f *FakeAuthority) Heartbeat(context.Context, lease.Credentials, lease.Renew) (lease.Receipt, error) {
	f.record("heartbeat")
	return lease.Receipt{}, nil
}
func (f *FakeAuthority) Checkpoint(context.Context, lease.Credentials, lease.CheckpointRequest) (lease.Receipt, error) {
	f.record("checkpoint")
	return lease.Receipt{}, nil
}
func (f *FakeAuthority) Release(context.Context, lease.Credentials, lease.ReleaseRequest) (lease.Receipt, error) {
	f.record("release")
	return lease.Receipt{}, nil
}
func (f *FakeAuthority) Transfer(context.Context, lease.Credentials, lease.TransferRequest) (lease.Grant, error) {
	f.record("transfer")
	return lease.Grant{}, nil
}
func (f *FakeAuthority) Verify(context.Context, lease.Credentials, []string) (lease.Verification, error) {
	f.record("verify")
	return lease.Verification{}, nil
}
func (f *FakeAuthority) BeginOperation(context.Context, lease.Credentials, lease.OperationIntent) (lease.Started, error) {
	f.record("begin")
	return lease.Started{}, nil
}
func (f *FakeAuthority) RenewOperation(context.Context, lease.Credentials, string, time.Duration) (lease.Receipt, error) {
	f.record("renew-operation")
	return lease.Receipt{}, nil
}
func (f *FakeAuthority) CompleteOperation(context.Context, lease.Credentials, string, map[string]any) (lease.Receipt, error) {
	f.record("complete")
	return lease.Receipt{}, nil
}
func (f *FakeAuthority) Inspect(context.Context, ledger.InspectRequest) (ledger.Operation, error) {
	f.record("inspect")
	return ledger.Operation{}, nil
}
func (f *FakeAuthority) Reconcile(context.Context, lease.Credentials, lease.ReconcileRequest) (lease.ReconciliationReceipt, error) {
	f.record("reconcile")
	return lease.ReconciliationReceipt{}, nil
}
func (f *FakeAuthority) Events(context.Context, string, int) (ledger.EventsPage, error) {
	f.record("events")
	return ledger.EventsPage{}, nil
}
func (f *FakeAuthority) History(context.Context, string, string, int, bool) (ledger.HistoryPage, error) {
	f.record("history")
	return ledger.HistoryPage{}, nil
}
func (f *FakeAuthority) Watch(context.Context, watch.Request) (watch.Result, error) {
	f.record("watch")
	return watch.Result{}, nil
}
