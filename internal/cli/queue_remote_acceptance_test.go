package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
	urfave "github.com/urfave/cli/v3"
)

func TestQueueRemoteViewUsesSelectedAuthorityWithoutLocalFallback(t *testing.T) {
	h := newQueueQueryHarness(t)
	h.setTasks(`[{"id":"TASK-1","title":"Remote scope","status":"Open","ordinal":1,"isReady":true}]`)
	profile, _, _ := remoteCLIFixture(t)
	queueConfig := strings.Replace(h.queueConfig, "authority: local", "authority: "+profile, 1)
	paths := config.UserProfilePaths(os.Getenv)
	checkout := strings.TrimSpace(strings.Split(strings.Split(h.queueConfig, "checkout: ")[1], "\n")[0])
	if err := config.SaveBindings(paths, map[string]string{checkout: profile}); err != nil {
		t.Fatal(err)
	}
	profiles, _, err := config.LoadProfiles(paths)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse(profiles[profile].Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	transport := original.(*http.Transport).Clone()
	var authorityDials, forbiddenDials atomic.Int32
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != endpoint.Host {
			forbiddenDials.Add(1)
			return nil, fmt.Errorf("unexpected remote queue endpoint")
		}
		authorityDials.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	var statusReads atomic.Int32
	http.DefaultTransport = cliRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/v1/claims/status" {
			statusReads.Add(1)
		}
		return transport.RoundTrip(request)
	})
	if err := os.WriteFile(filepath.Join(filepath.Dir(paths.Profiles), "queue.yaml"), []byte(queueConfig), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := h.run("queue", "query", "--view", "Ready", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Query struct {
			Authority queueAuthorityJSON `json:"authority"`
			Items     []queueQueryItem   `json:"items"`
		} `json:"query"`
	}
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatal(err)
	}
	if forbiddenDials.Load() != 0 || authorityDials.Load() == 0 || statusReads.Load() == 0 {
		t.Fatalf("remote view dials: authority=%d status=%d forbidden=%d", authorityDials.Load(), statusReads.Load(), forbiddenDials.Load())
	}
	if output.Query.Authority.Profile != profile || output.Query.Authority.Scope != "remote" || output.Query.Authority.ID == "" || len(output.Query.Items) != 1 {
		t.Fatalf("remote authority disappeared: %s", data)
	}
	row := output.Query.Items[0]
	if row.Claim.AuthorityID != output.Query.Authority.ID || !row.Claim.Known || row.Claim.State != "free" {
		t.Fatalf("remote observation incorrectly fell back to local: %+v", row.Claim)
	}
	registry := queue.NewRegistry()
	adapter, _ := registry.Get("backlog-md")
	source, err := adapter.Resolve(context.Background(), map[string]string{"id": "local", "checkout": checkout})
	if err != nil {
		t.Fatal(err)
	}
	loader := queue.NewLoader(registry)
	for range loader.Refresh(context.Background(), []queue.Source{source}) {
	}
	backend, selected, err := queueAuthorityForView(context.Background(), &urfave.Command{}, profile)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	snapshot := loader.Store.Current()
	for key, item := range snapshot.Items {
		snapshot.Items[key] = queue.OverlayClaims(context.Background(), []queue.Item{item}, map[string]queue.ClaimSource{"local": {Source: source, Policy: "generic", ClaimSource: "brettinternet/worklease/backlog"}}, selected, paths, os.Getenv)[0]
	}
	if item, ok := snapshot.Item(row.Ref); !ok || item.Claim.State != row.Claim.State || item.Claim.AuthorityID != row.Claim.AuthorityID || item.Readiness.Status != row.Readiness.Status {
		t.Fatalf("remote JSON/TUI observations differ: JSON=%+v snapshot=%+v", row, item)
	}
	model := queueui.New(snapshot)
	model.Sources = []queue.Source{{ID: "local", Name: "local"}}
	model.ViewName = "Ready"
	model.ViewRules = map[string]queueui.ViewRule{"Ready": {Assigned: []string{"nobody"}}}
	model.Authority = output.Query.Authority.Profile + " " + output.Query.Authority.ID
	model.Scope = output.Query.Authority.Scope
	view := model.View()
	if !strings.Contains(view, "(remote)") || !strings.Contains(view, "Remote scope") || !strings.Contains(view, output.Query.Authority.ID) {
		t.Fatalf("TUI hid remote scope: %s", view)
	}
}
