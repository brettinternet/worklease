package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/testkit"
)

func TestQueueAdapterApprovalIsExplicitDurableAndSourceBound(t *testing.T) {
	t.Parallel()
	source, env, _ := queueAdapterApprovalFixture(t)
	marker := filepath.Join(filepath.Dir(source.Executable), "invoked")
	if err := os.WriteFile(source.Executable, []byte("#!/bin/sh\nprintf invoked > "+marker+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ApproveQueueAdapter(context.Background(), env, source); err != nil {
		t.Fatalf("explicit approval: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("approval invoked the executable: %v", err)
	}
	if err := CheckQueueAdapterApproval(env, source); err != nil {
		t.Fatalf("approved executable rejected: %v", err)
	}
	// Existing opaque-reference approvals must retain their original binding
	// digest when the optional helper is not configured.
	oldBinding, err := json.Marshal(struct {
		Config        map[string]any `json:"config"`
		CredentialRef string         `json:"credentialRef"`
	}{source.Config, source.CredentialRef})
	if err != nil {
		t.Fatal(err)
	}
	oldDigest := sha256.Sum256(oldBinding)
	entry, err := currentQueueAdapterApproval(source)
	if err != nil || entry.ConfigSHA256 != hex.EncodeToString(oldDigest[:]) {
		t.Fatalf("legacy approval binding changed: %+v %v", entry, err)
	}
	data, err := os.ReadFile(QueueAdapterApprovalPath(env))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "credential:must-not-persist") || strings.Contains(string(data), "provider-secret") {
		t.Fatalf("approval record included source credentials or config: %s", data)
	}
	if !strings.Contains(string(data), `"claims":null`) {
		t.Fatalf("approval record omitted the unbound claim configuration: %s", data)
	}
	info, err := os.Stat(QueueAdapterApprovalPath(env))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("approval record is not private: mode=%v err=%v", info, err)
	}
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("approval record is not durable JSON: %v", err)
	}

	changedSource := source
	changedSource.ID = "another-source"
	if err := CheckQueueAdapterApproval(env, changedSource); err == nil {
		t.Fatal("approval was shared across sources")
	}
	changedSource = source
	changedSource.ExpectedVersion = "2.0.0"
	if err := CheckQueueAdapterApproval(env, changedSource); err == nil {
		t.Fatal("approval ignored expected version")
	}
	changedSource = source
	changedSource.ExpectedAdapterID = "other.adapter"
	if err := CheckQueueAdapterApproval(env, changedSource); err == nil {
		t.Fatal("approval ignored expected adapter ID")
	}
	changedSource = source
	changedSource.Config = map[string]any{"password": "new-provider-secret"}
	if err := CheckQueueAdapterApproval(env, changedSource); err == nil {
		t.Fatal("approval ignored changed source configuration")
	}
	changedSource = source
	changedSource.CredentialRef = "different-reference"
	if err := CheckQueueAdapterApproval(env, changedSource); err == nil {
		t.Fatal("approval ignored changed credential reference")
	}
	changedSource = source
	changedSource.Claims = &QueueClaims{Policy: "generic", Source: "acme/planning"}
	if err := CheckQueueAdapterApproval(env, changedSource); err == nil {
		t.Fatal("approval ignored a newly configured claim binding")
	}
	if err := ApproveQueueAdapter(context.Background(), env, changedSource); err != nil {
		t.Fatalf("reapprove changed claim binding: %v", err)
	}
	if err := CheckQueueAdapterApproval(env, changedSource); err != nil {
		t.Fatalf("reapproved generic claim binding rejected: %v", err)
	}
	data, err = os.ReadFile(QueueAdapterApprovalPath(env))
	if err != nil || !strings.Contains(string(data), `"claims":{"policy":"generic","source":"acme/planning"}`) || !strings.Contains(string(data), `"account":"alice"`) || !strings.Contains(string(data), `"workflow":{"start":"Doing"}`) {
		t.Fatalf("approval did not persist the exact claims, account, and workflow binding: %s %v", data, err)
	}
	if err := CheckQueueAdapterApproval(env, source); err == nil {
		t.Fatal("approval retained the old unbound claim configuration after reapproval")
	}
	changedSource.Claims.Source = "acme/other"
	if err := CheckQueueAdapterApproval(env, changedSource); err == nil {
		t.Fatal("approval ignored claim source drift")
	}
	changedSource = source
	changedSource.Claims = &QueueClaims{Policy: "generic", Source: "acme/planning"}
	changedSource.Account = "bob"
	if err := CheckQueueAdapterApproval(env, changedSource); err == nil {
		t.Fatal("approval ignored account drift")
	}
	changedSource = source
	changedSource.Claims = &QueueClaims{Policy: "generic", Source: "acme/planning"}
	changedSource.Workflow["start"] = "In Progress"
	if err := CheckQueueAdapterApproval(env, changedSource); err == nil {
		t.Fatal("approval ignored workflow drift")
	}
	invalidAccount := source
	invalidAccount.Account = "alice\nbob"
	if err := ApproveQueueAdapter(context.Background(), env, invalidAccount); err == nil {
		t.Fatal("approved unsafe external account")
	}
	invalidClaims := source
	invalidClaims.Claims = &QueueClaims{Policy: "github", Source: "acme/planning"}
	if err := ApproveQueueAdapter(context.Background(), env, invalidClaims); err == nil {
		t.Fatal("approved an unsupported external claim policy")
	}
	unapproved := source
	unapproved.Executable = filepath.Join(filepath.Dir(source.Executable), "unapproved")
	if err := os.WriteFile(unapproved.Executable, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(unapproved.Executable)
	if err != nil {
		t.Fatal(err)
	}
	unapproved.Executable = canonical
	if err := CheckQueueAdapterApproval(env, unapproved); err == nil {
		t.Fatal("unapproved executable was accepted")
	}
}

func TestQueueAdapterApprovalRejectsTamperingAndChangedExecutable(t *testing.T) {
	t.Parallel()
	source, env, _ := queueAdapterApprovalFixture(t)
	if err := os.WriteFile(source.Executable, []byte("#!/bin/sh\necho approved\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ApproveQueueAdapter(context.Background(), env, source); err != nil {
		t.Fatal(err)
	}
	approvalPath := QueueAdapterApprovalPath(env)
	data, err := os.ReadFile(approvalPath)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := currentQueueAdapterApproval(source)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(data), entry.SHA256, strings.Repeat("0", 64), 1)
	if tampered == string(data) {
		t.Fatal("test failed to alter approval digest")
	}
	if err := os.WriteFile(approvalPath, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckQueueAdapterApproval(env, source); err == nil {
		t.Fatal("tampered approval digest was accepted")
	}
	if err := os.WriteFile(approvalPath, []byte(`{"version":1,"approvals":[],"unexpected":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckQueueAdapterApproval(env, source); err == nil {
		t.Fatal("unknown approval file key was accepted")
	}
	if err := os.WriteFile(approvalPath, []byte(`{"version":1,"version":1,"approvals":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckQueueAdapterApproval(env, source); err == nil {
		t.Fatal("duplicate approval file key was accepted")
	}

	if err := ApproveQueueAdapter(context.Background(), env, source); err == nil {
		t.Fatal("corrupt approval record was overwritten implicitly")
	}
	if err := os.WriteFile(approvalPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source.Executable, []byte("#!/bin/sh\necho changed\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CheckQueueAdapterApproval(env, source); err == nil {
		t.Fatal("changed executable was accepted")
	}
}

func TestQueueAdapterLaunchSnapshotCopiesApprovedBytesAndChecksSourceBinding(t *testing.T) {
	t.Parallel()
	source, env, _ := queueAdapterApprovalFixture(t)
	contents := []byte("#!/bin/sh\nprintf approved\n")
	if err := os.WriteFile(source.Executable, contents, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ApproveQueueAdapter(context.Background(), env, source); err != nil {
		t.Fatal(err)
	}
	snapshot, err := PrepareQueueAdapterLaunch(env, source)
	if err != nil {
		t.Fatalf("prepare approved snapshot: %v", err)
	}
	path := snapshot.Path()
	if path == source.Executable {
		t.Fatal("launch snapshot reused the configured executable path")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, contents) {
		t.Fatalf("snapshot bytes = %q, want %q (err=%v)", got, contents, err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil || fileInfo.Mode().Perm() != 0o500 {
		t.Fatalf("snapshot mode = %v, err=%v; want owner read/execute only", fileInfo, err)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("snapshot directory mode = %v, err=%v; want owner-only", dirInfo, err)
	}
	for name, changed := range map[string]QueueSource{
		"config": func() QueueSource {
			copy := source
			copy.Config = map[string]any{"password": "changed"}
			return copy
		}(),
		"credentialRef": func() QueueSource {
			copy := source
			copy.CredentialRef = "changed-reference"
			return copy
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := PrepareQueueAdapterLaunch(env, changed); err == nil {
				t.Fatalf("snapshot accepted changed %s binding", name)
			}
		})
	}
	if err := snapshot.Close(); err != nil {
		t.Fatalf("remove snapshot: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("snapshot executable was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("snapshot directory was not removed: %v", err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatalf("idempotent snapshot close: %v", err)
	}
}

func TestQueueAdapterLaunchSnapshotBoundsCopiedBytes(t *testing.T) {
	t.Parallel()
	source, env, _ := queueAdapterApprovalFixture(t)
	if err := os.Truncate(source.Executable, maxQueueAdapterExecutableSize+1); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source.Executable, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareQueueAdapterLaunch(env, source); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized executable error = %v", err)
	}
}

func TestQueueAdapterApprovalRequiresExecutableAndRejectsSymlinks(t *testing.T) {
	t.Parallel()
	source, env, _ := queueAdapterApprovalFixture(t)
	if err := os.WriteFile(source.Executable, []byte("not executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source.Executable, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ApproveQueueAdapter(context.Background(), env, source); err == nil {
		t.Fatal("non-executable file was approved")
	}
	if err := os.Chmod(source.Executable, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ApproveQueueAdapter(context.Background(), env, source); err != nil {
		t.Fatal(err)
	}
	redirect := filepath.Join(filepath.Dir(source.Executable), "redirect")
	if err := os.Symlink(source.Executable, redirect); err != nil {
		t.Fatal(err)
	}
	linked := source
	linked.Executable = redirect
	if err := CheckQueueAdapterApproval(env, linked); err == nil {
		t.Fatal("symlink executable path was accepted")
	}
	if err := os.Remove(source.Executable); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(redirect, source.Executable); err != nil {
		t.Fatal(err)
	}
	if err := CheckQueueAdapterApproval(env, source); err == nil {
		t.Fatal("redirected executable path was accepted")
	}
}

func TestQueueAdapterApprovalFileMustRemainOwnerPrivate(t *testing.T) {
	t.Parallel()
	source, env, _ := queueAdapterApprovalFixture(t)
	if err := os.WriteFile(source.Executable, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ApproveQueueAdapter(context.Background(), env, source); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(QueueAdapterApprovalPath(env), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckQueueAdapterApproval(env, source); err == nil {
		t.Fatal("world-readable approval record was accepted")
	}
}

func queueAdapterApprovalFixture(t *testing.T) (QueueSource, func(string) string, string) {
	t.Helper()
	_, paths := testkit.Home(t)
	env := func(key string) string { return paths[key] }
	executable := filepath.Join(paths["HOME"], "adapter")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	source := QueueSource{
		ID:                "source-a",
		Adapter:           "external",
		Executable:        canonical,
		ExpectedAdapterID: "example.adapter",
		ExpectedVersion:   "1.2.3",
		Account:           "alice",
		Workflow:          map[string]string{"start": "Doing"},
		Config:            map[string]any{"password": "provider-secret"},
		CredentialRef:     "credential:must-not-persist",
	}
	return source, env, paths["HOME"]
}
