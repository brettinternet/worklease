package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileSelectionPrecedenceAndLocalFallback(t *testing.T) {
	dir := t.TempDir()
	paths := ProfilePaths{Profiles: filepath.Join(dir, "profiles.yaml"), Bindings: filepath.Join(dir, "bindings.yaml")}
	profiles := []Profile{{Name: "flag", Endpoint: "https://flag.example", AuthorityID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RestoreID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Credential: CredentialDescriptor{Path: filepath.Join(dir, "flag.cred")}}, {Name: "env", Endpoint: "https://env.example", AuthorityID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RestoreID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Credential: CredentialDescriptor{Path: filepath.Join(dir, "env.cred")}}, {Name: "bind", Endpoint: "https://bind.example", AuthorityID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RestoreID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Credential: CredentialDescriptor{Path: filepath.Join(dir, "bind.cred")}}, {Name: "default", Endpoint: "https://default.example", AuthorityID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", RestoreID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Credential: CredentialDescriptor{Path: filepath.Join(dir, "default.cred")}}}
	if err := SaveProfiles(paths, profiles, "default"); err != nil {
		t.Fatal(err)
	}
	if err := SaveBindings(paths, map[string]string{"/checkout": "bind"}); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == "WORKLEASE_PROFILE" {
			return "env"
		}
		return ""
	}
	got, err := SelectProfile(map[string]string{"profile": "flag"}, env, "/checkout", paths)
	if err != nil || got.Name != "flag" || got.Source != "flag" {
		t.Fatalf("flag precedence: %#v %v", got, err)
	}
	got, err = SelectProfile(nil, env, "/checkout", paths)
	if err != nil || got.Name != "env" {
		t.Fatalf("env precedence: %#v %v", got, err)
	}
	got, err = SelectProfile(nil, func(string) string { return "" }, "/checkout", paths)
	if err != nil || got.Name != "bind" {
		t.Fatalf("binding precedence: %#v %v", got, err)
	}
	got, err = SelectProfile(nil, func(string) string { return "" }, "/other", paths)
	if err != nil || got.Name != "default" {
		t.Fatalf("default precedence: %#v %v", got, err)
	}
	if err := os.Remove(paths.Profiles); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths.Bindings); err != nil {
		t.Fatal(err)
	}
	got, err = SelectProfile(nil, func(string) string { return "" }, "/other", paths)
	if err != nil || got.Profile != nil || got.Source != "local" {
		t.Fatalf("local fallback: %#v %v", got, err)
	}
}

func TestLocalSelectionIsReservedAndStopsEveryPrecedenceLayer(t *testing.T) {
	dir := t.TempDir()
	paths := ProfilePaths{Profiles: filepath.Join(dir, "profiles.yaml"), Bindings: filepath.Join(dir, "bindings.yaml")}
	remote := func(name string) Profile {
		return Profile{Name: name, Endpoint: "https://" + name + ".example", AuthorityID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Credential: CredentialDescriptor{Path: filepath.Join(dir, name)}}
	}
	if err := SaveProfiles(paths, []Profile{remote("lower"), remote("default")}, "local"); err != nil {
		t.Fatal(err)
	}
	if err := SaveBindings(paths, map[string]string{"/checkout": "local"}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, source string
		flags        map[string]string
		env          string
		root         string
	}{
		{"flag", "flag", map[string]string{"profile": "local"}, "lower", "/checkout"},
		{"env", "env", nil, "local", "/checkout"},
		{"binding", "binding", nil, "", "/checkout"},
		{"default", "default", nil, "", "/other"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := SelectProfile(test.flags, func(key string) string {
				if key == "WORKLEASE_PROFILE" {
					return test.env
				}
				return ""
			}, test.root, paths)
			if err != nil || got.Profile != nil || got.Name != LocalProfileName || got.Source != test.source {
				t.Fatalf("selection=%#v err=%v", got, err)
			}
		})
	}
	got, err := SelectProfile(nil, func(string) string { return "" }, "/missing", ProfilePaths{Profiles: filepath.Join(dir, "missing"), Bindings: filepath.Join(dir, "missing-bindings")})
	if err != nil || got.Profile != nil || got.Name != LocalProfileName || got.Source != "local" {
		t.Fatalf("implicit selection=%#v err=%v", got, err)
	}
	if err := SaveProfiles(paths, []Profile{remote("lower")}, LocalProfileName); err != nil {
		t.Fatal(err)
	}
	_, defaultName, err := LoadProfiles(paths)
	if err != nil || defaultName != LocalProfileName {
		t.Fatalf("explicit default=%q err=%v", defaultName, err)
	}
}

func TestPersistedLocalProfileFailsClosedWithMigrationGuidance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles.yaml")
	data := []byte("profiles:\n  - name: local\n    endpoint: https://old.example\n    credential:\n      path: " + filepath.Join(dir, "credential") + "\ndefault: local\n")
	if err := writePrivate(path, data); err != nil {
		t.Fatal(err)
	}
	_, _, err := LoadProfiles(ProfilePaths{Profiles: path})
	if err == nil || !strings.Contains(err.Error(), "rename") || !strings.Contains(err.Error(), "credential path") {
		t.Fatalf("collision error=%v", err)
	}
}

func TestProfileAllowsExplicitInsecureHTTPEndpointAndRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	p := Profile{Name: "lan", Endpoint: "http://192.168.1.20:8080", AllowInsecureHTTP: true, Credential: CredentialDescriptor{Path: filepath.Join(dir, "c")}}
	if err := SaveProfiles(ProfilePaths{Profiles: filepath.Join(dir, "p")}, []Profile{p}, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad"), []byte("profiles:\n - name: x\n   endpoint: https://x\n   credential:\n     path: /tmp/c\n     secret: nope\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProfiles(ProfilePaths{Profiles: filepath.Join(dir, "bad")}); err == nil {
		t.Fatal("unknown nested profile field accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "legacy"), []byte("profiles:\n - name: x\n   endpoint: http://192.168.1.20:8080\n   devHttp: true\n   credential:\n     path: /tmp/c\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadProfiles(ProfilePaths{Profiles: filepath.Join(dir, "legacy")}); err == nil {
		t.Fatal("removed devHttp profile field accepted")
	}
	p.AllowInsecureHTTP = false
	if err := validateProfile(p); err == nil {
		t.Fatal("insecure endpoint accepted")
	}
}
