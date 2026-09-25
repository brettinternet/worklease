package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/reason"
	"gopkg.in/yaml.v3"
)

// QueueConfig is loaded exclusively from the owner's private configuration directory.
type QueueConfig struct {
	Version int                  `yaml:"version"`
	Me      map[string]yaml.Node `yaml:"me"`
	Sources []QueueSource        `yaml:"sources"`
	Views   []QueueView          `yaml:"views"`
	Launch  []QueueLaunch        `yaml:"launch"`
}

// QueueLaunch is a trusted, owner-configured process handoff, not a shell command.
type QueueLaunch struct {
	Name    string   `yaml:"name"`
	Argv    []string `yaml:"argv"`
	Cwd     string   `yaml:"cwd"`
	PassEnv []string `yaml:"passEnv"`
}

var launchPlaceholders = map[string]bool{"ref": true, "sourceId": true, "itemId": true, "checkout": true}

// ValidateLaunchTemplate rejects unrecognized or unbalanced placeholders.
func ValidateLaunchTemplate(value string) error {
	for i := 0; i < len(value); {
		switch value[i] {
		case '{':
			end := strings.IndexByte(value[i+1:], '}')
			if end < 0 || !launchPlaceholders[value[i+1:i+1+end]] {
				return fmt.Errorf("unknown or unclosed placeholder")
			}
			i += end + 2
		case '}':
			return fmt.Errorf("unmatched closing brace")
		default:
			i++
		}
	}
	return nil
}

type QueueSource struct {
	ID                string            `yaml:"id"`
	Adapter           string            `yaml:"adapter"`
	Checkout          string            `yaml:"checkout"`
	Claims            *QueueClaims      `yaml:"claims"`
	Workflow          map[string]string `yaml:"workflow"`
	AllowGitNetwork   bool              `yaml:"allowGitNetwork"`
	Host              string            `yaml:"host"`
	Repository        string            `yaml:"repository"`
	Account           string            `yaml:"account"`
	Executable        string            `yaml:"executable"`
	ExpectedAdapterID string            `yaml:"expectedAdapterId"`
	ExpectedVersion   string            `yaml:"expectedVersion"`
	Config            map[string]any    `yaml:"config"`
	CredentialRef     string            `yaml:"credentialRef"`
}

type QueueClaims struct {
	Policy string `yaml:"policy" json:"policy"`
	Source string `yaml:"source" json:"source"`
}

type QueueView struct {
	Name      string      `yaml:"name"`
	Authority string      `yaml:"authority"`
	Sources   []string    `yaml:"sources"`
	Filter    QueueFilter `yaml:"filter"`
}

type QueueFilter struct {
	Readiness string   `yaml:"readiness"`
	Claim     string   `yaml:"claim"`
	Assigned  []string `yaml:"assigned"`
}

// QueuePath does not consult checkout, working directory, or Worklease-specific environment variables.
func QueuePath(env func(string) string) string {
	if env == nil {
		env = os.Getenv
	}
	root := strings.TrimSpace(env("XDG_CONFIG_HOME"))
	if root == "" {
		root = filepath.Join(env("HOME"), ".config")
	}
	return filepath.Join(root, "worklease", "queue.yaml")
}

// QueueRecoveryDir is durable owner-private state, independent of the disposable index.
// Every queue writer and cancellation path must use this same directory.
func QueueRecoveryDir(env func(string) string) string {
	if env == nil {
		env = os.Getenv
	}
	root := strings.TrimSpace(env("XDG_STATE_HOME"))
	if root == "" {
		root = filepath.Join(env("HOME"), ".local", "state")
	}
	return filepath.Join(root, "worklease", "queue-recovery")
}

// LoadQueue reads queue.yaml with the same owner/private and no-symlink rules as profiles.yaml.
// profiles are trusted names returned by LoadProfiles, not selected via project bindings.
func LoadQueue(env func(string) string) (QueueConfig, error) {
	path := QueuePath(env)
	if !filepath.IsAbs(path) {
		return QueueConfig{}, fmt.Errorf("queue.yaml: HOME or XDG_CONFIG_HOME must resolve to an absolute directory")
	}
	data, err := handle.ReadOwnerPrivate(path, 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return QueueConfig{}, reason.New(reason.ReasonNoSourcesConfigured, "no queue sources configured; create owner-private "+path+" (see docs/queue.md)")
	}
	if err != nil {
		return QueueConfig{}, fmt.Errorf("queue.yaml cannot be read safely: %w", err)
	}
	profiles, _, err := LoadProfiles(ProfilePaths{Profiles: filepath.Join(filepath.Dir(path), "profiles.yaml")})
	if err != nil {
		return QueueConfig{}, fmt.Errorf("queue authority profiles: %w", err)
	}
	cfg, err := parseQueue(data, env, profiles)
	if err != nil && reason.As(err) == nil {
		return QueueConfig{}, reason.New(reason.ReasonConfigInvalid, err.Error())
	}
	return cfg, err
}

func parseQueue(data []byte, env func(string) string, profiles map[string]Profile) (QueueConfig, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := decoder.Decode(&root); err != nil || len(root.Content) != 1 {
		return QueueConfig{}, fmt.Errorf("queue.yaml: invalid YAML")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return QueueConfig{}, fmt.Errorf("queue.yaml: multiple YAML documents")
		}
		return QueueConfig{}, fmt.Errorf("queue.yaml: invalid YAML")
	}
	schemas := map[string]map[string]bool{
		"queue":  {"version": true, "me": true, "sources": true, "views": true, "launch": true},
		"launch": {"name": true, "argv": true, "cwd": true, "passEnv": true},
		"source": {"id": true, "adapter": true, "checkout": true, "claims": true, "workflow": true, "allowGitNetwork": true, "host": true, "repository": true, "account": true, "executable": true, "expectedAdapterId": true, "expectedVersion": true, "config": true, "credentialRef": true},
		"claims": {"policy": true, "source": true},
		"view":   {"name": true, "authority": true, "sources": true, "filter": true},
		"filter": {"readiness": true, "claim": true, "assigned": true},
	}
	if err := checkQueueKeys(root.Content[0], "queue", schemas["queue"]); err != nil {
		return QueueConfig{}, err
	}
	fields := nodeFields(root.Content[0])
	if m, ok := fields["me"]; ok {
		if err := checkQueueKeys(m, "me", nil); err != nil {
			return QueueConfig{}, err
		}
		if value := nodeFields(m)["backlog-md"]; value != nil && value.Kind == yaml.SequenceNode {
			for _, item := range value.Content {
				if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
					return QueueConfig{}, fmt.Errorf("me.backlog-md: expected assignee strings")
				}
			}
		}
	} else {
		return QueueConfig{}, fmt.Errorf("me: required")
	}
	for _, section := range []string{"sources", "views"} {
		n, ok := fields[section]
		if !ok || n.Kind != yaml.SequenceNode {
			return QueueConfig{}, fmt.Errorf("%s: expected a list", section)
		}
		for i, item := range n.Content {
			label := fmt.Sprintf("%s[%d]", section, i)
			schema := schemas["source"]
			if section == "views" {
				schema = schemas["view"]
			}
			if err := checkQueueKeys(item, label, schema); err != nil {
				return QueueConfig{}, err
			}
			nested := nodeFields(item)
			child, name := "claims", "claims"
			if section == "views" {
				child, name = "filter", "filter"
			}
			if n, ok := nested[child]; ok {
				if err := checkQueueKeys(n, label+"."+child, schemas[name]); err != nil {
					return QueueConfig{}, err
				}
			}
			if section == "sources" {
				if workflow := nested["workflow"]; workflow != nil {
					if err := checkQueueKeys(workflow, label+".workflow", map[string]bool{"start": true, "blocked": true, "review": true, "complete": true, "reopen": true}); err != nil {
						return QueueConfig{}, err
					}
					for intent, transition := range nodeFields(workflow) {
						if transition.Kind != yaml.ScalarNode || transition.Tag != "!!str" || strings.TrimSpace(transition.Value) == "" {
							return QueueConfig{}, fmt.Errorf("%s.workflow.%s: expected non-empty provider transition", label, intent)
						}
					}
				}
				if allow, ok := nested["allowGitNetwork"]; ok && (allow.Kind != yaml.ScalarNode || allow.Tag != "!!bool") {
					return QueueConfig{}, fmt.Errorf("%s.allowGitNetwork: expected boolean", label)
				}
				adapter := nested["adapter"]
				if adapter == nil {
					return QueueConfig{}, fmt.Errorf("%s.adapter: required", label)
				}
				if adapter.Kind != yaml.ScalarNode || adapter.Tag != "!!str" || strings.TrimSpace(adapter.Value) == "" {
					return QueueConfig{}, fmt.Errorf("%s.adapter: expected adapter name", label)
				}
				switch adapter.Value {
				case "backlog-md":
					if nested["checkout"] == nil {
						return QueueConfig{}, fmt.Errorf("%s.checkout: required", label)
					}
					if err := rejectQueueFields(nested, label, "executable", "expectedAdapterId", "expectedVersion", "config", "credentialRef"); err != nil {
						return QueueConfig{}, err
					}
				case "github":
					for _, key := range []string{"host", "repository", "account"} {
						if nested[key] == nil {
							return QueueConfig{}, fmt.Errorf("%s.%s: required", label, key)
						}
					}
					if err := rejectQueueFields(nested, label, "executable", "expectedAdapterId", "expectedVersion", "config", "credentialRef"); err != nil {
						return QueueConfig{}, err
					}
				case "external":
					if err := rejectQueueFields(nested, label, "checkout", "allowGitNetwork", "host", "repository"); err != nil {
						return QueueConfig{}, err
					}
					if err := validateExternalQueueSource(label, nested); err != nil {
						return QueueConfig{}, err
					}
				default:
					return QueueConfig{}, fmt.Errorf("%s.adapter: unknown adapter %q", label, adapter.Value)
				}
			} else if nested["filter"] == nil {
				return QueueConfig{}, fmt.Errorf("%s.filter: required", label)
			}
		}
	}
	if launch := fields["launch"]; launch != nil {
		if launch.Kind != yaml.SequenceNode {
			return QueueConfig{}, fmt.Errorf("launch: expected a list")
		}
		for i, item := range launch.Content {
			if err := checkQueueKeys(item, fmt.Sprintf("launch[%d]", i), schemas["launch"]); err != nil {
				return QueueConfig{}, err
			}
		}
	}
	var cfg QueueConfig
	if err := root.Decode(&cfg); err != nil {
		return QueueConfig{}, fmt.Errorf("queue.yaml: %w", err)
	}
	if fields["version"] == nil {
		return QueueConfig{}, fmt.Errorf("version: required")
	}
	if cfg.Version != 1 {
		return QueueConfig{}, fmt.Errorf("version: expected 1")
	}
	if len(cfg.Sources) == 0 {
		return QueueConfig{}, fmt.Errorf("sources: at least one source required")
	}
	for host, node := range cfg.Me {
		if host == "backlog-md" {
			var names []string
			if err := node.Decode(&names); err != nil || len(names) == 0 {
				return QueueConfig{}, fmt.Errorf("me.backlog-md: expected assignee strings")
			}
			for _, name := range names {
				if strings.TrimSpace(name) == "" {
					return QueueConfig{}, fmt.Errorf("me.backlog-md: empty assignee")
				}
			}
		} else if !validHost(host) || node.Kind != yaml.ScalarNode || node.Tag != "!!str" || strings.TrimSpace(node.Value) == "" {
			return QueueConfig{}, fmt.Errorf("me.%s: expected provider account string", host)
		}
	}
	if env == nil {
		env = os.Getenv
	}
	seen := map[string]bool{}
	for i := range cfg.Sources {
		s := &cfg.Sources[i]
		label := fmt.Sprintf("sources[%d]", i)
		if s.ID == "" || seen[s.ID] {
			return QueueConfig{}, fmt.Errorf("%s.id: empty or duplicate id %q", label, s.ID)
		}
		// Refs are written SOURCE:ITEM, so a colon would make selectors ambiguous.
		if strings.Contains(s.ID, ":") {
			return QueueConfig{}, fmt.Errorf("%s.id: must not contain ':'", label)
		}
		seen[s.ID] = true
		switch s.Adapter {
		case "backlog-md":
			if s.Host != "" || s.Repository != "" || s.Account != "" {
				return QueueConfig{}, fmt.Errorf("%s: github fields are not valid for backlog-md", label)
			}
			if strings.HasPrefix(s.Checkout, "~/") {
				s.Checkout = filepath.Join(env("HOME"), s.Checkout[2:])
			}
			if !filepath.IsAbs(s.Checkout) {
				return QueueConfig{}, fmt.Errorf("%s.checkout: absolute existing directory required", label)
			}
			st, err := os.Stat(s.Checkout)
			if s.Checkout == "" || err != nil || !st.IsDir() {
				return QueueConfig{}, fmt.Errorf("%s.checkout: existing directory required", label)
			}
			if s.Claims != nil && (s.Claims.Policy != "generic" || strings.TrimSpace(s.Claims.Source) == "") {
				return QueueConfig{}, fmt.Errorf("%s.claims: generic policy and source required", label)
			}
		case "github":
			parts := strings.Split(s.Repository, "/")
			if !validHost(s.Host) || len(parts) != 2 || !validRepoPart(parts[0]) || !validRepoPart(parts[1]) || strings.TrimSpace(s.Account) == "" {
				return QueueConfig{}, fmt.Errorf("%s: host, owner/repo repository, and account required", label)
			}
			if s.Checkout != "" || s.Claims != nil || s.AllowGitNetwork {
				return QueueConfig{}, fmt.Errorf("%s: backlog-md fields are not valid for github", label)
			}
		case "external":
			// The source-specific external fields were checked against their YAML nodes above.
		default:
			return QueueConfig{}, fmt.Errorf("%s.adapter: unknown adapter %q", label, s.Adapter)
		}
	}
	names := map[string]bool{}
	for i, v := range cfg.Views {
		label := fmt.Sprintf("views[%d]", i)
		if v.Name == "" || names[v.Name] {
			return QueueConfig{}, fmt.Errorf("%s.name: empty or duplicate name %q", label, v.Name)
		}
		names[v.Name] = true
		if v.Authority != LocalProfileName {
			if _, ok := profiles[v.Authority]; !ok {
				return QueueConfig{}, fmt.Errorf("%s.authority: unknown profile %q", label, v.Authority)
			}
		}
		if len(v.Sources) == 0 {
			return QueueConfig{}, fmt.Errorf("%s.sources: at least one source required", label)
		}
		for _, id := range v.Sources {
			if !seen[id] {
				return QueueConfig{}, fmt.Errorf("%s.sources: unknown source %q", label, id)
			}
		}
	}
	launchNames := map[string]bool{}
	for i, action := range cfg.Launch {
		label := fmt.Sprintf("launch[%d]", i)
		if strings.TrimSpace(action.Name) == "" || launchNames[action.Name] {
			return QueueConfig{}, fmt.Errorf("%s.name: empty or duplicate name", label)
		}
		launchNames[action.Name] = true
		if len(action.Argv) == 0 || action.Argv[0] == "" {
			return QueueConfig{}, fmt.Errorf("%s.argv: non-empty executable and argv required", label)
		}
		for j, arg := range action.Argv {
			if err := ValidateLaunchTemplate(arg); err != nil {
				return QueueConfig{}, fmt.Errorf("%s.argv[%d]: %w", label, j, err)
			}
		}
		if err := ValidateLaunchTemplate(action.Cwd); err != nil {
			return QueueConfig{}, fmt.Errorf("%s.cwd: %w", label, err)
		}
		for j, name := range action.PassEnv {
			if !validLaunchEnvName(name) || strings.HasPrefix(name, "WORKLEASE_QUEUE_") || name == "WORKLEASE_PROFILE" || name == "WORKLEASE_SESSION_ID" || name == "PI_SESSION_ID" || name == "PI_LOOP_RUN_ID" {
				return QueueConfig{}, fmt.Errorf("%s.passEnv[%d]: invalid or reserved variable", label, j)
			}
		}
	}
	return cfg, nil
}

func rejectQueueFields(fields map[string]*yaml.Node, label string, keys ...string) error {
	for _, key := range keys {
		if fields[key] != nil {
			return fmt.Errorf("%s.%s: not valid for this adapter", label, key)
		}
	}
	return nil
}

func validateExternalQueueSource(label string, fields map[string]*yaml.Node) error {
	sourceID, err := queueStringField(fields["id"], label+".id")
	if err != nil {
		return err
	}
	if !validExternalAdapterID(sourceID) {
		return fmt.Errorf("%s.id: invalid external source ID", label)
	}
	for _, key := range []string{"executable", "expectedAdapterId", "expectedVersion", "config"} {
		if fields[key] == nil {
			return fmt.Errorf("%s.%s: required for external adapter", label, key)
		}
	}
	path, err := queueStringField(fields["executable"], label+".executable")
	if err != nil {
		return err
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("%s.executable: absolute canonical path required", label)
	}
	adapterID, err := queueStringField(fields["expectedAdapterId"], label+".expectedAdapterId")
	if err != nil {
		return err
	}
	if !validExternalAdapterID(adapterID) {
		return fmt.Errorf("%s.expectedAdapterId: invalid adapter ID", label)
	}
	version, err := queueStringField(fields["expectedVersion"], label+".expectedVersion")
	if err != nil {
		return err
	}
	if !validExternalAdapterVersion(version) {
		return fmt.Errorf("%s.expectedVersion: expected semantic version", label)
	}
	if config := fields["config"]; config.Kind != yaml.MappingNode {
		return fmt.Errorf("%s.config: expected an object", label)
	} else if err := checkQueueConfigKeys(config, label+".config"); err != nil {
		return err
	}
	if claims := fields["claims"]; claims != nil {
		if claims.Kind != yaml.MappingNode {
			return fmt.Errorf("%s.claims: expected an object", label)
		}
		claimFields := nodeFields(claims)
		policy, err := queueStringField(claimFields["policy"], label+".claims.policy")
		if err != nil {
			return err
		}
		source, err := queueStringField(claimFields["source"], label+".claims.source")
		if err != nil {
			return err
		}
		if policy != "generic" {
			return fmt.Errorf("%s.claims.policy: external adapters require generic", label)
		}
		if err := validateQueueClaimSource(source); err != nil {
			return fmt.Errorf("%s.claims.source: %w", label, err)
		}
	}
	if account := fields["account"]; account != nil {
		value, err := queueStringField(account, label+".account")
		if err != nil {
			return err
		}
		if !validExternalQueueText(value) {
			return fmt.Errorf("%s.account: expected a non-empty safe principal", label)
		}
	}
	if workflow := fields["workflow"]; workflow != nil {
		for intent, transition := range nodeFields(workflow) {
			if !validExternalQueueText(transition.Value) {
				return fmt.Errorf("%s.workflow.%s: expected a non-empty safe provider transition", label, intent)
			}
		}
	}
	if credential := fields["credentialRef"]; credential != nil {
		value, err := queueStringField(credential, label+".credentialRef")
		if err != nil {
			return err
		}
		if !validExternalAdapterID(value) {
			return fmt.Errorf("%s.credentialRef: invalid credential reference", label)
		}
	}
	return nil
}

func validExternalQueueText(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func validateQueueClaimSource(source string) error {
	if !utf8.ValidString(source) || source == "" || strings.TrimSpace(source) != source || len([]byte(source)) > 1024 {
		return fmt.Errorf("expected stable non-empty identity without surrounding whitespace (maximum 1024 bytes)")
	}
	for _, char := range source {
		if char < 0x20 || char == 0x7f {
			return fmt.Errorf("identity must not contain control characters")
		}
	}
	return nil
}

func queueStringField(node *yaml.Node, path string) (string, error) {
	if node == nil || node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return "", fmt.Errorf("%s: expected a string", path)
	}
	return node.Value, nil
}

func checkQueueConfigKeys(node *yaml.Node, path string) error {
	if node.Kind == yaml.AliasNode {
		return fmt.Errorf("%s: YAML aliases are not supported", path)
	}
	switch node.Kind {
	case yaml.MappingNode:
		if len(node.Content)%2 != 0 {
			return fmt.Errorf("%s: invalid object", path)
		}
		seen := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || seen[key.Value] {
				return fmt.Errorf("%s.%s: unknown or duplicate key", path, key.Value)
			}
			seen[key.Value] = true
			if err := checkQueueConfigKeys(node.Content[i+1], path+"."+key.Value); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for i, child := range node.Content {
			if err := checkQueueConfigKeys(child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validExternalAdapterID(value string) bool {
	return value != "" && len(value) <= 128 && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}

// Protocol v1's manifest schema accepts semantic versions without build metadata.
func validExternalAdapterVersion(value string) bool {
	core, prerelease, hasPrerelease := strings.Cut(value, "-")
	if strings.ContainsAny(core, "+-") || hasPrerelease && prerelease == "" {
		return false
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 1 && part[0] == '0' || !decimalDigits(part) {
			return false
		}
	}
	if !hasPrerelease {
		return true
	}
	for _, identifier := range strings.Split(prerelease, ".") {
		if identifier == "" {
			return false
		}
		numeric := true
		for _, char := range identifier {
			if !(char >= '0' && char <= '9' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char == '-') {
				return false
			}
			if char < '0' || char > '9' {
				numeric = false
			}
		}
		if numeric && len(identifier) > 1 && identifier[0] == '0' {
			return false
		}
	}
	return true
}

func decimalDigits(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return value != ""
}

func validLaunchEnvName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		if c == '_' || c >= 'A' && c <= 'Z' || i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

func validRepoPart(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, " \\:@?#\t\r\n")
}

func validHost(s string) bool {
	return s != "" && !strings.ContainsAny(s, "/\\ :@\t\r\n") && !strings.HasPrefix(s, ".") && !strings.HasSuffix(s, ".")
}

func nodeFields(n *yaml.Node) map[string]*yaml.Node {
	fields := make(map[string]*yaml.Node)
	for i := 0; i+1 < len(n.Content); i += 2 {
		fields[n.Content[i].Value] = n.Content[i+1]
	}
	return fields
}

func checkQueueKeys(n *yaml.Node, path string, allowed map[string]bool) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: expected an object", path)
	}
	seen := map[string]bool{}
	for i := 0; i < len(n.Content); i += 2 {
		k := n.Content[i]
		if k.Tag != "!!str" || seen[k.Value] || allowed != nil && !allowed[k.Value] {
			return fmt.Errorf("%s.%s: unknown or duplicate key", path, k.Value)
		}
		seen[k.Value] = true
	}
	return nil
}
