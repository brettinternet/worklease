package queue

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/testkit"
)

func backlogWriteProject(t *testing.T, autoCommit, bypass bool) (*BacklogWriteAdapter, Source, WritePipeline, *writeFixture) {
	t.Helper()
	binary, err := exec.LookPath("backlog")
	if err != nil {
		t.Skip("Backlog.md CLI unavailable")
	}
	_, env := testkit.Home(t)
	root := t.TempDir()
	config := "project_name: scratch\nstatuses: [To Do, In Progress, Done]\nbacklog_directory: docs/backlog\nremote_operations: false\ncheck_active_branches: false\nauto_commit: "
	if autoCommit {
		config += "true"
	} else {
		config += "false"
	}
	config += "\nbypass_git_hooks: "
	if bypass {
		config += "true\n"
	} else {
		config += "false\n"
	}
	if err := os.WriteFile(filepath.Join(root, "backlog.config.yml"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Test"},
		{"config", "user.email", "test@example.invalid"},
		{"commit", "--allow-empty", "-m", "initial"},
	} {
		cmd := testkit.GitCommand(args...)
		cmd.Dir = root
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, data)
		}
	}
	run := func(args ...string) []byte {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "HOME="+env["HOME"], "XDG_CONFIG_HOME="+env["XDG_CONFIG_HOME"])
		data, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("backlog %v: %v %s", args, err, data)
		}
		return data
	}
	run("task", "create", "Scratch write", "--assignee", "@alice", "--ac", "criterion", "--no-dod-defaults")
	a := NewBacklogAdapter("Done")
	a.Binary = binary
	source, err := a.Resolve(context.Background(), map[string]string{"id": "scratch", "checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := NewWriteJournal(filepath.Join(env["XDG_STATE_HOME"], "worklease", "queue-recovery"), filepath.Join(env["HOME"], ".cache", "worklease", "queue"))
	if err != nil {
		t.Fatal(err)
	}
	claim := &writeFixture{}
	writer := &BacklogWriteAdapter{BacklogAdapter: a, Me: "@bob"}
	pipeline := WritePipeline{Adapter: writer, Claim: claim, Journal: journal, Workflow: map[string]string{"start": "In Progress"}}
	return writer, source, pipeline, claim
}

func backlogWriteIntent(t *testing.T, source Source, action Action, patch map[string]string, appendText string) WriteIntent {
	t.Helper()
	id, err := NewWriteOperationID()
	if err != nil {
		t.Fatal(err)
	}
	return WriteIntent{OperationID: id, Source: source, Ref: Ref{SourceID: source.ID, ItemID: "TASK-1"}, Principal: "@bob", Patch: patch, Append: appendText, Action: action, Transition: patch["status"], AuthorityID: "test-authority", ClaimID: "test-claim", ClaimRevision: 1, Resources: []string{"scratch-resource"}, CheckpointTTL: time.Minute, CheckpointNotAfter: time.Now().Add(time.Hour)}
}

func TestBacklogWritePipelineScratchProject(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		action     Action
		patch      map[string]string
		appendText string
	}{
		{"state", ActionStart, map[string]string{"status": "In Progress"}, ""},
		{"notes", ActionRecordProgress, map[string]string{"append": "notes"}, "progress"},
		{"comment", ActionRecordProgress, map[string]string{"append": "comment"}, "progress"},
		{"assignment", ActionAssignToMe, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			writer, source, pipeline, claim := backlogWriteProject(t, false, false)
			intent, preview, err := writer.Prepare(context.Background(), backlogWriteIntent(t, source, tc.action, tc.patch, tc.appendText))
			if err != nil || preview.CreatesCommit || preview.AssignmentRace != (tc.action == ActionAssignToMe) {
				t.Fatalf("prepare: %+v %v", preview, err)
			}
			result, err := pipeline.Start(context.Background(), intent)
			if err != nil || result.Outcome != WriteVerified || claim.checkpointCalls != 1 {
				t.Fatalf("write: %+v %v checkpoints=%d", result, err, claim.checkpointCalls)
			}
			task, err := writer.writeTask(context.Background(), intent)
			if err != nil {
				t.Fatal(err)
			}
			if tc.action == ActionAssignToMe && !slices.Equal(task.Assignees, []string{"@alice", "@bob"}) {
				t.Fatalf("lost assignee: %v", task.Assignees)
			}
			if tc.action == ActionStart && task.Status != "In Progress" {
				t.Fatal(task.Status)
			}
			if tc.action == ActionRecordProgress && !strings.Contains(task.ImplementationNotes+" "+taskCommentText(task), intent.Marker) {
				t.Fatal("missing append marker")
			}
		})
	}
}

func taskCommentText(task backlogWriteTask) string {
	var texts []string
	for _, comment := range task.Comments {
		texts = append(texts, comment.Body)
	}
	return strings.Join(texts, " ")
}

func TestAdapterConformanceBacklogStaleWriteAndCapabilityDenial(t *testing.T) {
	t.Parallel()
	writer, source, pipeline, claim := backlogWriteProject(t, false, false)
	intent, preview, err := writer.Prepare(context.Background(), backlogWriteIntent(t, source, ActionAssignToMe, nil, ""))
	if err != nil || !preview.AssignmentRace {
		t.Fatalf("preview %+v: %v", preview, err)
	}
	ctx := context.Background()
	// Another writer adds an assignee after our preview. Pre-dispatch rejects the stale set.
	cmd := exec.CommandContext(ctx, writer.binary(), "task", "edit", "TASK-1", "--assignee", "@alice,@charlie")
	cmd.Dir = source.Locator
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("concurrent edit: %v %s", err, data)
	}
	result, err := pipeline.Start(ctx, intent)
	if err == nil || !result.SourceUnchanged || claim.checkpointCalls != 0 {
		t.Fatalf("stale preview: %+v %v", result, err)
	}
	// Even if the assignee changes after a successful write, read-back is a conflict.
	intent, _, err = writer.Prepare(ctx, backlogWriteIntent(t, source, ActionAssignToMe, nil, ""))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := writer.Write(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	cmd = exec.CommandContext(ctx, writer.binary(), "task", "edit", "TASK-1", "--assignee", "@charlie")
	cmd.Dir = source.Locator
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("concurrent edit: %v %s", err, data)
	}
	observation, err := writer.ReadReceipt(ctx, intent, &receipt)
	if err != nil || observation.Outcome != WriteConflict {
		t.Fatalf("race: %+v %v", observation, err)
	}
	criterion := backlogWriteIntent(t, source, Action("check-criterion"), map[string]string{"check-ac": "1"}, "")
	if _, _, err := writer.Prepare(ctx, criterion); !diag(err, "unstable-criterion-target") {
		t.Fatalf("criterion permitted: %v", err)
	}
	if args, err := backlogEditArgs(criterion); !diag(err, "unstable-criterion-target") || len(args) != 0 {
		t.Fatalf("check-ac argv: %v %v", args, err)
	}
}

type lostBacklogResponse struct{ *BacklogWriteAdapter }

func (a lostBacklogResponse) Write(ctx context.Context, intent WriteIntent) (ProviderReceipt, error) {
	if _, err := a.BacklogWriteAdapter.Write(ctx, intent); err != nil {
		return ProviderReceipt{}, err
	}
	return ProviderReceipt{}, errors.New("response lost after provider write")
}

func TestBacklogWritePreviewRejectsHookPolicyDriftAndUnsafeAssignee(t *testing.T) {
	t.Parallel()
	writer, source, pipeline, _ := backlogWriteProject(t, true, true)
	ctx := context.Background()
	intent, preview, err := writer.Prepare(ctx, backlogWriteIntent(t, source, ActionRecordProgress, map[string]string{"append": "notes"}, "progress"))
	if err != nil || preview.RunsHooks {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	config := filepath.Join(source.Locator, "backlog.config.yml")
	content, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(strings.Replace(string(content), "bypass_git_hooks: true", "bypass_git_hooks: false", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := pipeline.Start(ctx, intent)
	if err == nil || !result.SourceUnchanged {
		t.Fatalf("hook drift dispatched: %+v %v", result, err)
	}
	task, err := writer.writeTask(ctx, intent)
	if err != nil || task.ImplementationNotes != "" {
		t.Fatalf("source changed: %+v %v", task, err)
	}
	writer.Me = "@bob,@mallory"
	if _, _, err := writer.Prepare(ctx, backlogWriteIntent(t, source, ActionAssignToMe, nil, "")); !diag(err, "invalid-intent") {
		t.Fatalf("unsafe assignee accepted: %v", err)
	}
	task, err = writer.writeTask(ctx, intent)
	if err != nil || !slices.Equal(task.Assignees, []string{"@alice"}) {
		t.Fatalf("assignees changed: %v %v", task.Assignees, err)
	}
}

func TestBacklogWriteAppendRecoveryWithFollowingWrites(t *testing.T) {
	t.Parallel()
	writer, source, _, _ := backlogWriteProject(t, false, false)
	ctx := context.Background()
	intent, _, err := writer.Prepare(ctx, backlogWriteIntent(t, source, ActionRecordProgress, map[string]string{"append": "notes"}, "progress"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(ctx, intent); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, writer.binary(), "task", "edit", "TASK-1", "--append-notes", "later")
	cmd.Dir = source.Locator
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("later note: %v %s", err, out)
	}
	observation, err := writer.ReadReceipt(ctx, intent, nil)
	if err != nil || checkWriteEvidence(intent, nil, observation) != WriteVerified {
		t.Fatalf("earlier append lost: %+v %v", observation, err)
	}
	// A different body with the same trailing marker cannot impersonate our comment.
	commentIntent, _, err := writer.Prepare(ctx, backlogWriteIntent(t, source, ActionRecordProgress, map[string]string{"append": "comment"}, "progress"))
	if err != nil {
		t.Fatal(err)
	}
	cmd = exec.CommandContext(ctx, writer.binary(), "task", "edit", "TASK-1", "--comment", "extra\nprogress\n"+commentIntent.Marker, "--comment-author", "@bob")
	cmd.Dir = source.Locator
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("other comment: %v %s", err, out)
	}
	observation, err = writer.ReadReceipt(ctx, commentIntent, nil)
	if err != nil || checkWriteEvidence(commentIntent, nil, observation) != WriteUnknown {
		t.Fatalf("modified comment verified: %+v %v", observation, err)
	}
}

func TestBacklogWriteCommitVerifiedAfterUnrelatedCommit(t *testing.T) {
	t.Parallel()
	writer, source, _, _ := backlogWriteProject(t, true, true)
	ctx := context.Background()
	intent, _, err := writer.Prepare(ctx, backlogWriteIntent(t, source, ActionRecordProgress, map[string]string{"append": "notes"}, "progress"))
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := writer.Write(ctx, intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source.Locator, "later.txt"), []byte("later"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "later.txt"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "later"}} {
		cmd := testkit.GitCommand(args...)
		cmd.Dir = source.Locator
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	observation, err := writer.ReadReceipt(ctx, intent, &receipt)
	if err != nil || checkWriteEvidence(intent, &receipt, observation) != WriteVerified {
		t.Fatalf("lost task commit: %+v %v", observation, err)
	}
}

func TestBacklogWriteLostAppendResponseRecoversWithoutRedispatch(t *testing.T) {
	t.Parallel()
	writer, source, pipeline, claim := backlogWriteProject(t, false, false)
	pipeline.Adapter = lostBacklogResponse{writer}
	intent, _, err := writer.Prepare(context.Background(), backlogWriteIntent(t, source, ActionRecordProgress, map[string]string{"append": "comment"}, "progress"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := pipeline.Start(context.Background(), intent)
	if err == nil || result.Outcome != WriteUnknown {
		t.Fatalf("lost response: %+v %v", result, err)
	}
	result, err = pipeline.Recover(context.Background(), intent.OperationID)
	if err != nil || result.Outcome != WriteVerified || claim.checkpointCalls != 1 {
		t.Fatalf("recovery: %+v %v checkpoints=%d", result, err, claim.checkpointCalls)
	}
	task, err := writer.writeTask(context.Background(), intent)
	if err != nil || len(task.Comments) != 1 {
		t.Fatalf("duplicated append: %+v %v", task.Comments, err)
	}
}

func TestBacklogWriteMarkerAndStatusVerification(t *testing.T) {
	t.Parallel()
	writer, source, _, _ := backlogWriteProject(t, false, false)
	ctx := context.Background()
	bad := backlogWriteIntent(t, source, ActionStart, map[string]string{"status": "Imaginary"}, "")
	if _, _, err := writer.Prepare(ctx, bad); !diag(err, "invalid-status") {
		t.Fatalf("unknown status accepted: %v", err)
	}
	intent, _, err := writer.Prepare(ctx, backlogWriteIntent(t, source, ActionRecordProgress, map[string]string{"append": "notes"}, "progress"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(ctx, intent); err != nil {
		t.Fatal(err)
	}
	// Duplicating the marker on this item makes the result ambiguous, even
	// when one exact append is still visible.
	cmd := exec.CommandContext(ctx, writer.binary(), "task", "edit", "TASK-1", "--comment", "copied "+intent.Marker)
	cmd.Dir = source.Locator
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("copy marker: %v %s", err, out)
	}
	observation, err := writer.ReadReceipt(ctx, intent, nil)
	if err != nil || observation.MarkerCount != 2 || checkWriteEvidence(intent, nil, observation) != WriteUnknown {
		t.Fatalf("duplicate marker verified: %+v %v", observation, err)
	}
}

func TestBacklogWriteAutoCommitPreviewAndReceipt(t *testing.T) {
	t.Parallel()
	for _, bypass := range []bool{false, true} {
		t.Run(map[bool]string{false: "hooks", true: "bypass"}[bypass], func(t *testing.T) {
			t.Parallel()
			writer, source, pipeline, _ := backlogWriteProject(t, true, bypass)
			unrelated := filepath.Join(source.Locator, "unrelated.txt")
			if err := os.WriteFile(unrelated, []byte("user staged work"), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := testkit.GitCommand("add", "unrelated.txt")
			cmd.Dir = source.Locator
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("stage: %v %s", err, out)
			}
			hook := filepath.Join(source.Locator, ".git", "hooks", "pre-commit")
			if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf ran > hook-ran\n"), 0700); err != nil {
				t.Fatal(err)
			}
			intent, preview, err := writer.Prepare(context.Background(), backlogWriteIntent(t, source, ActionRecordProgress, map[string]string{"append": "notes"}, "progress"))
			if err != nil || !preview.CreatesCommit || preview.RunsHooks != !bypass || len(intent.Effects) != 1 {
				t.Fatalf("preview: %+v %+v %v", preview, intent.Effects, err)
			}
			result, err := pipeline.Start(context.Background(), intent)
			if err != nil || result.Outcome != WriteVerified {
				t.Fatalf("auto commit: %+v %v", result, err)
			}
			cmd = testkit.GitCommand("diff", "--cached", "--name-only")
			cmd.Dir = source.Locator
			staged, err := cmd.CombinedOutput()
			if err != nil || strings.TrimSpace(string(staged)) != "unrelated.txt" {
				t.Fatalf("staged work included in auto commit: %q %v", staged, err)
			}
			_, hookErr := os.Stat(filepath.Join(source.Locator, "hook-ran"))
			if (hookErr == nil) != !bypass {
				t.Fatalf("hook effect bypass=%v err=%v", bypass, hookErr)
			}
		})
	}
}
