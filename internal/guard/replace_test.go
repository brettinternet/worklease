package guard

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/resource"
	"github.com/brettinternet/worklease/internal/store"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestReplaceFileRejectsSymlinkTargetBeforeCanonicalization(t *testing.T) {
	root := t.TempDir()
	referent := filepath.Join(root, "referent.txt")
	target := filepath.Join(root, "target.txt")
	content := filepath.Join(root, "content.txt")
	if err := os.WriteFile(referent, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(referent, target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(content, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), filepath.Join(root, "state"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, nil, nil, lease.Defaults{TTL: time.Minute})
	sum := sha256.Sum256([]byte("old"))
	_, err = ReplaceFile(context.Background(), svc, lease.Credentials{}, ReplaceRequest{
		OperationID: strings.Repeat("9", 32), Path: target,
		ExpectedSHA256: hex.EncodeToString(sum[:]), ContentFile: content,
		TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour),
	})
	if got := reason.As(err); got == nil || got.Reason != reason.ReasonInvalidPath {
		t.Fatalf("symlink target error=%v", err)
	}
	if got, err := os.ReadFile(referent); err != nil || string(got) != "old" {
		t.Fatalf("referent changed: %q, %v", got, err)
	}
}

func TestReplaceFileExpectedHashPreservesModeAndReplay(t *testing.T) {
	root := t.TempDir()
	if out, err := testkit.GitCommand("-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	target := filepath.Join(root, "target.txt")
	content := filepath.Join(root, "content.txt")
	if err := os.WriteFile(target, []byte("old"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(content, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, err := resource.Resolve(resource.Input{Path: target})
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), filepath.Join(root, "state"), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := lease.New(st, nil, nil, lease.Defaults{TTL: time.Minute})
	token := strings.Repeat("a", 64)
	claim := strings.Repeat("1", 32)
	grant, err := svc.Acquire(context.Background(), lease.AcquireRequest{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Resources: []string{key.Resource}, AgentID: "agent", SessionID: "session", TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour), LocalReplaceAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("old"))
	expected := hex.EncodeToString(sum[:])
	deadline := time.Now().Add(time.Hour)
	creds := lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Revision: grant.Revision}
	op := strings.Repeat("2", 32)
	first, err := ReplaceFile(context.Background(), svc, creds, ReplaceRequest{OperationID: op, Path: target, ExpectedSHA256: expected, ContentFile: content, TTL: time.Minute, RequestNotAfter: deadline})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Receipt.Committed {
		t.Fatal("replacement was not committed")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Fatalf("content=%q", got)
	}
	info, _ := os.Stat(target)
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	replay, err := ReplaceFile(context.Background(), svc, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Revision: first.Receipt.Revision}, ReplaceRequest{OperationID: op, Path: target, ExpectedSHA256: expected, ContentFile: content, TTL: time.Minute, RequestNotAfter: deadline, RequestHash: first.Receipt.RequestHash})
	if err != nil {
		t.Fatal(err)
	}
	if !replay.Receipt.Idempotent {
		t.Fatal("replay was not idempotent")
	}
	// A completed replay is resolved from the authority record before any
	// mutable filesystem validation. The target may be gone or replaced by a
	// symlink without causing a second rename or a false fresh-operation error.
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(content, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(content); err != nil {
		t.Fatal(err)
	}
	replay, err = ReplaceFile(context.Background(), svc, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Revision: first.Receipt.Revision}, ReplaceRequest{OperationID: op, Path: target, ExpectedSHA256: expected, ContentFile: content, TTL: time.Minute, RequestNotAfter: deadline, RequestHash: first.Receipt.RequestHash})
	if err != nil || !replay.Receipt.Idempotent {
		t.Fatalf("replay after target swap: receipt=%+v err=%v", replay.Receipt, err)
	}
	_, err = ReplaceFile(context.Background(), svc, lease.Credentials{AuthorityID: st.AuthorityID(), ClaimID: claim, Token: token, Revision: first.Receipt.Revision}, ReplaceRequest{OperationID: op, Path: filepath.Join(root, "missing-target"), ExpectedSHA256: expected, ContentFile: filepath.Join(root, "missing-content"), TTL: time.Minute, RequestNotAfter: deadline, RequestHash: strings.Repeat("f", 64)})
	if got := reason.As(err); got == nil || got.Reason != reason.ReasonOperationRequestMismatch {
		t.Fatalf("request hash mismatch=%v", err)
	}
}
