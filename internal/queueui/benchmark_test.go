package queueui

import (
	"fmt"
	"testing"

	"github.com/brettinternet/worklease/internal/queue"
	tea "github.com/charmbracelet/bubbletea"
)

// benchSnapshot is deterministic and shared by the fixed-size scale benchmarks.
// The five source identities repeat in fixed order across the fixture.
func benchSnapshot(n int) queue.Snapshot {
	items := make(map[string]queue.Item, n)
	coverage := make(map[string]queue.Coverage, 5)
	for i := 0; i < n; i++ {
		source := fmt.Sprintf("source-%d", i%5)
		ref := queue.Ref{SourceID: source, ItemID: fmt.Sprintf("%06d", i)}
		items[ref.Key()] = queue.Item{Summary: queue.Summary{
			Ref: ref, CanonicalID: ref.Key(), Title: fmt.Sprintf("Fixture issue %06d", i),
			RawStatus: "open", Fresh: true, Order: fmt.Sprintf("%06d", i),
		}}
	}
	for i := 0; i < 5; i++ {
		coverage[fmt.Sprintf("source-%d", i)] = queue.Coverage{State: queue.CoverageComplete, Total: n / 5, TotalAccuracy: queue.TotalExact}
	}
	return queue.Snapshot{Items: items, Sources: coverage}
}

func BenchmarkQueueInputToRender(b *testing.B) {
	for _, size := range []int{10000, 50000, 100000} {
		b.Run(fmt.Sprintf("items-%d", size), func(b *testing.B) {
			m := New(benchSnapshot(size))
			m.Sources = []queue.Source{{ID: "source-0"}, {ID: "source-1"}, {ID: "source-2"}, {ID: "source-3"}, {ID: "source-4"}}
			key := tea.KeyMsg{Type: tea.KeyDown}
			_ = m.View() // Populate the warm projection before timing navigation.
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				next, _ := m.Update(key)
				m = next.(Model)
				_ = m.View()
				if m.rowCache.rows == nil {
					b.Fatal("missing cached rows")
				}
			}
		})
	}
}

func BenchmarkQueueWarmFirstView(b *testing.B) {
	snapshot := benchSnapshot(10000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = New(snapshot).View()
	}
}
