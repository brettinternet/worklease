package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/authority"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/reason"
	workleaseserver "github.com/brettinternet/worklease/internal/server"
	"github.com/brettinternet/worklease/internal/store"
)

func hostedTestFile(t *testing.T, dir, name, home string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	contents := "home: " + home + "\nlisten: 127.0.0.1:8443\nallowInsecureHTTP: true\nadmittedPrefixes:\n  - \"coordination:\"\nmaxTTL: 1h\nmaxHold: 24h\nhealthRate: 60\nmetadataRate: 60\nenrollmentRate: 20\n"
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestServeWithoutConfigurationPointsToServerInit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	var out, stderr strings.Builder
	err := Run(context.Background(), []string{"worklease", "serve"}, "test", "unknown", "unknown", &out, &stderr)
	failure := reason.As(err)
	if failure == nil || failure.Reason != reason.ReasonConfigMissing || !strings.Contains(err.Error(), "run worklease server init") {
		t.Fatalf("missing config error = %v", err)
	}
}

func TestDefaultServerConfigRefusesExistingSharedParent(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "server.yaml")
	if err := writeDefaultServerConfig(path); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHomeUnsafe {
		t.Fatalf("shared parent error = %v", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("shared parent mode changed to %v", info.Mode().Perm())
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("configuration unexpectedly created: %v", err)
	}
}

func TestServerInitWithoutArgumentsCreatesRunnableDefaults(t *testing.T) {
	root := t.TempDir()
	configRoot := filepath.Join(root, "config")
	stateRoot := filepath.Join(root, "state")
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("XDG_STATE_HOME", stateRoot)
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	var out, stderr strings.Builder
	if err := Run(context.Background(), []string{"worklease", "server", "init", "--json"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("server init: %v stderr=%s", err, stderr.String())
	}
	configPath := filepath.Join(configRoot, "worklease", "server.yaml")
	cfg, err := workleaseserver.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Home != filepath.Join(stateRoot, "worklease", "server") || cfg.Listen != "127.0.0.1:8443" || cfg.AdvertisedEndpoint != "https://127.0.0.1:8443" || cfg.AllowInsecureHTTP || cfg.TLSCert == "" || cfg.TLSKey == "" {
		t.Fatalf("default config = %+v", cfg)
	}
	invitePath := filepath.Join(configRoot, "worklease", "bootstrap.invite")
	for _, path := range []string{configPath, invitePath, filepath.Join(cfg.Home, store.HostedReadyFileName)} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing initialized file %s: %v", path, err)
		}
	}
	for _, path := range []string{configPath, invitePath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("private file %s mode=%v", path, info.Mode().Perm())
		}
	}
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	contents = []byte(strings.Replace(string(contents), "127.0.0.1:8443", address, 1))
	if err := os.WriteFile(configPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	serveCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	stderr.Reset()
	go func() {
		serveDone <- Run(serveCtx, []string{"worklease", "serve"}, "test", "unknown", "unknown", &out, &stderr)
	}()
	var healthy bool
	var lastHealthError error
	lastHealthStatus := 0
	client := &http.Client{Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, Timeout: 100 * time.Millisecond}
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		select {
		case serveErr := <-serveDone:
			t.Fatalf("argument-free serve exited before readiness: %v stderr=%s", serveErr, stderr.String())
		default:
		}
		request, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://"+address+"/healthz", nil)
		if requestErr == nil {
			request.Header.Set("Accept", "application/json")
		}
		var response *http.Response
		if requestErr == nil {
			response, requestErr = client.Do(request)
		}
		lastHealthError = requestErr
		if requestErr == nil {
			lastHealthStatus = response.StatusCode
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				healthy = true
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !healthy {
		t.Fatalf("argument-free serve did not become healthy: status=%d err=%v", lastHealthStatus, lastHealthError)
	}
	cancel()
	if err := <-serveDone; err != nil {
		t.Fatalf("argument-free serve: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "listening address="+address+" transport=https advertisedEndpoint=https://127.0.0.1:8443") {
		t.Fatalf("argument-free serve diagnostics = %q", stderr.String())
	}
	if command := NewRootCommand("test", "unknown", "unknown", &out, &stderr).Command("hosted"); command != nil {
		t.Fatal("obsolete hosted command remains registered")
	}
}

func TestZeroFlagRemoteJourneyTwoClients(t *testing.T) {
	clearWorkleaseEnvironment(t)
	for _, name := range []string{"WORKLEASE_PROFILE", "WORKLEASE_SERVER_CONFIG"} {
		t.Setenv(name, "")
	}
	if connection, err := net.DialTimeout("tcp", "127.0.0.1:8443", 100*time.Millisecond); err == nil {
		connection.Close()
		t.Skip("default authority port is already in use")
	}
	probe, err := net.Listen("tcp", "127.0.0.1:8443")
	if err != nil {
		t.Skipf("default authority port is unavailable: %v", err)
	}
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}

	serverHome := t.TempDir()
	t.Setenv("HOME", serverHome)
	var initOut, initErr strings.Builder
	if err := Run(context.Background(), []string{"worklease", "server", "init"}, "test", "unknown", "unknown", &initOut, &initErr); err != nil {
		t.Fatalf("zero-flag init: %v stderr=%s", err, initErr.String())
	}
	bootstrapPath := filepath.Join(serverHome, ".config", "worklease", "bootstrap.invite")
	if !strings.Contains(initOut.String(), bootstrapPath) || !strings.Contains(initOut.String(), "worklease serve") || !strings.Contains(initOut.String(), "worklease enroll --invite-file") {
		t.Fatalf("init handoff is incomplete: %s", initOut.String())
	}

	serveCtx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	var serveOut, serveErr strings.Builder
	go func() {
		serveDone <- Run(serveCtx, []string{"worklease", "serve"}, "test", "unknown", "unknown", &serveOut, &serveErr)
	}()
	serverStopped := false
	t.Cleanup(func() {
		if serverStopped {
			return
		}
		cancel()
		select {
		case err := <-serveDone:
			if err != nil {
				t.Errorf("zero-flag serve shutdown: %v stderr=%s", err, serveErr.String())
			}
		case <-time.After(5 * time.Second):
			t.Error("zero-flag serve did not stop")
		}
	})
	client := &http.Client{Transport: &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, Timeout: 100 * time.Millisecond}
	ready := false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		request, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://127.0.0.1:8443/healthz", nil)
		if requestErr == nil {
			request.Header.Set("Worklease-Protocol-Version", workleaseserver.ProtocolVersion)
			request.Header.Set("Accept", "application/json")
		}
		var response *http.Response
		if requestErr == nil {
			response, requestErr = client.Do(request)
		}
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("zero-flag serve did not become ready: %s", serveErr.String())
	}

	adminHome := t.TempDir()
	t.Setenv("HOME", adminHome)
	var out, stderr strings.Builder
	if err := Run(context.Background(), []string{"worklease", "enroll", "--invite-file", bootstrapPath}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("admin enroll: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(out.String(), "enrolled profile remote (role: admin)") {
		t.Fatalf("bootstrap enrollment did not separate profile and role: %s", out.String())
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "invite", "issue"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("zero-flag invite issue: %v stderr=%s", err, stderr.String())
	}
	clientInvite := filepath.Join(adminHome, ".config", "worklease", "remote.invite")
	if !strings.Contains(out.String(), clientInvite) || !strings.Contains(out.String(), "worklease enroll --invite-file") {
		t.Fatalf("invite handoff is incomplete: %s", out.String())
	}

	clientHome := t.TempDir()
	t.Setenv("HOME", clientHome)
	for _, args := range [][]string{
		{"worklease", "enroll", "--invite-file", clientInvite},
		{"worklease", "acquire", "--resource", "coordination:demo"},
		{"worklease", "list"},
		{"worklease", "heartbeat"},
		{"worklease", "release"},
	} {
		out.Reset()
		stderr.Reset()
		if err := Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err != nil {
			t.Fatalf("%v: %v stdout=%s stderr=%s", args, err, out.String(), stderr.String())
		}
		if args[1] == "list" && !strings.Contains(out.String(), "coordination:demo") {
			t.Fatalf("second client list omitted claim: %s", out.String())
		}
	}

	cancel()
	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("zero-flag serve shutdown: %v stderr=%s", err, serveErr.String())
		}
		serverStopped = true
	case <-time.After(5 * time.Second):
		t.Fatal("zero-flag serve did not stop")
	}
	t.Setenv("HOME", serverHome)
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "init"}, "test", "unknown", "unknown", &out, &stderr); err == nil || !strings.Contains(err.Error(), "bootstrap invite was used") || !strings.Contains(err.Error(), "worklease server bootstrap-reissue") {
		t.Fatalf("redeemed bootstrap rerun guidance: %v stdout=%s stderr=%s", err, out.String(), stderr.String())
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "bootstrap-reissue"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("bare bootstrap reissue after redemption: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(out.String(), bootstrapPath) {
		t.Fatalf("bootstrap reissue omitted default artifact: %s", out.String())
	}
}

func TestServerInitSetupOverridesDoNotRequireGuidedAlias(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	configPath := filepath.Join(root, "config", "worklease", "custom.yaml")
	var out, stderr strings.Builder
	args := []string{"worklease", "server", "init", "--server-config", configPath, "--listen", "127.0.0.1:9443", "--endpoint", "https://localhost:9443", "--transport", "tls", "--admitted-prefix", "task:", "--json"}
	if err := Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("setup override init: %v stderr=%s", err, stderr.String())
	}
	cfg, err := workleaseserver.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:9443" || cfg.AdvertisedEndpoint != "https://localhost:9443" || cfg.AllowInsecureHTTP || len(cfg.Prefixes) != 1 || cfg.Prefixes[0] != "task:" {
		t.Fatalf("override config=%+v", cfg)
	}
}

func TestServerInitNamesMismatchedConfigAndStatePaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	var out, stderr strings.Builder
	if err := Run(context.Background(), []string{"worklease", "server", "init"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatal(err)
	}
	otherConfig := filepath.Join(root, "other", "server.yaml")
	home := filepath.Join(root, "state", "worklease", "server")
	out.Reset()
	stderr.Reset()
	err := Run(context.Background(), []string{"worklease", "server", "init", "--server-config", otherConfig}, "test", "unknown", "unknown", &out, &stderr)
	if err == nil || !strings.Contains(err.Error(), otherConfig) || !strings.Contains(err.Error(), home) {
		t.Fatalf("mismatched config/state error=%v stdout=%s stderr=%s", err, out.String(), stderr.String())
	}
}

func TestExistingWildcardConfigRequiresClientFacingEndpointBeforeInitialization(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "authority")
	configPath := hostedTestFile(t, root, "server.yaml", home, 0o600)
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	contents = []byte(strings.Replace(string(contents), "127.0.0.1:8443", "0.0.0.0:8443", 1))
	if err := os.WriteFile(configPath, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr strings.Builder
	err = Run(context.Background(), []string{"worklease", "server", "init", "--server-config", configPath}, "test", "unknown", "unknown", &out, &stderr)
	if err == nil || !strings.Contains(err.Error(), "advertisedEndpoint is required") || strings.Contains(out.String(), "0.0.0.0") {
		t.Fatalf("wildcard config result err=%v stdout=%s stderr=%s", err, out.String(), stderr.String())
	}
	if _, statErr := os.Stat(filepath.Join(home, store.HostedMarkerFileName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("wildcard config initialized authority: %v", statErr)
	}
}

func TestGuidedServerInitGeneratesTLSAndHandoff(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	configPath := filepath.Join(root, "config", "worklease", "guided.yaml")
	invitePath := filepath.Join(root, "config", "worklease", "admin.invite")
	var out, stderr strings.Builder
	args := []string{"worklease", "server", "init", "--guided", "--server-config", configPath, "--bootstrap-invite-file", invitePath, "--listen", "0.0.0.0:9443", "--endpoint", "https://localhost:9443", "--transport", "tls", "--admitted-prefix", "task:", "--admitted-prefix", "custom:", "--confirm-non-loopback", "--json"}
	if err := Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("guided init: %v stderr=%s", err, stderr.String())
	}
	cfg, err := workleaseserver.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AdvertisedEndpoint != "https://localhost:9443" || cfg.AllowInsecureHTTP || strings.Join(cfg.Prefixes, ",") != "task:,custom:" {
		t.Fatalf("guided config = %+v", cfg)
	}
	for _, path := range []string{configPath, cfg.TLSCert, cfg.TLSKey, invitePath} {
		info, statErr := os.Stat(path)
		if statErr != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("guided file %s info=%v err=%v", path, info, statErr)
		}
	}
	certificate, err := os.ReadFile(cfg.TLSCert)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := firstCertificate(certificate)
	if err != nil || leaf.VerifyHostname("localhost") != nil {
		t.Fatalf("generated certificate does not cover endpoint: leaf=%v err=%v", leaf, err)
	}
	if leaf.NotAfter.Sub(leaf.NotBefore) < 364*24*time.Hour {
		t.Fatalf("generated certificate validity = %s", leaf.NotAfter.Sub(leaf.NotBefore))
	}
	for _, required := range []string{`"authorityId"`, `"certificateSha256"`, `"startCommand"`, `"enrollCommand":"worklease enroll --invite-file `} {
		if !strings.Contains(out.String(), required) {
			t.Fatalf("guided JSON output lacks %q: %s", required, out.String())
		}
	}
	artifactBytes, err := os.ReadFile(invitePath)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := authority.DecodeInviteArtifact(strings.TrimSpace(string(artifactBytes)))
	if err != nil || artifact.ProfileHint != "remote" || artifact.Endpoint != cfg.AdvertisedEndpoint || artifact.CertificateSHA256 != certificateFingerprint(leaf) {
		t.Fatalf("bootstrap artifact=%+v err=%v", artifact, err)
	}
	if strings.Contains(out.String(), artifact.Invite) {
		t.Fatal("bootstrap invite leaked in output")
	}
	legacyArtifact := artifact
	legacyArtifact.ProfileHint = "admin"
	legacyEncoded, err := authority.EncodeInviteArtifact(legacyArtifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invitePath, []byte(legacyEncoded+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "init", "--server-config", configPath, "--bootstrap-invite-file", invitePath, "--json"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("v1.6.0 bootstrap artifact upgrade: %v stderr=%s", err, stderr.String())
	}
	upgradedBytes, err := os.ReadFile(invitePath)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := authority.DecodeInviteArtifact(strings.TrimSpace(string(upgradedBytes)))
	if err != nil || upgraded.ProfileHint != "remote" {
		t.Fatalf("upgraded bootstrap artifact=%+v err=%v", upgraded, err)
	}
	if _, err := os.Stat(invitePath + ".legacy-secret"); !os.IsNotExist(err) {
		t.Fatalf("legacy staging secret remains after artifact publication: %v", err)
	}
}

func TestGuidedAliasMatchesZeroFlagDefaultsWithoutPrompting(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	var plain, guided strings.Builder
	if err := Run(context.Background(), []string{"worklease", "server", "init", "--json"}, "test", "unknown", "unknown", &plain, &strings.Builder{}); err != nil {
		t.Fatalf("plain init: %v", err)
	}
	// A fresh XDG root gives the alias a separate authority while preserving
	// identical defaults and proving that stdin is never consulted.
	aliasRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(aliasRoot, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(aliasRoot, "state"))
	if err := Run(context.Background(), []string{"worklease", "server", "init", "--guided", "--json"}, "test", "unknown", "unknown", &guided, &strings.Builder{}); err != nil {
		t.Fatalf("guided alias init: %v", err)
	}
	for _, configRoot := range []string{root, aliasRoot} {
		cfg, err := workleaseserver.LoadConfig(filepath.Join(configRoot, "config", "worklease", "server.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Listen != "127.0.0.1:8443" || cfg.AdvertisedEndpoint != "https://127.0.0.1:8443" || cfg.AllowInsecureHTTP || strings.Join(cfg.Prefixes, ",") != "coordination:" || cfg.TLSCert == "" {
			t.Fatalf("alias defaults = %+v", cfg)
		}
	}
}

func TestServerInitRerunPreservesCertificatePin(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	var out, stderr strings.Builder
	args := []string{"worklease", "server", "init", "--json"}
	if err := Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("initial init: %v stderr=%s", err, stderr.String())
	}
	invitePath := filepath.Join(root, "config", "worklease", "bootstrap.invite")
	firstData, err := os.ReadFile(invitePath)
	if err != nil {
		t.Fatal(err)
	}
	first, err := authority.DecodeInviteArtifact(strings.TrimSpace(string(firstData)))
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("rerun init: %v stderr=%s", err, stderr.String())
	}
	secondData, err := os.ReadFile(invitePath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := authority.DecodeInviteArtifact(strings.TrimSpace(string(secondData)))
	if err != nil {
		t.Fatal(err)
	}
	if first.CertificateSHA256 == "" || second.CertificateSHA256 != first.CertificateSHA256 {
		t.Fatalf("certificate pin changed across rerun: first=%q second=%q", first.CertificateSHA256, second.CertificateSHA256)
	}
}

func TestServerInitRerunReissuesExpiredUnredeemedBootstrap(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	var out, stderr strings.Builder
	args := []string{"worklease", "server", "init", "--json"}
	if err := Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("initial init: %v stderr=%s", err, stderr.String())
	}
	invitePath := filepath.Join(root, "config", "worklease", "bootstrap.invite")
	beforeData, err := os.ReadFile(invitePath)
	if err != nil {
		t.Fatal(err)
	}
	before, err := authority.DecodeInviteArtifact(strings.TrimSpace(string(beforeData)))
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "state", "worklease", "server")
	lock, err := store.AcquireHostedLock(context.Background(), home)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(context.Background(), home, store.Options{HostedWriter: true, RequireHostedReady: true, HostedLock: lock})
	if err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := st.WriteAt(context.Background(), now, func(tx *store.Tx) error {
		_, err := tx.ExecContext(context.Background(), `UPDATE invites SET issued_at=?,expires_at=? WHERE bootstrap=1 AND state='active'`, now.Add(-2*time.Minute).UnixMicro(), now.Add(-time.Minute).UnixMicro())
		return err
	}); err != nil {
		_ = st.Close()
		_ = lock.Close()
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("expired init rerun: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(out.String(), `"bootstrapStatus":"reissued"`) {
		t.Fatalf("expired init rerun status: %s", out.String())
	}
	afterData, err := os.ReadFile(invitePath)
	if err != nil {
		t.Fatal(err)
	}
	after, err := authority.DecodeInviteArtifact(strings.TrimSpace(string(afterData)))
	if err != nil {
		t.Fatal(err)
	}
	if after.Invite == before.Invite || after.CertificateSHA256 != before.CertificateSHA256 || after.AuthorityID != before.AuthorityID {
		t.Fatalf("expired bootstrap replacement before=%+v after=%+v", before, after)
	}
}

func TestServerInitNonLoopbackAndInsecureSafety(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	var out, stderr strings.Builder
	err := Run(context.Background(), []string{"worklease", "server", "init", "--listen", "0.0.0.0:9443", "--endpoint", "https://worklease.lan:9443", "--transport", "tls", "--admitted-prefix", "coordination:"}, "test", "unknown", "unknown", &out, &stderr)
	if err == nil || !strings.Contains(err.Error(), "--confirm-non-loopback") {
		t.Fatalf("missing LAN confirmation error = %v", err)
	}
	invalidPort := []string{"worklease", "server", "init", "--listen", "127.0.0.1:99999", "--endpoint", "https://localhost:9443", "--transport", "tls", "--admitted-prefix", "coordination:"}
	if err = Run(context.Background(), invalidPort, "test", "unknown", "unknown", &out, &stderr); err == nil || !strings.Contains(err.Error(), "1 through 65535") {
		t.Fatalf("invalid listen port error = %v", err)
	}
	invalidEndpoint := []string{"worklease", "server", "init", "--listen", "127.0.0.1:9443", "--endpoint", "https://localhost:65536", "--transport", "tls", "--admitted-prefix", "coordination:"}
	if err = Run(context.Background(), invalidEndpoint, "test", "unknown", "unknown", &out, &stderr); err == nil || !strings.Contains(err.Error(), "--endpoint port") {
		t.Fatalf("invalid endpoint port error = %v", err)
	}
	args := []string{"worklease", "server", "init", "--listen", "0.0.0.0:9443", "--endpoint", "http://worklease.lan:9443", "--transport", "http", "--admitted-prefix", "coordination:"}
	if err = Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err == nil || !strings.Contains(err.Error(), "--confirm-non-loopback") {
		t.Fatalf("missing LAN confirmation error = %v", err)
	}
	args = append(args, "--confirm-non-loopback")
	if err = Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err == nil || !strings.Contains(err.Error(), "--acknowledge-cleartext-credentials") {
		t.Fatalf("missing cleartext acknowledgement error = %v", err)
	}
	args = append(args, "--acknowledge-cleartext-credentials")
	if err = Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("acknowledged insecure LAN setup: %v", err)
	}
	cfg, err := workleaseserver.LoadConfig(filepath.Join(root, "config", "worklease", "server.yaml"))
	if err != nil || !cfg.AllowInsecureHTTP || cfg.TLSCert != "" {
		t.Fatalf("insecure guided config=%+v err=%v", cfg, err)
	}
}

func TestGuidedServerInitSuppliedCertificateWarnsOnSANMismatch(t *testing.T) {
	root := t.TempDir()
	configRoot := filepath.Join(root, "config")
	if err := os.Mkdir(configRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	certPEM, keyPEM, _, err := generateGuidedCertificate("other.example")
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(configRoot, "supplied.crt"), filepath.Join(configRoot, "supplied.key")
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	var out, stderr strings.Builder
	args := []string{"worklease", "server", "init", "--guided", "--listen", "0.0.0.0:9443", "--endpoint", "https://worklease.example:9443", "--transport", "tls", "--admitted-prefix", "coordination:", "--tls-cert", certPath, "--tls-key", keyPath, "--confirm-non-loopback", "--json"}
	if err := Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("supplied certificate setup: %v", err)
	}
	if !strings.Contains(stderr.String(), "SAN does not cover") || !strings.Contains(out.String(), certificateFingerprint(mustCertificate(t, certPEM))) {
		t.Fatalf("supplied certificate handoff stdout=%s stderr=%s", out.String(), stderr.String())
	}
}

func TestGuidedSetupRejectsCollidingOutputsAndRecoversVerifiedPartialWrites(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	configPath := filepath.Join(root, "config", "worklease", "server.yaml")
	certificatePath := filepath.Join(filepath.Dir(configPath), "server.crt")
	var out, stderr strings.Builder
	args := []string{"worklease", "server", "init", "--guided", "--server-config", configPath, "--bootstrap-invite-file", certificatePath, "--listen", "127.0.0.1:9443", "--endpoint", "https://localhost:9443", "--transport", "tls", "--admitted-prefix", "coordination:"}
	if err := Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err == nil || !strings.Contains(err.Error(), "must be distinct") {
		t.Fatalf("colliding output error = %v", err)
	}
	for _, path := range []string{configPath, certificatePath, filepath.Join(filepath.Dir(configPath), "server.key"), filepath.Join(root, "state", "worklease", "server")} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("collision created %s: %v", path, err)
		}
	}

	setupLock, err := acquireGuidedSetupLock(configPath)
	if err != nil {
		t.Fatal(err)
	}
	concurrentArgs := []string{"worklease", "server", "init", "--guided", "--server-config", configPath, "--listen", "127.0.0.1:9443", "--endpoint", "https://localhost:9443", "--transport", "tls", "--admitted-prefix", "coordination:"}
	if err := Run(context.Background(), concurrentArgs, "test", "unknown", "unknown", &out, &stderr); err == nil || !strings.Contains(err.Error(), "another server setup") {
		t.Fatalf("concurrent guided setup error = %v", err)
	}
	releaseGuidedSetupLock(setupLock)
	if _, err := os.Stat(configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("concurrent setup created config: %v", err)
	}

	partial := []byte("verified partial")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certificatePath, partial, 0o600); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(filepath.Dir(configPath), "server.key")
	manifest := guidedSetupManifest{Home: filepath.Join(root, "state", "worklease", "server"), ConfigPath: configPath, CertificatePath: certificatePath, KeyPath: keyPath, InvitePath: filepath.Join(filepath.Dir(configPath), "bootstrap.invite"), GeneratedTLS: true, Targets: map[string]string{certificatePath: contentSHA256(partial), keyPath: contentSHA256([]byte("future key")), configPath: contentSHA256([]byte("future config"))}}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	journalPath := configPath + ".guided-incomplete"
	if err := os.WriteFile(journalPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recoverGuidedSetup(configPath); err != nil {
		t.Fatalf("recover partial setup: %v", err)
	}
	for _, path := range []string{certificatePath, journalPath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("recovery retained %s: %v", path, err)
		}
	}

	unrelatedPath := filepath.Join(root, "unrelated.secret")
	unrelated := []byte("must remain")
	if err := os.WriteFile(unrelatedPath, unrelated, 0o600); err != nil {
		t.Fatal(err)
	}
	malicious := guidedSetupManifest{Home: defaultServerHome(), ConfigPath: configPath, InvitePath: filepath.Join(filepath.Dir(configPath), "bootstrap.invite"), Targets: map[string]string{unrelatedPath: contentSHA256(unrelated)}}
	encoded, err = json.Marshal(malicious)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recoverGuidedSetup(configPath); err == nil || !strings.Contains(err.Error(), "out of scope") {
		t.Fatalf("out-of-scope journal error = %v", err)
	}
	if contents, err := os.ReadFile(unrelatedPath); err != nil || string(contents) != string(unrelated) {
		t.Fatalf("out-of-scope recovery changed unrelated file: %q err=%v", contents, err)
	}
}

func TestGuidedSetupReportsAndCompletesPostCommitRecovery(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	configPath := filepath.Join(root, "config", "worklease", "server.yaml")
	invitePath := filepath.Join(root, "config", "worklease", "bootstrap.invite")
	guidedArgs := []string{"worklease", "server", "init", "--guided", "--server-config", configPath, "--bootstrap-invite-file", invitePath, "--listen", "127.0.0.1:9443", "--endpoint", "https://localhost:9443", "--transport", "tls", "--admitted-prefix", "coordination:"}
	beforeHostedReadyHook = func() error {
		beforeHostedReadyHook = nil
		return errors.New("injected post-commit failure")
	}
	t.Cleanup(func() { beforeHostedReadyHook = nil })
	var out, stderr strings.Builder
	if err := Run(context.Background(), guidedArgs, "test", "unknown", "unknown", &out, &stderr); err == nil {
		t.Fatal("guided setup unexpectedly survived injected post-commit failure")
	}
	journalPath := configPath + ".guided-incomplete"
	if _, err := os.Stat(journalPath); err != nil {
		t.Fatalf("missing guided recovery record: %v", err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), guidedArgs, "test", "unknown", "unknown", &out, &stderr); err == nil || !strings.Contains(err.Error(), "setup overrides apply only during initial server init") {
		t.Fatalf("guided retry recovery error = %v", err)
	}
	resumeArgs := []string{"worklease", "server", "init", "--server-config", configPath, "--bootstrap-invite-file", invitePath}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), resumeArgs, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("guided setup recovery: %v stderr=%s", err, stderr.String())
	}
	if _, err := os.Stat(journalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("guided recovery record remains: %v", err)
	}
	cfg, err := workleaseserver.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if ready, err := store.HostedReady(cfg.Home); err != nil || !ready {
		t.Fatalf("recovered guided authority ready=%v err=%v", ready, err)
	}
}

func TestGuidedHandoffCommandsQuotePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "with spaces")
	configRoot := filepath.Join(root, "config")
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	configPath := filepath.Join(configRoot, "worklease", "server config.yaml")
	invitePath := filepath.Join(configRoot, "worklease", "admin invite")
	var out, stderr strings.Builder
	args := []string{"worklease", "server", "init", "--guided", "--server-config", configPath, "--bootstrap-invite-file", invitePath, "--listen", "127.0.0.1:9443", "--endpoint", "https://localhost:9443", "--transport", "tls", "--admitted-prefix", "coordination:", "--json"}
	if err := Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "worklease enroll --invite-file '") || !strings.Contains(out.String(), "worklease serve --server-config '") {
		t.Fatalf("handoff paths are not shell quoted: %s", out.String())
	}
}

func TestGuidedCertificateValidationRejectsUnsafeMismatchAndExpiry(t *testing.T) {
	root := t.TempDir()
	firstCert, firstKey, _, err := generateGuidedCertificate("localhost")
	if err != nil {
		t.Fatal(err)
	}
	_, secondKey, _, err := generateGuidedCertificate("localhost")
	if err != nil {
		t.Fatal(err)
	}
	expiredCert, expiredKey, _, err := generateGuidedCertificateAt("localhost", time.Now().Add(-48*time.Hour), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, contents []byte, mode os.FileMode) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, contents, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	firstCertPath := write("first.crt", firstCert, 0o600)
	firstKeyPath := write("first.key", firstKey, 0o600)
	secondKeyPath := write("second.key", secondKey, 0o600)
	expiredCertPath := write("expired.crt", expiredCert, 0o600)
	expiredKeyPath := write("expired.key", expiredKey, 0o600)
	expiredChainPath := write("expired-chain.crt", append(append([]byte(nil), firstCert...), expiredCert...), 0o600)
	if _, _, _, err := inspectGuidedCertificate(firstCertPath, secondKeyPath, "localhost"); err == nil || !strings.Contains(err.Error(), "matching") {
		t.Fatalf("mismatched pair error = %v", err)
	}
	if _, _, _, err := inspectGuidedCertificate(expiredCertPath, expiredKeyPath, "localhost"); err == nil || !strings.Contains(err.Error(), "currently valid") {
		t.Fatalf("expired pair error = %v", err)
	}
	if _, _, _, err := inspectGuidedCertificate(expiredChainPath, firstKeyPath, "localhost"); err == nil || !strings.Contains(err.Error(), "currently valid") {
		t.Fatalf("expired chain error = %v", err)
	}
	if err := os.Chmod(firstKeyPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := inspectGuidedCertificate(firstCertPath, firstKeyPath, "localhost"); err == nil || !strings.Contains(err.Error(), "owner-private") {
		t.Fatalf("unsafe key error = %v", err)
	}
}

func mustCertificate(t *testing.T, contents []byte) *x509.Certificate {
	t.Helper()
	certificate, err := firstCertificate(contents)
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}

func TestHostedInitAndReissueAreDurableAndRedacted(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "authority")
	cfg := hostedTestFile(t, root, "server.conf", home, 0o600)
	secretDir := filepath.Join(root, "secrets")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(secretDir, "bootstrap")
	second := filepath.Join(secretDir, "bootstrap-2")
	var out, stderr strings.Builder
	if err := Run(context.Background(), []string{"worklease", "server", "init", "--home", home, "--server-config", cfg, "--bootstrap-invite-file", first, "--json"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
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
	if err := Run(context.Background(), []string{"worklease", "server", "bootstrap-reissue", "--home", home, "--bootstrap-invite-file", second, "--json"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
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

func TestServerInitRefusesReadyHomeWithMissingDatabase(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "authority")
	cfg := hostedTestFile(t, root, "server.conf", home, 0o600)
	secretDir := filepath.Join(root, "secrets")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	invite := filepath.Join(secretDir, "bootstrap")
	args := []string{"worklease", "server", "init", "--server-config", cfg, "--bootstrap-invite-file", invite}
	var out, stderr strings.Builder
	if err := Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(filepath.Join(home, store.DatabaseFileName) + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
	out.Reset()
	stderr.Reset()
	err := Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr)
	if failure := reason.As(err); failure == nil || failure.Reason != reason.ReasonStorageFailure {
		t.Fatalf("missing ready database error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, store.DatabaseFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing authority database was recreated: %v", err)
	}
}

func TestHostedInitRecoversCommittedGrantBeforeReadyMarker(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "authority")
	cfg := hostedTestFile(t, root, "server.conf", home, 0o600)
	secretDir := filepath.Join(root, "secrets")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	invite := filepath.Join(secretDir, "bootstrap")
	args := []string{"worklease", "server", "init", "--home", home, "--server-config", cfg, "--bootstrap-invite-file", invite, "--json"}
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
	secretDir := filepath.Join(root, "secrets")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "resume")
	cfg := hostedTestFile(t, root, "server.conf", home, 0o600)
	if err := store.MarkHosted(home); err != nil {
		t.Fatal(err)
	}
	var out, stderr strings.Builder
	if err := Run(context.Background(), []string{"worklease", "server", "init", "--home", home, "--server-config", cfg, "--bootstrap-invite-file", filepath.Join(secretDir, "bootstrap")}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("resume marker-only init: %v", err)
	}
	nonempty := filepath.Join(root, "nonempty")
	nonemptyCfg := hostedTestFile(t, root, "nonempty-server.conf", nonempty, 0o600)
	if err := os.Mkdir(nonempty, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonempty, "existing"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), []string{"worklease", "server", "init", "--home", nonempty, "--server-config", nonemptyCfg, "--bootstrap-invite-file", filepath.Join(secretDir, "other")}, "test", "unknown", "unknown", &out, &stderr); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonInvalidArgument {
		t.Fatalf("non-empty init error=%v", err)
	}
	markedJunk := filepath.Join(root, "marked-junk")
	markedCfg := hostedTestFile(t, root, "marked-server.conf", markedJunk, 0o600)
	if err := store.MarkHosted(markedJunk); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(markedJunk, "junk"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), []string{"worklease", "server", "init", "--home", markedJunk, "--server-config", markedCfg, "--bootstrap-invite-file", filepath.Join(secretDir, "marked-other")}, "test", "unknown", "unknown", &out, &stderr); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonInvalidArgument {
		t.Fatalf("marked non-empty init error=%v", err)
	}
}

func TestHostedCommandsRefuseHeldLockBeforeOpeningDatabase(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "authority")
	cfg := hostedTestFile(t, root, "server.conf", home, 0o600)
	secretDir := filepath.Join(root, "secrets")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	invite := filepath.Join(secretDir, "bootstrap")
	var out, stderr strings.Builder
	if err := Run(context.Background(), []string{"worklease", "server", "init", "--home", home, "--server-config", cfg, "--bootstrap-invite-file", invite}, "test", "unknown", "unknown", &out, &stderr); err != nil {
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
		{"worklease", "server", "init", "--home", home, "--server-config", cfg, "--bootstrap-invite-file", invite},
		{"worklease", "server", "restore", "--home", home, "--from", backup, "--selected-cutoff", now.Format(time.RFC3339Nano), "--loss-interval-start", now.Format(time.RFC3339Nano), "--loss-interval-end", now.Format(time.RFC3339Nano), "--bootstrap-invite-file", filepath.Join(secretDir, "restored")},
		{"worklease", "server", "bootstrap-reissue", "--home", home, "--bootstrap-invite-file", filepath.Join(secretDir, "next")},
		{"worklease", "server", "reset", "--home", home, "--bootstrap-invite-file", invite, "--confirm-reset"},
		{"worklease", "server", "retire", "--home", home, "--confirm-retire"},
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

func TestServerResetPreservesDeploymentAndSupportsReinitialization(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	var out, stderr strings.Builder
	if err := Run(context.Background(), []string{"worklease", "server", "init"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config", "worklease", "server.yaml")
	invitePath := filepath.Join(root, "config", "worklease", "bootstrap.invite")
	cfg, err := workleaseserver.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	originalConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	originalCertificate, err := os.ReadFile(cfg.TLSCert)
	if err != nil {
		t.Fatal(err)
	}
	originalStore, err := store.Open(context.Background(), cfg.Home, store.Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	originalAuthority := originalStore.AuthorityID()
	if err := originalStore.Close(); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "reset"}, "test", "unknown", "unknown", &out, &stderr); err == nil || !strings.Contains(err.Error(), cfg.Home) || !strings.Contains(err.Error(), "--confirm-reset") {
		t.Fatalf("reset preview error=%v stdout=%s stderr=%s", err, out.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(cfg.Home, store.DatabaseFileName)); err != nil {
		t.Fatalf("reset preview mutated authority: %v", err)
	}

	legacyPath := invitePath + ".legacy-secret"
	if err := store.WriteHostedSecret(legacyPath, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "reset", "--confirm-reset"}, "test", "unknown", "unknown", &out, &stderr); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonCredentialUnsafe {
		t.Fatalf("staged-secret reset error=%v", err)
	}
	if err := os.Rename(legacyPath, legacyPath+".parked"); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "reset", "--confirm-reset", "--force", "--unresolved-export", configPath}, "test", "unknown", "unknown", &out, &stderr); err == nil || !strings.Contains(err.Error(), "conflicts with a server deployment artifact") {
		t.Fatalf("config-collision reset error=%v", err)
	}
	if current, err := os.ReadFile(configPath); err != nil || string(current) != string(originalConfig) {
		t.Fatalf("config-collision reset changed config: err=%v", err)
	}

	aliasParent := filepath.Join(filepath.Dir(configPath), "home-alias")
	if err := os.Symlink(cfg.Home, aliasParent); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "reset", "--confirm-reset", "--force", "--unresolved-export", filepath.Join(aliasParent, "export.json")}, "test", "unknown", "unknown", &out, &stderr); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonHomeUnsafe {
		t.Fatalf("aliased-export reset error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.Home, "export.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("aliased export entered hosted home: %v", err)
	}

	artifactBytes, err := os.ReadFile(invitePath)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "bootstrap-reissue"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatal(err)
	}
	currentArtifactBytes, err := os.ReadFile(invitePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invitePath, artifactBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "reset", "--confirm-reset"}, "test", "unknown", "unknown", &out, &stderr); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonCredentialUnsafe {
		t.Fatalf("superseded-bootstrap reset error=%v", err)
	}
	if err := os.WriteFile(invitePath, currentArtifactBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	artifactBytes = currentArtifactBytes
	artifact, err := authority.DecodeInviteArtifact(strings.TrimSpace(string(artifactBytes)))
	if err != nil {
		t.Fatal(err)
	}
	artifact.AuthorityID = strings.Repeat("f", 32)
	conflicting, err := authority.EncodeInviteArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invitePath, []byte(conflicting+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	mismatchExport := filepath.Join(filepath.Dir(configPath), "mismatch-export.json")
	if err := Run(context.Background(), []string{"worklease", "server", "reset", "--confirm-reset", "--force", "--unresolved-export", mismatchExport}, "test", "unknown", "unknown", &out, &stderr); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonAuthorityMismatch {
		t.Fatalf("conflicting bootstrap reset error=%v", err)
	}
	if _, err := os.Stat(mismatchExport); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("conflicting bootstrap reset wrote export: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.Home, store.DatabaseFileName)); err != nil {
		t.Fatalf("conflicting bootstrap reset mutated authority: %v", err)
	}
	artifact.AuthorityID = originalAuthority
	artifact.Invite = strings.Repeat("b", 64)
	nonBootstrap, err := authority.EncodeInviteArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invitePath, []byte(nonBootstrap+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "reset", "--confirm-reset"}, "test", "unknown", "unknown", &out, &stderr); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonCredentialUnsafe {
		t.Fatalf("non-bootstrap artifact reset error=%v", err)
	}
	if err := os.WriteFile(invitePath, artifactBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "reset", "--confirm-reset", "--json"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("reset: %v stdout=%s stderr=%s", err, out.String(), stderr.String())
	}
	for _, path := range []string{filepath.Join(cfg.Home, store.DatabaseFileName), filepath.Join(cfg.Home, store.HostedReadyFileName), invitePath} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("reset retained %s: %v", path, err)
		}
	}
	if current, err := os.ReadFile(configPath); err != nil || string(current) != string(originalConfig) {
		t.Fatalf("reset changed config: err=%v", err)
	}
	if current, err := os.ReadFile(cfg.TLSCert); err != nil || string(current) != string(originalCertificate) {
		t.Fatalf("reset changed certificate: err=%v", err)
	}

	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "init"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("reinitialize after reset: %v stderr=%s", err, stderr.String())
	}
	reinitialized, err := store.Open(context.Background(), cfg.Home, store.Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if reinitialized.AuthorityID() == originalAuthority {
		t.Fatal("reset reused the retired authority ID")
	}
	if err := reinitialized.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestServerResetRecoversMissingDefaultConfiguration(t *testing.T) {
	root := t.TempDir()
	configRoot := filepath.Join(root, "config")
	stateRoot := filepath.Join(root, "state")
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("XDG_STATE_HOME", stateRoot)
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	var out, stderr strings.Builder
	if err := Run(context.Background(), []string{"worklease", "server", "init"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configRoot, "worklease", "server.yaml")
	cfg, err := workleaseserver.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	parked := filepath.Join(root, "parked")
	if err := os.Mkdir(parked, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{configPath, cfg.TLSCert, cfg.TLSKey} {
		if err := os.Rename(path, filepath.Join(parked, filepath.Base(path))); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("WORKLEASE_SERVER_CONFIG", configPath)
	out.Reset()
	stderr.Reset()
	err = Run(context.Background(), []string{"worklease", "server", "init"}, "test", "unknown", "unknown", &out, &stderr)
	if err == nil || !strings.Contains(err.Error(), "worklease server reset --home") || !strings.Contains(err.Error(), cfg.Home) {
		t.Fatalf("missing-config guidance=%v stdout=%s stderr=%s", err, out.String(), stderr.String())
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "reset", "--home", cfg.Home, "--confirm-reset"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("missing-config reset: %v stderr=%s", err, stderr.String())
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "init"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("guided init over retired home: %v stderr=%s", err, stderr.String())
	}
	if _, err := workleaseserver.LoadConfig(configPath); err != nil {
		t.Fatalf("replacement config: %v", err)
	}
}

func TestServerResetClearsReadinessBeforeArtifactRemoval(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	var out, stderr strings.Builder
	if err := Run(context.Background(), []string{"worklease", "server", "init"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config", "worklease", "server.yaml")
	invitePath := filepath.Join(root, "config", "worklease", "bootstrap.invite")
	cfg, err := workleaseserver.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeResetArtifactRemovalHook = func() error { return errors.New("injected reset interruption") }
	t.Cleanup(func() { beforeResetArtifactRemovalHook = nil })
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "reset", "--confirm-reset"}, "test", "unknown", "unknown", &out, &stderr); err == nil || !strings.Contains(err.Error(), "injected reset interruption") {
		t.Fatalf("interrupted reset error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(cfg.Home, store.HostedReadyFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted reset retained readiness: %v", err)
	}
	for _, path := range []string{invitePath, filepath.Join(cfg.Home, store.DatabaseFileName), filepath.Join(cfg.Home, store.HostedResetFileName)} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("interrupted reset lost recovery state %s: %v", path, err)
		}
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "serve"}, "test", "unknown", "unknown", &out, &stderr); err == nil {
		t.Fatal("serve restarted an authority after reset readiness was cleared")
	}
	beforeResetArtifactRemovalHook = nil
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "reset", "--confirm-reset"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("reset retry after interruption: %v stderr=%s", err, stderr.String())
	}
}

func TestServerResetResumesInterruptedCleanupStates(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	var out, stderr strings.Builder
	if err := Run(context.Background(), []string{"worklease", "server", "init"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config", "worklease", "server.yaml")
	invitePath := filepath.Join(root, "config", "worklease", "bootstrap.invite")
	cfg, err := workleaseserver.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := store.AcquireHostedLock(context.Background(), cfg.Home)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ClearHostedReady(lock); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "reset", "--confirm-reset"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("resume after readiness removal: %v stderr=%s", err, stderr.String())
	}
	if err := Run(context.Background(), []string{"worklease", "server", "init"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatal(err)
	}
	parked := filepath.Join(root, "parked")
	if err := os.Mkdir(parked, 0o700); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(cfg.Home, store.DatabaseFileName)
	missingDatabase := filepath.Join(parked, store.DatabaseFileName)
	if err := os.Rename(databasePath, missingDatabase); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "reset", "--confirm-reset"}, "test", "unknown", "unknown", &out, &stderr); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonStorageFailure {
		t.Fatalf("missing database without intent error=%v", err)
	}
	if err := os.Rename(missingDatabase, databasePath); err != nil {
		t.Fatal(err)
	}
	lock, err = store.AcquireHostedLock(context.Background(), cfg.Home)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ClearHostedReady(lock); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	if err := store.WriteHostedResetIntent(lock, store.HostedResetIntent{Version: 1, BootstrapInvitePath: invitePath}); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{invitePath, databasePath} {
		if err := os.Rename(path, filepath.Join(parked, filepath.Base(path))); err != nil {
			t.Fatal(err)
		}
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "reset", "--confirm-reset"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("resume after database removal: %v stderr=%s", err, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(cfg.Home, store.HostedReadyFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("resumed reset retained readiness: %v", err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "init"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("init after resumed reset: %v stderr=%s", err, stderr.String())
	}
}

func TestHostedRestoreAndForcedRetirement(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "authority")
	cfg := hostedTestFile(t, root, "server.conf", home, 0o600)
	secretDir := filepath.Join(root, "secrets")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var out, stderr strings.Builder
	firstSecret := filepath.Join(secretDir, "bootstrap")
	if err := Run(context.Background(), []string{"worklease", "server", "init", "--home", home, "--server-config", cfg, "--bootstrap-invite-file", firstSecret, "--json"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
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
	restoreArgs := []string{"worklease", "server", "restore", "--home", home, "--from", backup, "--selected-cutoff", now.Add(-time.Minute).Format(time.RFC3339Nano), "--loss-interval-start", now.Add(-time.Minute).Format(time.RFC3339Nano), "--loss-interval-end", now.Format(time.RFC3339Nano), "--bootstrap-invite-file", filepath.Join(secretDir, "restored"), "--json"}
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
	resetExport := filepath.Join(secretDir, "reset.json")
	if err := Run(context.Background(), []string{"worklease", "server", "reset", "--home", home, "--bootstrap-invite-file", firstSecret, "--confirm-reset", "--force", "--unresolved-export", resetExport}, "test", "unknown", "unknown", &out, &stderr); err == nil || !strings.Contains(err.Error(), "active claims") {
		t.Fatalf("active-claim reset error=%v", err)
	}
	if _, err := os.Stat(resetExport); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("active-claim reset wrote export: %v", err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "retire", "--home", home, "--json"}, "test", "unknown", "unknown", &out, &stderr); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonInvalidArgument {
		t.Fatalf("unsafe retire error=%v", err)
	}
	exportPath := filepath.Join(secretDir, "retirement.json")
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "retire", "--home", home, "--confirm-retire", "--force", "--unresolved-export", exportPath, "--json"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
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

func TestServerInitRefusesStagedBootstrapSecretFromAnotherRun(t *testing.T) {
	root := t.TempDir()
	secretDir := filepath.Join(root, "secrets")
	if err := os.Mkdir(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	invitePath := filepath.Join(secretDir, "bootstrap")
	stale := strings.Repeat("b", 64)
	if err := store.WriteHostedSecret(invitePath+".legacy-secret", stale); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "authority")
	cfg := hostedTestFile(t, root, "server.conf", home, 0o600)
	var out, stderr strings.Builder
	err := Run(context.Background(), []string{"worklease", "server", "init", "--home", home, "--server-config", cfg, "--bootstrap-invite-file", invitePath}, "test", "unknown", "unknown", &out, &stderr)
	if failure := reason.As(err); failure == nil || failure.Reason != reason.ReasonCredentialUnsafe {
		t.Fatalf("stale staged secret accepted: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, store.HostedMarkerFileName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("rejected init marked the authority home: %v", statErr)
	}
	if _, statErr := os.Stat(invitePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("rejected init published a bootstrap artifact: %v", statErr)
	}

	// An interrupted initialization of this same authority still resumes and
	// reuses its own staged secret.
	if err := store.MarkHosted(home); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if err := Run(context.Background(), []string{"worklease", "server", "init", "--home", home, "--server-config", cfg, "--bootstrap-invite-file", invitePath}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("resume with staged secret: %v stderr=%s", err, stderr.String())
	}
	data, readErr := os.ReadFile(invitePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	artifact, decodeErr := authority.DecodeInviteArtifact(strings.TrimSpace(string(data)))
	if decodeErr != nil || artifact.Invite != stale {
		t.Fatalf("resumed artifact did not reuse its staged secret: artifact=%+v err=%v", artifact, decodeErr)
	}
}

func TestGuidedServerInitRefusesStagedBootstrapSecret(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("WORKLEASE_SERVER_CONFIG", "")
	configPath := filepath.Join(root, "config", "worklease", "guided.yaml")
	invitePath := filepath.Join(root, "config", "worklease", "admin.invite")
	if err := os.MkdirAll(filepath.Dir(invitePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteHostedSecret(invitePath+".legacy-secret", strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	var out, stderr strings.Builder
	args := []string{"worklease", "server", "init", "--guided", "--server-config", configPath, "--bootstrap-invite-file", invitePath, "--listen", "127.0.0.1:9443", "--endpoint", "https://localhost:9443", "--transport", "tls", "--admitted-prefix", "task:", "--json"}
	if err := Run(context.Background(), args, "test", "unknown", "unknown", &out, &stderr); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonInvalidArgument {
		t.Fatalf("guided init accepted a staged bootstrap secret: %v", err)
	}
	for _, path := range []string{configPath, invitePath, configPath + ".guided-incomplete"} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("rejected guided init created %s: %v", path, statErr)
		}
	}
}
