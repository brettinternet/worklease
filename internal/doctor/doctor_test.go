package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/store"
)

func clearDoctorEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{"WORKLEASE_HOME", "WORKLEASE_CONFIG", "WORKLEASE_HANDLE", "WORKLEASE_AGENT_ID", "WORKLEASE_TTL", "WORKLEASE_POLL_INTERVAL"} {
		t.Setenv(key, "")
	}
}

func testConfig(home string) config.Config {
	return config.Config{Home: home, ConfigPath: filepath.Join(home, "config.yaml"), AgentID: "agent", SessionID: "session", Sources: map[string]string{
		"home": "flag", "config_path": "flag", "agent": "default", "session": "env",
	}}
}

func checkByID(checks []Check, id string) Check {
	for _, check := range checks {
		if check.ID == id {
			return check
		}
	}
	return Check{}
}

func TestDiagnoseValidatesMissingHomeAncestryReadOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	unsafe := filepath.Join(root, "unsafe")
	if err := os.Mkdir(unsafe, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafe, 0o777); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(unsafe, "not-created")
	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	checks := Diagnose(context.Background(), testConfig(missing), root)
	if got := checkByID(checks, "home.path"); got.Status != "fail" {
		t.Fatalf("home.path=%+v", got)
	}
	if got := checkByID(checks, "db.open"); got.Status != "warn" {
		t.Fatalf("db.open=%+v", got)
	}
	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("diagnose changed ancestry: before=%v after=%v", before, after)
	}
	unsafeInfo, err := os.Lstat(unsafe)
	if err != nil {
		t.Fatal(err)
	}
	if unsafeInfo.Mode().Perm() != 0o777 {
		t.Fatalf("diagnose chmodded unsafe ancestor to %04o", unsafeInfo.Mode().Perm())
	}
}

func TestDiagnoseReportsMetadataStatusesDetailsAndHints(t *testing.T) {
	clearDoctorEnvironment(t)
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	unsafeParent := filepath.Join(root, "unsafe-parent")
	if err := os.Mkdir(unsafeParent, 0o777); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(unsafeParent, "missing-home")
	handlePath := filepath.Join(unsafeParent, "handle")
	t.Setenv("WORKLEASE_HANDLE", handlePath)
	cfg := testConfig(home)
	cfg.AgentID = ""
	checks := Diagnose(context.Background(), cfg, filepath.Join(root, "not-a-repository"))
	assertCheck := func(id, status, detail, hint string) {
		t.Helper()
		got := checkByID(checks, id)
		if got.Status != status || !strings.Contains(got.Detail, detail) || (hint != "" && !strings.Contains(got.Hint, hint)) {
			t.Fatalf("%s=%+v want status=%q detail containing %q hint containing %q", id, got, status, detail, hint)
		}
	}
	assertCheck("config.sources", "ok", "resolved sources:", "")
	assertCheck("home.path", "warn", "not present", "run worklease acquire")
	assertCheck("home.permissions", "unknown", "cannot be checked until it exists", "")
	assertCheck("db.open", "warn", "not present", "run worklease acquire")
	assertCheck("db.schema", "unknown", "without an authority database", "")
	assertCheck("context.root", "fail", "cannot be resolved", "accessible directory")
	assertCheck("handle.present", "warn", "unavailable", "handle parent")
	assertCheck("handle.permissions", "fail", "unsafe", "mode 0600")
	assertCheck("agent.identity", "fail", "unavailable", "WORKLEASE_AGENT_ID")
	assertCheck("clock.authority", "unknown", "watermark is unavailable", "")
	assertCheck("authority.identity", "unknown", "identity is unavailable until state exists", "")
	assertCheck("restore.identity", "unknown", "identity is unavailable until state exists", "")
	assertCheck("mcp.available", "ok", "MCP stdio server is available", "")
	if strings.Contains(fmt.Sprint(checks), "secret") {
		t.Fatal("diagnostics exposed sensitive content")
	}
}

func TestDiagnoseReportsPythonEraStateAndAuthorityIdentity(t *testing.T) {
	clearDoctorEnvironment(t)
	home := t.TempDir()
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"leases.sqlite3", "locks", "context-leases", "mcp-leases"} {
		path := filepath.Join(home, name)
		if strings.Contains(name, "leases") {
			if err := os.WriteFile(path, []byte("legacy-secret"), 0o600); err != nil {
				t.Fatal(err)
			}
		} else if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	checks := Diagnose(context.Background(), testConfig(home), home)
	legacy := checkByID(checks, "state.python-era")
	if legacy.Status != "warn" || !strings.Contains(legacy.Detail, "leases.sqlite3") || !strings.Contains(legacy.Hint, "recoverable disposal") {
		t.Fatalf("legacy=%+v", legacy)
	}
	if strings.Contains(legacy.Detail, "legacy-secret") {
		t.Fatal("legacy state content was exposed")
	}

	st, err := store.Open(context.Background(), home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	authorityID := st.AuthorityID()
	restoreID := st.RestoreID()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	identity := checkByID(Diagnose(context.Background(), testConfig(home), home), "authority.identity")
	if identity.Status != "ok" || !strings.Contains(identity.Detail, "authority identity: "+authorityID) || identity.Hint != "" {
		t.Fatalf("authority identity=%+v", identity)
	}
	restore := checkByID(Diagnose(context.Background(), testConfig(home), home), "restore.identity")
	if restore.Status != "ok" || !strings.Contains(restore.Detail, "restore identity: "+restoreID) || restore.Hint != "" {
		t.Fatalf("restore identity=%+v", restore)
	}
}

func TestDiagnoseClockAuthorityAllowsOneSecondSkew(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	now := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	future := now.Add(900 * time.Millisecond).UnixMicro()
	if err := st.Write(context.Background(), func(tx *store.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE meta SET value=? WHERE key='last_observed_at'`, future)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if got := checkByID(diagnoseAt(context.Background(), testConfig(home), home, now), "clock.authority"); got.Status != "ok" || got.Detail != "local clock is not behind the authority watermark" || got.Hint != "" {
		t.Fatalf("under-one-second skew=%+v", got)
	}

	st, err = store.Open(context.Background(), home, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	future = now.Add(2 * time.Second).UnixMicro()
	if err := st.Write(context.Background(), func(tx *store.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE meta SET value=? WHERE key='last_observed_at'`, future)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if got := checkByID(diagnoseAt(context.Background(), testConfig(home), home, now), "clock.authority"); got.Status != "fail" || !strings.Contains(got.Detail, "more than one second") || !strings.Contains(got.Hint, "correct the host clock") {
		t.Fatalf("over-one-second skew=%+v", got)
	}
}

func TestDiagnoseDeletedContextStillReturnsEveryCheck(t *testing.T) {
	home := t.TempDir()
	if err := os.Chmod(home, 0o700); err != nil {
		t.Fatal(err)
	}
	deleted := filepath.Join(t.TempDir(), "deleted")
	checks := Diagnose(context.Background(), testConfig(home), deleted)
	if len(checks) != 16 {
		t.Fatalf("checks=%d want 16", len(checks))
	}
	if got := checkByID(checks, "context.root"); got.Status != "fail" {
		t.Fatalf("context.root=%+v", got)
	}
	if checkByID(checks, "home.path").Status == "fail" {
		t.Fatal("deleted cwd contaminated home diagnostics")
	}
	if _, err := os.Stat(deleted); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("diagnose created deleted cwd: %v", err)
	}
}
