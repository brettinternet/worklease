package config

import (
	"os"
	"path/filepath"
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
