package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/mcp"
	"github.com/brettinternet/worklease/internal/store"
)

func queueMCPServer(t *testing.T, h *queueQueryHarness) *mcp.Server {
	t.Helper()
	s, err := mcp.NewServer(mcp.Options{Home: h.state, ProfileName: config.LocalProfileName, TTL: 30 * time.Second, QueueNext: mcpQueueNext(h.state, config.LocalProfileName)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func TestMCPQueueNextLazyConfigAndLifecycle(t *testing.T) {
	h := newQueueQueryHarness(t)
	t.Setenv("WORKLEASE_HOME", h.state)
	s := queueMCPServer(t, h)
	if result, err := s.Call(context.Background(), "key", map[string]any{"provider": "generic", "source": "test", "item": "one"}); err != nil || result["isError"] == true {
		t.Fatalf("unrelated MCP tool requires queue config: %v %#v", err, result)
	}
	if err := os.WriteFile(config.QueuePath(os.Getenv), []byte("invalid: ["), 0600); err != nil {
		t.Fatal(err)
	}
	if result, err := s.Call(context.Background(), "queue_next", map[string]any{"view": "Ready"}); err != nil || result["isError"] != true {
		t.Fatalf("invalid configuration did not yield tool error: %v %#v", err, result)
	}
	h.writeQueueConfig(h.queueConfig)
	h.setTasks(`[{"id":"TASK-1","title":"First","status":"Open","dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true}]`)
	st, err := store.Open(context.Background(), h.state, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	if data, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge"); err != nil {
		t.Fatalf("confirm: %v: %s", err, data)
	}
	observed, err := s.Call(context.Background(), "queue_next", map[string]any{"view": "Ready"})
	if err != nil || observed["isError"] == true {
		t.Fatalf("observe: %v %#v", err, observed)
	}
	next := observed["structuredContent"].(map[string]any)["next"].(map[string]any)
	if next["result"] != "ready" || next["acquired"] != false {
		t.Fatalf("read-only result: %#v", next)
	}
	cliNext := nextResult(t, h)
	mcpResource := next["candidates"].([]any)[0].(map[string]any)["resources"].([]any)[0]
	cliResource := cliNext["candidates"].([]any)[0].(map[string]any)["resources"].([]any)[0]
	if mcpResource != cliResource {
		t.Fatalf("MCP candidate resource differs from CLI: %v != %v", mcpResource, cliResource)
	}
	manual, err := s.Call(context.Background(), "acquire", map[string]any{"resources": []any{mcpResource}, "sessionId": "manual-worker", "autoHeartbeat": false})
	if err != nil || manual["isError"] == true {
		t.Fatalf("manual acquire of MCP candidate resource: %v %#v", err, manual)
	}
	if contested := nextResult(t, h, "--claim", "--session", "cli-contender"); contested["result"] != "active-claims" {
		t.Fatalf("CLI did not contend on MCP candidate resource: %#v", contested)
	}
	manualLease := manual["structuredContent"].(map[string]any)["lease"].(string)
	if released, err := s.Call(context.Background(), "release", map[string]any{"lease": manualLease, "reason": "manual candidate identity verified"}); err != nil || released["isError"] == true {
		t.Fatalf("manual release: %v %#v", err, released)
	}
	claimed, err := s.Call(context.Background(), "queue_next", map[string]any{"view": "Ready", "claim": true, "sessionId": "mcp-worker", "ttl": float64(30), "maxHold": float64(60), "autoHeartbeat": false})
	if err != nil || claimed["isError"] == true {
		t.Fatalf("claim: %v %#v", err, claimed)
	}
	next = claimed["structuredContent"].(map[string]any)["next"].(map[string]any)
	grant := next["claim"].(map[string]any)
	lease := grant["lease"].(string)
	if next["acquired"] != true || len(lease) != 32 || grant["autoHeartbeat"] != "disabled" {
		t.Fatalf("MCP lease missing: %#v", next)
	}
	for _, name := range []string{"verify", "heartbeat", "checkpoint", "release"} {
		args := map[string]any{"lease": lease}
		if name == "checkpoint" {
			args["data"] = map[string]any{"phase": "verified"}
		}
		if name == "release" {
			args["reason"] = "test checkpoint verified"
		}
		result, err := s.Call(context.Background(), name, args)
		if err != nil || result["isError"] == true {
			t.Fatalf("%s: %v %#v", name, err, result)
		}
	}
}

func TestMCPQueueNextEightMixedContenders(t *testing.T) {
	if isolateCLIProcess(t) {
		return
	}
	h := newQueueQueryHarness(t)
	t.Setenv("WORKLEASE_HOME", h.state)
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
	_ = st.Close()
	if data, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge"); err != nil {
		t.Fatalf("confirm: %v: %s", err, data)
	}
	if result := nextResult(t, h); result["result"] != "ready" {
		t.Fatalf("prewarm: %#v", result)
	}
	results := make(chan string, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				s, err := mcp.NewServer(mcp.Options{Home: h.state, ProfileName: config.LocalProfileName, TTL: 30 * time.Second, QueueNext: mcpQueueNext(h.state, config.LocalProfileName)})
				if err != nil {
					results <- fmt.Sprint(err)
					return
				}
				defer s.Close()
				result, err := s.Call(context.Background(), "queue_next", map[string]any{"view": "Ready", "claim": true, "sessionId": fmt.Sprintf("mcp-%d", i), "autoHeartbeat": false})
				if err != nil || result["isError"] == true {
					results <- fmt.Sprintf("error: %v %#v", err, result)
					return
				}
				next := result["structuredContent"].(map[string]any)["next"].(map[string]any)
				if next["result"] != "ready" || next["claim"].(map[string]any)["lease"] == nil {
					results <- fmt.Sprintf("bad: %#v", next)
					return
				}
				results <- next["candidates"].([]any)[0].(map[string]any)["ref"].(map[string]any)["itemId"].(string)
				return
			}
			data, err := h.run("queue", "next", "--view", "Ready", "--json", "--claim", "--session", fmt.Sprintf("cli-%d", i))
			if err != nil {
				results <- fmt.Sprintf("cli: %v %s", err, data)
				return
			}
			var response struct {
				Next struct {
					Candidates []struct {
						Ref struct {
							ItemID string `json:"itemId"`
						} `json:"ref"`
					} `json:"candidates"`
				} `json:"next"`
			}
			if err := json.Unmarshal(data, &response); err != nil || len(response.Next.Candidates) != 1 {
				results <- fmt.Sprintf("cli result: %v %s", err, data)
				return
			}
			results <- response.Next.Candidates[0].Ref.ItemID
		}(i)
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for result := range results {
		if seen[result] || !strings.HasPrefix(result, "TASK-") {
			t.Fatalf("duplicate or failure: %s; seen=%v", result, seen)
		}
		seen[result] = true
	}
	if len(seen) != 8 {
		t.Fatalf("claimed %d distinct items", len(seen))
	}
}

func TestMCPQueueNextRejectsRemoteProfileDrift(t *testing.T) {
	h := newQueueQueryHarness(t)
	t.Setenv("WORKLEASE_HOME", h.state)
	profileName, _, _ := remoteCLIFixture(t)
	profiles, defaultName, err := config.LoadProfiles(config.UserProfilePaths(os.Getenv))
	if err != nil {
		t.Fatal(err)
	}
	pinned := profiles[profileName]
	queueConfig := strings.Replace(h.queueConfig, "authority: local", "authority: "+profileName, 1)
	if err := os.WriteFile(config.QueuePath(os.Getenv), []byte(queueConfig), 0600); err != nil {
		t.Fatal(err)
	}
	h.setTasks(`[{"id":"TASK-1","title":"First","status":"Open","dependencies":[],"readiness":{"missingDependencies":[]},"isReady":true}]`)
	st, err := store.Open(context.Background(), h.state, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	if data, err := h.run("queue", "--view", "Ready", "identity", "confirm", "--source", "local", "--acknowledge"); err != nil {
		t.Fatalf("confirm: %v: %s", err, data)
	}
	s, err := mcp.NewServer(mcp.Options{Home: h.state, Profile: &pinned, ProfileName: profileName, TTL: 30 * time.Second, QueueNext: mcpQueueNext(h.state, profileName)})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// The remote may be restored under the same authority ID and name. The
	// running MCP server must not acquire against its stale profile snapshot.
	changed := pinned
	changed.RestoreID = strings.Repeat("f", len(pinned.RestoreID))
	if err := config.SaveProfiles(config.UserProfilePaths(os.Getenv), []config.Profile{changed}, defaultName); err != nil {
		t.Fatal(err)
	}
	result, err := s.Call(context.Background(), "queue_next", map[string]any{"view": "Ready", "claim": true, "sessionId": "profile-worker", "autoHeartbeat": false})
	if err != nil || result["isError"] != true {
		t.Fatalf("stale profile accepted: %v %#v", err, result)
	}
	if details := fmt.Sprint(result["structuredContent"]); !strings.Contains(details, "authority-mismatch") {
		t.Fatalf("unexpected profile drift result: %s", details)
	}
}

func TestMCPDoesNotImportTUI(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "./internal/mcp")
	cmd.Dir = "../.."
	data, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range strings.Split(string(data), "\n") {
		if strings.Contains(pkg, "bubbletea") || strings.HasSuffix(pkg, "/internal/queueui") {
			t.Fatalf("MCP imports TUI package %s", pkg)
		}
	}
}
