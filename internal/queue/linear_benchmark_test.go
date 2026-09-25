package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
)

// BenchmarkLinearSync10000 runs the real adapter against a deterministic
// 10k-issue GraphQL fixture. It measures one complete issue traversal plus a
// bounded 50-item independent relation sweep (two directions per item). The
// fixture is local: numbers are not production API latency or complexity cost.
func BenchmarkLinearSync10000(b *testing.B) {
	const total, pageSize, relationSweep = 10000, 250, 50
	var lists, relations atomic.Int64
	issueID := func(i int) string { return fmt.Sprintf("%08x-0000-4000-8000-%012x", i+1, i+1) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string
			Variables map[string]json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			b.Error(err)
			w.WriteHeader(400)
			return
		}
		var data any
		switch request.Query {
		case linearIdentityQuery:
			data = linearIdentityFixture(request.Query)
		case linearListQuery:
			lists.Add(1)
			start := 0
			if string(request.Variables["after"]) != "null" {
				var cursor string
				_ = json.Unmarshal(request.Variables["after"], &cursor)
				start, _ = strconv.Atoi(cursor)
			}
			nodes := make([]any, 0, pageSize)
			for i := start; i < start+pageSize && i < total; i++ {
				nodes = append(nodes, linearIssueFixture(issueID(i), "unstarted"))
			}
			data = map[string]any{"team": map[string]any{"id": linearTestTeam, "issues": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": start+pageSize < total, "endCursor": strconv.Itoa(start + pageSize)}}}}
		case linearRelationsQuery, linearInverseQuery:
			relations.Add(1)
			field := "relations"
			if request.Query == linearInverseQuery {
				field = "inverseRelations"
			}
			var id string
			_ = json.Unmarshal(request.Variables["id"], &id)
			data = map[string]any{"issue": map[string]any{"id": id, "team": map[string]any{"id": linearTestTeam}, field: map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false}}}}
		default:
			b.Errorf("unexpected query")
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	adapter := NewLinearAdapter()
	adapter.APIBase = server.URL
	ctx := context.Background()
	source, err := adapter.Resolve(ctx, map[string]string{"id": "linear-benchmark", "organization": linearTestOrg, "team": linearTestTeam, "account": linearTestViewer, "credentialHelper": `["/bin/echo","fixture-token"]`})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		cursor, seen := "", 0
		for {
			page, err := adapter.List(ctx, source, Query{Budget: pageSize}, cursor)
			if err != nil {
				b.Fatal(err)
			}
			seen += len(page.Items)
			cursor = page.NextCursor
			if cursor == "" {
				break
			}
		}
		if seen != total {
			b.Fatalf("enumerated %d/%d", seen, total)
		}
		for i := 0; i < relationSweep; i++ {
			cursor := ""
			for {
				page, err := adapter.ReadDependencies(ctx, source, Ref{SourceID: source.ID, ItemID: issueID(i)}, cursor, pageSize)
				if err != nil {
					b.Fatal(err)
				}
				cursor = page.NextCursor
				if cursor == "" {
					break
				}
			}
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(lists.Load())/float64(b.N), "list-requests/op")
	b.ReportMetric(float64(relations.Load())/float64(b.N), "relation-requests/op")
	b.ReportMetric(total, "issues/op")
}
