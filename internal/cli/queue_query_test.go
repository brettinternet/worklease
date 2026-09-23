package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/queue"
	urfavecli "github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

func TestQueueQueryIsRegisteredAndDocumentsBoundedReadOnlyInterface(t *testing.T) {
	root := NewRootCommand("test", "unknown", "unknown", &bytes.Buffer{}, &bytes.Buffer{})
	var queryPath bool
	var find func([]*urfavecli.Command, string)
	find = func(commands []*urfavecli.Command, path string) {
		for _, command := range commands {
			current := strings.TrimSpace(path + " " + command.Name)
			if current == "queue query" {
				queryPath = true
				if !strings.Contains(command.UsageText, "--require-complete") || !strings.Contains(command.UsageText, "--cursor") {
					t.Fatalf("usage misses query flags: %s", command.UsageText)
				}
				for _, name := range []string{"limit", "cursor", "require-complete"} {
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
	if rows[1].(map[string]any)["coverage"].(map[string]any)["state"] != "unknown" {
		t.Fatalf("missing failure coverage: %s", data)
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
	if err := os.WriteFile(filepath.Join(checkout, "backlog.config.yml"), []byte("version: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \"$*\" in\n  --version) printf '1.52.0\\n' ;;\n  'config get autoCommit') printf 'true\\n' ;;\n  'config get '*) printf 'false\\n' ;;\n  'task list --json') printf '%s\\n' \"$QUEUE_LIST_JSON\" ;;\n  *) exit 2 ;;\nesac\n"
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
