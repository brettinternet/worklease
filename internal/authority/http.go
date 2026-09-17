package authority

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
)

const protocolVersion = "worklease-http/1"

type RequestSpec struct {
	Path                   string
	Body                   []byte
	Kind                   string
	RequestID              string
	Mutating               bool
	Terminal               bool
	Invite                 string
	NewCredential          string
	NewClaimCredential     string
	ClaimCredential        string
	ParentRequestID        string
	EffectEvidence         json.RawMessage
	HandlePath             string
	ClaimID                string
	CredentialPath         string
	ClaimHandlePath        string
	ClaimCredentialPath    string
	NewClaimHandlePath     string
	NewClaimCredentialPath string
	TargetOperationID      string
	TargetHandlePath       string
	AutoRenewOwner         string
	PreviousClaimID        string
	PreviousToken          string
	PreviousRevision       int64
	PreviousExpiresAt      time.Time
}
type Response struct {
	AuthorityID   string
	RestoreID     string
	AuthorityTime time.Time
	Result        json.RawMessage
	Metadata      *MetadataResult
}

type MetadataResult struct {
	AuthorityID               string    `json:"authorityId"`
	RestoreID                 string    `json:"restoreId"`
	SupportedProtocolVersions []string  `json:"supportedProtocolVersions"`
	AuthorityTime             time.Time `json:"authorityTime"`
	AdmittedPrefixes          *[]string `json:"admittedPrefixes,omitempty"`
}

type InstallationSelfResult struct {
	Role string `json:"role"`
}
type envelope struct {
	OK              bool            `json:"ok"`
	ProtocolVersion string          `json:"protocolVersion"`
	AuthorityID     string          `json:"authorityId"`
	RestoreID       string          `json:"restoreId"`
	AuthorityTime   time.Time       `json:"authorityTime"`
	Result          json.RawMessage `json:"result"`
	Error           *wireError      `json:"error"`
}
type wireError struct {
	Reason  string         `json:"reason"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

type HTTPClient struct {
	profile config.Profile
	http    *http.Client
	pending PendingStore
	clock   *AuthorityClock
}

func NewHTTPClient(profile config.Profile, pending PendingStore, transport http.RoundTripper) (*HTTPClient, error) {
	if err := validateClientProfile(profile); err != nil {
		return nil, err
	}
	if pending == nil {
		return nil, reason.New(reason.ReasonStorageFailure, "explicit pending recovery store is required")
	}
	if files, ok := pending.(*FilePendingStore); ok {
		if err := files.validateDir(); err != nil {
			return nil, reason.New(reason.ReasonStorageFailure, err.Error())
		}
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	transport, err := withCertificatePin(profile, transport)
	if err != nil {
		return nil, err
	}
	return &HTTPClient{profile: profile, http: &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}, pending: pending, clock: NewAuthorityClock(nil)}, nil
}
func (c *HTTPClient) Profile() config.Profile { return c.profile }
func (c *HTTPClient) Clock() *AuthorityClock  { return c.clock }
func (c *HTTPClient) Metadata(ctx context.Context) (Response, error) {
	resp, err := c.do(ctx, "GET", "/.well-known/worklease", nil, RequestSpec{})
	if err != nil {
		return Response{}, err
	}
	if err := c.validate(resp, true); err != nil {
		return Response{}, err
	}
	var metadata MetadataResult
	if err := decodeStrict(resp.Result, &metadata); err != nil || metadata.AuthorityID != "" && metadata.AuthorityID != resp.AuthorityID || metadata.RestoreID != "" && metadata.RestoreID != resp.RestoreID {
		return Response{}, reason.Invalid("remote metadata result is invalid")
	}
	metadata.AuthorityID, metadata.RestoreID, metadata.AuthorityTime = resp.AuthorityID, resp.RestoreID, resp.AuthorityTime
	if len(metadata.SupportedProtocolVersions) == 0 {
		metadata.SupportedProtocolVersions = []string{protocolVersion}
	}
	resp.Metadata = &metadata
	return resp, nil
}

func (c *HTTPClient) InstallationSelf(ctx context.Context) (InstallationSelfResult, error) {
	body, _ := json.Marshal(map[string]any{"protocolVersion": protocolVersion, "authorityId": c.profile.AuthorityID, "expectedRestoreId": c.profile.RestoreID})
	resp, err := c.Call(ctx, RequestSpec{Path: "/v1/installations/self", Body: body, Kind: "installation-self"})
	if err != nil {
		return InstallationSelfResult{}, err
	}
	var result InstallationSelfResult
	if err := decodeStrict(resp.Result, &result); err != nil || (result.Role != "read" && result.Role != "write" && result.Role != "admin") {
		return InstallationSelfResult{}, reason.Invalid("remote installation self result is invalid")
	}
	return result, nil
}
func requestDeadline(body []byte) (time.Time, error) {
	if !utf8.Valid(body) || duplicateJSON(body) != nil {
		return time.Time{}, reason.Invalid("mutation request is invalid")
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(body, &value); err != nil {
		return time.Time{}, reason.Invalid("mutation request is invalid")
	}
	var deadline time.Time
	if err := json.Unmarshal(value["requestNotAfter"], &deadline); err != nil || deadline.IsZero() {
		return time.Time{}, reason.Invalid("mutation requestNotAfter is required")
	}
	return deadline, nil
}

func remotePath(path string) (mutating bool, known bool) {
	switch path {
	case "/.well-known/worklease", "/healthz", "/v1/installations/self", "/v1/claims/status", "/v1/claims/list", "/v1/claims/verify", "/v1/operations/inspect", "/v1/events", "/v1/history", "/v1/watch", "/v1/admin/installations/list", "/v1/admin/recovery/status":
		return false, true
	case "/v1/enroll", "/v1/claims/acquire", "/v1/claims/heartbeat", "/v1/claims/checkpoint", "/v1/claims/release", "/v1/claims/transfer", "/v1/operations/begin", "/v1/operations/renew", "/v1/operations/complete", "/v1/operations/reconcile", "/v1/admin/gc", "/v1/admin/invites/issue", "/v1/admin/installations/revoke", "/v1/admin/claims/revoke", "/v1/admin/recovery/reopen":
		return true, true
	default:
		return false, false
	}
}

func (c *HTTPClient) requestPreviouslyStaged(p PendingRequest, handlePath string) bool {
	if handlePath != "" {
		if h, err := handle.Read(handlePath); err == nil && h.PendingRequest != nil && h.PendingRequest.OperationID == p.OperationID && h.PendingRequest.RequestHash == p.RequestSHA256 && bytes.Equal(h.PendingRequest.Request, p.Request) {
			return true
		}
	}
	old, err := c.pending.Load(p.RequestID)
	return err == nil && old.RequestSHA256 == p.RequestSHA256 && bytes.Equal(old.Request, p.Request)
}

// stagingFailure classifies a durable pre-dispatch staging failure. A cause
// describing the request itself is preserved so the operator can distinguish a
// changed request from a persistence fault; every storage-layer cause, however
// the filesystem classified it, stays storage-failure.
func (c *HTTPClient) stagingFailure(err error, previouslyStaged bool, fallback string) error {
	var classified *reason.Error
	switch {
	case errors.Is(err, errPendingRequestMismatch), errors.Is(err, errHandlePending):
		classified = reason.New(reason.ReasonOperationRequestMismatch, "pending request differs")
	default:
		classified = reason.As(err)
		if classified == nil || !stagingRequestCause(classified.Reason) {
			classified = reason.New(reason.ReasonStorageFailure, fallback)
		}
	}
	state := "not-committed"
	if previouslyStaged {
		state = "unknown"
	}
	classified.With("commitState", state)
	return classified
}

// stagingRequestCause names the classified causes that describe the request or
// its ownership rather than the durable store's health.
func stagingRequestCause(name string) bool {
	switch name {
	case reason.ReasonOperationRequestMismatch, reason.ReasonHandleInUse, reason.ReasonRecoveryRequired,
		reason.ReasonAuthorityMismatch, reason.ReasonAuthorityRestored, reason.ReasonReplayExpired:
		return true
	}
	return false
}

func pendingRecoveryFailure(path string, pending *handle.PendingRequest) error {
	return reason.New(reason.ReasonRecoveryRequired, "pending acquire requires exact recovery before this lifecycle action").
		With("operationId", pending.OperationID).
		With("pendingPath", path).
		With("commitState", "not-committed").
		With("recoveryHint", fmt.Sprintf("rerun worklease acquire --handle <pendingPath> to replay operation %s exactly; substitute the separately reported pendingPath and do not change inputs", pending.OperationID))
}

func (c *HTTPClient) Call(ctx context.Context, s RequestSpec) (Response, error) {
	mutation, known := remotePath(s.Path)
	if !known || mutation != s.Mutating || strings.Contains(s.Path, "?") || strings.Contains(s.Path, "#") {
		return Response{}, reason.Invalid("remote request path or mutation class is invalid")
	}
	public := s.Path == "/.well-known/worklease" || s.Path == "/healthz"
	previouslyStaged := false
	if !public && (c.profile.AuthorityID == "" || c.profile.RestoreID == "") {
		return Response{}, reason.New(reason.ReasonAuthorityMismatch, "remote profile identity is not pinned")
	}
	if s.Mutating {
		if c.clock.ResampleNeeded() {
			return Response{}, reason.New(reason.ReasonClockRegression, "authority time must be sampled before mutation")
		}
		if c.profile.AuthorityID == "" || c.profile.RestoreID == "" {
			return Response{}, reason.New(reason.ReasonAuthorityMismatch, "remote profile identity is not pinned")
		}
		deadline, deadlineErr := requestDeadline(s.Body)
		if deadlineErr != nil {
			return Response{}, deadlineErr
		}
		lower, lowerErr := c.clock.LowerBound()
		if lowerErr != nil || !deadline.After(lower) {
			return Response{}, reason.New(reason.ReasonReplayExpired, "remote request deadline has expired")
		}
		if s.ClaimCredential != "" && s.ClaimHandlePath == "" && s.ClaimCredentialPath == "" {
			return Response{}, reason.New(reason.ReasonCredentialUnsafe, "durable claim credential source is required")
		}
		if s.NewClaimCredential != "" && s.NewClaimHandlePath == "" && s.NewClaimCredentialPath == "" {
			return Response{}, reason.New(reason.ReasonCredentialUnsafe, "durable new-claim credential source is required")
		}
		if !validID(s.RequestID) {
			return Response{}, reason.Invalid("request ID must be 32 lowercase hex characters")
		}
		if len(s.Body) == 0 {
			return Response{}, reason.Invalid("mutation body is required")
		}
		sum := sha256.Sum256(s.Body)
		credentialRef := s.CredentialPath
		if credentialRef == "" {
			credentialRef = c.profile.Credential.Path
		}
		p := PendingRequest{RequestID: s.RequestID, OperationID: s.RequestID, Kind: s.Kind, Route: s.Path, TargetOperationID: s.TargetOperationID, CredentialRef: credentialRef, ClaimHandleRef: s.ClaimHandlePath, ClaimCredentialRef: s.ClaimCredentialPath, NewClaimHandleRef: s.NewClaimHandlePath, NewClaimCredentialRef: s.NewClaimCredentialPath, TargetHandleRef: s.TargetHandlePath, AuthorityID: c.profile.AuthorityID, Endpoint: c.profile.Endpoint, CertificateSHA256: c.profile.CertificateSHA256, ExpectedRestoreID: c.profile.RestoreID, RequestNotAfter: deadline, Request: s.Body, RequestSHA256: hex.EncodeToString(sum[:]), ParentRequestID: s.ParentRequestID, EffectEvidence: s.EffectEvidence, State: "pending"}
		previouslyStaged = c.requestPreviouslyStaged(p, s.HandlePath)
		if s.HandlePath != "" {
			replacement := handleReplacement{ClaimID: s.PreviousClaimID, Token: s.PreviousToken, Revision: s.PreviousRevision, ExpiresAt: s.PreviousExpiresAt}
			if err := persistHandleRequest(s.HandlePath, s.ClaimID, p, s.NewClaimCredential, replacement); err != nil {
				if !errors.Is(err, errHandlePending) || s.Kind == "operations/begin" {
					return Response{}, c.stagingFailure(err, previouslyStaged, "handle request could not be durably recorded")
				}
				if pending, readErr := handle.Read(s.HandlePath); readErr == nil && pending.PendingRequest != nil && pending.PendingRequest.Kind == "acquire" && s.Kind != "acquire" {
					return Response{}, pendingRecoveryFailure(s.HandlePath, pending.PendingRequest)
				}
				if err := c.pending.Save(p); err != nil {
					return Response{}, c.stagingFailure(err, previouslyStaged, "secondary remote request could not be durably recorded")
				}
			}
			if s.Kind == "acquire" && s.AutoRenewOwner != "" {
				if err := setHandleAutoRenewOwner(s.HandlePath, s.AutoRenewOwner); err != nil {
					return Response{}, c.stagingFailure(err, previouslyStaged, "automatic renewal state could not be durably recorded")
				}
			}
		} else if err := c.pending.Save(p); err != nil {
			return Response{}, c.stagingFailure(err, previouslyStaged, "remote request could not be durably recorded")
		}
	}
	method := "POST"
	if s.Body == nil {
		method = "GET"
	}
	resp, err := c.do(ctx, method, s.Path, s.Body, s)
	// Validate identity even when the application envelope reports an error;
	// an error from another authority must never reconcile this request. The
	// request already reached a server, so a mismatched response identity is
	// never evidence that this mutation did not commit.
	if resp.AuthorityID != "" {
		if identityErr := c.validate(resp, false); identityErr != nil {
			if s.Mutating {
				if classified := reason.As(identityErr); classified != nil {
					classified.With("commitState", "unknown")
				}
			}
			return Response{}, identityErr
		}
	}
	if err != nil {
		if s.Mutating && (resp.AuthorityID == "" || !reason.DefinitiveNoCommit(err)) {
			return Response{}, reason.New(reason.ReasonUnknownOutcome, "remote request outcome is uncertain")
		}
		if s.Mutating {
			classified := reason.As(err)
			if previouslyStaged {
				if classified != nil {
					classified.With("commitState", "unknown")
				}
				return resp, err
			}
			if clearErr := c.finalize(s.RequestID, s.HandlePath); clearErr != nil {
				if classified != nil {
					classified.With("commitState", "not-committed").With("recoveryHint", "retry the same lifecycle command with the original inputs to clear the retained definitively rejected request")
				}
				return resp, err
			}
			if classified != nil {
				classified.With("commitState", "not-committed")
			}
		}
		return resp, err
	}
	return resp, nil
}

// finalize clears a request only after its typed result has been validated by
// the typed consumer and, where applicable, profile activation has succeeded.
func (c *HTTPClient) finalize(id string, handlePath string) error {
	if handlePath != "" {
		if err := clearHandleRequest(handlePath, id); err == nil {
			return nil
		} else if !errors.Is(err, errHandlePending) {
			return err
		}
	}
	return c.pending.Clear(id)
}

func routeForHandleKind(kind string) (string, string, bool) {
	switch kind {
	case "acquire", "heartbeat", "checkpoint", "release", "transfer":
		return "/v1/claims/" + kind, kind, true
	case "exec":
		return "/v1/operations/begin", "operations/begin", true
	default:
		return "", "", false
	}
}

// ReplayHandle retries the exact request retained in a named remote handle.
func (c *HTTPClient) pendingTrustMatches(p PendingRequest) bool {
	return p.Endpoint != "" && p.Endpoint == c.profile.Endpoint && p.CertificateSHA256 == c.profile.CertificateSHA256 && p.AuthorityID == c.profile.AuthorityID && p.ExpectedRestoreID == c.profile.RestoreID
}

func (c *HTTPClient) ReplayHandle(ctx context.Context, path string) (Response, error) {
	h, err := handle.Read(path)
	if err != nil {
		return Response{}, err
	}
	if h.PendingRequest == nil {
		return Response{}, reason.Invalid("handle has no pending request")
	}
	trust := PendingRequest{Endpoint: h.PendingRequest.Endpoint, CertificateSHA256: h.PendingRequest.CertificateSHA256, AuthorityID: h.PendingRequest.AuthorityID, ExpectedRestoreID: h.PendingRequest.ExpectedRestoreID}
	if !c.pendingTrustMatches(trust) {
		return Response{}, reason.New(reason.ReasonAuthorityMismatch, "pending request trust does not match profile")
	}
	route, kind, ok := routeForHandleKind(h.PendingRequest.Kind)
	if !ok {
		return Response{}, reason.Invalid("handle pending request kind is unsupported remotely")
	}
	if c.clock.ResampleNeeded() {
		if _, err := c.Metadata(ctx); err != nil {
			return Response{}, err
		}
	}
	spec := RequestSpec{Path: route, Body: h.PendingRequest.Request, Kind: kind, RequestID: h.PendingRequest.OperationID, Mutating: true, HandlePath: path, ClaimID: h.ClaimID, ClaimHandlePath: path}
	if kind == "acquire" {
		spec.NewClaimHandlePath = path
	} else {
		spec.ClaimHandlePath = path
		if kind == "transfer" {
			spec.NewClaimCredential = h.PendingRequest.SuccessorToken
			spec.NewClaimHandlePath, _ = h.PendingRequest.Inputs["newClaimHandleRef"].(string)
			if spec.NewClaimHandlePath == "" {
				return Response{}, reason.Invalid("pending transfer successor handle is missing")
			}
		}
	}
	response, err := c.Call(ctx, spec)
	if err != nil || kind == "operations/begin" {
		return response, err
	}
	pending := PendingRequest{RequestID: spec.RequestID, Kind: kind, Route: route, Request: spec.Body}
	if err := validateReplayResult(pending, response.Result); err != nil {
		return Response{}, reason.New(reason.ReasonUnknownOutcome, "remote mutation result could not be validated")
	}
	if kind == "acquire" || kind == "transfer" {
		var grant lease.Grant
		if err := decodeResult(response.Result, &grant); err != nil {
			return Response{}, reason.New(reason.ReasonUnknownOutcome, "remote mutation result could not be validated")
		}
		if kind == "acquire" {
			err = activateGrantHandle(path, grant)
		} else {
			err = activateTransferHandle(path, spec.NewClaimHandlePath, grant)
		}
		if err != nil {
			return Response{}, reason.New(reason.ReasonUnknownOutcome, "remote mutation result could not be validated")
		}
	} else if err := c.finalize(spec.RequestID, path); err != nil {
		return Response{}, err
	}
	return response, nil
}

func (c *HTTPClient) Replay(ctx context.Context, id string) (Response, error) {
	p, err := c.pending.Load(id)
	if err != nil {
		return Response{}, err
	}
	if !c.pendingTrustMatches(p) {
		return Response{}, reason.New(reason.ReasonAuthorityMismatch, "pending request trust does not match profile")
	}
	if p.Kind == "enroll" {
		return Response{}, reason.Invalid("enrollment replay requires the invite secret")
	}
	if c.clock.ResampleNeeded() {
		if _, err := c.Metadata(ctx); err != nil {
			return Response{}, err
		}
	}
	response, err := c.Call(ctx, RequestSpec{Path: p.Route, Body: p.Request, Kind: p.Kind, RequestID: p.RequestID, Mutating: true, CredentialPath: p.CredentialRef, ClaimHandlePath: p.ClaimHandleRef, ClaimCredentialPath: p.ClaimCredentialRef, NewClaimHandlePath: p.NewClaimHandleRef, NewClaimCredentialPath: p.NewClaimCredentialRef, TargetOperationID: p.TargetOperationID, TargetHandlePath: p.TargetHandleRef})
	if err != nil || p.Kind == "operations/begin" {
		return response, err
	}
	if err := validateReplayResult(p, response.Result); err != nil {
		return Response{}, reason.New(reason.ReasonUnknownOutcome, "remote mutation result could not be validated")
	}
	if err := c.finalize(p.RequestID, ""); err != nil {
		return Response{}, err
	}
	if (p.Kind == "operations/complete" || p.Kind == "operations/reconcile") && p.TargetOperationID != "" {
		handlePath := p.TargetHandleRef
		if p.Kind == "operations/complete" {
			handlePath = p.ClaimHandleRef
			if handlePath == "" {
				handlePath = p.TargetHandleRef
			}
		}
		if err := c.finalize(p.TargetOperationID, handlePath); err != nil {
			return Response{}, err
		}
	}
	return response, nil
}

func validateReplayResult(p PendingRequest, result json.RawMessage) error {
	var request map[string]json.RawMessage
	if err := json.Unmarshal(p.Request, &request); err != nil {
		return reason.Invalid("pending request is invalid")
	}
	field := func(name string) string {
		var value string
		_ = json.Unmarshal(request[name], &value)
		return value
	}
	claimID, operationID := field("claimId"), field("operationId")
	switch p.Kind {
	case "acquire":
		var out lease.Grant
		if err := decodeResult(result, &out); err != nil || validateResult(&out) != nil || out.ClaimID != claimID || out.Receipt.OperationID != claimID || out.Receipt.ClaimID != claimID {
			return reason.Invalid("replayed grant does not match request")
		}
	case "transfer":
		var out lease.Grant
		if err := decodeResult(result, &out); err != nil || validateResult(&out) != nil || out.ClaimID != field("successorClaimId") || out.Receipt.OperationID != operationID || out.Receipt.ClaimID != claimID {
			return reason.Invalid("replayed transfer does not match request")
		}
	case "heartbeat", "checkpoint", "release", "operations/renew", "operations/complete":
		var out lease.Receipt
		if err := decodeResult(result, &out); err != nil || validateResult(&out) != nil || out.ClaimID != claimID {
			return reason.Invalid("replayed receipt does not match request")
		}
		expected := operationID
		if p.Kind == "operations/renew" {
			expected = field("renewalId")
		}
		if out.OperationID != expected {
			return reason.Invalid("replayed receipt does not match request")
		}
	case "operations/reconcile":
		var out lease.ReconciliationReceipt
		if err := decodeResult(result, &out); err != nil || !out.Committed || out.OperationID != operationID || out.ResolverClaimID != claimID || out.TargetClaimID != field("targetClaimId") || out.TargetOperationID != field("targetOperationId") {
			return reason.Invalid("replayed reconciliation does not match request")
		}
	default:
		var out map[string]json.RawMessage
		if err := decodeStrict(result, &out); err != nil || out == nil {
			return reason.Invalid("remote replay result is invalid")
		}
	}
	return nil
}

// ReplayEnrollment retries the exact durable enrollment body. The invite is
// supplied afresh and is never stored in the pending record.
func (c *HTTPClient) ReplayEnrollment(ctx context.Context, id, invite string, profilePath config.ProfilePaths) (config.Profile, lease.EnrollResult, error) {
	p, err := c.pending.Load(id)
	if err != nil {
		return config.Profile{}, lease.EnrollResult{}, err
	}
	if p.Kind != "enroll" {
		return config.Profile{}, lease.EnrollResult{}, reason.Invalid("pending request is not enrollment")
	}
	if !hex64(invite) {
		return config.Profile{}, lease.EnrollResult{}, reason.New(reason.ReasonCredentialMalformed, "invite is malformed")
	}
	metadataVerified := false
	if p.Endpoint == "" {
		if p.CertificateSHA256 != "" || p.AuthorityID != c.profile.AuthorityID || c.profile.AuthorityID == "" {
			return config.Profile{}, lease.EnrollResult{}, reason.New(reason.ReasonAuthorityMismatch, "legacy pending enrollment trust cannot be established")
		}
		metadata, metadataErr := c.Metadata(ctx)
		if metadataErr != nil {
			return config.Profile{}, lease.EnrollResult{}, metadataErr
		}
		if metadata.AuthorityID != p.AuthorityID || metadata.RestoreID != p.ExpectedRestoreID {
			return config.Profile{}, lease.EnrollResult{}, reason.New(reason.ReasonAuthorityRestored, "pending enrollment authority incarnation changed")
		}
		if err := c.pending.BindTrust(p.RequestID, c.profile.Endpoint, c.profile.CertificateSHA256); err != nil {
			return config.Profile{}, lease.EnrollResult{}, reason.New(reason.ReasonStorageFailure, "legacy pending enrollment trust could not be durably upgraded")
		}
		p.Endpoint, p.CertificateSHA256 = c.profile.Endpoint, c.profile.CertificateSHA256
		c.profile.AuthorityID, c.profile.RestoreID = p.AuthorityID, p.ExpectedRestoreID
		metadataVerified = true
	}
	if p.Endpoint != c.profile.Endpoint || p.CertificateSHA256 != c.profile.CertificateSHA256 || p.AuthorityID != c.profile.AuthorityID {
		return config.Profile{}, lease.EnrollResult{}, reason.New(reason.ReasonAuthorityMismatch, "pending enrollment trust does not match profile")
	}
	if c.profile.RestoreID != "" && p.ExpectedRestoreID != c.profile.RestoreID {
		return config.Profile{}, lease.EnrollResult{}, reason.New(reason.ReasonAuthorityRestored, "pending enrollment authority incarnation changed")
	}
	if !metadataVerified {
		metadata, err := c.Metadata(ctx)
		if err != nil {
			return config.Profile{}, lease.EnrollResult{}, err
		}
		if metadata.AuthorityID != p.AuthorityID || metadata.RestoreID != p.ExpectedRestoreID {
			return config.Profile{}, lease.EnrollResult{}, reason.New(reason.ReasonAuthorityRestored, "pending enrollment authority incarnation changed")
		}
		c.profile.AuthorityID, c.profile.RestoreID = p.AuthorityID, p.ExpectedRestoreID
	}
	c.profile.Credential.Path = p.CredentialRef
	cred, err := handle.ReadCredential(p.CredentialRef)
	if err != nil {
		return config.Profile{}, lease.EnrollResult{}, err
	}
	response, err := c.Call(ctx, RequestSpec{Path: p.Route, Body: p.Request, Kind: p.Kind, RequestID: p.RequestID, Mutating: true, Invite: invite, NewCredential: cred, CredentialPath: p.CredentialRef})
	if err != nil {
		return config.Profile{}, lease.EnrollResult{}, err
	}
	return c.activateEnrollment(id, response, profilePath)
}
func (c *HTTPClient) do(ctx context.Context, method, path string, body []byte, s RequestSpec) (Response, error) {
	base, err := url.Parse(c.profile.Endpoint)
	if err != nil {
		return Response{}, reason.New(reason.ReasonConfigInvalid, "profile endpoint is invalid")
	}
	u := *base
	u.Path = strings.TrimRight(base.Path, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return Response{}, reason.New(reason.ReasonInvalidArgument, "remote request could not be created")
	}
	req.Header.Set("Worklease-Protocol-Version", protocolVersion)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	if s.Invite != "" {
		req.Header.Set("Authorization", "Invite "+s.Invite)
	} else if path != "/.well-known/worklease" && path != "/healthz" {
		credentialPath := c.profile.Credential.Path
		if s.CredentialPath != "" {
			credentialPath = s.CredentialPath
		}
		if credentialPath == "" {
			return Response{}, reason.New(reason.ReasonCredentialUnsafe, "credential descriptor is required")
		}
		cred, e := handle.ReadCredential(credentialPath)
		if e != nil {
			return Response{}, e
		}
		req.Header.Set("Authorization", "Bearer "+cred)
	}
	if s.NewCredential != "" {
		req.Header.Set("Worklease-New-Installation-Authorization", "Bearer "+s.NewCredential)
	}
	newClaim := s.NewClaimCredential
	if newClaim == "" && s.NewClaimCredentialPath != "" {
		newClaim, err = handle.ReadCredential(s.NewClaimCredentialPath)
		if err != nil {
			return Response{}, err
		}
	}
	if newClaim == "" && s.NewClaimHandlePath != "" {
		h, e := handle.Read(s.NewClaimHandlePath)
		if e != nil {
			return Response{}, e
		}
		newClaim = h.Token
	}
	if newClaim != "" {
		req.Header.Set("Worklease-New-Claim-Authorization", "Bearer "+newClaim)
	}
	claim := s.ClaimCredential
	// A contextual handle is the authoritative claim credential source. A
	// secondary pending request may also retain the installation credential
	// path used for HTTP authentication; it must never be mistaken for the
	// claim bearer during exact replay.
	if claim == "" && s.ClaimHandlePath != "" {
		h, e := handle.Read(s.ClaimHandlePath)
		if e != nil {
			return Response{}, e
		}
		claim = h.Token
	}
	if claim == "" && s.ClaimCredentialPath != "" {
		claim, err = handle.ReadCredential(s.ClaimCredentialPath)
		if err != nil {
			return Response{}, err
		}
	}
	if claim != "" {
		req.Header.Set("Worklease-Claim-Authorization", "Bearer "+claim)
	}
	sent := time.Now()
	res, err := c.http.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		return Response{}, reason.New(reason.ReasonAuthenticationRequired, "remote redirects are not followed")
	}
	max := int64(4 << 20)
	if path == "/.well-known/worklease" {
		max = 4 << 10
	}
	if path == "/v1/enroll" {
		max = 16 << 10
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	received := time.Now()
	if err != nil || int64(len(b)) > max {
		return Response{}, reason.New(reason.ReasonResponseTooLarge, "remote response is too large")
	}
	ct, _, ctErr := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if ctErr != nil || !strings.EqualFold(ct, "application/json") {
		return Response{}, reason.Invalid("remote response media type is invalid")
	}
	var e envelope
	if err := decodeStrict(b, &e); err != nil {
		return Response{}, reason.Invalid("remote response envelope is invalid")
	}
	if e.OK && (res.StatusCode < 200 || res.StatusCode >= 300) || !e.OK && res.StatusCode >= 200 && res.StatusCode < 300 {
		return Response{}, reason.Invalid("remote response has an invalid status")
	}
	if e.OK && (e.Error != nil || len(e.Result) == 0 || bytes.Equal(bytes.TrimSpace(e.Result), []byte("null"))) || !e.OK && (e.Error == nil || len(e.Result) != 0) {
		return Response{}, reason.Invalid("remote response envelope is inconsistent")
	}
	if e.ProtocolVersion != protocolVersion || !validID(e.AuthorityID) || !validID(e.RestoreID) || e.AuthorityTime.IsZero() || e.AuthorityTime.Location() != time.UTC {
		return Response{}, reason.Invalid("remote response identity is invalid")
	}
	if err := c.clock.Sample(e.AuthorityTime, sent, received); err != nil {
		return Response{}, reason.New(reason.ReasonClockRegression, "authority time sample is invalid")
	}
	if !e.OK {
		if e.Error == nil || e.Error.Reason == "" {
			return Response{}, reason.Invalid("remote error envelope is invalid")
		}
		remoteErr := reason.New(e.Error.Reason, "remote request failed")
		if e.Error.Reason == reason.ReasonAlreadyClaimed {
			if resource, ok := boundedRemoteText(e.Error.Details["resource"], 1024); ok {
				remoteErr.With("resource", resource)
			}
			if holder := boundedRemoteHolder(e.Error.Details["holder"]); len(holder) > 0 {
				remoteErr.With("holder", holder)
			}
		}
		if e.Error.Reason == reason.ReasonResourceNotEnrolled {
			if prefixes, ok := e.Error.Details["admittedPrefixes"].([]any); ok {
				values := make([]string, 0, len(prefixes))
				for _, prefix := range prefixes {
					value, valid := prefix.(string)
					if !valid || len(value) > 256 {
						values = nil
						break
					}
					values = append(values, value)
				}
				if values != nil {
					remoteErr.With("admittedPrefixes", values)
				}
			}
		}
		return Response{AuthorityID: e.AuthorityID, RestoreID: e.RestoreID, AuthorityTime: e.AuthorityTime}, remoteErr
	}
	return Response{AuthorityID: e.AuthorityID, RestoreID: e.RestoreID, AuthorityTime: e.AuthorityTime, Result: e.Result}, nil
}

// boundedRemoteHolder admits only the redacted holder projection emitted by
// the authority. Error details are untrusted wire data, so malformed fields
// are omitted rather than forwarded to CLI renderers.
func boundedRemoteHolder(value any) map[string]any {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	projected := make(map[string]any, 4)
	if claimID, ok := boundedRemoteText(raw["claimId"], 32); ok && validID(claimID) {
		projected["claimId"] = claimID
	}
	if agentID, ok := boundedRemoteText(raw["agentId"], 128); ok {
		projected["agentId"] = agentID
	}
	if workKey, ok := boundedRemoteText(raw["workKey"], 1024); ok {
		projected["workKey"] = workKey
	}
	if expiresAt, ok := boundedRemoteText(raw["expiresAt"], 128); ok {
		if _, err := time.Parse(time.RFC3339Nano, expiresAt); err == nil {
			projected["expiresAt"] = expiresAt
		}
	}
	return projected
}

func boundedRemoteText(value any, max int) (string, bool) {
	text, ok := value.(string)
	if !ok || !utf8.ValidString(text) || len(text) == 0 || len(text) > max {
		return "", false
	}
	for _, r := range text {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			return "", false
		}
	}
	return text, true
}

func (c *HTTPClient) validate(r Response, metadata bool) error {
	if r.AuthorityID != c.profile.AuthorityID && c.profile.AuthorityID != "" {
		return reason.New(reason.ReasonAuthorityMismatch, "remote authority identity does not match profile")
	}
	if !metadata && c.profile.RestoreID != "" && r.RestoreID != c.profile.RestoreID {
		return reason.New(reason.ReasonAuthorityRestored, "remote authority incarnation does not match profile")
	}
	return nil
}
func decodeStrict(b []byte, v any) error {
	if !utf8.Valid(b) || duplicateJSON(b) != nil {
		return fmt.Errorf("invalid JSON")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	d.UseNumber()
	if err := d.Decode(v); err != nil {
		return err
	}
	var x any
	if err := d.Decode(&x); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
func duplicateJSON(b []byte) error {
	d := json.NewDecoder(bytes.NewReader(b))
	var walk func() error
	walk = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		if x, ok := t.(json.Delim); ok {
			if x == '{' {
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return e
					}
					ks, ok := k.(string)
					if !ok || seen[ks] {
						return fmt.Errorf("duplicate key")
					}
					seen[ks] = true
					if e = walk(); e != nil {
						return e
					}
				}
				_, e = d.Token()
				return e
			}
			if x == '[' {
				for d.More() {
					if e := walk(); e != nil {
						return e
					}
				}
				_, e = d.Token()
				return e
			}
		}
		return nil
	}
	if e := walk(); e != nil {
		return e
	}
	var extra any
	if e := d.Decode(&extra); !errors.Is(e, io.EOF) {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
func validateClientProfile(p config.Profile) error { return validateProfileForClient(p) }
func validateProfileForClient(p config.Profile) error { // config validation is intentionally repeated at the transport boundary
	if strings.TrimSpace(p.Endpoint) == "" {
		return reason.New(reason.ReasonConfigInvalid, "profile endpoint is required")
	}
	u, e := url.Parse(p.Endpoint)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return reason.New(reason.ReasonConfigInvalid, "profile endpoint is invalid")
	}
	if u.Scheme != "https" {
		if !p.AllowInsecureHTTP || u.Scheme != "http" {
			return reason.New(reason.ReasonConfigInvalid, "HTTPS is required for profile endpoint unless insecure HTTP is explicitly allowed")
		}
	}
	if p.AuthorityID != "" && !validID(p.AuthorityID) {
		return reason.New(reason.ReasonConfigInvalid, "profile authority ID is invalid")
	}
	if p.RestoreID != "" && !validID(p.RestoreID) {
		return reason.New(reason.ReasonConfigInvalid, "profile restore ID is invalid")
	}
	if p.CertificateSHA256 != "" && (!hex64(p.CertificateSHA256) || u.Scheme != "https") {
		return reason.New(reason.ReasonConfigInvalid, "profile certificate pin requires HTTPS and 64 lowercase hexadecimal characters")
	}
	return nil
}

// withCertificatePin makes the pinned leaf the TLS trust root. Hostname/IP
// and validity checks remain explicit because pinned deployments commonly use
// a private or self-signed certificate.
func withCertificatePin(profile config.Profile, transport http.RoundTripper) (http.RoundTripper, error) {
	if profile.CertificateSHA256 == "" || !strings.HasPrefix(strings.ToLower(profile.Endpoint), "https://") {
		return transport, nil
	}
	base, ok := transport.(*http.Transport)
	if !ok {
		return nil, reason.New(reason.ReasonConfigInvalid, "pinned HTTPS requires the standard HTTP transport")
	}
	//lint:ignore SA1019 DialTLS is deprecated but must still be rejected because it bypasses TLSClientConfig.
	if base.DialTLS != nil || base.DialTLSContext != nil {
		return nil, reason.New(reason.ReasonConfigInvalid, "pinned HTTPS rejects custom TLS dialers")
	}
	clone := base.Clone()
	tlsConfig := clone.TLSClientConfig
	if tlsConfig == nil {
		tlsConfig = &tls.Config{}
	} else {
		tlsConfig = tlsConfig.Clone()
	}
	tlsConfig.InsecureSkipVerify = true // certificate pin is the configured trust root
	tlsConfig.VerifyConnection = certificatePinVerifier(profile.Endpoint, profile.CertificateSHA256)
	clone.TLSClientConfig = tlsConfig
	return clone, nil
}

func certificatePinVerifier(endpoint, expected string) func(tls.ConnectionState) error {
	u, _ := url.Parse(endpoint)
	host := u.Hostname()
	return func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return reason.New(reason.ReasonAuthorityMismatch, "remote certificate is missing")
		}
		leaf := state.PeerCertificates[0]
		now := time.Now()
		if now.Before(leaf.NotBefore) || now.After(leaf.NotAfter) {
			return reason.New(reason.ReasonAuthorityMismatch, "remote certificate is not currently valid")
		}
		if err := leaf.VerifyHostname(host); err != nil {
			return reason.New(reason.ReasonAuthorityMismatch, "remote certificate does not match endpoint hostname")
		}
		digest := sha256.Sum256(leaf.Raw)
		decoded, err := hex.DecodeString(expected)
		if err != nil || len(decoded) != sha256.Size || subtle.ConstantTimeCompare(digest[:], decoded) != 1 {
			return reason.New(reason.ReasonAuthorityMismatch, "remote certificate does not match pinned certificate")
		}
		return nil
	}
}

// Enroll performs metadata trust establishment, then writes the credential and
// exact request before sending it. The profile is returned only after success.
func (c *HTTPClient) Enroll(ctx context.Context, invite, label string, profilePath config.ProfilePaths) (config.Profile, lease.EnrollResult, error) {
	pending, err := c.pending.List()
	if err != nil {
		return config.Profile{}, lease.EnrollResult{}, err
	}
	var enrollment *PendingRequest
	for i := range pending {
		if pending[i].Kind != "enroll" {
			continue
		}
		if enrollment != nil {
			return config.Profile{}, lease.EnrollResult{}, reason.New(reason.ReasonCredentialSourceConflict, "multiple pending enrollments require explicit recovery")
		}
		copy := pending[i]
		enrollment = &copy
	}
	if enrollment != nil {
		if enrollment.CredentialRef != c.profile.Credential.Path {
			return config.Profile{}, lease.EnrollResult{}, reason.New(reason.ReasonCredentialUnsafe, "pending enrollment credential path conflicts with profile")
		}
		return c.ReplayEnrollment(ctx, enrollment.RequestID, invite, profilePath)
	}
	if c.profile.AuthorityID == "" {
		return config.Profile{}, lease.EnrollResult{}, reason.New(reason.ReasonAuthorityMismatch, "enrollment requires a pinned expected authority")
	}
	metadata, err := c.Metadata(ctx)
	if err != nil {
		return config.Profile{}, lease.EnrollResult{}, err
	}
	if c.profile.RestoreID != "" && metadata.RestoreID != c.profile.RestoreID {
		return config.Profile{}, lease.EnrollResult{}, reason.New(reason.ReasonAuthorityRestored, "remote authority incarnation does not match profile")
	}
	// Discovery establishes the immutable identities used by the durable
	// enrollment request and every subsequent profile request.
	c.profile.AuthorityID, c.profile.RestoreID = metadata.AuthorityID, metadata.RestoreID
	id, err := newID()
	if err != nil {
		return config.Profile{}, lease.EnrollResult{}, err
	}
	cred, err := newCredential()
	if err != nil {
		return config.Profile{}, lease.EnrollResult{}, err
	}
	if err := handle.StoreCredentialNoReplace(c.profile.Credential.Path, cred); err != nil {
		return config.Profile{}, lease.EnrollResult{}, err
	}
	deadline := metadata.AuthorityTime.Add(24 * time.Hour)
	body, _ := json.Marshal(map[string]any{"protocolVersion": protocolVersion, "authorityId": metadata.AuthorityID, "expectedRestoreId": metadata.RestoreID, "requestId": id, "requestNotAfter": deadline, "installationId": id, "label": label})
	spec := RequestSpec{Path: "/v1/enroll", Body: body, Kind: "enroll", RequestID: id, Mutating: true, Terminal: true, Invite: invite, NewCredential: cred}
	response, err := c.Call(ctx, spec)
	if err != nil {
		if _, pendingErr := c.pending.Load(id); errors.Is(pendingErr, os.ErrNotExist) {
			if removeErr := handle.RemoveOwnerPrivate(c.profile.Credential.Path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				return config.Profile{}, lease.EnrollResult{}, reason.New(reason.ReasonStorageFailure, "failed enrollment credential cleanup")
			}
		}
		return config.Profile{}, lease.EnrollResult{}, err
	}
	return c.activateEnrollment(id, response, profilePath)
}

func (c *HTTPClient) activateEnrollment(id string, response Response, profilePath config.ProfilePaths) (config.Profile, lease.EnrollResult, error) {
	var enrolled lease.EnrollResult
	if err := decodeResult(response.Result, &enrolled); err != nil {
		return config.Profile{}, lease.EnrollResult{}, err
	}
	if !validID(enrolled.InstallationID) || (enrolled.Role != "read" && enrolled.Role != "write" && enrolled.Role != "admin") || enrolled.EnrolledAt.IsZero() {
		return config.Profile{}, lease.EnrollResult{}, reason.Invalid("remote enrollment result is invalid")
	}
	out := config.Profile{Name: c.profile.Name, Endpoint: c.profile.Endpoint, AuthorityID: c.profile.AuthorityID, RestoreID: c.profile.RestoreID, CertificateSHA256: c.profile.CertificateSHA256, AllowInsecureHTTP: c.profile.AllowInsecureHTTP, Credential: c.profile.Credential}
	if profilePath.Profiles == "" {
		return config.Profile{}, lease.EnrollResult{}, reason.New(reason.ReasonStorageFailure, "profile activation path is required")
	}
	profiles, defaultName, err := config.LoadProfiles(profilePath)
	if err != nil {
		return config.Profile{}, lease.EnrollResult{}, err
	}
	if existing, ok := profiles[out.Name]; ok {
		if existing.Endpoint != out.Endpoint || existing.AuthorityID != out.AuthorityID || existing.CertificateSHA256 != out.CertificateSHA256 {
			return config.Profile{}, lease.EnrollResult{}, reason.New(reason.ReasonAuthorityMismatch, "profile trust does not match invite artifact")
		}
		if existing.Credential.Path != out.Credential.Path {
			return config.Profile{}, lease.EnrollResult{}, reason.New(reason.ReasonCredentialUnsafe, "profile credential path collision")
		}
	}
	profiles[out.Name] = out
	if defaultName == "" {
		defaultName = out.Name
	}
	list := make([]config.Profile, 0, len(profiles))
	for _, profile := range profiles {
		list = append(list, profile)
	}
	if err := config.SaveProfiles(profilePath, list, defaultName); err != nil {
		return config.Profile{}, lease.EnrollResult{}, err
	}
	if err := c.finalize(id, ""); err != nil {
		return config.Profile{}, lease.EnrollResult{}, err
	}
	return out, enrolled, nil
}
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
func newCredential() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
