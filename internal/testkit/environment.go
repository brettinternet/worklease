package testkit

import (
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
	path := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("create isolated Worklease home: %v", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatalf("make isolated Worklease home private: %v", err)
	}
	return path, map[string]string{"WORKLEASE_HOME": path}
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
