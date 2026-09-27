package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/authority"
	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/runs"
	"github.com/brettinternet/worklease/internal/store"
)

func claimsLocalFixture(t *testing.T) (*authorityContext, lease.ClaimView, handle.Handle, string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	home := t.TempDir()
	st, err := store.Open(context.Background(), home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	svc := lease.New(st, nil, nil, lease.Defaults{TTL: time.Minute})
	api, err := authority.NewLocalAuthority(svc, st, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	claimID, token := strings.Repeat("a", 32), strings.Repeat("b", 64)
	grant, err := svc.Acquire(context.Background(), lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Resources: []string{"coordination:claim"}, AgentID: "same-agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	status, err := svc.Status(context.Background(), lease.Selector{ClaimID: claimID})
	if err != nil || status.Claim == nil {
		t.Fatalf("status: %v %+v", err, status)
	}
	h := handle.Handle{SchemaVersion: handle.SchemaVersion, AuthorityID: st.AuthorityID(), ClaimID: claimID, Token: token, Revision: grant.Revision, Resources: grant.Resources, ExpiresAt: grant.ExpiresAt, AgentID: "same-agent", SessionID: "session", State: "ready"}
	path := filepath.Join(home, "handles", "ctx-"+strings.Repeat("c", 64)+".json")
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := handle.Write(path, h); err != nil {
		t.Fatal(err)
	}
	backend := &authorityContext{API: authorityWithDefaults{Authority: api, ttl: time.Minute, local: svc}, Local: svc, Store: st, Config: config.Config{Home: home, TTL: time.Minute}}
	return backend, *status.Claim, h, path
}

func TestClaimsHandleIndexOwnershipAndUnavailableReasons(t *testing.T) {
	backend, claim, h, path := claimsLocalFixture(t)
	home := backend.Config.Home
	index := claimsHandleIndex(home, backend.AuthorityID(), "", []lease.ClaimView{claim})
	if got := index[claim.ClaimID]; got.Path != path || got.Unavailable != "" || got.Kind != "contextual" {
		t.Fatalf("contextual handle=%+v", got)
	}
	mcpPath := filepath.Join(home, "handles", "mcp-"+strings.Repeat("d", 32)+".json")
	if err := handle.Write(mcpPath, h); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := claimsHandleIndex(home, backend.AuthorityID(), "", []lease.ClaimView{claim})[claim.ClaimID]; got.Path != mcpPath || got.Unavailable != "" || got.Kind != "MCP" {
		t.Fatalf("MCP handle=%+v", got)
	}
	if err := os.Remove(mcpPath); err != nil {
		t.Fatal(err)
	}
	if got := claimsHandleIndex(home, backend.AuthorityID(), "", []lease.ClaimView{claim})[claim.ClaimID]; !strings.Contains(got.Unavailable, "not held here") {
		t.Fatalf("agent identity must not prove ownership: %+v", got)
	}
	if err := handle.Write(path, h); err != nil {
		t.Fatal(err)
	}
	h.State = "pending"
	h.PendingRequest = &handle.PendingRequest{OperationID: strings.Repeat("e", 32), Kind: "heartbeat", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: strings.Repeat("f", 64), RequestNotAfter: time.Now().Add(time.Hour), Inputs: map[string]any{"ttl": int64(60)}}
	if err := handle.Write(path, h); err != nil {
		t.Fatal(err)
	}
	if got := claimsHandleIndex(home, backend.AuthorityID(), "", []lease.ClaimView{claim})[claim.ClaimID]; !strings.Contains(got.Unavailable, "worklease heartbeat --handle "+path) {
		t.Fatalf("pending handle=%+v", got)
	}
	h.State, h.PendingRequest = "ready", nil
	if err := handle.Write(path, h); err != nil {
		t.Fatal(err)
	}
	queuePath := filepath.Join(home, "queue-handles", "session", "claim.json")
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(queuePath)); err != nil {
		t.Fatal(err)
	}
	if err := handle.Write(queuePath, h); err != nil {
		t.Fatal(err)
	}
	if got := claimsHandleIndex(home, backend.AuthorityID(), "", []lease.ClaimView{claim})[claim.ClaimID]; !strings.Contains(got.Unavailable, "queue-owned") {
		t.Fatalf("queue-owned handle=%+v", got)
	}
	if err := os.Remove(queuePath); err != nil {
		t.Fatal(err)
	}
	record := runs.Record{SchemaVersion: 1, ID: runs.NewID(time.Now(), "12345678"), Name: "run", Argv: []string{"true"}, Dir: home, State: runs.StateRunning, SupervisorPID: os.Getpid(), StartedAt: time.Now(), UpdatedAt: time.Now(), Claim: &runs.Claim{AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, State: runs.ClaimHeld}}
	if err := runStore().Create(record); err != nil {
		t.Fatal(err)
	}
	if got := claimsHandleIndex(home, backend.AuthorityID(), "", []lease.ClaimView{claim})[claim.ClaimID]; !strings.Contains(got.Unavailable, "runs stop") {
		t.Fatalf("supervised handle=%+v", got)
	}
}

func TestClaimsDispatchRefusesPostPreviewHandleAndOwnerDrift(t *testing.T) {
	backend, claim, original, path := claimsLocalFixture(t)
	selected := claimsHandleIndex(backend.Config.Home, backend.AuthorityID(), "", []lease.ClaimView{claim})[claim.ClaimID]
	refuse := func(want string) {
		t.Helper()
		if _, err := mutateClaimsHandle(context.Background(), backend, claim, selected, false, ""); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q refusal: %v", want, err)
		}
	}
	for _, changed := range []struct {
		handle handle.Handle
		want   string
	}{
		{func() handle.Handle { h := original; h.ClaimID = strings.Repeat("d", 32); return h }(), "claim ID"},
		{func() handle.Handle { h := original; h.Revision++; return h }(), "revision"},
		{func() handle.Handle { h := original; h.AuthorityID = strings.Repeat("d", 32); return h }(), "authority"},
	} {
		// A different owner can remove/recreate this slot between preview and
		// confirmation; Write alone intentionally rejects identity replacement.
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := handle.Write(path, changed.handle); err != nil {
			t.Fatal(err)
		}
		refuse(changed.want)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := handle.Write(path, original); err != nil {
		t.Fatal(err)
	}
	queuePath := filepath.Join(backend.Config.Home, "queue-handles", "session", "claim.json")
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(queuePath)); err != nil {
		t.Fatal(err)
	}
	if err := handle.Write(queuePath, original); err != nil {
		t.Fatal(err)
	}
	refuse("queue-owned")
	if err := os.Remove(queuePath); err != nil {
		t.Fatal(err)
	}
	record := runs.Record{ID: runs.NewID(time.Now(), "12345678"), Name: "worker", Argv: []string{"true"}, Dir: backend.Config.Home, State: runs.StateRunning, SupervisorPID: os.Getpid(), StartedAt: time.Now(), UpdatedAt: time.Now(), Claim: &runs.Claim{AuthorityID: original.AuthorityID, ClaimID: original.ClaimID, State: runs.ClaimHeld}}
	if err := runStore().Create(record); err != nil {
		t.Fatal(err)
	}
	refuse("supervised")
	current, err := handle.Read(path)
	if err != nil || current.State != "ready" || current.Revision != original.Revision {
		t.Fatalf("refusal changed handle: %+v %v", current, err)
	}
}

func TestClaimsRemoteHandleRestoreIdentityAndLifecycle(t *testing.T) {
	profileName, home, _ := remoteCLIFixture(t)
	path := filepath.Join(home, "handles", "mcp-"+strings.Repeat("d", 32)+".json")
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if _, err := runRemoteCLI(t, "acquire", "--profile", profileName, "--home", home, "--handle", path, "--resource", "coordination:claims-tui", "--ttl", "30s", "--json"); err != nil {
		t.Fatal(err)
	}
	profiles, _, err := config.LoadProfiles(config.UserProfilePaths(os.Getenv))
	if err != nil {
		t.Fatal(err)
	}
	profile := profiles[profileName]
	client, err := authority.NewHTTPClient(profile, authority.NewFilePendingStore(filepath.Join(home, "pending", profileName)), nil)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := authority.NewRemoteAuthority(client)
	if err != nil {
		t.Fatal(err)
	}
	backend := &authorityContext{API: authorityWithDefaults{Authority: remote, ttl: time.Minute}, Config: config.Config{Home: home, TTL: time.Minute}, Profile: &profile, HTTP: client, ProfileName: profileName, Remote: true}
	claims, err := backend.API.List(context.Background(), "", nil)
	if err != nil || len(claims) != 1 {
		t.Fatalf("remote list: %+v %v", claims, err)
	}
	claim := claims[0]
	selected := claimsHandleIndex(home, backend.AuthorityID(), profile.RestoreID, claims)[claim.ClaimID]
	if selected.Unavailable != "" || selected.Kind != "MCP" {
		t.Fatalf("remote MCP handle=%+v", selected)
	}
	h, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	original := h
	h.RestoreID = strings.Repeat("a", 32)
	if h.RestoreID == profile.RestoreID {
		h.RestoreID = strings.Repeat("b", 32)
	}
	if err := handle.Write(path, h); err != nil {
		t.Fatal(err)
	}
	if got := claimsHandleIndex(home, backend.AuthorityID(), profile.RestoreID, claims)[claim.ClaimID]; !strings.Contains(got.Unavailable, "restore ID mismatch") {
		t.Fatalf("restore mismatch=%+v", got)
	}
	if _, err := mutateClaimsHandle(context.Background(), backend, claim, selected, false, ""); err == nil || !strings.Contains(err.Error(), "restore ID") {
		t.Fatalf("restore drift not refused: %v", err)
	}
	if err := handle.Write(path, original); err != nil {
		t.Fatal(err)
	}
	updated, err := mutateClaimsHandle(context.Background(), backend, claim, selected, false, "")
	if err != nil || updated.Revision != claim.Revision+1 {
		t.Fatalf("remote renew=%+v %v", updated, err)
	}
	selected = claimsHandleIndex(home, backend.AuthorityID(), profile.RestoreID, []lease.ClaimView{updated})[claim.ClaimID]
	if _, err := mutateClaimsHandle(context.Background(), backend, updated, selected, true, "finished"); err != nil {
		t.Fatalf("remote release: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("remote release retained handle: %v", err)
	}
}

type ambiguousClaimsAuthority struct{ commandAuthority }

func (a ambiguousClaimsAuthority) Heartbeat(context.Context, lease.Credentials, lease.Renew) (lease.Receipt, error) {
	return lease.Receipt{}, errors.New("connection lost after dispatch")
}

func TestClaimsAmbiguousHeartbeatRetainsPendingHandle(t *testing.T) {
	backend, claim, _, path := claimsLocalFixture(t)
	selected := claimsHandleIndex(backend.Config.Home, backend.AuthorityID(), "", []lease.ClaimView{claim})[claim.ClaimID]
	backend.API = ambiguousClaimsAuthority{backend.API}
	if _, err := mutateClaimsHandle(context.Background(), backend, claim, selected, false, ""); err == nil || !strings.Contains(err.Error(), "outcome uncertain") || !strings.Contains(err.Error(), "worklease heartbeat --handle "+path) {
		t.Fatalf("ambiguous outcome=%v", err)
	}
	h, err := handle.Read(path)
	if err != nil || h.State != "pending" || h.PendingRequest == nil || h.PendingRequest.Kind != "heartbeat" {
		t.Fatalf("ambiguous handle=%+v %v", h, err)
	}
}

type ambiguousClaimsReleaseAuthority struct{ commandAuthority }

func (a ambiguousClaimsReleaseAuthority) Release(context.Context, lease.Credentials, lease.ReleaseRequest) (lease.Receipt, error) {
	return lease.Receipt{}, errors.New("connection lost after release dispatch")
}

func TestClaimsAmbiguousReleaseShowsReplayableReason(t *testing.T) {
	backend, claim, _, path := claimsLocalFixture(t)
	selected := claimsHandleIndex(backend.Config.Home, backend.AuthorityID(), "", []lease.ClaimView{claim})[claim.ClaimID]
	backend.API = ambiguousClaimsReleaseAuthority{backend.API}
	if _, err := mutateClaimsHandle(context.Background(), backend, claim, selected, true, "finished work"); err == nil || !strings.Contains(err.Error(), "--reason 'finished work'") || !strings.Contains(err.Error(), "worklease release --handle "+path) {
		t.Fatalf("missing exact recovery command: %v", err)
	}
	h, err := handle.Read(path)
	if err != nil || h.PendingRequest == nil || h.PendingRequest.Kind != "release" {
		t.Fatalf("release did not retain pending handle: %+v %v", h, err)
	}
	recovery := h
	recovery.RecoveryRequest = &handle.RecoveryRequest{}
	if guidance := claimsRecoveryCommand(recovery, path, backend.Config.Home, nil); !strings.Contains(guidance, "worklease handle inspect --handle "+path) || strings.Contains(guidance, "worklease heartbeat --handle") {
		t.Fatalf("reconciliation guidance: %s", guidance)
	}
	var out, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "release", "--handle", path, "--home", backend.Config.Home, "--local", "--reason", "finished work"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("displayed recovery command could not replay: %v %s", err, stderr.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("release recovery retained handle: %v", err)
	}
}

func TestClaimsHandleDispatchRejectsDriftAndUpdatesLifecycle(t *testing.T) {
	backend, claim, h, path := claimsLocalFixture(t)
	selected := claimsHandleIndex(backend.Config.Home, backend.AuthorityID(), "", []lease.ClaimView{claim})[claim.ClaimID]
	assertRefusal := func(snapshot lease.ClaimView, picked queueui.ClaimsHandle, want string) {
		t.Helper()
		if _, err := mutateClaimsHandle(context.Background(), backend, snapshot, picked, false, ""); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q refusal, got %v", want, err)
		}
		current, err := handle.Read(path)
		if err != nil || current.State != "ready" || current.Revision != h.Revision {
			t.Fatalf("refusal mutated handle: %+v %v", current, err)
		}
	}
	changed := claim
	changed.ClaimID = strings.Repeat("d", 32)
	assertRefusal(changed, selected, "claim ID")
	changed = claim
	changed.Revision++
	assertRefusal(changed, selected, "revision")
	changed = claim
	changed.AuthorityID = strings.Repeat("d", 32)
	assertRefusal(changed, selected, "authority")
	changed = claim
	selected.AuthorityID = strings.Repeat("d", 32)
	assertRefusal(changed, selected, "authority")
	selected.AuthorityID = backend.AuthorityID()
	updated, err := mutateClaimsHandle(context.Background(), backend, claim, selected, false, "")
	if err != nil || updated.Revision != claim.Revision+1 || updated.ExpiresAt.Before(claim.ExpiresAt) {
		t.Fatalf("renew result=%+v error=%v", updated, err)
	}
	if _, err := mutateClaimsHandle(context.Background(), backend, claim, selected, true, "released"); err == nil || !strings.Contains(err.Error(), "revision") {
		t.Fatalf("stale preview must refuse release: %v", err)
	}
	selected = claimsHandleIndex(backend.Config.Home, backend.AuthorityID(), "", []lease.ClaimView{updated})[updated.ClaimID]
	if _, err := mutateClaimsHandle(context.Background(), backend, updated, selected, true, "finished"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("release did not remove handle: %v", err)
	}
	events, err := backend.API.Events(context.Background(), "", 20)
	if err != nil || len(events.Events) == 0 {
		t.Fatalf("release lifecycle history unavailable: %+v %v", events.Events, err)
	}
	last := events.Events[len(events.Events)-1]
	if last.Kind != "released" || last.Detail["reason"] != "finished" {
		t.Fatalf("release reason not recorded in lifecycle history: %+v", last)
	}
}
