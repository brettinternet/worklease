package queue

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func credentialFixture(t *testing.T, script string) []string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "helper")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	return []string{binary}
}

func TestCredentialHelperResolve(t *testing.T) {
	t.Parallel()
	argv := credentialFixture(t, "printf 'test-token\\n'\n")
	verify := func(_ context.Context, token string) (string, error) {
		if token != "test-token" {
			return "", fmt.Errorf("bad token %s", token)
		}
		return "alice", nil
	}
	h := new(CredentialHelper)
	result, err := h.Resolve(context.Background(), argv, "alice", verify)
	if err != nil || result != "test-token" {
		t.Fatalf("resolve: %q %v", result, err)
	}
	for _, principal := range []string{"bob", "alice"} {
		check := verify
		if principal == "alice" {
			check = func(context.Context, string) (string, error) { return "", fmt.Errorf("secret-leak-canary") }
		}
		_, err := h.Resolve(context.Background(), argv, principal, check)
		if err == nil || strings.Contains(err.Error(), "test-token") || strings.Contains(err.Error(), "secret-leak-canary") || strings.Contains(err.Error(), argv[0]) {
			t.Fatalf("unsafe principal diagnostic: %v", err)
		}
	}
	failure := credentialFixture(t, "printf 'secret-leak-canary\\n' >&2\nexit 2\n")
	_, err = h.Resolve(context.Background(), failure, "alice", verify)
	if err == nil || strings.Contains(err.Error(), "secret-leak-canary") {
		t.Fatalf("unsafe failure diagnostic: %v", err)
	}
	oversize := credentialFixture(t, "printf '%4097s' x\n")
	_, err = h.Resolve(context.Background(), oversize, "alice", verify)
	if err == nil || !strings.Contains(err.Error(), "4KiB") {
		t.Fatalf("unbounded output: %v", err)
	}
	oversizeStderr := credentialFixture(t, "printf '%4097s' x >&2\nprintf 'test-token\\n'\n")
	_, err = h.Resolve(context.Background(), oversizeStderr, "alice", verify)
	if err == nil || !strings.Contains(err.Error(), "4KiB") {
		t.Fatalf("unbounded stderr: %v", err)
	}
}

func TestCredentialHelperTimeoutAndScrubbedProcess(t *testing.T) {
	// t.Setenv changes process-global state, so this test is not parallel.
	t.Setenv("GH_TOKEN", "secret-leak-canary")
	t.Setenv("WORKLEASE_SESSION_ID", "secret-leak-canary")
	h := new(CredentialHelper)
	verify := func(context.Context, string) (string, error) { return "alice", nil }
	argv := credentialFixture(t, "[ -z \"$GH_TOKEN$WORKLEASE_SESSION_ID\" ] || { printf 'secret-leak-canary\\n' >&2; exit 2; }\nprintf 'test-token\\n'\n")
	if _, err := h.Resolve(context.Background(), argv, "alice", verify); err != nil {
		t.Fatalf("process inherited ambient credentials: %v", err)
	}
	blocked := credentialFixture(t, "while :; do :; done\n")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := h.Resolve(ctx, blocked, "alice", verify); err == nil || strings.Contains(err.Error(), "secret-leak-canary") {
		t.Fatalf("unbounded or unsafe timed-out helper: %v", err)
	}
}

func TestCredentialHelperEnvironment(t *testing.T) {
	t.Parallel()
	vars := credentialHelperEnvironment(func(key string) string {
		return map[string]string{"PATH": "/bin", "HOME": "/private/home", "GH_TOKEN": "secret", "WORKLEASE_SESSION_ID": "session", "AWS_SESSION_TOKEN": "secret"}[key]
	})
	if strings.Join(vars, ",") != "PATH=/bin,HOME=/private/home" {
		t.Fatalf("unexpected helper environment: %q", vars)
	}
}

func TestCredentialHelperSerializesRefresh(t *testing.T) {
	t.Parallel()
	h := new(CredentialHelper)
	argv := credentialFixture(t, "printf 'test-token\\n'\n")
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	verify := func(context.Context, string) (string, error) {
		once.Do(func() { close(entered); <-release })
		return "alice", nil
	}
	first := make(chan error, 1)
	go func() { _, err := h.Resolve(context.Background(), argv, "alice", verify); first <- err }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.Resolve(ctx, argv, "alice", verify); err == nil || !strings.Contains(err.Error(), "canceled waiting") {
		t.Fatalf("wait for occupied credential: %v", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}
