package runs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHealthDerivesAbandonedAndIdle(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	live := func(int) bool { return true }
	dead := func(int) bool { return false }
	running := Record{State: StateRunning, SupervisorPID: 1, StartedAt: start, UpdatedAt: start, IntervalSeconds: 15}
	output := start.Add(20 * time.Minute)
	for name, tc := range map[string]struct {
		record Record
		now    time.Time
		alive  func(int) bool
		want   string
	}{
		"running":              {running, start.Add(time.Minute - time.Second), live, StateRunning},
		"supervisor gone":      {running, start.Add(time.Second), dead, "abandoned"},
		"record stale":         {running, start.Add(61 * time.Second), live, "abandoned"},
		"quiet worker":         {withUpdate(running, start.Add(IdleAfter+time.Second)), start.Add(IdleAfter + time.Second), live, "idle"},
		"recent output":        {withOutput(withUpdate(running, output), output), output.Add(time.Minute), live, StateRunning},
		"terminal ignores pid": {Record{State: StateExited, SupervisorPID: 1}, start, dead, StateExited},
	} {
		if got := tc.record.Health(tc.now, tc.alive); got != tc.want {
			t.Errorf("%s: health %q, want %q", name, got, tc.want)
		}
	}
}

func withUpdate(r Record, at time.Time) Record { r.UpdatedAt = at; return r }
func withOutput(r Record, at time.Time) Record { r.LastOutputAt = &at; return r }

func TestReadResultValidatesWorkerFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if result, err := ReadResult(filepath.Join(dir, "missing")); result != nil || err != nil {
		t.Fatalf("missing result: %+v %v", result, err)
	}
	result, err := ReadResult(write("ok", `{"outcome":"review","summary":"`+strings.Repeat("x", 2000)+`"}`))
	if err != nil || result.Outcome != OutcomeReview || len(result.Summary) != 1024 {
		t.Fatalf("valid result: %+v %v", result, err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(write("target", `{"outcome":"done"}`), link); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"unknown outcome": write("bad", `{"outcome":"shipped"}`),
		"not JSON":        write("text", "done"),
		"oversized":       write("big", `{"outcome":"done","summary":"`+strings.Repeat("x", MaxResultBytes)+`"}`),
		"symlink":         link,
	} {
		if result, err := ReadResult(path); err == nil {
			t.Errorf("%s accepted: %+v", name, result)
		}
	}
}

func TestStoreListsNewestFirstAndRejectsUnsafeIDs(t *testing.T) {
	t.Parallel()
	store := Store{Dir: filepath.Join(t.TempDir(), "runs")}
	if records, err := store.List(); err != nil || len(records) != 0 {
		t.Fatalf("missing directory: %v %v", records, err)
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	older := Record{ID: NewID(start, "0000000f"), StartedAt: start}
	newer := Record{ID: NewID(start, "00000001"), StartedAt: start.Add(time.Millisecond)}
	for _, r := range []Record{older, newer} {
		if err := store.Create(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Create(older); err == nil {
		t.Fatal("duplicate run ID was replaced")
	}
	records, err := store.List()
	if err != nil || len(records) != 2 || records[0].ID != newer.ID {
		t.Fatalf("order: %+v %v", records, err)
	}
	if _, err := store.Read("../escape"); err == nil {
		t.Fatal("path-like run ID accepted")
	}
}
