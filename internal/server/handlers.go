package server

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
)

type envelope struct {
	OK              bool       `json:"ok"`
	ProtocolVersion string     `json:"protocolVersion"`
	AuthorityID     string     `json:"authorityId"`
	RestoreID       string     `json:"restoreId"`
	AuthorityTime   time.Time  `json:"authorityTime"`
	Result          any        `json:"result,omitempty"`
	Error           *wireError `json:"error,omitempty"`
}
type wireError struct {
	Reason  string         `json:"reason"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (s *Server) handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	w = &statusWriter{ResponseWriter: w}
	start := time.Now()
	route := routeTemplate(r.URL.Path)
	defer func() {
		protocol := "unsupported"
		if r.Header.Get("Worklease-Protocol-Version") == ProtocolVersion {
			protocol = ProtocolVersion
		}
		s.logger.Printf("method=%s route=%s status=%d duration=%s protocol=%s", r.Method, route, responseStatus(w), time.Since(start), protocol)
	}()
	if r.URL.RawQuery != "" {
		s.fail(w, reason.Invalid("query parameters are not supported"))
		return
	}
	if !acceptsJSON(r.Header.Get("Accept")) {
		s.fail(w, reason.Invalid("Accept must include application/json"))
		return
	}
	if r.Method == "GET" && r.URL.Path == "/healthz" {
		if hasBody(r) {
			s.fail(w, reason.Invalid("GET request must not have a body"))
			return
		}
		ok, retry := s.rates.allow("health", source(r))
		if !ok {
			s.rate(w, retry)
			return
		}
		s.health(w)
		return
	}
	if r.Method == "GET" && r.URL.Path == "/.well-known/worklease" {
		if hasBody(r) {
			s.fail(w, reason.Invalid("GET request must not have a body"))
			return
		}
		ok, retry := s.rates.allow("metadata", source(r))
		if !ok {
			s.rate(w, retry)
			return
		}
		if !s.version(r) {
			s.failStatus(w, reason.New(reason.ReasonProtocolVersionUnsupported, "protocol version is unsupported").With("supportedProtocolVersions", []string{ProtocolVersion}), 426)
			return
		}
		s.metadata(w)
		return
	}
	if !s.version(r) {
		s.failStatus(w, reason.New(reason.ReasonProtocolVersionUnsupported, "protocol version is unsupported").With("supportedProtocolVersions", []string{ProtocolVersion}), 426)
		return
	}
	if r.Method != "POST" || !knownRoute(r.URL.Path) {
		s.fail(w, reason.New(reason.ReasonInvalidPath, "route is invalid"))
		return
	}
	if err := preflightAuthentication(r); err != nil {
		s.failStatus(w, err, 401)
		return
	}
	contentType, parameters, parseErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if parseErr != nil || contentType != "application/json" || len(parameters) != 1 || !strings.EqualFold(parameters["charset"], "utf-8") {
		s.fail(w, reason.Invalid("content type must be application/json; charset=utf-8"))
		return
	}
	bucket := ""
	if r.URL.Path == "/v1/enroll" {
		bucket = "enroll"
	}
	if bucket != "" {
		ok, retry := s.rates.allow(bucket, source(r))
		if !ok {
			s.rate(w, retry)
			return
		}
	}
	bodyLimit := int64(maxRequestBody)
	if r.URL.Path == "/v1/enroll" {
		bodyLimit = 16 << 10
	}
	body, err := readJSON(r, bodyLimit)
	if err != nil {
		s.failStatus(w, err, statusOf(err))
		return
	}
	var result any
	switch r.URL.Path {
	case "/v1/enroll":
		result, err = s.enroll(r.Context(), r, body)
	case "/v1/claims/acquire":
		result, err = s.acquire(r.Context(), r, body)
	case "/v1/claims/status":
		result, err = s.status(r.Context(), r, body)
	case "/v1/claims/list":
		result, err = s.list(r.Context(), r, body)
	case "/v1/claims/heartbeat":
		result, err = s.heartbeat(r.Context(), r, body)
	case "/v1/claims/checkpoint":
		result, err = s.checkpoint(r.Context(), r, body)
	case "/v1/claims/release":
		result, err = s.release(r.Context(), r, body)
	case "/v1/claims/transfer":
		result, err = s.transfer(r.Context(), r, body)
	case "/v1/claims/verify":
		result, err = s.verify(r.Context(), r, body)
	case "/v1/operations/begin":
		result, err = s.begin(r.Context(), r, body)
	case "/v1/operations/renew":
		result, err = s.operationRenew(r.Context(), r, body)
	case "/v1/operations/complete":
		result, err = s.complete(r.Context(), r, body)
	case "/v1/operations/inspect":
		result, err = s.inspect(r.Context(), r, body)
	case "/v1/operations/reconcile":
		result, err = s.reconcile(r.Context(), r, body)
	case "/v1/events":
		result, err = s.events(r.Context(), r, body)
	case "/v1/history":
		result, err = s.history(r.Context(), r, body)
	case "/v1/watch":
		result, err = s.watch(r.Context(), r, body)
	case "/v1/admin/gc":
		result, err = s.gc(r.Context(), r, body)
	case "/v1/admin/invites/issue":
		result, err = s.issueInvite(r.Context(), r, body)
	case "/v1/installations/self":
		result, err = s.installationSelf(r.Context(), r, body)
	case "/v1/admin/installations/list":
		result, err = s.installations(r.Context(), r, body)
	case "/v1/admin/installations/revoke":
		result, err = s.revokeInstallation(r.Context(), r, body)
	case "/v1/admin/claims/revoke":
		result, err = s.revokeClaim(r.Context(), r, body)
	case "/v1/admin/recovery/status":
		result, err = s.recoveryStatus(r.Context(), r, body)
	case "/v1/admin/recovery/reopen":
		result, err = s.reopen(r.Context(), r, body)
	default:
		s.fail(w, reason.New(reason.ReasonInvalidPath, "route is invalid"))
		return
	}
	if err != nil {
		s.failStatus(w, err, statusOf(err))
		return
	}
	responseLimit := maxResponseBody
	if r.URL.Path == "/v1/enroll" {
		responseLimit = 16 << 10
	}
	s.write(w, http.StatusOK, result, responseLimit)
}
func acceptsJSON(value string) bool {
	for _, part := range strings.Split(value, ",") {
		mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err == nil && mediaType == "application/json" {
			return true
		}
	}
	return false
}

func hasBody(r *http.Request) bool {
	return r.ContentLength != 0 || len(r.TransferEncoding) != 0
}

func knownRoute(path string) bool {
	switch path {
	case "/v1/enroll", "/v1/installations/self", "/v1/claims/acquire", "/v1/claims/status", "/v1/claims/list", "/v1/claims/heartbeat", "/v1/claims/checkpoint", "/v1/claims/release", "/v1/claims/transfer", "/v1/claims/verify", "/v1/operations/begin", "/v1/operations/renew", "/v1/operations/complete", "/v1/operations/inspect", "/v1/operations/reconcile", "/v1/events", "/v1/history", "/v1/watch", "/v1/admin/gc", "/v1/admin/invites/issue", "/v1/admin/installations/list", "/v1/admin/installations/revoke", "/v1/admin/claims/revoke", "/v1/admin/recovery/status", "/v1/admin/recovery/reopen":
		return true
	default:
		return false
	}
}

func routeTemplate(path string) string {
	if strings.HasPrefix(path, "/v1/") {
		for _, x := range []string{"/claims/", "/operations/", "/admin/"} {
			if i := strings.Index(path, x); i >= 0 {
				return path[:i] + x + "{operation}"
			}
		}
		return "/v1/{operation}"
	}
	if path == "/healthz" || path == "/.well-known/worklease" {
		return path
	}
	return "unknown"
}
func source(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return h
	}
	return "unknown"
}
func responseStatus(w http.ResponseWriter) int {
	if x, ok := w.(*statusWriter); ok && x.status != 0 {
		return x.status
	}
	return 200
}
func (s *Server) health(w http.ResponseWriter) { writeRaw(w, 200, []byte(`{"ok":true}`), 1024) }
func (s *Server) metadata(w http.ResponseWriter) {
	ctx := s.service.ResponseContext()
	s.write(w, 200, map[string]any{"authorityId": ctx.AuthorityID, "restoreId": ctx.RestoreID, "supportedProtocolVersions": []string{ProtocolVersion}, "authorityTime": ctx.AuthorityTime, "admittedPrefixes": append([]string(nil), s.cfg.Prefixes...)}, 4000)
}
func (s *Server) version(r *http.Request) bool {
	return r.Header.Get("Worklease-Protocol-Version") == ProtocolVersion
}
func (s *Server) rate(w http.ResponseWriter, retry int) {
	w.Header().Set("Retry-After", strconv.Itoa(retry))
	s.failStatus(w, reason.New(reason.ReasonRateLimited, "rate limit exceeded"), 429)
}
func readJSON(r *http.Request, limit int64) ([]byte, error) {
	if r.ContentLength > limit {
		return nil, reason.New(reason.ReasonRequestTooLarge, "request body is too large")
	}
	limited := io.LimitReader(r.Body, limit+1)
	b, err := io.ReadAll(limited)
	if err != nil {
		return nil, reason.New(reason.ReasonInvalidArgument, "request body could not be read")
	}
	if int64(len(b)) > limit {
		return nil, reason.New(reason.ReasonRequestTooLarge, "request body is too large")
	}
	if len(b) == 0 {
		return nil, reason.Invalid("request body is required")
	}
	if err := strictJSON(b); err != nil {
		return nil, reason.Invalid("request JSON is invalid")
	}
	return b, nil
}
func strictJSON(b []byte) error {
	if !utf8.Valid(b) {
		return errors.New("invalid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := duplicateValue(dec); err != nil {
		return err
	}
	var x any
	if err := dec.Decode(&x); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}
func duplicateValue(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); ok {
		if d == '{' {
			seen := map[string]bool{}
			for dec.More() {
				k, e := dec.Token()
				if e != nil {
					return e
				}
				ks, ok := k.(string)
				if !ok || seen[ks] {
					return errors.New("duplicate key")
				}
				seen[ks] = true
				if e = duplicateValue(dec); e != nil {
					return e
				}
			}
			_, err = dec.Token()
			return err
		}
		if d == '[' {
			for dec.More() {
				if err := duplicateValue(dec); err != nil {
					return err
				}
			}
			_, err = dec.Token()
			return err
		}
	}
	return nil
}
func decode(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	d.UseNumber()
	if err := d.Decode(v); err != nil {
		return reason.Invalid("request structure is invalid")
	}
	var x any
	if err := d.Decode(&x); !errors.Is(err, io.EOF) {
		return reason.Invalid("request JSON has trailing data")
	}
	return nil
}
func (s *Server) write(w http.ResponseWriter, status int, result any, max int) {
	projected, err := projectResult(result)
	if err != nil {
		s.failStatus(w, reason.New(reason.ReasonInternal, "response encoding failed"), 500)
		return
	}
	ctx := s.service.ResponseContext()
	e := envelope{OK: true, ProtocolVersion: ProtocolVersion, AuthorityID: ctx.AuthorityID, RestoreID: ctx.RestoreID, AuthorityTime: ctx.AuthorityTime, Result: projected}
	b, err := json.Marshal(e)
	if err != nil {
		s.failStatus(w, reason.New(reason.ReasonInternal, "response encoding failed"), 500)
		return
	}
	if len(b) > max || len(b) > maxResponseBody {
		s.failStatus(w, reason.New(reason.ReasonResponseTooLarge, "response is too large"), 507)
		return
	}
	writeRaw(w, status, b, max)
}

// projectResult validates that a typed result is JSON and strips fields that
// are local-only from lease projections at the remote transport boundary.
func projectResult(result any) (any, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var projected any
	if err := decoder.Decode(&projected); err != nil {
		return nil, err
	}
	stripLocalLeaseFields(projected)
	return projected, nil
}

func stripLocalLeaseFields(value any) {
	switch typed := value.(type) {
	case map[string]any:
		if _, claim := typed["claimId"]; claim {
			if _, resources := typed["resources"]; resources {
				delete(typed, "localReplaceAllowed")
				delete(typed, "installationId")
				delete(typed, "restoreId")
			}
		}
		for _, nested := range typed {
			stripLocalLeaseFields(nested)
		}
	case []any:
		for _, nested := range typed {
			stripLocalLeaseFields(nested)
		}
	}
}
func writeRaw(w http.ResponseWriter, status int, b []byte, max int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
func statusOf(err error) int {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 408
	}
	e := reason.As(err)
	if e == nil {
		return 500
	}
	switch e.Reason {
	case reason.ReasonAuthenticationRequired, reason.ReasonInstallationRevoked, reason.ReasonInvalidToken, reason.ReasonInviteInvalid, reason.ReasonInviteExpired, reason.ReasonCredentialUnsafe, reason.ReasonCredentialMalformed, reason.ReasonCredentialSourceConflict:
		return 401
	case reason.ReasonAuthorizationDenied, reason.ReasonResourceNotEnrolled, reason.ReasonUnsupportedCoordinationReplace:
		return 403
	case reason.ReasonOperationNotFound:
		return 404
	case reason.ReasonInterrupted, reason.ReasonCancelled:
		return 408
	case reason.ReasonAlreadyClaimed, reason.ReasonStaleClaim, reason.ReasonStaleRevision, reason.ReasonClaimExpired, reason.ReasonVerifyFailed, reason.ReasonOwnershipLost, reason.ReasonHandleInUse, reason.ReasonOperationInProgress, reason.ReasonUnknownOutcomePending, reason.ReasonOperationRequestMismatch, reason.ReasonExpectedHashMismatch, reason.ReasonReconciliationConflict, reason.ReasonAuthorityMismatch, reason.ReasonInviteUsed:
		return 409
	case reason.ReasonReplayExpired:
		return 410
	case reason.ReasonRequestTooLarge:
		return 413
	case reason.ReasonAuthorityRestored, reason.ReasonRecoveryRequired, reason.ReasonRecoveryClosed, reason.ReasonUnknownOutcome, reason.ReasonOperationAmbiguous:
		return 422
	case reason.ReasonProtocolVersionUnsupported, reason.ReasonSchemaUnsupported:
		return 426
	case reason.ReasonRateLimited, reason.ReasonWaitTimeout:
		return 429
	case reason.ReasonStorageFailure, reason.ReasonSchemaCorrupt, reason.ReasonHomeUnsafe, reason.ReasonClockRegression:
		return 503
	case reason.ReasonResponseTooLarge:
		return 507
	}
	return 400
}
func (s *Server) fail(w http.ResponseWriter, err error) { s.failStatus(w, err, statusOf(err)) }
func (s *Server) failStatus(w http.ResponseWriter, err error, status int) {
	e := reason.As(err)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		e = reason.New(reason.ReasonCancelled, "request was cancelled")
	} else if e == nil {
		e = reason.New(reason.ReasonInternal, "internal server error")
	}
	details := map[string]any{}
	for k, v := range e.Details {
		if strings.Contains(strings.ToLower(k), "token") || strings.Contains(strings.ToLower(k), "credential") || strings.Contains(strings.ToLower(k), "secret") || strings.Contains(strings.ToLower(k), "hash") {
			continue
		}
		details[k] = v
	}
	msg := e.Message
	if len(msg) > 4096 {
		msg = msg[:4096]
	}
	ctx := s.service.ResponseContext()
	if encoded, marshalErr := json.Marshal(details); marshalErr != nil || len(encoded) > 16<<10 {
		details = nil
	}
	x := envelope{OK: false, ProtocolVersion: ProtocolVersion, AuthorityID: ctx.AuthorityID, RestoreID: ctx.RestoreID, AuthorityTime: ctx.AuthorityTime, Error: &wireError{Reason: e.Reason, Message: msg, Details: details}}
	b, _ := json.Marshal(x)
	if len(b) > maxResponseBody {
		b = []byte(`{"ok":false,"protocolVersion":"worklease-http/1","error":{"reason":"internal","message":"internal server error"}}`)
	}
	writeRaw(w, status, b, maxResponseBody)
}

func preflightAuthentication(r *http.Request) error {
	if r.URL.Path == "/v1/enroll" {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || parts[0] != "Invite" || len(parts[1]) != 64 {
			return reason.New(reason.ReasonInviteInvalid, "invite authentication is required")
		}
		if _, err := auth(r, "Worklease-New-Installation-Authorization"); err != nil {
			return err
		}
		return nil
	}
	_, err := auth(r, "Authorization")
	return err
}

func inviteAuth(r *http.Request) (string, error) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || parts[0] != "Invite" || len(parts[1]) != 64 {
		return "", reason.New(reason.ReasonInviteInvalid, "invite authentication is required")
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", reason.New(reason.ReasonInviteInvalid, "invite authentication is required")
	}
	return parts[1], nil
}

func auth(r *http.Request, header string) (string, error) {
	v := r.Header.Get(header)
	parts := strings.Fields(v)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return "", reason.New(reason.ReasonAuthenticationRequired, "authentication is required")
	}
	if len(parts[1]) != 64 {
		return "", reason.New(reason.ReasonAuthenticationRequired, "authentication is required")
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", reason.New(reason.ReasonAuthenticationRequired, "authentication is required")
	}
	return parts[1], nil
}
func (s *Server) actor(r *http.Request) (lease.RemoteActor, string, error) {
	token, e := auth(r, "Authorization")
	if e != nil {
		return lease.RemoteActor{}, "", e
	}
	return lease.RemoteActor{AuthorityID: r.Header.Get("Worklease-Authority-ID"), ExpectedRestoreID: r.Header.Get("Worklease-Restore-ID"), Credential: token}, token, nil
}
func (s *Server) authenticated(r *http.Request, b []byte, _ string) (lease.RemoteActor, error) {
	a, _, e := s.actor(r)
	if e != nil {
		return a, e
	}
	// The route-specific decoder has already rejected unknown fields. Decode
	// only the common context here without applying that route schema again.
	var c common
	if e = json.Unmarshal(b, &c); e != nil {
		return a, reason.Invalid("request structure is invalid")
	}
	if c.ProtocolVersion != ProtocolVersion {
		return a, reason.New(reason.ReasonProtocolVersionUnsupported, "protocol version is unsupported")
	}
	if c.AuthorityID == "" || c.ExpectedRestoreID == "" {
		return a, reason.Invalid("authorityId and expectedRestoreId are required")
	}
	a.AuthorityID = c.AuthorityID
	a.ExpectedRestoreID = c.ExpectedRestoreID
	return a, nil
}

type common struct {
	ProtocolVersion   string `json:"protocolVersion"`
	AuthorityID       string `json:"authorityId"`
	ExpectedRestoreID string `json:"expectedRestoreId"`
	OperationID       string `json:"operationId"`
	RequestNotAfter   string `json:"requestNotAfter"`
}

func when(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, reason.Invalid("requestNotAfter is required")
	}
	t, e := time.Parse(time.RFC3339Nano, v)
	if e != nil {
		return time.Time{}, reason.Invalid("timestamp is invalid")
	}
	return t.UTC(), nil
}
func micros(v int64) time.Duration { return time.Duration(v) * time.Microsecond }
func validCommon(c common) (time.Time, error) {
	if c.ProtocolVersion != ProtocolVersion {
		return time.Time{}, reason.New(reason.ReasonProtocolVersionUnsupported, "protocol version is unsupported")
	}
	return when(c.RequestNotAfter)
}
