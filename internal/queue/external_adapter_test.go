package queue

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/testkit"
)

func TestExternalAdapterProcessHelper(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+2 >= len(os.Args) {
		return
	}
	mode := ""
	if separator+3 < len(os.Args) {
		mode = os.Args[separator+3]
	}
	runExternalAdapterHelper(os.Args[separator+1], os.Args[separator+2], mode)
}

func TestExternalAdapterRegistrationIsLazyAndReadsAreSourceScoped(t *testing.T) {
	t.Parallel()
	_, paths := testkit.Home(t)
	env := externalTestEnvironment(paths)
	registry := NewRegistry()
	configured := make([]config.QueueSource, 2)
	markers := make(map[string]string)
	for index, id := range []string{"external-one", "external-two"} {
		marker := filepath.Join(t.TempDir(), id+".starts")
		source := externalAdapterTestSource(t, env, id, marker)
		if err := config.ApproveQueueAdapter(context.Background(), env, source); err != nil {
			t.Fatalf("approve helper source %s: %v", id, err)
		}
		configured[index] = source
		markers[id] = marker
	}
	cleanup, err := RegisterExternalSources(registry, configured, env)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	for _, source := range configured {
		if _, err := os.Stat(markers[source.ID]); !os.IsNotExist(err) {
			t.Fatalf("registration started %s before Resolve: stat error %v", source.ID, err)
		}
	}

	for _, configuredSource := range configured {
		key := ExternalSourceAdapterKey(configuredSource.ID)
		registered, ok := registry.Get(key)
		if !ok {
			t.Fatalf("registry did not contain source key %q", key)
		}
		source, err := registered.Resolve(context.Background(), map[string]string{"id": configuredSource.ID})
		if err != nil {
			t.Fatalf("resolve %s: %v", configuredSource.ID, err)
		}
		if source.ID != configuredSource.ID || source.Adapter != key || source.Locator != "memory://"+configuredSource.ID {
			t.Fatalf("resolved source = %+v", source)
		}
		changed := source
		changed.Locator += "/wrong"
		if _, err := registered.List(context.Background(), changed, Query{}, ""); err == nil {
			t.Fatal("changed source identity was accepted")
		}

		first, err := registered.List(context.Background(), source, Query{Budget: 1, Fields: []string{"title"}}, "")
		if err != nil {
			t.Fatalf("first list page for %s: %v", source.ID, err)
		}
		if len(first.Items) != 1 || first.Items[0].Ref != (Ref{SourceID: source.ID, ItemID: "item-one"}) || first.NextCursor != "page-2" || first.Coverage.State != CoveragePartial || first.Coverage.Total != 2 || first.Coverage.TotalAccuracy != TotalExact {
			t.Fatalf("first page did not preserve partial coverage: %+v", first)
		}
		second, err := registered.List(context.Background(), source, Query{Budget: 1}, first.NextCursor)
		if err != nil {
			t.Fatalf("second list page for %s: %v", source.ID, err)
		}
		if len(second.Items) != 1 || second.Items[0].Ref.ItemID != "item-two" || second.NextCursor != "" || second.Coverage.State != CoverageComplete {
			t.Fatalf("second page did not preserve complete coverage: %+v", second)
		}

		ref := Ref{SourceID: source.ID, ItemID: "item-one"}
		missingRef := Ref{SourceID: source.ID, ItemID: "missing"}
		inaccessibleRef := Ref{SourceID: source.ID, ItemID: "private"}
		outcomes := registered.ReadItems(context.Background(), source, []Ref{ref, missingRef, inaccessibleRef, {SourceID: "other-source", ItemID: "foreign"}}, []string{"body"}, 2)
		if len(outcomes) != 4 || outcomes[0].Ref != ref || outcomes[0].Kind != "found" || outcomes[0].Item == nil || outcomes[0].Item.Body != "details for "+source.ID || outcomes[0].Item.Observation.ProviderVersion != "provider-v1" {
			t.Fatalf("found item mapping = %+v", outcomes)
		}
		if outcomes[1].Kind != "missing" || outcomes[1].Item != nil || outcomes[2].Kind != "inaccessible" || outcomes[2].Err != nil {
			t.Fatalf("non-found per-item outcomes = %+v", outcomes[1:3])
		}
		if outcomes[3].Kind != "failed" || outcomes[3].Err == nil {
			t.Fatalf("foreign reference did not fail closed: %+v", outcomes[3])
		}

		dependencies, err := registered.ReadDependencies(context.Background(), source, ref, "", 3)
		if err != nil {
			t.Fatalf("read dependencies for %s: %v", source.ID, err)
		}
		if dependencies.Completeness != CoveragePartial || dependencies.NextCursor != "deps-next" || len(dependencies.Edges) != 3 {
			t.Fatalf("dependency page coverage = %+v", dependencies)
		}
		if edge := dependencies.Edges[0]; edge.Type != HardPrerequisite || edge.Direction != DependentToPrerequisite || edge.From != ref || edge.To.ItemID != "prerequisite" || edge.Condition != "terminal" || edge.Support != Supported {
			t.Fatalf("dependency mapping = %+v", edge)
		}
		if edge := dependencies.Edges[1]; edge.Type != ParentChild || edge.Direction != ParentToChild {
			t.Fatalf("hierarchy was not retained as non-blocking: %+v", edge)
		}
		if edge := dependencies.Edges[2]; edge.Type != HardPrerequisite || edge.Support != SupportUnknown || edge.Direction != UnknownDirection || edge.From != ref {
			t.Fatalf("unknown dependency semantics were promoted: %+v", edge)
		}
		completeDependencies, err := registered.ReadDependencies(context.Background(), source, ref, dependencies.NextCursor, 3)
		if err != nil || completeDependencies.Completeness != CoverageComplete || completeDependencies.NextCursor != "" || len(completeDependencies.Edges) != 0 {
			t.Fatalf("dependency completion = %+v, error=%v", completeDependencies, err)
		}

		capabilities, err := registered.Capabilities(context.Background(), source, "principal", &ref)
		if err != nil || capabilities["discovery"].Support != Supported || capabilities["future-group"].Support != "" {
			t.Fatalf("capability mapping = %#v, error=%v", capabilities, err)
		}
	}
	for id, marker := range markers {
		data, err := os.ReadFile(marker)
		if err != nil {
			t.Fatalf("process start marker for %s: %v", id, err)
		}
		if starts := strings.Count(string(data), "started\n"); starts != 1 {
			t.Fatalf("%s started %d processes, marker %q", id, starts, data)
		}
	}
	cleanup()
	for _, source := range configured {
		if _, ok := registry.Get(ExternalSourceAdapterKey(source.ID)); ok {
			t.Fatalf("cleanup retained adapter registration for %s", source.ID)
		}
	}
}

func TestExternalAdapterReResolvesAfterProcessRestart(t *testing.T) {
	t.Parallel()
	_, paths := testkit.Home(t)
	env := externalTestEnvironment(paths)
	marker := filepath.Join(t.TempDir(), "restart.events")
	source := externalAdapterTestSource(t, env, "restart", marker)
	source.CredentialRef = "credential-ref-" + source.ID
	source.Executable = writeScopedExternalAdapterScriptMode(t, source.ID, marker, "crash-first-list")
	if err := config.ApproveQueueAdapter(context.Background(), env, source); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	cleanup, err := RegisterExternalSources(registry, []config.QueueSource{source}, env)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	registered, _ := registry.Get(ExternalSourceAdapterKey(source.ID))
	resolved, err := registered.Resolve(context.Background(), map[string]string{"id": source.ID})
	if err != nil {
		t.Fatalf("resolve source: %v", err)
	}
	if _, err := registered.List(context.Background(), resolved, Query{}, ""); err == nil {
		t.Fatal("first process crash unexpectedly returned a list page")
	}
	page, err := registered.List(context.Background(), resolved, Query{}, "")
	if err != nil || len(page.Items) != 1 {
		data, _ := os.ReadFile(marker)
		t.Fatalf("list after process restart = %+v, error=%v, events=%q", page, err, data)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	events := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{
		"started", "process-1 initialize", "process-1 resolve", "process-1 list",
		"started", "process-2 initialize", "process-2 resolve", "process-2 list",
	}
	if strings.Join(events, "\n") != strings.Join(want, "\n") {
		t.Fatalf("per-process protocol sequence = %q, want %q", events, want)
	}
}

func TestExternalAdapterMixedGenerationPageKeepsLoaderIncomplete(t *testing.T) {
	t.Parallel()
	_, paths := testkit.Home(t)
	env := externalTestEnvironment(paths)
	marker := filepath.Join(t.TempDir(), "mixed-generation.events")
	source := externalAdapterTestSource(t, env, "mixed-generation", marker)
	source.Executable = writeScopedExternalAdapterScriptMode(t, source.ID, marker, "mixed-generation-list")
	if err := config.ApproveQueueAdapter(context.Background(), env, source); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	cleanup, err := RegisterExternalSources(registry, []config.QueueSource{source}, env)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	registered, _ := registry.Get(ExternalSourceAdapterKey(source.ID))
	resolved, err := registered.Resolve(context.Background(), map[string]string{"id": source.ID})
	if err != nil {
		t.Fatalf("resolve source: %v", err)
	}
	cachedRef := Ref{SourceID: source.ID, ItemID: "cached-item"}
	loader := NewLoader(registry)
	loader.Store.SeedSnapshot(Snapshot{
		Items: map[string]Item{cachedRef.Key(): {Summary: Summary{Ref: cachedRef, Title: "cached", Fresh: true}, Observation: Observation{
			Principal: "adapter-user", ConfigurationGeneration: "generation-" + source.ID,
		}}},
		Sources: map[string]Coverage{source.ID: {State: CoverageComplete}},
	})
	drainRefresh(loader.Refresh(context.Background(), []Source{resolved}))
	snapshot := loader.Store.Current()
	if snapshot.Sources[source.ID].State == CoverageComplete {
		t.Fatalf("mixed-generation pagination was declared complete: %+v", snapshot.Sources[source.ID])
	}
	if _, ok := snapshot.Items[cachedRef.Key()]; !ok {
		t.Fatal("cached item was deleted after mixed-generation pagination")
	}
	if _, deleted := snapshot.Deleted[cachedRef.Key()]; deleted {
		t.Fatal("cached item was marked deleted after mixed-generation pagination")
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if calls := strings.Count(string(data), " list\n"); calls != 2 {
		t.Fatalf("loader issued %d list calls, want both pages; log=%q", calls, data)
	}
}

func TestExternalAdapterApprovalDenialIsolatesOnlyThatSource(t *testing.T) {
	t.Parallel()
	_, paths := testkit.Home(t)
	env := externalTestEnvironment(paths)
	registry := NewRegistry()
	deniedMarker := filepath.Join(t.TempDir(), "denied.starts")
	goodMarker := filepath.Join(t.TempDir(), "good.starts")
	denied := externalAdapterTestSource(t, env, "denied", deniedMarker)
	good := externalAdapterTestSource(t, env, "good", goodMarker)
	if err := config.ApproveQueueAdapter(context.Background(), env, good); err != nil {
		t.Fatal(err)
	}
	cleanup, err := RegisterExternalSources(registry, []config.QueueSource{denied, good}, env)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	deniedAdapter, _ := registry.Get(ExternalSourceAdapterKey(denied.ID))
	if _, err := deniedAdapter.Resolve(context.Background(), map[string]string{"id": denied.ID}); err == nil {
		t.Fatal("unapproved source resolved successfully")
	}
	if _, err := os.Stat(deniedMarker); !os.IsNotExist(err) {
		t.Fatalf("denied process was executed: %v", err)
	}
	goodAdapter, _ := registry.Get(ExternalSourceAdapterKey(good.ID))
	if source, err := goodAdapter.Resolve(context.Background(), map[string]string{"id": good.ID}); err != nil || source.ID != good.ID {
		t.Fatalf("approved source failed after sibling denial: source=%+v error=%v", source, err)
	}
	if data, err := os.ReadFile(goodMarker); err != nil || strings.Count(string(data), "started\n") != 1 {
		t.Fatalf("approved source process marker=%q error=%v", data, err)
	}
}

func TestExternalAdapterConfigSchemaFailsClosed(t *testing.T) {
	t.Parallel()
	_, paths := testkit.Home(t)
	env := externalTestEnvironment(paths)
	marker := filepath.Join(t.TempDir(), "schema.starts")
	source := externalAdapterTestSource(t, env, "schema", marker)
	source.Config = map[string]any{"project": "schema", "unexpected": true}
	if err := config.ApproveQueueAdapter(context.Background(), env, source); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	cleanup, err := RegisterExternalSources(registry, []config.QueueSource{source}, env)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	adapter, _ := registry.Get(ExternalSourceAdapterKey(source.ID))
	if _, err := adapter.Resolve(context.Background(), map[string]string{"id": source.ID}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("configuration outside manifest schema was accepted: %v", err)
	}
}

func externalAdapterTestSource(t *testing.T, env func(string) string, id, marker string) config.QueueSource {
	t.Helper()
	source := config.QueueSource{
		ID: id, Adapter: "external", Executable: writeScopedExternalAdapterScript(t, id, marker),
		ExpectedAdapterID: "example.adapter", ExpectedVersion: "1.2.3",
		Config: map[string]any{"project": id},
	}
	return source
}

func writeScopedExternalAdapterScript(t *testing.T, sourceID, marker string) string {
	return writeScopedExternalAdapterScriptMode(t, sourceID, marker, "")
}

func writeScopedExternalAdapterScriptMode(t *testing.T, sourceID, marker, mode string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "external-adapter-"+sourceID)
	command := fmt.Sprintf("#!/bin/sh\nexec %s -test.run='^TestExternalAdapterProcessHelper$' -- %s %s %s\n", shellQuote(os.Args[0]), shellQuote(sourceID), shellQuote(marker), shellQuote(mode))
	if err := os.WriteFile(path, []byte(command), 0o700); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func runExternalAdapterHelper(expectedSource, marker, mode string) {
	processNumber := 1
	if marker != "" {
		data, _ := os.ReadFile(marker)
		processNumber += strings.Count(string(data), "started\n")
		file, err := os.OpenFile(marker, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintln(file, "started")
			_ = file.Close()
		}
	}
	resolved := false
	listCalls := 0
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var request struct {
			ID     string                     `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &request) != nil {
			return
		}
		if mode != "" && marker != "" {
			if file, err := os.OpenFile(marker, os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
				_, _ = fmt.Fprintf(file, "process-%d %s\n", processNumber, request.Method)
				_ = file.Close()
			}
		}
		if mode != "" && request.Method != "initialize" && request.Method != "resolve" && !resolved {
			return
		}
		if mode == "crash-first-list" && processNumber == 1 && request.Method == "list" {
			os.Exit(0)
		}
		if mode == "mixed-generation-list" && request.Method == "list" {
			listCalls++
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": 1,
				"manifest": map[string]any{
					"id": "example.adapter", "version": "1.2.3",
					"protocol": map[string]int{"minMajor": 1, "maxMajor": 1},
					"configSchema": map[string]any{
						"type": "object", "properties": map[string]any{"project": map[string]any{"type": "string", "minLength": 1}},
						"required": []string{"project"}, "additionalProperties": false,
					},
					"authentication": []string{}, "resourcePolicy": "generic", "capabilities": []string{"discovery", "dependencies"}, "requiredFeatures": []string{},
				},
			}
		case "resolve":
			id := requestSourceID(request.Params)
			var configuration map[string]any
			_ = json.Unmarshal(request.Params["config"], &configuration)
			if id != expectedSource || configuration["project"] != id {
				return
			}
			if mode == "crash-first-list" {
				var credentialRef string
				if len(configuration) != 1 || json.Unmarshal(request.Params["credentialRef"], &credentialRef) != nil || credentialRef != "credential-ref-"+id {
					return
				}
			}
			resolved = true
			result = map[string]any{"context": wireContext(id, "complete", nil), "source": map[string]string{"id": id, "name": id, "locator": "memory://" + id}}
		case "capabilities":
			id := requestSourceID(request.Params)
			result = map[string]any{
				"context": wireContext(id, "complete", nil),
				"capabilities": map[string]any{
					"discovery":    map[string]any{"support": "supported", "permission": "allowed", "availability": "available", "semantics": map[string]string{"pagination": "cursor"}, "limits": map[string]int{"pageSize": 100}},
					"future-group": map[string]any{"future-field": []any{true, false}},
				},
			}
		case "list":
			id := requestSourceID(request.Params)
			var cursor *string
			_ = json.Unmarshal(request.Params["cursor"], &cursor)
			if cursor == nil {
				result = map[string]any{
					"context": wireContext(id, "partial", stringPointer("page-2")),
					"items":   []any{wireItem(id, "item-one", "open")}, "nextCursor": "page-2",
					"total": map[string]any{"value": 2, "accuracy": "exact"},
				}
			} else if *cursor == "page-2" {
				item := wireItem(id, "item-two", "complete")
				item["terminal"] = true
				context := wireContext(id, "complete", nil)
				if mode == "mixed-generation-list" && listCalls >= 2 {
					context["configurationGeneration"] = "changed-generation-" + id
				}
				result = map[string]any{
					"context": context, "items": []any{item}, "nextCursor": nil,
					"total": map[string]any{"value": 2, "accuracy": "exact"},
				}
			} else {
				return
			}
		case "readItems":
			id := requestSourceID(request.Params)
			var refs []Ref
			_ = json.Unmarshal(request.Params["refs"], &refs)
			outcomes := make([]any, 0, len(refs))
			for _, ref := range refs {
				if ref.SourceID != id {
					return
				}
				if ref.ItemID == "item-one" {
					item := wireItem(id, ref.ItemID, "open")
					item["body"] = "details for " + id
					outcomes = append(outcomes, map[string]any{"ref": ref, "status": "found", "item": item})
				} else if ref.ItemID == "private" {
					outcomes = append(outcomes, map[string]any{"ref": ref, "status": "inaccessible"})
				} else {
					outcomes = append(outcomes, map[string]any{"ref": ref, "status": "missing"})
				}
			}
			result = map[string]any{"context": wireContext(id, "complete", nil), "outcomes": outcomes}
		case "readDependencies":
			ref := requestRef(request.Params)
			var cursor *string
			_ = json.Unmarshal(request.Params["cursor"], &cursor)
			if cursor == nil {
				result = map[string]any{
					"context": wireContext(ref.SourceID, "partial", stringPointer("deps-next")),
					"edges": []any{
						wireEdge(ref, Ref{SourceID: ref.SourceID, ItemID: "prerequisite"}, "dependency", "prerequisite"),
						wireEdge(ref, Ref{SourceID: ref.SourceID, ItemID: "child"}, "parent-child", "dependent"),
						wireEdge(ref, Ref{SourceID: ref.SourceID, ItemID: "opaque"}, "future-relation", "unknown"),
					},
					"nextCursor": "deps-next", "completeness": "partial",
				}
			} else if *cursor == "deps-next" {
				result = map[string]any{"context": wireContext(ref.SourceID, "complete", nil), "edges": []any{}, "nextCursor": nil, "completeness": "complete"}
			} else {
				return
			}
		default:
			return
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return
		}
		response, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": json.RawMessage(encoded)})
		if err != nil {
			return
		}
		_, _ = os.Stdout.Write(append(response, '\n'))
	}
}

func requestSourceID(params map[string]json.RawMessage) string {
	var sourceID string
	_ = json.Unmarshal(params["sourceId"], &sourceID)
	if sourceID == "" {
		ref := requestRef(params)
		return ref.SourceID
	}
	return sourceID
}

func requestRef(params map[string]json.RawMessage) Ref {
	var ref Ref
	_ = json.Unmarshal(params["ref"], &ref)
	return ref
}

func wireContext(sourceID, state string, cursor *string) map[string]any {
	var coverageCursor any
	if cursor != nil {
		coverageCursor = *cursor
	}
	return map[string]any{
		"principal": "adapter-user", "configurationGeneration": "generation-" + sourceID,
		"observedAt": time.Now().UTC().Format(time.RFC3339Nano), "providerVersion": "provider-v1",
		"coverage": map[string]any{"state": state, "scope": sourceID, "cursor": coverageCursor},
	}
}

func wireItem(sourceID, itemID, state string) map[string]any {
	return map[string]any{
		"ref": Ref{SourceID: sourceID, ItemID: itemID}, "title": "Title " + itemID, "rawStatus": state,
		"state": state, "order": itemID, "priority": 2, "canonicalId": sourceID + ":" + itemID,
		"providerReady": true, "assignedTo": []string{"alice"}, "updatedAt": "2025-01-02T03:04:05Z",
	}
}

func wireEdge(from, to Ref, relationshipType, direction string) map[string]any {
	return map[string]any{
		"from": from, "to": to, "relationshipType": relationshipType, "direction": direction,
		"completionCondition": "terminal", "rawOutcome": map[string]string{"state": "open"},
		"interpretation": map[string]string{"result": "unknown"},
	}
}

func stringPointer(value string) *string { return &value }
