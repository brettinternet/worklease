package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestQueueWriteControllerPreviewsAndVerifiesBacklogMutation(t *testing.T) {
	binary, err := exec.LookPath("backlog")
	if err != nil {
		t.Skip("backlog CLI unavailable")
	}
	if _, err := exec.Command(binary, "--version").Output(); err != nil {
		t.Skip("backlog CLI cannot run")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	claimItem := queueClaimItem("tasks", "TASK-1")
	claimController, backend, fixture := newLocalQueueClaimController(t, 30*time.Second, claimItem)
	claimPreview := claimController.Preview(context.Background(), claimItem)().(queueui.ClaimPreviewMsg)
	if claimPreview.Err != nil {
		t.Fatal(claimPreview.Err)
	}
	if result := claimController.AcquireClaim(context.Background(), claimItem, *claimPreview.Preview)().(queueui.ClaimResultMsg); result.Err != nil {
		t.Fatal(result.Err)
	}
	originalPath, err := queueClaimHandlePath(backend.Config.Home, claimController.queueSession, config.LocalProfileName, fixture.source, claimItem.Ref)
	if err != nil {
		t.Fatal(err)
	}
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
	create := exec.Command(binary, "task", "create", "Scratch write", "--assignee", "@alice", "--ac", "criterion", "--no-dod-defaults")
	create.Dir = root
	if data, err := create.CombinedOutput(); err != nil {
		t.Fatalf("backlog fixture: %v %s", err, data)
	}
	registry := queue.NewRegistry()
	read, ok := registry.Get("backlog-md")
	if !ok {
		t.Fatal("backlog adapter unavailable")
	}
	source, err := read.Resolve(context.Background(), map[string]string{"id": "tasks", "checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	original, err := handle.Read(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	ref := queue.Ref{SourceID: source.ID, ItemID: "TASK-1"}
	path, err := queueClaimHandlePath(backend.Config.Home, claimController.queueSession, config.LocalProfileName, source, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Write(path, original); err != nil {
		t.Fatal(err)
	}
	queueConfig := fmt.Sprintf("version: 1\nme:\n  backlog-md: ['@bob']\nsources:\n  - id: tasks\n    adapter: backlog-md\n    checkout: %q\n    claims:\n      policy: generic\n      source: portable\n    workflow:\n      start: In Progress\nviews:\n  - name: Ready\n    authority: local\n    sources: [tasks]\n    filter:\n      readiness: ready\n", root)
	if err := handle.WriteOwnerPrivate(config.QueuePath(os.Getenv), []byte(queueConfig), 1<<20); err != nil {
		t.Fatal(err)
	}
	confirmed := config.QueueIdentity{Adapter: source.Adapter, Locator: source.Locator, Policy: "generic", Source: "portable", AuthorityID: backend.AuthorityID()}
	if err := config.SaveQueueIdentities(os.Getenv, config.QueueIdentities{Version: 1, Sources: map[string]config.QueueIdentity{source.ID: confirmed}}); err != nil {
		t.Fatal(err)
	}
	journal, err := queueRecoveryJournal()
	if err != nil {
		t.Fatal(err)
	}
	current := func() (queue.ClaimAuthority, uint64) { return claimController.current() }
	configured := config.QueueSource{ID: "tasks", Adapter: "backlog-md", Checkout: root, Workflow: map[string]string{"start": "In Progress"}}
	controller := queueWriteController{backend: backend, registry: registry, current: current, journal: journal, sources: map[string]queue.Source{source.ID: source}, configured: map[string]config.QueueSource{source.ID: configured}, me: map[string][]string{source.ID: {"@bob"}}, session: claimController.queueSession, profile: config.LocalProfileName}
	item := queue.Item{Summary: queue.Summary{Ref: ref}}
	for _, tc := range []struct {
		action                   queue.Action
		transition, text, effect string
	}{{queue.ActionStart, "In Progress", "", "--status In Progress"}, {queue.ActionRecordProgress, "", "evidence", "--append-notes"}, {queue.ActionAssignToMe, "", "", "--assignee"}} {
		preview := controller.Preview(context.Background(), item, tc.action, tc.transition, tc.text)().(queueui.WritePreviewMsg)
		if preview.Err != nil {
			t.Fatalf("%s preview: %v", tc.action, preview.Err)
		}
		if !strings.Contains(preview.Preview.Effect, tc.effect) || preview.Preview.Intent.ClaimID != original.ClaimID || len(preview.Preview.Races) == 0 || len(preview.Preview.SideEffects) == 0 || preview.Preview.Intent.AuthorityID != backend.AuthorityID() {
			t.Fatalf("%s missing effect, claim, or races: %+v", tc.action, preview.Preview)
		}
	}
	if entries, err := journal.Recovery(); err != nil || len(entries) != 0 {
		t.Fatalf("preview dispatched: %+v %v", entries, err)
	}
	preview := controller.Preview(context.Background(), item, queue.ActionStart, "In Progress", "")().(queueui.WritePreviewMsg)
	if preview.Err != nil {
		t.Fatal(preview.Err)
	}
	result := controller.Confirm(context.Background(), *preview.Preview)().(queueui.WriteResultMsg)
	if result.Err != nil || result.Result.Outcome != queue.WriteVerified || !result.Result.ClaimHeld {
		t.Fatalf("confirmed state not verified and held: %+v", result)
	}
	oldDomain := controller.Preview(context.Background(), item, queue.ActionAssignToMe, "", "")().(queueui.WritePreviewMsg)
	if oldDomain.Err != nil {
		t.Fatal(oldDomain.Err)
	}
	changedConfig := strings.Replace(queueConfig, "source: portable", "source: different", 1)
	if err := handle.WriteOwnerPrivate(config.QueuePath(os.Getenv), []byte(changedConfig), 1<<20); err != nil {
		t.Fatal(err)
	}
	if attempted := controller.Confirm(context.Background(), *oldDomain.Preview)().(queueui.WriteResultMsg); attempted.Err == nil {
		t.Fatal("old-domain handle authorized provider write after unconfirmed policy change")
	}
	if err := handle.WriteOwnerPrivate(config.QueuePath(os.Getenv), []byte(queueConfig), 1<<20); err != nil {
		t.Fatal(err)
	}
	stale := controller.Preview(context.Background(), item, queue.ActionAssignToMe, "", "")().(queueui.WritePreviewMsg)
	if stale.Err != nil {
		t.Fatal(stale.Err)
	}
	external := exec.Command(binary, "task", "edit", "TASK-1", "--append-notes", "external change")
	external.Dir = root
	if data, err := external.CombinedOutput(); err != nil {
		t.Fatalf("external change: %v %s", err, data)
	}
	rejected := controller.Confirm(context.Background(), *stale.Preview)().(queueui.WriteResultMsg)
	if rejected.Err == nil || rejected.Result.Outcome == queue.WriteVerified {
		t.Fatalf("stale preview dispatched: %+v", rejected)
	}
	if entries, err := journal.Recovery(); err != nil || len(entries) != 0 {
		t.Fatalf("preview wrote without confirmation or stale intent dispatched: %+v %v", entries, err)
	}
}
