package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQueueIdentityRecordIsPrivateAndDurable(t *testing.T) {
	root := t.TempDir()
	env := func(key string) string {
		if key == "XDG_CONFIG_HOME" {
			return root
		}
		return ""
	}
	state, err := LoadQueueIdentities(env)
	if err != nil || len(state.Sources) != 0 {
		t.Fatalf("empty: %+v %v", state, err)
	}
	if err := os.Mkdir(filepath.Join(root, "worklease"), 0o700); err != nil {
		t.Fatal(err)
	}
	state.Sources["project"] = QueueIdentity{Adapter: "backlog-md", Locator: "/checkout", Policy: "generic", Source: "portable", AuthorityID: "authority", ItemIDs: []string{"TASK-1"}}
	if err := SaveQueueIdentities(env, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadQueueIdentities(env)
	if err != nil || loaded.Sources["project"].Source != "portable" || len(loaded.Sources["project"].ItemIDs) != 1 {
		t.Fatalf("round trip: %+v %v", loaded, err)
	}
	if err := os.Chmod(QueueIdentityPath(env), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQueueIdentities(env); err == nil {
		t.Fatal("world-readable confirmation was accepted")
	}
	if err := os.Chmod(QueueIdentityPath(env), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(QueueIdentityPath(env), []byte(`{"version":2,"sources":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadQueueIdentities(env); err == nil {
		t.Fatal("unknown identity schema was accepted")
	}
	if _, err := os.Stat(filepath.Dir(QueueIdentityPath(env))); err != nil {
		t.Fatal(err)
	}
}
