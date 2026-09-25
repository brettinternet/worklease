package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/reason"
)

func queueFixture(checkout string) string {
	return "version: 1\nme:\n  github.com: brett\n  backlog-md: ['@brett']\nsources:\n  - id: local\n    adapter: backlog-md\n    checkout: " + checkout + "\n    claims: {policy: generic, source: project}\n  - id: remote\n    adapter: github\n    host: github.com\n    repository: acme/api\n    account: brett\nviews:\n  - name: Ready\n    authority: local\n    sources: [local, remote]\n    filter: {readiness: ready, claim: free, assigned: [me, nobody]}\n"
}

func TestQueueSchema(t *testing.T) {
	home := t.TempDir()
	base := queueFixture(home)
	env := func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}
	cfg, err := parseQueue([]byte(base), env, nil)
	if err != nil || len(cfg.Sources) != 2 || cfg.Sources[0].AllowGitNetwork {
		t.Fatalf("valid multi-source: %+v, %v", cfg, err)
	}
	launchConfig, err := parseQueue([]byte(base+"launch:\n  - name: agent\n    argv: [echo, '--', '{ref}']\n    cwd: '{checkout}'\n    passEnv: [GH_TOKEN, AWS_SESSION_TOKEN]\n"), env, nil)
	if err != nil || len(launchConfig.Launch) != 1 || launchConfig.Launch[0].Argv[2] != "{ref}" {
		t.Fatalf("valid launch: %+v, %v", launchConfig.Launch, err)
	}
	withWorkflow := strings.Replace(base, "    adapter: backlog-md", "    adapter: backlog-md\n    workflow: {start: 'In Progress', complete: Done}", 1)
	cfg, err = parseQueue([]byte(withWorkflow), env, nil)
	if err != nil || cfg.Sources[0].Workflow["start"] != "In Progress" {
		t.Fatalf("workflow mapping: %+v, %v", cfg.Sources[0].Workflow, err)
	}
	withGitNetwork := strings.Replace(base, "    adapter: backlog-md", "    adapter: backlog-md\n    allowGitNetwork: true", 1)
	cfg, err = parseQueue([]byte(withGitNetwork), env, nil)
	if err != nil || !cfg.Sources[0].AllowGitNetwork {
		t.Fatalf("literal true: %+v, %v", cfg, err)
	}
	cases := []struct{ name, content, want string }{
		{"version", strings.Replace(base, "version: 1", "version: 2", 1), "version"},
		{"trailing document", base + "---\nversion: 2\n", "multiple YAML documents"},
		{"top key", base + "unknown: []\n", "queue.unknown"},
		{"bad launch key", base + "launch: [{name: agent, argv: [echo], shell: true}]\n", "launch[0].shell"},
		{"bad launch placeholder", base + "launch: [{name: agent, argv: [echo, '{title}']}]\n", "launch[0].argv[1]"},
		{"bad cwd placeholder", base + "launch: [{name: agent, argv: [echo], cwd: '{body}'}]\n", "launch[0].cwd"},
		{"bad argv", base + "launch: [{name: agent, argv: []}]\n", "launch[0].argv"},
		{"duplicate launch", base + "launch: [{name: agent, argv: [echo]}, {name: agent, argv: [echo]}]\n", "launch[1].name"},
		{"bad env name", base + "launch: [{name: agent, argv: [echo], passEnv: ['GH-TOKEN']}]\n", "launch[0].passEnv[0]"},
		{"reserved env name", base + "launch: [{name: agent, argv: [echo], passEnv: [WORKLEASE_QUEUE_REF]}]\n", "launch[0].passEnv[0]"},
		{"reserved session ID", base + "launch: [{name: agent, argv: [echo], passEnv: [WORKLEASE_SESSION_ID]}]\n", "launch[0].passEnv[0]"},
		{"duplicate me", strings.Replace(base, "  github.com: brett", "  github.com: brett\n  github.com: alice", 1), "me.github.com"},
		{"wrong git boolean", strings.Replace(base, "    adapter: backlog-md", "    adapter: backlog-md\n    allowGitNetwork: not-a-bool", 1), "allowGitNetwork: expected boolean"},
		{"quoted git boolean", strings.Replace(base, "    adapter: backlog-md", "    adapter: backlog-md\n    allowGitNetwork: \"yes\"", 1), "allowGitNetwork: expected boolean"},
		{"me boolean", strings.Replace(base, "backlog-md: ['@brett']", "backlog-md: [true]", 1), "expected assignee strings"},
		{"me number", strings.Replace(base, "backlog-md: ['@brett']", "backlog-md: [12]", 1), "expected assignee strings"},
		{"duplicate yaml key", base + "version: 1\n", "queue.version"},
		{"unknown me", strings.Replace(base, "github.com: brett", "github.com: [brett]", 1), "me.github.com"},
		{"unknown source key", strings.Replace(base, "    adapter: backlog-md", "    bogus: yes\n    adapter: backlog-md", 1), "sources[0].bogus"},
		{"unknown workflow intent", strings.Replace(base, "    adapter: backlog-md", "    adapter: backlog-md\n    workflow: {assign: Active}", 1), "sources[0].workflow.assign"},
		{"empty workflow transition", strings.Replace(base, "    adapter: backlog-md", "    adapter: backlog-md\n    workflow: {start: ''}", 1), "sources[0].workflow.start"},
		{"non-string workflow transition", strings.Replace(base, "    adapter: backlog-md", "    adapter: backlog-md\n    workflow: {start: true}", 1), "sources[0].workflow.start"},
		{"unknown claims key", strings.Replace(base, "policy: generic", "policy: generic, extra: 1", 1), "sources[0].claims.extra"},
		{"bad claims", strings.Replace(base, "policy: generic", "policy: backlog-md", 1), "sources[0].claims"},
		{"unknown adapter", strings.Replace(base, "adapter: github", "adapter: jira", 1), "sources[1].adapter"},
		{"duplicate id", strings.Replace(base, "id: remote", "id: local", 1), "sources[1].id"},
		{"colon in id", strings.Replace(strings.Replace(base, "id: remote", "id: 'team:remote'", 1), "[local, remote]", "[local, 'team:remote']", 1), "sources[1].id: must not contain ':'"},
		{"missing adapter", strings.Replace(base, "    adapter: backlog-md\n", "", 1), "sources[0].adapter"},
		{"missing checkout", strings.Replace(base, "checkout: "+home, "checkout: /nonexistent-queue-checkout", 1), "sources[0].checkout"},
		{"missing host", strings.Replace(base, "    host: github.com\n", "", 1), "sources[1].host"},
		{"missing repository", strings.Replace(base, "    repository: acme/api\n", "", 1), "sources[1].repository"},
		{"missing account", strings.Replace(base, "    account: brett\n", "", 1), "sources[1].account"},
		{"missing filter", strings.Replace(base, "    filter: {readiness: ready, claim: free, assigned: [me, nobody]}\n", "", 1), "views[0].filter"},
		{"bad repo", strings.Replace(base, "repository: acme/api", "repository: acme", 1), "sources[1]"},
		{"bad repo path", strings.Replace(base, "repository: acme/api", "repository: ../api", 1), "sources[1]"},
		{"unknown filter", strings.Replace(base, "claim: free", "unknown: free", 1), "views[0].filter.unknown"},
		{"unknown view key", strings.Replace(base, "    authority: local", "    typo: true\n    authority: local", 1), "views[0].typo"},
		{"unknown authority", strings.Replace(base, "authority: local", "authority: ghost", 1), "views[0].authority"},
		{"unknown source", strings.Replace(base, "[local, remote]", "[local, ghost]", 1), "views[0].sources"},
		{"duplicate view", base + "  - name: Ready\n    authority: local\n    sources: [local]\n    filter: {}\n", "views[1].name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseQueue([]byte(tc.content), env, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestQueueRemoteCredentialHelperConfiguration(t *testing.T) {
	t.Parallel()
	for _, adapter := range []string{"linear", "jira-cloud"} {
		t.Run(adapter, func(t *testing.T) {
			t.Parallel()
			base := "version: 1\nme: {}\nsources:\n  - id: remote\n    adapter: " + adapter + "\n    account: alice\n    credentialHelper: [/usr/bin/credential-helper, --token]\nviews:\n  - name: Ready\n    authority: local\n    sources: [remote]\n    filter: {}\n"
			cfg, err := parseQueue([]byte(base), nil, nil)
			if err != nil || len(cfg.Sources) != 1 || len(cfg.Sources[0].CredentialHelper) != 2 {
				t.Fatalf("valid helper: %+v %v", cfg, err)
			}
			for _, tc := range []struct{ replace, with, want string }{
				{"[/usr/bin/credential-helper, --token]", "[credential-helper]", "credentialHelper"},
				{"[/usr/bin/credential-helper, --token]", "[]", "credentialHelper"},
				{"[/usr/bin/credential-helper, --token]", "plain-token", "queue.yaml"},
				{"    credentialHelper: [/usr/bin/credential-helper, --token]\n", "", "credentialHelper"},
				{"    account: alice\n", "", "account"},
				{"    account: alice\n", "    account: alice\n    claims: {policy: linear, source: org}\n", "claims and writes"},
			} {
				_, err := parseQueue([]byte(strings.Replace(base, tc.replace, tc.with, 1)), nil, nil)
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("invalid helper configuration: expected %q, got %v", tc.want, err)
				}
			}
		})
	}
}

func TestQueueExternalAdapterConfiguration(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	env := func(key string) string {
		if key == "HOME" {
			return home
		}
		return ""
	}
	valid := externalQueueFixture("/opt/worklease/adapters/example")
	cfg, err := parseQueue([]byte(valid), env, nil)
	if err != nil {
		t.Fatalf("valid external source: %v", err)
	}
	source := cfg.Sources[0]
	if source.Adapter != "external" || source.Executable != "/opt/worklease/adapters/example" || source.ExpectedAdapterID != "example.adapter" || source.ExpectedVersion != "1.2.3-rc.1" || source.Config["tenant"] != "acme" || source.CredentialRef != "credential:github" || source.Claims != nil {
		t.Fatalf("external read-only configuration was not preserved: %+v", source)
	}
	withClaims := strings.Replace(valid, "    config: {tenant: acme}\n", "    config: {tenant: acme}\n    claims: {policy: generic, source: acme/planning}\n", 1)
	cfg, err = parseQueue([]byte(withClaims), env, nil)
	if err != nil || cfg.Sources[0].Claims == nil || *cfg.Sources[0].Claims != (QueueClaims{Policy: "generic", Source: "acme/planning"}) {
		t.Fatalf("explicit generic claim binding: %+v, %v", cfg, err)
	}
	withWrites := strings.Replace(valid, "    config: {tenant: acme}\n", "    config: {tenant: acme}\n    account: alice\n    workflow: {start: Doing, complete: Done}\n", 1)
	cfg, err = parseQueue([]byte(withWrites), env, nil)
	if err != nil || cfg.Sources[0].Account != "alice" || cfg.Sources[0].Workflow["start"] != "Doing" || cfg.Sources[0].Workflow["complete"] != "Done" {
		t.Fatalf("external write identity and workflow mapping: %+v, %v", cfg.Sources[0], err)
	}

	cases := []struct{ name, content, want string }{
		{"missing source ID", strings.Replace(valid, "- id: docs", "-", 1), "sources[0].id: expected a string"},
		{"unknown field", strings.Replace(valid, "    adapter: external", "    adapter: external\n    extra: true", 1), "sources[0].extra"},
		{"built-in checkout", strings.Replace(valid, "    adapter: external", "    adapter: external\n    checkout: /tmp", 1), "sources[0].checkout"},
		{"built-in github locator", strings.Replace(valid, "    adapter: external", "    adapter: external\n    host: github.com", 1), "sources[0].host"},
		{"built-in repository locator", strings.Replace(valid, "    adapter: external", "    adapter: external\n    repository: org/repo", 1), "sources[0].repository"},
		{"relative executable", strings.Replace(valid, "/opt/worklease/adapters/example", "./adapter", 1), "sources[0].executable"},
		{"missing expected adapter ID", strings.Replace(valid, "    expectedAdapterId: example.adapter\n", "", 1), "expectedAdapterId: required"},
		{"invalid adapter ID", strings.Replace(valid, "example.adapter", "", 1), "expectedAdapterId"},
		{"missing expected version", strings.Replace(valid, "    expectedVersion: 1.2.3-rc.1\n", "", 1), "expectedVersion: required"},
		{"invalid semantic version", strings.Replace(valid, "1.2.3-rc.1", "01.2.3", 1), "expectedVersion"},
		{"missing config", strings.Replace(valid, "    config: {tenant: acme}\n", "", 1), "config: required"},
		{"non-object config", strings.Replace(valid, "config: {tenant: acme}", "config: [acme]", 1), "config: expected an object"},
		{"duplicate config key", strings.Replace(valid, "config: {tenant: acme}", "config: {tenant: acme, tenant: other}", 1), "config.tenant"},
		{"invalid claim policy", strings.Replace(valid, "    config: {tenant: acme}\n", "    config: {tenant: acme}\n    claims: {policy: github, source: acme/planning}\n", 1), "claims.policy: external adapters require generic"},
		{"blank claim source", strings.Replace(valid, "    config: {tenant: acme}\n", "    config: {tenant: acme}\n    claims: {policy: generic, source: '  '}\n", 1), "claims.source"},
		{"ambiguous claim source", strings.Replace(valid, "    config: {tenant: acme}\n", "    config: {tenant: acme}\n    claims: {policy: generic, source: ' acme/planning'}\n", 1), "claims.source"},
		{"missing claim source", strings.Replace(valid, "    config: {tenant: acme}\n", "    config: {tenant: acme}\n    claims: {policy: generic}\n", 1), "claims.source: expected a string"},
		{"empty account", strings.Replace(valid, "    config: {tenant: acme}\n", "    config: {tenant: acme}\n    account: '  '\n", 1), "account: expected a non-empty safe principal"},
		{"unsafe account", strings.Replace(valid, "    config: {tenant: acme}\n", "    config: {tenant: acme}\n    account: \"alice\\nbob\"\n", 1), "account: expected a non-empty safe principal"},
		{"unsafe workflow transition", strings.Replace(valid, "    config: {tenant: acme}\n", "    config: {tenant: acme}\n    workflow: {start: \"Doing\\nNow\"}\n", 1), "workflow.start: expected a non-empty safe provider transition"},
		{"unknown credential type", strings.Replace(valid, "credential:github", "true", 1), "credentialRef: expected a string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseQueue([]byte(tc.content), env, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}

	withoutCredential := strings.Replace(valid, "    credentialRef: credential:github\n", "", 1)
	if _, err := parseQueue([]byte(withoutCredential), env, nil); err != nil {
		t.Fatalf("optional credential reference: %v", err)
	}
	builtInWithExternalField := strings.Replace(queueFixture(home), "    adapter: backlog-md", "    adapter: backlog-md\n    executable: /opt/adapter", 1)
	if _, err := parseQueue([]byte(builtInWithExternalField), env, nil); err == nil || !strings.Contains(err.Error(), "sources[0].executable") {
		t.Fatalf("built-in accepted external-only field: %v", err)
	}
}

func externalQueueFixture(executable string) string {
	return "version: 1\nme: {}\nsources:\n  - id: docs\n    adapter: external\n    executable: " + executable + "\n    expectedAdapterId: example.adapter\n    expectedVersion: 1.2.3-rc.1\n    config: {tenant: acme}\n    credentialRef: credential:github\nviews:\n  - name: Ready\n    authority: local\n    sources: [docs]\n    filter: {}\n"
}

func TestQueueRecoveryDir(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	if got := QueueRecoveryDir(func(key string) string {
		if key == "HOME" {
			return home
		}
		return ""
	}); got != filepath.Join(home, ".local", "state", "worklease", "queue-recovery") {
		t.Fatalf("default recovery location: %s", got)
	}
	if got := QueueRecoveryDir(func(key string) string {
		if key == "XDG_STATE_HOME" {
			return home
		}
		return ""
	}); got != filepath.Join(home, "worklease", "queue-recovery") {
		t.Fatalf("XDG recovery location: %s", got)
	}
}

func TestLoadQueuePrivate(t *testing.T) {
	home := t.TempDir()
	if got := QueuePath(func(k string) string {
		if k == "HOME" {
			return home
		}
		return ""
	}); got != filepath.Join(home, ".config", "worklease", "queue.yaml") {
		t.Fatalf("HOME fallback: %s", got)
	}
	configDir := filepath.Join(home, "worklease")
	if err := os.Mkdir(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(configDir, "queue.yaml")
	env := func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return home
		}
		if k == "HOME" {
			return home
		}
		return ""
	}
	_, err := LoadQueue(env)
	var r *reason.Error
	if !errors.As(err, &r) || r.Reason != "no-sources-configured" || !strings.Contains(err.Error(), "https://github.com/brettinternet/worklease/blob/main/docs/queue.md") {
		t.Fatalf("missing diagnostic: %v", err)
	}
	fixture := strings.Replace(queueFixture(home), "checkout: "+home, "checkout: ~/worklease", 1)
	if err := os.Mkdir(filepath.Join(home, "worklease"), 0700); !os.IsExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	profiles := "profiles:\n  - name: team\n    endpoint: https://example.com\n    credential: {path: /private/token}\n"
	if err := os.WriteFile(filepath.Join(configDir, "profiles.yaml"), []byte(profiles), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("invalid: ["), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQueue(env); !errors.As(err, &r) || r.Reason != reason.ReasonConfigInvalid {
		t.Fatalf("invalid YAML not classified as config-invalid: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(fixture, "authority: local", "authority: team", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadQueue(env)
	if err != nil || cfg.Sources[0].Checkout != filepath.Join(home, "worklease") || cfg.Views[0].Authority != "team" {
		t.Fatalf("tilde: %+v %v", cfg, err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQueue(env); err == nil {
		t.Fatal("group-readable file accepted")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQueue(env); err == nil {
		t.Fatal("world-readable file accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, path+".real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path+".real", path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQueue(env); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQueue(env); err == nil {
		t.Fatal("public config directory accepted")
	}
}
