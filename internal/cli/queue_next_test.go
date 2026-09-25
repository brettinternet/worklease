package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/resource"
	"github.com/brettinternet/worklease/internal/store"
	urfave "github.com/urfave/cli/v3"
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

func TestBeadsQueueNextClaimAndMCP(t *testing.T) {
	binary, err := exec.LookPath("bd")
	if err != nil {
		t.Skip("bd 1.3.0 is not installed")
	}
	version, err := exec.Command(binary, "version").Output()
	if err != nil || !strings.HasPrefix(string(version), "bd version 1.3.0 (") {
		t.Skip("bd 1.3.0 is required")
	}
	h := newQueueQueryHarness(t)
	t.Setenv("WORKLEASE_HOME", h.state)
	checkout := filepath.Join(filepath.Dir(h.home), "checkout")
	init := exec.Command(binary, "init", "--non-interactive", "--skip-hooks", "--skip-agents", "--prefix", "probe")
	init.Dir = checkout
	if out, err := init.CombinedOutput(); err != nil {
		t.Fatalf("bd init: %v: %s", err, out)
	}
	for _, title := range []string{"First", "Second"} {
		create := exec.Command(binary, "create", title, "--silent")
		create.Dir = checkout
		if out, err := create.CombinedOutput(); err != nil {
			t.Fatalf("bd create: %v: %s", err, out)
		}
	}
	h.writeQueueConfig(fmt.Sprintf("version: 1\nme: {beads: brett}\nsources:\n  - id: local\n    adapter: beads\n    checkout: %s\n    claims: {policy: generic, source: agreed-team/planning}\nviews:\n  - name: Ready\n    authority: local\n    sources: [local]\n    filter: {readiness: ready}\n", checkout))
	st, err := store.Open(context.Background(), h.state, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	if data, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge"); err != nil {
		t.Fatalf("identity confirm: %v: %s", err, data)
	}
	first := nextResult(t, h, "--claim", "--session", "beads-worker")
	if first["result"] != "ready" || first["acquired"] != true {
		t.Fatalf("CLI claim: %#v", first)
	}
	candidate := first["candidates"].([]any)[0].(map[string]any)
	id := candidate["ref"].(map[string]any)["itemId"].(string)
	key, err := h.run("key", "--provider", "generic", "--source", "agreed-team/planning", "--item", id, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var derived map[string]any
	if err := json.Unmarshal(key, &derived); err != nil || candidate["resources"].([]any)[0] != derived["resource"] {
		t.Fatalf("CLI/queue bytes: %s %#v", key, candidate)
	}
	server := queueMCPServer(t, h)
	claimed, err := server.Call(context.Background(), "queue_next", map[string]any{"view": "Ready", "claim": true, "sessionId": "mcp-beads-worker", "autoHeartbeat": false})
	if err != nil || claimed["isError"] == true {
		t.Fatalf("MCP Beads: %v %#v", err, claimed)
	}
	mcpNext := claimed["structuredContent"].(map[string]any)["next"].(map[string]any)
	if mcpNext["result"] != "ready" || mcpNext["acquired"] != true || mcpNext["candidates"].([]any)[0].(map[string]any)["ref"].(map[string]any)["itemId"] == id {
		t.Fatalf("MCP did not claim independent Beads item: %#v", mcpNext)
	}
	secondID := mcpNext["candidates"].([]any)[0].(map[string]any)["ref"].(map[string]any)["itemId"].(string)
	registry := queue.NewRegistry()
	adapter, _ := registry.Get("beads")
	source, err := adapter.Resolve(context.Background(), map[string]string{"id": "local", "checkout": checkout})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.List(context.Background(), source, queue.Query{}, ""); err != nil {
		t.Fatal(err)
	}
	dep := exec.Command(binary, "dep", "add", secondID, id)
	dep.Dir = checkout
	if out, err := dep.CombinedOutput(); err != nil {
		t.Fatalf("bd dep: %v: %s", err, out)
	}
	selected := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "local", ItemID: secondID}}}
	fresh, err := refreshQueueActionClosure(context.Background(), registry, map[string]queue.Source{"local": source}, selected)
	if err != nil || fresh.Readiness.Status != queue.Blocked {
		t.Fatalf("action used stale bulk edges: %+v %v", fresh, err)
	}
	third := exec.Command(binary, "create", "TUI claim", "--silent")
	third.Dir = checkout
	thirdBytes, err := third.Output()
	if err != nil {
		t.Fatal(err)
	}
	thirdID := strings.TrimSpace(string(thirdBytes))
	backend, authority, err := queueAuthorityForClaim(context.Background(), &urfave.Command{}, config.LocalProfileName)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	controller := &queueClaimController{backend: backend, registry: registry, sources: map[string]queue.Source{"local": source}, claimSources: map[string]queue.ClaimSource{"local": {Source: source, Policy: "generic", ClaimSource: "agreed-team/planning"}}, queueSession: strings.Repeat("e", 32), paths: config.UserProfilePaths(os.Getenv), current: func() (queue.ClaimAuthority, uint64) { return authority, 1 }, blocked: func() bool { return false }, profile: backend.Profile, profileName: config.LocalProfileName, home: backend.Config.Home}
	thirdItem := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "local", ItemID: thirdID}}}
	preview := controller.Preview(context.Background(), thirdItem)().(queueui.ClaimPreviewMsg)
	if preview.Err != nil || preview.Preview == nil {
		t.Fatalf("TUI claim preview: %+v", preview)
	}
	acquired := controller.AcquireClaim(context.Background(), thirdItem, *preview.Preview)().(queueui.ClaimResultMsg)
	if acquired.Err != nil || !acquired.Claim.Active {
		t.Fatalf("TUI Claim for me: %+v", acquired)
	}
	blocked := exec.Command(binary, "create", "Blocked TUI claim", "--deps", "blocked-by:"+thirdID, "--silent")
	blocked.Dir = checkout
	blockedBytes, err := blocked.Output()
	if err != nil {
		t.Fatal(err)
	}
	blockedItem := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "local", ItemID: strings.TrimSpace(string(blockedBytes))}}}
	if _, err := controller.prepare(context.Background(), blockedItem); err == nil {
		t.Fatal("D11 check allowed claim of blocked Beads item")
	}
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

func TestQueueNextClaimWorkerLifecycleAndContention(t *testing.T) {
	h := newQueueQueryHarness(t)
	t.Setenv("WORKLEASE_HOME", h.state)
	h.setTasks(`[{"id":"TASK-1","title":"First","status":"Open","priority":"high","dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true},{"id":"TASK-2","title":"Second","status":"Open","priority":"medium","dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true}]`)
	st, err := store.Open(context.Background(), h.state, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if data, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge"); err != nil {
		t.Fatalf("confirm: %v: %s", err, data)
	}
	first := nextResult(t, h, "--claim", "--session", "worker-one")
	if first["result"] != "ready" || first["acquired"] != true || first["claim"] == nil {
		t.Fatalf("first claim: %#v", first)
	}
	if data, err := h.run("verify", "--session", "worker-one"); err != nil {
		t.Fatalf("verify: %v: %s", err, data)
	}
	second := nextResult(t, h, "--claim", "--session", "worker-two")
	if second["result"] != "ready" || second["candidates"].([]any)[0].(map[string]any)["ref"].(map[string]any)["itemId"] != "TASK-2" {
		t.Fatalf("second claim: %#v", second)
	}
	third := nextResult(t, h, "--claim", "--session", "worker-three")
	if third["result"] != "active-claims" || third["acquired"] != false || len(third["skipped"].([]any)) != 2 {
		t.Fatalf("contention: %#v", third)
	}
	for _, raw := range third["skipped"].([]any) {
		row := raw.(map[string]any)
		if row["holder"] == nil || row["expiresAt"] == nil {
			t.Fatalf("missing holder or expiry: %#v", row)
		}
	}
	selected := nextResult(t, h, "--claim", "--session", "worker-four", "--item", "local:TASK-1")
	if len(selected["skipped"].([]any)) != 1 || selected["skipped"].([]any)[0].(map[string]any)["ref"].(map[string]any)["itemId"] != "TASK-1" {
		t.Fatalf("unselected item reported skipped: %#v", selected)
	}
	if data, err := h.run("heartbeat", "--session", "worker-one"); err != nil {
		t.Fatalf("heartbeat: %v: %s", err, data)
	}
	if data, err := h.run("release", "--session", "worker-one", "--reason", "test complete"); err != nil {
		t.Fatalf("release: %v: %s", err, data)
	}
}

func TestQueueNextRechecksAssignmentBeforeClaim(t *testing.T) {
	h := newQueueQueryHarness(t)
	t.Setenv("WORKLEASE_HOME", h.state)
	h.setTasks(`[{"id":"TASK-1","title":"First","status":"Open","priority":"high","dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true},{"id":"TASK-2","title":"Second","status":"Open","priority":"medium","dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true}]`)
	st, err := store.Open(context.Background(), h.state, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if data, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge"); err != nil {
		t.Fatalf("confirm: %v: %s", err, data)
	}
	t.Setenv("QUEUE_VIEW_LIST_JSON", `{"kind":"task-list","schemaVersion":1,"tasks":[{"id":"TASK-1","title":"First","status":"Open","priority":"high","assignees":["@other"],"dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true},{"id":"TASK-2","title":"Second","status":"Open","priority":"medium","dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true}]}`)
	result := nextResult(t, h, "--claim", "--session", "assignment-worker")
	if result["result"] != "ready" || result["candidates"].([]any)[0].(map[string]any)["ref"].(map[string]any)["itemId"] != "TASK-2" || len(result["skipped"].([]any)) != 1 {
		t.Fatalf("fresh assignment not enforced: %#v", result)
	}
}

func TestQueueNextLocalProfileEnvironment(t *testing.T) {
	h := newQueueQueryHarness(t)
	t.Setenv("WORKLEASE_HOME", h.state)
	t.Setenv("WORKLEASE_PROFILE", "local")
	h.setTasks(`[{"id":"TASK-1","title":"First","status":"Open","dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true}]`)
	st, err := store.Open(context.Background(), h.state, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if data, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge"); err != nil {
		t.Fatalf("confirm: %v: %s", err, data)
	}
	if result := nextResult(t, h, "--claim", "--session", "local-profile-worker"); result["acquired"] != true {
		t.Fatalf("local profile claim failed: %#v", result)
	}
}

func TestQueueNextUncertainAcquireStopsWithPendingHandle(t *testing.T) {
	h := newQueueQueryHarness(t)
	t.Setenv("WORKLEASE_HOME", h.state)
	h.setTasks(`[{"id":"TASK-1","title":"First","status":"Open","priority":"high","dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true},{"id":"TASK-2","title":"Second","status":"Open","priority":"medium","dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true}]`)
	st, err := store.Open(context.Background(), h.state, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if data, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge"); err != nil {
		t.Fatalf("confirm: %v: %s", err, data)
	}
	writeHandleFile = func(path string, value handle.Handle) error {
		if value.State == "ready" {
			return errors.New("injected handle write failure")
		}
		return handle.Write(path, value)
	}
	defer func() { writeHandleFile = nil }()
	data, err := h.run("queue", "next", "--view", "Ready", "--claim", "--session", "uncertain-worker", "--json")
	if err == nil || !strings.Contains(string(data), "pendingPath") || !strings.Contains(string(data), "committed") {
		t.Fatalf("missing uncertain acquire recovery: %v: %s", err, data)
	}
	writeHandleFile = nil
	claims, err := h.run("list", "--json")
	if err != nil || !strings.Contains(string(claims), "TASK-1") || strings.Contains(string(claims), "TASK-2") {
		t.Fatalf("uncertain acquire tried another candidate: %v: %s", err, claims)
	}
}

func TestQueueNextRejectsChangedRemoteProfileBeforeAcquire(t *testing.T) {
	controller, backend, _ := newRemoteQueueClaimController(t, 30*time.Second, queueClaimItem("tasks", "1"))
	backend.Config.SessionID = "profile-worker"
	profiles, defaultName, err := config.LoadProfiles(config.UserProfilePaths(os.Getenv))
	if err != nil {
		t.Fatal(err)
	}
	changed := profiles[controller.profileName]
	changed.AuthorityID = strings.Repeat("f", 32)
	if err := config.SaveProfiles(config.UserProfilePaths(os.Getenv), []config.Profile{changed}, defaultName); err != nil {
		t.Fatal(err)
	}
	key, err := resource.Resolve(resource.Input{Provider: "generic", Source: "portable", Item: "1"})
	if err != nil {
		t.Fatal(err)
	}
	selected, _ := controller.current()
	_, err = acquireQueueWorker(context.Background(), &urfave.Command{}, backend, selected, []string{key.Resource}, "")
	if err == nil || !strings.Contains(err.Error(), "queue authority identity changed") {
		t.Fatalf("profile drift not rejected: %v", err)
	}
	status, err := backend.API.Status(context.Background(), lease.Selector{AuthorityID: backend.AuthorityID(), Resources: []string{key.Resource}})
	if err != nil || status.Claim != nil {
		t.Fatalf("claim dispatched despite profile change: %+v %v", status, err)
	}
}

func TestQueueNextEightConcurrentLocalWorkers(t *testing.T) {
	if isolateCLIProcess(t) {
		return
	}
	testQueueNextConcurrentWorkers(t, false)
}
func TestQueueNextEightConcurrentRemoteWorkers(t *testing.T) {
	if isolateCLIProcess(t) {
		return
	}
	testQueueNextConcurrentWorkers(t, true)
}

func testQueueNextConcurrentWorkers(t *testing.T, remote bool) {
	h := newQueueQueryHarness(t)
	t.Setenv("WORKLEASE_HOME", h.state)
	if remote {
		profile, _, _ := remoteCLIFixture(t)
		configText := strings.Replace(h.queueConfig, "authority: local", "authority: "+profile, 1)
		if err := os.WriteFile(config.QueuePath(os.Getenv), []byte(configText), 0600); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Setenv("WORKLEASE_PROFILE", "")
	}
	var tasks strings.Builder
	for i := 0; i < 8; i++ {
		if i > 0 {
			tasks.WriteByte(',')
		}
		fmt.Fprintf(&tasks, `{"id":"TASK-%d","title":"Item","status":"Open","ordinal":%d,"dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true}`, i, i)
	}
	h.setTasks("[" + tasks.String() + "]")
	st, err := store.Open(context.Background(), h.state, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if data, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge"); err != nil {
		t.Fatalf("confirm: %v: %s", err, data)
	}
	if next := nextResult(t, h); next["result"] != "ready" {
		t.Fatalf("prewarm: %#v", next)
	}
	var wg sync.WaitGroup
	results := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			session := fmt.Sprintf("worker-%d", i)
			data, err := h.run("queue", "next", "--view", "Ready", "--json", "--claim", "--ttl", "30s", "--session", session)
			if err != nil {
				results <- fmt.Sprintf("error: %v: %s", err, data)
				return
			}
			var response struct {
				Next struct {
					Result     string `json:"result"`
					Acquired   bool   `json:"acquired"`
					Candidates []struct {
						Ref struct {
							ItemID string `json:"itemId"`
						} `json:"ref"`
						Resources []string `json:"resources"`
					} `json:"candidates"`
					Claim struct {
						ClaimID   string   `json:"claimId"`
						SessionID string   `json:"sessionId"`
						Resources []string `json:"resources"`
					} `json:"claim"`
				} `json:"next"`
			}
			// Each worker must hold exactly the claim for the item it reports.
			if err := json.Unmarshal(data, &response); err != nil || response.Next.Result != "ready" || !response.Next.Acquired || len(response.Next.Candidates) != 1 || response.Next.Claim.ClaimID == "" || response.Next.Claim.SessionID != session || len(response.Next.Claim.Resources) != len(response.Next.Candidates[0].Resources) {
				results <- fmt.Sprintf("bad: %v: %s", err, data)
				return
			}
			results <- response.Next.Candidates[0].Ref.ItemID
		}(i)
	}
	wg.Wait()
	close(results)
	seen := make(map[string]bool)
	for result := range results {
		if seen[result] || !strings.HasPrefix(result, "TASK-") {
			t.Fatalf("duplicate or failure: %s; seen=%v", result, seen)
		}
		seen[result] = true
	}
	if len(seen) != 8 {
		t.Fatalf("claimed %d distinct items, want 8", len(seen))
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
	claim := nextResult(t, h, "--claim", "--session", "incomplete-worker")
	if claim["result"] != "incomplete" || claim["acquired"] != false {
		t.Fatalf("partial graph acquired: %#v", claim)
	}
}
