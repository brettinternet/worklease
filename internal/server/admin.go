package server

import (
	"context"
	"net/http"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
)

type inviteWire struct {
	common
	InviteID     string `json:"inviteId"`
	Role         string `json:"role"`
	Label        string `json:"label"`
	ExpiresAt    string `json:"expiresAt"`
	InviteSHA256 string `json:"inviteSha256"`
}

func (s *Server) issueInvite(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q inviteWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	a, e := s.authenticated(r, b, "admin")
	if e != nil {
		return nil, e
	}
	d, e := validCommon(q.common)
	if e != nil {
		return nil, e
	}
	var expires time.Time
	if q.ExpiresAt != "" {
		expires, e = time.Parse(time.RFC3339Nano, q.ExpiresAt)
		if e != nil {
			return nil, reason.Invalid("expiresAt is invalid")
		}
	}
	return s.service.IssueInvite(ctx, a, lease.IssueInviteRequest{OperationID: q.OperationID, RequestNotAfter: d, InviteID: q.InviteID, Role: q.Role, Label: q.Label, ExpiresAt: expires, InviteSha256: q.InviteSHA256})
}

type installsWire struct {
	authWire
	IncludeRevoked bool `json:"includeRevoked"`
}

func (s *Server) installations(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q installsWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	a, e := s.authenticated(r, b, "admin")
	if e != nil {
		return nil, e
	}
	v, e := s.service.ListInstallations(ctx, a, q.IncludeRevoked)
	return map[string]any{"installations": v}, e
}

type revokeInstallWire struct {
	common
	InstallationID string `json:"installationId"`
	Reason         string `json:"reason"`
}

func (s *Server) revokeInstallation(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q revokeInstallWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	a, e := s.authenticated(r, b, "admin")
	if e != nil {
		return nil, e
	}
	d, e := validCommon(q.common)
	if e != nil {
		return nil, e
	}
	return s.service.RevokeInstallation(ctx, a, lease.RevokeInstallationRequest{InstallationID: q.InstallationID, Reason: q.Reason, OperationID: q.OperationID, RequestNotAfter: d})
}

type revokeClaimWire struct {
	common
	ClaimID string `json:"claimId"`
	Reason  string `json:"reason"`
}

func (s *Server) revokeClaim(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q revokeClaimWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	a, e := s.authenticated(r, b, "admin")
	if e != nil {
		return nil, e
	}
	d, e := validCommon(q.common)
	if e != nil {
		return nil, e
	}
	return s.service.RevokeClaim(ctx, a, lease.RevokeClaimRequest{ClaimID: q.ClaimID, Reason: q.Reason, OperationID: q.OperationID, RequestNotAfter: d})
}
func (s *Server) recoveryStatus(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q authWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	a, e := s.authenticated(r, b, "admin")
	if e != nil {
		return nil, e
	}
	return s.service.RemoteRecoveryStatus(ctx, a)
}

type reopenWire struct {
	common
	ExpectedRecoveryRevision int64                   `json:"expectedRecoveryRevision"`
	Attestation              lease.ReopenAttestation `json:"attestation"`
}

func (s *Server) reopen(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q reopenWire
	if e := decode(b, &q); e != nil {
		return nil, e
	}
	a, e := s.authenticated(r, b, "admin")
	if e != nil {
		return nil, e
	}
	d, e := validCommon(q.common)
	if e != nil {
		return nil, e
	}
	return s.service.ReopenRecovery(ctx, a, lease.ReopenRequest{OperationID: q.OperationID, RequestNotAfter: d, ExpectedRecoveryRevision: q.ExpectedRecoveryRevision, Attestation: q.Attestation})
}
