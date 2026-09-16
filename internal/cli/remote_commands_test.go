package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/authority"
	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/server"
	"github.com/brettinternet/worklease/internal/store"
	urfave "github.com/urfave/cli/v3"
)

func remoteCLIFixture(t *testing.T) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	home := t.TempDir()
	if err := store.MarkHosted(home); err != nil {
		t.Fatal(err)
	}
	lock, err := store.AcquireHostedLock(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, home, store.Options{HostedWriter: true, HostedLock: lock})
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
	srv, err := server.New(ctx, server.Config{Home: home, Listen: "127.0.0.1:8443", Prefixes: []string{"coordination:"}, MaxTTL: "1m", MaxHold: "1h", HealthRate: 100, MetadataRate: 100, EnrollmentRate: 100}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })

	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	paths := config.UserProfilePaths(os.Getenv)
	profile := config.Profile{Name: "team", Endpoint: httpServer.URL, AuthorityID: bootstrap.AuthorityID, RestoreID: bootstrap.RestoreID, AllowInsecureHTTP: true, Credential: config.CredentialDescriptor{Path: filepath.Join(configRoot, "worklease", "credentials", "team")}}
	client, err := authority.NewHTTPClient(profile, authority.NewFilePendingStore(filepath.Join(configRoot, "pending")), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Enroll(ctx, invite, "cli-test", paths); err != nil {
		t.Fatal(err)
	}
	return "team", t.TempDir(), filepath.Join(t.TempDir(), "claim.json")
}

func runRemoteCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, stderr bytes.Buffer
	err := Run(context.Background(), append([]string{"worklease"}, args...), "test", "unknown", "unknown", &out, &stderr)
	return out.String(), err
}

func TestInviteFromCommandUsesHiddenPromptWithoutInviteOption(t *testing.T) {
	original := readHiddenInvite
	readHiddenInvite = func() (string, error) { return strings.Repeat("a", 64), nil }
	t.Cleanup(func() { readHiddenInvite = original })
	command := &urfave.Command{}
	invite, err := inviteFromCommand(command)
	if err != nil || invite != strings.Repeat("a", 64) {
		t.Fatalf("hidden invite=%q err=%v", invite, err)
	}
}

func TestInviteFromCommandParsesHiddenArtifact(t *testing.T) {
	original := readHiddenInvite
	artifact, err := authority.EncodeInviteArtifact(authority.InviteArtifact{Endpoint: "https://authority.example", AuthorityID: strings.Repeat("a", 32), ProfileHint: "team", Invite: strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	readHiddenInvite = func() (string, error) { return artifact, nil }
	t.Cleanup(func() { readHiddenInvite = original })
	invite, err := inviteFromCommand(&urfave.Command{})
	if err != nil || invite != strings.Repeat("b", 64) {
		t.Fatalf("hidden artifact invite=%q err=%v", invite, err)
	}
}

func TestArtifactEnrollmentRejectsEstablishedProfileCollision(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	paths := config.UserProfilePaths(os.Getenv)
	credentialPath := filepath.Join(configRoot, "worklease", "credentials", "team")
	existing := config.Profile{Name: "team", Endpoint: "https://established.example", AuthorityID: strings.Repeat("a", 32), Credential: config.CredentialDescriptor{Path: credentialPath}}
	if err := config.SaveProfiles(paths, []config.Profile{existing}, ""); err != nil {
		t.Fatal(err)
	}
	artifact, err := authority.EncodeInviteArtifact(authority.InviteArtifact{Endpoint: "https://replacement.example", AuthorityID: existing.AuthorityID, ProfileHint: "team", Invite: strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	artifactRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	artifactRoot = filepath.Join(artifactRoot, "secrets")
	if err := handle.EnsureOwnerPrivateDir(artifactRoot); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(artifactRoot, "invite")
	if err := handle.WriteOwnerPrivateNoReplace(artifactPath, []byte(artifact+"\n"), authority.MaxInviteArtifactBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := runRemoteCLI(t, "enroll", "--invite-file", artifactPath, "--json"); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonAuthorityMismatch {
		t.Fatalf("profile collision error=%v", err)
	}
	if _, err := os.Stat(credentialPath); !os.IsNotExist(err) {
		t.Fatalf("profile collision created credential: %v", err)
	}
}

func TestRemoteCLIRoutesLifecycleWithoutOpeningLocalAuthority(t *testing.T) {
	profile, clientHome, claimHandle := remoteCLIFixture(t)
	out, err := runRemoteCLI(t, "acquire", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--resource", "coordination:test", "--ttl", "30s", "--json")
	if err != nil {
		t.Fatalf("acquire: %v output=%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(clientHome, store.DatabaseFileName)); !os.IsNotExist(err) {
		t.Fatalf("remote CLI opened local authority: %v", err)
	}
	out, err = runRemoteCLI(t, "status", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--json")
	if err != nil || !strings.Contains(out, "coordination:test") {
		t.Fatalf("status: %v output=%s", err, out)
	}
	for name, args := range map[string][]string{
		"list":       {"list", "--profile", profile, "--home", clientHome, "--json"},
		"events":     {"events", "--profile", profile, "--home", clientHome, "--json"},
		"history":    {"history", "--profile", profile, "--home", clientHome, "--resource", "coordination:test", "--json"},
		"watch":      {"watch", "--profile", profile, "--home", clientHome, "--resource", "coordination:free", "--until", "free", "--timeout", "1s", "--json"},
		"verify":     {"verify", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--json"},
		"checkpoint": {"checkpoint", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--ttl", "30s", "--data", `{"phase":"test"}`, "--json"},
		"heartbeat":  {"heartbeat", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--ttl", "30s", "--json"},
	} {
		out, err = runRemoteCLI(t, args...)
		if err != nil {
			t.Fatalf("%s: %v output=%s", name, err, out)
		}
	}
	successor := filepath.Join(filepath.Dir(claimHandle), "successor.json")
	out, err = runRemoteCLI(t, "transfer", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--successor-handle", successor, "--to-agent", "next", "--to-session", "next-session", "--ttl", "30s", "--json")
	if err != nil {
		t.Fatalf("transfer: %v output=%s", err, out)
	}
	claimHandle = successor
	out, err = runRemoteCLI(t, "exec", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--ttl", "30s", "--json", "--", "true")
	if err != nil {
		t.Fatalf("exec: %v output=%s", err, out)
	}
	out, err = runRemoteCLI(t, "release", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--json")
	if err != nil {
		t.Fatalf("release: %v output=%s", err, out)
	}
}

func TestRemoteCLIExplicitLifecycleAndGuardRetainCredentialPath(t *testing.T) {
	profile, clientHome, claimHandle := remoteCLIFixture(t)
	out, err := runRemoteCLI(t, "acquire", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--resource", "coordination:explicit", "--ttl", "30s", "--json")
	if err != nil {
		t.Fatalf("acquire: %v output=%s", err, out)
	}
	stored, err := handle.Read(claimHandle)
	if err != nil {
		t.Fatal(err)
	}
	tokenRoot := filepath.Join(clientHome, "explicit-credentials")
	if err := os.Mkdir(tokenRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	tokenPath := filepath.Join(tokenRoot, "claim.token")
	if err := os.WriteFile(tokenPath, []byte(stored.Token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = runRemoteCLI(t, "heartbeat", "--profile", profile, "--home", clientHome, "--claim-id", stored.ClaimID, "--token-file", tokenPath, "--revision", "1", "--operation-id", strings.Repeat("e", 32), "--request-not-after", time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), "--ttl", "30s", "--json")
	if err != nil {
		t.Fatalf("explicit heartbeat: %v output=%s", err, out)
	}
	out, err = runRemoteCLI(t, "exec", "--profile", profile, "--home", clientHome, "--claim-id", stored.ClaimID, "--token-file", tokenPath, "--revision", "2", "--operation-id", strings.Repeat("f", 32), "--request-not-after", time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), "--ttl", "30s", "--max-duration", "1s", "--json", "--", "true")
	if err != nil {
		t.Fatalf("explicit guarded effect: %v output=%s", err, out)
	}
}

func TestRemoteCLIInspectsAndReconcilesInterruptedExec(t *testing.T) {
	profile, clientHome, claimHandle := remoteCLIFixture(t)
	out, err := runRemoteCLI(t, "acquire", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--resource", "coordination:reconcile", "--ttl", "30s", "--json")
	if err != nil {
		t.Fatalf("acquire: %v output=%s", err, out)
	}
	op := strings.Repeat("b", 32)
	ctx, cancel := context.WithCancel(context.Background())
	marker := filepath.Join(t.TempDir(), "started")
	var execOut bytes.Buffer
	execResult := make(chan error, 1)
	go func() {
		execResult <- Run(ctx, []string{"worklease", "exec", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--operation-id", op, "--ttl", "30s", "--json", "--", "sh", "-c", `touch "$1"; sleep 5`, "sh", marker}, "test", "unknown", "unknown", &execOut, &bytes.Buffer{})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, statErr := os.Stat(marker); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-execResult
			t.Fatal("guarded child did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	err = <-execResult
	if classified := reason.As(err); classified == nil || classified.Reason != reason.ReasonInterrupted {
		t.Fatalf("exec interruption=%v output=%s", err, execOut.String())
	}
	out, err = runRemoteCLI(t, "op", "inspect", "--profile", profile, "--home", clientHome, "--operation-id", op, "--full", "--json")
	if err != nil {
		t.Fatalf("inspect: %v output=%s", err, out)
	}
	var envelope struct {
		Inspection struct {
			ClaimID     string `json:"claimId"`
			RequestHash string `json:"requestSha256"`
		} `json:"inspection"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Inspection.ClaimID == "" || envelope.Inspection.RequestHash == "" {
		t.Fatalf("inspection=%s", out)
	}
	out, err = runRemoteCLI(t, "op", "reconcile", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--target-claim-id", envelope.Inspection.ClaimID, "--target-operation-id", op, "--expected-request-sha256", envelope.Inspection.RequestHash, "--outcome", "observed-failure", "--evidence", `{"outcome":"observed-failure","executorStopped":true}`, "--ttl", "30s", "--json")
	if err != nil {
		t.Fatalf("reconcile: %v output=%s", err, out)
	}
}

func TestRemoteDoctorAuthenticatesWithoutExposingCredential(t *testing.T) {
	profile, clientHome, _ := remoteCLIFixture(t)
	before, err := os.ReadDir(clientHome)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runRemoteCLI(t, "doctor", "--profile", profile, "--home", clientHome, "--resource", "coordination:doctor", "--json")
	if err != nil {
		t.Fatalf("doctor: %v output=%s", err, out)
	}
	profiles, _, err := config.LoadProfiles(config.UserProfilePaths(os.Getenv))
	if err != nil {
		t.Fatal(err)
	}
	credential, err := handle.ReadCredential(profiles[profile].Credential.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"remote.profile", "remote.credential", "remote.reachability", "remote.tls", "remote.protocol", "remote.metadata", "remote.prefixes", "remote.authentication", "remote.role", "remote.recovery", `current installation role is admin`} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, credential) {
		t.Fatalf("doctor exposed credential: %s", out)
	}
	after, err := os.ReadDir(clientHome)
	if err != nil || len(after) != len(before) {
		t.Fatalf("doctor mutated local home: before=%v after=%v err=%v", before, after, err)
	}

	out, err = runRemoteCLI(t, "doctor", "--profile", profile, "--home", clientHome, "--resource", "github:not-admitted", "--json")
	if err == nil || !strings.Contains(out, `"id":"remote.prefixes","status":"fail"`) || strings.Contains(out, credential) {
		t.Fatalf("unadmitted doctor err=%v output=%s", err, out)
	}

	out, err = runRemoteCLI(t, "doctor", "--profile", profile, "--home", clientHome)
	if err != nil || !strings.HasPrefix(out, "PASS remote onboarding diagnostics\n") || !strings.Contains(out, "rerun with --resource KEY") {
		t.Fatalf("doctor text err=%v output=%s", err, out)
	}
}

func TestRemoteDoctorReportsOldServerDiagnosticsUnavailable(t *testing.T) {
	clearWorkleaseEnvironment(t)
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	authorityID, restoreID := strings.Repeat("a", 32), strings.Repeat("b", 32)
	timestamp := "2026-01-01T00:00:00Z"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		switch r.URL.Path {
		case "/.well-known/worklease":
			fmt.Fprintf(w, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"%s","restoreId":"%s","authorityTime":"%s","result":{}}`, authorityID, restoreID, timestamp)
		case "/v1/installations/self":
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"ok":false,"protocolVersion":"worklease-http/1","authorityId":"%s","restoreId":"%s","authorityTime":"%s","error":{"reason":"invalid-path","message":"old server"}}`, authorityID, restoreID, timestamp)
		case "/v1/claims/list":
			fmt.Fprintf(w, `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"%s","restoreId":"%s","authorityTime":"%s","result":{"claims":[]}}`, authorityID, restoreID, timestamp)
		default:
			t.Fatalf("unexpected diagnostic route %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	paths := config.UserProfilePaths(os.Getenv)
	credentialPath := filepath.Join(configRoot, "worklease", "credentials", "old")
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(credentialPath)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialPath, []byte(strings.Repeat("c", 64)), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := config.Profile{Name: "old", Endpoint: server.URL, AuthorityID: authorityID, RestoreID: restoreID, AllowInsecureHTTP: true, Credential: config.CredentialDescriptor{Path: credentialPath}}
	if err := config.SaveProfiles(paths, []config.Profile{profile}, "old"); err != nil {
		t.Fatal(err)
	}
	out, err := runRemoteCLI(t, "doctor", "--profile", "old", "--home", t.TempDir(), "--json")
	if err != nil || !strings.Contains(out, `"id":"remote.prefixes","status":"warn"`) || !strings.Contains(out, `"id":"remote.role","status":"warn"`) || !strings.Contains(out, "not verified") {
		t.Fatalf("old-server doctor err=%v output=%s", err, out)
	}

	profile.AuthorityID = ""
	if err := config.SaveProfiles(paths, []config.Profile{profile}, "old"); err != nil {
		t.Fatal(err)
	}
	out, err = runRemoteCLI(t, "doctor", "--profile", "old", "--home", t.TempDir(), "--json")
	if err == nil || !strings.Contains(out, `"id":"remote.metadata","status":"fail"`) || !strings.Contains(out, "does not pin both authority and restore identities") || strings.Contains(out, `"id":"remote.authentication","status":"ok"`) {
		t.Fatalf("unpinned-profile doctor err=%v output=%s", err, out)
	}
}

func TestRemoteDoctorBoundsNetworkProbes(t *testing.T) {
	clearWorkleaseEnvironment(t)
	configRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	paths := config.UserProfilePaths(os.Getenv)
	credentialPath := filepath.Join(configRoot, "worklease", "credentials", "slow")
	if err := handle.StoreCredentialNoReplace(credentialPath, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	profile := config.Profile{Name: "slow", Endpoint: server.URL, AuthorityID: strings.Repeat("a", 32), RestoreID: strings.Repeat("b", 32), AllowInsecureHTTP: true, Credential: config.CredentialDescriptor{Path: credentialPath}}
	if err := config.SaveProfiles(paths, []config.Profile{profile}, "slow"); err != nil {
		t.Fatal(err)
	}
	previousTimeout := remoteDoctorProbeTimeout
	remoteDoctorProbeTimeout = 20 * time.Millisecond
	t.Cleanup(func() { remoteDoctorProbeTimeout = previousTimeout })
	started := time.Now()
	out, err := runRemoteCLI(t, "doctor", "--profile", "slow", "--home", t.TempDir(), "--json")
	if err == nil || time.Since(started) > time.Second || !strings.Contains(out, "timed out within the diagnostic deadline") || !strings.Contains(out, "possible causes") {
		t.Fatalf("bounded doctor duration=%s err=%v output=%s", time.Since(started), err, out)
	}
}

func TestRemoteDoctorProbeErrorClassification(t *testing.T) {
	if got := classifyRemoteProbeError(&net.DNSError{Err: "missing", Name: "redacted.invalid"}); got != "dns" {
		t.Fatalf("DNS classification = %q", got)
	}
	if got := classifyRemoteProbeError(syscall.ECONNREFUSED); got != "refused" {
		t.Fatalf("refused classification = %q", got)
	}
	if got := classifyRemoteProbeError(context.DeadlineExceeded); got != "timeout" {
		t.Fatalf("timeout classification = %q", got)
	}
	if got := classifyRemoteProbeError(reason.New(reason.ReasonAuthorityMismatch, "remote certificate does not match pinned certificate")); got != "tls" {
		t.Fatalf("TLS classification = %q", got)
	}
	if got := classifyRemoteProbeError(reason.New(reason.ReasonProtocolVersionUnsupported, "unsupported")); got != "protocol" {
		t.Fatalf("protocol classification = %q", got)
	}
}

func TestRemoteReplaceRejectsBeforeReadingFiles(t *testing.T) {
	profile, clientHome, claimHandle := remoteCLIFixture(t)
	_, err := runRemoteCLI(t, "replace-file", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--path", filepath.Join(t.TempDir(), "missing"), "--content-file", filepath.Join(t.TempDir(), "missing"), "--expected-sha256", strings.Repeat("0", 64), "--json")
	classified := reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonOperationKindUnsupported {
		t.Fatalf("replace error=%v", err)
	}
}

func TestArtifactEnrollmentFromFileAndFDActivatesDefaultProfile(t *testing.T) {
	adminProfile, adminHome, _ := remoteCLIFixture(t)
	secretRoot := filepath.Join(t.TempDir(), "secrets")
	if err := handle.EnsureOwnerPrivateDir(secretRoot); err != nil {
		t.Fatal(err)
	}
	fileArtifact := filepath.Join(secretRoot, "file.invite")
	fdArtifact := filepath.Join(secretRoot, "fd.invite")
	if out, err := runRemoteCLI(t, "invite", "issue", "--profile", adminProfile, "--home", adminHome, "--invite-file", fileArtifact, "--label", "file-team", "--json"); err != nil {
		t.Fatalf("file invite issue: %v output=%s", err, out)
	}
	if out, err := runRemoteCLI(t, "invite", "issue", "--profile", adminProfile, "--home", adminHome, "--invite-file", fdArtifact, "--label", "fd-team", "--json"); err != nil {
		t.Fatalf("fd invite issue: %v output=%s", err, out)
	}

	fileConfig := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", fileConfig)
	if out, err := runRemoteCLI(t, "enroll", "--home", t.TempDir(), "--invite-file", fileArtifact, "--allow-insecure-http", "--json"); err != nil {
		t.Fatalf("artifact file enrollment: %v output=%s", err, out)
	}
	profiles, defaultName, err := config.LoadProfiles(config.UserProfilePaths(os.Getenv))
	if err != nil || defaultName != "file-team" || profiles[defaultName].AuthorityID == "" {
		t.Fatalf("file enrollment profiles=%+v default=%q err=%v", profiles, defaultName, err)
	}
	if out, err := runRemoteCLI(t, "list", "--home", t.TempDir(), "--json"); err != nil {
		t.Fatalf("default profile later request: %v output=%s", err, out)
	}

	fdConfig := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", fdConfig)
	input, err := os.Open(fdArtifact)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if out, err := runRemoteCLI(t, "enroll", "--home", t.TempDir(), "--invite-fd", strconv.Itoa(int(input.Fd())), "--allow-insecure-http", "--json"); err != nil {
		t.Fatalf("artifact fd enrollment: %v output=%s", err, out)
	}
	profiles, defaultName, err = config.LoadProfiles(config.UserProfilePaths(os.Getenv))
	if err != nil || defaultName != "fd-team" || profiles[defaultName].AuthorityID == "" {
		t.Fatalf("fd enrollment profiles=%+v default=%q err=%v", profiles, defaultName, err)
	}
}

func TestDefinitiveInviteIssueFailureClearsStage(t *testing.T) {
	profile, clientHome, _ := remoteCLIFixture(t)
	secretRoot := filepath.Join(t.TempDir(), "secrets")
	if err := handle.EnsureOwnerPrivateDir(secretRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := runRemoteCLI(t, "invite", "issue", "--profile", profile, "--home", clientHome, "--role", "invalid", "--invite-file", filepath.Join(secretRoot, "invalid"), "--json"); err == nil {
		t.Fatal("invalid invite role was accepted")
	}
	if out, err := runRemoteCLI(t, "invite", "issue", "--profile", profile, "--home", clientHome, "--role", "read", "--invite-file", filepath.Join(secretRoot, "valid"), "--json"); err != nil {
		t.Fatalf("valid issue after definitive failure: %v output=%s", err, out)
	}
}

func TestDefinitiveAdminFailureClearsPendingRecoveryRecord(t *testing.T) {
	profile, clientHome, _ := remoteCLIFixture(t)
	secretRoot := filepath.Join(t.TempDir(), "secrets")
	if err := handle.EnsureOwnerPrivateDir(secretRoot); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := runRemoteCLI(t, "invite", "issue", "--profile", profile, "--home", clientHome, "--role", "invalid", "--invite-file", filepath.Join(secretRoot, "invalid"), "--json"); err == nil {
			t.Fatal("invalid invite role was accepted")
		}
	}
	pendingDir := filepath.Join(clientHome, "pending", profile)
	entries, err := os.ReadDir(pendingDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("definitively rejected mutations retained %d pending records", len(entries))
	}
}

func TestRemoteInviteWritesOnlyProtectedSecretFile(t *testing.T) {
	profile, clientHome, _ := remoteCLIFixture(t)
	secretRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secretRoot = filepath.Join(secretRoot, "secrets")
	if err := handle.EnsureOwnerPrivateDir(secretRoot); err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(secretRoot, "invite")
	out, err := runRemoteCLI(t, "invite", "issue", "--profile", profile, "--home", clientHome, "--role", "read", "--invite-file", secretPath, "--json")
	if err != nil {
		t.Fatalf("invite: %v output=%s", err, out)
	}
	raw, err := handle.ReadOwnerPrivate(secretPath, authority.MaxInviteArtifactBytes)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := authority.DecodeInviteArtifact(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, artifact.Invite) {
		t.Fatal("ordinary output exposed invite secret")
	}
}
