package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/store"
)

func hostedTestFile(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("server\n"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHostedInitAndReissueAreDurableAndRedacted(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "authority")
	cfg := hostedTestFile(t, root, "server.conf", 0o600)
	secretDir := filepath.Join(root, "secrets")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(secretDir, "bootstrap")
	second := filepath.Join(secretDir, "bootstrap-2")
	var out, stderr strings.Builder
	if err := Run(context.Background(), []string{"worklease", "hosted", "init", "--home", home, "--server-config", cfg, "--bootstrap-invite-file", first, "--json"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("%v stderr=%s out=%s", err, stderr.String(), out.String())
	}
	if out.Len() == 0 || strings.Contains(out.String(), "server\n") {
		t.Fatalf("unexpected init output %q", out.String())
	}
	if info, err := os.Stat(first); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("bootstrap file info=%v err=%v", info, err)
	}
	st, err := store.Open(context.Background(), home, store.Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	oldRestore := st.RestoreID()
	st.Close()
	out.Reset()
	if err := Run(context.Background(), []string{"worklease", "hosted", "bootstrap-reissue", "--home", home, "--bootstrap-invite-file", second, "--json"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("%v stderr=%s out=%s", err, stderr.String(), out.String())
	}
	st, err = store.Open(context.Background(), home, store.Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.RestoreID() != oldRestore {
		t.Fatalf("reissue changed restore id")
	}
	var active, revoked int
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		if err := tx.QueryRowContext(context.Background(), `SELECT count(*) FROM invites WHERE bootstrap=1 AND state='active'`).Scan(&active); err != nil {
			return err
		}
		return tx.QueryRowContext(context.Background(), `SELECT count(*) FROM invites WHERE bootstrap=1 AND state='revoked'`).Scan(&revoked)
	}); err != nil {
		t.Fatal(err)
	}
	if active != 1 || revoked != 1 {
		t.Fatalf("bootstrap states active=%d revoked=%d", active, revoked)
	}
}

func TestHostedInitRecoversCommittedGrantBeforeReadyMarker(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "authority")
	cfg := hostedTestFile(t, root, "server.conf", 0o600)
	secretDir := filepath.Join(root, "secrets")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	invite := filepath.Join(secretDir, "bootstrap")
	args := []string{"worklease", "hosted", "init", "--home", home, "--server-config", cfg, "--bootstrap-invite-file", invite, "--json"}
	beforeHostedReadyHook = func() error {
		beforeHostedReadyHook = nil
		return errors.New("injected crash before ready marker")
	}
	t.Cleanup(func() { beforeHostedReadyHook = nil })
	var out, stderr strings.Builder
	if err := Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err == nil {
		t.Fatal("init unexpectedly finalized after injected crash")
	}
	if ready, err := store.HostedReady(home); err != nil || ready {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
	if _, err := store.Open(context.Background(), home, store.Options{HostedWriter: true, RequireHostedReady: true}); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonStorageFailure {
		t.Fatalf("incomplete open error=%v", err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("restart recovery: %v stdout=%s", err, out.String())
	}
	st, err := store.Open(context.Background(), home, store.Options{ReadOnly: true, RequireHostedReady: true})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var active int
	if err := st.Read(context.Background(), func(tx *store.Tx) error {
		return tx.QueryRowContext(context.Background(), `SELECT count(*) FROM invites WHERE bootstrap=1 AND state='active'`).Scan(&active)
	}); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("active bootstrap invites=%d", active)
	}
}

func TestHostedInitResumesMarkerOnlyCrashAndRefusesFreshNonEmptyHome(t *testing.T) {
	root := t.TempDir()
	cfg := hostedTestFile(t, root, "server.conf", 0o600)
	secretDir := filepath.Join(root, "secrets")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "resume")
	if err := store.MarkHosted(home); err != nil {
		t.Fatal(err)
	}
	var out, stderr strings.Builder
	if err := Run(context.Background(), []string{"worklease", "hosted", "init", "--home", home, "--server-config", cfg, "--bootstrap-invite-file", filepath.Join(secretDir, "bootstrap")}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("resume marker-only init: %v", err)
	}
	nonempty := filepath.Join(root, "nonempty")
	if err := os.Mkdir(nonempty, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonempty, "existing"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), []string{"worklease", "hosted", "init", "--home", nonempty, "--server-config", cfg, "--bootstrap-invite-file", filepath.Join(secretDir, "other")}, "test", "unknown", "unknown", &out, &stderr); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonInvalidArgument {
		t.Fatalf("non-empty init error=%v", err)
	}
	markedJunk := filepath.Join(root, "marked-junk")
	if err := store.MarkHosted(markedJunk); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(markedJunk, "junk"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), []string{"worklease", "hosted", "init", "--home", markedJunk, "--server-config", cfg, "--bootstrap-invite-file", filepath.Join(secretDir, "marked-other")}, "test", "unknown", "unknown", &out, &stderr); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonInvalidArgument {
		t.Fatalf("marked non-empty init error=%v", err)
	}
}

func TestHostedCommandsRefuseHeldLockBeforeOpeningDatabase(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "authority")
	cfg := hostedTestFile(t, root, "server.conf", 0o600)
	secretDir := filepath.Join(root, "secrets")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	invite := filepath.Join(secretDir, "bootstrap")
	var out, stderr strings.Builder
	if err := Run(context.Background(), []string{"worklease", "hosted", "init", "--home", home, "--server-config", cfg, "--bootstrap-invite-file", invite}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("%v stderr=%s out=%s", err, stderr.String(), out.String())
	}
	backup := filepath.Join(root, "backup.db")
	contents, err := os.ReadFile(filepath.Join(home, store.DatabaseFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := store.AcquireHostedLock(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	now := time.Now().UTC()
	commands := [][]string{
		{"worklease", "hosted", "init", "--home", home, "--server-config", cfg, "--bootstrap-invite-file", invite},
		{"worklease", "hosted", "restore", "--home", home, "--from", backup, "--selected-cutoff", now.Format(time.RFC3339Nano), "--loss-interval-start", now.Format(time.RFC3339Nano), "--loss-interval-end", now.Format(time.RFC3339Nano), "--bootstrap-invite-file", filepath.Join(secretDir, "restored")},
		{"worklease", "hosted", "bootstrap-reissue", "--home", home, "--bootstrap-invite-file", filepath.Join(secretDir, "next")},
		{"worklease", "hosted", "retire", "--home", home},
	}
	for _, args := range commands {
		out.Reset()
		stderr.Reset()
		err = Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr)
		if failure := reason.As(err); failure == nil || failure.Reason != reason.ReasonHostedLockHeld {
			t.Fatalf("%v lock error=%v", args, err)
		}
	}
}

func TestHostedRestoreAndForcedRetirement(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "authority")
	cfg := hostedTestFile(t, root, "server.conf", 0o600)
	secretDir := filepath.Join(root, "secrets")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var out, stderr strings.Builder
	firstSecret := filepath.Join(secretDir, "bootstrap")
	if err := Run(context.Background(), []string{"worklease", "hosted", "init", "--home", home, "--server-config", cfg, "--bootstrap-invite-file", firstSecret, "--json"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatal(err)
	}
	original, err := store.Open(context.Background(), home, store.Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	originalRestore := original.RestoreID()
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "backup.db")
	contents, err := os.ReadFile(filepath.Join(home, store.DatabaseFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	restoreArgs := []string{"worklease", "hosted", "restore", "--home", home, "--from", backup, "--selected-cutoff", now.Add(-time.Minute).Format(time.RFC3339Nano), "--loss-interval-start", now.Add(-time.Minute).Format(time.RFC3339Nano), "--loss-interval-end", now.Format(time.RFC3339Nano), "--bootstrap-invite-file", filepath.Join(secretDir, "restored"), "--json"}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), restoreArgs, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("restore: %v stderr=%s", err, stderr.String())
	}
	restored, err := store.Open(context.Background(), home, store.Options{HostedWriter: true, RequireHostedReady: true})
	if err != nil {
		t.Fatal(err)
	}
	firstRestoredID := restored.RestoreID()
	if firstRestoredID == originalRestore {
		t.Fatal("restore ID did not rotate")
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), restoreArgs, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("second restore: %v stderr=%s", err, stderr.String())
	}
	restored, err = store.Open(context.Background(), home, store.Options{HostedWriter: true, RequireHostedReady: true})
	if err != nil {
		t.Fatal(err)
	}
	if restored.RestoreID() == firstRestoredID {
		t.Fatal("restoring the same backup reused the restore ID")
	}
	token := strings.Repeat("e", 64)
	if _, err := lease.New(restored, nil, nil, lease.Defaults{}).Acquire(context.Background(), lease.AcquireRequest{AuthorityID: restored.AuthorityID(), ClaimID: strings.Repeat("1", 32), Token: token, Resources: []string{"coordination:test"}, AgentID: "test", SessionID: "test", TTL: time.Minute, RequestNotAfter: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := restored.Close(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "hosted", "retire", "--home", home, "--json"}, "test", "unknown", "unknown", &out, &stderr); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonInvalidArgument {
		t.Fatalf("unsafe retire error=%v", err)
	}
	exportPath := filepath.Join(secretDir, "retirement.json")
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "hosted", "retire", "--home", home, "--force", "--unresolved-export", exportPath, "--json"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("forced retire: %v stdout=%s stderr=%s", err, out.String(), stderr.String())
	}
	exported, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(exported), token) || !strings.Contains(string(exported), `"activeClaimCount":1`) || !strings.Contains(string(exported), `"recordType":"manifest"`) {
		t.Fatalf("retirement export=%s", exported)
	}
	if _, err := os.Lstat(filepath.Join(home, store.DatabaseFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retired database still exists: %v", err)
	}
}

func TestHostedExportManifestIsCompleteAndRedacted(t *testing.T) {
	inv := struct {
		Records []map[string]any `json:"records"`
	}{[]map[string]any{{"claimId": "c", "operationId": "o", "requestSha256": strings.Repeat("a", 64)}}}
	encoded, err := json.Marshal(inv)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "argv") {
		t.Fatal("export contains private argv")
	}
}
