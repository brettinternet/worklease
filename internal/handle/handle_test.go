package handle

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/testkit"
)

func testHandle() Handle {
	return Handle{SchemaVersion: 1, AuthorityID: strings.Repeat("a", 32), ClaimID: strings.Repeat("b", 32), Token: strings.Repeat("c", 64), Revision: 1, Resources: []string{"r"}, ExpiresAt: time.Now().Add(time.Hour), AgentID: "agent", SessionID: "session", State: "ready"}
}

func TestReadCredentialFDOwnsDuplicateUntilClose(t *testing.T) {
	credentialPath := filepath.Join(t.TempDir(), "credential")
	credential := strings.Repeat("a", 64)
	if err := os.WriteFile(credentialPath, []byte(credential+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(credentialPath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if got, err := ReadCredentialFD(int(source.Fd())); err != nil || got != credential {
		t.Fatalf("credential=%q err=%v", got, err)
	}
	target, err := os.OpenFile(filepath.Join(t.TempDir(), "after"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	runtime.GC()
	runtime.Gosched()
	if _, err := target.WriteString("still-open\n"); err != nil {
		t.Fatalf("credential descriptor finalizer closed a reused descriptor: %v", err)
	}
}
func TestContextRootAndContextualPathAreStableSessionAndAuthorityScoped(t *testing.T) {
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
	authorityA, authorityB := strings.Repeat("a", 32), strings.Repeat("b", 32)
	if ContextualPath("/state", root, "one", authorityA) == ContextualPath("/state", root, "two", authorityA) {
		t.Fatal("sessions share contextual path")
	}
	first := ContextualPath("/state", root, "one", authorityA)
	if first == ContextualPath("/state", root, "one", authorityB) {
		t.Fatal("authorities share contextual path")
	}
	if first != ContextualPath("/state", root, "one", authorityA) || filepath.Base(first) == "" {
		t.Fatal("context path is unstable")
	}
}

func TestResolveContextualPathMigratesMatchingLegacyHandleExactly(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	if err := EnsureOwnerPrivateDir(filepath.Join(home, "handles")); err != nil {
		t.Fatal(err)
	}
	h := testHandle()
	h.PendingRequest = &PendingRequest{OperationID: h.ClaimID, Kind: "checkpoint", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, RequestHash: strings.Repeat("d", 64), RequestNotAfter: time.Now().Add(time.Hour), Inputs: map[string]any{"checkpoint": "exact"}}
	h.RecoveryRequest = &RecoveryRequest{OperationID: strings.Repeat("e", 32), TargetClaimID: h.ClaimID, RequestHash: strings.Repeat("f", 64), RequestNotAfter: time.Now().Add(time.Hour), Outcome: "unknown"}
	h.State = "pending"
	legacy := LegacyContextualPath(home, root, "session")
	if err := Write(legacy, h); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := ResolveContextualPath(context.Background(), home, root, "session", h.AuthorityID, true)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("migration rewrote handle contents")
	}
	if _, err := os.Stat(legacy); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy handle remains: %v", err)
	}
}

func TestResolveContextualPathPreservesMismatchesAndConflicts(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	if err := EnsureOwnerPrivateDir(filepath.Join(home, "handles")); err != nil {
		t.Fatal(err)
	}
	legacyHandle := testHandle()
	legacy := LegacyContextualPath(home, root, "session")
	if err := Write(legacy, legacyHandle); err != nil {
		t.Fatal(err)
	}
	otherAuthority := strings.Repeat("d", 32)
	otherPath, err := ResolveContextualPath(context.Background(), home, root, "session", otherAuthority, true)
	if err != nil {
		t.Fatal(err)
	}
	if otherPath != ContextualPath(home, root, "session", otherAuthority) {
		t.Fatal("wrong authority-scoped path")
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("mismatched legacy handle was changed: %v", err)
	}
	destination := ContextualPath(home, root, "session", legacyHandle.AuthorityID)
	if err := Write(destination, legacyHandle); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveContextualPath(context.Background(), home, root, "session", legacyHandle.AuthorityID, true); err == nil || !strings.Contains(err.Error(), "--handle") {
		t.Fatalf("conflict error=%v", err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy conflict was changed: %v", err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("destination conflict was changed: %v", err)
	}
}
func TestResolveContextualPathReadOnlySelectsLegacyWithoutWriting(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	if err := EnsureOwnerPrivateDir(filepath.Join(home, "handles")); err != nil {
		t.Fatal(err)
	}
	h := testHandle()
	legacy := LegacyContextualPath(home, root, "session")
	if err := Write(legacy, h); err != nil {
		t.Fatal(err)
	}
	selected, err := ResolveContextualPath(context.Background(), home, root, "session", h.AuthorityID, false)
	if err != nil || selected != legacy {
		t.Fatalf("read-only selection=%q err=%v", selected, err)
	}
	if _, err := os.Stat(ContextualPath(home, root, "session", h.AuthorityID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only selection created scoped handle: %v", err)
	}
	if matches, err := filepath.Glob(filepath.Join(home, "handles", "*.lock")); err != nil || len(matches) != 0 {
		t.Fatalf("read-only selection created locks: %v err=%v", matches, err)
	}
}

func TestResolveContextualPathSerializesConcurrentMigration(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	if err := EnsureOwnerPrivateDir(filepath.Join(home, "handles")); err != nil {
		t.Fatal(err)
	}
	h := testHandle()
	legacy := LegacyContextualPath(home, root, "session")
	if err := Write(legacy, h); err != nil {
		t.Fatal(err)
	}
	const workers = 8
	errorsSeen := make(chan error, workers)
	for range workers {
		go func() {
			_, err := ResolveContextualPath(context.Background(), home, root, "session", h.AuthorityID, true)
			errorsSeen <- err
		}()
	}
	var migrationErrors []error
	for range workers {
		if err := <-errorsSeen; err != nil {
			migrationErrors = append(migrationErrors, err)
		}
	}
	if len(migrationErrors) > 0 {
		t.Fatalf("concurrent migration: %v", migrationErrors)
	}
	if _, err := Read(ContextualPath(home, root, "session", h.AuthorityID)); err != nil {
		t.Fatal(err)
	}
}

func TestResolveContextualPathUsesCanonicalLockOrder(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	if err := EnsureOwnerPrivateDir(filepath.Join(home, "handles")); err != nil {
		t.Fatal(err)
	}
	h := testHandle()
	legacy := LegacyContextualPath(home, root, "session")
	destination := ContextualPath(home, root, "session", h.AuthorityID)
	if err := Write(legacy, h); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	errorsSeen := make(chan error, 2)
	go func() {
		_, err := ResolveContextualPath(ctx, home, root, "session", h.AuthorityID, true)
		errorsSeen <- err
	}()
	go func() {
		locks, err := AcquireLocks(ctx, destination+".lock", legacy+".lock")
		for _, lock := range locks {
			_ = lock.Close()
		}
		errorsSeen <- err
	}()
	for range 2 {
		if err := <-errorsSeen; err != nil {
			t.Fatalf("canonical migration lock order: %v", err)
		}
	}
}

func TestResolveContextualPathConcurrentAuthoritiesDoNotLoseLegacyState(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	if err := EnsureOwnerPrivateDir(filepath.Join(home, "handles")); err != nil {
		t.Fatal(err)
	}
	h := testHandle()
	if err := Write(LegacyContextualPath(home, root, "session"), h); err != nil {
		t.Fatal(err)
	}
	authorities := []string{h.AuthorityID, strings.Repeat("d", 32)}
	errorsSeen := make(chan error, len(authorities))
	for _, authorityID := range authorities {
		go func() {
			_, err := ResolveContextualPath(context.Background(), home, root, "session", authorityID, true)
			errorsSeen <- err
		}()
	}
	for range authorities {
		if err := <-errorsSeen; err != nil {
			t.Fatalf("concurrent authority resolution: %v", err)
		}
	}
	migrated, err := Read(ContextualPath(home, root, "session", h.AuthorityID))
	if err != nil || migrated.AuthorityID != h.AuthorityID {
		t.Fatalf("matching authority state lost: %#v err=%v", migrated, err)
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
		if out, err := testkit.GitCommand(args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repository, "tracked"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-C", repository, "add", "tracked"}, {"-C", repository, "commit", "-m", "initial"}} {
		if out, err := testkit.GitCommand(args...).CombinedOutput(); err != nil {
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
	if out, err := testkit.GitCommand("-C", repository, "worktree", "add", "--detach", linked).CombinedOutput(); err != nil {
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

func TestHandleRejectsAncestorSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(link, "handles")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock(context.Background(), filepath.Join(parent, "h.lock")); err == nil {
		t.Fatal("accepted symlink ancestor")
	}
}

func TestPinnedLockRejectsParentReplacement(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "handles")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "h.json")
	if err := Write(path, testHandle()); err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireLock(context.Background(), path+".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	old := filepath.Join(root, "handles-old")
	if err := os.Rename(parent, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Read(path); err == nil {
		t.Fatal("read replacement parent through old lock")
	}
}

func TestPinnedLockRejectsLockLeafReplacement(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "h.json")
	if err := Write(path, testHandle()); err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireLock(context.Background(), path+".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := os.Rename(path+".lock", path+".lock-old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".lock", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Read(path); err == nil {
		t.Fatal("read after lock leaf replacement")
	}
}

func TestPinnedLockRejectsParentReplacementBeforePersistence(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "handles")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "h.json")
	lock, err := AcquireLock(context.Background(), path+".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := os.Rename(parent, filepath.Join(root, "handles-old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := lock.Write(path, testHandle()); err == nil {
		t.Fatal("persisted through replacement parent")
	}
}

func TestAcquireLockRejectsParentReplacementWhileWaiting(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "handles")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "h.json")
	first, err := AcquireLock(context.Background(), path+".lock")
	if err != nil {
		t.Fatal(err)
	}
	opened := make(chan struct{})
	resume := make(chan struct{})
	afterLockOpenForTest = func() {
		close(opened)
		<-resume
	}
	defer func() { afterLockOpenForTest = func() {} }()
	result := make(chan error, 1)
	go func() {
		second, acquireErr := AcquireLock(context.Background(), path+".lock")
		if second != nil {
			_ = second.Close()
		}
		result <- acquireErr
	}()
	<-opened
	if err := os.Rename(parent, filepath.Join(root, "handles-old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	close(resume)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err == nil {
		t.Fatal("acquired lock after parent replacement")
	}
}

func TestLockRejectsDifferentSiblingHandle(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	firstPath := filepath.Join(dir, "a.json")
	otherPath := filepath.Join(dir, "b.json")
	lock, err := AcquireLock(context.Background(), firstPath+".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := lock.Write(otherPath, testHandle()); err == nil {
		t.Fatal("lock protected a different sibling handle")
	}
	if _, err := os.Stat(otherPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("different sibling was created: %v", err)
	}
}

func TestRemoveOwnerPrivateIfContentRequiresExactCurrentContent(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "artifact")
	original := []byte("original\n")
	if err := WriteOwnerPrivateNoReplace(path, original, 1024); err != nil {
		t.Fatal(err)
	}
	if err := RemoveOwnerPrivateIfContent(path, []byte("different\n"), 1024); err == nil {
		t.Fatal("removed private file with different content")
	}
	if current, err := os.ReadFile(path); err != nil || string(current) != string(original) {
		t.Fatalf("mismatch changed file: contents=%q err=%v", current, err)
	}
	if err := RemoveOwnerPrivateIfContent(path, original, 1024); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("matching private file remains: %v", err)
	}
}

func TestAcquireLocksCanonicalizesDarwinSystemAliasesBeforeSorting(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Darwin system alias behavior")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var alias string
	switch {
	case strings.HasPrefix(dir, "/private/tmp/"), strings.HasPrefix(dir, "/private/var/"):
		alias = strings.TrimPrefix(dir, "/private")
	case strings.HasPrefix(dir, "/tmp/"), strings.HasPrefix(dir, "/var/"):
		alias = "/private" + dir
	default:
		t.Skip("temporary directory does not use a Darwin system alias")
	}
	if _, err := AcquireLocks(context.Background(), filepath.Join(dir, "h.lock"), filepath.Join(alias, "h.lock")); err == nil {
		t.Fatal("accepted duplicate lock paths through Darwin system aliases")
	}
}
