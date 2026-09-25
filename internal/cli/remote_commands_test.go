package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/server"
	"github.com/brettinternet/worklease/internal/store"
	urfave "github.com/urfave/cli/v3"
)

func inviteFromCommand(cmd *urfave.Command) (string, error) {
	value, _, err := inviteInputFromCommand(cmd)
	return value, err
}

type cliRoundTripFunc func(*http.Request) (*http.Response, error)

func (f cliRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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

func TestRemoteAcquireContentionReportsRedactedHolderAndClearsAttempt(t *testing.T) {
	profileName, clientHome, holderHandle := remoteCLIFixture(t)
	out, err := runRemoteCLI(t, "acquire", "--profile", profileName, "--home", clientHome, "--handle", holderHandle, "--resource", "coordination:busy", "--ttl", "30s", "--json")
	if err != nil {
		t.Fatalf("holder acquire: %v output=%s", err, out)
	}
	var holderResult struct {
		ClaimID string `json:"claimId"`
		AgentID string `json:"agentId"`
		WorkKey string `json:"workKey"`
	}
	if err := json.Unmarshal([]byte(out), &holderResult); err != nil || holderResult.ClaimID == "" {
		t.Fatalf("holder result=%s err=%v", out, err)
	}
	attemptHandle := filepath.Join(t.TempDir(), "attempt.json")
	out, err = runRemoteCLI(t, "acquire", "--profile", profileName, "--home", clientHome, "--handle", attemptHandle, "--resource", "coordination:busy", "--ttl", "30s", "--json")
	if err == nil || !strings.Contains(out, `"reason":"already-claimed"`) || !strings.Contains(out, `"resource":"coordination:busy"`) || !strings.Contains(out, holderResult.ClaimID) {
		t.Fatalf("contention JSON=%v output=%s", err, out)
	}
	if !strings.Contains(out, `"requestClaimId"`) || strings.Contains(out, `"sessionId"`) || strings.Contains(out, `"token"`) {
		t.Fatalf("contention identifiers/redaction=%s", out)
	}
	if _, statErr := os.Stat(attemptHandle); !os.IsNotExist(statErr) {
		t.Fatalf("definitive contention retained attempt handle: %v", statErr)
	}

	multiHandle := filepath.Join(t.TempDir(), "attempt-multi.json")
	out, err = runRemoteCLI(t, "acquire", "--profile", profileName, "--home", clientHome, "--handle", multiHandle, "--resource", "coordination:free", "--resource", "coordination:busy", "--ttl", "30s", "--json")
	if err == nil || !strings.Contains(out, `"reason":"already-claimed"`) || !strings.Contains(out, `"resource":"coordination:busy"`) || !strings.Contains(out, holderResult.ClaimID) {
		t.Fatalf("multi-resource contention=%v output=%s", err, out)
	}
	if _, statErr := os.Stat(multiHandle); !os.IsNotExist(statErr) {
		t.Fatalf("multi-resource contention retained attempt handle: %v", statErr)
	}
	freeHandle := filepath.Join(t.TempDir(), "free.json")
	if out, err = runRemoteCLI(t, "acquire", "--profile", profileName, "--home", clientHome, "--handle", freeHandle, "--resource", "coordination:free", "--ttl", "30s", "--json"); err != nil {
		t.Fatalf("multi-resource contention did not roll back free member: %v output=%s", err, out)
	}
	if out, err = runRemoteCLI(t, "verify", "--profile", profileName, "--home", clientHome, "--handle", holderHandle, "--json"); err != nil || !strings.Contains(out, holderResult.ClaimID) {
		t.Fatalf("contention changed existing holder: %v output=%s", err, out)
	}

	textHandle := filepath.Join(t.TempDir(), "attempt-text.json")
	var textOut, textErr bytes.Buffer
	err = Run(context.Background(), []string{"worklease", "acquire", "--profile", profileName, "--home", clientHome, "--handle", textHandle, "--resource", "coordination:busy", "--ttl", "30s"}, "test", "unknown", "unknown", &textOut, &textErr)
	if err == nil {
		t.Fatalf("text contention unexpectedly succeeded: stdout=%s stderr=%s", textOut.String(), textErr.String())
	}
	if err := output.WriteTextError(&textErr, err); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textErr.String(), "resource: coordination:busy") || !strings.Contains(textErr.String(), holderResult.ClaimID) || !strings.Contains(textErr.String(), "holder:") || !strings.Contains(textErr.String(), "agentId") || !strings.Contains(textErr.String(), "workKey") || !strings.Contains(textErr.String(), "expiresAt") {
		t.Fatalf("contention text=%v stdout=%s stderr=%s", err, textOut.String(), textErr.String())
	}
	if _, statErr := os.Stat(textHandle); !os.IsNotExist(statErr) {
		t.Fatalf("definitive text contention retained attempt handle: %v", statErr)
	}
}

func TestPendingAcquireBlocksLifecycleAndReplaysWithoutResourceFlags(t *testing.T) {
	profileName, clientHome, claimHandle := remoteCLIFixture(t)
	profiles, _, err := config.LoadProfiles(config.UserProfilePaths(os.Getenv))
	if err != nil {
		t.Fatal(err)
	}
	profile := profiles[profileName]
	mutationCalls := 0
	transport := cliRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		response, roundTripErr := http.DefaultTransport.RoundTrip(request)
		if request.URL.Path != "/v1/claims/acquire" || roundTripErr != nil {
			return response, roundTripErr
		}
		mutationCalls++
		_ = response.Body.Close()
		return nil, errors.New("lost acquire response")
	})
	client, err := authority.NewHTTPClient(profile, authority.NewFilePendingStore(filepath.Join(t.TempDir(), "pending")), transport)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := authority.NewRemoteAuthority(client)
	if err != nil {
		t.Fatal(err)
	}
	claimID := strings.Repeat("c", 32)
	_, err = remote.Acquire(context.Background(), lease.AcquireRequest{ClaimID: claimID, Token: strings.Repeat("d", 64), Resources: []string{"coordination:pending"}, AgentID: "agent", SessionID: "session", WorkKey: "pending", TTL: 30 * time.Second, MaxHold: time.Hour, RequestNotAfter: time.Now().Add(time.Hour), HandlePath: claimHandle})
	if classified := reason.As(err); classified == nil || classified.Reason != reason.ReasonUnknownOutcome || mutationCalls != 1 {
		t.Fatalf("staged acquire=%v calls=%d", err, mutationCalls)
	}
	before, err := handle.Read(claimHandle)
	if err != nil || before.PendingRequest == nil || before.PendingRequest.Kind != "acquire" {
		t.Fatalf("pending acquire=%#v err=%v", before, err)
	}
	out, err := runRemoteCLI(t, "heartbeat", "--profile", profileName, "--home", clientHome, "--handle", claimHandle, "--ttl", "30s", "--json")
	classified := reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonRecoveryRequired || !strings.Contains(out, `"commitState":"not-committed"`) || !strings.Contains(out, claimHandle) || !strings.Contains(out, claimID) || strings.Contains(out, strings.Repeat("d", 64)) {
		t.Fatalf("pending acquire heartbeat err=%v output=%s", err, out)
	}
	afterRefusal, err := handle.Read(claimHandle)
	if err != nil || afterRefusal.PendingRequest == nil || afterRefusal.PendingRequest.OperationID != claimID {
		t.Fatalf("refusal changed pending acquire: %#v err=%v", afterRefusal, err)
	}
	out, err = runRemoteCLI(t, "acquire", "--profile", profileName, "--home", clientHome, "--handle", claimHandle, "--json")
	if err != nil || !strings.Contains(out, `"resources":["coordination:pending"]`) {
		t.Fatalf("exact acquire replay: %v output=%s", err, out)
	}
	ready, err := handle.Read(claimHandle)
	if err != nil || ready.State != "ready" || ready.PendingRequest != nil || ready.ClaimID != claimID {
		t.Fatalf("replayed acquire handle=%#v err=%v", ready, err)
	}
}

// stagePendingRemoteAcquire drops one acquire response so the named handle
// retains an exact unresolved acquire request.
func stagePendingRemoteAcquire(t *testing.T, profileName, claimHandle, resource, claimID string) int {
	t.Helper()
	profiles, _, err := config.LoadProfiles(config.UserProfilePaths(os.Getenv))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	transport := cliRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		response, roundTripErr := http.DefaultTransport.RoundTrip(request)
		if request.URL.Path != "/v1/claims/acquire" || roundTripErr != nil {
			return response, roundTripErr
		}
		calls++
		_ = response.Body.Close()
		return nil, errors.New("lost acquire response")
	})
	client, err := authority.NewHTTPClient(profiles[profileName], authority.NewFilePendingStore(filepath.Join(t.TempDir(), "pending")), transport)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := authority.NewRemoteAuthority(client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = remote.Acquire(context.Background(), lease.AcquireRequest{ClaimID: claimID, Token: strings.Repeat("d", 64), Resources: []string{resource}, AgentID: "agent", SessionID: "session", WorkKey: "pending", TTL: 30 * time.Second, MaxHold: time.Hour, RequestNotAfter: time.Now().Add(time.Hour), HandlePath: claimHandle})
	if classified := reason.As(err); classified == nil || classified.Reason != reason.ReasonUnknownOutcome || calls != 1 {
		t.Fatalf("staged acquire=%v calls=%d", err, calls)
	}
	staged, err := handle.Read(claimHandle)
	if err != nil || staged.PendingRequest == nil || staged.PendingRequest.OperationID != claimID {
		t.Fatalf("pending acquire=%#v err=%v", staged, err)
	}
	return calls
}

// A pending acquire is recovered by exact replay only. Supplying different
// resources must never silently substitute a new request for the retained one.
func TestPendingRemoteAcquireRefusesChangedAndInvalidAcquisitionInputs(t *testing.T) {
	profileName, clientHome, claimHandle := remoteCLIFixture(t)
	claimID := strings.Repeat("c", 32)
	stagePendingRemoteAcquire(t, profileName, claimHandle, "coordination:staged", claimID)
	before, err := handle.Read(claimHandle)
	if err != nil {
		t.Fatal(err)
	}

	out, err := runRemoteCLI(t, "acquire", "--profile", profileName, "--home", clientHome, "--handle", claimHandle, "--resource", "coordination:different", "--json")
	classified := reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonRecoveryRequired || !strings.Contains(out, claimID) {
		t.Fatalf("changed resources replayed or misreported: err=%v output=%s", err, out)
	}
	if strings.Contains(out, strings.Repeat("d", 64)) {
		t.Fatalf("credential leaked: %s", out)
	}
	out, err = runRemoteCLI(t, "acquire", "--profile", profileName, "--home", clientHome, "--handle", claimHandle, "--resource", "coordination:staged", "--session", "other-session", "--json")
	if classified := reason.As(err); classified == nil || classified.Reason != reason.ReasonRecoveryRequired || !strings.Contains(out, claimID) {
		t.Fatalf("another session adopted the pending acquire: err=%v output=%s", err, out)
	}

	// Mixed and malformed resource input must be rejected as invalid input
	// rather than dispatching the retained request.
	if _, err = runRemoteCLI(t, "acquire", "--profile", profileName, "--home", clientHome, "--handle", claimHandle, "--resource", "coordination:staged", "--path", "README.md", "--json"); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonResourceInputConflict {
		t.Fatalf("mixed resource input=%v", err)
	}
	if _, err = runRemoteCLI(t, "acquire", "--profile", profileName, "--home", clientHome, "--handle", claimHandle, "--resource", "coordination:staged", "--resource", "coordination:staged", "--json"); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonInvalidResource {
		t.Fatalf("malformed resource input=%v", err)
	}

	after, err := handle.Read(claimHandle)
	if err != nil || after.PendingRequest == nil || after.PendingRequest.OperationID != claimID || !bytes.Equal(after.PendingRequest.Request, before.PendingRequest.Request) {
		t.Fatalf("refusals changed retained request: %#v err=%v", after, err)
	}

	// The same resources still recover the retained request exactly.
	out, err = runRemoteCLI(t, "acquire", "--profile", profileName, "--home", clientHome, "--handle", claimHandle, "--resource", "coordination:staged", "--json")
	if err != nil || !strings.Contains(out, `"resources":["coordination:staged"]`) {
		t.Fatalf("matching-input replay: %v output=%s", err, out)
	}
	ready, err := handle.Read(claimHandle)
	if err != nil || ready.State != "ready" || ready.PendingRequest != nil || ready.ClaimID != claimID {
		t.Fatalf("replayed handle=%#v err=%v", ready, err)
	}
}

// A handle carrying both a pending acquire and an unresolved recovery record
// must be reconciled first; replay must not dispatch past that guard.
func TestPendingRemoteAcquireWithRecoveryRecordRefusesReplay(t *testing.T) {
	profileName, clientHome, claimHandle := remoteCLIFixture(t)
	claimID := strings.Repeat("c", 32)
	stagePendingRemoteAcquire(t, profileName, claimHandle, "coordination:dual", claimID)
	staged, err := handle.Read(claimHandle)
	if err != nil {
		t.Fatal(err)
	}
	staged.RecoveryRequest = &handle.RecoveryRequest{OperationID: strings.Repeat("e", 32), TargetClaimID: staged.ClaimID, RequestHash: strings.Repeat("f", 64), RequestNotAfter: time.Now().Add(time.Hour).UTC()}
	if err := handle.Write(claimHandle, staged); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"acquire", "--profile", profileName, "--home", clientHome, "--handle", claimHandle, "--json"},
		{"acquire", "--profile", profileName, "--home", clientHome, "--handle", claimHandle, "--resource", "coordination:dual", "--json"},
	} {
		out, runErr := runRemoteCLI(t, args...)
		classified := reason.As(runErr)
		if classified == nil || classified.Reason != reason.ReasonHandleInUse {
			t.Fatalf("dual-record handle replayed: args=%v err=%v output=%s", args, runErr, out)
		}
	}
	after, err := handle.Read(claimHandle)
	if err != nil || after.PendingRequest == nil || after.RecoveryRequest == nil {
		t.Fatalf("dual records changed: %#v err=%v", after, err)
	}
}

// Only a remote pending request is recoverable from a bare handle. A local
// acquire must still reject missing or invalid resource input before it
// creates or opens the authority store.
func TestLocalAcquireValidatesResourceInputBeforeOpeningStore(t *testing.T) {
	home := filepath.Join(t.TempDir(), "authority")
	claimHandle := filepath.Join(t.TempDir(), "claim.json")
	for _, args := range [][]string{
		{"--local", "--home", home, "acquire", "--handle", claimHandle, "--json"},
		{"--local", "--home", home, "acquire", "--handle", claimHandle, "--resource", "a", "--path", "README.md", "--json"},
	} {
		_, err := runRemoteCLI(t, args...)
		classified := reason.As(err)
		if classified == nil || classified.Reason != reason.ReasonInvalidResource && classified.Reason != reason.ReasonResourceInputConflict {
			t.Fatalf("invalid local input=%v args=%v", err, args)
		}
		if _, statErr := os.Stat(home); !os.IsNotExist(statErr) {
			t.Fatalf("invalid input opened the authority store: %v", statErr)
		}
	}
}

func TestRemoteCLIReacquiresExpiredContextualClaimWithFreshInputs(t *testing.T) {
	profile, clientHome, claimHandle := remoteCLIFixture(t)
	out, err := runRemoteCLI(t, "acquire", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--resource", "coordination:old", "--ttl", "1s", "--json")
	if err != nil {
		t.Fatalf("initial acquire: %v output=%s", err, out)
	}
	old, err := handle.Read(claimHandle)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	old.ExpiresAt = time.Now().Add(time.Hour)
	if err := handle.Write(claimHandle, old); err != nil {
		t.Fatal(err)
	}
	out, err = runRemoteCLI(t, "acquire", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--resource", "coordination:new-a", "--resource", "coordination:new-b", "--agent", "fresh-agent", "--session", "fresh-session", "--work-key", "fresh-work", "--ttl", "20s", "--coordination-only", "--json")
	if err != nil {
		t.Fatalf("reacquire: %v output=%s", err, out)
	}
	fresh, err := handle.Read(claimHandle)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ClaimID == old.ClaimID || fresh.Token == old.Token {
		t.Fatalf("expired epoch was reused: old=%s fresh=%s", old.ClaimID, fresh.ClaimID)
	}
	if fresh.AgentID != "fresh-agent" || fresh.SessionID != "fresh-session" || strings.Join(fresh.Resources, ",") != "coordination:new-a,coordination:new-b" || fresh.State != "ready" || fresh.PendingRequest != nil {
		t.Fatalf("fresh handle did not use current inputs: %#v", fresh)
	}
	if strings.Contains(out, old.ClaimID) || !strings.Contains(out, fresh.ClaimID) || !strings.Contains(out, `"workKey":"fresh-work"`) {
		t.Fatalf("reacquire returned stale success: %s", out)
	}
}

func TestRemoteCLIDefaultAndExplicitSessionsSelectIndependentHandles(t *testing.T) {
	profile, clientHome, _ := remoteCLIFixture(t)
	out, err := runRemoteCLI(t, "acquire", "--profile", profile, "--home", clientHome, "--resource", "coordination:default-old", "--ttl", "1s", "--json")
	if err != nil {
		t.Fatalf("default acquire: %v output=%s", err, out)
	}
	paths, err := filepath.Glob(filepath.Join(clientHome, "handles", "*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("default handles=%v err=%v", paths, err)
	}
	old, err := handle.Read(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	out, err = runRemoteCLI(t, "acquire", "--profile", profile, "--home", clientHome, "--resource", "coordination:default-new", "--ttl", "20s", "--json")
	if err != nil {
		t.Fatalf("default reacquire: %v output=%s", err, out)
	}
	fresh, err := handle.Read(paths[0])
	if err != nil || fresh.ClaimID == old.ClaimID || fresh.Token == old.Token || strings.Join(fresh.Resources, ",") != "coordination:default-new" {
		t.Fatalf("default handle was not replaced: %#v err=%v", fresh, err)
	}
	out, err = runRemoteCLI(t, "acquire", "--profile", profile, "--home", clientHome, "--session", "other", "--resource", "coordination:other", "--ttl", "20s", "--json")
	if err != nil {
		t.Fatalf("explicit-session acquire: %v output=%s", err, out)
	}
	paths, err = filepath.Glob(filepath.Join(clientHome, "handles", "*.json"))
	if err != nil || len(paths) != 2 {
		t.Fatalf("session handles=%v err=%v", paths, err)
	}
	foundOther := false
	for _, path := range paths {
		h, readErr := handle.Read(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		foundOther = foundOther || h.SessionID == "other" && strings.Join(h.Resources, ",") == "coordination:other"
	}
	if !foundOther {
		t.Fatalf("explicit session did not select an independent handle: %v", paths)
	}
}

func TestContextualHandlesSwitchByAuthorityAndProfileAliasesShareSlot(t *testing.T) {
	profile, clientHome, _ := remoteCLIFixture(t)
	remoteOut, err := runRemoteCLI(t, "acquire", "--profile", profile, "--home", clientHome, "--resource", "coordination:remote", "--ttl", "30s", "--json")
	if err != nil {
		t.Fatalf("remote acquire: %v output=%s", err, remoteOut)
	}
	localOut, err := runRemoteCLI(t, "acquire", "--local", "--home", clientHome, "--resource", "local", "--json")
	if err != nil {
		t.Fatalf("local acquire: %v output=%s", err, localOut)
	}
	var localEnvelope struct {
		Result struct {
			ClaimID string `json:"claimId"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(localOut), &localEnvelope); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(clientHome, "handles", "*.json"))
	if err != nil || len(paths) != 2 {
		t.Fatalf("authority-scoped handles=%v err=%v", paths, err)
	}
	localStatus, err := runRemoteCLI(t, "status", "--local", "--home", clientHome, "--json")
	if err != nil || !strings.Contains(localStatus, localEnvelope.Result.ClaimID) {
		t.Fatalf("switching back did not resume local handle: %v output=%s", err, localStatus)
	}

	profilePaths := config.UserProfilePaths(os.Getenv)
	profiles, defaultName, err := config.LoadProfiles(profilePaths)
	if err != nil {
		t.Fatal(err)
	}
	alias := profiles[profile]
	alias.Name = "alias"
	if err := config.SaveProfiles(profilePaths, []config.Profile{profiles[profile], alias}, defaultName); err != nil {
		t.Fatal(err)
	}
	aliasStatus, err := runRemoteCLI(t, "status", "--profile", alias.Name, "--home", clientHome, "--json")
	if err != nil {
		t.Fatalf("alias status: %v output=%s", err, aliasStatus)
	}
	var remoteEnvelope struct {
		Result struct {
			ClaimID string `json:"claimId"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(remoteOut), &remoteEnvelope); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(aliasStatus, remoteEnvelope.Result.ClaimID) {
		t.Fatalf("profile alias selected another handle: %s", aliasStatus)
	}
	paths, err = filepath.Glob(filepath.Join(clientHome, "handles", "*.json"))
	if err != nil || len(paths) != 2 {
		t.Fatalf("profile alias created a slot: %v err=%v", paths, err)
	}
}

func TestRemoteCLIRefusesLocallyExpiredButRemotelyActiveClaim(t *testing.T) {
	profile, clientHome, claimHandle := remoteCLIFixture(t)
	out, err := runRemoteCLI(t, "acquire", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--resource", "coordination:active", "--ttl", "30s", "--json")
	if err != nil {
		t.Fatalf("acquire: %v output=%s", err, out)
	}
	stored, err := handle.Read(claimHandle)
	if err != nil {
		t.Fatal(err)
	}
	stale := stored
	stale.ExpiresAt = time.Now().Add(-time.Minute)
	if err := handle.Write(claimHandle, stale); err != nil {
		t.Fatal(err)
	}
	out, err = runRemoteCLI(t, "acquire", "--profile", profile, "--home", clientHome, "--handle", claimHandle, "--resource", "coordination:replacement", "--ttl", "30s", "--json")
	classified := reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonHandleInUse {
		t.Fatalf("stale local expiry replaced active claim: err=%v output=%s", err, out)
	}
	after, readErr := handle.Read(claimHandle)
	if readErr != nil || after.ClaimID != stored.ClaimID || after.Token != stored.Token {
		t.Fatalf("active handle changed: %#v err=%v", after, readErr)
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
	for _, kind := range []string{"dns", "refused", "timeout", "tls"} {
		if got := classifyRemoteProbeError(reason.New(reason.ReasonRemoteTransportFailure, "sanitized").With("transport", kind)); got != kind {
			t.Fatalf("typed %s classification = %q", kind, got)
		}
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

func TestArtifactEnrollmentCleanupGuidanceAndInputPreservation(t *testing.T) {
	adminProfile, adminHome, _ := remoteCLIFixture(t)
	secretRoot := filepath.Join(t.TempDir(), "secrets")
	if err := handle.EnsureOwnerPrivateDir(secretRoot); err != nil {
		t.Fatal(err)
	}
	artifacts := map[string]string{}
	for _, label := range []string{"file-team", "fd-team", "prompt-team", "json-team"} {
		path := filepath.Join(secretRoot, label+".invite")
		if out, err := runRemoteCLI(t, "invite", "issue", "--profile", adminProfile, "--home", adminHome, "--invite-file", path, "--label", label, "--json"); err != nil {
			t.Fatalf("%s invite issue: %v output=%s", label, err, out)
		}
		artifacts[label] = path
	}

	fileBytes, err := os.ReadFile(artifacts["file-team"])
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	out, err := runRemoteCLI(t, "enroll", "--home", t.TempDir(), "--invite-file", artifacts["file-team"], "--allow-insecure-http")
	if err != nil || !strings.Contains(out, "invite consumed") || !strings.Contains(out, "remove the local invite file") || strings.Contains(out, strings.TrimSpace(string(fileBytes))) {
		t.Fatalf("file enrollment guidance: err=%v output=%s", err, out)
	}
	if after, readErr := os.ReadFile(artifacts["file-team"]); readErr != nil || !bytes.Equal(after, fileBytes) {
		t.Fatalf("successful enrollment changed invite file: err=%v", readErr)
	}
	profiles, defaultName, err := config.LoadProfiles(config.UserProfilePaths(os.Getenv))
	if err != nil || defaultName != "file-team" || profiles[defaultName].AuthorityID == "" {
		t.Fatalf("file enrollment profiles=%+v default=%q err=%v", profiles, defaultName, err)
	}
	if out, err := runRemoteCLI(t, "list", "--home", t.TempDir(), "--json"); err != nil {
		t.Fatalf("default profile later request: %v output=%s", err, out)
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	out, err = runRemoteCLI(t, "enroll", "--home", t.TempDir(), "--invite-file", artifacts["file-team"], "--allow-insecure-http")
	if err == nil || strings.Contains(out, "invite consumed") || strings.Contains(out, "remove the local invite file") {
		t.Fatalf("definitive failure reported consumption: err=%v output=%s", err, out)
	}
	if after, readErr := os.ReadFile(artifacts["file-team"]); readErr != nil || !bytes.Equal(after, fileBytes) {
		t.Fatalf("definitive failure changed invite file: err=%v", readErr)
	}

	fdBytes, err := os.ReadFile(artifacts["fd-team"])
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(artifacts["fd-team"])
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	out, err = runRemoteCLI(t, "enroll", "--home", t.TempDir(), "--invite-fd", strconv.Itoa(int(input.Fd())), "--allow-insecure-http")
	if err != nil || strings.Contains(out, "invite consumed") || strings.Contains(out, "local invite file") {
		t.Fatalf("fd enrollment mentioned a local file: err=%v output=%s", err, out)
	}
	if after, readErr := os.ReadFile(artifacts["fd-team"]); readErr != nil || !bytes.Equal(after, fdBytes) {
		t.Fatalf("fd enrollment changed invite file: err=%v", readErr)
	}

	promptBytes, err := os.ReadFile(artifacts["prompt-team"])
	if err != nil {
		t.Fatal(err)
	}
	originalPrompt := readHiddenInvite
	readHiddenInvite = func() (string, error) { return strings.TrimSpace(string(promptBytes)), nil }
	t.Cleanup(func() { readHiddenInvite = originalPrompt })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	out, err = runRemoteCLI(t, "enroll", "--home", t.TempDir(), "--allow-insecure-http")
	if err != nil || strings.Contains(out, "invite consumed") || strings.Contains(out, "local invite file") {
		t.Fatalf("prompt enrollment mentioned a local file: err=%v output=%s", err, out)
	}
	if after, readErr := os.ReadFile(artifacts["prompt-team"]); readErr != nil || !bytes.Equal(after, promptBytes) {
		t.Fatalf("prompt enrollment changed invite file: err=%v", readErr)
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	out, err = runRemoteCLI(t, "enroll", "--home", t.TempDir(), "--invite-file", artifacts["json-team"], "--allow-insecure-http", "--json")
	var envelope map[string]any
	if err != nil || json.Unmarshal([]byte(out), &envelope) != nil || envelope["operation"] != "enroll" || strings.Contains(out, "invite consumed") || strings.Contains(out, "local invite file") {
		t.Fatalf("JSON enrollment output changed: err=%v output=%s", err, out)
	}
}

func TestUncertainEnrollmentPreservesInviteWithoutConsumptionGuidance(t *testing.T) {
	artifact, err := authority.EncodeInviteArtifact(authority.InviteArtifact{Endpoint: "http://authority.example", AuthorityID: strings.Repeat("a", 32), ProfileHint: "team", Invite: strings.Repeat("c", 64)})
	if err != nil {
		t.Fatal(err)
	}
	secretRoot := filepath.Join(t.TempDir(), "secrets")
	if err := handle.EnsureOwnerPrivateDir(secretRoot); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(secretRoot, "uncertain.invite")
	artifactBytes := []byte(artifact + "\n")
	if err := handle.WriteOwnerPrivateNoReplace(artifactPath, artifactBytes, authority.MaxInviteArtifactBytes+1); err != nil {
		t.Fatal(err)
	}
	originalTransport := http.DefaultTransport
	http.DefaultTransport = cliRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/.well-known/worklease" {
			body := `{"ok":true,"protocolVersion":"worklease-http/1","authorityId":"` + strings.Repeat("a", 32) + `","restoreId":"` + strings.Repeat("b", 32) + `","authorityTime":"2026-01-01T00:00:00Z","result":{"supportedProtocolVersions":["worklease-http/1"]}}`
			header := make(http.Header)
			header.Set("Content-Type", "application/json")
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}
		return nil, io.ErrUnexpectedEOF
	})
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, err := runRemoteCLI(t, "enroll", "--home", t.TempDir(), "--invite-file", artifactPath, "--allow-insecure-http")
	classified := reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonUnknownOutcome || strings.Contains(out, "invite consumed") || strings.Contains(out, "remove the local invite file") {
		t.Fatalf("uncertain enrollment output: err=%v output=%s", err, out)
	}
	if after, readErr := os.ReadFile(artifactPath); readErr != nil || !bytes.Equal(after, artifactBytes) {
		t.Fatalf("uncertain enrollment changed invite file: err=%v", readErr)
	}
}

func TestInviteIssueRejectsReservedLocalHintBeforeProfileLoading(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	out, err := runRemoteCLI(t, "invite", "issue", "--profile", "missing", "--label", "local", "--home", t.TempDir(), "--json")
	classified := reason.As(err)
	if classified == nil || classified.Reason != reason.ReasonConfigInvalid || !strings.Contains(out, "reserved for the built-in local authority") {
		t.Fatalf("reserved invite hint output=%s err=%v", out, err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "worklease")); !os.IsNotExist(statErr) {
		t.Fatalf("reserved invite hint touched profile or staging storage: %v", statErr)
	}
}

func TestInviteIssueDefaultsToPrivateArtifactAndPrintsEnrollCommand(t *testing.T) {
	profile, clientHome, _ := remoteCLIFixture(t)
	paths := config.UserProfilePaths(os.Getenv)
	expected := filepath.Join(filepath.Dir(paths.Profiles), profile+".invite")
	out, err := runRemoteCLI(t, "invite", "issue", "--profile", profile, "--home", clientHome, "--json")
	if err != nil {
		t.Fatalf("default invite issue: %v output=%s", err, out)
	}
	artifactData, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("default artifact: %v", err)
	}
	artifact, err := authority.DecodeInviteArtifact(strings.TrimSpace(string(artifactData)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, artifact.Invite) || !strings.Contains(out, expected) || !strings.Contains(out, "worklease enroll --invite-file") {
		t.Fatalf("default invite output leaked or omitted handoff: %s", out)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || envelope["artifactPath"] != expected {
		t.Fatalf("default invite envelope=%v err=%v", envelope, err)
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
