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
	key := "coordination:generic:" + strings.Repeat("a", 64)
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
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "--json", "--home", home, "runs", "--all"}, "", "", "", &out, &out); err != nil {
		t.Fatalf("runs: %v %s", err, out.String())
	}
	var listed struct {
		Runs []runs.Record `json:"runs"`
	}
	if err := json.Unmarshal(out.Bytes(), &listed); err != nil {
		t.Fatalf("runs: %v %s", err, out.String())
	}
	for _, got := range listed.Runs {
		if got.ID == record.ID {
			if got.Claim == nil || len(got.Claim.Resources) != 1 || got.Claim.Resources[0] != key {
				t.Fatalf("runs lost claim identity: %s", out.String())
			}
			return
		}
	}
	t.Fatalf("run %s missing from list: %s", record.ID, out.String())
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

// waitForRunState returns the ID of the named run once it reaches state.
func waitForRunState(t *testing.T, name, state string) string {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		records, _ := runStore().List()
		for _, r := range records {
			if r.Name == name && r.State == state {
				return r.ID
			}
		}
	}
	t.Fatalf("run %s never reached %s", name, state)
	return ""
}

func seedHolder(t *testing.T, home, key string) string {
	t.Helper()
	holder := filepath.Join(home, "handles", "holder.json")
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "--home", home, "acquire", "--resource", key, "--session", "other", "--handle", holder}, "", "", "", &out, &out); err != nil {
		t.Fatalf("seed: %v %s", err, out.String())
	}
	return holder
}

func TestRunWaitStartsWorkerAfterHolderReleasesOrTimesOut(t *testing.T) {
	t.Parallel()
	home, _ := testkit.Home(t)
	key := "coordination:generic:run-wait"
	holder := seedHolder(t, home, key)
	marker := filepath.Join(t.TempDir(), "started")
	if _, err := supervisedRun(t, home, "--resource", key, "--wait", "300ms", "--", "touch", marker); reason.As(err) == nil || reason.As(err).Reason != reason.ReasonWaitTimeout {
		t.Fatalf("expired wait: %v", err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("worker started after the wait expired")
	}
	name := "wait-" + randomHex(4)
	done := make(chan error, 1)
	go func() {
		_, err := supervisedRun(t, home, "--name", name, "--resource", key, "--wait", "1m", "--", "touch", marker)
		done <- err
	}()
	waitForRunState(t, name, runs.StateWaiting)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("worker started while the resource was held")
	}
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "--home", home, "release", "--handle", holder}, "", "", "", &out, &out); err != nil {
		t.Fatalf("release holder: %v %s", err, out.String())
	}
	if err := <-done; err != nil {
		t.Fatalf("waited run: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("worker did not start after the holder released")
	}
}

func TestRunStopWhileWaitingStartsNothingAndHoldsNoClaim(t *testing.T) {
	t.Parallel()
	home, _ := testkit.Home(t)
	key := "coordination:generic:run-wait-stop"
	holder := seedHolder(t, home, key)
	marker := filepath.Join(t.TempDir(), "started")
	name := "wait-stop-" + randomHex(4)
	type result struct {
		record runs.Record
		err    error
	}
	done := make(chan result, 1)
	go func() {
		record, err := supervisedRun(t, home, "--name", name, "--resource", key, "--wait", "1m", "--", "touch", marker)
		done <- result{record, err}
	}()
	id := waitForRunState(t, name, runs.StateWaiting)
	var out bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "runs", "stop", id}, "", "", "", &out, &out); err != nil {
		t.Fatalf("stop: %v %s", err, out.String())
	}
	got := <-done
	if classified := reason.As(got.err); classified == nil || classified.Reason != reason.ReasonCancelled {
		t.Fatalf("stopped wait: %v", got.err)
	}
	record, err := runStore().Read(id)
	if err != nil || record.State != runs.StateFailed || record.Reason != reason.ReasonCancelled || record.Claim != nil {
		t.Fatalf("stopped waiting record: %v %+v", err, record)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("worker started after the run was stopped")
	}
	out.Reset()
	if err := Run(context.Background(), []string{"worklease", "--home", home, "release", "--handle", holder}, "", "", "", &out, &out); err != nil {
		t.Fatalf("release holder: %v %s", err, out.String())
	}
	if state := resourceState(t, home, key); state != "free" {
		t.Fatalf("resource %s after the holder released; the stopped run kept a claim", state)
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
	id := waitForRunState(t, name, runs.StateRunning)
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

func TestRunWorkerCannotReacquireButMayReleaseEarly(t *testing.T) {
	t.Parallel()
	home, _ := testkit.Home(t)
	binary := filepath.Join(t.TempDir(), "worklease")
	if err := os.Symlink(os.Args[0], binary); err != nil {
		t.Fatal(err)
	}
	key := "coordination:generic:run-worker-release"
	refusal := filepath.Join(t.TempDir(), "refusal")
	// The worker keeps running past a renewal interval after releasing, which
	// must not be treated as a lost claim.
	worker := `! "$0" --json acquire --resource "$1" > "$2" && "$0" release --reason early && sleep 3`
	record, err := supervisedRun(t, home, "--ttl", "3s", "--resource", key, "--", "sh", "-c", worker, binary, key, refusal)
	if err != nil || record.Outcome != runs.OutcomeDone || record.Claim == nil || record.Claim.State != runs.ClaimReleased || record.Claim.Detail != "released by the worker" {
		t.Fatalf("run: %v %+v claim %+v", err, record, record.Claim)
	}
	if data, _ := os.ReadFile(refusal); !strings.Contains(string(data), "runs under worklease run "+record.ID) {
		t.Fatalf("acquire refusal: %s", data)
	}
}
