package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/testkit"
)

func queueStartFixture(t *testing.T) (queueStartController, queue.Item, string) {
	t.Helper()
	binary, err := exec.LookPath("backlog")
	if err != nil {
		t.Skip("backlog CLI unavailable")
	}
	if err := exec.Command(binary, "--version").Run(); err != nil {
		t.Skip("backlog CLI cannot run")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	root := t.TempDir()
	configText := "project_name: scratch\nstatuses: [To Do, In Progress, Done]\nbacklog_directory: docs/backlog\nremote_operations: false\ncheck_active_branches: false\nauto_commit: false\nbypass_git_hooks: false\n"
	if err := os.WriteFile(filepath.Join(root, "backlog.config.yml"), []byte(configText), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "initial"}} {
		cmd := testkit.GitCommand(args...)
		cmd.Dir = root
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git fixture: %v %s", err, data)
		}
	}
	create := exec.Command(binary, "task", "create", "Start target", "--no-dod-defaults")
	create.Dir = root
	if data, err := create.CombinedOutput(); err != nil {
		t.Fatalf("backlog fixture: %v %s", err, data)
	}
	item := queueClaimItem("tasks", "TASK-1")
	claim, backend, _ := newLocalQueueClaimController(t, config.DefaultTTL, item)
	registry := queue.NewRegistry()
	read, ok := registry.Get("backlog-md")
	if !ok {
		t.Fatal("backlog adapter unavailable")
	}
	read.(*queue.BacklogAdapter).TerminalStatuses["Done"] = true
	source, err := read.Resolve(context.Background(), map[string]string{"id": "tasks", "checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	claim.registry = registry
	claim.sources = map[string]queue.Source{source.ID: source}
	claim.claimSources = map[string]queue.ClaimSource{source.ID: {Source: source, Policy: "generic", ClaimSource: "portable"}}
	confirmed := config.QueueIdentity{Adapter: source.Adapter, Locator: source.Locator, Policy: "generic", Source: "portable", AuthorityID: backend.AuthorityID()}
	if err := config.SaveQueueIdentities(os.Getenv, config.QueueIdentities{Version: 1, Sources: map[string]config.QueueIdentity{source.ID: confirmed}}); err != nil {
		t.Fatal(err)
	}
	queueConfig := fmt.Sprintf("version: 1\nme:\n  backlog-md: ['@bob']\nsources:\n  - id: tasks\n    adapter: backlog-md\n    checkout: %q\n    claims:\n      policy: generic\n      source: portable\n    workflow:\n      start: In Progress\nviews:\n  - name: Ready\n    authority: local\n    sources: [tasks]\n    filter:\n      readiness: ready\n", root)
	if err := handle.WriteOwnerPrivate(config.QueuePath(os.Getenv), []byte(queueConfig), 1<<20); err != nil {
		t.Fatal(err)
	}
	journal, err := queueRecoveryJournal()
	if err != nil {
		t.Fatal(err)
	}
	configured := config.QueueSource{ID: source.ID, Adapter: source.Adapter, Checkout: root, Workflow: map[string]string{"start": "In Progress"}}
	write := queueWriteController{backend: backend, registry: registry, current: claim.current, journal: journal, sources: claim.sources, configured: map[string]config.QueueSource{source.ID: configured}, me: map[string][]string{source.ID: {"@bob"}}, session: claim.queueSession, profile: config.LocalProfileName}
	return queueStartController{claim: claim, write: write}, item, root
}

func TestQueueStartWorkRequiresAnExplicitProjectsWriteBinding(t *testing.T) {
	t.Parallel()
	item := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "issues", ItemID: "6"}}}
	claim := &queueClaimController{blocked: func() bool { return true }}
	configured := config.QueueSource{ID: "issues", Adapter: "github", Host: "github.com", Repository: "org/repo", Account: "tester", Workflow: map[string]string{"start": "option-progress"}, Project: &config.QueueProject{AllowWrites: true}}
	controller := queueStartController{claim: claim, write: queueWriteController{configured: map[string]config.QueueSource{"issues": configured}}}
	_, _, err := controller.prepare(context.Background(), item)
	if err == nil || !strings.Contains(err.Error(), "claim actions stopped") {
		t.Fatalf("explicit project mapping was rejected before claim preparation: %v", err)
	}
	configured.Project.AllowWrites = false
	controller.write.configured["issues"] = configured
	controller.claim = nil
	_, _, err = controller.prepare(context.Background(), item)
	if err == nil || !strings.Contains(err.Error(), "no supported provider mapping") {
		t.Fatalf("GitHub Start work was enabled without project.allowWrites: %v", err)
	}
	configured.Project = nil
	controller.write.configured["issues"] = configured
	_, _, err = controller.prepare(context.Background(), item)
	if err == nil || !strings.Contains(err.Error(), "no supported provider mapping") {
		t.Fatalf("unbound GitHub Start work was enabled: %v", err)
	}
}

func TestQueueStartWorkComposesClaimAndProviderTransition(t *testing.T) {
	if isolateCLIProcess(t) {
		return
	}
	ctx := context.Background()
	controller, item, root := queueStartFixture(t)
	preview := controller.Preview(ctx, item)().(queueui.StartPreviewMsg)
	if preview.Err != nil || preview.Preview == nil || preview.Preview.Actor != "@bob" || preview.Preview.Transition != "In Progress" || preview.Preview.RequiredFields != "status=In Progress" || len(preview.Preview.Claim.Resources) == 0 {
		t.Fatalf("start preview: %+v", preview)
	}
	result := controller.Start(ctx, item, *preview.Preview)().(queueui.StartResultMsg)
	if result.ClaimStep != "applied" || result.TransitionStep != "applied" || result.Write == nil || result.Write.Result.Outcome != queue.WriteVerified || result.Claim.HandlePath == "" {
		t.Fatalf("start outcome: %+v", result)
	}
	view := exec.Command("backlog", "task", "view", "TASK-1", "--json")
	view.Dir = root
	data, err := view.Output()
	if err != nil || !strings.Contains(string(data), `"status": "In Progress"`) {
		t.Fatalf("provider status unchanged: %v %s", err, data)
	}
	if strings.Contains(string(data), `"assignees": [\n    "@bob"`) {
		t.Fatal("Start work assigned the item")
	}
}

func TestQueueStartWorkRevalidatesBeforeClaimAndStopsOnCLIContention(t *testing.T) {
	if isolateCLIProcess(t) {
		return
	}
	ctx := context.Background()
	controller, item, _ := queueStartFixture(t)
	preview := controller.Preview(ctx, item)().(queueui.StartPreviewMsg)
	if preview.Err != nil {
		t.Fatal(preview.Err)
	}
	if _, err := runAcquireForQueueTest(controller.claim.backend.Config.Home, "generic", "portable", "TASK-1", false); err != nil {
		t.Fatal(err)
	}
	result := controller.Start(ctx, item, *preview.Preview)().(queueui.StartResultMsg)
	if result.ClaimStep != "rejected" || result.TransitionStep != "not attempted" || result.Write != nil {
		t.Fatalf("contention dispatched provider write: %+v", result)
	}
	status, err := controller.claim.backend.API.Status(ctx, lease.Selector{AuthorityID: controller.claim.backend.AuthorityID(), Resources: preview.Preview.Claim.Resources})
	if err != nil || len(status.Resources) != 1 || status.Resources[0].State != "active" {
		t.Fatalf("CLI claim not held: %+v %v", status, err)
	}
}

func TestQueueStartWorkRequiresSupportedMapping(t *testing.T) {
	controller, item, _ := queueStartFixture(t)
	cfg := controller.write.configured[item.Ref.SourceID]
	cfg.Workflow = nil
	controller.write.configured[item.Ref.SourceID] = cfg
	if preview := controller.Preview(context.Background(), item)().(queueui.StartPreviewMsg); preview.Err == nil || preview.Preview != nil {
		t.Fatalf("missing mapping invented Start work: %+v", preview)
	}
	cfg.Adapter = "github"
	cfg.Workflow = map[string]string{"start": "in-progress"}
	controller.write.configured[item.Ref.SourceID] = cfg
	github, ok := controller.claim.registry.Get("github")
	if !ok {
		t.Fatal("GitHub adapter unavailable")
	}
	githubSource := queue.Source{ID: item.Ref.SourceID, Adapter: "github", Locator: "https://github.com/example/project"}
	controller.claim.sources[item.Ref.SourceID] = githubSource
	controller.claim.claimSources[item.Ref.SourceID] = queue.ClaimSource{Source: githubSource}
	if err := queue.NewGitHubWriteAdapter(github.(*queue.GitHubAdapter), true).ValidateTransition(context.Background(), githubSource, queue.ActionStart, "in-progress"); err == nil {
		t.Fatal("GitHub Issues unexpectedly accepted a start transition")
	}
	if preview := controller.Preview(context.Background(), item)().(queueui.StartPreviewMsg); preview.Err == nil || preview.Preview != nil {
		t.Fatalf("GitHub issue mapping invented Start work: %+v", preview)
	}
}

func TestQueueStartWorkPreviewsCommitAndHookEffects(t *testing.T) {
	if isolateCLIProcess(t) {
		return
	}
	controller, item, root := queueStartFixture(t)
	path := filepath.Join(root, "backlog.config.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "auto_commit: false", "auto_commit: true", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	preview := controller.Preview(context.Background(), item)().(queueui.StartPreviewMsg)
	if preview.Err != nil || !strings.Contains(strings.Join(preview.Preview.SideEffects, "; "), "Git commit; Git hooks") {
		t.Fatalf("auto-commit/hook effects hidden: %+v", preview)
	}
}

func TestQueueStartWorkRevalidatesMappingBeforeClaim(t *testing.T) {
	if isolateCLIProcess(t) {
		return
	}
	ctx := context.Background()
	controller, item, _ := queueStartFixture(t)
	preview := controller.Preview(ctx, item)().(queueui.StartPreviewMsg)
	if preview.Err != nil {
		t.Fatal(preview.Err)
	}
	path := config.QueuePath(os.Getenv)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.WriteOwnerPrivate(path, []byte(strings.Replace(string(data), "source: portable", "source: moved", 1)), 1<<20); err != nil {
		t.Fatal(err)
	}
	result := controller.Start(ctx, item, *preview.Preview)().(queueui.StartResultMsg)
	if result.ClaimStep != "rejected" || result.TransitionStep != "not attempted" || result.Write != nil {
		t.Fatalf("changed claim binding acquired or wrote: %+v", result)
	}
	status, err := controller.claim.backend.API.Status(ctx, lease.Selector{AuthorityID: controller.claim.backend.AuthorityID(), Resources: preview.Preview.Claim.Resources})
	if err != nil || len(status.Resources) != 1 || status.Resources[0].State != "free" {
		t.Fatalf("revalidation held claim: %+v %v", status, err)
	}
}

func TestQueueStartWorkRevalidatesPrerequisitesBeforeClaim(t *testing.T) {
	if isolateCLIProcess(t) {
		return
	}
	ctx := context.Background()
	controller, item, root := queueStartFixture(t)
	for _, args := range [][]string{{"task", "create", "Prerequisite", "--no-dod-defaults"}, {"task", "edit", "TASK-2", "--status", "Done"}, {"task", "edit", "TASK-1", "--depends-on", "TASK-2"}} {
		command := exec.Command("backlog", args...)
		command.Dir = root
		if data, err := command.CombinedOutput(); err != nil {
			t.Fatalf("prepare dependency: %v %s", err, data)
		}
	}
	preview := controller.Preview(ctx, item)().(queueui.StartPreviewMsg)
	if preview.Err != nil {
		t.Fatal(preview.Err)
	}
	reopen := exec.Command("backlog", "task", "edit", "TASK-2", "--status", "To Do")
	reopen.Dir = root
	if data, err := reopen.CombinedOutput(); err != nil {
		t.Fatalf("reopen prerequisite: %v %s", err, data)
	}
	result := controller.Start(ctx, item, *preview.Preview)().(queueui.StartResultMsg)
	if result.ClaimStep != "rejected" || result.TransitionStep != "not attempted" || result.Write != nil {
		t.Fatalf("blocked prerequisite acquired or wrote: %+v", result)
	}
	status, err := controller.claim.backend.API.Status(ctx, lease.Selector{AuthorityID: controller.claim.backend.AuthorityID(), Resources: preview.Preview.Claim.Resources})
	if err != nil || len(status.Resources) != 1 || status.Resources[0].State != "free" {
		t.Fatalf("blocked claim held: %+v %v", status, err)
	}
}
