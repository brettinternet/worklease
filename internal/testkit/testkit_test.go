package testkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	if info.Mode().Perm() != 0o700 || values["WORKLEASE_HOME"] != home {
		t.Fatalf("home = %s mode=%o env=%v", home, info.Mode().Perm(), values)
	}
	child := Environment([]string{"PATH=/bin", "WORKLEASE_HOME=leak", "GIT_DIR=leak", "LANG=C"}, map[string]string{"WORKLEASE_HOME": home, "GIT_CONFIG_NOSYSTEM": "1"})
	joined := strings.Join(child, "\n")
	for _, want := range []string{"PATH=/bin", "LANG=C", "WORKLEASE_HOME=" + home, "GIT_CONFIG_NOSYSTEM=1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("environment missing %q: %v", want, child)
		}
	}
	if strings.Contains(joined, "=leak") || os.Getenv("WORKLEASE_HOME") != before {
		t.Fatalf("environment leaked or mutated process: %v", child)
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

func TestRunTestProcessSuccessAndBoundedCleanup(t *testing.T) {
	result, err := RunTestProcess("echo", time.Second, "one", "two")
	if err != nil {
		t.Fatal(err)
	}
	if result.Helper != "echo" || result.ExitCode != 0 || strings.TrimSpace(string(result.Stdout)) != "one two" || len(result.Stderr) != 0 {
		t.Fatalf("result = %+v", result)
	}

	spawned, err := RunTestProcess("spawn", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	childPID, parseErr := strconv.Atoi(strings.TrimSpace(string(spawned.Stdout)))
	if parseErr != nil {
		t.Fatalf("spawned child PID %q: %v", spawned.Stdout, parseErr)
	}
	assertProcessGone(t, childPID)

	timeout := 250 * time.Millisecond
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
