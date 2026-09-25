package queue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/testkit"
)

func TestLinearExhaustedRequestOrComplexityQuotaStopsNextCall(t *testing.T) {
	t.Parallel()
	for _, quota := range []string{"Requests", "Complexity"} {
		t.Run(quota, func(t *testing.T) {
			t.Parallel()
			testkit.Home(t)
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct{ Query string }
				_ = json.NewDecoder(r.Body).Decode(&request)
				if request.Query == linearIdentityQuery {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": linearIdentityFixture(request.Query)})
					return
				}
				count.Add(1)
				w.Header().Set("X-RateLimit-"+quota+"-Remaining", "0")
				w.Header().Set("X-RateLimit-"+quota+"-Reset", strconv.FormatInt(time.Now().Add(time.Minute).UnixMilli(), 10))
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"team": map[string]any{"id": linearTestTeam, "issues": map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false}}}}})
			}))
			defer server.Close()
			adapter := NewLinearAdapter()
			adapter.APIBase = server.URL
			source, err := adapter.Resolve(context.Background(), map[string]string{"id": "linear-test", "organization": linearTestOrg, "team": linearTestTeam, "account": linearTestViewer, "credentialHelper": `["/bin/echo","fixture-token"]`})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = adapter.List(context.Background(), source, Query{}, ""); err != nil {
				t.Fatal(err)
			}
			if _, err = adapter.List(context.Background(), source, Query{}, ""); err == nil {
				t.Fatal("exhausted quota allowed another read")
			}
			if count.Load() != 1 {
				t.Fatalf("provider saw %d list reads after exhausted quota", count.Load())
			}
		})
	}
}
