package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueindex"
	"github.com/brettinternet/worklease/internal/testkit"
	urfavecli "github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

func TestQueueQueryConcurrentProcessHelper(t *testing.T) {
	root := os.Getenv("QUEUE_QUERY_CONCURRENT_ROOT")
	if root == "" {
		return
	}
	for key, value := range map[string]string{
		"HOME":              filepath.Join(root, "home"),
		"XDG_CONFIG_HOME":   filepath.Join(root, "config"),
		"XDG_CACHE_HOME":    filepath.Join(root, "cache"),
		"QUEUE_LIST_JSON":   `{"kind":"task-list","schemaVersion":1,"tasks":[{"id":"TASK-CONCURRENT","title":"Concurrent refresh","status":"Open","ordinal":1,"isReady":true}]}`,
		"QUEUE_LIST_MARKER": filepath.Join(root, "list-count"),
	} {
		if err := os.Setenv(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("ready-%d", os.Getpid())), nil, 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "start")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("concurrent CLI process never received start signal")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"worklease", "--home", filepath.Join(root, "state"), "queue", "query", "--view", "Ready", "--json"}
	if err := Run(context.Background(), args, "test", "unknown", "unknown", &stdout, &stderr); err != nil {
		t.Fatalf("queue query failed: %v: %s", err, stderr.String())
	}
}

func TestQueueQueryConcurrentProcessesShareOneRefreshWithoutMaxAge(t *testing.T) {
	h := newQueueQueryHarness(t)
	root := filepath.Dir(h.home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	marker := filepath.Join(root, "list-count")
	t.Setenv("QUEUE_LIST_MARKER", marker)
	if err := os.MkdirAll(filepath.Join(root, "cache"), 0700); err != nil {
		t.Fatal(err)
	}
	h.setTasks(`[{"id":"TASK-CONCURRENT","title":"Concurrent refresh","status":"Open","ordinal":1,"isReady":true}]`)
	queueConfig := strings.Replace(h.queueConfig, "    claims: {policy: generic, source: brettinternet/worklease/backlog}\n", "", 1)
	h.writeQueueConfig(queueConfig)
	binary := filepath.Join(root, "bin", "backlog")
	script := `#!/bin/sh
case "$*" in
  --version) printf '1.52.0\n' ;;
  'config get autoCommit') printf 'false\n' ;;
  'config get '*) printf 'false\n' ;;
  'task list --json') printf x >> "$QUEUE_LIST_MARKER"; sleep 1; printf '%s\n' "$QUEUE_LIST_JSON" ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	sharedIndex, err := queueindex.Open(context.Background(), filepath.Join(root, "cache", "worklease", "queue"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sharedIndex.Close(); err != nil {
		t.Fatal(err)
	}
	commands := make([]*exec.Cmd, 2)
	var outputs [2]bytes.Buffer
	for index := range commands {
		command := exec.Command(os.Args[0], "-test.run=^TestQueueQueryConcurrentProcessHelper$")
		command.Env = testkit.Environment(os.Environ(), map[string]string{"QUEUE_QUERY_CONCURRENT_ROOT": root})
		command.Stdout = &outputs[index]
		command.Stderr = &outputs[index]
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		commands[index] = command
	}
	defer func() {
		for _, command := range commands {
			if command.Process != nil && command.ProcessState == nil {
				_ = command.Process.Kill()
				_ = command.Wait()
			}
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		ready, err := filepath.Glob(filepath.Join(root, "ready-*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(ready) == len(commands) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d concurrent CLI processes became ready", len(ready))
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.WriteFile(filepath.Join(root, "start"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	for index, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatalf("queue query process %d: %v: %s", index, err, outputs[index].String())
		}
	}
	countRefreshes := func() int {
		t.Helper()
		data, err := os.ReadFile(marker)
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return len(data)
	}
	if got := countRefreshes(); got != 1 {
		t.Fatalf("concurrent queue queries refreshed provider %d times, want one", got)
	}
	if _, err := h.run("queue", "query", "--view", "Ready", "--json"); err != nil {
		t.Fatal(err)
	}
	if got := countRefreshes(); got != 2 {
		t.Fatalf("independent queue query did not refresh provider: calls=%d", got)
	}
}

func TestCompletedRefreshCanBeReusedWithoutMaxAge(t *testing.T) {
	before := time.Now().Add(-time.Minute)
	after := time.Now()
	if !completedRefreshObserved(before, after, queue.Coverage{State: queue.CoverageComplete}, false) {
		t.Fatal("waiter did not reuse newly completed refresh")
	}
	if completedRefreshObserved(after, before, queue.Coverage{State: queue.CoverageComplete}, false) {
		t.Fatal("independent later call reused prior refresh")
	}
	if completedRefreshObserved(before, after, queue.Coverage{State: queue.CoveragePartial}, false) {
		t.Fatal("incomplete refresh was reused")
	}
}

func TestQueueQueryIsRegisteredAndDocumentsBoundedReadOnlyInterface(t *testing.T) {
	root := NewRootCommand("test", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	var queryPath bool
	var find func([]*urfavecli.Command, string)
	find = func(commands []*urfavecli.Command, path string) {
		for _, command := range commands {
			current := strings.TrimSpace(path + " " + command.Name)
			if current == "queue query" {
				queryPath = true
				if !strings.Contains(command.UsageText, "--require-complete") || !strings.Contains(command.UsageText, "--cursor") || !strings.Contains(command.UsageText, "--max-age") {
					t.Fatalf("usage misses query flags: %s", command.UsageText)
				}
				for _, name := range []string{"limit", "cursor", "max-age", "require-complete"} {
					found := false
					for _, flag := range command.Flags {
						for _, alias := range flag.Names() {
							if alias == name {
								found = true
							}
						}
					}
					if !found {
						t.Errorf("query is missing --%s", name)
					}
				}
			}
			find(command.Commands, current)
		}
	}
	find(root.Commands, "")
	if !queryPath {
		t.Fatal("queue query command is not registered")
	}
}

func TestQueueQueryFingerprintBindsPrincipalAndConfigurationGeneration(t *testing.T) {
	view := &config.QueueView{Name: "Ready", Sources: []string{"source"}}
	item := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "source", ItemID: "1"}}, Observation: queue.Observation{Principal: "alice", ConfigurationGeneration: "generation-1"}}
	base := queueQueryFingerprint(view, nil, nil, nil, queueAuthorityJSON{Profile: "local", Scope: "local"}, map[string]queue.Coverage{}, []queue.Item{item})
	changedPrincipal := item
	changedPrincipal.Observation.Principal = "bob"
	if base == queueQueryFingerprint(view, nil, nil, nil, queueAuthorityJSON{Profile: "local", Scope: "local"}, map[string]queue.Coverage{}, []queue.Item{changedPrincipal}) {
		t.Fatal("principal change did not invalidate cursor fingerprint")
	}
	changedGeneration := item
	changedGeneration.Observation.ConfigurationGeneration = "generation-2"
	if base == queueQueryFingerprint(view, nil, nil, nil, queueAuthorityJSON{Profile: "local", Scope: "local"}, map[string]queue.Coverage{}, []queue.Item{changedGeneration}) {
		t.Fatal("configuration generation change did not invalidate cursor fingerprint")
	}
	filteredView := &config.QueueView{Name: "Filtered", Sources: []string{"source"}, Filter: config.QueueFilter{Assigned: []string{"nobody"}}}
	hidden := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "source", ItemID: "hidden"}, AssignedTo: []string{"other"}}}
	hiddenFingerprint := queueQueryFingerprint(filteredView, nil, nil, nil, queueAuthorityJSON{Profile: "local", Scope: "local"}, map[string]queue.Coverage{}, []queue.Item{item, hidden})
	hidden.Title = "changed while filtered out"
	if hiddenFingerprint == queueQueryFingerprint(filteredView, nil, nil, nil, queueAuthorityJSON{Profile: "local", Scope: "local"}, map[string]queue.Coverage{}, []queue.Item{item, hidden}) {
		t.Fatal("filtered-out source content did not invalidate cursor fingerprint")
	}
	var bob yaml.Node
	if err := yaml.Unmarshal([]byte("bob"), &bob); err != nil {
		t.Fatal(err)
	}
	if base == queueQueryFingerprint(view, nil, map[string]yaml.Node{"backlog-md": bob}, nil, queueAuthorityJSON{Profile: "local", Scope: "local"}, map[string]queue.Coverage{}, []queue.Item{item}) {
		t.Fatal("me mapping change did not invalidate cursor fingerprint")
	}
	emptyView := &config.QueueView{Name: "Empty", Sources: []string{"source"}}
	if queueQueryFingerprint(emptyView, nil, nil, map[string]string{"source": "token-a"}, queueAuthorityJSON{}, nil, nil) == queueQueryFingerprint(emptyView, nil, nil, map[string]string{"source": "token-b"}, queueAuthorityJSON{}, nil, nil) {
		t.Fatal("credential rotation with an empty result set did not invalidate cursor")
	}
	transientTime := item
	transientTime.Observation.ObservedAt = time.Now()
	if base != queueQueryFingerprint(view, nil, nil, nil, queueAuthorityJSON{Profile: "local", Scope: "local"}, map[string]queue.Coverage{}, []queue.Item{transientTime}) {
		t.Fatal("observation timestamp changed stable cursor fingerprint")
	}
}

func TestQueueQueryEndToEndJSONCursorIdentityTextAndCompleteness(t *testing.T) {
	h := newQueueQueryHarness(t)
	h.setTasks(`[{ 
	  "id":"TASK-126","title":"` + strings.Repeat("a", 64) + `","status":"Open","priority":"high","ordinal":1,"isReady":true
	},{"id":"TASK-127","title":"Beta","status":"Open","priority":"medium","ordinal":2,"isReady":true
	}]`)
	if !json.Valid([]byte(h.list)) {
		t.Fatalf("invalid fixture list: %s", h.list)
	}
	data, err := h.run("queue", "query", "--view", "Ready", "--limit", "1", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	query := response["query"].(map[string]any)
	if query["schemaVersion"] != float64(1) {
		t.Fatalf("schema version: %#v", query["schemaVersion"])
	}
	source := query["sources"].([]any)[0].(map[string]any)
	if !containsString(source["diagnostics"].([]any), "commit-effects") {
		t.Fatalf("source diagnostics absent: %#v", source)
	}
	rows := query["items"].([]any)
	if len(rows) == 0 {
		t.Fatalf("configured nobody filter returned no items: %s", data)
	}
	item := rows[0].(map[string]any)
	if item["ref"].(map[string]any)["itemId"] != "TASK-126" {
		t.Fatalf("wrong first page: %#v", item)
	}
	keyInputs, ok := item["keyInputs"].(map[string]any)
	if !ok || keyInputs["provider"] != "generic" {
		t.Fatalf("missing key inputs in %s", data)
	}
	if item["actions"] == nil {
		t.Fatal("missing per-action eligibility")
	}
	// Explicit max-age should reuse the local-source index and report provenance.
	cached, cacheErr := h.run("queue", "query", "--view", "Ready", "--max-age", "1h", "--json")
	if cacheErr != nil {
		t.Fatal(cacheErr)
	}
	var cachedResponse map[string]any
	if err := json.Unmarshal(cached, &cachedResponse); err != nil {
		t.Fatal(err)
	}
	cachedQuery := cachedResponse["query"].(map[string]any)
	cachedSource := cachedQuery["sources"].([]any)[0].(map[string]any)
	if cachedSource["servedFromIndex"] != true || cachedSource["observedAt"] == nil {
		t.Fatalf("cache provenance absent: %#v", cachedSource)
	}
	if len(cachedQuery["items"].([]any)) != 2 {
		t.Fatalf("cached query items: %s", cached)
	}
	if strings.Contains(string(data), strings.Repeat("a", 64)) {
		t.Fatal("JSON output bypassed redaction")
	}
	resource := item["resources"].([]any)[0].(string)
	keyData, err := h.run("key", "--provider", "generic", "--source", "brettinternet/worklease/backlog", "--item", "TASK-126", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var key map[string]any
	if err := json.Unmarshal(keyData, &key); err != nil {
		t.Fatal(err)
	}
	if resource != "coordination:generic:f6e6df0bd554949ea97c94ccd9de78e2a656432654d7057ec4a56c3bb8062cd1" {
		t.Fatalf("query identity differs from TASK-126.4 vector: %v", resource)
	}
	if key["resource"] != resource {
		t.Fatalf("query resource differs from worklease key: %v != %v", resource, key["resource"])
	}
	cursor := query["nextCursor"].(string)
	second, err := h.run("queue", "query", "--view", "Ready", "--limit", "1", "--cursor", cursor, "--json")
	if err != nil {
		t.Fatal(err)
	}
	var secondResponse map[string]any
	if err := json.Unmarshal(second, &secondResponse); err != nil {
		t.Fatal(err)
	}
	secondItem := secondResponse["query"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if secondItem["ref"].(map[string]any)["itemId"] != "TASK-127" {
		t.Fatalf("wrong second page: %#v", secondItem)
	}
	incomplete, err := h.runWithError("queue", "query", "--view", "Ready", "--limit", "1", "--require-complete", "--json")
	if err == nil {
		t.Fatal("expected incomplete query failure")
	}
	var failure map[string]any
	if json.Unmarshal(incomplete, &failure) != nil || failure["ok"] != false || failure["error"].(map[string]any)["reason"] != "incomplete" || failure["error"].(map[string]any)["details"].(map[string]any)["result"] != "incomplete" || failure["error"].(map[string]any)["details"].(map[string]any)["query"].(map[string]any)["schemaVersion"] != float64(1) {
		t.Fatalf("incomplete response not structured: %s", incomplete)
	}
	zero, zeroErr := h.run("queue", "query", "--view", "Ready", "--limit", "0", "--json")
	if zeroErr == nil {
		t.Fatal("explicit zero limit accepted")
	}
	var zeroFailure map[string]any
	if json.Unmarshal(zero, &zeroFailure) != nil || zeroFailure["ok"] != false {
		t.Fatalf("limit error was not structured: %s", zero)
	}
	missing, missingErr := h.run("queue", "query", "--json")
	if missingErr == nil {
		t.Fatal("required view accepted as absent")
	}
	var missingFailure map[string]any
	if json.Unmarshal(missing, &missingFailure) != nil || missingFailure["ok"] != false {
		t.Fatalf("argument error was not structured: %s", missing)
	}
	text, err := h.run("queue", "query", "--view", "Ready", "--limit", "1")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), strings.Repeat("a", 64)) {
		t.Fatal("text output bypassed redaction")
	}
	if !strings.Contains(string(text), "ID\tSTATE\tREADY\tCLAIM\tTITLE") {
		t.Fatalf("missing text table: %s", text)
	}
	// A cursor must bind to the full source snapshot, not only the returned page.
	h.setTasks(`[{"id":"TASK-126","title":"Alpha changed","status":"Open","ordinal":1,"isReady":true},{"id":"TASK-127","title":"Beta","status":"Open","ordinal":2,"isReady":true}]`)
	stale, staleErr := h.run("queue", "query", "--view", "Ready", "--limit", "1", "--cursor", cursor, "--json")
	if staleErr == nil {
		t.Fatal("snapshot mutation accepted stale cursor")
	}
	var staleFailure map[string]any
	if json.Unmarshal(stale, &staleFailure) != nil || staleFailure["error"].(map[string]any)["reason"] != "cursor-invalid" {
		t.Fatalf("unexpected stale cursor result: %s", stale)
	}
	generationCheckout := filepath.Join(filepath.Dir(h.home), "checkout-next")
	if err := os.MkdirAll(generationCheckout, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(generationCheckout, "backlog.config.yml"), []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changedConfig := strings.Replace(h.queueConfig, strings.TrimSpace(strings.Split(strings.Split(h.queueConfig, "checkout: ")[1], "\n")[0]), generationCheckout, 1)
	h.writeQueueConfig(changedConfig)
	generationResult, generationErr := h.run("queue", "query", "--view", "Ready", "--limit", "1", "--cursor", cursor, "--json")
	if generationErr == nil {
		t.Fatal("source generation change accepted stale cursor")
	}
	var generationFailure map[string]any
	if json.Unmarshal(generationResult, &generationFailure) != nil || generationFailure["error"].(map[string]any)["reason"] != "cursor-invalid" {
		t.Fatalf("unexpected generation cursor result: %s (%v)", generationResult, generationErr)
	}
}

func TestQueueQueryCacheOnlyCoverageDoesNotInvalidateCursor(t *testing.T) {
	h := newQueueQueryHarness(t)
	h.setTasks(`[{"id":"TASK-1","title":"First","status":"Open","ordinal":1,"isReady":true},{"id":"TASK-2","title":"Second","status":"Open","ordinal":2,"isReady":true}]`)
	first, err := h.run("queue", "query", "--view", "Ready", "--max-age", "1h", "--limit", "1", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var firstResponse struct {
		Query struct {
			NextCursor string `json:"nextCursor"`
		} `json:"query"`
	}
	if err := json.Unmarshal(first, &firstResponse); err != nil {
		t.Fatal(err)
	}
	if firstResponse.Query.NextCursor == "" {
		t.Fatalf("first page omitted cursor: %s", first)
	}
	second, err := h.run("queue", "query", "--view", "Ready", "--max-age", "1h", "--limit", "1", "--cursor", firstResponse.Query.NextCursor, "--json")
	if err != nil {
		t.Fatalf("warm cached second page rejected cursor: %v; %s", err, second)
	}
}

func TestQueueQueryRetainsStaleIndexOnFailedRefresh(t *testing.T) {
	h := newQueueQueryHarness(t)
	h.setTasks(`[{"id":"TASK-1","title":"Retained","status":"Open","ordinal":1,"isReady":true}]`)
	if _, err := h.run("queue", "query", "--view", "Ready", "--json"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QUEUE_LIST_JSON", "invalid-list")
	data, err := h.run("queue", "query", "--view", "Ready", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Query struct {
			Items []struct {
				Title string `json:"title"`
			} `json:"items"`
			Sources []struct {
				Coverage queue.Coverage `json:"coverage"`
			} `json:"sources"`
		} `json:"query"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Query.Items) != 1 || envelope.Query.Items[0].Title != "Retained" || envelope.Query.Sources[0].Coverage.State != queue.CoverageUnknown {
		t.Fatalf("failed refresh lost stale item or claimed completeness: %s", data)
	}
}

func TestQueueQueryReusesCompleteEmptyIndex(t *testing.T) {
	h := newQueueQueryHarness(t)
	h.setTasks(`[]`)
	if _, err := h.run("queue", "query", "--view", "Ready", "--json"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QUEUE_LIST_JSON", "invalid-list")
	data, err := h.run("queue", "query", "--view", "Ready", "--max-age", "1h", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Query struct {
			Items   []json.RawMessage `json:"items"`
			Sources []struct {
				Coverage        queue.Coverage `json:"coverage"`
				ServedFromIndex bool           `json:"servedFromIndex"`
			} `json:"sources"`
		} `json:"query"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Query.Items) != 0 || !envelope.Query.Sources[0].ServedFromIndex || envelope.Query.Sources[0].Coverage.State != queue.CoverageComplete {
		t.Fatalf("complete empty cache not reused: %s", data)
	}
}

func TestQueueQueryKeepsHealthySourceWhenAnotherCannotResolve(t *testing.T) {
	h := newQueueQueryHarness(t)
	h.setTasks(`[{"id":"TASK-126","title":"Healthy","status":"Open","ordinal":1,"isReady":true}]`)
	missing := filepath.Join(filepath.Dir(h.home), "empty-checkout")
	if err := os.MkdirAll(missing, 0700); err != nil {
		t.Fatal(err)
	}
	// Insert an existing checkout without a Backlog project into the configured source list.
	cfg := strings.Replace(h.queueConfig, "views:", fmt.Sprintf("  - id: missing\n    adapter: backlog-md\n    checkout: %s\nviews:", missing), 1)
	cfg = strings.Replace(cfg, "sources: [local]", "sources: [local, missing]", 1)
	h.writeQueueConfig(cfg)
	data, err := h.run("queue", "query", "--view", "Ready", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	query := response["query"].(map[string]any)
	if len(query["items"].([]any)) != 1 || !query["incomplete"].(bool) {
		t.Fatalf("healthy items lost or failure concealed: %s", data)
	}
	rows := query["sources"].([]any)
	missingRow := rows[1].(map[string]any)
	if missingRow["coverage"].(map[string]any)["state"] != "unknown" {
		t.Fatalf("missing failure coverage: %s", data)
	}
	diagnostics := missingRow["diagnostics"].([]any)
	if len(diagnostics) != 1 || diagnostics[0] != "source-resolve-failed" {
		t.Fatalf("unexpected missing-source diagnostics: %#v", diagnostics)
	}
	_, err = h.run("queue", "query", "--view", "Ready", "--require-complete", "--json")
	if err == nil {
		t.Fatal("incomplete source accepted by require-complete")
	}
}

func TestQueueQueryRequiresCompletenessForFilteredOutItems(t *testing.T) {
	h := newQueueQueryHarness(t)
	h.setTasks(`[{"id":"TASK-126","title":"Unknown dependency closure","status":"Open","ordinal":1,"isReady":true}]`)
	h.writeQueueConfig(strings.Replace(h.queueConfig, "filter: {assigned: [nobody]}", "filter: {readiness: ready}", 1))
	data, err := h.run("queue", "query", "--view", "Ready", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	query := response["query"].(map[string]any)
	if len(query["items"].([]any)) != 0 || query["incomplete"] != true {
		t.Fatalf("hidden unresolved item declared complete: %s", data)
	}
	_, err = h.run("queue", "query", "--view", "Ready", "--require-complete", "--json")
	if err == nil {
		t.Fatal("hidden incomplete dependency accepted")
	}
}

func TestQueueQueryReportsEachLaunchActionAndItsGate(t *testing.T) {
	h := newQueueQueryHarness(t)
	h.setTasks(`[{"id":"TASK-126","title":"Launch test","status":"Open","ordinal":1,"isReady":true}]`)
	cfg := h.queueConfig + "launch:\n  - name: first\n    argv: [echo, '--', '{itemId}']\n    cwd: '{checkout}'\n  - name: second\n    argv: [echo, '{ref}']\n    cwd: '{checkout}'\n"
	h.writeQueueConfig(cfg)
	data, err := h.run("queue", "query", "--view", "Ready", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Query struct {
			Items []struct {
				Launches []queue.LaunchOption         `json:"launches"`
				Actions  map[string]queue.Eligibility `json:"actions"`
			} `json:"items"`
		} `json:"query"`
	}
	if err := json.Unmarshal(data, &response); err != nil || len(response.Query.Items) != 1 {
		t.Fatalf("query: %s %v", data, err)
	}
	item := response.Query.Items[0]
	if len(item.Launches) != 2 || item.Launches[0].Name != "first" || item.Launches[1].Name != "second" || item.Launches[0].Eligibility.Eligible != item.Actions["launch"].Eligible {
		t.Fatalf("launch availability differs from action: %+v", item)
	}
	if item.Launches[0].Eligibility.Eligible && (len(item.Launches[0].Argv) != 3 || item.Launches[0].Cwd == "" || len(item.Launches[0].EnvNames) == 0) {
		t.Fatalf("launch preview missing: %+v", item.Launches[0])
	}
}

func TestQueueQueryRedactsNonDigestResources(t *testing.T) {
	h := newQueueQueryHarness(t)
	secretLikeID := strings.Repeat("a", 64)
	h.setTasks(fmt.Sprintf(`[{"id":%q,"title":"Secret-like item ID","status":"Open","ordinal":1,"isReady":true}]`, secretLikeID))
	cfg := strings.Replace(h.queueConfig, "    claims: {policy: generic, source: brettinternet/worklease/backlog}\n", "", 1)
	h.writeQueueConfig(cfg)
	data, err := h.run("queue", "query", "--view", "Ready", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secretLikeID) {
		t.Fatalf("non-digest identity bypassed redaction: %s", data)
	}
}

type queueQueryHarness struct {
	t           *testing.T
	state       string
	queueConfig string
	list        string
	home        string
}

func newQueueQueryHarness(t *testing.T) *queueQueryHarness {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	conf := filepath.Join(root, "config", "worklease")
	checkout := filepath.Join(root, "checkout")
	bin := filepath.Join(root, "bin")
	state := filepath.Join(root, "state")
	for _, dir := range []string{home, conf, checkout, bin, state} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := testkit.GitCommand("init", "-b", "main", checkout).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if output, err := testkit.GitCommand("-C", checkout, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
	if err := os.WriteFile(filepath.Join(checkout, "backlog.config.yml"), []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \"$*\" in\n  --version) printf '1.52.0\\n' ;;\n  'config get autoCommit') printf 'true\\n' ;;\n  'config get '*) printf 'false\\n' ;;\n  'task list --json') printf '%s\\n' \"$QUEUE_LIST_JSON\" ;;\n  task\\ view\\ TASK-*\\ --json) python3 -c 'import json,os,sys; tasks=json.loads(os.environ.get(\"QUEUE_VIEW_LIST_JSON\") or os.environ[\"QUEUE_LIST_JSON\"])[\"tasks\"]; selected=next(task for task in tasks if task[\"id\"]==sys.argv[1]); print(json.dumps({\"kind\":\"task-view\",\"schemaVersion\":1,\"task\":selected}))' \"$3\" ;;\n  *) exit 2 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "backlog"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Dir(conf))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("QUEUE_LIST_JSON", "")
	queueConfig := fmt.Sprintf("version: 1\nme: {}\nsources:\n  - id: local\n    adapter: backlog-md\n    checkout: %s\n    claims: {policy: generic, source: brettinternet/worklease/backlog}\nviews:\n  - name: Ready\n    authority: local\n    sources: [local]\n    filter: {assigned: [nobody]}\n", checkout)
	h := &queueQueryHarness{t: t, state: state, queueConfig: queueConfig, home: home}
	h.writeQueueConfig(queueConfig)
	return h
}
func containsString(values []any, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func (h *queueQueryHarness) writeQueueConfig(value string) {
	h.t.Helper()
	p := filepath.Join(filepath.Dir(h.home), "config", "worklease", "queue.yaml")
	if err := os.WriteFile(p, []byte(value), 0600); err != nil {
		h.t.Fatal(err)
	}
}
func (h *queueQueryHarness) setTasks(tasks string) {
	h.t.Helper()
	h.list = fmt.Sprintf(`{"kind":"task-list","schemaVersion":1,"tasks":%s}`, tasks)
	h.t.Setenv("QUEUE_LIST_JSON", h.list)
}
func (h *queueQueryHarness) run(args ...string) ([]byte, error) { return h.runWithError(args...) }
func (h *queueQueryHarness) runWithError(args ...string) ([]byte, error) {
	h.t.Helper()
	var stdout, stderr bytes.Buffer
	argv := append([]string{"worklease", "--home", h.state}, args...)
	err := Run(context.Background(), argv, "test", "unknown", "unknown", &stdout, &stderr)
	if stdout.Len() == 0 && stderr.Len() > 0 {
		return stderr.Bytes(), err
	}
	return stdout.Bytes(), err
}
