package testkit

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Home creates an owner-private state directory and returns an isolated
// environment map. It never changes the process environment.
func Home(t testing.TB) (string, map[string]string) {
	t.Helper()
	root := t.TempDir()
	paths := map[string]string{
		"HOME":            filepath.Join(root, "home"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_STATE_HOME":  filepath.Join(root, "state"),
		"WORKLEASE_HOME":  filepath.Join(root, "worklease"),
	}
	for key, path := range paths {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("create isolated %s: %v", key, err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			t.Fatalf("make isolated %s private: %v", key, err)
		}
	}
	return paths["WORKLEASE_HOME"], paths
}

// IsolateProcessEnvironment replaces ambient user and Worklease configuration
// with owner-private temporary roots. It returns a cleanup function restoring
// the caller's environment after the test binary finishes.
func IsolateProcessEnvironment() (func(), error) {
	root, err := os.MkdirTemp("", "worklease-test-")
	if err != nil {
		return nil, fmt.Errorf("create test environment: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		_ = os.RemoveAll(root)
		return nil, fmt.Errorf("make test environment private: %w", err)
	}
	paths := map[string]string{
		"HOME":            filepath.Join(root, "home"),
		"XDG_CONFIG_HOME": filepath.Join(root, "config"),
		"XDG_STATE_HOME":  filepath.Join(root, "state"),
	}
	for key, path := range paths {
		if err := os.Mkdir(path, 0o700); err != nil {
			_ = os.RemoveAll(root)
			return nil, fmt.Errorf("create isolated %s: %w", key, err)
		}
	}

	before := append([]string(nil), os.Environ()...)
	preserve := helperEnvironmentForInvocation(before, os.Args)
	for _, entry := range before {
		key, _, ok := strings.Cut(entry, "=")
		if ok && (strings.HasPrefix(key, "WORKLEASE_") || strings.HasPrefix(key, "GIT_")) {
			_ = os.Unsetenv(key)
		}
	}
	for key, value := range paths {
		if err := os.Setenv(key, value); err != nil {
			restoreEnvironment(before)
			_ = os.RemoveAll(root)
			return nil, fmt.Errorf("set isolated %s: %w", key, err)
		}
	}
	for key, value := range preserve {
		if err := os.Setenv(key, value); err != nil {
			restoreEnvironment(before)
			_ = os.RemoveAll(root)
			return nil, fmt.Errorf("preserve test helper %s: %w", key, err)
		}
	}
	return func() {
		restoreEnvironment(before)
		_ = os.RemoveAll(root)
	}, nil
}

var testHelperInvocations = map[string][]string{
	"TestDriverProcessHelper":                     {"WORKLEASE_DRIVER_HELPER"},
	"TestGuardHelperProcess":                      {"WORKLEASE_GO_HELPER"},
	"TestHandleCLIProcessHelper":                  {"WORKLEASE_HANDLE_TEST_HELPER"},
	"TestHostedLockSurvivesPausedHolder":          {"WORKLEASE_HOSTED_HOLDER", "WORKLEASE_HOSTED_HOME"},
	"TestProcessHelper":                           {"WORKLEASE_TEST_HELPER"},
	"TestStoreEventProcessHelper":                 {"WORKLEASE_STORE_EVENT_HOME", "WORKLEASE_STORE_EVENT_RESULT"},
	"TestWatchSubprocessEventWakesFilteredWaiter": {"WORKLEASE_WATCH_HELPER", "WORKLEASE_WATCH_HOME", "WORKLEASE_WATCH_CURSOR"},
	"TestQueueQueryConcurrentProcessHelper":       {"QUEUE_QUERY_CONCURRENT_ROOT"},
	"TestLaunchChildHelper":                       {"WORKLEASE_PROFILE", "WORKLEASE_QUEUE_AUTHORITY_ID", "WORKLEASE_QUEUE_REF", "WORKLEASE_QUEUE_RESOURCES"},
}

func helperEnvironmentForInvocation(environment, arguments []string) map[string]string {
	allowed := map[string]bool{}
	for testName, keys := range testHelperInvocations {
		for _, argument := range arguments {
			if strings.HasPrefix(argument, "-test.run=") && strings.Contains(argument, testName) {
				for _, key := range keys {
					allowed[key] = true
				}
			}
		}
	}
	preserved := map[string]string{}
	for _, entry := range environment {
		key, value, ok := strings.Cut(entry, "=")
		if ok && allowed[key] {
			preserved[key] = value
		}
	}
	return preserved
}

func restoreEnvironment(entries []string) {
	current := os.Environ()
	for _, entry := range current {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			_ = os.Unsetenv(key)
		}
	}
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			_ = os.Setenv(key, value)
		}
	}
}

// Environment returns a deterministic child environment. WORKLEASE_* and
// GIT_* entries from base are removed before overrides are applied.
func Environment(base []string, overrides map[string]string) []string {
	values := make(map[string]string)
	for _, entry := range base {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || strings.HasPrefix(key, "WORKLEASE_") || strings.HasPrefix(key, "GIT_") {
			continue
		}
		values[key] = value
	}
	for key, value := range overrides {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}
