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
