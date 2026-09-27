package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/runs"
	"github.com/brettinternet/worklease/internal/testkit"
	urfave "github.com/urfave/cli/v3"
)

// supervisedRun runs `worklease run` in the foreground and decodes its record.
func supervisedRun(t *testing.T, home string, args ...string) (runs.Record, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := Run(context.Background(), append([]string{"worklease", "--json", "--home", home, "run"}, args...), "", "", "", &stdout, &stderr)
	var envelope struct {
		Run runs.Record `json:"run"`
	}
	if decodeErr := json.Unmarshal(stdout.Bytes(), &envelope); decodeErr != nil && err == nil {
		t.Fatalf("run output: %v: %s %s", decodeErr, stdout.String(), stderr.String())
	}
	return envelope.Run, err
}

func resourceState(t *testing.T, home, key string) string {
	t.Helper()
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "--json", "--home", home, "status", "--resource", key}, "", "", "", &out, &out); err != nil {
		t.Fatalf("status: %v %s", err, out.String())
	}
	var status struct {
		Resources []struct {
			State string `json:"state"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(out.Bytes(), &status); err != nil || len(status.Resources) != 1 {
		t.Fatalf("status output: %v %s", err, out.String())
	}
	return status.Resources[0].State
}

func TestRunHoldsClaimForWorkerAndReleasesOnExit(t *testing.T) {
	t.Parallel()
	home, _ := testkit.Home(t)
	key := "coordination:generic:run-release"
	// The worker sees the run's claim session and reports a structured result.
	worker := `test -n "$WORKLEASE_SESSION_ID" && test -f "$WORKLEASE_HANDLE" && printf '{"outcome":"review","summary":"ok"}' > "$WORKLEASE_RUN_RESULT"`
	record, err := supervisedRun(t, home, "--resource", key, "--ref", "s:1", "--", "sh", "-c", worker)
	if err != nil {
		t.Fatalf("run: %v %+v", err, record)
	}
	if record.State != runs.StateExited || record.Outcome != runs.OutcomeReview || record.Result == nil || record.Result.Summary != "ok" || record.Ref != "s:1" {
		t.Fatalf("record: %+v", record)
	}
	if record.Claim == nil || record.Claim.State != runs.ClaimReleased || record.Claim.SessionID != "run-"+record.ID {
		t.Fatalf("claim: %+v", record.Claim)
	}
	if state := resourceState(t, home, key); state != "free" {
		t.Fatalf("resource %s after run", state)
	}
}

func TestRunPropagatesWorkerFailureAndReleases(t *testing.T) {
	t.Parallel()
	home, _ := testkit.Home(t)
	key := "coordination:generic:run-failure"
	record, err := supervisedRun(t, home, "--resource", key, "--", "sh", "-c", "exit 3")
	var coder urfave.ExitCoder
	if !errors.As(err, &coder) || coder.ExitCode() != 3 {
		t.Fatalf("exit status not propagated: %v", err)
	}
	if record.Outcome != runs.OutcomeFailed || record.ExitCode == nil || *record.ExitCode != 3 || record.Claim.State != runs.ClaimReleased {
		t.Fatalf("record: %+v claim %+v", record, record.Claim)
	}
	if state := resourceState(t, home, key); state != "free" {
		t.Fatalf("resource %s after failed run", state)
	}
}

func TestRunDoesNotStartWorkerWhenClaimConflicts(t *testing.T) {
	t.Parallel()
	home, _ := testkit.Home(t)
	key := "coordination:generic:run-conflict"
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "--home", home, "acquire", "--resource", key, "--session", "other"}, "", "", "", &out, &out); err != nil {
		t.Fatalf("seed: %v %s", err, out.String())
	}
	marker := filepath.Join(t.TempDir(), "started")
	_, err := supervisedRun(t, home, "--resource", key, "--", "touch", marker)
	if classified := reason.As(err); classified == nil || classified.Reason != reason.ReasonAlreadyClaimed {
		t.Fatalf("conflict: %v", err)
	}
	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("worker started without the claim")
	}
}

func TestRunStopsWorkerOnStopRequestAndReleases(t *testing.T) {
	t.Parallel()
	home, _ := testkit.Home(t)
	key := "coordination:generic:run-stop"
	name := "stop-" + randomHex(4)
	done := make(chan runs.Record, 1)
	go func() {
		record, _ := supervisedRun(t, home, "--name", name, "--resource", key, "--", "sleep", "60")
		done <- record
	}()
	var id string
	for deadline := time.Now().Add(30 * time.Second); id == ""; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("run did not start")
		}
		records, _ := runStore().List()
		for _, r := range records {
			if r.Name == name && r.State == runs.StateRunning {
				id = r.ID
			}
		}
	}
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "runs", "stop", id}, "", "", "", &out, &out); err != nil {
		t.Fatalf("stop: %v %s", err, out.String())
	}
	select {
	case record := <-done:
		if record.Outcome != runs.OutcomeStopped || record.Claim == nil || record.Claim.State != runs.ClaimReleased {
			t.Fatalf("stopped record: %+v claim %+v", record, record.Claim)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("run ignored the stop request")
	}
	if state := resourceState(t, home, key); state != "free" {
		t.Fatalf("resource %s after stop", state)
	}
}

func TestRunEndsBackgroundWorkerProcessesOnExit(t *testing.T) {
	t.Parallel()
	home, _ := testkit.Home(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	if _, err := supervisedRun(t, home, "--", "sh", "-c", `sleep 60 >/dev/null 2>&1 & echo $! > "$0"`, pidFile); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	// Once killed, the orphaned background process is reaped by init.
	for deadline := time.Now().Add(10 * time.Second); runs.Alive(pid); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("background process %d outlived the run", pid)
		}
	}
}

func TestRunClaimIgnoresInheritedHandle(t *testing.T) {
	home, _ := testkit.Home(t)
	stale := filepath.Join(t.TempDir(), "inherited-handle.json")
	t.Setenv("WORKLEASE_HANDLE", stale)
	key := "coordination:generic:run-inherited-handle"
	record, err := supervisedRun(t, home, "--resource", key, "--", "sh", "-c", `test "$WORKLEASE_HANDLE" != "$0"`, stale)
	if err != nil || record.Claim == nil || record.Claim.State != runs.ClaimReleased {
		t.Fatalf("run with inherited handle: %v %+v", err, record.Claim)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("run acquired through the inherited handle")
	}
}
