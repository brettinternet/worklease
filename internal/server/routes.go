package server

import (
	"context"
	"encoding/json"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/remoteadmin"
	"github.com/brettinternet/worklease/internal/watch"
	"net/http"
	"time"
)

type enrollWire struct {
	ProtocolVersion   string `json:"protocolVersion"`
	AuthorityID       string `json:"authorityId"`
	ExpectedRestoreID string `json:"expectedRestoreId"`
	RequestID         string `json:"requestId"`
	RequestNotAfter   string `json:"requestNotAfter"`
	InstallationID    string `json:"installationId"`
	Label             string `json:"label"`
}

func (s *Server) enroll(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q enrollWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	if q.ProtocolVersion != ProtocolVersion {
		return nil, reason.New(reason.ReasonProtocolVersionUnsupported, "protocol version is unsupported")
	}
	inv, e := inviteAuth(r)
	if e != nil {
		return nil, e
	}
	cred, e := auth(r, "Worklease-New-Installation-Authorization")
	if e != nil {
		return nil, e
	}
	deadline, e := when(q.RequestNotAfter)
	if e != nil {
		return nil, e
	}
	return s.service.Enroll(ctx, lease.EnrollRequest{AuthorityID: q.AuthorityID, ExpectedRestoreID: q.ExpectedRestoreID, RequestID: q.RequestID, RequestNotAfter: deadline, InstallationID: q.InstallationID, Invite: inv, Credential: cred, Label: q.Label})
}

type acquireWire struct {
	common
	ClaimID          string   `json:"claimId"`
	Resources        []string `json:"resources"`
	AgentID          string   `json:"agentId"`
	SessionID        string   `json:"sessionId"`
	WorkKey          string   `json:"workKey"`
	TTLMicros        int64    `json:"ttlMicros"`
	MaxHoldMicros    int64    `json:"maxHoldMicros"`
	CoordinationOnly bool     `json:"coordinationOnly"`
}

func (s *Server) acquire(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q acquireWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	a, e := s.authenticated(r, b, "write")
	if e != nil {
		return nil, e
	}
	token, e := auth(r, "Worklease-New-Claim-Authorization")
	if e != nil {
		return nil, e
	}
	deadline, e := validCommon(q.common)
	if e != nil {
		return nil, e
	}
	return s.service.Acquire(ctx, lease.AcquireRequest{AuthorityID: q.AuthorityID, ClaimID: q.ClaimID, Token: token, Resources: q.Resources, AgentID: q.AgentID, SessionID: q.SessionID, WorkKey: q.WorkKey, TTL: micros(q.TTLMicros), MaxHold: micros(q.MaxHoldMicros), RequestNotAfter: deadline, CoordinationOnly: q.CoordinationOnly, Actor: &a})
}

type authWire struct {
	ProtocolVersion   string `json:"protocolVersion"`
	AuthorityID       string `json:"authorityId"`
	ExpectedRestoreID string `json:"expectedRestoreId"`
}

type selectorWire struct {
	authWire
	ClaimID   string   `json:"claimId"`
	Resources []string `json:"resources"`
}

func (s *Server) status(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q selectorWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	if (q.ClaimID == "") == (len(q.Resources) == 0) {
		return nil, reason.New(reason.ReasonClaimSelectionMissing, "status requires exactly one claim or resource selection")
	}
	a, e := s.authenticated(r, b, "read")
	if e != nil {
		return nil, e
	}
	out, e := s.service.Status(ctx, lease.Selector{AuthorityID: q.AuthorityID, ClaimID: q.ClaimID, Resources: q.Resources, Actor: &a})
	if e != nil {
		return nil, e
	}
	return map[string]any{"claim": out.Claim, "claims": out.Claims, "resources": out.Resources}, nil
}

type listWire struct {
	authWire
	Resource string `json:"resource"`
}

func (s *Server) list(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q listWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	a, e := s.authenticated(r, b, "read")
	if e != nil {
		return nil, e
	}
	v, e := s.service.List(ctx, q.Resource, &a)
	return map[string]any{"claims": v}, e
}

type claimWire struct {
	common
	ClaimID  string `json:"claimId"`
	Revision int64  `json:"revision"`
}

func (s *Server) creds(r *http.Request, q claimWire) (lease.Credentials, error) {
	a, _, e := s.actor(r)
	if e != nil {
		return lease.Credentials{}, e
	}
	t, e := auth(r, "Worklease-Claim-Authorization")
	if e != nil {
		return lease.Credentials{}, e
	}
	a.AuthorityID = q.AuthorityID
	a.ExpectedRestoreID = q.ExpectedRestoreID
	return lease.Credentials{AuthorityID: q.AuthorityID, ClaimID: q.ClaimID, Revision: q.Revision, Token: t, Actor: &a}, nil
}
func (s *Server) heartbeat(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q struct {
		claimWire
		TTLMicros int64 `json:"ttlMicros"`
	}
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	d, e := validCommon(q.common)
	if e != nil {
		return nil, e
	}
	c, e := s.creds(r, q.claimWire)
	if e != nil {
		return nil, e
	}
	return s.service.Heartbeat(ctx, c, lease.Renew{OperationID: q.OperationID, TTL: micros(q.TTLMicros), RequestNotAfter: d})
}
func (s *Server) checkpoint(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q struct {
		claimWire
		TTLMicros int64           `json:"ttlMicros"`
		Data      json.RawMessage `json:"data"`
	}
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	d, e := validCommon(q.common)
	if e != nil {
		return nil, e
	}
	c, e := s.creds(r, q.claimWire)
	if e != nil {
		return nil, e
	}
	return s.service.Checkpoint(ctx, c, lease.CheckpointRequest{OperationID: q.OperationID, TTL: micros(q.TTLMicros), Data: q.Data, RequestNotAfter: d})
}
func (s *Server) release(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q struct {
		claimWire
		Reason string `json:"reason"`
	}
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	d, e := validCommon(q.common)
	if e != nil {
		return nil, e
	}
	c, e := s.creds(r, q.claimWire)
	if e != nil {
		return nil, e
	}
	return s.service.Release(ctx, c, lease.ReleaseRequest{OperationID: q.OperationID, Reason: q.Reason, RequestNotAfter: d})
}

type transferWire struct {
	claimWire
	SuccessorClaimID string `json:"successorClaimId"`
	ToAgent          string `json:"toAgent"`
	ToSession        string `json:"toSession"`
	ToWorkKey        string `json:"toWorkKey"`
	TTLMicros        int64  `json:"ttlMicros"`
}

func (s *Server) transfer(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q transferWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	d, e := validCommon(q.common)
	if e != nil {
		return nil, e
	}
	c, e := s.creds(r, q.claimWire)
	if e != nil {
		return nil, e
	}
	tok, e := auth(r, "Worklease-New-Claim-Authorization")
	if e != nil {
		return nil, e
	}
	return s.service.Transfer(ctx, c, lease.TransferRequest{OperationID: q.OperationID, SuccessorClaimID: q.SuccessorClaimID, SuccessorToken: tok, ToAgent: q.ToAgent, ToSession: q.ToSession, ToWorkKey: q.ToWorkKey, TTL: micros(q.TTLMicros), RequestNotAfter: d})
}
func (s *Server) verify(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q struct {
		claimWire
		ExpectedResources []string `json:"expectedResources"`
	}
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	if _, e := s.authenticated(r, b, "read"); e != nil {
		return nil, e
	}
	c, e := s.creds(r, q.claimWire)
	if e != nil {
		return nil, e
	}
	return s.service.Verify(ctx, c, q.ExpectedResources)
}

type beginWire struct {
	claimWire
	Kind          string         `json:"kind"`
	Request       map[string]any `json:"request"`
	RequestSHA256 string         `json:"requestSha256"`
	TTLMicros     int64          `json:"ttlMicros"`
}

func (s *Server) begin(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q beginWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	d, e := validCommon(q.common)
	if e != nil {
		return nil, e
	}
	c, e := s.creds(r, q.claimWire)
	if e != nil {
		return nil, e
	}
	if q.Kind != "exec" {
		return nil, reason.New(reason.ReasonOperationKindUnsupported, "only exec is supported remotely")
	}
	if encoded, marshalErr := json.Marshal(q.Request); marshalErr != nil || len(encoded) > 48<<10 {
		return nil, reason.New(reason.ReasonRequestTooLarge, "operation request exceeds 48 KiB")
	}
	return s.service.BeginOperation(ctx, c, lease.OperationIntent{OperationID: q.OperationID, Kind: q.Kind, Request: q.Request, RequestHash: q.RequestSHA256, TTL: micros(q.TTLMicros), RequestNotAfter: d})
}

type opRenewWire struct {
	ProtocolVersion   string `json:"protocolVersion"`
	AuthorityID       string `json:"authorityId"`
	ExpectedRestoreID string `json:"expectedRestoreId"`
	ClaimID           string `json:"claimId"`
	Revision          int64  `json:"revision"`
	RenewalID         string `json:"renewalId"`
	OperationID       string `json:"operationId"`
	TTLMicros         int64  `json:"ttlMicros"`
	RequestNotAfter   string `json:"requestNotAfter"`
}

func (s *Server) operationRenew(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q opRenewWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	d, e := validCommon(common{ProtocolVersion: q.ProtocolVersion, AuthorityID: q.AuthorityID, ExpectedRestoreID: q.ExpectedRestoreID, OperationID: q.RenewalID, RequestNotAfter: q.RequestNotAfter})
	if e != nil {
		return nil, e
	}
	c, e := s.creds(r, claimWire{common: common{ProtocolVersion: q.ProtocolVersion, AuthorityID: q.AuthorityID, ExpectedRestoreID: q.ExpectedRestoreID}, ClaimID: q.ClaimID, Revision: q.Revision})
	if e != nil {
		return nil, e
	}
	return s.service.RemoteRenewOperation(ctx, c, lease.RemoteOperationRenewRequest{RenewalID: q.RenewalID, OperationID: q.OperationID, TTL: micros(q.TTLMicros), RequestNotAfter: d})
}

type completeWire struct {
	ProtocolVersion   string         `json:"protocolVersion"`
	AuthorityID       string         `json:"authorityId"`
	ExpectedRestoreID string         `json:"expectedRestoreId"`
	ClaimID           string         `json:"claimId"`
	Revision          int64          `json:"revision"`
	OperationID       string         `json:"operationId"`
	RequestNotAfter   string         `json:"requestNotAfter"`
	Receipt           map[string]any `json:"receipt"`
}

func (s *Server) complete(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q completeWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	if _, e := validCommon(common{ProtocolVersion: q.ProtocolVersion, AuthorityID: q.AuthorityID, ExpectedRestoreID: q.ExpectedRestoreID, OperationID: q.OperationID, RequestNotAfter: q.RequestNotAfter}); e != nil {
		return nil, e
	}
	c, e := s.creds(r, claimWire{common: common{ProtocolVersion: q.ProtocolVersion, AuthorityID: q.AuthorityID, ExpectedRestoreID: q.ExpectedRestoreID}, ClaimID: q.ClaimID, Revision: q.Revision})
	if e != nil {
		return nil, e
	}
	if encoded, marshalErr := json.Marshal(q.Receipt); marshalErr != nil || len(encoded) > 512<<10 {
		return nil, reason.New(reason.ReasonRequestTooLarge, "completion receipt exceeds 512 KiB")
	}
	return s.service.CompleteOperation(ctx, c, q.OperationID, q.Receipt)
}

type inspectWire struct {
	authWire
	OperationID string `json:"operationId"`
	ClaimID     string `json:"claimId"`
	Resource    string `json:"resource"`
	View        string `json:"view"`
}

func (s *Server) inspect(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q inspectWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	a, e := s.authenticated(r, b, "read")
	if e != nil {
		return nil, e
	}
	if e := s.service.AuthorizeRemote(ctx, a, "read"); e != nil {
		return nil, e
	}
	if q.View != "public" && q.View != "private" {
		return nil, reason.Invalid("view must be public or private")
	}
	tok, _ := auth(r, "Worklease-Claim-Authorization")
	trustedFull := false
	if q.View == "private" && tok == "" {
		if e := s.service.AuthorizeRemote(ctx, a, "admin"); e != nil {
			return nil, reason.New(reason.ReasonAuthenticationRequired, "claim authentication is required")
		}
		trustedFull = true
	}
	return ledger.NewChecked(s.store, s.service.RemoteTransactionCheck(a, "read")).Inspect(ctx, ledger.InspectRequest{OperationID: q.OperationID, ClaimID: q.ClaimID, Resource: q.Resource, Token: tok, Full: q.View == "private", TrustedFull: trustedFull})
}

type eventsWire struct {
	authWire
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}

type historyWire struct {
	authWire
	Resource string `json:"resource"`
	Cursor   string `json:"cursor"`
	Limit    int    `json:"limit"`
	Full     bool   `json:"full"`
}

func (s *Server) events(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q eventsWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	a, e := s.authenticated(r, b, "read")
	if e != nil {
		return nil, e
	}
	if e := s.service.AuthorizeRemote(ctx, a, "read"); e != nil {
		return nil, e
	}
	return ledger.NewChecked(s.store, s.service.RemoteTransactionCheck(a, "read")).Events(ctx, q.Cursor, q.Limit)
}
func (s *Server) history(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q historyWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	a, e := s.authenticated(r, b, "read")
	if e != nil {
		return nil, e
	}
	if e := s.service.AuthorizeRemote(ctx, a, "read"); e != nil {
		return nil, e
	}
	return ledger.NewChecked(s.store, s.service.RemoteTransactionCheck(a, "read")).History(ctx, q.Resource, q.Cursor, q.Limit, false)
}

type watchWire struct {
	authWire
	Cursor        string   `json:"cursor"`
	Resources     []string `json:"resources"`
	Until         string   `json:"until"`
	TimeoutMicros int64    `json:"timeoutMicros"`
}

func (s *Server) watch(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q watchWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	a, e := s.authenticated(r, b, "read")
	if e != nil {
		return nil, e
	}
	if e := s.service.AuthorizeRemote(ctx, a, "read"); e != nil {
		return nil, e
	}
	if q.TimeoutMicros < 0 || q.TimeoutMicros > 30_000_000 {
		return nil, reason.Invalid("watch timeout must be between 0 and 30 seconds")
	}
	return watch.Wait(ctx, s.store, watch.Request{Cursor: q.Cursor, Resources: q.Resources, Until: q.Until, Timeout: micros(q.TimeoutMicros), PollInterval: watch.DefaultPoll, Check: s.service.RemoteTransactionCheck(a, "read")})
}

type gcWire struct {
	common
	Cutoff        string  `json:"cutoff"`
	RetentionDays float64 `json:"retentionDays"`
	Apply         bool    `json:"apply"`
}

func (s *Server) gc(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q gcWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	d, e := validCommon(q.common)
	if e != nil {
		return nil, e
	}
	a, e := s.authenticated(r, b, "admin")
	if e != nil {
		return nil, e
	}
	if !q.Apply {
		return nil, reason.Invalid("remote GC requires apply=true")
	}
	var cutoff time.Time
	if q.Cutoff != "" {
		cutoff, e = time.Parse(time.RFC3339Nano, q.Cutoff)
		if e != nil {
			return nil, reason.Invalid("cutoff is invalid")
		}
	}
	return remoteadmin.GC(ctx, s.store, s.service, a, remoteadmin.GCRequest{OperationID: q.OperationID, RequestNotAfter: d, Cutoff: cutoff, RetentionDays: q.RetentionDays})
}
