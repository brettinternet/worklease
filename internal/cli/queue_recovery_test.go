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
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/testkit"
)

// A new recovery process has no resolved source binding; it must resolve the
// journaled source from configuration before read-back.
func TestQueueRecoveryAdapterResolvesJournaledBacklogSource(t *testing.T) {
	binary, err := exec.LookPath("backlog")
	if err != nil {
		t.Skip("backlog CLI unavailable")
	}
	if _, err := exec.Command(binary, "--version").Output(); err != nil {
		t.Skip("backlog CLI cannot run")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	configText := "project_name: scratch\nstatuses: [To Do, In Progress, Done]\nbacklog_directory: docs/backlog\nremote_operations: false\ncheck_active_branches: false\nauto_commit: false\nbypass_git_hooks: false\n"
	if err := os.WriteFile(filepath.Join(root, "backlog.config.yml"), []byte(configText), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := testkit.GitCommand("init", "-b", "main")
	cmd.Dir = root
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git fixture: %v %s", err, data)
	}
	for _, args := range [][]string{{"task", "create", "Scratch write", "--no-dod-defaults"}, {"task", "edit", "TASK-1", "--status", "In Progress"}} {
		c := exec.Command(binary, args...)
		c.Dir = root
		if data, err := c.CombinedOutput(); err != nil {
			t.Fatalf("backlog fixture: %v %s", err, data)
		}
	}
	queueConfig := fmt.Sprintf("version: 1\nme:\n  backlog-md: ['@bob']\nsources:\n  - id: tasks\n    adapter: backlog-md\n    checkout: %q\n    workflow:\n      start: In Progress\nviews:\n  - name: Ready\n    authority: local\n    sources: [tasks]\n    filter:\n      readiness: ready\n", root)
	if err := handle.EnsureOwnerPrivateDir(filepath.Dir(config.QueuePath(os.Getenv))); err != nil {
		t.Fatal(err)
	}
	if err := handle.WriteOwnerPrivate(config.QueuePath(os.Getenv), []byte(queueConfig), 1<<20); err != nil {
		t.Fatal(err)
	}
	read, _ := queue.NewRegistry().Get("backlog-md")
	source, err := read.Resolve(context.Background(), map[string]string{"id": "tasks", "checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	source.ID, source.Adapter = "tasks", "backlog-md"
	intent := queue.WriteIntent{Source: source, Ref: queue.Ref{SourceID: "tasks", ItemID: "TASK-1"}, Principal: "@bob", Action: queue.ActionStart, Transition: "In Progress", Patch: map[string]string{"status": "In Progress"}}

	adapter, err := queueBuiltinRecoveryAdapter(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := adapter.ReadReceipt(context.Background(), intent, &queue.ProviderReceipt{SourceID: "tasks", ItemID: "TASK-1"})
	if err != nil || observed.Outcome != queue.WriteVerified || observed.Patch["status"] != "In Progress" {
		t.Fatalf("fresh recovery read-back = %+v, %v", observed, err)
	}

	drifted := intent
	drifted.Source.Locator = filepath.Join(root, "elsewhere")
	if _, err := queueBuiltinRecoveryAdapter(context.Background(), drifted); err == nil || !strings.Contains(err.Error(), "drifted") {
		t.Fatalf("drifted source accepted: %v", err)
	}
}
