package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkGitHubEnumeration exercises the real adapter against a deterministic,
// latency-injected fake GraphQL endpoint. Authentication requests are excluded
// from the data request counter; each data page contains 100 distinct issues.
func BenchmarkGitHubEnumeration(b *testing.B) {
	const total = 10000
	const latency = 5 * time.Millisecond
	var requests, bytesSent, quotaPoints atomic.Int64
	a, _ := fakeGitHub(b, func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string                     `json:"query"`
			Variables map[string]json.RawMessage `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			b.Error(err)
			return
		}
		if strings.Contains(req.Query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		requests.Add(1)
		// The fake charges one point per page. This is an injected fixture
		// cost, not a measurement of GitHub's production GraphQL formula.
		quotaPoints.Add(1)
		time.Sleep(latency)
		start := 0
		if value := req.Variables["after"]; len(value) > 0 && string(value) != "null" {
			var cursor string
			if err := json.Unmarshal(value, &cursor); err != nil {
				b.Error(err)
				return
			}
			start, _ = strconv.Atoi(cursor)
		}
		if start < 0 || start >= total {
			b.Errorf("invalid cursor %d", start)
			return
		}
		var result strings.Builder
		result.WriteString(`{"data":{"rateLimit":{"cost":1,"remaining":5000},"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":10000,"nodes":[`)
		for i := start; i < start+100; i++ {
			if i > start {
				result.WriteByte(',')
			}
			fmt.Fprintf(&result, `{"id":"fixture-%d","number":%d,"title":"Fixture issue %06d","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}`, i, i+1, i)
		}
		if start+100 < total {
			fmt.Fprintf(&result, `],"pageInfo":{"hasNextPage":true,"endCursor":"%d"}}}}}`, start+100)
		} else {
			result.WriteString(`],"pageInfo":{"hasNextPage":false}}}}}`)
		}
		bytesSent.Add(int64(result.Len()))
		fmt.Fprint(w, result.String())
	})
	ctx := context.Background()
	source, err := a.Resolve(ctx, map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		cursor := ""
		seen := 0
		for {
			page, err := a.List(ctx, source, Query{Budget: 100}, cursor)
			if err != nil {
				b.Fatal(err)
			}
			seen += len(page.Items)
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		if seen != total {
			b.Fatalf("enumerated %d of %d", seen, total)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(requests.Load())/float64(b.N), "requests/op")
	b.ReportMetric(float64(bytesSent.Load())/float64(b.N), "bytes/op")
	b.ReportMetric(float64(quotaPoints.Load())/float64(b.N), "fixture-quota-points/op")
}
