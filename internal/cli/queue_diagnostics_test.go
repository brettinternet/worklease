package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/testkit"
)

func TestQueueRefreshFailureHistoryIsPrivateBoundedAndRedacted(t *testing.T) {
	t.Parallel()
	_, values := testkit.Home(t)
	env := func(key string) string { return values[key] }
	when := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	for i := range queueFailureLimit + 1 {
		failure := queueRefreshFailure{At: when.Add(time.Duration(i) * time.Second), Source: "worklease", Operation: "untrusted", Code: "provider-failed", DurationMS: 15}
		if i == 1 {
			failure.Source = "secret\nsource"
			failure.Code = "secret\nprovider stderr"
		}
		if err := writeQueueRefreshFailure(context.Background(), env, failure); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(values["XDG_STATE_HOME"], "worklease", "queue-refresh-failures.json")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("diagnostic file mode: %v %v", info, err)
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil || dir.Mode().Perm() != 0o700 {
		t.Fatalf("diagnostic directory mode: %v %v", dir, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(data), "secret") {
		t.Fatalf("diagnostic file contains raw input: %v %s", err, data)
	}
	var history []queueRefreshFailure
	if err := json.Unmarshal(data, &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != queueFailureLimit || history[0].At != when.Add(time.Second) || history[0].Source != "unclassified" || history[0].Code != "unclassified" || history[len(history)-1].Code != "provider-failed" || history[0].Operation != "refresh" {
		t.Fatalf("incorrect bounded history: %+v", history)
	}
}

func TestQueueRefreshFailureHistoryRejectsSymlink(t *testing.T) {
	t.Parallel()
	_, values := testkit.Home(t)
	env := func(key string) string { return values[key] }
	state := filepath.Join(values["XDG_STATE_HOME"], "worklease")
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(values["HOME"], "outside")
	if err := os.WriteFile(outside, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(state, "queue-refresh-failures.json")); err != nil {
		t.Fatal(err)
	}
	if err := writeQueueRefreshFailure(context.Background(), env, queueRefreshFailure{Code: "provider-failed"}); err == nil {
		t.Fatal("followed diagnostic file symlink")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "unchanged" {
		t.Fatalf("modified symlink target: %q %v", data, err)
	}
}
