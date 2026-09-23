package queue

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/lease"
)

// A poisoned default dialer catches accidental network use by a local-only
// queue. The GitHub fixture has an explicit, allowlisted transport instead.
func TestReadOnlyFixtureNetworkAndAuthorityBoundary(t *testing.T) {
	root, binary := fakeBacklog(t)
	backlog := NewBacklogAdapter("Done")
	backlog.Binary = binary // The fake accepts reads only; every write exits nonzero.
	if _, err := backlog.run(context.Background(), root, binary, "task", "edit", "TASK-2", "--status", "Done"); !diag(err, "read-only") {
		t.Fatalf("dynamically assembled provider write crossed read seam: %v", err)
	}
	var forbidden atomic.Int32
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	transport := original.(*http.Transport).Clone()
	transport.DialContext = func(context.Context, string, string) (net.Conn, error) {
		forbidden.Add(1)
		return nil, fmt.Errorf("unexpected queue network access")
	}
	http.DefaultTransport = transport
	ctx := context.Background()
	local, err := backlog.Resolve(ctx, map[string]string{"id": "local", "checkout": root})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	registry.adapters["backlog-md"] = backlog
	loader := NewLoader(registry)
	for range loader.Refresh(ctx, []Source{local}) {
	}
	if forbidden.Load() != 0 || len(loader.Store.Current().Items) == 0 {
		t.Fatalf("local view dialed network or lost items: dials=%d", forbidden.Load())
	}
	// This status-only authority has no mutation capability to offer the queue.
	reader := statusOnlyAuthority{calls: &forbidden}
	items := []Item{{Summary: Summary{Ref: Ref{SourceID: "github", ItemID: "1"}}}}
	observed := OverlayClaims(ctx, items, map[string]ClaimSource{"github": {Source: Source{ID: "github", Adapter: "github", Locator: "org/repo"}}}, ClaimAuthority{API: reader, ID: "local", Profile: "local"}, config.ProfilePaths{}, nil)
	if len(observed) != 1 || !observed[0].Claim.Known || observed[0].Claim.State != "free" {
		t.Fatalf("read-only authority observation: %+v", observed)
	}
	if forbidden.Load() != 1 {
		t.Fatalf("expected exactly one read-only status call: %d", forbidden.Load())
	}
	forbidden.Store(0)
	github, server := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/" {
			t.Errorf("unexpected provider request %s %s", r.Method, r.URL)
		}
		query, _ := githubRequest(t, r)
		if !strings.HasPrefix(query, "query") {
			t.Errorf("provider mutation attempted: %s", query)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch {
		case strings.Contains(query, "viewer"):
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
		case strings.Contains(query, "nodes(ids:"):
			fmt.Fprint(w, `{"data":{"nodes":[{"id":"issue-1","number":1,"title":"GitHub task","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}]}}`)
		case strings.Contains(query, "blockedBy("):
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issue":{"id":"issue-1","number":1,"repository":{"nameWithOwner":"org/repo"},"blockedBy":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}},"subIssues":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`)
		case strings.Contains(query, "issue(number:"):
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issue":{"id":"issue-1","number":1,"title":"GitHub task","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}}}}`)
		default:
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":1,"nodes":[{"id":"issue-1","number":1,"title":"GitHub task","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
		}
	})
	providerTransport := original.(*http.Transport).Clone()
	providerTransport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != strings.TrimPrefix(server.URL, "http://") {
			forbidden.Add(1)
			return nil, fmt.Errorf("unconfigured provider endpoint")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	github.Client = &http.Client{Transport: providerTransport}
	remoteSource, err := github.Resolve(ctx, map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := github.binding(remoteSource)
	if err != nil {
		t.Fatal(err)
	}
	if err := github.query(ctx, binding, `mutation { addComment(input:{}) { clientMutationId } }`, nil, nil); err == nil {
		t.Fatal("GraphQL mutation crossed read seam")
	} else if failure, ok := err.(GitHubDiagnostic); !ok || failure.Code != "read-only" {
		t.Fatalf("unexpected mutation rejection: %v", err)
	}
	registry.adapters["github"] = github
	for range loader.Refresh(ctx, []Source{local, remoteSource}) {
	}
	combined := loader.Store.Current()
	if combined.Sources[remoteSource.ID].State != CoverageComplete || combined.Sources[local.ID].State != CoverageComplete || len(combined.Items) < 2 || combined.Items[Ref{SourceID: remoteSource.ID, ItemID: "1"}.Key()].Title != "GitHub task" {
		t.Fatalf("combined Backlog/GitHub fixture lost coverage: %+v", combined.Sources)
	}
	if forbidden.Load() != 0 {
		t.Fatalf("view reached an unconfigured endpoint %d times", forbidden.Load())
	}
}

type statusOnlyAuthority struct{ calls *atomic.Int32 }

func (a statusOnlyAuthority) Status(_ context.Context, s lease.Selector) (lease.Status, error) {
	a.calls.Add(1)
	results := make([]lease.ResourceStatus, len(s.Resources))
	for i, resource := range s.Resources {
		results[i] = lease.ResourceStatus{Resource: resource, State: "free"}
	}
	return lease.Status{Resources: results}, nil
}
