package queue

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestExternalProcessHelper(t *testing.T) {
	args := os.Args
	separator := -1
	for i, arg := range args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(args) {
		return
	}
	mode := args[separator+1]
	marker := ""
	if separator+2 < len(args) {
		marker = args[separator+2]
	}
	runExternalProcessHelper(mode, marker)
}

// TestProcessHelper supplies bounded process fixtures to testkit.RunTestProcess.
func TestProcessHelper(t *testing.T) {
	switch os.Getenv("WORKLEASE_TEST_HELPER") {
	case "external-crash":
		os.Exit(23)
	case "external-hang":
		blockExternalProcess()
	}
}

func TestRetryBusyExecutableOnlyBeforeDispatch(t *testing.T) {
	t.Parallel()
	attempts := 0
	err := retryBusyExecutable(func() error {
		attempts++
		if attempts < 3 {
			return syscall.ETXTBSY
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("busy startup was not retried: attempts=%d err=%v", attempts, err)
	}
	attempts = 0
	err = retryBusyExecutable(func() error {
		attempts++
		return syscall.EACCES
	})
	if !errors.Is(err, syscall.EACCES) || attempts != 1 {
		t.Fatalf("unrelated startup error was retried: attempts=%d err=%v", attempts, err)
	}
	attempts = 0
	err = retryBusyExecutable(func() error {
		attempts++
		return syscall.ETXTBSY
	})
	if !errors.Is(err, syscall.ETXTBSY) || attempts != 4 {
		t.Fatalf("persistent busy startup exceeded bound: attempts=%d err=%v", attempts, err)
	}
}

func TestExternalProcessStartErrorReportsErrnoWithoutPath(t *testing.T) {
	t.Parallel()
	err := externalProcessStartError("exec", &os.PathError{Op: "fork/exec", Path: "provider-secret-canary", Err: syscall.EAGAIN})
	if got := err.Error(); !strings.Contains(got, fmt.Sprintf("exec errno %d", syscall.EAGAIN)) || strings.Contains(got, "provider-secret-canary") {
		t.Fatalf("unsafe or missing startup diagnostic: %q", got)
	}
}

func TestRememberSecretDeduplicatesAndBoundsCredentialVariants(t *testing.T) {
	t.Parallel()
	process := &ExternalProcess{}
	for range 1000 {
		process.rememberSecret("same-token")
	}
	if got := len(process.secretValues()); got != 1 {
		t.Fatalf("repeated token retained %d variants, want 1 unique variant", got)
	}
	for i := range 1000 {
		process.rememberSecret(fmt.Sprintf("rotated-token-%d", i))
	}
	if got := len(process.secretValues()); got > 256 {
		t.Fatalf("retained %d credential variants, over limit 256", got)
	}
	if !strings.Contains(redactExternalText("rotated-token-999", process.secretValues()), "[redacted]") {
		t.Fatal("latest credential was not retained for redaction")
	}
}

func TestExternalResultScopeIgnoresOpaqueNestedSourceID(t *testing.T) {
	t.Parallel()
	result := json.RawMessage(`{"edges":[{"from":{"sourceId":"source-a","itemId":"1"},"to":{"sourceId":"source-a","itemId":"2"},"rawOutcome":{"sourceId":"provider-specific-id"}}]}`)
	if !externalResultSourceScoped(result, "readDependencies", "source-a") {
		t.Fatal("opaque nested sourceId was treated as a Worklease reference")
	}
	foreignReference := json.RawMessage(`{"edges":[{"from":{"sourceId":"other-source","itemId":"1"},"to":{"sourceId":"source-a","itemId":"2"}}]}`)
	if externalResultSourceScoped(foreignReference, "readDependencies", "source-a") {
		t.Fatal("foreign source-qualified reference was accepted")
	}
}

func TestExternalProcessNegotiatesAndScopesCalls(t *testing.T) {
	client, source, _ := newExternalProcessTestClient(t, "normal", "")
	manifest, err := client.Initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ID != source.ExpectedAdapterID || manifest.Version != source.ExpectedVersion || manifest.ResourcePolicy != "generic" {
		t.Fatalf("manifest = %+v", manifest)
	}
	if _, ok := client.Manifest(); !ok {
		t.Fatal("verified manifest was not retained")
	}
	var result map[string]any
	if err := client.Call(context.Background(), "list", map[string]any{"sourceId": source.ID}, &result); err != nil {
		t.Fatal(err)
	}
	if result["ok"] != true {
		t.Fatalf("result = %#v", result)
	}
	if err := client.Call(context.Background(), "readItem", map[string]any{"ref": map[string]any{"sourceId": "another-source", "itemId": "1"}}, &result); err == nil {
		t.Fatal("cross-source reference was accepted")
	}
	if err := client.Call(context.Background(), "list", map[string]any{"sourceId": source.ID, "credentialRef": source.CredentialRef}, &result); err == nil {
		t.Fatal("credentialRef was accepted outside resolve")
	}
}

func TestExternalProcessManifestRejectsUnexpectedBindingAndPolicy(t *testing.T) {
	for _, mode := range []string{"wrong-id", "wrong-version", "wrong-policy", "required-feature", "wrong-major"} {
		t.Run(mode, func(t *testing.T) {
			client, _, _ := newExternalProcessTestClient(t, mode, "")
			defer client.Close()
			if _, err := client.Initialize(context.Background()); err == nil {
				t.Fatalf("invalid %s manifest was accepted", mode)
			}
		})
	}
}

func TestExternalProcessCallDoesNotDispatchAcrossValidatedGeneration(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "methods")
	client, source, _ := newExternalProcessTestClient(t, "record-methods", marker)
	if _, err := client.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	generation, alive := client.Generation()
	if !alive {
		t.Fatal("initial process is not running")
	}
	client.mu.Lock()
	run := client.run
	client.mu.Unlock()
	client.failProcess(run, "simulated idle crash", true)
	client.mu.Lock()
	client.restartAt = time.Time{}
	client.mu.Unlock()
	params := map[string]any{
		"ref": Ref{SourceID: source.ID, ItemID: "item-1"}, "operationId": "operation-1",
		"patch": map[string]any{}, "authority": map[string]string{"authorizationRef": "operation-1", "scope": source.ID},
	}
	var result map[string]any
	if err := client.CallAtGeneration(context.Background(), generation, "writeState", params, &result); err == nil {
		t.Fatal("write was dispatched after the validated process generation changed")
	}
	methods, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(methods), "writeState") {
		t.Fatalf("write reached replacement process: %q", methods)
	}
}

func TestExternalProcessRateLimitRetryAtBlocksSourceUntilDeadline(t *testing.T) {
	t.Parallel()
	marker := filepath.Join(t.TempDir(), "list-calls")
	client, source, _ := newExternalProcessTestClient(t, "rate-limited-once", marker)
	clock := testkit.NewClock(time.Date(2099, time.January, 1, 0, 0, 0, 0, time.UTC))
	client.now = clock.Now
	params := map[string]any{"sourceId": source.ID}
	var result map[string]any
	if err := client.Call(context.Background(), "list", params, &result); err == nil || !strings.Contains(err.Error(), "rate-limited") {
		t.Fatalf("rate-limited response error = %v", err)
	}
	if err := client.Call(context.Background(), "list", params, &result); err == nil || !strings.Contains(err.Error(), "rate-limited") {
		t.Fatalf("call before retryAt was not refused: %v", err)
	}
	calls, err := os.ReadFile(marker)
	if err != nil || strings.Count(string(calls), "list\n") != 1 {
		t.Fatalf("host dispatched before retryAt: calls=%q err=%v", calls, err)
	}
	clock.Advance(366 * 24 * time.Hour)
	if err := client.Call(context.Background(), "list", params, &result); err != nil {
		t.Fatalf("call after retryAt failed: %v", err)
	}
	calls, err = os.ReadFile(marker)
	if err != nil || strings.Count(string(calls), "list\n") != 2 {
		t.Fatalf("expected one dispatch after retryAt: calls=%q err=%v", calls, err)
	}
}

func TestAdapterConformanceUncertainMutationAfterCrash(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started")
	client, source, _ := newExternalProcessTestClient(t, "crash-once", marker)
	defer client.Close()
	var result map[string]any
	mutation := map[string]any{
		"ref":         map[string]any{"sourceId": source.ID, "itemId": "item-1"},
		"operationId": "operation-1",
		"patch":       map[string]any{},
		"authority":   map[string]any{"authorizationRef": "auth-1", "scope": "item"},
	}
	if err := client.Call(context.Background(), "writeState", mutation, &result); err == nil || !strings.Contains(err.Error(), "outcome is unknown") {
		t.Fatalf("dispatched mutation crash error = %v", err)
	}
	params := map[string]any{"sourceId": source.ID}
	if err := client.Call(context.Background(), "list", params, &result); err != nil {
		t.Fatalf("subsequent call did not restart the adapter: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("restarted result = %#v", result)
	}
}

func TestAdapterConformanceRejectsMalformedAndOversizedResponses(t *testing.T) {
	for _, mode := range []string{"malformed", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			client, source, _ := newExternalProcessTestClient(t, mode, "")
			defer client.Close()
			var result map[string]any
			if err := client.Call(context.Background(), "list", map[string]any{"sourceId": source.ID}, &result); err == nil {
				t.Fatalf("%s response was accepted", mode)
			}
		})
	}
	client, source, _ := newExternalProcessTestClient(t, "malformed-once", filepath.Join(t.TempDir(), "malformed"))
	defer client.Close()
	var result map[string]any
	if err := client.Call(context.Background(), "list", map[string]any{"sourceId": source.ID}, &result); err == nil {
		t.Fatal("malformed response was accepted")
	}
	if err := client.Call(context.Background(), "list", map[string]any{"sourceId": source.ID}, &result); err == nil {
		t.Fatal("protocol failure restarted the stopped source")
	}
}

func TestAdapterConformanceCancellationNotifiesAdapter(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "cancel-received")
	client, source, _ := newExternalProcessTestClient(t, "cancel", marker)
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() {
		var result map[string]any
		finished <- client.Call(ctx, "list", map[string]any{"sourceId": source.ID}, &result)
	}()
	waitForFile(t, marker+".request")
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) && (err == nil || !strings.Contains(err.Error(), "cancelled")) {
		t.Fatalf("cancelled call error = %v", err)
	}
	waitForFile(t, marker)

	hanging, source, _ := newExternalProcessTestClient(t, "hang", "")
	ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var result map[string]any
	err := hanging.Call(ctx, "list", map[string]any{"sourceId": source.ID}, &result)
	if !errors.Is(err, context.DeadlineExceeded) && (err == nil || !strings.Contains(err.Error(), "deadline exceeded")) {
		t.Fatalf("hung adapter call error = %v", err)
	}
	hanging.Close()
}

func TestAdapterConformanceCancellationGraceRestartsAndReclaimsSlots(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ignored-cancellations")
	client, source, _ := newExternalProcessTestClient(t, "ignore-cancel", marker)
	if _, err := client.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	firstRun := client.run
	client.mu.Unlock()

	type callResult struct {
		index int
		err   error
	}
	results := make(chan callResult, externalMaxInFlight)
	cancels := make([]context.CancelFunc, externalMaxInFlight)
	for i := 0; i < externalMaxInFlight; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancels[i] = cancel
		method := "list"
		params := map[string]any{"sourceId": source.ID}
		if i == externalMaxInFlight-1 {
			method = "writeState"
			params = map[string]any{
				"ref":         map[string]any{"sourceId": source.ID, "itemId": "item-1"},
				"operationId": "operation-cancelled",
				"patch":       map[string]any{},
				"authority":   map[string]any{"authorizationRef": "auth-1", "scope": "item"},
			}
		}
		go func(index int, ctx context.Context, method string, params map[string]any) {
			var result map[string]any
			results <- callResult{index: index, err: client.Call(ctx, method, params, &result)}
		}(i, ctx, method, params)
	}
	waitForLineCount(t, marker, externalMaxInFlight)
	for _, cancel := range cancels {
		cancel()
	}
	for i := 0; i < externalMaxInFlight; i++ {
		select {
		case result := <-results:
			if result.err == nil {
				t.Fatalf("cancelled request %d unexpectedly succeeded", result.index)
			}
			if result.index == externalMaxInFlight-1 && !strings.Contains(result.err.Error(), "outcome is unknown") {
				t.Fatalf("cancelled dispatched mutation error = %v", result.err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("cancelled calls did not return")
		}
	}
	select {
	case <-firstRun.waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("adapter process did not stop after ignored cancellations")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var result map[string]any
	if err := client.Call(ctx, "list", map[string]any{"sourceId": source.ID}, &result); err != nil {
		t.Fatalf("request after cancellation restart failed: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("restarted request result = %#v", result)
	}
	requests, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(requests), "writeState\n"); got != 1 {
		t.Fatalf("cancelled mutation was dispatched %d times; requests=%q", got, requests)
	}
}

func TestExternalProcessTerminatesDescendants(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("process-group termination is supported on Linux and macOS")
	}
	marker := filepath.Join(t.TempDir(), "descendant-marker")
	client, source, _ := newExternalProcessTestClient(t, "spawn-descendant", marker)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		var result map[string]any
		finished <- client.Call(ctx, "list", map[string]any{"sourceId": source.ID}, &result)
	}()
	started := time.NewTimer(3 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	for {
		if _, err := os.Stat(marker + ".started"); err == nil {
			break
		}
		select {
		case err := <-finished:
			t.Fatalf("adapter call ended before descendant started: %v", err)
		case <-started.C:
			t.Fatal("timed out waiting for descendant to start")
		case <-ticker.C:
		}
	}
	started.Stop()
	ticker.Stop()
	client.mu.Lock()
	run := client.run
	client.mu.Unlock()
	client.Close()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("call succeeded after the adapter was closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call did not return after closing the adapter")
	}
	select {
	case <-run.waitDone:
	case <-time.After(2 * time.Second):
		t.Fatal("adapter process did not stop after close")
	}
	assertFileAbsentFor(t, marker, 600*time.Millisecond)
}

func TestExternalProcessDescendantHelper(t *testing.T) {
	marker := ""
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			marker = os.Args[i+1]
			break
		}
	}
	if marker == "" {
		return
	}
	if err := os.WriteFile(marker+".started", []byte("started"), 0o600); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	<-timer.C
	_ = os.WriteFile(marker, []byte("escaped"), 0o600)
}

func TestAdapterConformanceSecretRedaction(t *testing.T) {
	t.Setenv("GH_TOKEN", "ambient-gh-bearer-canary")
	t.Setenv("GITHUB_TOKEN", "ambient-github-bearer-canary")
	t.Setenv("PI_API_KEY", "ambient-pi-canary")
	t.Setenv("WORKLEASE_ADAPTER_CANARY", "ambient-worklease-canary")
	t.Setenv("LC_QUEUE_CANARY", "locale-is-allowed")
	t.Setenv("XDG_PRIVATE_HOME", "xdg-home-is-allowed")
	client, source, _ := newExternalProcessTestClient(t, "environment", "")
	defer client.Close()
	var result map[string]any
	if err := client.Call(context.Background(), "list", map[string]any{"sourceId": source.ID}, &result); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	for _, omitted := range []string{"GH_TOKEN", "GITHUB_TOKEN", "PI_API_KEY", "WORKLEASE_ADAPTER_CANARY"} {
		if strings.Contains(string(encoded), omitted) {
			t.Fatalf("disallowed environment variable %s reached adapter: %s", omitted, encoded)
		}
	}
	for _, allowed := range []string{"LC_QUEUE_CANARY", "XDG_PRIVATE_HOME"} {
		if !strings.Contains(string(encoded), allowed) {
			t.Fatalf("allowed environment variable %s was omitted: %s", allowed, encoded)
		}
	}

	client.Close()
	stderrClient, _, _ := newExternalProcessTestClient(t, "stderr-crash", "")
	defer stderrClient.Close()
	err := stderrClient.Call(context.Background(), "list", map[string]any{"sourceId": source.ID}, &result)
	if err == nil {
		t.Fatal("crashed adapter call unexpectedly succeeded")
	}
	for _, secret := range []string{"provider-secret-canary", "opaque-source-credential", "url-password-canary", "url-token-canary", "ambient-gh-bearer-canary", "ambient-github-bearer-canary"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("diagnostic leaked %q: %v", secret, err)
		}
	}
	if !strings.Contains(err.Error(), "safe diagnostic") || len(err.Error()) > 5000 {
		t.Fatalf("stderr diagnostic was not bounded and retained: len=%d err=%v", len(err.Error()), err)
	}
}

func TestExternalProcessApprovalRefusalDoesNotExecute(t *testing.T) {
	_, paths := testkit.Home(t)
	env := externalTestEnvironment(paths)
	marker := filepath.Join(t.TempDir(), "executed")
	source := externalTestSource(writeExternalAdapterScript(t, "normal", ""))
	source.Executable = writeRefusalScript(t, marker)
	if _, err := NewExternalProcess(source, env); err == nil {
		t.Fatal("unapproved executable was started")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("refused adapter executed: stat error = %v", err)
	}
}

func TestExternalProcessLaunchUsesApprovedSnapshotAfterPathReplacement(t *testing.T) {
	_, paths := testkit.Home(t)
	env := externalTestEnvironment(paths)
	marker := filepath.Join(t.TempDir(), "launched-by")
	executable := filepath.Join(t.TempDir(), "external-adapter")
	script := func(label string) []byte {
		temporaryMarker := shellQuote(marker + ".tmp")
		return []byte(fmt.Sprintf("#!/bin/sh\nprintf %s > %s\nmv %s %s\nexec %s -test.run='^TestExternalProcessHelper$' -- normal ''\n", shellQuote(label), temporaryMarker, temporaryMarker, shellQuote(marker), shellQuote(os.Args[0])))
	}
	approved := script("approved")
	replacement := script("replacement")
	if err := os.WriteFile(executable, approved, 0o700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	source := externalTestSource(canonical)
	if err := config.ApproveQueueAdapter(context.Background(), env, source); err != nil {
		t.Fatalf("approve external adapter: %v", err)
	}
	replacementPath := executable + ".replacement"
	var replaceErr error
	client, err := newExternalProcess(source, env, func() {
		if writeErr := os.WriteFile(replacementPath, replacement, 0o700); writeErr != nil {
			replaceErr = writeErr
			return
		}
		replaceErr = os.Rename(replacementPath, executable)
	})
	if err != nil {
		t.Fatalf("launch approved snapshot: %v", err)
	}
	defer client.Close()
	if replaceErr != nil {
		t.Fatalf("replace configured executable before Start: %v", replaceErr)
	}
	waitForFile(t, marker)
	launchedBy, err := os.ReadFile(marker)
	if err != nil || string(launchedBy) != "approved" {
		t.Fatalf("executed adapter marker = %q, err=%v; want approved bytes", launchedBy, err)
	}
	configuredBytes, err := os.ReadFile(executable)
	if err != nil || !bytes.Equal(configuredBytes, replacement) {
		t.Fatalf("configured path was not replaced: bytes=%q err=%v", configuredBytes, err)
	}
	client.mu.Lock()
	snapshotPath := client.run.cmd.Path
	client.mu.Unlock()
	if snapshotPath == source.Executable {
		t.Fatal("child was started from the configured executable path")
	}
	if _, err := os.Stat(snapshotPath); err != nil {
		t.Fatalf("launch snapshot disappeared before process exit: %v", err)
	}
	client.Close()
	if _, err := os.Stat(snapshotPath); !os.IsNotExist(err) {
		t.Fatalf("launch snapshot remained after Close: %v", err)
	}
}

func TestAdapterConformanceBudgetAndCredentialScope(t *testing.T) {
	client, source, _ := newExternalProcessTestClient(t, "normal", "")
	defer client.Close()
	var result map[string]any
	params := map[string]any{"sourceId": source.ID, "budget": map[string]any{"maxItems": 101, "maxBytes": 10}}
	if err := client.Call(context.Background(), "list", params, &result); err == nil {
		t.Fatal("over-limit request budget was accepted")
	}
	unsafeResolve := map[string]any{"sourceId": source.ID, "config": map[string]any{"password": "provider-secret-canary"}}
	if err := client.Call(context.Background(), "resolve", unsafeResolve, &result); err == nil {
		t.Fatal("raw config credential was accepted")
	}
	resolve := map[string]any{"sourceId": source.ID, "config": map[string]any{}}
	if err := client.Call(context.Background(), "resolve", resolve, &result); err != nil {
		t.Fatalf("opaque source credential reference was not limited to resolve: %v", err)
	}
}

func TestExternalProcessHelperHarnessIsBounded(t *testing.T) {
	crashed, err := testkit.RunTestProcess("external-crash", time.Second)
	if err == nil || crashed.ExitCode != 23 {
		t.Fatalf("crash helper result=%+v error=%v", crashed, err)
	}
	_, err = testkit.RunTestProcess("external-hang", 100*time.Millisecond)
	var timeoutErr *testkit.TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("hang helper error = %v", err)
	}
}

func newExternalProcessTestClient(t *testing.T, mode, marker string) (*ExternalProcess, config.QueueSource, func(string) string) {
	t.Helper()
	_, paths := testkit.Home(t)
	env := externalTestEnvironment(paths)
	executable := writeExternalAdapterScript(t, mode, marker)
	source := externalTestSource(executable)
	if err := config.ApproveQueueAdapter(context.Background(), env, source); err != nil {
		t.Fatalf("approve helper adapter: %v", err)
	}
	client, err := NewExternalProcess(source, env)
	if err != nil {
		t.Fatalf("start helper adapter: %v", err)
	}
	t.Cleanup(client.Close)
	return client, source, env
}

func externalTestSource(executable string) config.QueueSource {
	return config.QueueSource{
		ID:                "source-a",
		Adapter:           "external",
		Executable:        executable,
		ExpectedAdapterID: "example.adapter",
		ExpectedVersion:   "1.2.3",
		Config:            map[string]any{"password": "provider-secret-canary", "endpoint": "https://user:url-password-canary@provider.invalid/?access_token=url-token-canary"},
		CredentialRef:     "opaque-source-credential",
	}
}

func externalTestEnvironment(paths map[string]string) func(string) string {
	return func(name string) string {
		if value, ok := paths[name]; ok {
			return value
		}
		return os.Getenv(name)
	}
}

func writeExternalAdapterScript(t *testing.T, mode, marker string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "external-adapter")
	command := fmt.Sprintf("#!/bin/sh\nexec %s -test.run='^TestExternalProcessHelper$' -- %s %s\n", shellQuote(os.Args[0]), shellQuote(mode), shellQuote(marker))
	if err := os.WriteFile(path, []byte(command), 0o700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func writeRefusalScript(t *testing.T, marker string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "unapproved-adapter")
	contents := fmt.Sprintf("#!/bin/sh\nprintf started > %s\n", shellQuote(marker))
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func runExternalProcessHelper(mode, marker string) {
	reader := bufio.NewReader(os.Stdin)
	pendingCancelID := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			os.Exit(0)
		}
		var request map[string]json.RawMessage
		if json.Unmarshal([]byte(line), &request) != nil {
			os.Exit(31)
		}
		var method, id string
		_ = json.Unmarshal(request["method"], &method)
		_ = json.Unmarshal(request["id"], &id)
		if mode == "record-methods" && marker != "" {
			file, err := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				os.Exit(32)
			}
			_, _ = fmt.Fprintln(file, method)
			_ = file.Close()
		}
		if method == "$/cancelRequest" {
			if mode == "ignore-cancel" {
				continue
			}
			if pendingCancelID != "" {
				if marker != "" {
					_ = os.WriteFile(marker, []byte("cancel"), 0o600)
				}
				writeExternalHelperResponse(pendingCancelID, `{"ok":true}`)
				pendingCancelID = ""
			}
			continue
		}
		if method == "resolve" {
			var params map[string]json.RawMessage
			_ = json.Unmarshal(request["params"], &params)
			var credentialRef string
			_ = json.Unmarshal(params["credentialRef"], &credentialRef)
			if credentialRef != "opaque-source-credential" || strings.Contains(string(params["config"]), "provider-secret-canary") {
				os.Exit(30)
			}
			writeExternalHelperResponse(id, `{"source":{"id":"source-a"},"ok":true}`)
			continue
		}
		if method == "initialize" {
			if mode == "stderr-crash" {
				_, _ = fmt.Fprintln(os.Stderr, "safe diagnostic provider-secret-canary opaque-source-credential url-password-canary url-token-canary ambient-gh-bearer-canary ambient-github-bearer-canary")
				_, _ = fmt.Fprintln(os.Stderr, strings.Repeat("x", externalStderrLimit+4096))
			}
			policy := "generic"
			adapterID := "example.adapter"
			version := "1.2.3"
			protocol := `{"minMajor":1,"maxMajor":1}`
			features := `[]`
			switch mode {
			case "wrong-id":
				adapterID = "other.adapter"
			case "wrong-version":
				version = "1.2.4"
			case "wrong-policy":
				policy = "github"
			case "required-feature":
				features = ` ["new-feature"] `
			case "wrong-major":
				protocol = `{"minMajor":2,"maxMajor":2}`
			}
			result := fmt.Sprintf(`{"protocolVersion":1,"manifest":{"id":%q,"version":%q,"protocol":%s,"configSchema":{},"authentication":[],"resourcePolicy":%q,"capabilities":[],"requiredFeatures":%s}}`, adapterID, version, protocol, policy, features)
			writeExternalHelperResponse(id, result)
			continue
		}
		switch mode {
		case "rate-limited-once":
			if method == "list" {
				file, err := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
				if err != nil {
					os.Exit(33)
				}
				_, _ = fmt.Fprintln(file, method)
				_ = file.Close()
				calls, _ := os.ReadFile(marker)
				if strings.Count(string(calls), "list\n") == 1 {
					_, _ = fmt.Fprintf(os.Stdout, `{"jsonrpc":"2.0","id":%q,"error":{"code":-32006,"message":"rate limited","data":{"diagnostic":"rate-limited","retryAt":"2099-12-31T00:00:00Z"}}}`+"\n", id)
					continue
				}
			}
			writeExternalHelperResponse(id, `{"ok":true}`)
		case "crash-once":
			file, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err == nil {
				_ = file.Close()
				os.Exit(17)
			}
			writeExternalHelperResponse(id, `{"ok":true}`)
		case "hang":
			blockExternalProcess()
		case "ignore-cancel":
			file, err := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err != nil {
				os.Exit(32)
			}
			_, _ = fmt.Fprintln(file, method)
			_ = file.Close()
			requests, _ := os.ReadFile(marker)
			if strings.Count(string(requests), "\n") > externalMaxInFlight {
				writeExternalHelperResponse(id, `{"ok":true}`)
			}
		case "spawn-descendant":
			child := exec.Command(os.Args[0], "-test.run=^TestExternalProcessDescendantHelper$", "--", marker)
			if err := child.Start(); err != nil {
				os.Exit(33)
			}
			blockExternalProcess()
		case "malformed", "malformed-once":
			if mode == "malformed-once" {
				file, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
				if err != nil {
					writeExternalHelperResponse(id, `{"ok":true}`)
					continue
				}
				_ = file.Close()
			}
			_, _ = fmt.Fprintf(os.Stdout, `{"jsonrpc":"2.0","id":%q,"result":{},"result":{}}`+"\n", id)
		case "oversized":
			_, _ = fmt.Fprintf(os.Stdout, `{"jsonrpc":"2.0","id":%q,"result":{"data":"`, id)
			_, _ = ioWriteString(os.Stdout, strings.Repeat("x", externalFrameLimit))
			_, _ = fmt.Fprintln(os.Stdout, `"}}`)
		case "cancel":
			pendingCancelID = id
			if marker != "" {
				_ = os.WriteFile(marker+".request", []byte("request"), 0o600)
			}
		case "stderr-crash":
			os.Exit(19)
		case "environment":
			keys := make([]string, 0)
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				keys = append(keys, key)
			}
			encoded, _ := json.Marshal(map[string]any{"env": keys, "ok": true})
			writeExternalHelperResponse(id, string(encoded))
		default:
			writeExternalHelperResponse(id, `{"ok":true}`)
		}
	}
}

func writeExternalHelperResponse(id, result string) {
	_, _ = fmt.Fprintf(os.Stdout, `{"jsonrpc":"2.0","id":%q,"result":%s}`+"\n", id, result)
}

func ioWriteString(writer *os.File, value string) (int, error) {
	return writer.Write([]byte(value))
}

func blockExternalProcess() {
	ticker := time.NewTicker(time.Hour)
	<-ticker.C
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for helper marker %s", path)
		case <-ticker.C:
		}
	}
}

func waitForLineCount(t *testing.T, path string, want int) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		contents, err := os.ReadFile(path)
		if err == nil && strings.Count(string(contents), "\n") >= want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for %d requests in %s; contents=%q", want, path, contents)
		case <-ticker.C:
		}
	}
}

func assertFileAbsentFor(t *testing.T, path string, duration time.Duration) {
	t.Helper()
	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("unexpected marker from terminated descendant: %s", path)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat descendant marker: %v", err)
		}
		select {
		case <-deadline.C:
			return
		case <-ticker.C:
		}
	}
}
