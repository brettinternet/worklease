package authority

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failingStore struct{}

func (failingStore) Save(PendingRequest) error { return errors.New("disk full") }
func (failingStore) Load(string) (PendingRequest, error) {
	return PendingRequest{}, errors.New("missing")
}
func (failingStore) Clear(string) error              { return nil }
func (failingStore) List() ([]PendingRequest, error) { return nil, nil }

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
}
