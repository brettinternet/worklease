package testkit

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestFakeBacklogMatchesRealCLI keeps FakeBacklog honest. Tests that use the
// fake rely on it reproducing every field Worklease reads, so this runs one
// scenario through both CLIs and compares those fields.
func TestFakeBacklogMatchesRealCLI(t *testing.T) {
	t.Parallel()
	RequireProviderTests(t)
	real, err := exec.LookPath("backlog")
	if err != nil {
		t.Skip("backlog CLI unavailable")
	}
	if out, err := exec.Command(real, "--version").Output(); err != nil || !strings.HasPrefix(string(out), "1.52.") {
		t.Skip("Backlog.md 1.52 required")
	}
	fake := filepath.Join(FakeBacklog(t), "backlog")
	want := backlogScenario(t, real)
	got := backlogScenario(t, fake)
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(want, "", "  ")
		t.Fatalf("fake Backlog.md diverged\nfake: %s\nreal: %s", gotJSON, wantJSON)
	}
}

func backlogScenario(t *testing.T, binary string) map[string]any {
	t.Helper()
	root := t.TempDir()
	config := "project_name: scratch\nstatuses: [To Do, In Progress, Done]\nbacklog_directory: docs/backlog\nremote_operations: false\ncheck_active_branches: false\nauto_commit: false\nbypass_git_hooks: true\n"
	if err := os.WriteFile(filepath.Join(root, "backlog.config.yml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := GitCommand("init", "-b", "main", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	run := func(args ...string) (string, bool) {
		cmd := exec.Command(binary, args...)
		cmd.Dir = root
		out, err := cmd.Output()
		return string(out), err == nil
	}
	must := func(args ...string) string {
		out, ok := run(args...)
		if !ok {
			t.Fatalf("%s %v failed", binary, args)
		}
		return out
	}
	result := map[string]any{}
	for _, key := range []string{"remoteOperations", "checkActiveBranches", "autoCommit", "bypassGitHooks", "statuses"} {
		result["config "+key] = must("config", "get", key)
	}
	result["version"] = strings.HasPrefix(must("--version"), "1.52.")
	must("task", "create", "First", "--assignee", "@alice", "--ac", "criterion", "--no-dod-defaults")
	must("task", "create", "Second", "--no-dod-defaults")
	result["list before edits"] = backlogFields(t, must("task", "list", "--json"), "task-list")
	must("task", "edit", "TASK-1", "--depends-on", "TASK-2")
	must("task", "edit", "TASK-1", "--append-notes", "evidence")
	must("task", "edit", "TASK-1", "--append-notes", "more\nworklease-op:1")
	must("task", "edit", "TASK-1", "--comment", "hello\nworklease-op:2", "--comment-author", "@bob")
	must("task", "edit", "TASK-1", "--comment", "anonymous")
	must("task", "edit", "TASK-1", "--assignee", "@alice,@bob", "--status", "in progress")
	_, result["unknown status accepted"] = run("task", "edit", "TASK-2", "--status", "Imaginary")
	_, result["missing task viewed"] = run("task", "view", "TASK-9", "--json")
	result["blocked view"] = backlogFields(t, must("task", "view", "TASK-1", "--json"), "task-view")
	result["blocked list"] = backlogFields(t, must("task", "list", "--json"), "task-list")
	must("task", "edit", "TASK-2", "--status", "Done")
	result["ready view"] = backlogFields(t, must("task", "view", "TASK-1", "--json"), "task-view")
	result["done view"] = backlogFields(t, must("task", "view", "TASK-2", "--json"), "task-view")
	result["ready list"] = backlogFields(t, must("task", "list", "--json"), "task-list")
	return result
}

// backlogFields keeps the fields Worklease reads and replaces values that
// legitimately differ between the CLIs (timestamps, file names) with their
// shape.
func backlogFields(t *testing.T, output, kind string) any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("%s: %v: %s", kind, err, output)
	}
	if payload["kind"] != kind || payload["schemaVersion"] != float64(1) {
		t.Fatalf("header: %s", output)
	}
	keep := func(task map[string]any) map[string]any {
		fields := map[string]any{}
		for _, key := range []string{"id", "title", "status", "priority", "ordinal", "assignees", "isReady", "parentTaskId", "dependencies", "description", "readiness", "implementationNotes", "acceptanceCriteria"} {
			if value, ok := task[key]; ok {
				fields[key] = value
			}
		}
		if readiness, ok := fields["readiness"].(map[string]any); ok {
			fields["readiness"] = map[string]any{"isReady": readiness["isReady"], "isBlocked": readiness["isBlocked"], "blockingDependencies": readiness["blockingDependencies"], "missingDependencies": readiness["missingDependencies"]}
		}
		if comments, ok := task["comments"].([]any); ok {
			kept := []any{}
			for _, comment := range comments {
				entry := comment.(map[string]any)
				kept = append(kept, map[string]any{"index": entry["index"], "body": entry["body"], "author": entry["author"]})
			}
			fields["comments"] = kept
		}
		fields["updatedAt set"] = task["updatedAt"] != nil
		if path, ok := task["path"].(string); ok {
			fields["path"] = strings.HasPrefix(path, "docs/backlog/tasks/")
		}
		return fields
	}
	if kind == "task-view" {
		return keep(payload["task"].(map[string]any))
	}
	var tasks []any
	for _, task := range payload["tasks"].([]any) {
		tasks = append(tasks, keep(task.(map[string]any)))
	}
	return tasks
}
