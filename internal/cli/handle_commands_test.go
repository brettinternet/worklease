package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/store"
)

func handleCommandFixture(t *testing.T, state string, recovery bool) (string, string, handle.Handle) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	st, err := store.Open(context.Background(), home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	authorityID := st.AuthorityID()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "handles")
	if err := handle.EnsureOwnerPrivateDir(directory); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "selected.json")
	h := handle.Handle{SchemaVersion: handle.RemoteSchemaVersion, AuthorityID: strings.Repeat("b", 32), ClaimID: strings.Repeat("c", 32), Token: strings.Repeat("d", 64), Revision: 7, Resources: []string{"coordination:one", "coordination:two"}, ExpiresAt: time.Now().UTC().Add(time.Hour), AgentID: "agent", SessionID: "claim-session", LocalReplaceAllowed: false, State: state}
	if state == "pending" {
		h.PendingRequest = &handle.PendingRequest{OperationID: strings.Repeat("e", 32), Kind: "heartbeat", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: strings.Repeat("f", 64), RequestNotAfter: time.Now().UTC().Add(time.Hour), Request: []byte("private request bytes"), EffectEvidence: json.RawMessage(`{"private":"evidence"}`), Inputs: map[string]any{"ttl": int64(1)}}
	}
	if recovery {
		h.RecoveryRequest = &handle.RecoveryRequest{OperationID: strings.Repeat("1", 32), TargetClaimID: h.ClaimID, TargetOperationID: strings.Repeat("2", 32), RequestHash: strings.Repeat("3", 64), RequestNotAfter: time.Now().UTC().Add(time.Hour), Outcome: "observed-failure", Evidence: json.RawMessage(`{"private":"recovery evidence"}`)}
	}
	if err := handle.Write(path, h); err != nil {
		t.Fatal(err)
	}
	if authorityID == h.AuthorityID {
		t.Fatal("fixture authority unexpectedly matches")
	}
	return home, path, h
}

func runHandleCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), append([]string{"worklease"}, args...), "dev", "unknown", "unknown", &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func TestHandleInspectIsOfflineRedactedAndNonMutating(t *testing.T) {
	home, path, h := handleCommandFixture(t, "pending", true)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runHandleCommand(t, "--home", home, "--local", "--json", "handle", "inspect", "--handle", path)
	if err != nil {
		t.Fatalf("inspect: %v stderr=%q", err, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"operation": "handle inspect", "authorityId": h.AuthorityID, "claimId": h.ClaimID, "recordedState": "pending", "selectorType": "explicit-handle", "selectorProvenance": "unknown", "authorityMatchesSelection": false, "authorityStatus": "not-contacted", "recordedExpiryIsProof": false, "pendingRequestPresent": true, "recoveryRequestPresent": true} {
		if result[key] != want {
			t.Errorf("%s=%#v want %#v", key, result[key], want)
		}
	}
	for _, secret := range []string{h.Token, "private request bytes", "evidence", strings.Repeat("f", 64)} {
		if strings.Contains(stdout, secret) || strings.Contains(stderr, secret) {
			t.Fatalf("inspection leaked %q: stdout=%q stderr=%q", secret, stdout, stderr)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("inspection changed handle: equal=%v err=%v", bytes.Equal(before, after), err)
	}
	if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspection created lock: %v", err)
	}
}

func TestHandleArchiveRequiresAcknowledgementAndPreservesExactRecoveryState(t *testing.T) {
	home, source, h := handleCommandFixture(t, "pending", true)
	archiveDirectory := filepath.Join(t.TempDir(), "archive space")
	if err := handle.EnsureOwnerPrivateDir(archiveDirectory); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(archiveDirectory, "saved.json")
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	_, stderr, err := runHandleCommand(t, "--home", home, "--local", "handle", "archive", "--handle", source, "--destination", destination)
	if err == nil || !strings.Contains(err.Error(), "acknowledge-pending-recovery") {
		t.Fatalf("archive without acknowledgement err=%v stderr=%q", err, stderr)
	}
	if current, readErr := os.ReadFile(source); readErr != nil || !bytes.Equal(current, before) {
		t.Fatalf("refusal changed source: equal=%v err=%v", bytes.Equal(current, before), readErr)
	}
	if _, statErr := os.Stat(destination); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("refusal created destination: %v", statErr)
	}
	stdout, stderr, err := runHandleCommand(t, "--home", home, "--local", "--json", "handle", "archive", "--handle", source, "--destination", destination, "--acknowledge-pending-recovery")
	if err != nil {
		t.Fatalf("archive: %v stderr=%q", err, stderr)
	}
	if strings.Contains(stdout, h.Token) || strings.Contains(stdout, "private request bytes") || strings.Contains(stdout, "recovery evidence") {
		t.Fatalf("archive output leaked private state: %q", stdout)
	}
	archived, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(archived, before) {
		t.Fatalf("archive changed bytes: equal=%v err=%v", bytes.Equal(archived, before), err)
	}
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source remains: %v", err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("archive permissions=%v", info.Mode().Perm())
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result["authorityMutation"] != "none" || result["claimMayRemainActive"] != true || result["archivePath"] != destination || result["recoveryCommand"] != "worklease handle inspect --handle "+shellQuote(destination) {
		t.Fatalf("archive result=%#v", result)
	}
}

func TestHandleArchiveRejectsContextualDestination(t *testing.T) {
	home, source, _ := handleCommandFixture(t, "ready", false)
	directory := filepath.Join(home, "handles")
	if err := handle.EnsureOwnerPrivateDir(directory); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(directory, "ctx-"+strings.Repeat("a", 64)+".json")
	if _, _, err := runHandleCommand(t, "--home", home, "--local", "handle", "archive", "--handle", source, "--destination", destination); err == nil {
		t.Fatal("archived into contextual handle directory")
	}
	if _, err := handle.Read(source); err != nil {
		t.Fatalf("rejected destination changed source: %v", err)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected destination was created: %v", err)
	}
}

func TestHandleInspectMissingContextDoesNotCreateState(t *testing.T) {
	home := filepath.Join(t.TempDir(), "missing-home")
	if _, _, err := runHandleCommand(t, "--home", home, "--local", "handle", "inspect"); err == nil {
		t.Fatal("inspected missing contextual handle")
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("inspection created missing home: %v", err)
	}
}

func TestHandleArchiveReadyNoOverwriteAndInputFailures(t *testing.T) {
	home, source, _ := handleCommandFixture(t, "ready", false)
	archiveDirectory := filepath.Join(t.TempDir(), "archive")
	if err := handle.EnsureOwnerPrivateDir(archiveDirectory); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(archiveDirectory, "saved.json")
	if err := os.WriteFile(destination, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runHandleCommand(t, "--home", home, "--local", "handle", "archive", "--handle", source, "--destination", destination); err == nil {
		t.Fatal("archive overwrote destination")
	}
	if _, err := handle.Read(source); err != nil {
		t.Fatalf("collision changed source: %v", err)
	}
	readyDestination := filepath.Join(archiveDirectory, "ready.json")
	if _, stderr, err := runHandleCommand(t, "--home", home, "--local", "handle", "archive", "--handle", source, "--destination", readyDestination); err != nil {
		t.Fatalf("ready archive required acknowledgement: %v stderr=%q", err, stderr)
	}
	if _, err := handle.Read(readyDestination); err != nil {
		t.Fatalf("ready archive is not recoverable: %v", err)
	}
	malformed := filepath.Join(filepath.Dir(source), "malformed.json")
	if err := os.WriteFile(malformed, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runHandleCommand(t, "--home", home, "--local", "handle", "inspect", "--handle", malformed); err == nil {
		t.Fatal("inspected malformed handle")
	}
	unsafe := filepath.Join(filepath.Dir(source), "unsafe.json")
	if err := os.WriteFile(unsafe, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runHandleCommand(t, "--home", home, "--local", "handle", "inspect", "--handle", unsafe); err == nil {
		t.Fatal("inspected unsafe handle")
	}
	if _, _, err := runHandleCommand(t, "--home", home, "--local", "handle", "inspect", "--handle", filepath.Join(filepath.Dir(source), "missing.json")); err == nil {
		t.Fatal("inspected missing handle")
	}
}
