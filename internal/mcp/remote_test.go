package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/authority"
	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/server"
	"github.com/brettinternet/worklease/internal/store"
)

func remoteMCPFixture(t *testing.T, enroll bool) (config.Profile, string, *authority.HTTPClient, string) {
	t.Helper()
	ctx := context.Background()
	serverHome := t.TempDir()
	if err := store.MarkHosted(serverHome); err != nil {
		t.Fatal(err)
	}
	lock, err := store.AcquireHostedLock(ctx, serverHome)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, serverHome, store.Options{HostedWriter: true, HostedLock: lock})
	if err != nil {
		t.Fatal(err)
	}
	invite := strings.Repeat("a", 64)
	bootstrap, err := lease.New(st, nil, nil, lease.Defaults{}).HostedInitialize(ctx, invite)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.WriteHostedReady(lock); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	srv, err := server.New(ctx, server.Config{Home: serverHome, Listen: "127.0.0.1:8443", Prefixes: []string{"coordination:"}, MaxTTL: "1m", MaxHold: "1h", HealthRate: 100, MetadataRate: 100, EnrollmentRate: 100}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })

	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	profile := config.Profile{Name: "team", Endpoint: httpServer.URL, AuthorityID: bootstrap.AuthorityID, RestoreID: bootstrap.RestoreID, AllowInsecureHTTP: true, Credential: config.CredentialDescriptor{Path: filepath.Join(configRoot, "worklease", "credentials", "team")}}
	var client *authority.HTTPClient
	var installationID string
	if enroll {
		client, err = authority.NewHTTPClient(profile, authority.NewFilePendingStore(filepath.Join(configRoot, "enroll-pending")), nil)
		if err != nil {
			t.Fatal(err)
		}
		_, installation, err := client.Enroll(ctx, invite, "mcp-test", config.UserProfilePaths(os.Getenv))
		if err != nil {
			t.Fatal(err)
		}
		installationID = installation.InstallationID
	}
	return profile, t.TempDir(), client, installationID
}

func mcpFields(t *testing.T, result map[string]any) map[string]any {
	t.Helper()
	fields, ok := result["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("missing structured content: %#v", result)
	}
	return fields
}

func TestRemoteMCPRoutesExistingAuthorityTools(t *testing.T) {
	profile, home, _, _ := remoteMCPFixture(t, true)
	s, err := NewServer(Options{Home: home, AgentID: "mcp-agent", SessionID: "mcp-session", TTL: 30 * time.Second, PollInterval: time.Millisecond, Profile: &profile, ProfileName: profile.Name})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)

	if got := len(s.tools()); got != 11 {
		t.Fatalf("tool count=%d want 11", got)
	}
	acquire, err := s.Call(context.Background(), "acquire", map[string]any{"resources": []string{"coordination:mcp"}, "ttl": 30.0, "maxHold": 60.0, "autoHeartbeat": false})
	if err != nil {
		t.Fatal(err)
	}
	acquireFields := mcpFields(t, acquire)
	if ok, _ := acquireFields["ok"].(bool); !ok {
		t.Fatalf("acquire failed: %#v", acquireFields)
	}
	leaseRef, _ := acquireFields["lease"].(string)
	if leaseRef == "" {
		t.Fatalf("missing lease reference: %#v", acquireFields)
	}
	if _, err := os.Stat(filepath.Join(home, store.DatabaseFileName)); !os.IsNotExist(err) {
		t.Fatalf("remote MCP opened local authority: %v", err)
	}
	localKeyAcquire, err := s.Call(context.Background(), "acquire", map[string]any{"path": "go.mod", "maxHold": 60.0, "autoHeartbeat": false})
	if err != nil {
		t.Fatal(err)
	}
	localKeyError, _ := mcpFields(t, localKeyAcquire)["error"].(map[string]any)
	if localKeyError["reason"] != "resource-not-enrolled" {
		t.Fatalf("remote host-local acquire=%#v", mcpFields(t, localKeyAcquire))
	}

	calls := []struct {
		name string
		args map[string]any
	}{
		{"status", map[string]any{"lease": leaseRef}},
		{"list", map[string]any{}},
		{"events", map[string]any{}},
		{"watch", map[string]any{"resources": []string{"coordination:free"}, "until": "free", "timeout": 1.0}},
		{"verify", map[string]any{"lease": leaseRef}},
		{"checkpoint", map[string]any{"lease": leaseRef, "data": map[string]any{"phase": "test"}, "ttl": 30.0}},
		{"heartbeat", map[string]any{"lease": leaseRef, "ttl": 30.0}},
	}
	for _, call := range calls {
		result, err := s.Call(context.Background(), call.name, call.args)
		if err != nil {
			t.Fatalf("%s: %v", call.name, err)
		}
		if fields := mcpFields(t, result); fields["ok"] != true {
			t.Fatalf("%s failed: %#v", call.name, fields)
		}
	}
	released, err := s.Call(context.Background(), "release", map[string]any{"lease": leaseRef, "reason": "done"})
	if err != nil || mcpFields(t, released)["ok"] != true {
		t.Fatalf("release failed: err=%v result=%#v", err, released)
	}
}

func TestRemoteMCPDefinitiveMutationFailureClearsPendingRequest(t *testing.T) {
	profile, home, _, _ := remoteMCPFixture(t, true)
	s, err := NewServer(Options{Home: home, AgentID: "expiry-agent", SessionID: "expiry-session", TTL: time.Second, Profile: &profile, ProfileName: profile.Name})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Call(context.Background(), "acquire", map[string]any{"resources": []string{"coordination:expiry"}, "ttl": 1.0, "maxHold": 60.0, "autoHeartbeat": false})
	if err != nil {
		t.Fatal(err)
	}
	leaseRef := mcpFields(t, result)["lease"].(string)
	time.Sleep(1100 * time.Millisecond)
	result, err = s.Call(context.Background(), "heartbeat", map[string]any{"lease": leaseRef, "ttl": 1.0})
	if err != nil {
		t.Fatal(err)
	}
	heartbeatError := mcpFields(t, result)["error"].(map[string]any)
	if heartbeatError["reason"] != "claim-expired" {
		t.Fatalf("heartbeat=%#v", mcpFields(t, result))
	}
	h, err := handle.Read(s.handlePath(leaseRef))
	if err != nil {
		t.Fatal(err)
	}
	if h.PendingRequest != nil {
		t.Fatalf("definitive failure retained pending request: %#v", h.PendingRequest)
	}
	result, err = s.Call(context.Background(), "release", map[string]any{"lease": leaseRef, "reason": "cleanup"})
	if err != nil {
		t.Fatal(err)
	}
	releaseError := mcpFields(t, result)["error"].(map[string]any)
	if releaseError["reason"] == "operation-request-mismatch" {
		t.Fatalf("release was blocked by stale pending request: %#v", mcpFields(t, result))
	}
}

func TestRemoteMCPMissingCredentialPreflightsMutation(t *testing.T) {
	profile, home, _, _ := remoteMCPFixture(t, true)
	s, err := NewServer(Options{Home: home, AgentID: "auth-agent", SessionID: "auth-session", TTL: 30 * time.Second, Profile: &profile, ProfileName: profile.Name})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Call(context.Background(), "acquire", map[string]any{"resources": []string{"coordination:auth"}, "ttl": 30.0, "maxHold": 60.0, "autoHeartbeat": false})
	if err != nil {
		t.Fatal(err)
	}
	leaseRef := mcpFields(t, result)["lease"].(string)
	if err := os.Rename(profile.Credential.Path, profile.Credential.Path+".saved"); err != nil {
		t.Fatal(err)
	}
	result, err = s.Call(context.Background(), "heartbeat", map[string]any{"lease": leaseRef, "ttl": 30.0})
	if err != nil {
		t.Fatal(err)
	}
	fields := mcpFields(t, result)
	errorFields := fields["error"].(map[string]any)
	if errorFields["reason"] != "authentication-required" {
		t.Fatalf("heartbeat=%#v", fields)
	}
	h, err := handle.Read(s.handlePath(leaseRef))
	if err != nil {
		t.Fatal(err)
	}
	if h.PendingRequest != nil {
		t.Fatalf("credential preflight persisted a request: %#v", h.PendingRequest)
	}
}

func TestRemoteMCPAutomaticRenewalUsesRemoteAuthority(t *testing.T) {
	profile, home, _, _ := remoteMCPFixture(t, true)
	s, err := NewServer(Options{Home: home, AgentID: "renew-agent", SessionID: "renew-session", TTL: time.Second, Profile: &profile, ProfileName: profile.Name})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	result, err := s.Call(context.Background(), "acquire", map[string]any{"resources": []string{"coordination:renew"}, "ttl": 1.0, "maxHold": 60.0})
	if err != nil {
		t.Fatal(err)
	}
	leaseRef, _ := mcpFields(t, result)["lease"].(string)
	path := s.handlePath(leaseRef)
	deadline := time.Now().Add(3 * time.Second)
	for {
		h, readErr := handle.Read(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if h.Revision > 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("remote automatic renewal did not advance revision: %#v", h)
		}
		time.Sleep(25 * time.Millisecond)
	}
	result, err = s.Call(context.Background(), "release", map[string]any{"lease": leaseRef, "reason": "done"})
	if err != nil || mcpFields(t, result)["ok"] != true {
		t.Fatalf("release failed: err=%v result=%#v", err, result)
	}
}

func TestRemoteMCPLocalToolsDoNotContactAuthority(t *testing.T) {
	home := t.TempDir()
	profile := config.Profile{Name: "offline", Endpoint: "http://127.0.0.1:1", AuthorityID: strings.Repeat("a", 32), RestoreID: strings.Repeat("b", 32), AllowInsecureHTTP: true, Credential: config.CredentialDescriptor{Path: filepath.Join(home, "credential")}}
	s, err := NewServer(Options{Home: home, Profile: &profile, ProfileName: profile.Name})
	if err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string]map[string]any{
		"key":          {"provider": "generic", "source": "source", "item": "item"},
		"instructions": {"topic": "loop"},
	} {
		result, err := s.Call(context.Background(), name, args)
		if err != nil || mcpFields(t, result)["ok"] != true {
			t.Fatalf("%s made remote authority mandatory: err=%v result=%#v", name, err, result)
		}
	}
}

func remoteAdminCall(t *testing.T, client *authority.HTTPClient, profile config.Profile, path, kind, operationID string, fields map[string]any) {
	t.Helper()
	if _, err := client.Metadata(context.Background()); err != nil {
		t.Fatal(err)
	}
	fields["protocolVersion"], fields["authorityId"], fields["expectedRestoreId"] = "worklease-http/1", profile.AuthorityID, profile.RestoreID
	fields["operationId"], fields["requestNotAfter"] = operationID, time.Now().UTC().Add(time.Hour)
	request, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := authority.NewRemoteAuthority(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Execute(context.Background(), authority.RequestSpec{Path: path, Kind: kind, RequestID: operationID, Body: request, Mutating: true, Terminal: true}); err != nil {
		t.Fatalf("%s: %#v", kind, err)
	}
}

func TestRemoteMCPRevokedInstallationGuidanceNamesProfile(t *testing.T) {
	adminProfile, home, adminClient, _ := remoteMCPFixture(t, true)
	invite := strings.Repeat("b", 64)
	digest := sha256.Sum256([]byte(invite))
	remoteAdminCall(t, adminClient, adminProfile, "/v1/admin/invites/issue", "invite-issue", strings.Repeat("c", 32), map[string]any{"inviteId": strings.Repeat("d", 32), "role": "write", "label": "revoked-test", "expiresAt": "", "inviteSha256": hex.EncodeToString(digest[:])})
	workerProfile := adminProfile
	workerProfile.Name = "worker"
	workerProfile.Credential.Path = filepath.Join(filepath.Dir(adminProfile.Credential.Path), "worker")
	workerClient, err := authority.NewHTTPClient(workerProfile, authority.NewFilePendingStore(filepath.Join(home, "worker-enroll-pending")), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, installation, err := workerClient.Enroll(context.Background(), invite, "worker", config.UserProfilePaths(os.Getenv))
	if err != nil {
		t.Fatal(err)
	}
	remoteAdminCall(t, adminClient, adminProfile, "/v1/admin/installations/revoke", "installation-revoke", strings.Repeat("e", 32), map[string]any{"installationId": installation.InstallationID, "reason": "test"})
	s, err := NewServer(Options{Home: home, Profile: &workerProfile, ProfileName: workerProfile.Name})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Call(context.Background(), "list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	fields := mcpFields(t, result)
	errorFields, _ := fields["error"].(map[string]any)
	details, _ := errorFields["details"].(map[string]any)
	if errorFields["reason"] != "installation-revoked" || !strings.Contains(details["action"].(string), "--profile worker") {
		t.Fatalf("guidance=%#v", fields)
	}
}

func TestRemoteMCPAuthenticationGuidanceNamesProfile(t *testing.T) {
	profile, home, _, _ := remoteMCPFixture(t, false)
	s, err := NewServer(Options{Home: home, Profile: &profile, ProfileName: profile.Name})
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Call(context.Background(), "list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	fields := mcpFields(t, result)
	errorFields, _ := fields["error"].(map[string]any)
	if errorFields["reason"] != "authentication-required" || !strings.Contains(errorFields["details"].(map[string]any)["action"].(string), "--profile team") {
		t.Fatalf("guidance=%#v", fields)
	}
}
