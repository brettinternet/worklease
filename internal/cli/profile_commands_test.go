package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/reason"
)

func saveTestProfiles(t *testing.T, profiles []config.Profile, defaultName string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	for i := range profiles {
		if profiles[i].Credential.Path == "" {
			profiles[i].Credential.Path = filepath.Join(root, "credentials", profiles[i].Name)
		}
	}
	if err := config.SaveProfiles(config.UserProfilePaths(nil), profiles, defaultName); err != nil {
		t.Fatal(err)
	}
}

func testProfile(name string) config.Profile {
	return config.Profile{
		Name:              name,
		Endpoint:          "https://" + name + ".example.com",
		AuthorityID:       strings.Repeat("a", 32),
		RestoreID:         strings.Repeat("b", 32),
		CertificateSHA256: strings.Repeat("c", 64),
	}
}

func runProfileCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), append([]string{"worklease"}, args...), "dev", "unknown", "unknown", &stdout, &stderr)
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	return stdout.String(), err
}

func TestForcedLocalBypassesUnreadableProfileStore(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	configDir := filepath.Join(root, "worklease")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "profiles.yaml"), []byte("not: valid: profiles"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := runProfileCLI(t, "--local", "profile", "show")
	if err != nil || out != "local (selected by forced-local)\n" {
		t.Fatalf("output=%q err=%v", out, err)
	}
}

func TestReservedLocalMutationsFailBeforeSideEffects(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	_, err := runProfileCLI(t, "profile", "add", "local", "--endpoint", "https://authority.example")
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("add local error=%v", err)
	}
	_, err = runProfileCLI(t, "profile", "remove", "local")
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("remove local error=%v", err)
	}
	_, err = runProfileCLI(t, "enroll", "--profile", "local", "--invite-file", filepath.Join(root, "missing.invite"))
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("enroll local error=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "worklease", "profiles.yaml")); !os.IsNotExist(statErr) {
		t.Fatalf("reserved mutation created profile store: %v", statErr)
	}
}

func TestPersistedLocalCollisionReportsMigrationGuidance(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	paths := config.UserProfilePaths(nil)
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(paths.Profiles)); err != nil {
		t.Fatal(err)
	}
	contents := "profiles:\n  - name: local\n    endpoint: https://legacy.example\n    authorityId: " + strings.Repeat("a", 32) + "\n    restoreId: " + strings.Repeat("b", 32) + "\n    credential:\n      path: " + filepath.Join(root, "credentials", "legacy") + "\ndefault: local\n"
	if err := os.WriteFile(paths.Profiles, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--json", "profile", "list"}, {"--json", "profile", "default"}, {"--json", "profile", "bind", "local"}} {
		out, err := runProfileCLI(t, args...)
		classified := reason.As(err)
		if classified == nil || classified.Reason != reason.ReasonConfigInvalid || !strings.Contains(out, "rename the remote profile") || !strings.Contains(out, "retain credential path") {
			t.Fatalf("args=%v output=%q err=%v", args, out, err)
		}
	}
}

func TestProfileShowWithoutNameUsesSelectionPrecedence(t *testing.T) {
	saveTestProfiles(t, []config.Profile{testProfile("default-team"), testProfile("env-team")}, "default-team")
	t.Setenv("WORKLEASE_PROFILE", "env-team")

	out, err := runProfileCLI(t, "profile", "show")
	if err != nil {
		t.Fatal(err)
	}
	if out != "env-team (selected by env)\n" {
		t.Fatalf("output = %q", out)
	}

	out, err = runProfileCLI(t, "--json", "profile", "show")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Operation       string `json:"operation"`
		SelectionSource string `json:"selectionSource"`
		Profile         struct {
			Name string `json:"name"`
		} `json:"profile"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Operation != "profile-show" || result.SelectionSource != "env" || result.Profile.Name != "env-team" {
		t.Fatalf("result = %+v", result)
	}
}

func TestProfileShowWithoutSelectableProfileShowsBuiltInLocal(t *testing.T) {
	saveTestProfiles(t, nil, "")
	out, err := runProfileCLI(t, "profile", "show")
	if err != nil || out != "local (selected by local)\n" {
		t.Fatalf("output=%q err=%v", out, err)
	}
	out, err = runProfileCLI(t, "--json", "profile", "show", "local")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Profile struct {
			Name     string `json:"name"`
			Builtin  bool   `json:"builtin"`
			Endpoint string `json:"endpoint"`
		} `json:"profile"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Profile.Name != "local" || !result.Profile.Builtin || result.Profile.Endpoint != "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestProfileDefaultWithoutNameReportsWithoutMutation(t *testing.T) {
	profiles := []config.Profile{testProfile("one"), testProfile("two")}
	saveTestProfiles(t, profiles, "one")

	out, err := runProfileCLI(t, "profile", "default")
	if err != nil || out != "one\n" {
		t.Fatalf("output=%q err=%v", out, err)
	}
	out, err = runProfileCLI(t, "--json", "profile", "default")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Operation string  `json:"operation"`
		Default   *string `json:"default"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Operation != "profile-default" || result.Default == nil || *result.Default != "one" {
		t.Fatalf("result = %+v", result)
	}

	if _, err := runProfileCLI(t, "profile", "default", "two"); err != nil {
		t.Fatal(err)
	}
	_, defaultName, err := config.LoadProfiles(config.UserProfilePaths(nil))
	if err != nil || defaultName != "two" {
		t.Fatalf("default=%q err=%v", defaultName, err)
	}
}

func TestProfileDefaultLocalAndBindLocalPersistSelectionsWithoutRemoteProfile(t *testing.T) {
	remote := testProfile("team")
	saveTestProfiles(t, []config.Profile{remote}, "team")
	out, err := runProfileCLI(t, "profile", "default", "local")
	if err != nil || out != "profile-default completed\n" {
		t.Fatalf("default local output=%q err=%v", out, err)
	}
	profiles, defaultName, err := config.LoadProfiles(config.UserProfilePaths(nil))
	if err != nil || defaultName != "local" || len(profiles) != 1 || profiles["team"].Name != "team" {
		t.Fatalf("profiles=%+v default=%q err=%v", profiles, defaultName, err)
	}
	if _, err := runProfileCLI(t, "profile", "bind", "local"); err != nil {
		t.Fatal(err)
	}
	checkoutRoot, err := handle.ContextRoot(mustGetwd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := config.LoadBinding(config.UserProfilePaths(nil), checkoutRoot)
	if err != nil || binding != "local" {
		t.Fatalf("binding=%q err=%v", binding, err)
	}
	out, err = runProfileCLI(t, "profile", "default")
	if err != nil || out != "local\n" {
		t.Fatalf("default report=%q err=%v", out, err)
	}
	out, err = runProfileCLI(t, "--json", "profile", "list")
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Profiles map[string]any `json:"profiles"`
		Local    struct {
			Name    string `json:"name"`
			Builtin bool   `json:"builtin"`
		} `json:"local"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Profiles) != 1 || listed.Local.Name != "local" || !listed.Local.Builtin {
		t.Fatalf("list=%+v", listed)
	}
}

func TestProfileDefaultWithoutConfiguredDefault(t *testing.T) {
	saveTestProfiles(t, []config.Profile{testProfile("one")}, "")
	out, err := runProfileCLI(t, "profile", "default")
	if err != nil || out != "no default profile\n" {
		t.Fatalf("output=%q err=%v", out, err)
	}
	out, err = runProfileCLI(t, "--json", "profile", "default")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if value, exists := result["default"]; !exists || value != nil {
		t.Fatalf("result = %#v", result)
	}
	out, err = runProfileCLI(t, "--json", "profile", "list")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if value, exists := result["default"]; !exists || value != "" {
		t.Fatalf("profile list changed unset default shape: %#v", result)
	}
}

func TestProfileCommandHelpDocumentsNameArguments(t *testing.T) {
	out, err := runProfileCLI(t, "profile", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--profile, WORKLEASE_PROFILE, checkout binding, user default, then implicit local", "Selecting local explicitly stops fallback", "--local instead forces local", "Unbind removes only the checkout override", "persisted remote profile named local must be renamed manually"} {
		if !strings.Contains(out, want) {
			t.Errorf("profile help missing %q: %q", want, out)
		}
	}

	tests := map[string][]string{
		"show":    {"worklease profile show [NAME]", "Without NAME"},
		"default": {"worklease profile default [NAME]", "Without NAME"},
		"remove":  {"worklease profile remove NAME", "NAME is required"},
		"bind":    {"worklease profile bind NAME [--cwd DIR]", "NAME is required"},
		"unbind":  {"worklease profile unbind [--cwd DIR]", "takes no profile NAME"},
	}
	for command, wants := range tests {
		t.Run(command, func(t *testing.T) {
			out, err := runProfileCLI(t, "profile", command, "--help")
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range wants {
				if !strings.Contains(out, want) {
					t.Errorf("help missing %q: %q", want, out)
				}
			}
		})
	}
}
