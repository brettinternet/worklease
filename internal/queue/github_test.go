package queue

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
