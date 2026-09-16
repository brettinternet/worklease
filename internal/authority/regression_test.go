package authority

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
)

func TestCredentialBearingReadRequiresPinnedProfile(t *testing.T) {
	profile := profileForTest(t)
	profile.AuthorityID, profile.RestoreID = "", ""
	calls := 0
	client, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(t.TempDir(), "pending")), roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("must not dispatch")
	}))
	if err != nil {
		t.Fatal(err)
	}
	authority, _ := NewRemoteAuthority(client)
	if _, err := authority.Status(context.Background(), lease.Selector{ClaimID: strings.Repeat("c", 32)}); err == nil || calls != 0 {
		t.Fatalf("unpinned credential request dispatched: err=%v calls=%d", err, calls)
	}
}

func TestRemoteStatusRejectsNestedClaimIdentityMismatch(t *testing.T) {
	profile := profileForTest(t)
	if err := handle.StoreCredential(profile.Credential.Path, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	client, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(t.TempDir(), "pending")), roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2026-01-01T00:00:00Z","result":{"claim":{"claimId":"cccccccccccccccccccccccccccccccc","authorityId":"dddddddddddddddddddddddddddddddd","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","active":false}}}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	authority, _ := NewRemoteAuthority(client)
	if _, err := authority.Status(context.Background(), lease.Selector{ClaimID: strings.Repeat("c", 32)}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonAuthorityMismatch {
		t.Fatalf("nested status identity mismatch accepted: %v", err)
	}
}

func TestEnrollmentRejectsMetadataAuthorityMismatch(t *testing.T) {
	profile := profileForTest(t)
	calls := 0
	client, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(t.TempDir(), "pending")), roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/.well-known/worklease" {
			t.Fatalf("enrollment dispatched to mismatched authority: %s", r.URL.Path)
		}
		return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"cccccccccccccccccccccccccccccccc","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2026-01-01T00:00:00Z","result":{}}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Enroll(context.Background(), strings.Repeat("1", 64), "agent", config.ProfilePaths{Profiles: filepath.Join(t.TempDir(), "profiles.yaml")}); err == nil || calls != 1 {
		t.Fatalf("mismatched enrollment accepted: err=%v calls=%d", err, calls)
	}
}

func TestPendingStoreRequiresAbsolutePrivateRoot(t *testing.T) {
	if _, err := NewHTTPClient(profileForTest(t), NewFilePendingStore("."), roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("transport called")
		return nil, nil
	})); err == nil {
		t.Fatal("relative pending root accepted")
	}
}

func TestExpiredShortWindowDoesNotDispatch(t *testing.T) {
	profile := profileForTest(t)
	calls := 0
	client, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(t.TempDir(), "pending")), roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("must not dispatch")
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := client.Clock().Sample(now, now, now); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"requestNotAfter": now.Add(-time.Second)})
	_, err = client.Call(context.Background(), RequestSpec{Path: "/v1/admin/gc", Kind: "admin/gc", RequestID: strings.Repeat("f", 32), Body: body, Mutating: true})
	if err == nil || calls != 0 {
		t.Fatalf("expired request dispatched: err=%v calls=%d", err, calls)
	}
}

func TestAuthorityClockBoundariesAndInvalidation(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 10, 0, time.UTC)
	clock := NewAuthorityClock(func() time.Time { return now })
	start := now
	if err := clock.Sample(now, now, now); err != nil {
		t.Fatal(err)
	}
	now = start.Add(2900 * time.Millisecond)
	if !clock.DispatchAllowedFromSample(4 * time.Second) {
		t.Fatal("start response rejected before three quarters TTL")
	}
	now = start.Add(3100 * time.Millisecond)
	if clock.DispatchAllowedFromSample(4 * time.Second) {
		t.Fatal("start response allowed after three quarters TTL")
	}
	now = start.Add(5 * time.Second)
	if !clock.ShouldRenew(start, 10*time.Second) {
		t.Fatal("half-TTL renewal was not scheduled")
	}
	now = start.Add(7500 * time.Millisecond)
	if clock.CanStart(start, 10*time.Second) {
		t.Fatal("new work allowed at three quarters TTL")
	}
	now = start.Add(10 * time.Second)
	if clock.DispatchAllowed(start, 10*time.Second) {
		t.Fatal("late start response allowed at expiry")
	}
	clock.MarkRestartOrResume()
	if !clock.ResampleNeeded() || clock.CanStart(start, time.Hour) {
		t.Fatal("restart/resume did not require resampling")
	}
}

func TestRemoteTransferAcceptsSuccessorGrant(t *testing.T) {
	root := t.TempDir()
	profile := profileForTest(t)
	if err := handle.StoreCredential(profile.Credential.Path, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	claimCredential, successorCredential := filepath.Join(root, "claim-token"), filepath.Join(root, "successor-token")
	if err := handle.StoreCredential(claimCredential, strings.Repeat("8", 64)); err != nil {
		t.Fatal(err)
	}
	if err := handle.StoreCredential(successorCredential, strings.Repeat("7", 64)); err != nil {
		t.Fatal(err)
	}
	claimID, successorID, operationID := strings.Repeat("c", 32), strings.Repeat("d", 32), strings.Repeat("e", 32)
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/.well-known/worklease" {
			return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2026-01-01T00:00:00Z","result":{}}`), nil
		}
		if r.Header.Get("Worklease-Claim-Authorization") == "" || r.Header.Get("Worklease-New-Claim-Authorization") == "" {
			t.Fatal("transfer credentials missing")
		}
		return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2026-01-01T00:00:01Z","result":{"claimId":"dddddddddddddddddddddddddddddddd","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","receipt":{"operationId":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","claimId":"cccccccccccccccccccccccccccccccc","kind":"transfer","requestSha256":"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","revision":2,"committed":true}}}`), nil
	})
	store := NewFilePendingStore(filepath.Join(root, "pending"))
	client, err := NewHTTPClient(profile, store, transport)
	if err != nil {
		t.Fatal(err)
	}
	authority, _ := NewRemoteAuthority(client)
	_, err = authority.Transfer(context.Background(), lease.Credentials{ClaimID: claimID, Token: strings.Repeat("8", 64), Revision: 1, CredentialPath: claimCredential}, lease.TransferRequest{OperationID: operationID, SuccessorClaimID: successorID, SuccessorToken: strings.Repeat("7", 64), SuccessorCredentialPath: successorCredential, ToAgent: "next", ToSession: "session", TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if records, err := store.List(); err != nil || len(records) != 0 {
		t.Fatalf("transfer recovery not finalized: %d %v", len(records), err)
	}
}

func TestFreshEpochAcquirePersistsBeforeDispatchAndReplaysExactly(t *testing.T) {
	root := t.TempDir()
	profile := profileForTest(t)
	if err := handle.StoreCredential(profile.Credential.Path, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	handlePath := filepath.Join(root, "handles", "claim.json")
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(handlePath)); err != nil {
		t.Fatal(err)
	}
	old := handle.Handle{SchemaVersion: handle.RemoteSchemaVersion, AuthorityID: profile.AuthorityID, ClaimID: strings.Repeat("c", 32), Token: strings.Repeat("6", 64), Revision: 4, Resources: []string{"coordination:old"}, ExpiresAt: time.Now().Add(-time.Minute), AgentID: "old-agent", SessionID: "old-session", State: "ready"}
	if err := handle.Write(handlePath, old); err != nil {
		t.Fatal(err)
	}
	newID, newToken := strings.Repeat("d", 32), strings.Repeat("7", 64)
	deadline := time.Now().Add(time.Hour).UTC()
	first, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(root, "pending")), roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("lost response")
	}))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := first.Clock().Sample(now, now, now); err != nil {
		t.Fatal(err)
	}
	authority, _ := NewRemoteAuthority(first)
	request := lease.AcquireRequest{ClaimID: newID, Token: newToken, Resources: []string{"coordination:new-a", "coordination:new-b"}, AgentID: "new-agent", SessionID: "new-session", WorkKey: "new-work", TTL: 20 * time.Second, MaxHold: time.Hour, CoordinationOnly: true, RequestNotAfter: deadline, HandlePath: handlePath, PreviousClaimID: old.ClaimID, PreviousToken: old.Token, PreviousRevision: old.Revision, PreviousExpiresAt: old.ExpiresAt}
	if _, err := authority.Acquire(context.Background(), request); err == nil {
		t.Fatal("lost acquire response was not reported")
	}
	pending, err := handle.Read(handlePath)
	if err != nil {
		t.Fatal(err)
	}
	if pending.State != "pending" || pending.ClaimID != newID || pending.Token != newToken || pending.PendingRequest == nil || pending.PendingRequest.OperationID != newID || pending.PendingRequest.RequestNotAfter != deadline {
		t.Fatalf("fresh request was not durably staged: %#v", pending)
	}
	var saved map[string]any
	if err := json.Unmarshal(pending.PendingRequest.Request, &saved); err != nil || saved["workKey"] != "new-work" || saved["agentId"] != "new-agent" || saved["sessionId"] != "new-session" {
		t.Fatalf("fresh request bytes=%s err=%v", pending.PendingRequest.Request, err)
	}

	mismatched, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(root, "pending")), roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2099-01-01T00:00:00Z","result":{"claimId":"dddddddddddddddddddddddddddddddd","resources":["coordination:wrong"],"agentId":"new-agent","sessionId":"new-session","workKey":"new-work","revision":1,"expiresAt":"2099-01-01T00:01:00Z","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","receipt":{"operationId":"dddddddddddddddddddddddddddddddd","claimId":"dddddddddddddddddddddddddddddddd","kind":"acquire","requestSha256":"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","revision":1,"committed":true}}}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := mismatched.Clock().Sample(now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := mismatched.ReplayHandle(context.Background(), handlePath); err == nil {
		t.Fatal("mismatched grant activated")
	}
	stillPending, err := handle.Read(handlePath)
	if err != nil || stillPending.State != "pending" || stillPending.PendingRequest == nil || stillPending.ClaimID != newID {
		t.Fatalf("mismatched grant did not preserve pending request: %#v err=%v", stillPending, err)
	}

	second, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(root, "pending")), roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Worklease-New-Claim-Authorization") != "Bearer "+newToken {
			t.Fatal("replay did not use the staged new token")
		}
		return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2099-01-01T00:00:00Z","result":{"claimId":"dddddddddddddddddddddddddddddddd","resources":["coordination:new-a","coordination:new-b"],"agentId":"new-agent","sessionId":"new-session","workKey":"new-work","revision":1,"expiresAt":"2099-01-01T00:01:00Z","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","receipt":{"operationId":"dddddddddddddddddddddddddddddddd","claimId":"dddddddddddddddddddddddddddddddd","kind":"acquire","requestSha256":"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","revision":1,"committed":true}}}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Clock().Sample(now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := second.ReplayHandle(context.Background(), handlePath); err != nil {
		t.Fatal(err)
	}
	ready, err := handle.Read(handlePath)
	if err != nil || ready.State != "ready" || ready.ClaimID != newID || ready.PendingRequest != nil || strings.Join(ready.Resources, ",") != "coordination:new-a,coordination:new-b" {
		t.Fatalf("replayed fresh epoch was not activated: %#v err=%v", ready, err)
	}
}

func TestFreshEpochAcquireRejectsConcurrentHandleChange(t *testing.T) {
	root := t.TempDir()
	profile := profileForTest(t)
	if err := handle.StoreCredential(profile.Credential.Path, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	handlePath := filepath.Join(root, "handles", "claim.json")
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(handlePath)); err != nil {
		t.Fatal(err)
	}
	old := handle.Handle{SchemaVersion: handle.RemoteSchemaVersion, AuthorityID: profile.AuthorityID, ClaimID: strings.Repeat("c", 32), Token: strings.Repeat("6", 64), Revision: 4, Resources: []string{"coordination:old"}, ExpiresAt: time.Now().Add(-time.Minute), AgentID: "old-agent", SessionID: "old-session", State: "ready"}
	changed := old
	changed.Revision++
	if err := handle.Write(handlePath, changed); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(root, "pending")), roundTripFunc(func(*http.Request) (*http.Response, error) {
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
	authority, _ := NewRemoteAuthority(client)
	_, err = authority.Acquire(context.Background(), lease.AcquireRequest{ClaimID: strings.Repeat("d", 32), Token: strings.Repeat("7", 64), Resources: []string{"coordination:new"}, AgentID: "new", SessionID: "new", WorkKey: "new", TTL: time.Minute, MaxHold: time.Hour, RequestNotAfter: time.Now().Add(time.Hour), HandlePath: handlePath, PreviousClaimID: old.ClaimID, PreviousToken: old.Token, PreviousRevision: old.Revision, PreviousExpiresAt: old.ExpiresAt})
	if err == nil || calls != 0 {
		t.Fatalf("concurrent handle change was overwritten: err=%v calls=%d", err, calls)
	}
	after, readErr := handle.Read(handlePath)
	if readErr != nil || after.Revision != changed.Revision || after.ClaimID != old.ClaimID || after.PendingRequest != nil {
		t.Fatalf("changed handle was mutated: %#v err=%v", after, readErr)
	}
}

func TestNamedTransferReplaysSuccessorCredential(t *testing.T) {
	root := t.TempDir()
	profile := profileForTest(t)
	if err := handle.StoreCredential(profile.Credential.Path, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	predecessorPath, successorPath := filepath.Join(root, "handles", "old.json"), filepath.Join(root, "handles", "new.json")
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(predecessorPath)); err != nil {
		t.Fatal(err)
	}
	claimID, successorID, operationID := strings.Repeat("c", 32), strings.Repeat("d", 32), strings.Repeat("e", 32)
	old := handle.Handle{SchemaVersion: handle.RemoteSchemaVersion, AuthorityID: profile.AuthorityID, ClaimID: claimID, Token: strings.Repeat("8", 64), Revision: 1, Resources: []string{"coordination:test"}, ExpiresAt: time.Now().Add(time.Hour), AgentID: "old", SessionID: "session", State: "ready"}
	if err := handle.Write(predecessorPath, old); err != nil {
		t.Fatal(err)
	}
	metadata := `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2026-01-01T00:00:00Z","result":{}}`
	first, _ := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(root, "pending")), roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/.well-known/worklease" {
			return response(200, metadata), nil
		}
		return nil, errors.New("lost response")
	}))
	authority, _ := NewRemoteAuthority(first)
	_, err := authority.Transfer(context.Background(), lease.Credentials{ClaimID: claimID, Token: old.Token, Revision: 1, HandlePath: predecessorPath}, lease.TransferRequest{OperationID: operationID, SuccessorClaimID: successorID, SuccessorToken: strings.Repeat("7", 64), SuccessorHandlePath: successorPath, ToAgent: "new", ToSession: "session", TTL: time.Minute})
	if err == nil {
		t.Fatal("lost transfer response was not uncertain")
	}
	fresh, _ := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(root, "pending")), roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/.well-known/worklease" {
			return response(200, metadata), nil
		}
		if r.Header.Get("Worklease-New-Claim-Authorization") != "Bearer "+strings.Repeat("7", 64) {
			t.Fatal("successor credential was not retained for replay")
		}
		return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2026-01-01T00:00:01Z","result":{"claimId":"dddddddddddddddddddddddddddddddd","resources":["coordination:test"],"agentId":"new","sessionId":"session","revision":1,"expiresAt":"2026-01-01T00:01:00Z","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","receipt":{"operationId":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","claimId":"cccccccccccccccccccccccccccccccc","kind":"transfer","requestSha256":"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","revision":2,"committed":true}}}`), nil
	}))
	if _, err := fresh.ReplayHandle(context.Background(), predecessorPath); err != nil {
		t.Fatal(err)
	}
	newHandle, err := handle.Read(successorPath)
	if err != nil || newHandle.ClaimID != successorID || newHandle.Token != strings.Repeat("7", 64) {
		t.Fatalf("successor handle not activated: %#v %v", newHandle, err)
	}
	if _, err := handle.Read(predecessorPath); err == nil {
		t.Fatal("ended predecessor handle retained")
	}
}

func TestNamedBeginPersistsBeforeDispatch(t *testing.T) {
	root := t.TempDir()
	profile := profileForTest(t)
	if err := handle.StoreCredential(profile.Credential.Path, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	handlePath := filepath.Join(root, "handles", "claim.json")
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(handlePath)); err != nil {
		t.Fatal(err)
	}
	claimID := strings.Repeat("c", 32)
	operationID := strings.Repeat("d", 32)
	requestHash := strings.Repeat("e", 64)
	h := handle.Handle{SchemaVersion: handle.SchemaVersion, AuthorityID: profile.AuthorityID, ClaimID: claimID, Token: strings.Repeat("8", 64), Revision: 1, Resources: []string{"coordination:test"}, ExpiresAt: time.Now().Add(time.Hour), AgentID: "agent", SessionID: "session", State: "ready"}
	if err := handle.Write(handlePath, h); err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path == "/.well-known/worklease" {
			return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2026-01-01T00:00:00Z","result":{}}`), nil
		}
		if r.URL.Path != "/v1/operations/begin" {
			t.Fatalf("unexpected route %s", r.URL.Path)
		}
		body, _ := json.Marshal(map[string]any{"operationId": operationID, "claimId": claimID, "kind": "exec", "revision": 2, "requestSha256": requestHash, "completed": false})
		return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2026-01-01T00:00:01Z","result":`+string(body)+`}`), nil
	})
	client, err := NewHTTPClient(profile, NewFilePendingStore(filepath.Join(root, "pending")), transport)
	if err != nil {
		t.Fatal(err)
	}
	authority, _ := NewRemoteAuthority(client)
	_, err = authority.BeginOperation(context.Background(), lease.Credentials{ClaimID: claimID, Token: h.Token, Revision: 1, HandlePath: handlePath}, lease.OperationIntent{OperationID: operationID, Kind: "exec", Request: map[string]any{"argv": []string{"true"}}, RequestHash: requestHash, TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := handle.Read(handlePath)
	if err != nil || stored.PendingRequest == nil || stored.PendingRequest.Kind != "exec" || calls != 2 {
		t.Fatalf("named begin was not durably dispatched: handle=%#v err=%v calls=%d", stored, err, calls)
	}
}
