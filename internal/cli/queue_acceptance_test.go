package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
)

// A command response and a separately loaded TUI snapshot must report the
// same source and item evidence. Do not build the TUI input from query JSON.
func TestQueueQueryAndTUIFixtureParity(t *testing.T) {
	h := newQueueQueryHarness(t)
	h.setTasks(`[{"id":"TASK-1","title":"Unresolved prerequisite","status":"Open","ordinal":1,"isReady":true},{"id":"TASK-2","title":"Complete","status":"Done","ordinal":2,"isReady":false}]`)
	assertQueueFixtureParity(t, h, false)
	missing := filepath.Join(filepath.Dir(h.home), "missing-project")
	if err := os.MkdirAll(missing, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := strings.Replace(h.queueConfig, "views:", "  - id: missing\n    adapter: backlog-md\n    checkout: "+missing+"\nviews:", 1)
	cfg = strings.Replace(cfg, "sources: [local]", "sources: [local, missing]", 1)
	h.writeQueueConfig(cfg)
	h.queueConfig = cfg
	assertQueueFixtureParity(t, h, true)
}

func assertQueueFixtureParity(t *testing.T, h *queueQueryHarness, missing bool) {
	t.Helper()
	data, err := h.run("queue", "query", "--view", "Ready", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Query struct {
			Authority  queueAuthorityJSON `json:"authority"`
			Sources    []queueSourceJSON  `json:"sources"`
			Items      []queueQueryItem   `json:"items"`
			Incomplete bool               `json:"incomplete"`
		} `json:"query"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	checkout := strings.TrimSpace(strings.Split(strings.Split(h.queueConfig, "checkout: ")[1], "\n")[0])
	registry := queue.NewRegistry()
	adapter, _ := registry.Get("backlog-md")
	resolved, err := adapter.Resolve(context.Background(), map[string]string{"id": "local", "checkout": checkout})
	if err != nil {
		t.Fatal(err)
	}
	loader := queue.NewLoader(registry)
	for range loader.Refresh(context.Background(), []queue.Source{resolved}) {
	}
	snapshot := loader.Store.Current()
	for key, item := range snapshot.Items {
		snapshot.Items[key] = queue.OverlayClaims(context.Background(), []queue.Item{item}, map[string]queue.ClaimSource{"local": {Source: resolved, Policy: "generic", ClaimSource: "brettinternet/worklease/backlog"}}, queue.ClaimAuthority{ID: response.Query.Authority.ID, Profile: "local"}, config.UserProfilePaths(nil), nil)[0]
	}
	model := queueui.New(snapshot)
	model.ViewName = "Ready"
	model.Views = []string{"Ready"}
	model.ViewRules = map[string]queueui.ViewRule{"Ready": {Assigned: []string{"nobody"}}}
	model.Sources = []queue.Source{resolved}
	model.Authority = response.Query.Authority.Profile + " " + response.Query.Authority.ID
	model.Scope = response.Query.Authority.Scope
	if missing {
		missingCheckout := strings.TrimSpace(strings.Split(strings.Split(h.queueConfig, "checkout: ")[2], "\n")[0])
		if _, resolveErr := adapter.Resolve(context.Background(), map[string]string{"id": "missing", "checkout": missingCheckout}); resolveErr == nil {
			t.Fatal("unreadable source unexpectedly resolved")
		} else {
			model.SourceErrors = map[string]string{"missing": queueSourceFailure(resolveErr)}
		}
		model.Sources = append(model.Sources, queue.Source{ID: "missing", Name: "missing"})
	}
	if len(response.Query.Items) != len(snapshot.Items) {
		t.Fatalf("JSON/TUI item count differs: snapshot=%+v query=%s", snapshot.Sources, data)
	}
	for _, row := range response.Query.Items {
		item, ok := snapshot.Item(row.Ref)
		if !ok || item.Readiness.Status != row.Readiness.Status || item.Readiness.Freshness != row.Readiness.Freshness || item.Fresh != row.Fresh || item.Closure != row.Closure || item.Coverage.State != row.Coverage.State || item.Observation.Coverage.State != row.Observation.Coverage.State || item.Claim.State != row.Claim.State || item.Claim.Known != row.Claim.Known || item.Claim.AuthorityID != row.Claim.AuthorityID {
			t.Fatalf("JSON/TUI item evidence differs: JSON=%+v TUI=%+v", row, item)
		}
	}
	if response.Query.Incomplete != true || len(response.Query.Sources) != len(model.Sources) {
		t.Fatalf("incomplete/coverage disagreement: %s", data)
	}
	if response.Query.Sources[0].Coverage.State != snapshot.Sources["local"].State || response.Query.Sources[0].Freshness != "fresh" {
		t.Fatalf("source coverage differs: %s", data)
	}
	if missing && (response.Query.Sources[1].ID != "missing" || response.Query.Sources[1].Coverage.State != queue.CoverageUnknown || response.Query.Sources[1].Freshness != "unknown" || !strings.Contains(strings.Join(response.Query.Sources[1].Diagnostics, " "), "source-resolve-failed")) {
		t.Fatalf("unreadable source concealed: %s", data)
	}
	view := model.View()
	if !strings.Contains(view, "Ready 2") {
		t.Fatalf("configured view count disagrees with its rows: %s", view)
	}
	for _, row := range response.Query.Items {
		if !strings.Contains(view, row.Title[:min(8, len(row.Title))]) || !strings.Contains(view, string(row.Readiness.Status)[:min(7, len(row.Readiness.Status))]) {
			t.Fatalf("TUI hid JSON observation for %s: %s", row.Ref.ItemID, view)
		}
	}
	if !strings.Contains(view, response.Query.Authority.ID) || missing && !strings.Contains(view, "offline/unavailable") {
		t.Fatalf("TUI hid authority/source failure: %s", view)
	}
}
