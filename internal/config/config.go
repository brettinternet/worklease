// Package config resolves Worklease's typed configuration.
package config

import (
	"fmt"
	"math"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brettinternet/worklease/internal/reason"
	"gopkg.in/yaml.v3"
)

// Input contains raw flags, environment access, and optional explicit paths.
type Input struct {
	Flags      map[string]string
	Env        func(string) string
	Home       string
	ConfigPath string
}

// Config contains resolved settings and the source which supplied each value.
type Config struct {
	Home          string
	ConfigPath    string
	SessionID     string
	AgentID       string
	TTL           time.Duration
	MaxDuration   time.Duration
	RetentionDays float64
	PollInterval  time.Duration
	Sources       map[string]string
}

const (
	DefaultTTL          = 15 * time.Minute
	DefaultMaxDuration  = time.Hour
	DefaultRetention    = 30.0
	DefaultPollInterval = 250 * time.Millisecond
)

var envNames = map[string]string{
	"home": "WORKLEASE_HOME", "config": "WORKLEASE_CONFIG", "session": "WORKLEASE_SESSION_ID",
	"agent": "WORKLEASE_AGENT_ID", "ttl": "WORKLEASE_TTL", "max_duration": "WORKLEASE_MAX_DURATION",
	"retention_days": "WORKLEASE_RETENTION_DAYS", "poll_interval": "WORKLEASE_POLL_INTERVAL",
}

// Load applies flag, environment, YAML, and default precedence. Empty strings
// are unset at every layer. A default config file is optional; an explicitly
// selected config file must exist.
func Load(in Input) (Config, error) {
	env := in.Env
	if env == nil {
		env = os.Getenv
	}
	flags := in.Flags
	if flags == nil {
		flags = map[string]string{}
	}
	flag := func(key string) string { return strings.TrimSpace(flags[key]) }
	environment := func(key string) string { return strings.TrimSpace(env(envNames[key])) }

	configPath, configSource := flag("config"), "flag"
	if configPath == "" {
		configPath, configSource = environment("config"), "env"
	}
	if configPath == "" && strings.TrimSpace(in.ConfigPath) != "" {
		configPath, configSource = strings.TrimSpace(in.ConfigPath), "flag"
	}
	if configPath == "" {
		configPath, configSource = defaultConfigPath(env), "default"
	}
	configPath, err := expandPath(configPath)
	if err != nil {
		return Config{}, invalid("config_path", configSource, err)
	}

	fileValues, err := loadFile(configPath, configSource == "default")
	if err != nil {
		return Config{}, err
	}
	values := make(map[string]string)
	sources := make(map[string]string)
	for _, key := range []string{"home", "session", "agent", "ttl", "max_duration", "retention_days", "poll_interval"} {
		value, source := flag(key), "flag"
		if value == "" {
			value, source = environment(key), "env"
		}
		if value == "" {
			value, source = fileValues[key], "file"
		}
		if value != "" {
			values[key], sources[key] = strings.TrimSpace(value), source
		}
	}

	cfg := Config{
		ConfigPath: configPath,
		TTL:        DefaultTTL, MaxDuration: DefaultMaxDuration,
		RetentionDays: DefaultRetention, PollInterval: DefaultPollInterval,
		Sources: sources,
	}
	cfg.Sources["config_path"] = configSource

	home := values["home"]
	if home == "" && strings.TrimSpace(in.Home) != "" {
		home, cfg.Sources["home"] = strings.TrimSpace(in.Home), "flag"
	}
	if home == "" {
		home, cfg.Sources["home"] = defaultHome(env), "default"
	}
	cfg.Home, err = expandPath(home)
	if err != nil || !filepath.IsAbs(cfg.Home) {
		if err == nil {
			err = fmt.Errorf("must be absolute after expansion")
		}
		return Config{}, invalid("home", cfg.Sources["home"], err)
	}

	cfg.SessionID = values["session"]
	if cfg.AgentID = values["agent"]; cfg.AgentID == "" {
		current, userErr := user.Current()
		if userErr != nil || strings.TrimSpace(current.Username) == "" {
			return Config{}, reason.New(reason.ReasonAgentIDRequired, "agent ID is required").With("key", "agent").With("source", "default")
		}
		cfg.AgentID, cfg.Sources["agent"] = current.Username, "default"
	}
	if err := validateID("session", cfg.SessionID); err != nil {
		return Config{}, invalid("session", sourceOf(sources, "session"), err)
	}
	if err := validateID("agent", cfg.AgentID); err != nil {
		return Config{}, invalid("agent", sourceOf(cfg.Sources, "agent"), err)
	}

	cfg.TTL, err = parseDuration("ttl", values["ttl"], DefaultTTL, time.Second, time.Hour, sourceOf(sources, "ttl"))
	if err != nil {
		return Config{}, err
	}
	cfg.MaxDuration, err = parseDuration("max_duration", values["max_duration"], DefaultMaxDuration, time.Second, 24*time.Hour, sourceOf(sources, "max_duration"))
	if err != nil {
		return Config{}, err
	}
	if raw := values["retention_days"]; raw != "" {
		cfg.RetentionDays, err = strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(cfg.RetentionDays) || math.IsInf(cfg.RetentionDays, 0) || cfg.RetentionDays <= 0 {
			return Config{}, invalid("retention_days", sourceOf(sources, "retention_days"), fmt.Errorf("must be greater than zero"))
		}
	}
	cfg.PollInterval, err = parseDuration("poll_interval", values["poll_interval"], DefaultPollInterval, 10*time.Millisecond, 30*time.Second, sourceOf(sources, "poll_interval"))
	if err != nil {
		return Config{}, err
	}
	for _, key := range []string{"session", "agent", "ttl", "max_duration", "retention_days", "poll_interval"} {
		if _, ok := cfg.Sources[key]; !ok {
			cfg.Sources[key] = "default"
		}
	}
	return cfg, nil
}

func sourceOf(sources map[string]string, key string) string {
	if source := sources[key]; source != "" {
		return source
	}
	return "default"
}

// loadFile parses the YAML document without converting values through weak
// interface types, which makes wrong YAML types and duplicate keys explicit.
func loadFile(path string, optional bool) (map[string]string, error) {
	st, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) && optional {
			return map[string]string{}, nil
		}
		if os.IsNotExist(err) {
			return nil, reason.New("config-missing", "configuration file is missing").With("path", path)
		}
		return nil, invalid("config_path", "file", err)
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
		return nil, invalid("config_path", "file", fmt.Errorf("must be a regular file and not a symlink"))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, invalid("config_path", "file", err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, invalid("config", "file", err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind == 0 {
		return map[string]string{}, nil
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, invalid("config", "file", fmt.Errorf("must be a mapping"))
	}
	known := map[string]bool{"home": true, "agent_id": true, "ttl": true, "max_duration": true, "retention_days": true, "poll_interval": true}
	values := make(map[string]string)
	for i := 0; i < len(doc.Content); i += 2 {
		keyNode, valueNode := doc.Content[i], doc.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Tag != "!!str" {
			return nil, invalid("config", "file", fmt.Errorf("keys must be strings"))
		}
		key := keyNode.Value
		if !known[key] {
			return nil, invalid(key, "file", fmt.Errorf("unknown field"))
		}
		canonical := key
		if key == "agent_id" {
			canonical = "agent"
		}
		if valueNode.Tag == "!!null" {
			continue
		}
		if valueNode.Kind != yaml.ScalarNode {
			return nil, invalid(key, "file", fmt.Errorf("must be a scalar"))
		}
		if key == "retention_days" {
			if valueNode.Tag != "!!int" && valueNode.Tag != "!!float" {
				return nil, invalid(key, "file", fmt.Errorf("must be a number"))
			}
		} else if valueNode.Tag != "!!str" && valueNode.Tag != "!!int" {
			return nil, invalid(key, "file", fmt.Errorf("must be a string or duration integer"))
		}
		values[canonical] = valueNode.Value
	}
	return values, nil
}

func parseDuration(key, raw string, fallback, min, max time.Duration, source string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	raw = strings.TrimSpace(raw)
	var d time.Duration
	var err error
	if isInteger(raw) {
		var seconds int64
		seconds, err = strconv.ParseInt(raw, 10, 64)
		if err == nil {
			d = time.Duration(seconds) * time.Second
		}
	} else {
		d, err = time.ParseDuration(raw)
	}
	if err != nil || d < min || d > max {
		return 0, invalid(key, source, fmt.Errorf("must be between %s and %s", min, max))
	}
	return d, nil
}

func isInteger(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if (r < '0' || r > '9') && !(i == 0 && r == '-') {
			return false
		}
	}
	return true
}

func validateID(key, value string) error {
	if value == "" {
		return nil
	}
	if !utf8.ValidString(value) || len([]byte(value)) > 128 {
		return fmt.Errorf("must be 1 to 128 UTF-8 bytes")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("must not contain control characters")
		}
	}
	return nil
}

func invalid(key, source string, err error) error {
	return reason.New("config-invalid", fmt.Sprintf("%s: %s", key, err)).With("key", key).With("source", source)
}

func expandPath(value string) (string, error) {
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
	}
	return filepath.Abs(value)
}

func defaultHome(env func(string) string) string {
	if value := strings.TrimSpace(env("XDG_STATE_HOME")); value != "" {
		return filepath.Join(value, "worklease")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "state", "worklease")
	}
	return filepath.Join(os.TempDir(), "worklease")
}

func defaultConfigPath(env func(string) string) string {
	if value := strings.TrimSpace(env("XDG_CONFIG_HOME")); value != "" {
		return filepath.Join(value, "worklease", "config.yaml")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "worklease", "config.yaml")
	}
	return filepath.Join(os.TempDir(), "worklease", "config.yaml")
}
