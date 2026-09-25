package testkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	appcli "github.com/brettinternet/worklease/internal/cli"
)

func TestClockAndGeneratorAreDeterministicAndRaceSafe(t *testing.T) {
	wall := time.Date(2026, 9, 12, 7, 0, 0, 0, time.UTC)
	clock := NewClock(wall)
	generator := NewGenerator(0)
	const workers = 32
	ids := make(chan string, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			clock.Advance(time.Millisecond)
			_ = clock.Now()
			_ = clock.Monotonic()
			ids <- generator.ID()
		}()
	}
	wait.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		if len(id) != 32 || seen[id] {
			t.Fatalf("invalid or duplicate ID %q", id)
		}
		seen[id] = true
	}
	if clock.Monotonic() != workers*time.Millisecond || !clock.Now().Equal(wall.Add(workers*time.Millisecond)) {
		t.Fatalf("clock = %v / %v", clock.Now(), clock.Monotonic())
	}
	clock.SetWall(wall)
	if !clock.Now().Equal(wall) || clock.Monotonic() != workers*time.Millisecond {
		t.Fatal("SetWall changed monotonic time")
	}
	if token := generator.Token(); len(token) != 64 || strings.Trim(token, "0123456789abcdef") != "" {
		t.Fatalf("invalid token %q", token)
	}
}

func TestHomeAndEnvironmentArePrivateAndProcessIsolated(t *testing.T) {
	before := os.Getenv("WORKLEASE_HOME")
	home, values := Home(t)
	info, err := os.Stat(home)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 || values["WORKLEASE_HOME"] != home || values["HOME"] == "" || values["XDG_CONFIG_HOME"] == "" || values["XDG_STATE_HOME"] == "" {
		t.Fatalf("home = %s mode=%o env=%v", home, info.Mode().Perm(), values)
	}
	child := Environment([]string{"PATH=/bin", "HOME=leak", "XDG_CONFIG_HOME=leak", "WORKLEASE_HOME=leak", "WORKLEASE_PROFILE=leak", "GIT_DIR=leak", "LANG=C"}, values)
	joined := strings.Join(child, "\n")
	for key, value := range values {
		if !strings.Contains(joined, key+"="+value) {
			t.Errorf("environment missing %s=%s: %v", key, value, child)
		}
	}
	for _, want := range []string{"PATH=/bin", "LANG=C"} {
		if !strings.Contains(joined, want) {
			t.Errorf("environment missing %q: %v", want, child)
		}
	}
	if strings.Contains(joined, "=leak") || os.Getenv("WORKLEASE_HOME") != before {
		t.Fatalf("environment leaked or mutated process: %v", child)
	}
}

func TestIsolateProcessEnvironmentRemovesHostileConfiguration(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "WORKLEASE_HOME", "WORKLEASE_CONFIG", "WORKLEASE_PROFILE", "WORKLEASE_SERVER_CONFIG", "WORKLEASE_TEST_HELPER", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"} {
		t.Setenv(key, filepath.Join(root, key))
	}
	restore, err := IsolateProcessEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME"} {
		value := os.Getenv(key)
		if value == "" || strings.HasPrefix(value, root) {
			t.Fatalf("%s was not isolated: %q", key, value)
		}
		info, statErr := os.Stat(value)
		if statErr != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("%s=%q is not a private temporary directory: %v", key, value, statErr)
		}
	}
	for _, key := range []string{"WORKLEASE_HOME", "WORKLEASE_CONFIG", "WORKLEASE_PROFILE", "WORKLEASE_SERVER_CONFIG", "WORKLEASE_TEST_HELPER", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"} {
		if value, ok := os.LookupEnv(key); ok {
			t.Fatalf("hostile %s survived isolation: %q", key, value)
		}
	}
}

func TestHelperEnvironmentIsPreservedOnlyForMatchingReexec(t *testing.T) {
	environment := []string{"WORKLEASE_TEST_HELPER=echo", "WORKLEASE_DRIVER_HELPER=crash"}
	if got := helperEnvironmentForInvocation(environment, []string{"test", "-test.run=^TestProcessHelper$"}); len(got) != 1 || got["WORKLEASE_TEST_HELPER"] != "echo" {
		t.Fatalf("matching helper environment = %v", got)
	}
	if got := helperEnvironmentForInvocation(environment, []string{"test", "-test.run=TestOrdinaryTest"}); len(got) != 0 {
		t.Fatalf("ordinary invocation preserved helper environment: %v", got)
	}
}

func TestRunCLIParsesInjectedVersionResult(t *testing.T) {
	runner := func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		return appcli.Run(ctx, args, "1.2.3", "abc", "now", stdout, stderr)
	}
	result := RunCLI(context.Background(), []string{"worklease", "version", "--json"}, runner)
	if result.Err != nil || len(result.Stderr) != 0 {
		t.Fatalf("CLI result: err=%v stderr=%q", result.Err, result.Stderr)
	}
	var payload struct {
		SchemaVersion int    `json:"schemaVersion"`
		Operation     string `json:"operation"`
		Version       string `json:"version"`
		Commit        string `json:"commit"`
	}
	if err := result.DecodeJSON(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.SchemaVersion != 2 || payload.Operation != "version" || payload.Version != "1.2.3" || payload.Commit != "abc" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestRunIsolatedTestKeepsEnvironmentPrivate(t *testing.T) {
	before := os.Getenv("TESTKIT_LOCAL_VALUE")
	if RunIsolatedTest(t) {
		if os.Getenv("TESTKIT_LOCAL_VALUE") != before {
			t.Fatal("isolated test mutated parent environment")
		}
		return
	}
	if os.Getenv("TESTKIT_FORCE_SKIP") == "1" {
		t.Skip("requested child skip")
	}
	if path := os.Getenv("TESTKIT_SLOW_ISOLATED_TEST"); path != "" {
		if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			t.Fatal(err)
		}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		_, _ = listener.Accept() // Wait for the parent to enforce its process deadline.
	}
	t.Setenv("TESTKIT_LOCAL_VALUE", "child")
	if os.Getenv("TESTKIT_LOCAL_VALUE") != "child" {
		t.Fatal("isolated child did not run test body")
	}
	for _, scenario := range []string{"chosen", "other"} {
		t.Run(scenario, func(t *testing.T) {
			if path := os.Getenv("TESTKIT_SELECTION_RESULT"); path != "" {
				if err := os.WriteFile(filepath.Join(path, scenario), []byte(scenario), 0o600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestRunIsolatedTestPreservesSelectionAndSkip(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	selector := "^TestRunIsolatedTestKeepsEnvironmentPrivate$/^chosen$"
	selected, err := runTestBinary(ProcessResult{Helper: "subtest selection", ExitCode: -1}, 10*time.Second,
		[]string{"-test.run=" + selector, "-test.v"}, map[string]string{"TESTKIT_SELECTION_RESULT": root})
	if err != nil {
		t.Fatalf("selected test: %v %s", err, selected.Stdout)
	}
	if _, err := os.Stat(filepath.Join(root, "chosen")); err != nil {
		t.Fatalf("selected subtest did not run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "other")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unselected subtest ran: %v", err)
	}
	skipped, err := runTestBinary(ProcessResult{Helper: "child skip", ExitCode: -1}, 10*time.Second,
		[]string{"-test.run=^TestRunIsolatedTestKeepsEnvironmentPrivate$", "-test.v"}, map[string]string{"TESTKIT_FORCE_SKIP": "1"})
	if err != nil || !strings.Contains(string(skipped.Stdout), "--- SKIP: TestRunIsolatedTestKeepsEnvironmentPrivate (") {
		t.Fatalf("child skip not propagated: %v %s", err, skipped.Stdout)
	}
}

func TestRunIsolatedTestStopsBeforeParentDeadline(t *testing.T) {
	t.Parallel()
	pidFile := filepath.Join(t.TempDir(), "child-pid")
	result, err := runTestBinary(ProcessResult{Helper: "short parent deadline", ExitCode: -1}, 12*time.Second,
		[]string{"-test.run=^TestRunIsolatedTestKeepsEnvironmentPrivate$", "-test.timeout=14s", "-test.v"},
		map[string]string{"TESTKIT_SLOW_ISOLATED_TEST": pidFile})
	if err == nil || !strings.Contains(string(result.Stdout), "timed out after") {
		t.Fatalf("isolated child did not stop before parent deadline: %v %s", err, result.Stdout)
	}
	data, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	pid, parseErr := strconv.Atoi(string(data))
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	assertProcessGone(t, pid)
}

func TestRunTestProcessSuccessAndBoundedCleanup(t *testing.T) {
	result, err := RunTestProcess("echo", 10*time.Second, "one", "two")
	if err != nil {
		t.Fatal(err)
	}
	if result.Helper != "echo" || result.ExitCode != 0 || strings.TrimSpace(string(result.Stdout)) != "one two" || len(result.Stderr) != 0 {
		t.Fatalf("result = %+v", result)
	}

	spawned, err := RunTestProcess("spawn", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	childPID, parseErr := strconv.Atoi(strings.TrimSpace(string(spawned.Stdout)))
	if parseErr != nil {
		t.Fatalf("spawned child PID %q: %v", spawned.Stdout, parseErr)
	}
	assertProcessGone(t, childPID)

	failed, err := RunTestProcess("fail", 10*time.Second)
	if err == nil || failed.ExitCode == 0 || !strings.Contains(string(failed.Stdout), "intentional helper failure") {
		t.Fatalf("helper failure not propagated: result=%+v err=%v", failed, err)
	}

	timeout := 2 * time.Second
	result, err = RunTestProcess("hang", timeout)
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) || timeoutErr.Helper != "hang" || timeoutErr.Timeout != timeout {
		t.Fatalf("timeout error = %v", err)
	}
	if !strings.Contains(err.Error(), "hang") || !strings.Contains(err.Error(), timeout.String()) {
		t.Fatalf("timeout lacks diagnostics: %v", err)
	}
	if result.PID <= 0 || result.ExitCode == 0 {
		t.Fatalf("timeout result = %+v", result)
	}
	assertProcessGone(t, result.PID)
}

// TestProcessHelper is entered only through RunTestProcess.
func TestProcessHelper(t *testing.T) {
	switch os.Getenv(helperEnvironment) {
	case "":
		return
	case "echo":
		fmt.Println(strings.Join(argsAfterSeparator(os.Args), " "))
		os.Exit(0)
	case "spawn":
		child := exec.Command("sleep", "30")
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		fmt.Println(child.Process.Pid)
		os.Exit(0)
	case "fail":
		t.Fatal("intentional helper failure")
	case "hang":
		for {
			time.Sleep(time.Hour)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown helper marker")
		os.Exit(2)
	}
}

func assertProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("helper process %d survived cleanup: %v", pid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func argsAfterSeparator(args []string) []string {
	for index, arg := range args {
		if arg == "--" {
			return args[index+1:]
		}
	}
	return nil
}
