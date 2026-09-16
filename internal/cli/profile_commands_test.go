package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
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

func TestProfileShowWithoutSelectableProfileGuidesRecovery(t *testing.T) {
	saveTestProfiles(t, nil, "")
	_, err := runProfileCLI(t, "profile", "show")
	if err == nil {
		t.Fatal("expected missing profile error")
	}
	for _, want := range []string{"profile list", "enroll"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
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
}

func TestProfileCommandHelpDocumentsNameArguments(t *testing.T) {
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
