package config

import (
	"context"
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

func TestRecordQueueClaimItemAddsIDOnlyToUnchangedDomain(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	env := func(key string) string {
		if key == "XDG_CONFIG_HOME" {
			return root
		}
		return ""
	}
	if err := os.Mkdir(filepath.Join(root, "worklease"), 0o700); err != nil {
		t.Fatal(err)
	}
	confirmed := QueueIdentity{Adapter: "backlog-md", Locator: "/checkout", Policy: "generic", Source: "portable", AuthorityID: "authority", ItemIDs: []string{"1"}}
	if err := SaveQueueIdentities(env, QueueIdentities{Version: 1, Sources: map[string]QueueIdentity{"s": confirmed}}); err != nil {
		t.Fatal(err)
	}
	if err := RecordQueueClaimItem(context.Background(), env, "s", confirmed, "2"); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadQueueIdentities(env)
	if err != nil || len(loaded.Sources["s"].ItemIDs) != 2 || loaded.Sources["s"].ItemIDs[1] != "2" {
		t.Fatalf("recorded IDs: %+v %v", loaded, err)
	}
	changed := confirmed
	changed.Source = "other"
	if err := RecordQueueClaimItem(context.Background(), env, "s", changed, "3"); err == nil {
		t.Fatal("recorded an item against a stale claim domain")
	}
}
