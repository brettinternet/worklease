package authority

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/reason"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failingStore struct{}

func (failingStore) Save(PendingRequest) error              { return errors.New("disk full") }
func (failingStore) BindTrust(string, string, string) error { return errors.New("disk full") }
func (failingStore) Load(string) (PendingRequest, error) {
	return PendingRequest{}, errors.New("missing")
}
func (failingStore) Clear(string) error              { return nil }
func (failingStore) List() ([]PendingRequest, error) { return nil, nil }

type retainedFailingStore struct{ pending PendingRequest }

func (s retainedFailingStore) Save(PendingRequest) error {
	return reason.New(reason.ReasonStorageFailure, "pending request could not be written")
}
func (retainedFailingStore) BindTrust(string, string, string) error { return nil }
func (s retainedFailingStore) Load(string) (PendingRequest, error)  { return s.pending, nil }
func (retainedFailingStore) Clear(string) error                     { return nil }
func (s retainedFailingStore) List() ([]PendingRequest, error) {
	return []PendingRequest{s.pending}, nil
}

func profileForTest(t *testing.T) config.Profile {
	return config.Profile{Name: "remote", Endpoint: "https://authority.example", AuthorityID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RestoreID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Credential: config.CredentialDescriptor{Path: t.TempDir() + "/credential"}}
}
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestRemoteMutationWritesBeforeDispatchAndRetainsUncertainty(t *testing.T) {
	p := profileForTest(t)
	calls := 0
	c, err := NewHTTPClient(p, failingStore{}, roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return response(200, `{}`), nil }))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Clock().Sample(time.Now().UTC(), time.Now().Add(-time.Millisecond), time.Now()); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"requestNotAfter":"2099-01-01T00:00:00Z"}`)
	_, err = c.Call(context.Background(), RequestSpec{Path: "/v1/claims/release", Kind: "release", RequestID: "cccccccccccccccccccccccccccccccc", Body: body, Mutating: true})
	if err == nil || calls != 0 {
		t.Fatalf("write failure dispatched: err=%v calls=%d", err, calls)
	}
	if classified := reason.As(err); classified == nil || classified.Reason != reason.ReasonStorageFailure || classified.Details["commitState"] != "not-committed" {
		t.Fatalf("staging failure classification=%v", err)
	}

	store := NewFilePendingStore(t.TempDir())
	c, err = NewHTTPClient(p, store, roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("connection reset") }))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Clock().Sample(time.Now().UTC(), time.Now().Add(-time.Millisecond).UTC(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_, err = c.Call(context.Background(), RequestSpec{Path: "/v1/claims/release", Kind: "release", RequestID: "dddddddddddddddddddddddddddddddd", Body: body, Mutating: true})
	if err == nil {
		t.Fatal("transport uncertainty was swallowed")
	}
	pending, err := store.Load("dddddddddddddddddddddddddddddddd")
	if err != nil || pending.State != "pending" {
		t.Fatalf("pending evidence lost: %#v %v", pending, err)
	}
}

func TestPreviouslyStagedRequestFailureRemainsUnknown(t *testing.T) {
	p := profileForTest(t)
	body := []byte(`{"requestNotAfter":"2099-01-01T00:00:00Z"}`)
	sum := sha256.Sum256(body)
	pending := PendingRequest{RequestID: strings.Repeat("c", 32), Request: body, RequestSHA256: hex.EncodeToString(sum[:])}
	calls := 0
	client, err := NewHTTPClient(p, retainedFailingStore{pending: pending}, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("must not dispatch")
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := client.Clock().Sample(now, now, now); err != nil {
		t.Fatal(err)
	}
	_, err = client.Call(context.Background(), RequestSpec{Path: "/v1/claims/release", Kind: "release", RequestID: pending.RequestID, Body: body, Mutating: true})
	classified := reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonStorageFailure || classified.Details["commitState"] != "unknown" || calls != 0 {
		t.Fatalf("retry staging failure=%v calls=%d", err, calls)
	}
}

func TestSecondaryPendingCollisionIsClassifiedWithoutDispatch(t *testing.T) {
	p := profileForTest(t)
	store := NewFilePendingStore(t.TempDir())
	requestID := strings.Repeat("c", 32)
	body := []byte(`{"requestNotAfter":"2099-01-01T00:00:00Z","private":"do-not-expose"}`)
	sum := sha256.Sum256(body)
	old := PendingRequest{RequestID: requestID, OperationID: requestID, Kind: "release", Route: "/v1/claims/release", AuthorityID: p.AuthorityID, Endpoint: p.Endpoint, ExpectedRestoreID: p.RestoreID, RequestNotAfter: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), Request: body, RequestSHA256: hex.EncodeToString(sum[:]), State: "pending"}
	if err := store.Save(old); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client, err := NewHTTPClient(p, store, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("must not dispatch")
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := client.Clock().Sample(now, now, now); err != nil {
		t.Fatal(err)
	}
	changed := []byte(`{"requestNotAfter":"2099-01-01T00:00:00Z","private":"changed"}`)
	_, err = client.Call(context.Background(), RequestSpec{Path: "/v1/claims/release", Kind: "release", RequestID: requestID, Body: changed, Mutating: true})
	classified := reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonOperationRequestMismatch || classified.Details["commitState"] != "not-committed" || calls != 0 {
		t.Fatalf("collision classification=%v calls=%d", err, calls)
	}
	preserved, loadErr := store.Load(requestID)
	if loadErr != nil || !bytes.Equal(preserved.Request, old.Request) || preserved.RequestSHA256 != old.RequestSHA256 {
		t.Fatalf("older pending request changed: %#v err=%v", preserved, loadErr)
	}
	if strings.Contains(err.Error(), "do-not-expose") || strings.Contains(err.Error(), "changed") {
		t.Fatalf("private request leaked: %v", err)
	}
	_, err = (&RemoteAuthority{Client: client}).Execute(context.Background(), RequestSpec{Path: "/v1/claims/release", Kind: "release", RequestID: requestID, Body: changed, Mutating: true})
	preserved, loadErr = store.Load(requestID)
	if reason.As(err) == nil || loadErr != nil || !bytes.Equal(preserved.Request, old.Request) {
		t.Fatalf("authority wrapper changed older pending request: err=%v pending=%#v load=%v", err, preserved, loadErr)
	}
}

func TestPendingAcquireBlocksLifecycleBeforeDispatch(t *testing.T) {
	p := profileForTest(t)
	root := t.TempDir()
	if err := handle.EnsureOwnerPrivateDir(root); err != nil {
		t.Fatal(err)
	}
	handlePath := root + "/claim.json"
	pendingID := strings.Repeat("a", 32)
	h := handle.Handle{SchemaVersion: handle.RemoteSchemaVersion, AuthorityID: p.AuthorityID, ClaimID: pendingID, Token: strings.Repeat("b", 64), Resources: []string{"coordination:test"}, AgentID: "agent", SessionID: "session", State: "pending", PendingRequest: &handle.PendingRequest{OperationID: pendingID, Kind: "acquire", AuthorityID: p.AuthorityID, Endpoint: p.Endpoint, ClaimID: pendingID, RequestHash: strings.Repeat("c", 64), RequestNotAfter: time.Now().Add(time.Hour), ExpectedRestoreID: p.RestoreID, Request: []byte(`{"requestNotAfter":"2099-01-01T00:00:00Z"}`), Inputs: map[string]any{"request": "retained"}}}
	if err := handle.Write(handlePath, h); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client, err := NewHTTPClient(p, NewFilePendingStore(root+"/pending"), roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("must not dispatch")
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := client.Clock().Sample(now, now, now); err != nil {
		t.Fatal(err)
	}
	for i, test := range []struct{ kind, path string }{{"heartbeat", "/v1/claims/heartbeat"}, {"checkpoint", "/v1/claims/checkpoint"}, {"release", "/v1/claims/release"}, {"transfer", "/v1/claims/transfer"}} {
		requestID := strings.Repeat([]string{"d", "e", "f", "1"}[i], 32)
		_, callErr := client.Call(context.Background(), RequestSpec{Path: test.path, Kind: test.kind, RequestID: requestID, Body: []byte(`{"requestNotAfter":"2099-01-01T00:00:00Z"}`), Mutating: true, HandlePath: handlePath, ClaimID: pendingID})
		classified := reason.As(callErr)
		if classified == nil || classified.Reason != reason.ReasonRecoveryRequired || classified.Details["operationId"] != pendingID || classified.Details["pendingPath"] != handlePath || classified.Details["commitState"] != "not-committed" {
			t.Fatalf("%s refusal=%v", test.kind, callErr)
		}
	}
	if calls != 0 {
		t.Fatalf("blocked lifecycle requests dispatched %d times", calls)
	}
	preserved, err := handle.Read(handlePath)
	if err != nil || preserved.PendingRequest == nil || preserved.PendingRequest.OperationID != pendingID {
		t.Fatalf("pending acquire changed: %#v err=%v", preserved, err)
	}
}

func TestRemoteRejectsRedirectAndValidatesEnvelope(t *testing.T) {
	p := profileForTest(t)
	c, err := NewHTTPClient(p, NewFilePendingStore(t.TempDir()), roundTripFunc(func(*http.Request) (*http.Response, error) { return response(302, `<a>`), nil }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata(context.Background()); err == nil {
		t.Fatal("redirect followed/accepted")
	}
	c, err = NewHTTPClient(p, NewFilePendingStore(t.TempDir()), roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2026-01-01T00:00:00Z","result":{},"extra":1}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata(context.Background()); err == nil {
		t.Fatal("unknown envelope field accepted")
	}
}

func TestMetadataDecodesOptionalPrefixesAndOldServers(t *testing.T) {
	p := profileForTest(t)
	for _, test := range []struct {
		name, result string
		wantPrefixes bool
	}{
		{name: "old server", result: `{}`, wantPrefixes: false},
		{name: "prefixes", result: `{"authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","supportedProtocolVersions":["worklease-http/1"],"authorityTime":"2026-01-01T00:00:00Z","admittedPrefixes":["coordination:"]}`, wantPrefixes: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, err := NewHTTPClient(p, NewFilePendingStore(t.TempDir()), roundTripFunc(func(*http.Request) (*http.Response, error) {
				return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2026-01-01T00:00:00Z","result":`+test.result+`}`), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			metadata, err := c.Metadata(context.Background())
			if err != nil || metadata.Metadata == nil || (metadata.Metadata.AdmittedPrefixes != nil) != test.wantPrefixes {
				t.Fatalf("metadata=%#v err=%v", metadata.Metadata, err)
			}
		})
	}
}

func TestRemoteErrorPreservesSafeDetails(t *testing.T) {
	p := profileForTest(t)
	credential := strings.Repeat("c", 64)
	if err := handle.StoreCredentialNoReplace(p.Credential.Path, credential); err != nil {
		t.Fatal(err)
	}
	store := NewFilePendingStore(t.TempDir())
	authorityTime := time.Now().UTC()
	c, err := NewHTTPClient(p, store, roundTripFunc(func(*http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`{"ok":false,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":%q,"error":{"reason":"resource-not-enrolled","message":"redacted","details":{"admittedPrefixes":["coordination:"]}}}`, authorityTime.Format(time.RFC3339Nano))
		return response(403, body), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Call(context.Background(), RequestSpec{Path: "/v1/claims/list", Body: []byte(`{}`), Kind: "list"})
	classified := reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonResourceNotEnrolled || classified.Details["admittedPrefixes"] == nil {
		t.Fatalf("error=%#v", err)
	}
	now := authorityTime
	if err := c.Clock().Sample(now, now, now); err != nil {
		t.Fatal(err)
	}
	requestID := strings.Repeat("d", 32)
	_, err = c.Call(context.Background(), RequestSpec{Path: "/v1/claims/release", Kind: "release", RequestID: requestID, Body: []byte(`{"requestNotAfter":"2099-01-01T00:00:00Z"}`), Mutating: true})
	classified = reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonResourceNotEnrolled || classified.Details["commitState"] != "not-committed" {
		t.Fatalf("definitive mutation rejection=%#v", err)
	}
	if _, loadErr := store.Load(requestID); !errors.Is(loadErr, os.ErrNotExist) {
		t.Fatalf("definitively rejected request retained: %v", loadErr)
	}

	retryID := strings.Repeat("e", 32)
	retryBody := []byte(`{"requestNotAfter":"2099-01-01T00:00:00Z"}`)
	retryHash := sha256.Sum256(retryBody)
	previouslyDispatched := PendingRequest{RequestID: retryID, OperationID: retryID, Kind: "release", Route: "/v1/claims/release", AuthorityID: p.AuthorityID, Endpoint: p.Endpoint, ExpectedRestoreID: p.RestoreID, RequestNotAfter: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), Request: retryBody, RequestSHA256: hex.EncodeToString(retryHash[:]), State: "pending"}
	if err := store.Save(previouslyDispatched); err != nil {
		t.Fatal(err)
	}
	_, err = c.Call(context.Background(), RequestSpec{Path: "/v1/claims/release", Kind: "release", RequestID: retryID, Body: retryBody, Mutating: true})
	classified = reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonResourceNotEnrolled || classified.Details["commitState"] != "unknown" {
		t.Fatalf("previously dispatched rejection=%#v", err)
	}
	preserved, loadErr := store.Load(retryID)
	if loadErr != nil || !bytes.Equal(preserved.Request, retryBody) || preserved.RequestSHA256 != previouslyDispatched.RequestSHA256 {
		t.Fatalf("previously dispatched request was cleared: %#v err=%v", preserved, loadErr)
	}
}

func TestAuthorityTimeIsConservativeUnderAsymmetricLatency(t *testing.T) {
	current := time.Date(2026, 1, 1, 0, 0, 10, 0, time.UTC)
	c := NewAuthorityClock(func() time.Time { return current })
	sent := current.Add(-2 * time.Second)
	received := current
	authority := current.Add(-1500 * time.Millisecond)
	if err := c.Sample(authority, sent, received); err != nil {
		t.Fatal(err)
	}
	lower, err := c.LowerBound()
	if err != nil {
		t.Fatal(err)
	}
	if !lower.Equal(current.Add(-1500 * time.Millisecond)) {
		t.Fatalf("lower bound=%s", lower)
	}
	upper, err := c.UpperBound()
	if err != nil || !upper.Equal(current.Add(500*time.Millisecond)) {
		t.Fatalf("upper bound=%s err=%v", upper, err)
	}
	deadline, err := c.RequestNotAfter()
	if err != nil || !deadline.Equal(lower.Add(24*time.Hour)) {
		t.Fatalf("deadline=%s err=%v", deadline, err)
	}
	if c.ShouldRenew(current, 2*time.Second) {
		t.Fatal("renewed before half ttl")
	}
	if !c.CanStart(current, 2*time.Second) {
		t.Fatal("stopped work before three-quarter ttl")
	}
	if c.DispatchAllowedFromSample(time.Second) {
		t.Fatal("response latency extended the authority-time ttl")
	}
}
