package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadPrecedenceAndSources(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("home: /from-file\nagent_id: file-agent\nttl: 3s\nmax_duration: 4s\nretention_days: 5\npoll_interval: 20ms\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(Input{
		Flags: map[string]string{"config": path, "home": "/from-flag", "ttl": "7s"},
		Env:   env(map[string]string{"WORKLEASE_HOME": "/from-env", "WORKLEASE_AGENT_ID": "env-agent", "WORKLEASE_TTL": "6s"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Home != "/from-flag" || cfg.AgentID != "env-agent" || cfg.TTL != 7*time.Second || cfg.MaxDuration != 4*time.Second || cfg.RetentionDays != 5 || cfg.PollInterval != 20*time.Millisecond {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	wantSources := map[string]string{"home": "flag", "agent": "env", "ttl": "flag", "max_duration": "file", "retention_days": "file", "poll_interval": "file", "session": "default", "config_path": "flag"}
	for key, want := range wantSources {
		if got := cfg.Sources[key]; got != want {
			t.Errorf("source %s = %q, want %q", key, got, want)
		}
	}
}

func TestLoadSessionAndBlankValues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg, err := Load(Input{Flags: map[string]string{"agent": " ", "session": "flag-session"}, Env: env(map[string]string{
		"XDG_CONFIG_HOME": dir, "XDG_STATE_HOME": dir, "WORKLEASE_AGENT_ID": "env-agent", "WORKLEASE_SESSION_ID": "env-session",
	})})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionID != "flag-session" || cfg.AgentID != "env-agent" || cfg.Sources["session"] != "flag" {
		t.Fatalf("unexpected identity config: %+v", cfg)
	}
}

func TestLoadDurationFormsAndBounds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	base := map[string]string{"XDG_CONFIG_HOME": dir, "WORKLEASE_AGENT_ID": "agent"}
	cfg, err := Load(Input{Env: env(base), Flags: map[string]string{"ttl": "2", "poll_interval": "10ms", "max_duration": "24h"}})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TTL != 2*time.Second || cfg.PollInterval != 10*time.Millisecond || cfg.MaxDuration != 24*time.Hour {
		t.Fatalf("unexpected durations: %+v", cfg)
	}
	_, err = Load(Input{Env: env(base), Flags: map[string]string{"ttl": "0"}})
	assertReason(t, err, reason.ReasonConfigInvalid)
}

func TestLoadRejectsUnknownFieldsWrongTypesAndSymlinks(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, body string
	}{
		{"unknown", "mystery: value\n"},
		{"wrong type", "ttl: [1, 2]\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := Load(Input{ConfigPath: path, Env: env(map[string]string{"WORKLEASE_AGENT_ID": "agent"})})
			assertReason(t, err, reason.ReasonConfigInvalid)
		})
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real.yaml")
	link := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(target, []byte("agent_id: agent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, err := Load(Input{ConfigPath: link, Env: env(nil)})
	assertReason(t, err, reason.ReasonConfigInvalid)
}

func TestLoadExplicitAndDefaultMissingConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := Load(Input{ConfigPath: filepath.Join(dir, "missing.yaml"), Env: env(map[string]string{"WORKLEASE_AGENT_ID": "agent"})})
	assertReason(t, err, reason.ReasonConfigMissing)
	cfg, err := Load(Input{Env: env(map[string]string{"XDG_CONFIG_HOME": dir, "XDG_STATE_HOME": dir, "WORKLEASE_AGENT_ID": "agent"})})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sources["config_path"] != "default" {
		t.Fatalf("config source = %q", cfg.Sources["config_path"])
	}
}

func assertReason(t *testing.T, err error, want string) {
	t.Helper()
	var got *reason.Error
	if !errors.As(err, &got) || got.Reason != want {
		t.Fatalf("error = %#v, want reason %s", err, want)
	}
}
