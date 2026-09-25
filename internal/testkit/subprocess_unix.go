//go:build unix

package testkit

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

const helperEnvironment = "WORKLEASE_TEST_HELPER"
const isolatedTestEnvironment = "TESTKIT_ISOLATED_TEST"

// ProcessResult contains bounded test-binary subprocess output and status.
type ProcessResult struct {
	Helper   string
	PID      int
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// TimeoutError identifies the helper and bound that forced cleanup.
type TimeoutError struct {
	Helper  string
	Timeout time.Duration
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("test helper %q timed out after %s", e.Helper, e.Timeout)
}

// RunTestProcess re-executes the current test binary with an explicit helper
// marker. On timeout it kills and reaps the helper's complete process group.
func RunTestProcess(helper string, timeout time.Duration, args ...string) (ProcessResult, error) {
	result := ProcessResult{Helper: helper, ExitCode: -1}
	if strings.TrimSpace(helper) == "" {
		return result, errors.New("test helper marker is required")
	}
	if timeout <= 0 {
		return result, fmt.Errorf("test helper %q timeout must be positive", helper)
	}
	commandArgs := append([]string{"-test.run=^TestProcessHelper$", "--"}, args...)
	return runTestBinary(result, timeout, commandArgs, map[string]string{helperEnvironment: helper})
}

// RunIsolatedTest runs one environment-mutating top-level test in a private
// test process. The parent can be scheduled in parallel without changing its
// process environment; the child runs the original test body, including its
// usual TestMain setup. A bound and process-group cleanup cover descendants.
// It returns true in the parent, which must return immediately.
func RunIsolatedTest(t *testing.T) bool {
	t.Helper()
	if os.Getenv(isolatedTestEnvironment) == t.Name() {
		return false
	}
	t.Parallel()
	name := t.Name()
	// Preserve Go's slash-separated subtest filter, but restrict the child to
	// exactly this top-level test instead of re-running other selected tests.
	selector := "^" + regexp.QuoteMeta(name) + "$"
	if selected := flag.Lookup("test.run"); selected != nil {
		if _, suffix, ok := strings.Cut(selected.Value.String(), "/"); ok {
			selector += "/" + suffix
		}
	}
	timeout := 8 * time.Minute
	if deadline, ok := t.Deadline(); ok {
		remaining := time.Until(deadline) - 10*time.Second
		if remaining <= 0 {
			t.Fatal("insufficient test timeout to clean up isolated process")
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	result, err := runTestBinary(ProcessResult{Helper: name, ExitCode: -1}, timeout,
		[]string{"-test.run=" + selector, "-test.v"},
		map[string]string{isolatedTestEnvironment: name})
	if err != nil {
		t.Fatalf("isolated test %s: %v\n%s%s", name, err, result.Stdout, result.Stderr)
	}
	if strings.Contains(string(result.Stdout), "--- SKIP: "+name+" (") {
		t.Skip("isolated child skipped: " + name)
	}
	return true
}

func runTestBinary(result ProcessResult, timeout time.Duration, commandArgs []string, overrides map[string]string) (ProcessResult, error) {
	cmd := exec.Command(os.Args[0], commandArgs...)
	cmd.Env = Environment(os.Environ(), overrides)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return result, fmt.Errorf("start test helper %q: %w", result.Helper, err)
	}
	result.PID = cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		// The helper may have exited after spawning descendants. Kill the process
		// group before returning so no helper-owned child escapes test cleanup.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		result.Stdout, result.Stderr = stdout.Bytes(), stderr.Bytes()
		result.ExitCode = cmd.ProcessState.ExitCode()
		if err != nil {
			return result, fmt.Errorf("test helper %q exited with status %d: %w", result.Helper, result.ExitCode, err)
		}
		return result, nil
	case <-timer.C:
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
		<-done
		result.Stdout, result.Stderr = stdout.Bytes(), stderr.Bytes()
		result.ExitCode = cmd.ProcessState.ExitCode()
		return result, &TimeoutError{Helper: result.Helper, Timeout: timeout}
	}
}
