package queue

import (
	"context"
	"fmt"
	"net/http"
	"strings"
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
