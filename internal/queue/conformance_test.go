package queue

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/resource"
	"github.com/brettinternet/worklease/internal/sampleadapter"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestAdapterConformanceSampleProcessHelper(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) && os.Args[i+1] == "sample" {
			if err := sampleadapter.Run(os.Stdin, os.Stdout); err != nil {
				os.Exit(1)
			}
			return
		}
	}
}

// TestAdapterConformanceProcessHelper is a test-only wire shim for the two built-in
// adapters. Both are exercised by ExternalAdapter and its production process host.
func TestAdapterConformanceProcessHelper(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	kind := os.Args[separator+1]
	reader := bufio.NewScanner(os.Stdin)
	reader.Buffer(make([]byte, 4096), externalFrameLimit)
	var adapter Adapter
	var source Source
	var cancelMarker string
	var outputMu, pendingMu sync.Mutex
	pending := make(map[string]context.CancelFunc)
	type wireRequest struct {
		ID     string                     `json:"id"`
		Method string                     `json:"method"`
		Params map[string]json.RawMessage `json:"params"`
	}
	respond := func(request wireRequest, ctx context.Context) {
		var result any
		var diagnostic string
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1, "manifest": map[string]any{"id": "conformance.shim", "version": "1.0.0", "protocol": map[string]int{"minMajor": 1, "maxMajor": 1}, "configSchema": map[string]any{"type": "object"}, "authentication": []string{}, "resourcePolicy": "generic", "capabilities": []string{"identity", "discovery", "dependencies", "state", "mutation"}, "requiredFeatures": []string{}}}
		case "resolve":
			var cfg map[string]string
			if json.Unmarshal(request.Params["config"], &cfg) != nil {
				return
			}
			cancelMarker = cfg["cancelMarker"]
			switch kind {
			case "backlog-md":
				built := NewBacklogAdapter("Done")
				built.Binary = cfg["binary"]
				adapter = built
				source, _ = adapter.Resolve(context.Background(), map[string]string{"id": requestSourceID(request.Params), "checkout": cfg["checkout"]})
			case "github":
				built := NewGitHubAdapter()
				built.Binary = cfg["binary"]
				built.APIBase = cfg["apiBase"]
				adapter = built
				source, _ = adapter.Resolve(context.Background(), map[string]string{"id": requestSourceID(request.Params), "host": "github.com", "repository": "org/repo", "account": "tester"})
			}
			if source.ID == "" {
				diagnostic = "unavailable-source"
				break
			}
			result = map[string]any{"context": conformanceContext(source.ID, "complete"), "source": map[string]string{"id": source.ID, "name": source.Name, "locator": source.Locator}}
		case "capabilities":
			ref := requestRef(request.Params)
			var pointer *Ref
			if ref.ItemID != "" {
				pointer = &ref
			}
			caps, err := adapter.Capabilities(context.Background(), source, "tester", pointer)
			if err != nil {
				diagnostic = "unavailable-source"
				break
			}
			result = map[string]any{"context": conformanceContext(source.ID, "complete"), "capabilities": caps}
		case "list":
			var cursor *string
			_ = json.Unmarshal(request.Params["cursor"], &cursor)
			page := ""
			if cursor != nil {
				page = *cursor
			}
			providerCursor := page
			start := 0
			if kind == "backlog-md" {
				providerCursor = ""
				if page != "" {
					start, _ = strconv.Atoi(page)
				}
			}
			items, err := adapter.List(ctx, source, Query{Budget: 1}, providerCursor)
			if ctx.Err() != nil && cancelMarker != "" {
				_ = os.WriteFile(cancelMarker+".done", []byte("cancelled"), 0600)
			}
			if err != nil || start < 0 || start > len(items.Items) {
				diagnostic = "unavailable-source"
				break
			}
			end := len(items.Items)
			if kind == "backlog-md" && end > start+1 {
				end = start + 1
			}
			wire := make([]any, 0, end-start)
			for _, item := range items.Items[start:end] {
				wire = append(wire, conformanceItem(item, ""))
			}
			state := "complete"
			var next any
			if kind == "backlog-md" && end < len(items.Items) {
				state, next = "partial", strconv.Itoa(end)
			} else if items.NextCursor != "" {
				state, next = "partial", items.NextCursor
			}
			result = map[string]any{"context": conformanceContext(source.ID, state), "items": wire, "nextCursor": next, "total": map[string]any{"value": len(items.Items), "accuracy": "estimated"}}
		case "readItems":
			var refs []Ref
			_ = json.Unmarshal(request.Params["refs"], &refs)
			outcomes := adapter.ReadItems(context.Background(), source, refs, nil, len(refs))
			wire := make([]any, 0, len(outcomes))
			for _, outcome := range outcomes {
				status := outcome.Kind
				if status == "" || status == "failed" {
					status = "failed"
				}
				entry := map[string]any{"ref": outcome.Ref, "status": status}
				if status == "found" && outcome.Item != nil {
					entry["item"] = conformanceItem(outcome.Item.Summary, outcome.Item.Body)
				}
				wire = append(wire, entry)
			}
			result = map[string]any{"context": conformanceContext(source.ID, "complete"), "outcomes": wire}
		case "recordProgress":
			ref := requestRef(request.Params)
			intent := WriteIntent{OperationID: strings.Repeat("a", 32), Source: source, Ref: ref,
				Action: ActionRecordProgress, Append: "fixture progress", Marker: "worklease-op:" + strings.Repeat("a", 32),
				Patch: map[string]string{"append": "comment"}, Precondition: "deliberately-stale-version", Principal: "tester"}
			switch built := adapter.(type) {
			case *BacklogAdapter:
				intent.Patch["append"] = "notes"
				_, err := (&BacklogWriteAdapter{BacklogAdapter: built, Me: "@tester"}).Write(context.Background(), intent)
				if err == nil {
					return
				} // A stale precondition must never dispatch a mutation.
				if diagnostic, ok := err.(BacklogDiagnostic); !ok || diagnostic.Code != "conflict" {
					return
				}
			case *GitHubAdapter:
				_, err := NewGitHubWriteAdapter(built, true).Write(context.Background(), intent)
				if err == nil {
					return
				}
				if diagnostic, ok := err.(GitHubDiagnostic); !ok || diagnostic.Code != "conflict" {
					return
				}
			default:
				return
			}
			diagnostic = "conflict"
		case "readReceipt":
			var target struct {
				Ref Ref `json:"ref"`
			}
			_ = json.Unmarshal(request.Params["target"], &target)
			// An unsuccessful stale preflight has no mutation receipt. Read the
			// authoritative provider rather than inferring absence from the RPC error.
			outcomes := adapter.ReadItems(context.Background(), source, []Ref{target.Ref}, nil, 1)
			if len(outcomes) != 1 || outcomes[0].Kind != "found" {
				diagnostic = "unknown-outcome"
				break
			}
			result = map[string]any{"context": conformanceContext(source.ID, "complete"), "verification": "conflict", "evidence": map[string]any{"item": target.Ref, "status": outcomes[0].Item.RawStatus}}
		case "readDependencies":
			ref := requestRef(request.Params)
			var cursor *string
			_ = json.Unmarshal(request.Params["cursor"], &cursor)
			page := ""
			if cursor != nil {
				page = *cursor
			}
			deps, err := adapter.ReadDependencies(context.Background(), source, ref, page, 100)
			if err != nil {
				diagnostic = "unavailable-source"
				break
			}
			wire := make([]any, 0, len(deps.Edges))
			for _, edge := range deps.Edges {
				direction := "non-blocking"
				if edge.Type == HardPrerequisite {
					direction = "prerequisite"
				}
				kind := "related"
				if edge.Type == HardPrerequisite {
					kind = "dependency"
				}
				wire = append(wire, map[string]any{"from": edge.From, "to": edge.To, "relationshipType": kind, "direction": direction, "completionCondition": edge.Condition, "rawOutcome": map[string]string{"state": edge.RawOutcome}, "interpretation": map[string]string{"result": "unknown"}})
			}
			completeness := string(deps.Completeness)
			if completeness == "" {
				completeness = "unknown"
			}
			var next any
			if deps.NextCursor != "" {
				next = deps.NextCursor
			}
			result = map[string]any{"context": conformanceContext(source.ID, completeness), "edges": wire, "nextCursor": next, "completeness": completeness}
		default:
			diagnostic = "unsupported-capability"
		}
		var response any
		if diagnostic != "" {
			response = map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": externalDiagnosticCode(diagnostic), "message": "unsupported fixture operation", "data": map[string]string{"diagnostic": diagnostic}}}
		} else {
			response = map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}
		}
		if ctx.Err() != nil {
			return
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			return
		}
		outputMu.Lock()
		_, _ = os.Stdout.Write(append(encoded, '\n'))
		outputMu.Unlock()
	}
	for reader.Scan() {
		var request wireRequest
		if json.Unmarshal(reader.Bytes(), &request) != nil {
			return
		}
		if request.Method == "$/cancelRequest" {
			var id struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(request.Params["id"], &id.ID)
			pendingMu.Lock()
			cancel := pending[id.ID]
			pendingMu.Unlock()
			if cancel != nil {
				cancel()
			}
			continue
		}
		if request.Method == "list" {
			ctx, cancel := context.WithCancel(context.Background())
			pendingMu.Lock()
			pending[request.ID] = cancel
			pendingMu.Unlock()
			go func() {
				defer cancel()
				defer func() { pendingMu.Lock(); delete(pending, request.ID); pendingMu.Unlock() }()
				respond(request, ctx)
			}()
			continue
		}
		respond(request, context.Background())
	}
}

func conformanceContext(sourceID, state string) map[string]any {
	return map[string]any{"principal": "tester", "configurationGeneration": "fixture-v1", "observedAt": time.Now().UTC().Format(time.RFC3339Nano), "providerVersion": nil, "coverage": map[string]any{"state": state, "scope": sourceID, "cursor": nil}}
}
func conformanceItem(item Summary, body string) map[string]any {
	return map[string]any{"ref": item.Ref, "title": item.Title, "rawStatus": item.RawStatus, "state": item.State, "order": item.Order, "priority": item.Priority, "canonicalId": item.CanonicalID, "body": body, "terminal": item.Terminal}
}

func TestAdapterConformance(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"backlog-md", "github", "sample"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			_, paths := testkit.Home(t)
			env := externalTestEnvironment(paths)
			fixture := map[string]any{}
			itemID := "TASK-2"
			if kind == "sample" {
				itemID = "sample-1"
			} else if kind == "backlog-md" {
				root, binary := fakeBacklog(t)
				fixture["checkout"], fixture["binary"] = root, binary
			} else {
				itemID = "1"
				built, server := fakeGitHub(t, func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						Query string `json:"query"`
					}
					_ = json.NewDecoder(r.Body).Decode(&req)
					switch {
					case strings.Contains(req.Query, "viewer"):
						_, _ = fmt.Fprint(w, `{"data":{"viewer":{"login":"tester"}}}`)
					case strings.Contains(req.Query, "nodes(ids:"):
						_, _ = fmt.Fprint(w, `{"data":{"nodes":[{"id":"issue-1","number":1,"title":"Fixture","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}]}}`)
					case strings.Contains(req.Query, "blockedBy("):
						_, _ = fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issue":{"number":1,"repository":{"nameWithOwner":"org/repo"},"blockedBy":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}},"subIssues":{"totalCount":0,"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`)
					case strings.Contains(req.Query, "issue(number:"):
						_, _ = fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issue":{"id":"issue-1","number":1,"title":"Fixture","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}}}}`)
					default:
						_, _ = fmt.Fprint(w, `{"data":{"repository":{"nameWithOwner":"org/repo","issues":{"totalCount":1,"nodes":[{"id":"issue-1","number":1,"title":"Fixture","state":"OPEN","repository":{"nameWithOwner":"org/repo"}}],"pageInfo":{"hasNextPage":false}}}}}`)
					}
				})
				fixture["binary"], fixture["apiBase"] = built.Binary, server.URL
			}
			executable := filepath.Join(t.TempDir(), "conformance-shim")
			helper := "TestAdapterConformanceProcessHelper"
			adapterID := "conformance.shim"
			if kind == "sample" {
				helper, adapterID = "TestAdapterConformanceSampleProcessHelper", "worklease.sample.static"
			}
			script := fmt.Sprintf("#!/bin/sh\nexec %s -test.run='^%s$' -- %s\n", shellQuote(os.Args[0]), helper, shellQuote(kind))
			if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			executable, err := filepath.EvalSymlinks(executable)
			if err != nil {
				t.Fatal(err)
			}
			sourceConfig := config.QueueSource{ID: "conformance", Adapter: "external", Executable: executable, ExpectedAdapterID: adapterID, ExpectedVersion: "1.0.0", Config: fixture}
			if err := config.ApproveQueueAdapter(context.Background(), env, sourceConfig); err != nil {
				t.Fatal(err)
			}
			adapter := &ExternalAdapter{source: sourceConfig, env: env}
			t.Cleanup(adapter.Close)
			source, err := adapter.Resolve(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			first, err := adapter.List(context.Background(), source, Query{Budget: 1}, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(first.Items) != 1 || first.Coverage.State == CoverageUnknown {
				t.Fatalf("first page exceeded budget or lacked coverage: %+v", first)
			}
			seen := map[string]bool{first.Items[0].Ref.Key(): true}
			cursor := first.NextCursor
			for pages := 0; cursor != "" && pages < 5; pages++ {
				page, err := adapter.List(context.Background(), source, Query{Budget: 1}, cursor)
				if err != nil || len(page.Items) != 1 || seen[page.Items[0].Ref.Key()] {
					t.Fatalf("invalid continuation: %+v, %v", page, err)
				}
				seen[page.Items[0].Ref.Key()] = true
				cursor = page.NextCursor
			}
			if cursor != "" || kind != "github" && len(seen) < 2 {
				t.Fatalf("pagination did not exhaust fixture: %+v", seen)
			}
			ref := Ref{SourceID: source.ID, ItemID: itemID}
			outcomes := adapter.ReadItems(context.Background(), source, []Ref{ref, {SourceID: source.ID, ItemID: "missing"}}, nil, 2)
			if len(outcomes) != 2 || outcomes[0].Kind != "found" || outcomes[0].Item == nil || outcomes[1].Kind == "found" {
				t.Fatalf("item outcomes: %+v", outcomes)
			}
			caps, err := adapter.Capabilities(context.Background(), source, "tester", nil)
			if err != nil || caps["native-claims"].Support == Supported {
				t.Fatalf("unsafe capabilities: %+v %v", caps, err)
			}
			if _, err := adapter.ReadDependencies(context.Background(), source, ref, "", 100); err != nil {
				t.Fatal(err)
			}
			process, err := adapter.validatedProcess(source)
			if err != nil {
				t.Fatal(err)
			}
			mutation := map[string]any{"ref": ref, "operationId": strings.Repeat("a", 32), "patch": map[string]string{"append": "fixture progress"}, "expectedVersion": "deliberately-stale-version", "authority": map[string]string{"authorizationRef": "fixture", "scope": source.ID}}
			var ignored map[string]any
			writeErr := process.Call(context.Background(), "recordProgress", mutation, &ignored)
			if writeErr == nil || !strings.Contains(writeErr.Error(), "outcome is unknown") {
				t.Fatalf("dispatched rejected mutation was not treated as uncertain: %v", writeErr)
			}
			if kind != "sample" {
				var verification map[string]any
				readback := map[string]any{"sourceId": source.ID, "operation": "recordProgress", "operationId": strings.Repeat("a", 32), "target": map[string]any{"ref": ref}, "intent": mutation, "receipt": nil}
				if err := process.Call(context.Background(), "readReceipt", readback, &verification); err != nil || verification["verification"] != "conflict" {
					t.Fatalf("stale write readback = %+v, error=%v", verification, err)
				}
			}
			policy, err := resource.Resolve(resource.Input{Provider: "generic", Source: "conformance-source", Item: itemID})
			if err != nil || policy.Resource == "" || strings.Contains(policy.Resource, executable) {
				t.Fatalf("resource derivation: %+v %v", policy, err)
			}
		})
	}
}
