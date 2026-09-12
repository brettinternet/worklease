// Package resource derives deterministic opaque resource identities.
package resource

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/reason"
)

const (
	ContractVersion  = 1
	KeyPolicyVersion = 1
	maxIdentityBytes = 1024
)

// Input is the normalized input to a resource policy. Path is used only by
// the path policy; WorkingDir resolves relative paths.
type Input struct {
	Provider         string
	Source           string
	Item             string
	Path             string
	CoordinationOnly bool
	WorkingDir       string
}

// Key is the complete public description of a derived resource.
type Key struct {
	Provider            string `json:"provider"`
	Source              string `json:"source,omitempty"`
	Item                string `json:"item,omitempty"`
	Resource            string `json:"resource"`
	Capability          string `json:"capability"`
	Scope               string `json:"scope"`
	IdentityScope       string `json:"identityScope"`
	LocalReplaceAllowed bool   `json:"localReplaceAllowed"`
	ProviderFencing     bool   `json:"providerFencing"`
}

// Descriptor is static, inspectable policy metadata.
type Descriptor struct {
	Name                string `json:"name"`
	Resource            string `json:"resource"`
	Capability          string `json:"capability"`
	Scope               string `json:"scope"`
	IdentityScope       string `json:"identityScope"`
	LocalReplaceAllowed bool   `json:"localReplaceAllowed"`
	ProviderFencing     bool   `json:"providerFencing"`
	ContractVersion     int    `json:"contractVersion"`
	KeyPolicyVersion    int    `json:"keyPolicyVersion"`
}

type Policy interface {
	Name() string
	Describe() Descriptor
	Key(Input) (Key, error)
}

func invalid(message string) error     { return reason.New(reason.ReasonInvalidResource, message) }
func invalidPath(message string) error { return reason.New(reason.ReasonInvalidPath, message) }
func unknown(name string) error {
	return reason.New(reason.ReasonUnknownPolicy, fmt.Sprintf("unknown policy %q (available: %s)", name, strings.Join(Names(), ", "))).With("available", Names())
}

// ValidateIdentity rejects unusable identities while preserving every byte.
// Policies apply only the normalization explicitly required by their contract.
func ValidateIdentity(field, value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", invalid(field + " must be valid UTF-8")
	}
	if strings.TrimSpace(value) == "" {
		return "", invalid(field + " must not be blank")
	}
	if len([]byte(value)) > maxIdentityBytes {
		return "", invalid(field + " exceeds 1024 bytes")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return "", invalid(field + " must not contain control characters")
		}
	}
	return value, nil
}

func validateResource(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", invalid("resource must be valid UTF-8")
	}
	if len([]byte(value)) == 0 || len([]byte(value)) > maxIdentityBytes {
		return "", invalid("resource must be 1 to 1024 bytes")
	}
	if strings.TrimSpace(value) != value {
		return "", invalid("resource must not start or end with whitespace")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return "", invalid("resource must not contain control characters")
		}
	}
	return value, nil
}

// RFC3986Encode encodes UTF-8 bytes, leaving only RFC3986 unreserved bytes.
func RFC3986Encode(value string) string {
	const hexchars = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-._~", rune(c)) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hexchars[c>>4])
			b.WriteByte(hexchars[c&15])
		}
	}
	return b.String()
}

func localDescriptor(name, capability, scope string) Descriptor {
	return Descriptor{Name: name, Resource: name + ":<common>:<locator>:<item>", Capability: capability, Scope: scope, IdentityScope: "host-local", LocalReplaceAllowed: true, ContractVersion: ContractVersion, KeyPolicyVersion: KeyPolicyVersion}
}
func coordinationDescriptor(name string) Descriptor {
	return Descriptor{Name: name, Resource: "coordination:" + name + ":<sha256>", Capability: "local-coordination", Scope: "item", IdentityScope: "portable", LocalReplaceAllowed: false, ContractVersion: ContractVersion, KeyPolicyVersion: KeyPolicyVersion}
}

type staticPolicy struct {
	descriptor Descriptor
	derive     func(Input) (Key, error)
}

func (p staticPolicy) Name() string              { return p.descriptor.Name }
func (p staticPolicy) Describe() Descriptor      { return p.descriptor }
func (p staticPolicy) Key(in Input) (Key, error) { return p.derive(in) }

func localKey(provider, source, item, capability, scope string, coordination bool, cwd string) (Key, error) {
	source, err := canonicalSource(source, cwd)
	if err != nil {
		return Key{}, err
	}
	item, err = ValidateIdentity("item", item)
	if err != nil {
		return Key{}, err
	}
	common, locator, err := gitIdentity(source)
	if err != nil {
		return Key{}, err
	}
	resource := provider + ":" + RFC3986Encode(common) + ":" + RFC3986Encode(locator) + ":" + RFC3986Encode(func() string {
		if provider == "markdown" {
			return "__source__"
		}
		return item
	}())
	if _, err := validateResource(resource); err != nil {
		return Key{}, invalid("derived resource exceeds 1024 bytes")
	}
	return Key{Provider: provider, Source: source, Item: item, Resource: resource, Capability: chooseCapability(capability, coordination), Scope: scope, IdentityScope: "host-local", LocalReplaceAllowed: !coordination && capability != "local-coordination", ProviderFencing: false}, nil
}
func chooseCapability(capability string, coordination bool) string {
	if coordination {
		return "local-coordination"
	}
	return capability
}

func coordinationKey(provider, source, item string) (Key, error) {
	source, err := ValidateIdentity("source", source)
	if err != nil {
		return Key{}, err
	}
	item, err = ValidateIdentity("item", item)
	if err != nil {
		return Key{}, err
	}
	if source == "" {
		return Key{}, invalid("source must not be blank")
	}
	// Keep the byte representation compatible with the reference adapters:
	// sorted keys, no spaces, and ASCII JSON string escaping.
	data := []byte(`{"item":` + quoteJSONASCII(item) + `,"provider":` + quoteJSONASCII(provider) + `,"source":` + quoteJSONASCII(source) + `}`)
	sum := sha256.Sum256(data)
	resource := "coordination:" + provider + ":" + hex.EncodeToString(sum[:])
	if _, err := validateResource(resource); err != nil {
		return Key{}, invalid("derived resource exceeds 1024 bytes")
	}
	return Key{Provider: provider, Source: source, Item: item, Resource: resource, Capability: "local-coordination", Scope: "item", IdentityScope: "portable", LocalReplaceAllowed: false, ProviderFencing: false}, nil
}

func quoteJSONASCII(value string) string {
	const hexchars = "0123456789abcdef"
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r > 0x7e {
				if r <= 0xffff {
					b.WriteString(`\u`)
					b.WriteByte(hexchars[r>>12])
					b.WriteByte(hexchars[(r>>8)&15])
					b.WriteByte(hexchars[(r>>4)&15])
					b.WriteByte(hexchars[r&15])
				} else {
					r -= 0x10000
					hi, lo := 0xd800+(r>>10), 0xdc00+(r&1023)
					b.WriteString(`\u`)
					for _, v := range []rune{hi, lo} {
						b.WriteByte(hexchars[v>>12])
						b.WriteByte(hexchars[(v>>8)&15])
						b.WriteByte(hexchars[(v>>4)&15])
						b.WriteByte(hexchars[v&15])
					}
				}
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func canonicalSource(value, cwd string) (string, error) {
	value, err := ValidateIdentity("source", value)
	if err != nil {
		return "", err
	}
	return canonicalPath(value, cwd), nil
}

func canonicalPath(value, cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(cwd, value)
	}
	value = filepath.Clean(value)
	// EvalSymlinks requires the leaf to exist. Resolve the nearest existing
	// ancestor and append the missing suffix unchanged.
	var suffix []string
	probe := value
	for {
		if _, err := os.Lstat(probe); err == nil {
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		suffix = append(suffix, filepath.Base(probe))
		probe = parent
	}
	resolved, err := filepath.EvalSymlinks(probe)
	if err != nil {
		resolved = probe
	}
	resolved, _ = filepath.Abs(resolved)
	for i := len(suffix) - 1; i >= 0; i-- {
		resolved = filepath.Join(resolved, suffix[i])
	}
	return filepath.Clean(resolved)
}

func gitOutput(cwd string, args ...string) (string, bool) {
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	env := make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "GIT_") {
			env = append(env, item)
		}
	}
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	value := strings.Trim(string(out), "\r\n")
	return value, value != ""
}
func existingAncestor(path string) string {
	probe := path
	for {
		if _, err := os.Lstat(probe); err == nil {
			return probe
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return probe
		}
		probe = parent
	}
}

func gitRepoInfo(path string) (root, common string, ok bool) {
	probe := existingAncestor(path)
	if st, err := os.Lstat(probe); err == nil {
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			probe = filepath.Dir(probe)
		}
	}
	root, rootOK := gitOutput(probe, "rev-parse", "--show-toplevel")
	commonValue, commonOK := gitOutput(probe, "rev-parse", "--git-common-dir")
	if !rootOK || !commonOK {
		return "", "", false
	}
	return canonicalPath(root, ""), canonicalPath(commonValue, probe), true
}

func gitIdentity(source string) (common, locator string, err error) {
	probe := existingAncestor(source)
	if st, e := os.Stat(probe); e == nil && !st.IsDir() {
		probe = filepath.Dir(probe)
	}
	top, ok := gitOutput(probe, "rev-parse", "--show-toplevel")
	commonValue, commonOK := gitOutput(probe, "rev-parse", "--git-common-dir")
	if !ok || !commonOK {
		return source, source, nil
	}
	top = canonicalPath(top, "")
	commonValue = canonicalPath(commonValue, probe)
	rel, e := filepath.Rel(top, source)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		locator = source
	} else {
		locator = filepath.ToSlash(rel)
	}
	return commonValue, locator, nil
}

func pathKey(in Input) (Key, error) {
	if !utf8.ValidString(in.Path) || len([]byte(in.Path)) == 0 || len([]byte(in.Path)) > maxIdentityBytes {
		return Key{}, invalidPath("path must be 1 to 1024 UTF-8 bytes")
	}
	if strings.TrimSpace(in.Path) != in.Path {
		return Key{}, invalidPath("path must not start or end with whitespace")
	}
	for _, r := range in.Path {
		if r < 0x20 || r == 0x7f {
			return Key{}, invalidPath("path must not contain control characters")
		}
	}
	for _, part := range strings.FieldsFunc(in.Path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return Key{}, invalidPath("path traversal is not allowed")
		}
	}
	cwd := in.WorkingDir
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	lexical := in.Path
	if !filepath.IsAbs(lexical) {
		lexical = filepath.Join(cwd, lexical)
	}
	lexical = filepath.Clean(lexical)
	lexicalProbe := existingAncestor(lexical)
	if lexicalProbe != lexical {
		if st, e := os.Stat(lexicalProbe); e == nil && !st.IsDir() {
			return Key{}, invalidPath("path has a non-directory ancestor")
		}
	}
	lexicalRoot, lexicalCommon, ok := gitRepoInfo(lexicalProbe)
	if !ok {
		return Key{}, invalidPath("path is not inside a Git repository")
	}
	target := canonicalPath(lexical, cwd)
	if st, e := os.Stat(target); e == nil && st.IsDir() {
		return Key{}, invalidPath("path must name a file")
	}
	targetProbe := existingAncestor(target)
	if targetProbe != target {
		if st, e := os.Stat(targetProbe); e == nil && !st.IsDir() {
			return Key{}, invalidPath("path has a non-directory ancestor")
		}
	}
	_, targetCommon, targetOK := gitRepoInfo(targetProbe)
	if !targetOK || targetCommon != lexicalCommon {
		return Key{}, invalidPath("resolved path belongs to a different Git repository")
	}
	top, common := lexicalRoot, lexicalCommon
	rel, e := filepath.Rel(top, target)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return Key{}, invalidPath("path escapes repository")
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return Key{}, invalidPath("repository root is not a file path")
	}
	resource := "path:" + RFC3986Encode(common) + ":" + RFC3986Encode(rel)
	if _, err := validateResource(resource); err != nil {
		return Key{}, invalidPath("derived resource exceeds 1024 bytes")
	}
	return Key{Provider: "path", Source: target, Item: rel, Resource: resource, Capability: chooseCapability("item-claim", in.CoordinationOnly), Scope: "path", IdentityScope: "host-local", LocalReplaceAllowed: !in.CoordinationOnly, ProviderFencing: false}, nil
}

var policies = map[string]Policy{
	"backlog-md": staticPolicy{localDescriptor("backlog-md", "item-claim", "item"), func(in Input) (Key, error) {
		return localKey("backlog-md", in.Source, in.Item, "item-claim", "item", in.CoordinationOnly, in.WorkingDir)
	}},
	"markdown": staticPolicy{Descriptor{Name: "markdown", Resource: "markdown:<common>:<locator>:__source__", Capability: "source-claim", Scope: "source", IdentityScope: "host-local", LocalReplaceAllowed: true, ContractVersion: ContractVersion, KeyPolicyVersion: KeyPolicyVersion}, func(in Input) (Key, error) {
		return localKey("markdown", in.Source, in.Item, "source-claim", "source", in.CoordinationOnly, in.WorkingDir)
	}},
	"github": staticPolicy{Descriptor{Name: "github", Resource: "github:<source>#<item>", Capability: "item-claim", Scope: "item", IdentityScope: "portable", LocalReplaceAllowed: true, ContractVersion: ContractVersion, KeyPolicyVersion: KeyPolicyVersion}, func(in Input) (Key, error) {
		source, e := ValidateIdentity("source", in.Source)
		if e != nil {
			return Key{}, e
		}
		item, e := ValidateIdentity("item", in.Item)
		if e != nil {
			return Key{}, e
		}
		source = strings.TrimSuffix(strings.ToLower(source), ".git")
		if source == "" {
			return Key{}, invalid("source must not be blank")
		}
		resource := "github:" + RFC3986Encode(source) + "#" + RFC3986Encode(item)
		if _, err := validateResource(resource); err != nil {
			return Key{}, invalid("derived resource exceeds 1024 bytes")
		}
		return Key{Provider: "github", Source: source, Item: item, Resource: resource, Capability: chooseCapability("item-claim", in.CoordinationOnly), Scope: "item", IdentityScope: "portable", LocalReplaceAllowed: !in.CoordinationOnly, ProviderFencing: false}, nil
	}},
	"linear": staticPolicy{coordinationDescriptor("linear"), func(in Input) (Key, error) { return coordinationKey("linear", in.Source, in.Item) }},
	"generic": staticPolicy{coordinationDescriptor("generic"), func(in Input) (Key, error) {
		return coordinationKey("generic", in.Source, in.Item)
	}},
	"path": staticPolicy{Descriptor{Name: "path", Resource: "path:<common>:<repo-relative>", Capability: "item-claim", Scope: "path", IdentityScope: "host-local", LocalReplaceAllowed: true, ContractVersion: ContractVersion, KeyPolicyVersion: KeyPolicyVersion}, pathKey},
}

func Lookup(name string) (Policy, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if p, ok := policies[name]; ok {
		return p, nil
	}
	return nil, unknown(name)
}
func Names() []string {
	names := make([]string, 0, len(policies))
	for name := range policies {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
func Descriptors() []Descriptor {
	names := Names()
	out := make([]Descriptor, 0, len(names))
	for _, name := range names {
		out = append(out, policies[name].Describe())
	}
	return out
}
func (d Descriptor) Map() map[string]any {
	b, _ := json.Marshal(d)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// Resolve derives a key from one provider policy input.
func Resolve(in Input) (Key, error) {
	if in.Path != "" {
		return pathKey(in)
	}
	provider := strings.ToLower(strings.TrimSpace(in.Provider))
	if provider == "" {
		return Key{}, invalid("provider must not be blank")
	}
	p, e := Lookup(provider)
	if e != nil {
		return Key{}, e
	}
	return p.Key(in)
}

// Direct creates a key for an already-derived opaque resource.
func Direct(value string, coordinationOnly bool) (Key, error) {
	value, e := validateResource(value)
	if e != nil {
		return Key{}, e
	}
	return Key{Provider: "resource", Resource: value, Capability: chooseCapability("item-claim", coordinationOnly), Scope: "item", IdentityScope: "portable", LocalReplaceAllowed: !coordinationOnly, ProviderFencing: false}, nil
}
