package queueindex_test

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

	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueindex"
)

// The fake provider changes a node's issue number after the first page is
// committed. Reopening the index must resume at that cursor, not restart from
// a new watermark or expose both references to the same node.
func TestGitHubReconciliationResumesAfterInterruptedMovingPage(t *testing.T) {
	var interrupted atomic.Bool
	interrupted.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string                     `json:"query"`
			Variables map[string]json.RawMessage `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		if strings.Contains(request.Query, "viewer") {
			fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
			return
		}
		if strings.Contains(request.Query, "nodes(ids:") {
			number, title := 1, "before move"
			if !interrupted.Load() {
				number, title = 2, "moved"
			}
			fmt.Fprintf(w, `{"data":{"nodes":[{"id":"node-1","number":%d,"title":%q,"state":"OPEN","repository":{"nameWithOwner":"org/repo"}}]}}`, number, title)
			return
		}
		if string(request.Variables["after"]) == `"next"` {
			if interrupted.Load() {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":2,"nodes":[{"id":"node-1","number":2,"title":"moved","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
			return
		}
		fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":2,"nodes":[{"id":"node-1","number":1,"title":"before move","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":true,"endCursor":"next"}}}}}`)
	}))
	defer server.Close()
	binary := filepath.Join(t.TempDir(), "gh")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf 'test-token\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	registry := queue.NewRegistry()
	builtin, _ := registry.Get("github")
	adapter := builtin.(*queue.GitHubAdapter)
	adapter.Binary = binary
	adapter.APIBase = server.URL
	ctx := context.Background()
	source, err := adapter.Resolve(ctx, map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	indexDir := t.TempDir()
	index, err := queueindex.Open(ctx, indexDir)
	if err != nil {
		t.Fatal(err)
	}
	refresh := func() {
		loader := queue.NewLoader(registry)
		loader.GitHubSync = queueindex.GitHubSyncStore{Index: index, Registry: registry}
		for range loader.Refresh(ctx, []queue.Source{source}) {
		}
	}
	refresh()
	partition, ok := queueindex.ForGitHubSync(adapter, source)
	if !ok {
		t.Fatal("missing GitHub sync identity")
	}
	state, err := index.ReadGitHubSyncState(ctx, partition)
	if err != nil || state.ReconciliationCursor == "" || state.CommittedWatermark != (time.Time{}) {
		t.Fatalf("interrupted scan advanced watermark: %+v %v", state, err)
	}
	if err := index.Close(); err != nil {
		t.Fatal(err)
	}
	index, err = queueindex.Open(ctx, indexDir)
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	interrupted.Store(false)
	// The adapter discarded its in-memory scan after the failed page. The
	// persisted cursor is rejected and restarts a fresh generation; that
	// generation completes on the following bounded refresh.
	refresh()
	state, err = index.ReadGitHubSyncState(ctx, partition)
	if err != nil || state.ReconciliationCursor != "@start" {
		t.Fatalf("expired scan did not restart safely: %+v %v", state, err)
	}
	refresh()
	state, err = index.ReadGitHubSyncState(ctx, partition)
	if err != nil || state.ReconciliationCursor != "" || state.CommittedWatermark.IsZero() {
		t.Fatalf("resumed scan did not finish: %+v %v", state, err)
	}
	items, _, _, err := index.Read(ctx, partition, time.Hour)
	if err != nil || len(items.Items) != 1 || items.Items[queue.Ref{SourceID: source.ID, ItemID: "2"}.Key()].Title != "moved" {
		t.Fatalf("recovered projection has stale identity: %+v %v", items.Items, err)
	}
}
