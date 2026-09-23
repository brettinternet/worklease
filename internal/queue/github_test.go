package queue

import (
	"context"
	"encoding/base64"
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
)

func fakeGitHub(t *testing.T, handler http.HandlerFunc) (*GitHubAdapter, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	dir := t.TempDir()
	binary := filepath.Join(dir, "gh")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\n[ \"$1\" = auth ] && [ \"$2\" = token ] && [ \"$3\" = --hostname ] && [ \"$4\" = github.com ] && [ \"$5\" = --user ] && [ \"$6\" = tester ] || exit 3\n[ -z \"$GH_TOKEN$GITHUB_TOKEN$GH_ENTERPRISE_TOKEN$GITHUB_ENTERPRISE_TOKEN\" ] || exit 4\nprintf 'canary-secret-token\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	a := NewGitHubAdapter()
	a.Binary = binary
	a.APIBase = server.URL
	return a, server
}
func githubRequest(t *testing.T, r *http.Request) (string, map[string]json.RawMessage) {
	t.Helper()
	if r.Header.Get("Authorization") != "Bearer canary-secret-token" {
		t.Error("missing credential")
	}
	var req struct {
		Query     string                     `json:"query"`
		Variables map[string]json.RawMessage `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Error(err)
	}
	return req.Query, req.Variables
}
func TestGitHubPrincipalAndTokenSecrecy(t *testing.T) {
	t.Setenv("GH_TOKEN", "wrong")
	t.Setenv("GITHUB_TOKEN", "wrong")
	t.Setenv("GH_ENTERPRISE_TOKEN", "wrong")
	t.Setenv("GITHUB_ENTERPRISE_TOKEN", "wrong")
	var dataCalls atomic.Int32
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		query, _ := githubRequest(t, r)
		if strings.Contains(query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"someone-else"}}}`)
			return
		}
		dataCalls.Add(1)
	})
	_, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if d, ok := err.(GitHubDiagnostic); !ok || d.Code != "authentication" {
		t.Fatalf("unexpected diagnostic: %v", err)
	}
	if dataCalls.Load() != 0 {
		t.Fatal("data requested before principal verification")
	}
	if strings.Contains(fmt.Sprint(err), "canary-secret-token") {
		t.Fatal("credential leaked in error")
	}
	encoded, _ := json.Marshal(err)
	if strings.Contains(string(encoded), "canary-secret-token") {
		t.Fatal("credential leaked in JSON")
	}
}
func TestGitHubCredentialRotationChangesObservationGeneration(t *testing.T) {
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		if strings.Contains(request.Query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`)
	})
	opts := map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"}
	source, err := a.Resolve(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.List(context.Background(), source, Query{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.Binary, []byte("#!/bin/sh\nprintf 'rotated-secret-token\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	source, err = a.Resolve(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.List(context.Background(), source, Query{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Observation.ConfigurationGeneration == second.Observation.ConfigurationGeneration || strings.Contains(second.Observation.ConfigurationGeneration, "rotated-secret-token") {
		t.Fatalf("credential rotation did not safely change generation: %q %q", first.Observation.ConfigurationGeneration, second.Observation.ConfigurationGeneration)
	}
}

func TestGitHubPagesAndRelationships(t *testing.T) {
	var concurrent, peak atomic.Int32
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		current := concurrent.Add(1)
		defer concurrent.Add(-1)
		for {
			p := peak.Load()
			if current <= p || peak.CompareAndSwap(p, current) {
				break
			}
		}
		time.Sleep(time.Millisecond)
		query, vars := githubRequest(t, r)
		if strings.Contains(query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		if strings.Contains(query, "nodes(ids:") {
			fmt.Fprint(w, `{"data":{"nodes":[{"id":"A","number":1,"title":"one","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}]}}`)
			return
		}
		if strings.Contains(query, "blockedBy(") {
			if string(vars["after"]) == `"next"` {
				fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issue":{"number":1,"repository":{"nameWithOwner":"org/repo"},"blockedBy":{"totalCount":2,"nodes":[{"id":"E","number":5,"repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}},"subIssues":{"totalCount":1,"nodes":[{"id":"C","number":3,"repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}}`)
				return
			}
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issue":{"number":1,"repository":{"nameWithOwner":"org/repo"},"blockedBy":{"totalCount":2,"nodes":[{"id":"B","number":2,"state":"CLOSED","repository":{"nameWithOwner":"other/repo"}}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}},"subIssues":{"totalCount":1,"nodes":[{"id":"C","number":3,"repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}}`)
			return
		}
		if strings.Contains(query, "issue(number:") {
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issue":{"id":"A","number":1,"title":"one","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}}}}`)
			return
		}
		if _, ok := vars["after"]; ok && string(vars["after"]) == `"page-2"` {
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":3,"nodes":[{"id":"A","number":1,"title":"one","repository":{"nameWithOwner":"org/repo"}},{"id":"D","number":4,"title":"four","repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
		} else {
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":2,"nodes":[{"id":"A","number":1,"title":"one","repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":true,"endCursor":"page-2"}}}}}`)
		}
	})
	ctx := context.Background()
	source, err := a.Resolve(ctx, map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.List(ctx, source, Query{Budget: 200}, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.List(ctx, source, Query{}, first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if first.Coverage.Total != 2 || second.Coverage.Total != 3 || len(second.Items) != 1 || second.Items[0].CanonicalID != "D" || first.NextCursor == "" || !strings.Contains(githubListQuery, "CREATED_AT") {
		t.Fatalf("bad pages: %+v %+v", first, second)
	}
	other, err := a.Resolve(ctx, map[string]string{"id": "other-custom", "host": "github.com", "repository": "other/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	ref := Ref{source.ID, "1"}
	items := a.ReadItems(ctx, source, []Ref{ref}, nil, 100)
	if items[0].Item == nil {
		t.Fatalf("missing detail: %+v", items)
	}
	deps, err := a.ReadDependencies(ctx, source, ref, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if deps.Completeness != CoveragePartial || deps.NextCursor == "" || deps.Observation.Coverage.Total != 2 || len(deps.Edges) != 2 || deps.Edges[0].Type != CrossSourcePrerequisite || deps.Edges[0].To.SourceID != other.ID || deps.Edges[1].Type != ParentChild {
		t.Fatalf("bad edges: %+v", deps)
	}
	last, err := a.ReadDependencies(ctx, source, ref, deps.NextCursor, 100)
	if err != nil || last.Completeness != CoverageComplete || last.NextCursor != "" || len(last.Edges) != 1 {
		t.Fatalf("last dependency page: %+v %v", last, err)
	}
	if peak.Load() != 1 {
		t.Fatalf("requests overlapped: %d", peak.Load())
	}
}
func TestGitHubListRejectsExpiredCursor(t *testing.T) {
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		query, _ := githubRequest(t, r)
		if strings.Contains(query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
		}
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	cursorData, _ := json.Marshal(struct {
		ID      string `json:"id"`
		After   string `json:"after"`
		Started int64  `json:"started"`
	}{"scan", "after", time.Now().Add(-25 * time.Hour).UnixNano()})
	_, err = a.List(context.Background(), source, Query{}, base64.RawURLEncoding.EncodeToString(cursorData))
	if diagnostic, ok := err.(GitHubDiagnostic); !ok || diagnostic.Code != "invalid-cursor" {
		t.Fatalf("expired cursor error=%v", err)
	}
}

func TestGitHubReadItemsBatchesVisibleNodeHydration(t *testing.T) {
	var nodeQueries atomic.Int32
	var nodeCount atomic.Int32
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		query, vars := githubRequest(t, r)
		if strings.Contains(query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		if strings.Contains(query, "nodes(ids:") {
			nodeQueries.Add(1)
			var ids []string
			_ = json.Unmarshal(vars["ids"], &ids)
			nodeCount.Store(int32(len(ids)))
			fmt.Fprint(w, `{"data":{"nodes":[{"id":"N1","number":1,"title":"one","state":"OPEN","repository":{"nameWithOwner":"org/repo"}},{"id":"N2","number":2,"title":"two","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}]}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":2,"nodes":[{"id":"N1","number":1,"title":"one","state":"OPEN","repository":{"nameWithOwner":"org/repo"}},{"id":"N2","number":2,"title":"two","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.List(context.Background(), source, Query{}, ""); err != nil {
		t.Fatal(err)
	}
	outcomes := a.ReadItems(context.Background(), source, []Ref{{source.ID, "1"}, {source.ID, "2"}}, nil, 100)
	if len(outcomes) != 2 || outcomes[0].Item == nil || outcomes[1].Item == nil || nodeQueries.Load() != 1 || nodeCount.Load() != 2 {
		t.Fatalf("batch results=%+v queries=%d ids=%d", outcomes, nodeQueries.Load(), nodeCount.Load())
	}
}

func TestGitHubNewest304DoesNotHideOlderIncrementalChange(t *testing.T) {
	var hints, scans atomic.Int32
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.URL.Path != "/repos/org/repo/issues" || r.URL.Query().Get("page") != "1" {
				t.Errorf("hint was not scoped to the newest page: %s", r.URL)
			}
			if hints.Add(1) == 1 {
				if r.Header.Get("If-None-Match") != "" {
					t.Error("first hint unexpectedly conditional")
				}
				w.Header().Set("ETag", `"newest-page"`)
				fmt.Fprint(w, `[{"number":1}]`)
				return
			}
			if r.Header.Get("If-None-Match") != `"newest-page"` {
				t.Errorf("hint lost its exact ETag: %q", r.Header.Get("If-None-Match"))
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}
		query, vars := githubRequest(t, r)
		if strings.Contains(query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		scans.Add(1)
		if string(vars["after"]) == `"older"` {
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":2,"nodes":[{"id":"N2","number":2,"title":"older changed","repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
		} else {
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":2,"nodes":[{"id":"N1","number":1,"title":"newest unchanged","repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":true,"endCursor":"older"}}}}}`)
		}
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	committed := time.Now().Add(-time.Hour)
	for range 2 {
		a.pollNewestHint(context.Background(), source)
		first, err := a.ListIncremental(context.Background(), source, Query{}, "", committed, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		older, err := a.ListIncremental(context.Background(), source, Query{}, first.NextCursor, committed, time.Now())
		if err != nil || len(older.Items) != 1 || older.Items[0].Title != "older changed" {
			t.Fatalf("older update hidden by hint: %+v %v", older, err)
		}
	}
	if hints.Load() != 2 || scans.Load() != 4 {
		t.Fatalf("304 suppressed incremental scan: hints=%d scans=%d", hints.Load(), scans.Load())
	}
}

func TestLoaderNewest304StillRefreshesOlderIssue(t *testing.T) {
	var hints, scans atomic.Int32
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if hints.Add(1) > 1 {
				if r.Header.Get("If-None-Match") != `"newest"` {
					t.Errorf("conditional hint lost ETag: %q", r.Header.Get("If-None-Match"))
				}
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", `"newest"`)
			fmt.Fprint(w, `[]`)
			return
		}
		query, vars := githubRequest(t, r)
		switch {
		case strings.Contains(query, "viewer"):
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
		case strings.Contains(query, "nodes(ids:"):
			fmt.Fprint(w, `{"data":{"nodes":[{"id":"N1","number":1,"title":"newest","state":"OPEN","repository":{"nameWithOwner":"org/repo"}},{"id":"N2","number":2,"title":"older changed","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}]}}`)
		case strings.Contains(query, "blockedBy(first:"):
			fmt.Fprintf(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issue":{"number":%s,"repository":{"nameWithOwner":"org/repo"},"blockedBy":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}},"subIssues":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`, vars["number"])
		default:
			if strings.Contains(query, "filterBy:{since:") {
				scans.Add(1)
			}
			if string(vars["after"]) == `"older"` {
				title := "older"
				if hints.Load() > 1 {
					title = "older changed"
				}
				fmt.Fprintf(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":2,"nodes":[{"id":"N2","number":2,"title":%q,"state":"OPEN","repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`, title)
			} else {
				fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":2,"nodes":[{"id":"N1","number":1,"title":"newest","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":true,"endCursor":"older"}}}}}`)
			}
		}
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.adapters["github"] = a
	loader := NewLoader(registry)
	loader.GitHubSync = &syncTestStore{}
	for range 3 {
		drainRefresh(loader.Refresh(context.Background(), []Source{source}))
	}
	older := loader.Store.Current().Items[Ref{source.ID, "2"}.Key()]
	if hints.Load() != 2 || scans.Load() != 4 || older.Title != "older changed" {
		t.Fatalf("304 hid older change: hints=%d scans=%d older=%+v", hints.Load(), scans.Load(), older)
	}
}

func TestLoaderDefersGitHubDetailsAndBatchesVisibleRows(t *testing.T) {
	var batchCalls atomic.Int32
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		query, vars := githubRequest(t, r)
		switch {
		case strings.Contains(query, "viewer"):
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
		case strings.Contains(query, "blockedBy(first:"):
			fmt.Fprintf(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issue":{"number":%s,"repository":{"nameWithOwner":"org/repo"},"blockedBy":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}},"subIssues":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`, vars["number"])
		case strings.Contains(query, "nodes(ids:"):
			batchCalls.Add(1)
			if string(vars["ids"]) != `["N1","N2"]` {
				t.Errorf("unexpected visible batch: %s", vars["ids"])
			}
			fmt.Fprint(w, `{"data":{"nodes":[{"id":"N1","number":1,"title":"one","body":"body one","repository":{"nameWithOwner":"org/repo"}},{"id":"N2","number":2,"title":"two","body":"body two","repository":{"nameWithOwner":"org/repo"}}]}}`)
		default:
			if strings.Contains(query, "comments(") {
				t.Error("comments were loaded before requested")
			}
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":3,"nodes":[{"id":"N1","number":1,"title":"one","repository":{"nameWithOwner":"org/repo"}},{"id":"N2","number":2,"title":"two","repository":{"nameWithOwner":"org/repo"}},{"id":"N3","number":3,"title":"offscreen","repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
		}
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.adapters["github"] = a
	loader := NewLoader(registry)
	loader.DeferDetails = true
	drainRefresh(loader.Refresh(context.Background(), []Source{source}))
	if batchCalls.Load() != 0 {
		t.Fatal("offscreen details loaded during summary refresh")
	}
	refs := []Ref{{source.ID, "1"}, {source.ID, "2"}}
	for range loader.HydrateVisible(context.Background(), source, refs) {
	}
	if batchCalls.Load() != 1 {
		t.Fatalf("visible rows were not batched: %d calls", batchCalls.Load())
	}
	items := loader.Store.Current().Items
	if items[refs[0].Key()].Body != "body one" || items[refs[1].Key()].Body != "body two" || items[Ref{source.ID, "3"}.Key()].Body != "" {
		t.Fatalf("visible-only hydration: %+v", items)
	}
}

func TestGitHubCommentsLoadOnlyOnExplicitRead(t *testing.T) {
	var calls atomic.Int32
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		query, vars := githubRequest(t, r)
		switch {
		case strings.Contains(query, "viewer"):
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
		case strings.Contains(query, "comments(first:"):
			calls.Add(1)
			if string(vars["after"]) == `"next"` {
				fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issue":{"comments":{"nodes":[{"id":"C2","body":"second","author":null}],"pageInfo":{"hasNextPage":false}}}}}}`)
			} else {
				fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issue":{"comments":{"nodes":[{"id":"C1","body":"first","author":{"login":"tester"}}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}}}}}`)
			}
		default:
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`)
		}
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.List(context.Background(), source, Query{}, ""); err != nil || calls.Load() != 0 {
		t.Fatalf("list loaded comments: calls=%d err=%v", calls.Load(), err)
	}
	first, cursor, err := a.ReadComments(context.Background(), source, Ref{source.ID, "1"}, "", 1)
	if err != nil || len(first) != 1 || first[0].Author != "tester" || cursor != "next" {
		t.Fatalf("first comments page: %+v %q %v", first, cursor, err)
	}
	last, cursor, err := a.ReadComments(context.Background(), source, Ref{source.ID, "1"}, cursor, 1)
	if err != nil || len(last) != 1 || last[0].Body != "second" || cursor != "" || calls.Load() != 2 {
		t.Fatalf("last comments page: %+v %q %v calls=%d", last, cursor, err, calls.Load())
	}
}

func TestGitHubTransferredNodeWithholdsItemAndDisablesClaims(t *testing.T) {
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		query, _ := githubRequest(t, r)
		switch {
		case strings.Contains(query, "viewer"):
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
		case strings.Contains(query, "nodes(ids:"):
			fmt.Fprint(w, `{"data":{"nodes":[{"id":"N1","number":1,"title":"transferred","repository":{"nameWithOwner":"other/repo"}},{"id":"N2","number":2,"title":"still here","repository":{"nameWithOwner":"org/repo"}}]}}`)
		default:
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":2,"nodes":[{"id":"N1","number":1,"repository":{"nameWithOwner":"org/repo"}},{"id":"N2","number":2,"repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
		}
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.List(context.Background(), source, Query{}, ""); err != nil {
		t.Fatal(err)
	}
	outcomes := a.ReadItems(context.Background(), source, []Ref{{source.ID, "1"}, {source.ID, "2"}}, nil, 100)
	if outcomes[0].Kind != "withheld" || outcomes[1].Kind != "found" {
		t.Fatalf("transfer contaminated sibling hydration: %+v", outcomes)
	}
	if _, err := a.Capabilities(context.Background(), source, "", nil); err == nil {
		t.Fatal("transferred issue left claims available")
	}
}

func TestGitHubIncrementalPagesKeepFixedWatermarkAndOverlap(t *testing.T) {
	var sinceValues []string
	var page int
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		query, vars := githubRequest(t, r)
		if strings.Contains(query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		if !strings.Contains(query, "filterBy:{since:$since}") {
			t.Errorf("incremental query missing since filter: %s", query)
		}
		var since string
		if err := json.Unmarshal(vars["since"], &since); err != nil {
			t.Fatal(err)
		}
		sinceValues = append(sinceValues, since)
		page++
		if page == 1 {
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":2,"nodes":[{"id":"N1","number":1,"title":"one","state":"OPEN","updatedAt":"2026-09-23T10:00:00Z","repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}}}}`)
		} else {
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":2,"nodes":[{"id":"N1","number":1,"title":"edited between pages","state":"OPEN","updatedAt":"2026-09-23T10:01:00Z","repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
		}
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	committed := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	watermark := time.Date(2026, 9, 23, 11, 0, 0, 0, time.UTC)
	first, err := a.ListIncremental(context.Background(), source, Query{}, "", committed, watermark)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.ListIncremental(context.Background(), source, Query{}, first.NextCursor, committed, watermark)
	if err != nil {
		t.Fatal(err)
	}
	wantSince := committed.Add(-5 * time.Minute).Format(time.RFC3339Nano)
	if len(first.Items) != 1 || len(second.Items) != 1 || first.Items[0].CanonicalID != second.Items[0].CanonicalID || second.Items[0].Title != "edited between pages" || first.NextCursor == "" || second.NextCursor != "" || len(sinceValues) != 2 || sinceValues[0] != wantSince || sinceValues[1] != wantSince {
		t.Fatalf("pages=%+v %+v since=%v", first, second, sinceValues)
	}
}

func TestGitHubIncrementalRevisitsIssueMovedAheadOfCursor(t *testing.T) {
	var scan int
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		query, vars := githubRequest(t, r)
		if strings.Contains(query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		if !strings.Contains(query, "field:UPDATED_AT,direction:DESC") {
			t.Errorf("updated-order scan must be newest-first: %s", query)
		}
		if string(vars["after"]) == `"next"` {
			// Issue C was edited from an unseen older page and moved before the
			// cursor. It must be found in the next overlapping window.
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":2,"nodes":[{"id":"B","number":2,"repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
			return
		}
		scan++
		id, number := "A", 1
		if scan == 2 {
			id, number = "C", 3
		}
		fmt.Fprintf(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":2,"nodes":[{"id":%q,"number":%d,"repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}}}}`, id, number)
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	committed := time.Date(2026, 9, 23, 9, 0, 0, 0, time.UTC)
	watermark := committed.Add(time.Hour)
	first, err := a.ListIncremental(context.Background(), source, Query{}, "", committed, watermark)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.ListIncremental(context.Background(), source, Query{}, first.NextCursor, committed, watermark); err != nil {
		t.Fatal(err)
	}
	followup, err := a.ListIncremental(context.Background(), source, Query{}, "", watermark, watermark.Add(time.Hour))
	if err != nil || len(followup.Items) != 1 || followup.Items[0].CanonicalID != "C" {
		t.Fatalf("moved issue missing from next window: %+v %v", followup, err)
	}
}

func TestGitHubDependencyAccessLossWithholdsHydratedBody(t *testing.T) {
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		query, _ := githubRequest(t, r)
		switch {
		case strings.Contains(query, "viewer"):
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
		case strings.Contains(query, "nodes(ids:"):
			fmt.Fprint(w, `{"data":{"nodes":[{"id":"N1","number":1,"body":"private body","repository":{"nameWithOwner":"org/repo"}}]}}`)
		case strings.Contains(query, "blockedBy(first:"):
			fmt.Fprint(w, `{"errors":[{"type":"NOT_FOUND","message":"not found"}]}`)
		default:
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":1,"nodes":[{"id":"N1","number":1,"repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
		}
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.adapters["github"] = a
	loader := NewLoader(registry)
	loader.GitHubSync = &syncTestStore{}
	drainRefresh(loader.Refresh(context.Background(), []Source{source}))
	if _, ok := loader.Store.Item(Ref{source.ID, "1"}); ok {
		t.Fatal("private body remained visible after dependency access loss")
	}
}

func TestGitHub404WithholdsExistingIssueInsteadOfDeletingIt(t *testing.T) {
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		query, _ := githubRequest(t, r)
		if strings.Contains(query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	outcomes := a.ReadItems(context.Background(), source, []Ref{{SourceID: source.ID, ItemID: "42"}}, nil, 100)
	if len(outcomes) != 1 || outcomes[0].Kind != "withheld" || outcomes[0].Err != nil || outcomes[0].Item != nil {
		t.Fatalf("404 outcome must withhold, not assert deletion: %+v", outcomes)
	}
}

func TestGitHubDiagnosticsAndIdentity(t *testing.T) {
	cases := []struct {
		status                 int
		body, header, expected string
	}{
		{401, "", "", "authentication"}, {403, "", "", "permission-denied"}, {403, "SAML SSO required", "", "saml-sso"}, {404, "", "", "not-found-or-inaccessible"}, {410, "", "", "gone"}, {429, "", "", "rate-limited"},
	}
	for _, test := range cases {
		t.Run(test.expected+fmt.Sprint(test.status), func(t *testing.T) {
			a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
				q, _ := githubRequest(t, r)
				if strings.Contains(q, "viewer") {
					fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
					return
				}
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})
			source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, err = a.List(ctx, source, Query{}, "")
			if d, ok := err.(GitHubDiagnostic); !ok || d.Code != test.expected {
				t.Fatalf("got %v, want %s", err, test.expected)
			}
		})
	}
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		q, _ := githubRequest(t, r)
		if strings.Contains(q, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"new/repo","issues":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`)
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.List(context.Background(), source, Query{}, "")
	if d, ok := err.(GitHubDiagnostic); !ok || d.Code != "identity-changed" {
		t.Fatalf("identity: %v", err)
	}
	_, err = a.Capabilities(context.Background(), source, "", nil)
	if d, ok := err.(GitHubDiagnostic); !ok || d.Code != "identity-changed" {
		t.Fatalf("claim availability: %v", err)
	}
}
func TestGitHubUnsupportedDependencyFields(t *testing.T) {
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		q, _ := githubRequest(t, r)
		if strings.Contains(q, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		fmt.Fprint(w, `{"errors":[{"message":"Cannot query field blockedBy on type Issue"}]}`)
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := a.ReadDependencies(context.Background(), source, Ref{source.ID, "1"}, "", 100)
	if err != nil || page.Completeness != CoverageUnknown {
		t.Fatalf("unsupported: %+v %v", page, err)
	}
	caps, err := a.Capabilities(context.Background(), source, "", nil)
	if err != nil || caps["dependencies"].Support != Unsupported {
		t.Fatalf("capability: %+v %v", caps, err)
	}
}
