package authority

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
)

func TestReplayMismatchedCompletionRetainsParentAndRequest(t *testing.T) {
	root := t.TempDir()
	profile := profileForTest(t)
	if err := handle.StoreCredential(profile.Credential.Path, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	claimCredential := filepath.Join(root, "claim-token")
	if err := handle.StoreCredential(claimCredential, strings.Repeat("8", 64)); err != nil {
		t.Fatal(err)
	}
	store := NewFilePendingStore(filepath.Join(root, "pending"))
	deadline := time.Now().Add(time.Hour).UTC()
	claimID, targetID, completionID := strings.Repeat("c", 32), strings.Repeat("d", 32), strings.Repeat("e", 32)
	parentBody := []byte(`{"requestNotAfter":"2099-01-01T00:00:00Z"}`)
	if err := store.Save(pendingForTest(targetID, "operations/begin", "/v1/operations/begin", parentBody, profile)); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"protocolVersion": protocolVersion, "authorityId": profile.AuthorityID, "expectedRestoreId": profile.RestoreID, "claimId": claimID, "revision": 1, "operationId": targetID, "requestNotAfter": deadline, "receipt": map[string]any{"exitCode": 0}})
	pending := pendingForTest(completionID, "operations/complete", "/v1/operations/complete", body, profile)
	pending.TargetOperationID, pending.ClaimCredentialRef = targetID, claimCredential
	if err := store.Save(pending); err != nil {
		t.Fatal(err)
	}
	client, err := NewHTTPClient(profile, store, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2026-01-01T00:00:00Z","result":{}}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Clock().Sample(time.Now(), time.Now().Add(-time.Millisecond), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Replay(context.Background(), completionID); err == nil {
		t.Fatal("mismatched completion replay accepted")
	}
	if _, err := store.Load(completionID); err != nil {
		t.Fatal("completion evidence cleared")
	}
	if _, err := store.Load(targetID); err != nil {
		t.Fatal("parent effect evidence cleared")
	}
}

func pendingForTest(id, kind, route string, body []byte, profile config.Profile) PendingRequest {
	p := PendingRequest{RequestID: id, OperationID: id, Kind: kind, Route: route, AuthorityID: profile.AuthorityID, Endpoint: profile.Endpoint, CertificateSHA256: profile.CertificateSHA256, ExpectedRestoreID: profile.RestoreID, RequestNotAfter: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), Request: body, State: "pending"}
	p.RequestSHA256 = requestSHA(body)
	return p
}

func requestSHA(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func TestLegacyPendingMutationTrustFailsBeforeDispatch(t *testing.T) {
	root := t.TempDir()
	profile := config.Profile{Name: "team", Endpoint: "https://new.example", AuthorityID: strings.Repeat("a", 32), RestoreID: strings.Repeat("b", 32), Credential: config.CredentialDescriptor{Path: filepath.Join(root, "credential")}}
	requestID := strings.Repeat("c", 32)
	body := []byte(`{"requestNotAfter":"2099-01-01T00:00:00Z"}`)
	store := NewFilePendingStore(filepath.Join(root, "pending"))
	if err := store.Save(pendingForTest(requestID, "invite-issue", "/v1/admin/invites/issue", body, config.Profile{AuthorityID: profile.AuthorityID, RestoreID: profile.RestoreID})); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client, err := NewHTTPClient(profile, store, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Replay(context.Background(), requestID); err == nil || calls != 0 {
		t.Fatalf("legacy unbound replay err=%v calls=%d", err, calls)
	}
}

func TestPendingMutationIdentityMismatchFailsBeforeDispatch(t *testing.T) {
	root := t.TempDir()
	requestID := strings.Repeat("e", 32)
	body := []byte(`{"requestNotAfter":"2099-01-01T00:00:00Z"}`)
	store := NewFilePendingStore(filepath.Join(root, "pending"))
	original := config.Profile{Endpoint: "https://same.example", AuthorityID: strings.Repeat("a", 32), RestoreID: strings.Repeat("b", 32)}
	if err := store.Save(pendingForTest(requestID, "invite-issue", "/v1/admin/invites/issue", body, original)); err != nil {
		t.Fatal(err)
	}
	profile := config.Profile{Name: "changed", Endpoint: original.Endpoint, AuthorityID: strings.Repeat("c", 32), RestoreID: strings.Repeat("d", 32), Credential: config.CredentialDescriptor{Path: filepath.Join(root, "credential")}}
	calls := 0
	client, err := NewHTTPClient(profile, store, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Replay(context.Background(), requestID); err == nil || calls != 0 {
		t.Fatalf("identity-mismatched replay err=%v calls=%d", err, calls)
	}
}

func TestLegacyEnrollmentTrustUpgradePersistsBeforeLostResponse(t *testing.T) {
	root := t.TempDir()
	profile := config.Profile{Name: "team", Endpoint: "https://authority.example", AuthorityID: strings.Repeat("a", 32), Credential: config.CredentialDescriptor{Path: filepath.Join(root, "credential")}}
	store := NewFilePendingStore(filepath.Join(root, "pending"))
	requestID := strings.Repeat("d", 32)
	body := []byte(`{"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expectedRestoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","requestId":"dddddddddddddddddddddddddddddddd","requestNotAfter":"2099-01-01T00:00:00Z","installationId":"dddddddddddddddddddddddddddddddd","label":"agent"}`)
	pending := pendingForTest(requestID, "enroll", "/v1/enroll", body, config.Profile{AuthorityID: profile.AuthorityID, RestoreID: strings.Repeat("b", 32)})
	pending.CredentialRef = profile.Credential.Path
	if err := store.Save(pending); err != nil {
		t.Fatal(err)
	}
	if err := handle.StoreCredential(profile.Credential.Path, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	client, err := NewHTTPClient(profile, store, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/.well-known/worklease" {
			return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"2026-01-01T00:00:00Z","result":{"supportedProtocolVersions":["worklease-http/1"]}}`), nil
		}
		return nil, errors.New("connection reset")
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.ReplayEnrollment(context.Background(), requestID, strings.Repeat("1", 64), config.ProfilePaths{Profiles: filepath.Join(root, "profiles.yaml")}); err == nil {
		t.Fatal("lost enrollment response unexpectedly succeeded")
	}
	upgraded, err := store.Load(requestID)
	if err != nil || upgraded.Endpoint != profile.Endpoint {
		t.Fatalf("upgraded pending=%+v err=%v", upgraded, err)
	}
	calls := 0
	profile.Endpoint = "https://changed.example"
	changed, err := NewHTTPClient(profile, store, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := changed.ReplayEnrollment(context.Background(), requestID, strings.Repeat("1", 64), config.ProfilePaths{Profiles: filepath.Join(root, "profiles.yaml")}); err == nil || calls != 0 {
		t.Fatalf("changed trust replay err=%v calls=%d", err, calls)
	}
}

func TestReplayEnrollmentAfterRestartActivatesProfile(t *testing.T) {
	root := t.TempDir()
	profile := config.Profile{Name: "new", Endpoint: "https://authority.example", AuthorityID: strings.Repeat("a", 32), Credential: config.CredentialDescriptor{Path: filepath.Join(root, "credential")}}
	credential := strings.Repeat("9", 64)
	if err := handle.StoreCredential(profile.Credential.Path, credential); err != nil {
		t.Fatal(err)
	}
	store := NewFilePendingStore(filepath.Join(root, "pending"))
	requestID := strings.Repeat("d", 32)
	body := []byte(`{"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expectedRestoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","requestId":"dddddddddddddddddddddddddddddddd","requestNotAfter":"2099-01-01T00:00:00Z","installationId":"dddddddddddddddddddddddddddddddd","label":"agent"}`)
	// A pre-artifact pending enrollment has no transport binding. Supplying the
	// invite again explicitly upgrades it to the currently trusted profile
	// before replay.
	pending := pendingForTest(requestID, "enroll", "/v1/enroll", body, config.Profile{AuthorityID: profile.AuthorityID, RestoreID: strings.Repeat("b", 32)})
	pending.CredentialRef = profile.Credential.Path
	if err := store.Save(pending); err != nil {
		t.Fatal(err)
	}
	calls := 0
	client, err := NewHTTPClient(profile, store, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path == "/.well-known/worklease" {
			authorityTime := "2026-01-01T00:00:00Z"
			if calls > 2 {
				authorityTime = "2026-01-01T00:00:02Z"
			}
			return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"`+authorityTime+`","result":{}}`), nil
		}
		if r.Header.Get("Worklease-New-Installation-Authorization") != "Bearer "+credential {
			t.Fatal("durable enrollment credential not replayed")
		}
		authorityTime := "2026-01-01T00:00:01Z"
		if calls > 2 {
			authorityTime = "2026-01-01T00:00:03Z"
		}
		return response(200, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","restoreId":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","authorityTime":"`+authorityTime+`","result":{"installationId":"dddddddddddddddddddddddddddddddd","role":"write","label":"agent","enrolledAt":"2026-01-01T00:00:01Z"}}`), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	blockedParent := filepath.Join(root, "blocked")
	if err := os.WriteFile(blockedParent, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	badPaths := config.ProfilePaths{Profiles: filepath.Join(blockedParent, "profiles.yaml")}
	if _, _, err := client.ReplayEnrollment(context.Background(), requestID, strings.Repeat("1", 64), badPaths); err == nil {
		t.Fatal("profile save failure unexpectedly completed enrollment")
	}
	if _, err := store.Load(requestID); err != nil {
		t.Fatalf("profile save failure lost pending enrollment: %v", err)
	}
	if _, err := handle.ReadCredential(profile.Credential.Path); err != nil {
		t.Fatalf("profile save failure lost replay credential: %v", err)
	}
	paths := config.ProfilePaths{Profiles: filepath.Join(root, "profiles.yaml")}
	out, _, err := client.ReplayEnrollment(context.Background(), requestID, strings.Repeat("1", 64), paths)
	if err != nil || out.RestoreID != strings.Repeat("b", 32) || calls != 4 {
		t.Fatalf("enrollment recovery failed: out=%#v err=%v calls=%d", out, err, calls)
	}
	if _, err := store.Load(requestID); err == nil {
		t.Fatal("completed enrollment pending record retained")
	}
}
