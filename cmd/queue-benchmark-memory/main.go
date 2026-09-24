// queue-benchmark-memory measures the queue model in a standalone process so
// the parent's Go test harness does not inflate resident-memory measurements.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: queue-benchmark-memory SUMMARY_COUNT")
		os.Exit(2)
	}
	n, err := strconv.Atoi(os.Args[1])
	if err != nil || (n != 10000 && n != 50000 && n != 100000) {
		fmt.Fprintln(os.Stderr, "SUMMARY_COUNT must be 10000, 50000 or 100000")
		os.Exit(2)
	}
	items := make(map[string]queue.Item, n)
	coverage := make(map[string]queue.Coverage, 5)
	sources := make([]queue.Source, 5)
	for i := range sources {
		sources[i] = queue.Source{ID: fmt.Sprintf("source-%d", i)}
		coverage[sources[i].ID] = queue.Coverage{State: queue.CoverageComplete, Total: n / 5, TotalAccuracy: queue.TotalExact}
	}
	for i := 0; i < n; i++ {
		ref := queue.Ref{SourceID: sources[i%5].ID, ItemID: fmt.Sprintf("%06d", i)}
		items[ref.Key()] = queue.Item{Summary: queue.Summary{Ref: ref, CanonicalID: ref.Key(), Title: fmt.Sprintf("Fixture issue %06d", i), RawStatus: "open", Fresh: true, Order: ref.ItemID}}
	}
	snapshot := queue.Snapshot{Items: items, Sources: coverage}
	model := queueui.New(snapshot)
	model.Sources = sources
	model.ViewFilters = make(map[string]queue.Filters, len(model.Views))
	model.ViewRules = make(map[string]queueui.ViewRule, len(model.Views))
	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		sourceIDs = append(sourceIDs, source.ID)
	}
	for _, name := range model.Views {
		model.ViewFilters[name] = queue.Filters{SourceIDs: append([]string(nil), sourceIDs...)}
		model.ViewRules[name] = queueui.ViewRule{}
	}
	start := time.Now()
	_ = model.View()
	first := time.Since(start)
	snapshot.Revision++
	prepared := queueui.PrepareSnapshotForModel(snapshot, model)
	start = time.Now()
	updated, _ := model.Update(prepared)
	model = updated.(queueui.Model)
	_ = model.View()
	refresh := time.Since(start)
	runtime.KeepAlive(model)
	if err := json.NewEncoder(os.Stdout).Encode(map[string]float64{"first_view_ms": float64(first) / float64(time.Millisecond), "refresh_ms": float64(refresh) / float64(time.Millisecond)}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
