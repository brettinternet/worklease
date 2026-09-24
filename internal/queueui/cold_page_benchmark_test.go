package queueui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/queue"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
)

// TestQueueColdPageOutputLatency observes the first real adapter page in a
// live renderer. It does not measure physical terminal paint or full sync.
func TestQueueColdPageOutputLatency(t *testing.T) {
	count, err := strconv.Atoi(os.Getenv("QUEUE_COLD_PAGE_SAMPLES"))
	if err != nil || count < 1 {
		t.Skip("set QUEUE_COLD_PAGE_SAMPLES to opt in")
	}
	var requests, bytesSent atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if strings.Contains(request.Query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		requests.Add(1)
		time.Sleep(5 * time.Millisecond)
		var result strings.Builder
		result.WriteString(`{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":10000,"nodes":[`)
		for i := 0; i < 100; i++ {
			if i > 0 {
				result.WriteByte(',')
			}
			fmt.Fprintf(&result, `{"id":"fixture-%d","number":%d,"title":"Cold issue %06d","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}`, i, i+1, i)
		}
		result.WriteString(`],"pageInfo":{"hasNextPage":true,"endCursor":"100"}}}}}`)
		bytesSent.Add(int64(result.Len()))
		fmt.Fprint(w, result.String())
	}))
	defer server.Close()
	binary := filepath.Join(t.TempDir(), "gh")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf 'fixture-token\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	adapter := queue.NewGitHubAdapter()
	adapter.Binary, adapter.APIBase = binary, server.URL
	ctx := context.Background()
	source, err := adapter.Resolve(ctx, map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(master, &pty.Winsize{Rows: 35, Cols: 120}); err != nil {
		t.Fatal(err)
	}
	chunks := make(chan []byte, 256)
	go func() {
		var buf [8192]byte
		for {
			n, err := master.Read(buf[:])
			if n > 0 {
				chunks <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				close(chunks)
				return
			}
		}
	}()
	model := New(queue.Snapshot{Items: map[string]queue.Item{}, Sources: map[string]queue.Coverage{source.ID: {State: queue.CoveragePartial}}})
	model.Sources = []queue.Source{source}
	program := tea.NewProgram(model, tea.WithInput(slave), tea.WithOutput(slave), tea.WithoutSignalHandler())
	done := make(chan error, 1)
	go func() { _, err := program.Run(); done <- err }()
	defer func() {
		program.Quit()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("renderer: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("renderer did not stop")
		}
	}()
	awaitTerminalRow(t, chunks, "0 loaded of 0")
	durations := make([]time.Duration, 0, count)
	arrivalToOutput := make([]time.Duration, 0, count)
	for i := 0; i < count; i++ {
		start := time.Now()
		page, err := adapter.List(ctx, source, queue.Query{Budget: 100}, "")
		if err != nil || len(page.Items) != 100 || page.NextCursor == "" {
			t.Fatalf("first page: items=%d cursor=%q error=%v", len(page.Items), page.NextCursor, err)
		}
		arrival := time.Now()
		items := make(map[string]queue.Item, len(page.Items))
		for _, summary := range page.Items {
			items[summary.Ref.Key()] = queue.Item{Summary: summary}
		}
		// A unique marker proves that output belongs to this page, not a
		// previously displayed renderer frame.
		first := page.Items[0].Ref.Key()
		item := items[first]
		item.Title = fmt.Sprintf("COLD%04d", i)
		items[first] = item
		program.Send(PrepareSnapshot(queue.Snapshot{Revision: uint64(i + 1), Items: items, Sources: map[string]queue.Coverage{source.ID: {State: queue.CoveragePartial}}}, source))
		awaitTerminalRow(t, chunks, fmt.Sprintf("COLD%04d", i))
		durations = append(durations, time.Since(start))
		arrivalToOutput = append(arrivalToOutput, time.Since(arrival))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	sort.Slice(arrivalToOutput, func(i, j int) bool { return arrivalToOutput[i] < arrivalToOutput[j] })
	quantile := func(values []time.Duration, percent int) time.Duration { return values[(percent*len(values)+99)/100-1] }
	if requests.Load() != int64(count) {
		t.Fatalf("unexpected data request count: %d", requests.Load())
	}
	t.Logf("cold first-page request-to-PTY-output n=%d p50=%s p95=%s p99=%s; page-arrival-to-output p50=%s p95=%s p99=%s; data requests=%d response bytes=%d (excludes authentication, index publication, physical paint and full enumeration)", count, quantile(durations, 50), quantile(durations, 95), quantile(durations, 99), quantile(arrivalToOutput, 50), quantile(arrivalToOutput, 95), quantile(arrivalToOutput, 99), requests.Load(), bytesSent.Load())
}
