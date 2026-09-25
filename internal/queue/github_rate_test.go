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

func TestAdapterConformanceGitHubRateLimitRetryTime(t *testing.T) {
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		query, _ := githubRequest(t, r)
		if strings.Contains(query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.List(context.Background(), source, Query{}, "")
	if diagnostic, ok := err.(GitHubRateDiagnostic); !ok || diagnostic.RetryAt.Before(time.Now().Add(50*time.Second)) || diagnostic.Code != "rate-limited" {
		t.Fatalf("missing retry time: %v", err)
	}
	_, err = a.List(context.Background(), source, Query{}, "")
	if _, ok := err.(GitHubRateDiagnostic); !ok {
		t.Fatalf("shared quota: %v", err)
	}
}

func TestGitHubRateLimitRetryAndOffline(t *testing.T) {
	var calls atomic.Int32
	a, server := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		query, _ := githubRequest(t, r)
		if strings.Contains(query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`)
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := a.List(context.Background(), source, Query{}, "")
	if err != nil || page.Coverage.Total != 0 || calls.Load() != 2 {
		t.Fatalf("retry: %+v %v calls=%d", page, err, calls.Load())
	}
	server.Close()
	_, err = a.List(context.Background(), source, Query{}, "")
	if d, ok := err.(GitHubDiagnostic); !ok || d.Code != "offline" {
		t.Fatalf("network diagnostic: %v", err)
	}
}
