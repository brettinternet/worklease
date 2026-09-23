package queueindex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/brettinternet/worklease/internal/queue"
)

func BenchmarkIndexedSearch10000(b *testing.B) {
	ctx := context.Background()
	directory := b.TempDir()
	index, err := Open(ctx, directory)
	if err != nil {
		b.Fatal(err)
	}
	defer index.Close()
	partition := Partition{Source: "fixture", Principal: "tester", Scope: "local", Generation: "1"}
	items := make([]queue.Item, 10000)
	for i := range items {
		items[i] = queue.Item{Summary: queue.Summary{
			Ref:   queue.Ref{SourceID: partition.Source, ItemID: fmt.Sprintf("%06d", i)},
			Title: fmt.Sprintf("Fixture searchable issue %06d", i),
		}}
	}
	if err := index.Replace(ctx, partition, items, true); err != nil {
		b.Fatal(err)
	}
	var size int64
	entries, err := os.ReadDir(directory)
	if err != nil {
		b.Fatal(err)
	}
	for _, entry := range entries {
		info, err := os.Stat(filepath.Join(directory, entry.Name()))
		if err == nil && info.Mode().IsRegular() {
			size += info.Size()
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		refs, _, err := index.Search(ctx, partition, "000001")
		if err != nil || len(refs) != 1 {
			b.Fatalf("search found %d: %v", len(refs), err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(size), "index-bytes")
}
