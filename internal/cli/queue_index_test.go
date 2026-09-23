package cli

import (
	"context"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueindex"
)

type inaccessibleSourceAdapter struct{ ref queue.Ref }

func (a inaccessibleSourceAdapter) Resolve(context.Context, map[string]string) (queue.Source, error) {
	return queue.Source{ID: a.ref.SourceID, Adapter: "inaccessible"}, nil
}
func (inaccessibleSourceAdapter) Capabilities(context.Context, queue.Source, string, *queue.Ref) (queue.CapabilitySet, error) {
	return queue.CapabilitySet{}, nil
}
func (a inaccessibleSourceAdapter) List(context.Context, queue.Source, queue.Query, string) (queue.SummaryPage, error) {
	return queue.SummaryPage{Items: []queue.Summary{{Ref: a.ref, Title: "revoked"}}, Coverage: queue.Coverage{State: queue.CoverageComplete, TotalAccuracy: queue.TotalExact}}, nil
}
func (a inaccessibleSourceAdapter) ReadItems(context.Context, queue.Source, []queue.Ref, []string, int) []queue.ItemOutcome {
	return []queue.ItemOutcome{{Ref: a.ref, Kind: "inaccessible"}}
}
func (inaccessibleSourceAdapter) ReadDependencies(context.Context, queue.Source, queue.Ref, string, int) (queue.DependencyPage, error) {
	return queue.DependencyPage{Completeness: queue.CoverageComplete}, nil
}

func TestAdapterInaccessibilityPurgesQueueIndexProjection(t *testing.T) {
	ctx := context.Background()
	index, err := queueindex.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	ref := queue.Ref{SourceID: "source", ItemID: "secret"}
	partition := queueindex.Partition{Source: "source", Principal: "user", Scope: "repo", Generation: "generation"}
	cached := queue.Item{Summary: queue.Summary{Ref: ref, Title: "secretcached"}}
	if err := index.Replace(ctx, partition, []queue.Item{cached}, true); err != nil {
		t.Fatal(err)
	}
	registry := queue.NewRegistry()
	adapter := inaccessibleSourceAdapter{ref: ref}
	if err := registry.Register("inaccessible", adapter); err != nil {
		t.Fatal(err)
	}
	source := queue.Source{ID: ref.SourceID, Adapter: "inaccessible"}
	loader := queue.NewLoader(registry)
	for range loader.Refresh(ctx, []queue.Source{source}) {
	}
	snapshot := loader.Store.Current()
	if snapshot.Deleted[ref.Key()] != ref {
		t.Fatalf("adapter outcome did not mark the cached ref removed: %+v", snapshot.Deleted)
	}
	if err := replaceQueueIndexSnapshot(ctx, index, partition, source.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	got, _, _, err := index.Read(ctx, partition, time.Hour)
	if err != nil || len(got.Items) != 0 {
		t.Fatalf("inaccessible row remained cached: %+v %v", got, err)
	}
	refs, _, err := index.Search(ctx, partition, "secretcached")
	if err != nil || len(refs) != 0 {
		t.Fatalf("inaccessible search row remained cached: %v %v", refs, err)
	}
}
