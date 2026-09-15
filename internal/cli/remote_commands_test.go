package cli

import (
	"bytes"
	"context"
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
	go func() { time.Sleep(150 * time.Millisecond); cancel() }()
	var execOut bytes.Buffer
	err = Run(ctx, []string{"worklease", "exec", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--operation-id", op, "--ttl", "30s", "--json", "--", "sleep", "5"}, "test", "unknown", "unknown", &execOut, &bytes.Buffer{})
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
	out, err := runRemoteCLI(t, "doctor", "--profile", profile, "--home", clientHome, "--json")
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
	if strings.Contains(out, credential) || !strings.Contains(out, "remote.authentication") || !strings.Contains(out, "remote.recovery") {
		t.Fatalf("doctor output=%s", out)
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
	secret, err := handle.ReadCredential(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, secret) {
		t.Fatal("ordinary output exposed invite secret")
	}
}
