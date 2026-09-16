package config

// Trusted profiles and project bindings are deliberately separate from the
// repository configuration file. A repository can contain neither a profile
// selection nor an endpoint override.

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/brettinternet/worklease/internal/handle"
	"gopkg.in/yaml.v3"
)

type CredentialDescriptor struct {
	Path string `yaml:"path" json:"path"`
}

type Profile struct {
	Name              string               `yaml:"name" json:"name"`
	Endpoint          string               `yaml:"endpoint" json:"endpoint"`
	AuthorityID       string               `yaml:"authorityId" json:"authorityId"`
	RestoreID         string               `yaml:"restoreId" json:"restoreId"`
	CertificateSHA256 string               `yaml:"certificateSha256,omitempty" json:"certificateSha256,omitempty"`
	AllowInsecureHTTP bool                 `yaml:"allowInsecureHTTP,omitempty" json:"allowInsecureHTTP,omitempty"`
	Credential        CredentialDescriptor `yaml:"credential" json:"credential"`
}

type ProfileSelection struct {
	Profile *Profile
	Source  string // flag, env, binding, default, or local
	Name    string
}

type ProfilePaths struct{ Profiles, Bindings string }

func UserProfilePaths(env func(string) string) ProfilePaths {
	if env == nil {
		env = os.Getenv
	}
	root := strings.TrimSpace(env("XDG_CONFIG_HOME"))
	if root == "" {
		if h, err := os.UserHomeDir(); err == nil {
			root = filepath.Join(h, ".config")
		}
	}
	return ProfilePaths{Profiles: filepath.Join(root, "worklease", "profiles.yaml"), Bindings: filepath.Join(root, "worklease", "bindings.yaml")}
}

// LoadProfiles reads only owner-private user data. Missing files are treated as
// empty stores so an unbound invocation remains local and network-free.
func LoadProfiles(paths ProfilePaths) (map[string]Profile, string, error) {
	profiles := map[string]Profile{}
	data, err := handle.ReadOwnerPrivate(paths.Profiles, 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return profiles, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("profiles file cannot be read safely")
	}
	var doc struct {
		Profiles []Profile `yaml:"profiles"`
		Default  string    `yaml:"default"`
	}
	if err := strictYAML(data, &doc, map[string]bool{"profiles": true, "default": true}); err != nil {
		return nil, "", err
	}
	for _, p := range doc.Profiles {
		if err := validateProfile(p); err != nil {
			return nil, "", err
		}
		if _, exists := profiles[p.Name]; exists {
			return nil, "", fmt.Errorf("duplicate profile %q", p.Name)
		}
		profiles[p.Name] = p
	}
	return profiles, doc.Default, nil
}

func LoadBinding(paths ProfilePaths, checkoutRoot string) (string, error) {
	data, err := handle.ReadOwnerPrivate(paths.Bindings, 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("bindings file cannot be read safely")
	}
	var doc struct {
		Bindings map[string]string `yaml:"bindings"`
	}
	if err := strictYAML(data, &doc, map[string]bool{"bindings": true}); err != nil {
		return "", err
	}
	return strings.TrimSpace(doc.Bindings[checkoutRoot]), nil
}

// SelectProfile implements the fixed precedence. The caller supplies the
// already-resolved checkout root; no repository file is read here.
func SelectProfile(flags map[string]string, env func(string) string, checkoutRoot string, paths ProfilePaths) (ProfileSelection, error) {
	if env == nil {
		env = os.Getenv
	}
	profiles, defaultName, err := LoadProfiles(paths)
	if err != nil {
		return ProfileSelection{}, err
	}
	binding, err := LoadBinding(paths, checkoutRoot)
	if err != nil {
		return ProfileSelection{}, err
	}
	name, source := strings.TrimSpace(flags["profile"]), "flag"
	if name == "" {
		name, source = strings.TrimSpace(env("WORKLEASE_PROFILE")), "env"
	}
	if name == "" {
		name, source = binding, "binding"
	}
	if name == "" {
		name, source = strings.TrimSpace(defaultName), "default"
	}
	if name == "" {
		return ProfileSelection{Source: "local"}, nil
	}
	p, ok := profiles[name]
	if !ok {
		return ProfileSelection{}, fmt.Errorf("profile %q is not trusted", name)
	}
	return ProfileSelection{Profile: &p, Source: source, Name: name}, nil
}

func SaveProfiles(paths ProfilePaths, profiles []Profile, defaultName string) error {
	for _, p := range profiles {
		if err := validateProfile(p); err != nil {
			return err
		}
	}
	if defaultName != "" {
		found := false
		for _, p := range profiles {
			found = found || p.Name == defaultName
		}
		if !found {
			return fmt.Errorf("default profile is not defined")
		}
	}
	data, err := yaml.Marshal(struct {
		Profiles []Profile `yaml:"profiles"`
		Default  string    `yaml:"default,omitempty"`
	}{profiles, defaultName})
	if err != nil {
		return err
	}
	return writePrivate(paths.Profiles, data)
}
func SaveBindings(paths ProfilePaths, bindings map[string]string) error {
	for root, name := range bindings {
		if !filepath.IsAbs(root) || strings.TrimSpace(name) == "" {
			return fmt.Errorf("invalid project binding")
		}
	}
	data, err := yaml.Marshal(struct {
		Bindings map[string]string `yaml:"bindings"`
	}{bindings})
	if err != nil {
		return err
	}
	return writePrivate(paths.Bindings, data)
}

func ValidateProfileName(name string) error {
	if name == "" || name == "." || name == ".." || strings.TrimSpace(name) != name || len(name) > 128 || strings.ContainsAny(name, "/\\\x00\r\n") {
		return fmt.Errorf("profile name is invalid")
	}
	return nil
}

func validateProfile(p Profile) error {
	if err := ValidateProfileName(p.Name); err != nil {
		return err
	}
	u, err := url.Parse(p.Endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return fmt.Errorf("profile endpoint is invalid")
	}
	if u.Scheme != "https" {
		if !p.AllowInsecureHTTP || u.Scheme != "http" {
			return fmt.Errorf("HTTPS is required for profile endpoint unless insecure HTTP is explicitly allowed")
		}
	}
	if p.AuthorityID != "" && !hexID(p.AuthorityID) {
		return fmt.Errorf("profile authority ID is invalid")
	}
	if p.RestoreID != "" && !hexID(p.RestoreID) {
		return fmt.Errorf("profile restore ID is invalid")
	}
	if p.CertificateSHA256 != "" && (!hex64(p.CertificateSHA256) || u.Scheme != "https") {
		return fmt.Errorf("profile certificate pin requires HTTPS and 64 lowercase hexadecimal characters")
	}
	if p.Credential.Path == "" || !filepath.IsAbs(p.Credential.Path) {
		return fmt.Errorf("credential path must be absolute")
	}
	return nil
}
func hex64(s string) bool {
	if len(s) != 64 || s != strings.ToLower(s) {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func hexID(s string) bool {
	if len(s) != 32 || s != strings.ToLower(s) {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func writePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := handle.EnsureOwnerPrivateDir(dir); err != nil {
		return err
	}
	return handle.WriteOwnerPrivate(path, data, 1<<20)
}
func strictYAML(data []byte, out any, known map[string]bool) error {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("YAML is invalid")
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("YAML must be an object")
	}
	n := root.Content[0]
	seen := map[string]bool{}
	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i].Value
		if !known[k] || seen[k] {
			return fmt.Errorf("unknown or duplicate YAML field %q", k)
		}
		seen[k] = true
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("YAML structure is invalid")
	}
	return nil
}
