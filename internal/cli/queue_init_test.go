package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/testkit"
)

type initHarness struct {
	t                    *testing.T
	checkout, configPath string
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
	backlog := "#!/bin/sh\ncase \"$*\" in\n  'config get statuses') printf '%s\\n' \"${INIT_STATUSES:-To Do, In Progress, Done}\" ;;\n  'config get defaultStatus') printf '%s\\n' \"${INIT_DEFAULT_STATUS:-To Do}\" ;;\n  'config get defaultAssignee') printf '%s\\n' \"${INIT_ASSIGNEE:-@tester}\" ;;\n  'config get '*) printf 'false\\n' ;;\n  '--version') printf '1.52.0\\n' ;;\n  'task list --json') if [ \"${INIT_LIST_ERROR:-0}\" = 1 ]; then exit 1; fi; printf '%s\\n' \"$INIT_LIST_JSON\" ;;\n  *) exit 2 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "backlog"), []byte(backlog), 0700); err != nil {
		t.Fatal(err)
	}
	gh := "#!/bin/sh\nif [ \"$*\" = 'api --hostname github.com user --jq .login' ] && [ \"${INIT_GH_UNAUTH:-0}\" = 0 ]; then printf 'tester\\n'; else exit 1; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(gh), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("INIT_LIST_JSON", `{"kind":"task-list","schemaVersion":1,"tasks":[]}`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return initHarness{t, checkout, config.QueuePath(os.Getenv)}
}

func (h initHarness) invoke(args ...string) testkit.CLIResult {
	h.t.Helper()
	argv := append([]string{"worklease", "--home", os.Getenv("WORKLEASE_HOME"), "queue", "init", "--checkout", h.checkout}, args...)
	return testkit.RunCLI(context.Background(), argv, func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		return Run(ctx, args, "test", "unknown", "unknown", stdout, stderr)
	})
}

func TestQueueInitPreviewApplyAndIdempotence(t *testing.T) {
	h := newInitHarness(t)
	preview := h.invoke("--json")
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
	if payload.SchemaVersion != 2 || payload.Outcome != "created" || len(payload.Facts) < 5 || !strings.Contains(payload.YAML, "allowGitNetwork: false") {
		t.Fatalf("preview: %s", preview.Stdout)
	}
	if _, err := os.Stat(filepath.Dir(h.configPath)); !os.IsNotExist(err) {
		t.Fatalf("preview created directory: %v", err)
	}
	t.Setenv("INIT_LIST_JSON", `{"kind":"task-list","schemaVersion":1,"tasks":[{"id":"TASK-1","title":"Ready work","status":"To Do","ordinal":1,"isReady":true,"dependencies":[]}]}`)
	applied := h.invoke("--apply", "--json")
	if applied.Err != nil {
		t.Fatalf("apply: %v %s %s", applied.Err, applied.Stdout, applied.Stderr)
	}
	if err := json.Unmarshal(applied.Stdout, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Applied || payload.Identity != "confirmed" {
		t.Fatalf("apply: %s", applied.Stdout)
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
	again := h.invoke("--apply", "--json")
	if again.Err != nil || !strings.Contains(string(again.Stdout), `"outcome":"unchanged"`) {
		t.Fatalf("repeat: %v %s", again.Err, again.Stdout)
	}
	after, _ := os.ReadFile(h.configPath)
	if string(before) != string(after) {
		t.Fatal("idempotent apply changed file")
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
	preview := h.invoke("--json")
	if preview.Err != nil {
		t.Fatalf("merge preview: %v %s", preview.Err, preview.Stdout)
	}
	if !strings.Contains(string(preview.Stdout), `"sourceId":"project-2"`) || !strings.Contains(string(preview.Stdout), `"start"`) {
		t.Fatalf("missing suffix/unmapped: %s", preview.Stdout)
	}
	result := h.invoke("--apply")
	if result.Err != nil {
		t.Fatalf("merge apply: %v %s", result.Err, result.Stdout)
	}
	data, err := os.ReadFile(h.configPath)
	if err != nil || !strings.Contains(string(data), "# retained") || !strings.Contains(string(data), "# retained view") || !strings.Contains(string(data), "name: terminal") {
		t.Fatalf("merge lost comments/launch: %s %v", data, err)
	}
	cfg, err := config.LoadQueue(os.Getenv)
	if err != nil || len(cfg.Sources) != 2 || len(cfg.Views[0].Sources) != 2 || cfg.Sources[1].Workflow["start"] != "" {
		t.Fatalf("merge: %+v %v", cfg, err)
	}
}

func TestQueueInitRefusalsAndPortable(t *testing.T) {
	h := newInitHarness(t)
	t.Setenv("INIT_ASSIGNEE", "@one,@two")
	preview := h.invoke("--json")
	if preview.Err != nil || !strings.Contains(string(preview.Stdout), "me-required") {
		t.Fatalf("me preview: %v %s", preview.Err, preview.Stdout)
	}
	refused := h.invoke("--apply", "--json")
	if refused.Err == nil || !strings.Contains(string(refused.Stdout), `"reason":"me-required"`) {
		t.Fatalf("me refusal: %v %s", refused.Err, refused.Stdout)
	}
	if _, err := os.Stat(h.configPath); !os.IsNotExist(err) {
		t.Fatalf("refusal wrote file: %v", err)
	}
	portable := h.invoke("--me", "@one", "--portable-claims", "shared/project", "--apply", "--json")
	if portable.Err != nil || !strings.Contains(string(portable.Stdout), `"identity":"confirmation-required"`) {
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
	result := h.invoke("--json")
	if result.Err != nil || !strings.Contains(string(result.Stdout), "repository: Owner/Repo") || !strings.Contains(string(result.Stdout), "github.com: tester") {
		t.Fatalf("github: %v %s", result.Err, result.Stdout)
	}
	t.Setenv("INIT_GH_UNAUTH", "1")
	result = h.invoke("--apply", "--json")
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
	refused := h.invoke("--apply", "--json")
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
	failed := h.invoke("--apply", "--json")
	if failed.Err == nil || !strings.Contains(string(failed.Stdout), `"applied":true`) || !strings.Contains(string(failed.Stdout), "identity confirm --source 'project' --acknowledge") {
		t.Fatalf("confirmation failure did not preserve recovery command: %v %s", failed.Err, failed.Stdout)
	}
	if _, err := config.LoadQueue(os.Getenv); err != nil {
		t.Fatalf("configuration lost after confirmation failure: %v", err)
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
	result := h.invoke("--apply", "--json")
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
			args := []string{"worklease", "--home", os.Getenv("WORKLEASE_HOME"), "queue", "init", "--checkout", checkout, "--portable-claims", "shared/project", "--apply"}
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

func TestQueueInitInvalidInputsDoNotWrite(t *testing.T) {
	h := newInitHarness(t)
	for _, args := range [][]string{{"--authority", "missing", "--apply"}, {"--source-id", "bad:id", "--apply"}, {"--portable-claims", " ", "--apply"}} {
		result := h.invoke(args...)
		if result.Err == nil {
			t.Fatalf("accepted invalid input %v: %s", args, result.Stdout)
		}
		if _, err := os.Stat(h.configPath); !os.IsNotExist(err) {
			t.Fatalf("invalid input wrote config: %v", err)
		}
	}
}
