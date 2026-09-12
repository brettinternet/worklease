package handle

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testHandle() Handle {
	return Handle{SchemaVersion: 1, AuthorityID: strings.Repeat("a", 32), ClaimID: strings.Repeat("b", 32), Token: strings.Repeat("c", 64), Revision: 1, Resources: []string{"r"}, ExpiresAt: time.Now().Add(time.Hour), AgentID: "agent", SessionID: "session", State: "ready"}
}
func TestContextRootAndContextualPathAreStableAndSessionScoped(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := ContextRoot(sub, func(c string, args ...string) (string, error) { return "false", nil })
	if err != nil {
		t.Fatal(err)
	}
	resolved, _ := filepath.EvalSymlinks(sub)
	if got != resolved {
		t.Fatalf("root=%q want=%q", got, resolved)
	}
	if ContextualPath("/state", root, "one") == ContextualPath("/state", root, "two") {
		t.Fatal("sessions share contextual path")
	}
	first := ContextualPath("/state", root, "one")
	if filepath.Base(first) == "" {
		t.Fatal("context path is unstable")
	}
}
func TestValidateMetadataChecksParentAndLeafWithoutReading(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "h.json")
	if present, err := ValidateMetadata(path); err != nil || present {
		t.Fatalf("missing metadata present=%t err=%v", present, err)
	}
	if err := os.Chmod(dir, 0o770); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateMetadata(path); err == nil {
		t.Fatal("accepted writable handle parent")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkBase := t.TempDir()
	symlinkTarget := t.TempDir()
	if err := os.Chmod(symlinkTarget, 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkParent := filepath.Join(symlinkBase, "handles")
	if err := os.Symlink(symlinkTarget, symlinkParent); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateMetadata(filepath.Join(symlinkParent, "h.json")); err == nil {
		t.Fatal("accepted symlink handle parent")
	}
	if err := Write(path, testHandle()); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateMetadata(link); err == nil {
		t.Fatal("accepted symlink handle parent/leaf")
	}
	hardlink := filepath.Join(dir, "hardlink")
	if err := os.Link(path, hardlink); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateMetadata(path); err == nil {
		t.Fatal("accepted hard-linked handle")
	}
}

func TestHandleAtomicPrivateRoundTripAndRejectsUnsafe(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "h.json")
	h := testHandle()
	if err := Write(p, h); err != nil {
		t.Fatal(err)
	}
	got, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.Token != h.Token {
		t.Fatal("round trip changed handle")
	}
	if err := os.Chmod(p, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(p); err == nil {
		t.Fatal("accepted group-readable handle")
	}
	if err := os.Chmod(p, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(p, p+".link"); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(p + ".link"); err == nil {
		t.Fatal("accepted symlink handle")
	}
}
func TestContextRootResolvesSymlinksGitSubdirectoriesAndLinkedWorktrees(t *testing.T) {
	repository := filepath.Join(t.TempDir(), "repo")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", repository}, {"-C", repository, "config", "user.name", "test"}, {"-C", repository, "config", "user.email", "test@example.invalid"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repository, "tracked"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-C", repository, "add", "tracked"}, {"-C", repository, "commit", "-m", "initial"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	subdirectory := filepath.Join(repository, "sub")
	if err := os.Mkdir(subdirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(subdirectory, alias); err != nil {
		t.Fatal(err)
	}
	root, err := ContextRoot(alias, nil)
	resolvedRepository, resolveErr := filepath.EvalSymlinks(repository)
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if err != nil || root != resolvedRepository {
		t.Fatalf("symlinked git root=%q err=%v want=%q", root, err, resolvedRepository)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	if out, err := exec.Command("git", "-C", repository, "worktree", "add", "--detach", linked).CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v: %s", err, out)
	}
	linkedRoot, err := ContextRoot(linked, nil)
	resolvedLinked, resolveErr := filepath.EvalSymlinks(linked)
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if err != nil || linkedRoot != resolvedLinked || linkedRoot == root {
		t.Fatalf("linked root=%q err=%v primary=%q", linkedRoot, err, root)
	}
}

func TestCredentialReaderStrictEncodingAndBounded(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "token")
	token := strings.Repeat("a", 64)
	if err := os.WriteFile(p, []byte(token+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadCredential(p); err != nil || got != token {
		t.Fatalf("credential=%q err=%v", got, err)
	}
	if err := os.WriteFile(p, []byte(strings.Repeat("A", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCredential(p); err == nil {
		t.Fatal("accepted uppercase token")
	}
	if err := os.WriteFile(p, []byte(token+"\n\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCredential(p); err == nil {
		t.Fatal("accepted multiple newlines")
	}
	if err := os.WriteFile(p, []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCredential(p); err == nil {
		t.Fatal("accepted credential from writable directory")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(dir, "hardlink")
	if err := os.Link(p, hardlink); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCredential(p); err == nil {
		t.Fatal("accepted hard-linked credential")
	}
}

func TestHandleSchemaRejectsTrailingPendingAndAuthorityErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "h.json")
	h := testHandle()
	b, _ := json.Marshal(h)
	if err := os.WriteFile(p, append(append([]byte{}, b...), []byte(" {}")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(p); err == nil {
		t.Fatal("accepted trailing JSON")
	}
	duplicate := strings.Replace(string(b), `"state":"ready"`, `"state":"ready","state":"ready"`, 1)
	if err := os.WriteFile(p, []byte(duplicate), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(p); err == nil {
		t.Fatal("accepted duplicate JSON key")
	}
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Write(p, Handle{SchemaVersion: 1, AuthorityID: strings.Repeat("d", 32), ClaimID: strings.Repeat("e", 32), Token: strings.Repeat("f", 64), Revision: 1, Resources: []string{"r"}, ExpiresAt: time.Now().Add(time.Hour), AgentID: "agent", SessionID: "session", State: "ready"}); err == nil {
		t.Fatal("replaced handle across authorities")
	}
	pending := h
	pending.State, pending.Revision, pending.ExpiresAt = "pending", 0, time.Time{}
	pending.PendingRequest = &PendingRequest{OperationID: strings.Repeat("1", 32), Kind: "heartbeat", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: strings.Repeat("2", 64), RequestNotAfter: time.Now().Add(time.Hour), Inputs: map[string]any{"ttl": int64(1)}}
	if err := Write(p, pending); err != nil {
		t.Fatal(err)
	}
	pending.RecoveryRequest = &RecoveryRequest{OperationID: strings.Repeat("d", 32), TargetClaimID: strings.Repeat("e", 32), RequestHash: strings.Repeat("f", 64), RequestNotAfter: time.Now().Add(time.Hour), Outcome: "observed-success", Evidence: json.RawMessage(`{"stopped":true}`)}
	got, err := Read(p)
	if err != nil {
		t.Fatalf("valid pending rejected: %v", err)
	}
	if got.PendingRequest == nil {
		t.Fatal("pending request was not retained")
	}
	if err := Write(p, pending); err != nil {
		t.Fatal(err)
	}
	got, err = Read(p)
	if err != nil || got.PendingRequest == nil || got.RecoveryRequest == nil {
		t.Fatalf("pending and recovery requests did not coexist: %#v err=%v", got, err)
	}
	pending.PendingRequest.Inputs = nil
	if err := Write(p, pending); err == nil {
		t.Fatal("accepted malformed pending replacement")
	}
}

func TestExistingMalformedHandleIsNeverReplaceable(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "h.json")
	if err := os.WriteFile(p, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Write(p, testHandle()); err == nil {
		t.Fatal("replaced malformed existing handle")
	}
}

func TestExistingSharedLockDoesNotCreateMissingLock(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "missing.lock")
	missing, err := AcquireExistingLock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer missing.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("lock path exists after failed verify: %v", err)
	}
	first, err := AcquireLock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := AcquireExistingLock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
}

func TestSiblingLockSerializesAndDoesNotUnlink(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "h.lock")
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	first, err := AcquireLock(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := AcquireLock(ctx, p); err == nil {
		t.Fatal("second process acquired held lock")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}
