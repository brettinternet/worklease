package config

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestQueueSessionIDIsPrivatePersistentAndSeparateFromWorkerSession(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("WORKLEASE_SESSION_ID", "worker-session")
	first, err := QueueSessionID(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(first) || first == os.Getenv("WORKLEASE_SESSION_ID") {
		t.Fatalf("queue session is not an independent full UUID: %q", first)
	}
	second, err := QueueSessionID(os.Getenv)
	if err != nil || second != first {
		t.Fatalf("queue session was not persisted: %q %v", second, err)
	}
	info, err := os.Stat(filepath.Join(root, "worklease", "queue-session-id"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("queue session mode = %o, want 600", info.Mode().Perm())
	}
	dir, err := os.Stat(filepath.Join(root, "worklease"))
	if err != nil || dir.Mode().Perm() != 0o700 {
		t.Fatalf("queue session directory = %v %v", dir, err)
	}
}
