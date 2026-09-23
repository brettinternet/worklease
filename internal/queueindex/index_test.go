package queueindex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/queue"
)

func TestIndexPartitionsRetentionAndIncompleteScan(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	p := Partition{Source: "s", Principal: "alice", Scope: "repo:a", Generation: "g1"}
	item := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "s", ItemID: "1"}, Title: "searchable"}, Observation: queue.Observation{ObservedAt: time.Now()}}
	if err := idx.Replace(ctx, p, []queue.Item{item}, true); err != nil {
		t.Fatal(err)
	}
	got, observed, fresh, err := idx.Read(ctx, p, time.Minute)
	if err != nil || len(got.Items) != 1 || !fresh || observed.IsZero() {
		t.Fatalf("read=%+v observed=%v fresh=%v err=%v", got, observed, fresh, err)
	}
	other := p
	other.Scope = "repo:b"
	got, _, _, err = idx.Read(ctx, other, time.Hour)
	if err != nil || len(got.Items) != 0 {
		t.Fatalf("partition leak: %+v %v", got, err)
	}
	if _, err := idx.db.Exec("UPDATE partitions SET observed=? WHERE partition=(SELECT partition FROM entries LIMIT 1)", time.Now().Add(-2*time.Hour).UnixNano()); err != nil {
		t.Fatal(err)
	}
	if err := idx.Replace(ctx, p, nil, false); err != nil {
		t.Fatal(err)
	}
	got, observed, fresh, readErr := idx.Read(ctx, p, time.Hour)
	if readErr != nil || fresh || time.Since(observed) < time.Hour {
		t.Fatalf("incomplete scan refreshed complete observation: %v %v %v", observed, fresh, readErr)
	}
	if len(got.Items) != 1 {
		t.Fatal("incomplete scan removed cached item")
	}
	if err := idx.Replace(ctx, p, nil, true); err != nil {
		t.Fatal(err)
	}
	got, _, fresh, err = idx.Read(ctx, p, time.Hour)
	if err != nil || len(got.Items) != 0 || !fresh || got.Sources[p.Source].State != queue.CoverageComplete {
		t.Fatalf("complete empty scan lost cache coverage: %+v fresh=%v err=%v", got, fresh, err)
	}
	if refs, coverage, err := idx.Search(ctx, p, "searchable"); err != nil || len(refs) != 0 || coverage != "indexed-summaries" {
		t.Fatalf("stale FTS projection refs=%v coverage=%s err=%v", refs, coverage, err)
	}
	if err := idx.Replace(ctx, p, []queue.Item{item}, true); err != nil {
		t.Fatal(err)
	}
	if err := idx.Revoke(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, _, _, err = idx.Read(ctx, p, time.Hour)
	if err != nil || len(got.Items) != 0 {
		t.Fatalf("revocation left entries: %v %+v", err, got)
	}
	if refs, _, err := idx.Search(ctx, p, "searchable"); err != nil || len(refs) != 0 {
		t.Fatalf("revocation left search projection: %v %v", refs, err)
	}
}
func TestExplicitInaccessibleEvidencePurgesIncompleteProjection(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	p := Partition{Source: "s", Principal: "u", Scope: "scope", Generation: "g"}
	ref := queue.Ref{SourceID: "s", ItemID: "secret"}
	item := queue.Item{Summary: queue.Summary{Ref: ref, Title: "revokedsecret"}, Observation: queue.Observation{ObservedAt: time.Now()}}
	if err := idx.Replace(ctx, p, []queue.Item{item}, true); err != nil {
		t.Fatal(err)
	}
	if err := idx.ReplaceWithDeletes(ctx, p, nil, []queue.Ref{ref}, false); err != nil {
		t.Fatal(err)
	}
	got, _, _, err := idx.Read(ctx, p, time.Hour)
	if err != nil || len(got.Items) != 0 {
		t.Fatalf("inaccessible payload retained: %v %+v", err, got)
	}
	refs, _, err := idx.Search(ctx, p, "revokedsecret")
	if err != nil || len(refs) != 0 {
		t.Fatalf("inaccessible search projection retained: %v %v", refs, err)
	}
}

func TestGitHubSyncPageAndWatermarkAreAtomic(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	p := Partition{Source: "github", Principal: "alice", Scope: "origin/repo-id", Generation: "g1"}
	watermark := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	first := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "github", ItemID: "1"}, CanonicalID: "node-1", Title: "one"}}
	if err := idx.CommitGitHubSyncPage(ctx, p, []queue.Item{first}, "resume", watermark, false); err != nil {
		t.Fatal(err)
	}
	state, err := idx.ReadGitHubSyncState(ctx, p)
	if err != nil || state.Cursor != "resume" || !state.CommittedWatermark.IsZero() || !state.ScanWatermark.Equal(watermark) {
		t.Fatalf("partial state=%+v err=%v", state, err)
	}
	second := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "github", ItemID: "2"}, CanonicalID: "node-2", Title: "two"}}
	if err := idx.CommitGitHubSyncPage(ctx, p, []queue.Item{second}, "", watermark, true); err != nil {
		t.Fatal(err)
	}
	state, err = idx.ReadGitHubSyncState(ctx, p)
	if err != nil || state.Cursor != "" || !state.CommittedWatermark.Equal(watermark) {
		t.Fatalf("complete state=%+v err=%v", state, err)
	}
	got, _, _, err := idx.Read(ctx, p, time.Hour)
	if err != nil || len(got.Items) != 2 {
		t.Fatalf("persisted pages=%d err=%v", len(got.Items), err)
	}
}

func TestGitHubReconciliationRetiresOnlyAtCompletedGeneration(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	p := Partition{Source: "github", Principal: "alice", Scope: "origin/repo", Generation: "g1"}
	old := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "github", ItemID: "old"}, CanonicalID: "old-node", Title: "old"}}
	if err := idx.CommitGitHubSyncPage(ctx, p, []queue.Item{old}, "", time.Now(), true); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC()
	state, err := idx.StartGitHubReconciliation(ctx, p, started)
	if err != nil || state.ReconciliationCursor != "@start" {
		t.Fatalf("start=%+v err=%v", state, err)
	}
	first := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "github", ItemID: "new-1"}, CanonicalID: "new-node-1"}}
	retired, err := idx.CommitGitHubReconciliationPage(ctx, p, []queue.Item{first}, "after", state.ReconciliationGeneration, started, false)
	if err != nil || len(retired) != 0 {
		t.Fatalf("partial retired=%v err=%v", retired, err)
	}
	got, _, _, err := idx.Read(ctx, p, time.Hour)
	if err != nil || len(got.Items) != 2 {
		t.Fatalf("partial scan retired rows: %v %+v", err, got.Items)
	}
	second := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "github", ItemID: "new-2"}, CanonicalID: "new-node-2"}}
	retired, err = idx.CommitGitHubReconciliationPage(ctx, p, []queue.Item{second}, "", state.ReconciliationGeneration, started, true)
	if err != nil || len(retired) != 1 || retired[0] != old.Ref {
		t.Fatalf("complete retired=%v err=%v", retired, err)
	}
	got, _, _, err = idx.Read(ctx, p, time.Hour)
	if err != nil || len(got.Items) != 2 {
		t.Fatalf("completed projection=%v err=%v", got.Items, err)
	}
}

func TestGitHubReconciliationNeverMovesIncrementalWatermarkBackwards(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	p := Partition{Source: "github", Principal: "alice", Scope: "origin/repo", Generation: "g1"}
	started := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	state, err := idx.StartGitHubReconciliation(ctx, p, started)
	if err != nil {
		t.Fatal(err)
	}
	later := started.Add(time.Hour)
	if err := idx.CommitGitHubSyncPage(ctx, p, nil, "", later, true); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.CommitGitHubReconciliationPage(ctx, p, nil, "", state.ReconciliationGeneration, started, true); err != nil {
		t.Fatal(err)
	}
	got, err := idx.ReadGitHubSyncState(ctx, p)
	if err != nil || !got.CommittedWatermark.Equal(later) {
		t.Fatalf("completed older reconciliation lost incremental progress: %+v %v", got, err)
	}
}

func TestGitHubSyncDeduplicatesByNodeIDAcrossReferences(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	p := Partition{Source: "github", Principal: "alice", Scope: "origin/repo", Generation: "g1"}
	watermark := time.Now().UTC()
	first := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "github", ItemID: "1"}, CanonicalID: "node-1", Title: "old"}}
	second := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "github", ItemID: "2"}, CanonicalID: "node-1", Title: "moved"}}
	if err := idx.CommitGitHubSyncPage(ctx, p, []queue.Item{first}, "next", watermark, false); err != nil {
		t.Fatal(err)
	}
	if err := idx.CommitGitHubSyncPage(ctx, p, []queue.Item{second}, "", watermark, true); err != nil {
		t.Fatal(err)
	}
	got, _, _, err := idx.Read(ctx, p, time.Hour)
	if err != nil || len(got.Items) != 1 {
		t.Fatalf("node dedup items=%v err=%v", got.Items, err)
	}
	if _, ok := got.Items[second.Ref.Key()]; !ok {
		t.Fatalf("latest node reference missing: %+v", got.Items)
	}
}

func TestBodySearchRequiresOptIn(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	p := Partition{Source: "s", Principal: "u", Scope: "scope", Generation: "g"}
	item := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "s", ItemID: "1"}, Title: "summary"}, Body: "uniquebodyterm"}
	if err := idx.Replace(ctx, p, []queue.Item{item}, true); err != nil {
		t.Fatal(err)
	}
	refs, coverage, err := idx.Search(ctx, p, "uniquebodyterm")
	if err != nil || len(refs) != 0 || coverage != "indexed-summaries" {
		t.Fatalf("body indexed without opt-in: %v %s %v", refs, coverage, err)
	}
	idx.EnableBodyIndex()
	if err := idx.Replace(ctx, p, []queue.Item{item}, true); err != nil {
		t.Fatal(err)
	}
	refs, coverage, err = idx.Search(ctx, p, "uniquebodyterm")
	if err != nil || len(refs) != 1 || coverage != "indexed-bodies" {
		t.Fatalf("body not indexed after opt-in: %v %s %v", refs, coverage, err)
	}
}

func TestRetentionPurgesSummaryAndFTS(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	p := Partition{Source: "s", Principal: "u", Scope: "scope", Generation: "g"}
	item := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "s", ItemID: "old"}, Title: "expiredphrase"}, Observation: queue.Observation{ObservedAt: time.Now()}}
	if err := idx.Replace(ctx, p, []queue.Item{item}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.db.Exec("UPDATE entries SET observed=?", time.Now().Add(-Retention-time.Hour).UnixNano()); err != nil {
		t.Fatal(err)
	}
	got, _, _, err := idx.Read(ctx, p, time.Hour)
	if err != nil || len(got.Items) != 0 {
		t.Fatalf("expired row survived: %v %+v", err, got)
	}
	refs, _, err := idx.Search(ctx, p, "expiredphrase")
	if err != nil || len(refs) != 0 {
		t.Fatalf("expired FTS projection survived: %v %v", refs, err)
	}
}

func TestIndexPrivateWALAndSingleFlight(t *testing.T) {
	dir := t.TempDir()
	idx, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	info, err := os.Stat(filepath.Join(dir, "index.sqlite"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("database permissions: %v %v", info, err)
	}
	var mode string
	if err := idx.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal mode=%q err=%v", mode, err)
	}
	p := Partition{Source: "s", Principal: "u", Scope: "r", Generation: "g"}
	release, ok, err := idx.TryRefreshLock(p)
	if err != nil || !ok {
		t.Fatalf("first lock: %v %v", ok, err)
	}
	defer release()
	_, ok, err = idx.TryRefreshLock(p)
	if err != nil || ok {
		t.Fatalf("second lock acquired=%v err=%v", ok, err)
	}
}
func TestLockProcessHelper(t *testing.T) {
	if os.Getenv("QUEUE_INDEX_LOCK_HELPER") != "1" {
		return
	}
	idx, err := Open(context.Background(), os.Getenv("QUEUE_INDEX_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	p := Partition{Source: "s", Principal: "u", Scope: "r", Generation: "g"}
	release, ok, err := idx.TryRefreshLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return
	}
	defer release()
	marker, err := os.OpenFile(os.Getenv("QUEUE_INDEX_MARKER"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = marker.WriteString("winner\\n")
	marker.Close()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
}

func TestLockIsSingleFlightAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "winners")
	cmds := make([]*exec.Cmd, 8)
	outputs := make([]strings.Builder, len(cmds))
	for n := range cmds {
		cmds[n] = exec.Command(os.Args[0], "-test.run=^TestLockProcessHelper$")
		cmds[n].Env = append(os.Environ(), "QUEUE_INDEX_LOCK_HELPER=1", "QUEUE_INDEX_DIR="+dir, "QUEUE_INDEX_MARKER="+marker)
		cmds[n].Stderr = &outputs[n]
	}
	for _, cmd := range cmds {
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for index, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("helper %d failed: %v: %s", index, err, outputs[index].String())
		}
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "winner"); got != 1 {
		t.Fatalf("cross-process lock winners=%d output=%q", got, data)
	}
}

func TestCorruptOrUnknownSchemaRebuiltOnce(t *testing.T) {
	dir := t.TempDir()
	idx, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = idx.db.Exec("PRAGMA user_version=77"); err != nil {
		t.Fatal(err)
	}
	idx.Close()
	idx, err = Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	var version int
	if err := idx.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != SchemaGeneration {
		t.Fatalf("version=%d err=%v", version, err)
	}
}

func TestForSourceBypassesUnprovenAccessScope(t *testing.T) {
	if partition, ok := ForSource(queue.NewGitHubAdapter(), queue.Source{ID: "github", Locator: "owner/repo", Adapter: "github"}); ok || partition != (Partition{}) {
		t.Fatalf("GitHub without provider scope fingerprint was cached: %+v", partition)
	}
}

func TestCacheDir(t *testing.T) {
	got, err := CacheDir(func(string) string { return "" }, "/home/u")
	if err != nil || got != "/home/u/.cache/worklease/queue" {
		t.Fatalf("%q %v", got, err)
	}
}
