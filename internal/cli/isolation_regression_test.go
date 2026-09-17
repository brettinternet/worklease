package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestIsolatedCLIRejectsHostileRemoteConfiguration(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()

	hostile := t.TempDir()
	hostileConfig := filepath.Join(hostile, "config.yaml")
	if err := os.WriteFile(hostileConfig, []byte("invalid: [poisoned\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := config.ProfilePaths{
		Profiles: filepath.Join(hostile, "config", "worklease", "profiles.yaml"),
		Bindings: filepath.Join(hostile, "config", "worklease", "bindings.yaml"),
	}
	if err := config.SaveProfiles(paths, []config.Profile{{Name: "hostile", Endpoint: server.URL, AuthorityID: strings.Repeat("a", 32), Credential: config.CredentialDescriptor{Path: filepath.Join(hostile, "credential")}, AllowInsecureHTTP: true}}, "hostile"); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveBindings(paths, map[string]string{mustGetwd(): "hostile"}); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"HOME":                    filepath.Join(hostile, "home"),
		"XDG_CONFIG_HOME":         filepath.Join(hostile, "config"),
		"XDG_STATE_HOME":          filepath.Join(hostile, "state"),
		"WORKLEASE_HOME":          filepath.Join(hostile, "state", "worklease"),
		"WORKLEASE_CONFIG":        hostileConfig,
		"WORKLEASE_PROFILE":       "hostile",
		"WORKLEASE_SERVER_CONFIG": hostileConfig,
	} {
		t.Setenv(key, value)
	}

	selection, err := config.SelectProfile(nil, os.Getenv, mustGetwd(), paths)
	if err != nil || selection.Name != "hostile" || selection.Profile == nil || selection.Profile.Endpoint != server.URL {
		t.Fatalf("hostile fixture did not select its remote profile: selection=%+v err=%v", selection, err)
	}

	restore, err := testkit.IsolateProcessEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	localHome := t.TempDir()
	var out, stderr strings.Builder
	runErr := Run(context.Background(), []string{"worklease", "acquire", "--home", localHome, "--path", "README.md"}, "test", "unknown", "unknown", &out, &stderr)
	restore()
	if runErr != nil {
		t.Fatalf("isolated in-process operation: %v (%s)", runErr, out.String())
	}

	childHome := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestIsolatedCLIProcessHelper$", "--", childHome)
	cmd.Env = testkit.Environment(os.Environ(), map[string]string{
		"HOME": filepath.Join(hostile, "home"), "XDG_CONFIG_HOME": filepath.Join(hostile, "config"), "XDG_STATE_HOME": filepath.Join(hostile, "state"),
		"WORKLEASE_HOME": filepath.Join(hostile, "state", "worklease"), "WORKLEASE_CONFIG": hostileConfig, "WORKLEASE_PROFILE": "hostile", "WORKLEASE_SERVER_CONFIG": hostileConfig,
		"TEST_ISOLATED_CLI_HELPER": "1",
	})
	if output, childErr := cmd.CombinedOutput(); childErr != nil {
		t.Fatalf("isolated child operation: %v (%s)", childErr, output)
	}
	if requests.Load() != 0 {
		t.Fatalf("hostile endpoint received %d requests", requests.Load())
	}
	if data, readErr := os.ReadFile(hostileConfig); readErr != nil || string(data) != "invalid: [poisoned\n" {
		t.Fatalf("hostile config changed or was unavailable: %v", readErr)
	}
}

func TestIsolatedCLIProcessHelper(t *testing.T) {
	if os.Getenv("TEST_ISOLATED_CLI_HELPER") != "1" {
		return
	}
	separator := 0
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index + 1
			break
		}
	}
	if separator == 0 || separator >= len(os.Args) {
		t.Fatal("missing isolated child home")
	}
	var out, stderr strings.Builder
	if err := Run(context.Background(), []string{"worklease", "acquire", "--home", os.Args[separator], "--path", "README.md"}, "test", "unknown", "unknown", &out, &stderr); err != nil {
		t.Fatalf("isolated local operation: %v (%s)", err, out.String())
	}
}
