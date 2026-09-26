package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/testkit"
)

type initHarness struct {
	t                         *testing.T
	checkout, configPath, bin string
}

func newInitHarness(t *testing.T) initHarness {
	t.Helper()
	_, paths := testkit.Home(t)
	for name, value := range paths {
		t.Setenv(name, value)
	}
	checkout := filepath.Join(t.TempDir(), "project")
	if out, err := testkit.GitCommand("init", "-b", "main", checkout).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(checkout, "backlog.config.yml"), []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := testkit.GitCommand("-C", checkout, "add", "backlog.config.yml").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, out)
	}
	if out, err := testkit.GitCommand("-C", checkout, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v %s", err, out)
	}
	bin := t.TempDir()
	backlog := "#!/bin/sh\ncase \"$*\" in\n  'config get statuses') printf '%s\\n' \"${INIT_STATUSES:-To Do, In Progress, Done}\" ;;\n  'config get defaultStatus') printf '%s\\n' \"${INIT_DEFAULT_STATUS:-To Do}\" ;;\n  'config get defaultAssignee') printf '%s\\n' \"${INIT_ASSIGNEE-@tester}\" ;;\n  'config get '*) printf '%s\\n' \"${INIT_NETWORK:-false}\" ;;\n  '--version') printf '%s\\n' \"${INIT_VERSION:-1.52.0}\" ;;\n  'task list --json') if [ \"${INIT_LIST_ERROR:-0}\" = 1 ]; then exit 1; fi; printf '%s\\n' \"$INIT_LIST_JSON\" ;;\n  *) exit 2 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "backlog"), []byte(backlog), 0700); err != nil {
		t.Fatal(err)
	}
	gh := "#!/bin/sh\ncase \"$*\" in\n  'api --hostname github.com user --jq .login') if [ \"${INIT_GH_UNAUTH:-0}\" = 0 ]; then printf 'tester\\n'; else exit 1; fi ;;\n  'auth token --hostname github.com --user tester') printf 'fake-token\\n' ;;\n  *) exit 1 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(gh), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INIT_LIST_JSON", `{"kind":"task-list","schemaVersion":1,"tasks":[]}`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return initHarness{t, checkout, config.QueuePath(os.Getenv), bin}
}

func (h initHarness) invoke(args ...string) testkit.CLIResult {
	h.t.Helper()
	argv := append([]string{"worklease", "--home", os.Getenv("WORKLEASE_HOME"), "queue", "init", "--checkout", h.checkout}, args...)
	return testkit.RunCLI(context.Background(), argv, func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		return Run(ctx, args, "test", "unknown", "unknown", stdout, stderr)
	})
}

func TestQueueInitDirectWriteDryRunAndIdempotence(t *testing.T) {
	h := newInitHarness(t)
	t.Setenv("INIT_ASSIGNEE", "")
	preview := h.invoke("--dry-run", "--json")
	if preview.Err != nil {
		t.Fatalf("preview: %v %s", preview.Err, preview.Stdout)
	}
	var payload struct {
		queueInitResult
		SchemaVersion int `json:"schemaVersion"`
	}
	if err := json.Unmarshal(preview.Stdout, &payload); err != nil {
		t.Fatal(err)
	}
	osLogin, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if payload.SchemaVersion != 2 || payload.Applied || payload.Outcome != "created" || len(payload.Facts) < 5 || !strings.Contains(payload.YAML, "allowGitNetwork: false") || !strings.Contains(payload.YAML, "@"+osLogin.Username) || !strings.Contains(string(preview.Stdout), `"origin":"OS user"`) {
		t.Fatalf("preview: %s", preview.Stdout)
	}
	if _, err := os.Stat(filepath.Dir(h.configPath)); !os.IsNotExist(err) {
		t.Fatalf("preview created directory: %v", err)
	}
	plain := h.invoke("--dry-run")
	if plain.Err != nil || !strings.Contains(string(plain.Stdout), "YAML:\n") || !strings.Contains(string(plain.Stdout), "from OS user") {
		t.Fatalf("text preview: %v %s", plain.Err, plain.Stdout)
	}
	if _, err := os.Stat(config.QueueIdentityPath(os.Getenv)); !os.IsNotExist(err) {
		t.Fatalf("preview wrote identity: %v", err)
	}
	rejected := h.invoke("--apply", "--json")
	if rejected.Err == nil || !strings.Contains(string(rejected.Stdout), "invalid command-line arguments") {
		t.Fatalf("removed --apply accepted: %v %s", rejected.Err, rejected.Stdout)
	}
	t.Setenv("INIT_LIST_JSON", `{"kind":"task-list","schemaVersion":1,"tasks":[{"id":"TASK-1","title":"Ready work","status":"To Do","ordinal":1,"isReady":true,"dependencies":[]}]}`)
	applied := h.invoke("--json")
	if applied.Err != nil {
		t.Fatalf("apply: %v %s %s", applied.Err, applied.Stdout, applied.Stderr)
	}
	if err := json.Unmarshal(applied.Stdout, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Applied || payload.Identity != "confirmed" || len(payload.NextCommands) != 1 || payload.NextCommands[0] != "worklease queue" {
		t.Fatalf("apply: %s", applied.Stdout)
	}
	written, err := os.ReadFile(h.configPath)
	if err != nil || string(written) != payload.YAML {
		t.Fatalf("rendered YAML differs from written file: %v", err)
	}
	for _, flow := range []string{"me: {", "sources: [{", "views: [{", "workflow: {", "filter: {", "assigned: ["} {
		if strings.Contains(string(written), flow) {
			t.Errorf("flow-style output %q: %s", flow, written)
		}
	}
	for path, mode := range map[string]os.FileMode{filepath.Dir(h.configPath): 0700, h.configPath: 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("mode %s: %v %v", path, info, err)
		}
	}
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil || len(cfg.Sources) != 1 || cfg.Sources[0].Workflow["start"] != "In Progress" {
		t.Fatalf("load: %+v %v", cfg, err)
	}
	ids, err := config.LoadQueueIdentities(os.Getenv)
	if err != nil || ids.Sources[payload.SourceID].Adapter != "backlog-md" {
		t.Fatalf("identity: %+v %v", ids, err)
	}
	query := testkit.RunCLI(context.Background(), []string{"worklease", "--home", os.Getenv("WORKLEASE_HOME"), "queue", "query", "--view", "Ready", "--json"}, func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		return Run(ctx, args, "test", "unknown", "unknown", stdout, stderr)
	})
	if query.Err != nil || !strings.Contains(string(query.Stdout), `"eligible":true`) || strings.Contains(string(query.Stdout), "binding-migration-required") {
		t.Fatalf("claim unavailable after automatic confirmation: %v %s", query.Err, query.Stdout)
	}
	before, _ := os.ReadFile(h.configPath)
	again := h.invoke("--json")
	if again.Err != nil || !strings.Contains(string(again.Stdout), `"outcome":"unchanged"`) {
		t.Fatalf("repeat: %v %s", again.Err, again.Stdout)
	}
	after, _ := os.ReadFile(h.configPath)
	if string(before) != string(after) {
		t.Fatal("idempotent init changed file")
	}
	plainWrite := h.invoke()
	if plainWrite.Err != nil || strings.Contains(string(plainWrite.Stdout), "YAML:") || !strings.Contains(string(plainWrite.Stdout), "Source: project (backlog-md)") || !strings.HasSuffix(strings.TrimSpace(string(plainWrite.Stdout)), "worklease queue") {
		t.Fatalf("write summary: %v %s", plainWrite.Err, plainWrite.Stdout)
	}
}

func TestQueueInitMergeAndUnmapped(t *testing.T) {
	h := newInitHarness(t)
	original := "# retained\nversion: 1\nme:\n  backlog-md: ['@tester']\nsources:\n  - id: project\n    adapter: backlog-md\n    checkout: " + h.checkout + "\nviews:\n  - name: Ready # retained view\n    authority: local\n    sources: [project]\n    filter: {assigned: [me]}\nlaunch:\n  - name: terminal\n    argv: [echo, hello]\n"
	if err := os.MkdirAll(filepath.Dir(h.configPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.configPath, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(t.TempDir(), "project")
	if out, err := testkit.GitCommand("init", "-b", "main", second).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(second, "backlog.config.yml"), []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INIT_STATUSES", "To Do, Done")
	h.checkout = second
	preview := h.invoke("--dry-run", "--json")
	if preview.Err != nil {
		t.Fatalf("merge preview: %v %s", preview.Err, preview.Stdout)
	}
	if !strings.Contains(string(preview.Stdout), `"sourceId":"project-2"`) || !strings.Contains(string(preview.Stdout), `"start"`) {
		t.Fatalf("missing suffix/unmapped: %s", preview.Stdout)
	}
	result := h.invoke("--me", "@someone-else")
	if result.Err != nil {
		t.Fatalf("merge apply: %v %s", result.Err, result.Stdout)
	}
	data, err := os.ReadFile(h.configPath)
	if err != nil || !strings.Contains(string(data), "# retained") || !strings.Contains(string(data), "# retained view") || !strings.Contains(string(data), "name: terminal") || !strings.Contains(string(data), "@someone-else") {
		t.Fatalf("merge lost comments/launch: %s %v", data, err)
	}
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil || len(cfg.Sources) != 2 || len(cfg.Views[0].Sources) != 2 || cfg.Sources[1].Workflow["start"] != "" {
		t.Fatalf("merge: %+v %v", cfg, err)
	}
	dryAdd := h.invoke("--me", "@another", "--dry-run", "--json")
	if dryAdd.Err != nil || !strings.Contains(string(dryAdd.Stdout), `"applied":false`) || !strings.Contains(string(dryAdd.Stdout), "init --checkout") {
		t.Fatalf("append preview command: %v %s", dryAdd.Err, dryAdd.Stdout)
	}
	added := h.invoke("--me", "@another", "--json")
	if added.Err != nil || !strings.Contains(string(added.Stdout), `"outcome":"merged"`) {
		t.Fatalf("append principal: %v %s", added.Err, added.Stdout)
	}
	before, _ := os.ReadFile(h.configPath)
	again := h.invoke("--me", "@another", "--json")
	after, _ := os.ReadFile(h.configPath)
	if again.Err != nil || !strings.Contains(string(again.Stdout), `"outcome":"unchanged"`) || string(before) != string(after) {
		t.Fatalf("existing principal changed config: %v %s", again.Err, again.Stdout)
	}
}

func TestQueueInitNewReadyViewAfterAnotherDefault(t *testing.T) {
	h := newInitHarness(t)
	initial := fmt.Sprintf("version: 1\nme:\n  backlog-md: ['@tester']\nsources:\n  - id: project\n    adapter: backlog-md\n    checkout: %s\nviews:\n  - name: Team\n    authority: local\n    sources: [project]\n    filter: {assigned: [me]}\n", h.checkout)
	if err := os.MkdirAll(filepath.Dir(h.configPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.configPath, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(t.TempDir(), "second")
	if out, err := testkit.GitCommand("init", "-b", "main", second).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(second, "backlog.config.yml"), []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h.checkout = second
	result := h.invoke("--json")
	if result.Err != nil || !strings.Contains(string(result.Stdout), `"worklease queue --view Ready"`) {
		t.Fatalf("new view hidden by first view: %v %s", result.Err, result.Stdout)
	}
}

func TestQueueInitDryRunSuggestsOnlyNonDefaultFlags(t *testing.T) {
	h := newInitHarness(t)
	preview := h.invoke("--adapter", "backlog-md", "--source-id", "project", "--me", "@tester", "--dry-run", "--json")
	if preview.Err != nil {
		t.Fatalf("dry-run: %v %s", preview.Err, preview.Stdout)
	}
	var envelope queueInitResult
	if err := json.Unmarshal(preview.Stdout, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.NextCommands) != 1 || strings.Contains(envelope.NextCommands[0], "--adapter") || strings.Contains(envelope.NextCommands[0], "--source-id") || strings.Contains(envelope.NextCommands[0], "--me") {
		t.Fatalf("redundant flags: %v", envelope.NextCommands)
	}
	different := h.invoke("--source-id", "custom", "--dry-run", "--json")
	if different.Err != nil || !strings.Contains(string(different.Stdout), "--source-id custom") {
		t.Fatalf("custom source ID lost: %v %s", different.Err, different.Stdout)
	}
}

func TestQueueInitPortableRequiresConfirmation(t *testing.T) {
	h := newInitHarness(t)
	portable := h.invoke("--me", "@one", "--portable-claims", "shared/project", "--json")
	if portable.Err != nil || !strings.Contains(string(portable.Stdout), `"identity":"confirmation-required"`) || !strings.Contains(string(portable.Stdout), "worklease queue --view Ready identity confirm") {
		t.Fatalf("portable: %v %s", portable.Err, portable.Stdout)
	}
	ids, err := config.LoadQueueIdentities(os.Getenv)
	if err != nil || len(ids.Sources) != 0 {
		t.Fatalf("portable confirmed automatically: %+v %v", ids, err)
	}
}

func TestQueueInitGitHubDetectionAndAuth(t *testing.T) {
	h := newInitHarness(t)
	if out, err := testkit.GitCommand("-C", h.checkout, "remote", "add", "origin", "git@GitHub.com:Owner/Repo.git").CombinedOutput(); err != nil {
		t.Fatalf("remote: %v %s", err, out)
	}
	if err := os.Remove(filepath.Join(h.checkout, "backlog.config.yml")); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "graphql") {
			_, _ = io.WriteString(w, `{"data":{"viewer":{"login":"tester"},"repository":{"id":"repo-id","nameWithOwner":"Owner/Repo"}}}`)
		}
	}))
	defer server.Close()
	queueInitGitHubAdapter = func() *queue.GitHubAdapter {
		adapter := queue.NewGitHubAdapter()
		adapter.APIBase = server.URL + "/graphql"
		return adapter
	}
	t.Cleanup(func() { queueInitGitHubAdapter = queue.NewGitHubAdapter })
	result := h.invoke("--dry-run", "--json")
	if result.Err != nil || !strings.Contains(string(result.Stdout), `"value":"Owner/Repo"`) || !strings.Contains(string(result.Stdout), `"me"`) {
		t.Fatalf("github: %v %s", result.Err, result.Stdout)
	}
	mismatch := h.invoke("--me", "someone-else", "--dry-run", "--json")
	if mismatch.Err == nil || !strings.Contains(string(mismatch.Stdout), "conflicts with authenticated GitHub account") {
		t.Fatalf("different GitHub account: %v %s", mismatch.Err, mismatch.Stdout)
	}
	t.Setenv("INIT_GH_UNAUTH", "1")
	result = h.invoke("--json")
	if result.Err == nil || !strings.Contains(string(result.Stdout), "gh auth login --hostname github.com") {
		t.Fatalf("auth: %v %s", result.Err, result.Stdout)
	}
	if _, err := os.Stat(h.configPath); !os.IsNotExist(err) {
		t.Fatalf("auth refusal wrote: %v", err)
	}
}

func TestQueueInitInvalidConfigAndConfirmationFailure(t *testing.T) {
	h := newInitHarness(t)
	if err := os.MkdirAll(filepath.Dir(h.configPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.configPath, []byte("version: 99\nme: {}\nsources: []\nviews: []\n"), 0600); err != nil {
		t.Fatal(err)
	}
	refused := h.invoke("--json")
	if refused.Err == nil || !strings.Contains(string(refused.Stdout), "expected 1") {
		t.Fatalf("invalid config accepted: %v %s", refused.Err, refused.Stdout)
	}
	before, _ := os.ReadFile(h.configPath)
	if string(before) != "version: 99\nme: {}\nsources: []\nviews: []\n" {
		t.Fatal("invalid config changed")
	}
	if err := os.Remove(h.configPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INIT_LIST_ERROR", "1")
	failed := h.invoke("--json")
	if failed.Err == nil || !strings.Contains(string(failed.Stdout), `"applied":true`) || !strings.Contains(string(failed.Stdout), "identity confirm --source project --acknowledge") {
		t.Fatalf("confirmation failure did not preserve recovery command: %v %s", failed.Err, failed.Stdout)
	}
	if _, err := config.LoadQueue(os.Getenv); err != nil {
		t.Fatalf("configuration lost after confirmation failure: %v", err)
	}
}

func TestQueueInitRemoteAuthoritySkipsAutomaticConfirmation(t *testing.T) {
	h := newInitHarness(t)
	profile := testProfile("team")
	profile.Credential.Path = filepath.Join(t.TempDir(), "credential")
	if err := config.SaveProfiles(config.UserProfilePaths(nil), []config.Profile{profile}, ""); err != nil {
		t.Fatal(err)
	}
	result := h.invoke("--authority", "team", "--json")
	if result.Err != nil || !strings.Contains(string(result.Stdout), `"identity":"confirmation-required"`) {
		t.Fatalf("remote authority confirmation: %v %s", result.Err, result.Stdout)
	}
	ids, err := config.LoadQueueIdentities(os.Getenv)
	if err != nil || len(ids.Sources) != 0 {
		t.Fatalf("remote authority confirmed: %+v %v", ids, err)
	}
}

func TestQueueInitExistingIdentitySkipsAutomaticConfirmation(t *testing.T) {
	h := newInitHarness(t)
	if err := os.MkdirAll(filepath.Dir(h.configPath), 0700); err != nil {
		t.Fatal(err)
	}
	state := config.QueueIdentities{Version: 1, Sources: map[string]config.QueueIdentity{"project": {Locator: "previous"}}}
	if err := config.SaveQueueIdentities(os.Getenv, state); err != nil {
		t.Fatal(err)
	}
	result := h.invoke("--json")
	if result.Err != nil || !strings.Contains(string(result.Stdout), `"identity":"confirmation-required"`) {
		t.Fatalf("existing identity overwritten: %v %s", result.Err, result.Stdout)
	}
	kept, err := config.LoadQueueIdentities(os.Getenv)
	if err != nil || kept.Sources["project"].Locator != "previous" {
		t.Fatalf("existing identity lost: %+v %v", kept, err)
	}
}

func TestQueueInitConcurrentAddsPreserveBothSources(t *testing.T) {
	h := newInitHarness(t)
	second := filepath.Join(t.TempDir(), "second")
	if out, err := testkit.GitCommand("init", "-b", "main", second).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(second, "backlog.config.yml"), []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan string, 2)
	for _, checkout := range []string{h.checkout, second} {
		wg.Add(1)
		go func(checkout string) {
			defer wg.Done()
			var stdout, stderr bytes.Buffer
			args := []string{"worklease", "--home", os.Getenv("WORKLEASE_HOME"), "queue", "init", "--checkout", checkout, "--portable-claims", "shared/project"}
			if err := Run(context.Background(), args, "test", "unknown", "unknown", &stdout, &stderr); err != nil {
				failures <- err.Error() + ": " + stdout.String()
			}
		}(checkout)
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil || len(cfg.Sources) != 2 || len(cfg.Views[0].Sources) != 2 {
		t.Fatalf("lost concurrent source: %+v %v", cfg, err)
	}
}

func TestQueueInitIgnoresProviderEnvironmentOverrides(t *testing.T) {
	h := newInitHarness(t)
	t.Setenv("BACKLOG_CWD", filepath.Dir(h.checkout))
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "not-a-repository"))
	result := h.invoke("--json")
	if result.Err != nil || !strings.Contains(string(result.Stdout), "In Progress") {
		t.Fatalf("environment redirected detection: %v %s", result.Err, result.Stdout)
	}
}

func TestQueueInitRemovesColonFromDefaultSourceID(t *testing.T) {
	h := newInitHarness(t)
	withColon := filepath.Join(filepath.Dir(h.checkout), "proj:ect")
	if err := os.Rename(h.checkout, withColon); err != nil {
		t.Fatal(err)
	}
	h.checkout = withColon
	result := h.invoke("--json")
	if result.Err != nil || !strings.Contains(string(result.Stdout), `"sourceId":"project"`) {
		t.Fatalf("source ID: %v %s", result.Err, result.Stdout)
	}
}

func TestQueueInitPreflightBeforeWrite(t *testing.T) {
	for _, test := range []struct {
		name, env, value, args, failure string
		removeBinary                    string
	}{
		{"missing backlog", "", "", "", "cli-missing: install the backlog CLI", "backlog"},
		{"unsupported version", "INIT_VERSION", "1.51.0", "", "unsupported-version: found Backlog.md 1.51.0; required 1.52.x", ""},
		{"network consent", "INIT_NETWORK", "true", "", "git-network-consent: this project needs --allow-git-network", ""},
		{"missing gh", "", "", "--adapter github", "cli-missing: install the gh CLI", "gh"},
		{"unauthenticated gh", "INIT_GH_UNAUTH", "1", "--adapter github", "gh auth login --hostname github.com", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newInitHarness(t)
			if test.env != "" {
				t.Setenv(test.env, test.value)
			}
			if test.removeBinary != "" {
				gitBinary, err := exec.LookPath("git")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(gitBinary, filepath.Join(h.bin, "git")); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(h.bin, test.removeBinary)); err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", h.bin)
			}
			if test.args != "" {
				if out, err := testkit.GitCommand("-C", h.checkout, "remote", "add", "origin", "git@github.com:Owner/Repo.git").CombinedOutput(); err != nil {
					t.Fatalf("remote: %v %s", err, out)
				}
			}
			for _, mode := range []string{"--dry-run", "--json"} {
				args := []string{"--json", mode}
				if test.args != "" {
					args = append(args, strings.Fields(test.args)...)
					args = append(args, "--me", "tester")
				}
				result := h.invoke(args...)
				if result.Err == nil || !strings.Contains(string(result.Stdout), test.failure) {
					t.Fatalf("%s: expected %s: %v %s", mode, test.failure, result.Err, result.Stdout)
				}
				if _, err := os.Stat(filepath.Dir(h.configPath)); !os.IsNotExist(err) {
					t.Fatalf("preflight wrote directory: %v", err)
				}
			}
		})
	}
}

func TestQueueInitNetworkConsentAndBothDetected(t *testing.T) {
	h := newInitHarness(t)
	t.Setenv("INIT_NETWORK", "true")
	if out, err := testkit.GitCommand("-C", h.checkout, "remote", "add", "origin", "git@github.com:Owner/Repo.git").CombinedOutput(); err != nil {
		t.Fatalf("remote: %v %s", err, out)
	}
	result := h.invoke("--allow-git-network", "--dry-run", "--json")
	if result.Err != nil || !strings.Contains(string(result.Stdout), `"origin":"--allow-git-network"`) || !strings.Contains(string(result.Stdout), "allowGitNetwork: true") || !strings.Contains(string(result.Stdout), "--adapter github") || !strings.Contains(string(result.Stdout), "--checkout ") {
		t.Fatalf("consent and hint: %v %s", result.Err, result.Stdout)
	}
	result = h.invoke("--allow-git-network", "--json")
	if result.Err != nil || !strings.Contains(string(result.Stdout), "adapter: backlog-md") || !strings.Contains(string(result.Stdout), "--adapter github") {
		t.Fatalf("both detected: %v %s", result.Err, result.Stdout)
	}
}

func TestQueueInitMissingGitDoesNotWrite(t *testing.T) {
	h := newInitHarness(t)
	t.Setenv("PATH", h.bin)
	result := h.invoke("--dry-run", "--json")
	if result.Err == nil || !strings.Contains(string(result.Stdout), "git-missing: install Git") {
		t.Fatalf("missing Git: %v %s", result.Err, result.Stdout)
	}
	if _, err := os.Stat(filepath.Dir(h.configPath)); !os.IsNotExist(err) {
		t.Fatalf("missing Git wrote directory: %v", err)
	}
}

func TestQueueInitExternalManifestApprovalAndReadOnlyDefaults(t *testing.T) {
	_, paths := testkit.Home(t)
	for name, value := range paths {
		t.Setenv(name, value)
	}
	marker := filepath.Join(t.TempDir(), "started")
	executable := writeQueueAdapterApprovalExecutable(t, "schema", marker)
	invoke := func(args ...string) testkit.CLIResult {
		argv := append([]string{"worklease", "--home", os.Getenv("WORKLEASE_HOME"), "queue", "init", "--adapter", "external", "--executable", executable}, args...)
		return testkit.RunCLI(context.Background(), argv, func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
			return Run(ctx, args, "test", "unknown", "unknown", stdout, stderr)
		})
	}
	configPath := config.QueuePath(os.Getenv)
	preview := invoke("--adapter-config", `{"tenant":"acme"}`, "--dry-run", "--json")
	if preview.Err != nil || !strings.Contains(string(preview.Stdout), `"operation":"queue-init"`) || !strings.Contains(string(preview.Stdout), `"executableSHA256":"`+queueAdapterTestDigest(t, executable)+`"`) || !strings.Contains(string(preview.Stdout), "--adapter external --executable") {
		t.Fatalf("manifest preview: %v %s", preview.Err, preview.Stdout)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("preview wrote queue.yaml: %v", err)
	}
	applied := invoke("--adapter-config", `{"tenant":"acme"}`, "--json")
	if applied.Err != nil {
		t.Fatalf("apply: %v %s", applied.Err, applied.Stdout)
	}
	var response struct {
		queueInitResult
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
	}
	if err := json.Unmarshal(applied.Stdout, &response); err != nil || !response.OK || response.Operation != "queue-init" || response.Identity != "confirmation-required" || len(response.NextCommands) != 2 || response.NextCommands[0] != "worklease queue adapter approve --source example.adapter" || response.ExecutableSHA256 != queueAdapterTestDigest(t, executable) {
		t.Fatalf("result: %+v %v %s", response, err, applied.Stdout)
	}
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil || len(cfg.Sources) != 1 {
		t.Fatalf("load: %+v %v", cfg, err)
	}
	source := cfg.Sources[0]
	if source.ID != "example.adapter" || source.Executable != executable || source.ExpectedAdapterID != "example.adapter" || source.ExpectedVersion != "1.2.3" || source.Config["tenant"] != "acme" || source.Claims != nil || source.Workflow != nil || source.CredentialRef != "" || source.CredentialHelper != nil {
		t.Fatalf("unsafe source: %+v", source)
	}
	if err := config.CheckQueueAdapterApproval(os.Getenv, source); err == nil {
		t.Fatal("init approved adapter")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("manifest process was not started: %v", err)
	}
	before, _ := os.ReadFile(configPath)
	for _, args := range [][]string{{"--adapter-config", `{"tenant":"acme"}`}, {"--adapter-config", `{"tenant":"acme"}`, "--source-id", "example.adapter"}} {
		failed := invoke(append(args, "--json")...)
		if failed.Err == nil || !strings.Contains(string(failed.Stdout), `"reason":"source-already-configured"`) || !strings.Contains(string(failed.Stdout), `"exitCode":64`) {
			t.Fatalf("duplicate: %v %s", failed.Err, failed.Stdout)
		}
	}
	after, _ := os.ReadFile(configPath)
	if !bytes.Equal(before, after) {
		t.Fatal("failed external init modified queue.yaml")
	}
}

func TestQueueInitExternalFailureAndPortableClaims(t *testing.T) {
	_, paths := testkit.Home(t)
	for name, value := range paths {
		t.Setenv(name, value)
	}
	marker := filepath.Join(t.TempDir(), "started")
	executable := writeQueueAdapterApprovalExecutable(t, "schema", marker)
	t.Chdir(t.TempDir()) // External init must not require a repository or source tree.
	if entries, err := os.ReadDir("."); err != nil || len(entries) != 0 {
		t.Fatalf("not an empty working directory: %v %v", entries, err)
	}
	invoke := func(binary string, args ...string) testkit.CLIResult {
		argv := append([]string{"worklease", "--home", os.Getenv("WORKLEASE_HOME"), "queue", "init", "--adapter", "external", "--executable", binary}, args...)
		return testkit.RunCLI(context.Background(), argv, func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
			return Run(ctx, args, "test", "unknown", "unknown", stdout, stderr)
		})
	}
	configPath := config.QueuePath(os.Getenv)
	for _, test := range []struct {
		binary string
		args   []string
		reason string
	}{
		{executable, nil, "adapter-config-invalid"},
		{executable, []string{"--adapter-config", `{"tenant":123}`}, "adapter-config-invalid"},
		{executable, []string{"--adapter-config", `{"tenant":"acme","extra":true}`}, "adapter-config-invalid"},
		{writeQueueAdapterApprovalExecutable(t, "broken", filepath.Join(t.TempDir(), "broken")), nil, "adapter-manifest-invalid"},
	} {
		failed := invoke(test.binary, append(test.args, "--json")...)
		if failed.Err == nil || !strings.Contains(string(failed.Stdout), `"schemaVersion":2`) || !strings.Contains(string(failed.Stdout), `"operation":"queue-init"`) || !strings.Contains(string(failed.Stdout), `"reason":"`+test.reason+`"`) || !strings.Contains(string(failed.Stdout), `"exitCode":64`) {
			t.Fatalf("%s: %v %s", test.reason, failed.Err, failed.Stdout)
		}
		if _, err := os.Stat(configPath); !os.IsNotExist(err) {
			t.Fatalf("failure wrote queue.yaml: %v", err)
		}
	}
	configFile := filepath.Join(t.TempDir(), "adapter.json")
	if err := os.WriteFile(configFile, []byte(`{"tenant":"acme"}`), 0600); err != nil {
		t.Fatal(err)
	}
	preview := invoke(executable, "--adapter-config-file", configFile, "--dry-run", "--json")
	if preview.Err != nil || !strings.Contains(string(preview.Stdout), "--adapter-config-file") {
		t.Fatalf("file preview: %v %s", preview.Err, preview.Stdout)
	}
	result := invoke(executable, "--adapter-config-file", configFile, "--portable-claims", "shared/team", "--json")
	if result.Err != nil || !strings.Contains(string(result.Stdout), `"identity":"confirmation-required"`) {
		t.Fatalf("portable claim: %v %s", result.Err, result.Stdout)
	}
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil || cfg.Sources[0].Claims == nil || cfg.Sources[0].Claims.Source != "shared/team" || cfg.Sources[0].Claims.Policy != "generic" {
		t.Fatalf("portable binding: %+v %v", cfg, err)
	}
	command := func(args ...string) testkit.CLIResult {
		argv := append([]string{"worklease", "--home", os.Getenv("WORKLEASE_HOME"), "queue"}, args...)
		return testkit.RunCLI(context.Background(), argv, func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
			return Run(ctx, args, "test", "unknown", "unknown", stdout, stderr)
		})
	}
	approved := command("adapter", "approve", "--source", cfg.Sources[0].ID, "--acknowledge", "--json")
	if approved.Err != nil {
		t.Fatalf("approve: %v %s", approved.Err, approved.Stdout)
	}
	confirmed := command("--view", "Ready", "identity", "confirm", "--source", cfg.Sources[0].ID, "--acknowledge", "--json")
	if confirmed.Err != nil {
		t.Fatalf("external identity confirmation: %v %s", confirmed.Err, confirmed.Stdout)
	}
	identities, err := config.LoadQueueIdentities(os.Getenv)
	if err != nil || identities.Sources[cfg.Sources[0].ID].Policy != "generic" {
		t.Fatalf("external identity not saved: %+v %v", identities, err)
	}
}

func TestQueueInitBareBacklogFolderNotDetected(t *testing.T) {
	h := newInitHarness(t)
	if err := os.Remove(filepath.Join(h.checkout, "backlog.config.yml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(h.checkout, "backlog"), 0700); err != nil {
		t.Fatal(err)
	}
	result := h.invoke("--json")
	if result.Err == nil || !strings.Contains(string(result.Stdout), "cannot detect source") {
		t.Fatalf("bare folder: %v %s", result.Err, result.Stdout)
	}
}

func TestQueueInitInvalidInputsDoNotWrite(t *testing.T) {
	h := newInitHarness(t)
	for _, args := range [][]string{{"--authority", "missing"}, {"--source-id", "bad:id"}, {"--portable-claims", " "}} {
		result := h.invoke(args...)
		if result.Err == nil {
			t.Fatalf("accepted invalid input %v: %s", args, result.Stdout)
		}
		if _, err := os.Stat(h.configPath); !os.IsNotExist(err) {
			t.Fatalf("invalid input wrote config: %v", err)
		}
	}
}
