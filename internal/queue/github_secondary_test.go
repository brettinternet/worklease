package queue

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGitHubSecondaryGraphQLLimitRetry(t *testing.T) {
	var calls atomic.Int32
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		q, _ := githubRequest(t, r)
		if strings.Contains(q, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		if calls.Add(1) == 1 {
			fmt.Fprint(w, `{"errors":[{"message":"You have exceeded a secondary rate limit"}]}`)
			return
		}
		fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`)
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.List(context.Background(), source, Query{}, "")
	if err != nil || calls.Load() != 2 {
		t.Fatalf("secondary retry: %v calls=%d", err, calls.Load())
	}
}
func TestGitHubIssueTransferDisablesClaims(t *testing.T) {
	a, _ := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		q, _ := githubRequest(t, r)
		if strings.Contains(q, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":1,"nodes":[{"id":"A","number":1,"repository":{"nameWithOwner":"new/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
	})
	source, err := a.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.List(context.Background(), source, Query{}, "")
	if d, ok := err.(GitHubDiagnostic); !ok || d.Code != "identity-changed" {
		t.Fatalf("transfer: %v", err)
	}
	_, err = a.Capabilities(context.Background(), source, "", nil)
	if d, ok := err.(GitHubDiagnostic); !ok || d.Code != "identity-changed" {
		t.Fatalf("claims still available: %v", err)
	}
}
