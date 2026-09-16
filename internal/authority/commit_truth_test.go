package authority

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
)

// A mutation that reaches a server and commits, whose response then fails
// identity validation, must never be reported as definitively uncommitted.
func TestPostDispatchIdentityMismatchRetainsUncertainty(t *testing.T) {
	root := t.TempDir()
	profile := profileForTest(t)
	if err := handle.StoreCredential(profile.Credential.Path, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	store := NewFilePendingStore(filepath.Join(root, "pending"))
	client, err := NewHTTPClient(profile, store, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"cccccccccccccccccccccccccccccccc","authorityTime":"2099-01-01T00:00:00Z","result":{}}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := client.Clock().Sample(now, now, now); err != nil {
		t.Fatal(err)
	}
	requestID := strings.Repeat("d", 32)
	body := []byte(`{"requestNotAfter":"2099-01-01T00:00:00Z"}`)
	_, err = client.Call(context.Background(), RequestSpec{Path: "/v1/claims/release", Kind: "release", RequestID: requestID, Body: body, Mutating: true})
	classified := reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonAuthorityRestored {
		t.Fatalf("restored identity mismatch=%#v", err)
	}
	if classified.Details["commitState"] != "unknown" {
		t.Fatalf("post-dispatch identity mismatch reported commitState=%v", classified.Details["commitState"])
	}
	if _, loadErr := store.Load(requestID); loadErr != nil {
		t.Fatalf("uncertain request was not retained: %v", loadErr)
	}
}

// A validated grant whose local activation fails is committed remotely; the
// caller must see an uncertain outcome, never a definitive no-commit.
func TestAcquireActivationFailureIsUncertainNotDefinitive(t *testing.T) {
	root := t.TempDir()
	profile := profileForTest(t)
	if err := handle.StoreCredential(profile.Credential.Path, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	handlePath := filepath.Join(root, "handles", "claim.json")
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(handlePath)); err != nil {
		t.Fatal(err)
	}
	// The grant matches the request identity but reports a different agent, so
	// activation refuses while the receipt still says the acquire committed.
	client, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(root, "pending")), roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2099-01-01T00:00:00Z","result":{"claimId":"dddddddddddddddddddddddddddddddd","resources":["coordination:new"],"agentId":"other-agent","sessionId":"new-session","workKey":"new-work","revision":1,"expiresAt":"2099-01-01T00:01:00Z","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","receipt":{"operationId":"dddddddddddddddddddddddddddddddd","claimId":"dddddddddddddddddddddddddddddddd","kind":"acquire","requestSha256":"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","revision":1,"committed":true}}}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := client.Clock().Sample(now, now, now); err != nil {
		t.Fatal(err)
	}
	remote, _ := NewRemoteAuthority(client)
	_, err = remote.Acquire(context.Background(), lease.AcquireRequest{ClaimID: strings.Repeat("d", 32), Token: strings.Repeat("7", 64), Resources: []string{"coordination:new"}, AgentID: "new-agent", SessionID: "new-session", WorkKey: "new-work", TTL: time.Minute, MaxHold: time.Hour, RequestNotAfter: time.Now().Add(time.Hour), HandlePath: handlePath})
	classified := reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonUnknownOutcome {
		t.Fatalf("activation failure=%#v", err)
	}
	if reason.DefinitiveNoCommit(err) {
		t.Fatalf("committed acquire was reported as definitively uncommitted: %#v", err)
	}
	stillPending, readErr := handle.Read(handlePath)
	if readErr != nil || stillPending.State != "pending" || stillPending.PendingRequest == nil {
		t.Fatalf("uncertain acquire lost its recoverable request: %#v err=%v", stillPending, readErr)
	}
}

// Resource order is not grant identity, so an authority that canonicalizes
// order must not permanently wedge a committed pending acquire.
func TestAcquireActivationAcceptsCanonicalizedResourceOrder(t *testing.T) {
	root := t.TempDir()
	profile := profileForTest(t)
	if err := handle.StoreCredential(profile.Credential.Path, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	handlePath := filepath.Join(root, "handles", "claim.json")
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(handlePath)); err != nil {
		t.Fatal(err)
	}
	client, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(root, "pending")), roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2099-01-01T00:00:00Z","result":{"claimId":"dddddddddddddddddddddddddddddddd","resources":["coordination:a","coordination:b"],"agentId":"new-agent","sessionId":"new-session","workKey":"new-work","revision":1,"expiresAt":"2099-01-01T00:01:00Z","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","receipt":{"operationId":"dddddddddddddddddddddddddddddddd","claimId":"dddddddddddddddddddddddddddddddd","kind":"acquire","requestSha256":"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","revision":1,"committed":true}}}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := client.Clock().Sample(now, now, now); err != nil {
		t.Fatal(err)
	}
	remote, _ := NewRemoteAuthority(client)
	// The request orders resources b,a; the grant returns a,b.
	if _, err := remote.Acquire(context.Background(), lease.AcquireRequest{ClaimID: strings.Repeat("d", 32), Token: strings.Repeat("7", 64), Resources: []string{"coordination:b", "coordination:a"}, AgentID: "new-agent", SessionID: "new-session", WorkKey: "new-work", TTL: time.Minute, MaxHold: time.Hour, RequestNotAfter: time.Now().Add(time.Hour), HandlePath: handlePath}); err != nil {
		t.Fatalf("canonicalized resource order wedged a committed acquire: %v", err)
	}
	ready, readErr := handle.Read(handlePath)
	if readErr != nil || ready.State != "ready" || ready.PendingRequest != nil || strings.Join(ready.Resources, ",") != "coordination:a,coordination:b" {
		t.Fatalf("activated handle=%#v err=%v", ready, readErr)
	}
	// A grant that substitutes a resource is still refused. The response above
	// advanced the sampled authority clock, so use a deadline beyond it.
	other := filepath.Join(root, "handles", "other.json")
	_, err = remote.Acquire(context.Background(), lease.AcquireRequest{ClaimID: strings.Repeat("d", 32), Token: strings.Repeat("7", 64), Resources: []string{"coordination:a", "coordination:c"}, AgentID: "new-agent", SessionID: "new-session", WorkKey: "new-work", TTL: time.Minute, MaxHold: time.Hour, RequestNotAfter: time.Date(2099, 1, 1, 0, 30, 0, 0, time.UTC), HandlePath: other})
	if classified := reason.As(err); classified == nil || classified.Reason != reason.ReasonUnknownOutcome {
		t.Fatalf("substituted resource was accepted: %#v", err)
	}
}

// A blocked or hostile pending store is a storage fault, not a request-level
// cause. Its filesystem classification must not leak past staging.
func TestStagingFailureReportsStorageFaultsAsStorageFailure(t *testing.T) {
	root := t.TempDir()
	profile := profileForTest(t)
	if err := handle.StoreCredential(profile.Credential.Path, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	// A regular file where the pending directory belongs classifies as an
	// unsafe path at the handle layer.
	pendingRoot := filepath.Join(root, "pending")
	if err := os.WriteFile(pendingRoot, []byte("blocked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client, err := NewHTTPClient(profile, NewFilePendingStore(pendingRoot), roundTripFunc(func(*http.Request) (*http.Response, error) {
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
	_, err = client.Call(context.Background(), RequestSpec{Path: "/v1/claims/release", Kind: "release", RequestID: strings.Repeat("d", 32), Body: []byte(`{"requestNotAfter":"2099-01-01T00:00:00Z"}`), Mutating: true})
	classified := reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonStorageFailure || classified.Details["commitState"] != "not-committed" || calls != 0 {
		t.Fatalf("blocked pending store=%#v calls=%d", err, calls)
	}
}
