package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/store"
)

func nextResult(t *testing.T, h *queueQueryHarness, args ...string) map[string]any {
	t.Helper()
	argv := append([]string{"queue", "next", "--view", "Ready", "--json"}, args...)
	data, err := h.run(argv...)
	if err != nil {
		t.Fatalf("next: %v: %s", err, data)
	}
	var response map[string]any
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	return response["next"].(map[string]any)
}

func TestQueueNextUsesEntireScopeAndNeverAcquires(t *testing.T) {
	h := newQueueQueryHarness(t)
	t.Setenv("WORKLEASE_HOME", h.state)
	st, err := store.Open(context.Background(), h.state, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	h.setTasks(`[{"id":"TASK-1","title":"First","status":"Open","priority":"medium","ordinal":1,"dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true},{"id":"TASK-2","title":"Second","status":"Open","priority":"high","ordinal":2,"dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true}]`)
	if data, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge"); err != nil {
		t.Fatalf("confirm identity: %v: %s", err, data)
	}
	next := nextResult(t, h)
	if next["result"] != "ready" || next["acquired"] != false || !strings.Contains(next["hint"].(string), "--claim") {
		query, _ := h.run("queue", "query", "--view", "Ready", "--json")
		t.Fatalf("next not an unclaimed observation: %#v; query: %s", next, query)
	}
	candidate := next["candidates"].([]any)[0].(map[string]any)
	if candidate["ref"].(map[string]any)["itemId"] != "TASK-2" || candidate["keyInputs"] == nil || candidate["readiness"] == nil || candidate["claim"] == nil || len(candidate["resources"].([]any)) != 1 {
		t.Fatalf("wrong candidate/evidence: %#v", candidate)
	}
	keyData, err := h.run("key", "--provider", "generic", "--source", "brettinternet/worklease/backlog", "--item", "TASK-2", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var key map[string]any
	if err := json.Unmarshal(keyData, &key); err != nil || candidate["resources"].([]any)[0] != key["resource"] {
		t.Fatalf("next resource must be exact claim key: %s; candidate: %#v", keyData, candidate)
	}
	group := nextResult(t, h, "--group", "2")
	if len(group["candidates"].([]any)) != 2 {
		t.Fatalf("independent group not returned: %#v", group)
	}
	selected := nextResult(t, h, "--item", "local:TASK-1", "--item", "local:TASK-2", "--group", "2")
	if selected["candidates"].([]any)[0].(map[string]any)["ref"].(map[string]any)["itemId"] != "TASK-1" {
		t.Fatalf("explicit item order ignored: %#v", selected)
	}
	if data, err := h.run("list", "--json"); err != nil || strings.Contains(string(data), `"active":true`) {
		t.Fatalf("next acquired a claim: %v: %s", err, data)
	}
	plain, err := h.run("queue", "next", "--view", "Ready")
	if err != nil || !strings.Contains(string(plain), "no claim acquired") {
		t.Fatalf("plain next not explicit about no acquisition: %v: %s", err, plain)
	}
	// Deliberate selection overrides advisory assignment even when the view
	// filters to unassigned items. The default still excludes the other owner.
	h.setTasks(`[{"id":"TASK-1","title":"Assigned","status":"Open","ordinal":1,"assignees":["@other"],"dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true}]`)
	if result := nextResult(t, h); result["result"] != "assigned-elsewhere" {
		t.Fatalf("other owner's item suggested by default: %#v", result)
	}
	for _, filtered := range []bool{true, false} {
		if !filtered {
			h.writeQueueConfig(strings.Replace(h.queueConfig, "filter: {assigned: [nobody]}", "filter: {}", 1))
		}
		result := nextResult(t, h, "--item", "local:TASK-1")
		if result["result"] != "ready" || len(result["candidates"].([]any)) != 1 {
			t.Fatalf("explicit selection ignored (filtered=%t): %#v", filtered, result)
		}
	}
}

func TestQueueNextIncompleteScopeDoesNotSelectReadyItem(t *testing.T) {
	h := newQueueQueryHarness(t)
	h.setTasks(`[{"id":"TASK-1","title":"First","status":"Open","ordinal":1,"dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true},{"id":"TASK-2","title":"Second","status":"Open","ordinal":2,"dependencies":["TASK-404"],"readiness":{"missingDependencies":[]},"isReady":false}]`)
	next := nextResult(t, h)
	if next["result"] != "incomplete" || len(next["candidates"].([]any)) != 0 {
		t.Fatalf("partial graph selected a candidate: %#v", next)
	}
	if len(next["sources"].([]any)) == 0 {
		t.Fatalf("missing source coverage: %#v", next)
	}
}
