package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
)

func TestCombinedBacklogAndGitHubQueryTUIParity(t *testing.T) {
	h := newQueueQueryHarness(t)
	h.setTasks(`[{"id":"TASK-1","title":"Local task","status":"Open","ordinal":1,"isReady":true}]`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/" {
			t.Errorf("unexpected provider request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var request struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if !strings.HasPrefix(request.Query, "query") {
			t.Errorf("mutation sent to GitHub fixture: %s", request.Query)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		switch {
		case strings.Contains(request.Query, "viewer"):
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
		case strings.Contains(request.Query, "blockedBy("):
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"Owner/Repo","issue":{"id":"issue-1","number":1,"repository":{"nameWithOwner":"Owner/Repo"},"blockedBy":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}},"subIssues":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`)
		case strings.Contains(request.Query, "issue(number:"):
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"Owner/Repo","issue":{"id":"issue-1","number":1,"title":"GitHub task","state":"OPEN","repository":{"nameWithOwner":"Owner/Repo"}}}}}`)
		default:
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"Owner/Repo","issues":{"totalCount":1,"nodes":[{"id":"issue-1","number":1,"title":"GitHub task","state":"OPEN","repository":{"nameWithOwner":"Owner/Repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
		}
	}))
	t.Cleanup(server.Close)
	ghBinary := filepath.Join(filepath.Dir(h.home), "bin", "gh")
	if err := os.WriteFile(ghBinary, []byte("#!/bin/sh\nprintf 'test-token\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	configuration := strings.Replace(h.queueConfig, "views:", "  - id: github\n    adapter: github\n    host: github.com\n    repository: Owner/Repo\n    account: tester\nviews:", 1)
	configuration = strings.Replace(configuration, "sources: [local]", "sources: [local, github]", 1)
	h.writeQueueConfig(configuration)
	newRegistry := func() *queue.Registry {
		registry := queue.NewRegistry()
		github, _ := registry.Get("github")
		adapter := github.(*queue.GitHubAdapter)
		adapter.Binary, adapter.APIBase = ghBinary, server.URL
		return registry
	}
	var stdout, stderr bytes.Buffer
	root := NewRootCommand("test", "unknown", "unknown", &stdout, &stderr)
	state := root.Metadata[jsonStateKey].(*boundary)
	for _, command := range root.Commands {
		if command.Name == "queue" {
			for _, child := range command.Commands {
				if child.Name == "query" {
					child.Action = queueQueryActionWithRegistry(state, newRegistry, queue.NewLoader)
				}
			}
		}
	}
	args := []string{"worklease", "--home", h.state, "queue", "query", "--view", "Ready", "--json"}
	SetInvocationArgs(root, args)
	if err := root.Run(context.Background(), args); err != nil {
		t.Fatalf("combined query: %v %s", err, stdout.String())
	}
	var output struct {
		Query queueQueryEnvelope `json:"query"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if len(output.Query.Sources) != 2 || len(output.Query.Items) != 2 {
		t.Fatalf("combined fixture lost provider data: %s", stdout.String())
	}
	var githubItem *queue.Item
	for i := range output.Query.Items {
		if output.Query.Items[i].Ref.SourceID == "github" {
			githubItem = &output.Query.Items[i].Item
		}
	}
	if githubItem == nil || githubItem.KeyInputs == nil {
		t.Fatalf("GitHub key inputs missing: %+v", output.Query.Items)
	}
	keyData, err := h.run("key", "--provider", "github", "--source", "Owner/Repo", "--item", "1", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var keyOutput map[string]any
	if err := json.Unmarshal(keyData, &keyOutput); err != nil {
		t.Fatal(err)
	}
	if githubItem.KeyInputs.Provider != keyOutput["provider"] || githubItem.KeyInputs.Source != keyOutput["source"] || githubItem.KeyInputs.Item != keyOutput["item"] {
		t.Fatalf("queue key inputs differ from worklease key: %+v != %s", githubItem.KeyInputs, keyData)
	}
	registry := newRegistry()
	backlog, _ := registry.Get("backlog-md")
	github, _ := registry.Get("github")
	checkout := strings.TrimSpace(strings.Split(strings.Split(h.queueConfig, "checkout: ")[1], "\n")[0])
	localSource, err := backlog.Resolve(context.Background(), map[string]string{"id": "local", "checkout": checkout})
	if err != nil {
		t.Fatal(err)
	}
	remoteSource, err := github.Resolve(context.Background(), map[string]string{"id": "github", "host": "github.com", "repository": "Owner/Repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	loader := queue.NewLoader(registry)
	for range loader.Refresh(context.Background(), []queue.Source{localSource, remoteSource}) {
	}
	snapshot := loader.Store.Current()
	for key, item := range snapshot.Items {
		claimSource := map[string]queue.ClaimSource{item.Ref.SourceID: {Source: localSource, Policy: "generic", ClaimSource: "brettinternet/worklease/backlog"}}
		if item.Ref.SourceID == "github" {
			claimSource = map[string]queue.ClaimSource{"github": {Source: remoteSource}}
		}
		snapshot.Items[key] = queue.OverlayClaims(context.Background(), []queue.Item{item}, claimSource, queue.ClaimAuthority{ID: output.Query.Authority.ID, Profile: "local"}, config.UserProfilePaths(nil), nil)[0]
	}
	model := queueui.New(snapshot)
	model.ViewName = "Ready"
	model.ViewRules = map[string]queueui.ViewRule{"Ready": {Assigned: []string{"nobody"}}}
	model.Sources = []queue.Source{localSource, remoteSource}
	model.Authority = output.Query.Authority.Profile + " " + output.Query.Authority.ID
	model.Scope = output.Query.Authority.Scope
	view := model.View()
	for _, row := range output.Query.Items {
		observed, ok := snapshot.Item(row.Ref)
		if !ok || observed.Readiness.Status != row.Readiness.Status || observed.Fresh != row.Fresh || observed.Claim.State != row.Claim.State || observed.Coverage.State != row.Coverage.State || !strings.Contains(view, row.Title) {
			t.Fatalf("combined JSON/TUI mismatch: row=%+v snapshot=%+v view=%s", row, observed, view)
		}
	}
	for _, source := range output.Query.Sources {
		if source.Coverage.State != snapshot.Sources[source.ID].State {
			t.Fatalf("combined source coverage mismatch: %+v", source)
		}
	}
}
