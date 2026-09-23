package queue

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGitHubIssueTextIsNotRateLimit(t *testing.T) {
	var reads atomic.Int32
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		q, _ := githubRequest(t, r)
		if strings.Contains(q, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		reads.Add(1)
		fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":1,"nodes":[{"id":"A","number":1,"title":"secondary rate limit","repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := a.List(context.Background(), source, Query{}, "")
	if err != nil || len(page.Items) != 1 || reads.Load() != 1 {
		t.Fatalf("text mistaken for rate limit: %v %+v calls=%d", err, page, reads.Load())
	}
}
func TestGitHubInterleavedScans(t *testing.T) {
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		q, v := githubRequest(t, r)
		if strings.Contains(q, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		if string(v["after"]) == `"second"` {
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":2,"nodes":[{"id":"B","number":2,"repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
		} else {
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":2,"nodes":[{"id":"A","number":1,"repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":true,"endCursor":"second"}}}}}`)
		}
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	older, _ := a.List(ctx, source, Query{}, "")
	newer, _ := a.List(ctx, source, Query{}, "")
	oldSecond, _ := a.List(ctx, source, Query{}, older.NextCursor)
	newSecond, _ := a.List(ctx, source, Query{}, newer.NextCursor)
	if len(oldSecond.Items) != 1 || len(newSecond.Items) != 1 || newSecond.Items[0].CanonicalID != "B" {
		t.Fatalf("overlap dropped issue: %+v %+v", oldSecond, newSecond)
	}
}
func TestGitHubConcurrentFinalPageCursor(t *testing.T) {
	var requests atomic.Int32
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		q, vars := githubRequest(t, r)
		if strings.Contains(q, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		if requests.Add(1) == 1 {
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":1,"nodes":[{"id":"A","number":1,"repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}}}}`)
			return
		}
		if string(vars["after"]) != `"next"` {
			t.Errorf("unexpected final-page cursor: %s", vars["after"])
		}
		fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":1,"nodes":[{"id":"A","number":1,"repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.List(context.Background(), source, Query{}, "")
	if err != nil {
		t.Fatal(err)
	}
	// Give both calls a chance to pass cursor validation before either final page is processed.
	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	for i := 0; i < 2; i++ {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			_, _ = a.List(context.Background(), source, Query{}, first.NextCursor)
		}()
	}
	start.Done()
	done.Wait()
}

func TestGitHubAbandonedScansAreBounded(t *testing.T) {
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		q, _ := githubRequest(t, r)
		if strings.Contains(q, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":1,"nodes":[{"id":"A","number":1,"repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}}}}`)
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.List(context.Background(), source, Query{}, "")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if _, err := a.List(context.Background(), source, Query{}, ""); err != nil {
			t.Fatal(err)
		}
	}
	if len(a.bindings[source.ID].scans) != 100 {
		t.Fatalf("scan map size = %d, want 100", len(a.bindings[source.ID].scans))
	}
	if _, err := a.List(context.Background(), source, Query{}, first.NextCursor); err == nil {
		t.Fatal("old scan cursor remained valid after eviction")
	}
}

func TestGitHubResolveFailureInvalidatesOldBinding(t *testing.T) {
	var reject atomic.Bool
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		q, _ := githubRequest(t, r)
		if strings.Contains(q, "viewer") {
			login := "tester"
			if reject.Load() {
				login = "someone-else"
			}
			fmt.Fprintf(w, `{"data":{"viewer":{"login":%q}}}`, login)
			return
		}
		fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`)
	})
	opts := map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"}
	source, err := a.Resolve(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	reject.Store(true)
	if _, err := a.Resolve(context.Background(), opts); err == nil {
		t.Fatal("principal mismatch unexpectedly resolved")
	}
	if _, err := a.List(context.Background(), source, Query{}, ""); err == nil {
		t.Fatal("old binding remained usable after failed re-verification")
	}
}

func TestGitHubDependencyCountUsesDistinctIDs(t *testing.T) {
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		q, v := githubRequest(t, r)
		if strings.Contains(q, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		page := `{"totalCount":2,"nodes":[{"id":"A","number":2,"repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}`
		if string(v["after"]) == `"next"` {
			page = `{"totalCount":2,"nodes":[{"id":"A","number":2,"repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}`
		}
		fmt.Fprintf(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issue":{"number":1,"repository":{"nameWithOwner":"org/repo"},"blockedBy":%s,"subIssues":{"nodes":[],"totalCount":0,"pageInfo":{"hasNextPage":false}}}}}}`, page)
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.ReadDependencies(context.Background(), source, Ref{source.ID, "1"}, "", 100)
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first page: %+v %v", first, err)
	}
	last, err := a.ReadDependencies(context.Background(), source, Ref{source.ID, "1"}, first.NextCursor, 100)
	if err != nil || last.Completeness != CoveragePartial {
		t.Fatalf("duplicate ID incorrectly completed coverage: %+v %v", last, err)
	}
}

func TestGitHubRedirectAndTypedErrors(t *testing.T) {
	for _, mode := range []string{"detail", "dependencies"} {
		t.Run(mode, func(t *testing.T) {
			a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
				q, _ := githubRequest(t, r)
				if strings.Contains(q, "viewer") {
					fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
					return
				}
				w.Header().Set("Location", "https://github.com/new/repo")
				w.WriteHeader(302)
			})
			source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "detail" {
				a.ReadItems(context.Background(), source, []Ref{{source.ID, "1"}}, nil, 100)
			} else {
				a.ReadDependencies(context.Background(), source, Ref{source.ID, "1"}, "", 100)
			}
			_, err = a.Capabilities(context.Background(), source, "", nil)
			if d, ok := err.(GitHubDiagnostic); !ok || d.Code != "identity-changed" {
				t.Fatalf("drift not latched: %v", err)
			}
		})
	}
	for _, test := range []struct{ kind, expected string }{{"FORBIDDEN", "permission-denied"}, {"NOT_FOUND", "not-found-or-inaccessible"}} {
		t.Run(test.kind, func(t *testing.T) {
			a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
				q, _ := githubRequest(t, r)
				if strings.Contains(q, "viewer") {
					fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
					return
				}
				fmt.Fprintf(w, `{"errors":[{"type":%q,"message":"redacted"}]}`, test.kind)
			})
			source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = a.List(context.Background(), source, Query{}, "")
			if d, ok := err.(GitHubDiagnostic); !ok || d.Code != test.expected {
				t.Fatalf("typed error: %v", err)
			}
		})
	}
}
func TestGitHubSuccessfulQuotaLastUnit(t *testing.T) {
	gate := quotaScheduler("github:github.com\x00tester", 1)
	t.Cleanup(func() { gate.mu.Lock(); gate.retryAt = time.Time{}; gate.mu.Unlock() })
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		q, _ := githubRequest(t, r)
		if strings.Contains(q, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(time.Now().Add(time.Hour).Unix()))
		fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`)
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	page, err := a.List(ctx, source, Query{}, "")
	if err != nil || page.Coverage.State != CoverageComplete {
		t.Fatalf("successful response lost: %+v %v", page, err)
	}
}
