package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/brettinternet/worklease/internal/lease"
)

type reconcileWire struct {
	claimWire
	TargetClaimID         string          `json:"targetClaimId"`
	TargetOperationID     string          `json:"targetOperationId"`
	ExpectedRequestSHA256 string          `json:"expectedRequestSha256"`
	Outcome               string          `json:"outcome"`
	Evidence              json.RawMessage `json:"evidence"`
	TTLMicros             int64           `json:"ttlMicros"`
}

func (s *Server) reconcile(ctx context.Context, r *http.Request, b []byte) (any, error) {
	var q reconcileWire
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
	return s.service.Reconcile(ctx, c, lease.ReconcileRequest{OperationID: q.OperationID, TargetClaimID: q.TargetClaimID, TargetOperationID: q.TargetOperationID, ExpectedRequestSHA256: q.ExpectedRequestSHA256, Outcome: q.Outcome, Evidence: q.Evidence, TTL: micros(q.TTLMicros), RequestNotAfter: d})
}
