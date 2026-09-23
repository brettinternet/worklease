package queue

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBacklogCancellationKillsProcessGroup(t *testing.T) {
	root := t.TempDir()
	pidFile := filepath.Join(root, "child.pid")
	binary := filepath.Join(root, "backlog")
	script := "#!/bin/sh\nsleep 30 &\necho $! > " + pidFile + "\nwait\n"
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	a := NewBacklogAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		_, _ = a.runCommand(ctx, root, binary, "--version")
		close(done)
	}()
	var pidBytes []byte
	for deadline := time.Now().Add(10 * time.Second); len(strings.TrimSpace(string(pidBytes))) == 0; {
		if time.Now().After(deadline) {
			t.Fatal("fake backlog never started its child")
		}
		time.Sleep(10 * time.Millisecond)
		pidBytes, _ = os.ReadFile(pidFile)
	}
	cancel()
	<-done
	pid := strings.TrimSpace(string(pidBytes))
	for range 50 {
		if err := exec.Command("kill", "-0", pid).Run(); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("child process %s survived cancellation", pid)
}

func TestBacklogGitStatusDisablesFsmonitor(t *testing.T) {
	root, binary := fakeBacklog(t)
	for _, args := range [][]string{{"init"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "seed"}} {
		cmd := scratchGit(root, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, out, err)
		}
	}
	marker := filepath.Join(root, "fsmonitor-ran")
	hook := filepath.Join(root, "fsmonitor-hook")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho ran > "+marker+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := scratchGit(root, "config", "core.fsmonitor", hook)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("configure hook: %s: %v", out, err)
	}
	a := NewBacklogAdapter()
	a.Binary = binary
	source, err := a.Resolve(context.Background(), map[string]string{"checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("fsmonitor hook ran: %v", err)
	}
	_ = source
}

func TestBacklogDependencyFallbackDoesNotCacheView(t *testing.T) {
	root, binary := fakeBacklog(t)
	a := NewBacklogAdapter()
	a.Binary = binary
	source := Source{ID: "fixture", Locator: root}
	a.consent[source.ID] = true
	ref := Ref{SourceID: source.ID, ItemID: "TASK-2"}
	first, err := a.ReadDependencies(context.Background(), source, ref, "", 0)
	if err != nil || len(first.Edges) != 3 {
		t.Fatalf("first dependencies: %+v %v", first, err)
	}
	viewFile := filepath.Join(root, "backlog-view.json")
	data, err := os.ReadFile(viewFile)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), `"dependencies":["TASK-1","TASK-404"]`, `"dependencies":["TASK-3","TASK-404"]`, 1)
	if err := os.WriteFile(viewFile, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := a.ReadDependencies(context.Background(), source, ref, "", 0)
	if err != nil || len(second.Edges) != 3 || second.Edges[0].To.ItemID != "TASK-3" {
		t.Fatalf("fallback consumed cached view: %+v %v", second, err)
	}
}

func TestBacklogMissingReadinessHasUnknownCoverage(t *testing.T) {
	data := []byte(`{"kind":"task-view","schemaVersion":1,"task":{"id":"TASK-2","dependencies":[]}}`)
	var payload struct {
		Task *backlogTask `json:"task"`
	}
	if err := decodeBacklog(data, "task-view", &payload); err != nil {
		t.Fatal(err)
	}
	a := NewBacklogAdapter()
	a.details["fixture\x00TASK-2"] = *payload.Task
	source := Source{ID: "fixture", Locator: t.TempDir()}
	a.consent[source.ID] = true
	page, err := a.ReadDependencies(context.Background(), source, Ref{SourceID: "fixture", ItemID: "TASK-2"}, "", 0)
	if err != nil || page.Completeness != CoveragePartial {
		t.Fatalf("missing readiness reported complete: %+v %v", page, err)
	}
}
