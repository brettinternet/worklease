package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
)

func TestStaleSourceRemainsVisibleInQueryAndTUI(t *testing.T) {
	h := newQueueQueryHarness(t)
	h.setTasks(`[{"id":"TASK-1","title":"Cached task","status":"Open","ordinal":1,"isReady":true}]`)
	var loader *queue.Loader
	run := func() queueQueryEnvelope {
		t.Helper()
		var stdout, stderr bytes.Buffer
		root := NewRootCommand("test", "unknown", "unknown", &stdout, &stderr)
		state := root.Metadata[jsonStateKey].(*boundary)
		for _, c := range root.Commands {
			if c.Name != "queue" {
				continue
			}
			for _, child := range c.Commands {
				if child.Name == "query" {
					child.Action = queueQueryActionWithLoader(state, func(registry *queue.Registry) *queue.Loader {
						if loader == nil {
							loader = queue.NewLoader(registry)
						}
						return loader
					})
				}
			}
		}
		args := []string{"worklease", "--home", h.state, "queue", "query", "--view", "Ready", "--json"}
		SetInvocationArgs(root, args)
		if err := root.Run(context.Background(), args); err != nil {
			t.Fatalf("query failed: %v (%s)", err, stdout.String())
		}
		var result struct {
			Query queueQueryEnvelope `json:"query"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Query
	}
	first := run()
	if len(first.Items) != 1 || !first.Items[0].Fresh {
		t.Fatalf("initial source not fresh: %+v", first)
	}
	h.setTasks(`not-json`)
	stale := run()
	if len(stale.Items) != 1 || stale.Items[0].Fresh || !stale.Incomplete || stale.Sources[0].Freshness != "stale" || stale.Sources[0].Coverage.State != queue.CoverageUnknown {
		t.Fatalf("stale source lost or marked fresh: %+v", stale)
	}
	model := queueui.New(loader.Store.Current())
	model.ViewName = "Ready"
	model.ViewRules = map[string]queueui.ViewRule{"Ready": {Assigned: []string{"nobody"}}}
	model.Sources = []queue.Source{{ID: "local", Name: "local"}}
	view := model.View()
	if !containsAll(view, "Cached task", "stale", "0/1") {
		t.Fatalf("TUI hid cached stale task/source: %s", view)
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
