package queueui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/queue"
)

// BenchmarkGitHubFirstPageToRender times the real adapter's first remote page
// through a bounded TUI View. It does not include terminal paint or full sync.
func BenchmarkGitHubFirstPageToRender(b *testing.B) {
	const latency = 5 * time.Millisecond
	var requests, bytesSent atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			b.Error(err)
			return
		}
		if strings.Contains(request.Query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		requests.Add(1)
		time.Sleep(latency)
		var result strings.Builder
		result.WriteString(`{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":10000,"nodes":[`)
		for i := 0; i < 100; i++ {
			if i > 0 {
				result.WriteByte(',')
			}
			fmt.Fprintf(&result, `{"id":"fixture-%d","number":%d,"title":"Fixture issue %06d","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}`, i, i+1, i)
		}
		result.WriteString(`],"pageInfo":{"hasNextPage":true,"endCursor":"100"}}}}}`)
		bytesSent.Add(int64(result.Len()))
		fmt.Fprint(w, result.String())
	}))
	defer server.Close()
	binary := filepath.Join(b.TempDir(), "gh")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n[ \"$1\" = auth ] && [ \"$2\" = token ] && [ \"$3\" = --hostname ] && [ \"$4\" = github.com ] && [ \"$5\" = --user ] && [ \"$6\" = tester ] || exit 3\nprintf 'fixture-token\\n'\n"), 0700); err != nil {
		b.Fatal(err)
	}
	adapter := queue.NewGitHubAdapter()
	adapter.Binary = binary
	adapter.APIBase = server.URL
	ctx := context.Background()
	source, err := adapter.Resolve(ctx, map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page, err := adapter.List(ctx, source, queue.Query{Budget: 100}, "")
		if err != nil {
			b.Fatal(err)
		}
		if len(page.Items) != 100 || page.NextCursor == "" {
			b.Fatalf("expected a partial first page, got %d items and cursor %q", len(page.Items), page.NextCursor)
		}
		items := make(map[string]queue.Item, len(page.Items))
		for _, item := range page.Items {
			items[item.Ref.Key()] = queue.Item{Summary: item}
		}
		model := New(queue.Snapshot{Items: items, Sources: map[string]queue.Coverage{source.ID: {State: queue.CoveragePartial}}})
		_ = model.View()
	}
	b.StopTimer()
	b.ReportMetric(float64(requests.Load())/float64(b.N), "requests/op")
	b.ReportMetric(float64(bytesSent.Load())/float64(b.N), "bytes/op")
}
