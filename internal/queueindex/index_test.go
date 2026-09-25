package queueindex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/testkit"
)

type inaccessibleAdapter struct{ ref queue.Ref }

func (a inaccessibleAdapter) Resolve(context.Context, map[string]string) (queue.Source, error) {
	return queue.Source{ID: a.ref.SourceID, Adapter: "inaccessible"}, nil
}
func (inaccessibleAdapter) Capabilities(context.Context, queue.Source, string, *queue.Ref) (queue.CapabilitySet, error) {
	return queue.CapabilitySet{}, nil
}
func (a inaccessibleAdapter) List(context.Context, queue.Source, queue.Query, string) (queue.SummaryPage, error) {
	return queue.SummaryPage{Items: []queue.Summary{{Ref: a.ref, Title: "revoked"}}, Coverage: queue.Coverage{State: queue.CoverageComplete, TotalAccuracy: queue.TotalExact}}, nil
}
func (a inaccessibleAdapter) ReadItems(context.Context, queue.Source, []queue.Ref, []string, int) []queue.ItemOutcome {
	return []queue.ItemOutcome{{Ref: a.ref, Kind: "inaccessible"}}
}
func (inaccessibleAdapter) ReadDependencies(context.Context, queue.Source, queue.Ref, string, int) (queue.DependencyPage, error) {
	return queue.DependencyPage{Completeness: queue.CoverageComplete}, nil
}

func TestAdapterInaccessibilityPurgesIndexProjection(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	ref := queue.Ref{SourceID: "s", ItemID: "secret"}
	p := Partition{Source: "s", Principal: "u", Scope: "r", Generation: "g"}
	cached := queue.Item{Summary: queue.Summary{Ref: ref, Title: "secretcached"}}
	if err := idx.Replace(ctx, p, []queue.Item{cached}, true); err != nil {
		t.Fatal(err)
	}
	registry := queue.NewRegistry()
	adapter := inaccessibleAdapter{ref: ref}
	if err := registry.Register("inaccessible", adapter); err != nil {
		t.Fatal(err)
	}
	loader := queue.NewLoader(registry)
	for range loader.Refresh(ctx, []queue.Source{{ID: "s", Adapter: "inaccessible"}}) {
	}
	snapshot := loader.Store.Current()
	if snapshot.Deleted[ref.Key()] != ref {
		t.Fatalf("adapter evidence did not mark the removed ref: %+v", snapshot.Deleted)
	}
	if err := idx.ReplaceWithDeletes(ctx, p, nil, []queue.Ref{snapshot.Deleted[ref.Key()]}, false); err != nil {
		t.Fatal(err)
	}
	got, _, _, err := idx.Read(ctx, p, time.Hour)
	if err != nil || len(got.Items) != 0 {
		t.Fatalf("inaccessible row remained cached: %+v %v", got, err)
	}
	refs, _, err := idx.Search(ctx, p, "secretcached")
	if err != nil || len(refs) != 0 {
		t.Fatalf("inaccessible search row remained cached: %v %v", refs, err)
	}
}

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

func TestGitHubWithheldItemRetainsUnknownIdentity(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	p := Partition{Source: "github", Principal: "alice", Scope: "origin/repo", Generation: "g1"}
	ref := queue.Ref{SourceID: "github", ItemID: "1"}
	item := queue.Item{Summary: queue.Summary{Ref: ref, CanonicalID: "node-1", Title: "private"}}
	if err := idx.CommitGitHubSyncPage(ctx, p, []queue.Item{item}, "", time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if err := idx.WithholdGitHubItems(ctx, p, []queue.Ref{ref}); err != nil {
		t.Fatal(err)
	}
	got, _, _, err := idx.Read(ctx, p, time.Hour)
	if err != nil || len(got.Items) != 0 {
		t.Fatalf("stale payload visible after access loss: %+v %v", got.Items, err)
	}
	absences, err := idx.GitHubAbsences(ctx, p)
	if err != nil || absences[ref] != "unknown" {
		t.Fatalf("404 was treated as conclusive deletion or lost identity: %+v %v", absences, err)
	}
}

func TestGitHubConfirmedAccessLossClassifiesInaccessible(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	p := Partition{Source: "github", Principal: "alice", Scope: "origin/repo", Generation: "g1"}
	ref := queue.Ref{SourceID: "github", ItemID: "1"}
	item := queue.Item{Summary: queue.Summary{Ref: ref, CanonicalID: "node-1", Title: "private"}}
	if err := idx.CommitGitHubSyncPage(ctx, p, []queue.Item{item}, "", time.Now(), true); err != nil {
		t.Fatal(err)
	}
	if err := idx.WithholdGitHubItems(ctx, p, nil); err != nil {
		t.Fatal(err)
	}
	if err := idx.MarkGitHubInaccessible(ctx, p); err != nil {
		t.Fatal(err)
	}
	absences, err := idx.GitHubAbsences(ctx, p)
	if err != nil || absences[ref] != "inaccessible" {
		t.Fatalf("confirmed access loss: %+v %v", absences, err)
	}
	if err := idx.CommitGitHubSyncPage(ctx, p, []queue.Item{item}, "", time.Now(), true); err != nil {
		t.Fatal(err)
	}
	absences, err = idx.GitHubAbsences(ctx, p)
	if err != nil || len(absences) != 0 {
		t.Fatalf("restored access did not clear classification: %+v %v", absences, err)
	}
}

func TestGitHubSyncResumesAfterReopenWithMovedNode(t *testing.T) {
	ctx := context.Background()
	indexDir := t.TempDir()
	p := Partition{Source: "github", Principal: "alice", Scope: "origin/repo", Generation: "g1"}
	firstWatermark := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	secondWatermark := firstWatermark.Add(time.Hour)
	idx, err := Open(ctx, indexDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.CommitGitHubSyncPage(ctx, p, nil, "", firstWatermark, true); err != nil {
		t.Fatal(err)
	}
	old := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "github", ItemID: "1"}, CanonicalID: "node-1", Title: "before move"}}
	if err := idx.CommitGitHubSyncPage(ctx, p, []queue.Item{old}, "next", secondWatermark, false); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	idx, err = Open(ctx, indexDir)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	state, err := idx.ReadGitHubSyncState(ctx, p)
	if err != nil || state.Cursor != "next" || !state.CommittedWatermark.Equal(firstWatermark) || !state.ScanWatermark.Equal(secondWatermark) {
		t.Fatalf("recovered page and watermark: %+v %v", state, err)
	}
	moved := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "github", ItemID: "2"}, CanonicalID: "node-1", Title: "after move"}}
	if err := idx.CommitGitHubSyncPage(ctx, p, []queue.Item{moved}, "", state.ScanWatermark, true); err != nil {
		t.Fatal(err)
	}
	state, err = idx.ReadGitHubSyncState(ctx, p)
	if err != nil || state.Cursor != "" || !state.CommittedWatermark.Equal(secondWatermark) {
		t.Fatalf("completed window: %+v %v", state, err)
	}
	got, _, _, err := idx.Read(ctx, p, time.Hour)
	if err != nil || len(got.Items) != 1 || got.Items[moved.Ref.Key()].Title != "after move" {
		t.Fatalf("moved node after recovery: %+v %v", got.Items, err)
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
	absences, err := idx.GitHubAbsences(ctx, p)
	if err != nil || absences[old.Ref] != "unknown" {
		t.Fatalf("absent node was classified as deletion without proof: %+v %v", absences, err)
	}
	if err := idx.CommitGitHubSyncPage(ctx, p, []queue.Item{old}, "", time.Now(), true); err != nil {
		t.Fatal(err)
	}
	absences, err = idx.GitHubAbsences(ctx, p)
	if err != nil || len(absences) != 0 {
		t.Fatalf("reappearing node retained an absence: %+v %v", absences, err)
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
	absences, err := idx.GitHubAbsences(ctx, p)
	if err != nil || absences[first.Ref] != "moved" {
		t.Fatalf("moved node lost its recovery evidence: %+v %v", absences, err)
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

func TestBacklogBranchSwitchDoesNotReuseFreshCachePartition(t *testing.T) {
	ctx := context.Background()
	checkout := t.TempDir()
	if output, err := testkit.GitCommand("init", "-b", "main", checkout).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if output, err := testkit.GitCommand("-C", checkout, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(checkout, "backlog.config.yml"), []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	registry := queue.NewRegistry()
	adapter, _ := registry.Get("backlog-md")
	source := queue.Source{ID: "backlog", Adapter: "backlog-md", Locator: checkout}
	mainPartition, ok := ForSource(adapter, source)
	if !ok {
		t.Fatal("initial branch did not provide a cache identity")
	}
	idx, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	item := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: source.ID, ItemID: "main-task"}, Title: "main task"}}
	if err := idx.Replace(ctx, mainPartition, []queue.Item{item}, true); err != nil {
		t.Fatal(err)
	}
	if output, err := testkit.GitCommand("-C", checkout, "checkout", "-b", "other").CombinedOutput(); err != nil {
		t.Fatalf("git branch switch: %v: %s", err, output)
	}
	otherPartition, ok := ForSource(adapter, source)
	if !ok || otherPartition == mainPartition {
		t.Fatal("branch switch reused the previous cache partition")
	}
	got, _, fresh, err := idx.Read(ctx, otherPartition, time.Hour)
	if err != nil || len(got.Items) != 0 || fresh {
		t.Fatalf("branch switch reused fresh rows from the previous branch: %+v fresh=%v err=%v", got, fresh, err)
	}
}

func TestRetentionInvalidatesPartitionFreshness(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	p := Partition{Source: "s", Principal: "u", Scope: "scope", Generation: "g"}
	item := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "s", ItemID: "old"}, Title: "expired"}, Observation: queue.Observation{ObservedAt: time.Now()}}
	if err := idx.Replace(ctx, p, []queue.Item{item}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.db.Exec("UPDATE entries SET observed=?", time.Now().Add(-Retention-time.Hour).UnixNano()); err != nil {
		t.Fatal(err)
	}
	got, _, fresh, err := idx.Read(ctx, p, 60*24*time.Hour)
	if err != nil || len(got.Items) != 0 || fresh || got.Sources[p.Source].State != queue.CoveragePartial {
		t.Fatalf("expired cache remained fresh: %+v fresh=%v err=%v", got, fresh, err)
	}
}

func TestBodyOptInNeedsCompleteReindex(t *testing.T) {
	ctx := context.Background()
	idx, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	p := Partition{Source: "s", Principal: "u", Scope: "scope", Generation: "g"}
	old := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "s", ItemID: "old"}, Title: "oldsummary"}, Body: "oldbody"}
	if err := idx.Replace(ctx, p, []queue.Item{old}, true); err != nil {
		t.Fatal(err)
	}
	idx.EnableBodyIndex()
	newItem := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "s", ItemID: "new"}, Title: "newsummary"}, Body: "newbody"}
	if err := idx.Replace(ctx, p, []queue.Item{newItem}, false); err != nil {
		t.Fatal(err)
	}
	refs, coverage, err := idx.Search(ctx, p, "oldbody")
	if err != nil || len(refs) != 0 || coverage != "indexed-summaries" {
		t.Fatalf("incomplete body coverage reported: %v %s %v", refs, coverage, err)
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
	_, err = marker.WriteString(strconv.Itoa(os.Getpid()) + "\n")
	marker.Close()
	if err != nil {
		t.Fatal(err)
	}
	// Keep the winner alive until the parent has observed the competing helpers.
	if _, err := io.ReadAll(os.Stdin); err != nil {
		t.Fatal(err)
	}
}

func TestLockIsSingleFlightAcrossProcesses(t *testing.T) {
	dir := t.TempDir()
	// Initialize the index before racing the lock: concurrent schema setup
	// exercises a different boundary and can fail with SQLITE_BUSY.
	idx, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "winners")
	cmds := make([]*exec.Cmd, 8)
	outputs := make([]strings.Builder, len(cmds))
	for n := range cmds {
		cmds[n] = exec.Command(os.Args[0], "-test.run=^TestLockProcessHelper$")
		cmds[n].Env = append(os.Environ(), "QUEUE_INDEX_LOCK_HELPER=1", "QUEUE_INDEX_DIR="+dir, "QUEUE_INDEX_MARKER="+marker)
		cmds[n].Stdout = &outputs[n]
		cmds[n].Stderr = &outputs[n]
	}
	writers := make([]*io.PipeWriter, len(cmds))
	for n, cmd := range cmds {
		cmd.Stdin, writers[n] = io.Pipe()
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	// The winner stays alive while every losing process finishes its attempt.
	deadline := time.After(2 * time.Second)
	var winner int
	for {
		data, err := os.ReadFile(marker)
		if err == nil {
			winner, _ = strconv.Atoi(strings.TrimSpace(string(data)))
		}
		if winner != 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("no helper acquired the lock")
		default:
			runtime.Gosched()
		}
	}
	for index, cmd := range cmds {
		if cmd.Process.Pid == winner {
			continue
		}
		_ = writers[index].Close()
		if err := cmd.Wait(); err != nil {
			t.Fatalf("helper %d failed: %v: %s", index, err, outputs[index].String())
		}
	}
	for index, cmd := range cmds {
		_ = writers[index].Close()
		if cmd.Process.Pid == winner {
			if err := cmd.Wait(); err != nil {
				t.Fatalf("winner failed: %v: %s", err, outputs[index].String())
			}
		}
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(strings.Fields(string(data))); got != 1 {
		t.Fatalf("cross-process lock winners=%d output=%q", got, data)
	}
}

func TestBusyOpenDoesNotRebuildLiveIndex(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	first, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	p := Partition{Source: "s", Principal: "u", Scope: "r", Generation: "g"}
	item := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "s", ItemID: "before"}, Title: "before"}}
	if err := first.Replace(ctx, p, []queue.Item{item}, true); err != nil {
		t.Fatal(err)
	}
	conn, err := first.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	secondOpen := make(chan struct {
		index *Index
		err   error
	}, 1)
	busyCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	go func() {
		index, openErr := openWithBusyTimeout(busyCtx, dir, true, 100)
		secondOpen <- struct {
			index *Index
			err   error
		}{index, openErr}
	}()
	select {
	case result := <-secondOpen:
		if result.index != nil {
			_ = result.index.Close()
		}
		if result.err == nil {
			t.Error("second Open unexpectedly succeeded under write lock")
		} else if errors.Is(result.err, context.DeadlineExceeded) {
			t.Fatalf("second Open waited past the test's busy timeout: %v", result.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second Open did not honor its context deadline")
	}
	committed := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "s", ItemID: "committed"}, Title: "committed"}}
	payload, err := json.Marshal(committed)
	if err != nil {
		t.Fatal(err)
	}
	key, err := p.key()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "INSERT INTO entries(partition,ref,payload,observed) VALUES(?,?,?,?)", key, committed.Ref.Key(), payload, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	check, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	got, _, _, err := check.Read(ctx, p, time.Hour)
	if err != nil || len(got.Items) != 2 || got.Items[committed.Ref.Key()].Title != "committed" {
		t.Fatalf("writer commit disappeared after busy Open: %+v %v", got, err)
	}
}

func TestDamagedGenerationTwoSchemaIsRebuilt(t *testing.T) {
	dir := t.TempDir()
	idx, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = idx.db.Exec("DROP TABLE entries"); err != nil {
		t.Fatal(err)
	}
	idx.Close()
	idx, err = Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if _, _, _, err := idx.Read(context.Background(), Partition{Source: "s", Principal: "u", Scope: "r", Generation: "g"}, time.Hour); err != nil {
		t.Fatalf("damaged schema was not rebuilt: %v", err)
	}
}

func TestLinearSyncSchemaMigratesFromSeven(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	idx, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = idx.db.ExecContext(ctx, `DROP TABLE linear_sync; PRAGMA user_version=7`); err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	idx, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	var version int
	if err = idx.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || version != SchemaGeneration {
		t.Fatalf("schema generation: %d %v", version, err)
	}
	if _, err = idx.db.ExecContext(ctx, `SELECT relation_offset FROM linear_sync`); err != nil {
		t.Fatalf("missing Linear sync table: %v", err)
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
