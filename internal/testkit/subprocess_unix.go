//go:build unix

package testkit

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const helperEnvironment = "WORKLEASE_TEST_HELPER"

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
	cmd := exec.Command(os.Args[0], commandArgs...)
	cmd.Env = Environment(os.Environ(), map[string]string{helperEnvironment: helper})
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		return result, fmt.Errorf("start test helper %q: %w", helper, err)
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
			return result, fmt.Errorf("test helper %q exited with status %d: %w", helper, result.ExitCode, err)
		}
		return result, nil
	case <-timer.C:
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Process.Kill()
		<-done
		result.Stdout, result.Stderr = stdout.Bytes(), stderr.Bytes()
		result.ExitCode = cmd.ProcessState.ExitCode()
		return result, &TimeoutError{Helper: helper, Timeout: timeout}
	}
}
