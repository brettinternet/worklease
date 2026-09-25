package queue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/brettinternet/worklease/internal/testkit"
)

const linearTestOrg = "0bfebf80-70af-4eca-9e39-2029a01f5b77"
const linearTestTeam = "d19193f6-0501-485b-93af-65e829c2039d"
const linearTestViewer = "72088203-6bc7-4a63-a71b-22048b88da64"
const linearTestIssue = "075a1740-eeda-4b85-be0b-39755abf4c8c"
const linearTestBlocker = "9f3b2707-b6d8-456d-9079-32f60cd33474"

func linearFixture(t *testing.T, handler func(string, map[string]json.RawMessage) any) (*LinearAdapter, Source) {
	t.Helper()
	testkit.Home(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "fixture-token" {
			t.Errorf("wrong method or credential")
			w.WriteHeader(401)
			return
		}
		var request struct {
			Query     string
			Variables map[string]json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("request: %v", err)
			w.WriteHeader(400)
			return
		}
		data := handler(request.Query, request.Variables)
		if data == nil {
			t.Errorf("unexpected query: %s", request.Query)
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)
	adapter := NewLinearAdapter()
	adapter.APIBase = server.URL
	source, err := adapter.Resolve(context.Background(), map[string]string{"id": "linear-test", "organization": linearTestOrg, "team": linearTestTeam, "account": linearTestViewer, "credentialHelper": `["/bin/echo","fixture-token"]`})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return adapter, source
}
func linearIdentityFixture(query string) any {
	if query == linearIdentityQuery {
		return map[string]any{"viewer": map[string]any{"id": linearTestViewer, "organization": map[string]any{"id": linearTestOrg}}, "team": map[string]any{"id": linearTestTeam, "name": "TEST"}}
	}
	return nil
}
func linearIssueFixture(id, stateType string) map[string]any {
	return map[string]any{"id": id, "identifier": "TEST-1", "title": "task", "description": "body", "updatedAt": "2026-09-25T12:00:00Z", "archivedAt": nil, "trashed": false, "team": map[string]any{"id": linearTestTeam}, "project": nil, "state": map[string]any{"id": "workflow-state", "name": "Doing", "type": stateType}, "assignee": map[string]any{"id": linearTestViewer}}
}
func TestLinearReadOnlyAdapterCoverageAndIdentity(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	adapter, source := linearFixture(t, func(query string, variables map[string]json.RawMessage) any {
		if result := linearIdentityFixture(query); result != nil {
			return result
		}
		switch query {
		case linearListQuery:
			calls.Add(1)
			if string(variables["count"]) != "250" {
				t.Errorf("page not bounded: %s", variables["count"])
			}
			first := string(variables["after"]) == "null"
			nodes := []any{linearIssueFixture(linearTestIssue, "started")}
			if !first {
				nodes = []any{linearIssueFixture(linearTestBlocker, "completed")}
			}
			return map[string]any{"team": map[string]any{"id": linearTestTeam, "issues": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": first, "endCursor": "next"}}}}
		case linearDetailQuery:
			if string(variables["id"]) != `"`+linearTestIssue+`"` {
				t.Errorf("lookup uses alias: %s", variables["id"])
			}
			return map[string]any{"issue": linearIssueFixture(linearTestIssue, "started")}
		}
		return nil
	})
	caps, err := adapter.Capabilities(context.Background(), source, "", nil)
	if err != nil || caps["mutation"].Permission != Allowed || caps["progress"].Permission != Allowed || caps["assignment"].Permission != Allowed || caps["native-claims"].Support != Unsupported || caps["dependencies"].Permission != Allowed {
		t.Fatalf("unsafe capabilities: %v %v", caps, err)
	}
	first, err := adapter.List(context.Background(), source, Query{Budget: 300}, "")
	if err != nil || len(first.Items) != 1 || first.Items[0].Ref.ItemID != linearTestIssue || first.Items[0].RawStatus != "workflow-state:Doing:started" || first.NextCursor == "" || first.Coverage.State != CoveragePartial || first.Observation.Principal != linearTestViewer {
		t.Fatalf("first page: %+v %v", first, err)
	}
	last, err := adapter.List(context.Background(), source, Query{}, first.NextCursor)
	if err != nil || len(last.Items) != 1 || last.NextCursor != "" || last.Coverage.State != CoveragePartial || calls.Load() != 2 {
		t.Fatalf("unreconciled final page: %+v %v", last, err)
	}
	if _, err = adapter.List(context.Background(), source, Query{}, "bad-cursor"); err == nil {
		t.Fatal("invalid cursor accepted")
	}
	outcomes := adapter.ReadItems(context.Background(), source, []Ref{{source.ID, linearTestIssue}, {source.ID, "TEST-1"}}, nil, 2)
	if len(outcomes) != 2 || outcomes[0].Kind != "found" || outcomes[0].Item.State != StateInProgress || outcomes[0].Item.Body != "body" || outcomes[1].Kind != "failed" {
		t.Fatalf("read outcomes: %+v", outcomes)
	}
	_, err = adapter.Resolve(context.Background(), map[string]string{"id": source.ID, "organization": linearTestOrg, "team": linearTestTeam, "account": linearTestBlocker, "credentialHelper": `["/bin/echo","fixture-token"]`})
	if err == nil {
		t.Fatal("mismatched viewer accepted")
	}
	if _, err = adapter.List(context.Background(), source, Query{}, ""); err == nil || !strings.Contains(err.Error(), "invalid-source") {
		t.Fatalf("old credential remained usable: %v", err)
	}
}
func TestLinearRelationsBothDirectionsAndUnknownScope(t *testing.T) {
	t.Parallel()
	adapter, source := linearFixture(t, func(query string, variables map[string]json.RawMessage) any {
		if result := linearIdentityFixture(query); result != nil {
			return result
		}
		if query != linearRelationsQuery && query != linearInverseQuery {
			return nil
		}
		relations := map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false}}
		field := "relations"
		if query == linearInverseQuery {
			field = "inverseRelations"
			for _, kind := range []string{"blocks", "related", "duplicate", "similar"} {
				relation := map[string]any{"type": kind, "issue": map[string]any{"id": linearTestBlocker, "team": map[string]any{"id": linearTestTeam}, "state": map[string]any{"type": "completed"}}}
				relations["nodes"] = append(relations["nodes"].([]any), relation)
			}
		}
		return map[string]any{"issue": map[string]any{"id": linearTestIssue, "team": map[string]any{"id": linearTestTeam}, field: relations}}
	})
	ref := Ref{source.ID, linearTestIssue}
	first, err := adapter.ReadDependencies(context.Background(), source, ref, "", 500)
	if err != nil || first.Completeness != CoveragePartial || first.NextCursor == "" {
		t.Fatalf("outgoing: %+v %v", first, err)
	}
	second, err := adapter.ReadDependencies(context.Background(), source, ref, first.NextCursor, 50)
	if err != nil || second.NextCursor != "" || second.Completeness != CoverageComplete || len(second.Edges) != 4 || second.Edges[0].Type != HardPrerequisite || second.Edges[0].To.ItemID != linearTestBlocker || second.Edges[0].Condition != "terminal" || second.Edges[1].Type != Related || second.Edges[2].Type != Related || second.Edges[3].Type != Related {
		t.Fatalf("incoming: %+v %v", second, err)
	}
	if _, err = adapter.ReadDependencies(context.Background(), source, Ref{source.ID, linearTestBlocker}, first.NextCursor, 1); err == nil {
		t.Fatal("cross-item cursor accepted")
	}
}
func TestLinearProjectParentRemainsInformational(t *testing.T) {
	t.Parallel()
	project := linearTestBlocker
	adapter, source := linearFixture(t, func(query string, _ map[string]json.RawMessage) any {
		if result := linearIdentityFixture(query); result != nil {
			return result
		}
		if query == linearRelationsQuery {
			return map[string]any{"issue": map[string]any{"id": linearTestIssue, "team": map[string]any{"id": linearTestTeam}, "project": map[string]any{"id": project}, "parent": map[string]any{"id": linearTestBlocker, "team": map[string]any{"id": linearTestTeam}, "project": map[string]any{"id": project}}, "relations": map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false}}}}
		}
		return nil
	})
	adapter.mu.Lock()
	binding := adapter.bindings[source.ID]
	binding.project = project
	adapter.bindings[source.ID] = binding
	adapter.mu.Unlock()
	page, err := adapter.ReadDependencies(context.Background(), source, Ref{source.ID, linearTestIssue}, "", 50)
	if err != nil || len(page.Edges) != 1 || page.Edges[0].Type != ParentChild || page.Edges[0].From.ItemID != linearTestBlocker || page.Edges[0].Direction != ParentToChild {
		t.Fatalf("project parent: %+v %v", page, err)
	}
}

func TestLinearEmptyRelationsHydrateComplete(t *testing.T) {
	t.Parallel()
	adapter, source := linearFixture(t, func(query string, _ map[string]json.RawMessage) any {
		if result := linearIdentityFixture(query); result != nil {
			return result
		}
		switch query {
		case linearListQuery:
			return map[string]any{"team": map[string]any{"id": linearTestTeam, "issues": map[string]any{"nodes": []any{linearIssueFixture(linearTestIssue, "unstarted")}, "pageInfo": map[string]any{"hasNextPage": false}}}}
		case linearDetailQuery:
			return map[string]any{"issue": linearIssueFixture(linearTestIssue, "unstarted")}
		case linearRelationsQuery:
			return map[string]any{"issue": map[string]any{"id": linearTestIssue, "team": map[string]any{"id": linearTestTeam}, "relations": map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false}}}}
		case linearInverseQuery:
			return map[string]any{"issue": map[string]any{"id": linearTestIssue, "team": map[string]any{"id": linearTestTeam}, "inverseRelations": map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false}}}}
		}
		return nil
	})
	registry := NewRegistry()
	registry.adapters["linear"] = adapter
	loader := NewLoader(registry)
	for range loader.Refresh(context.Background(), []Source{source}) {
	}
	item, found := loader.Store.Item(Ref{source.ID, linearTestIssue})
	if !found || !item.DependenciesKnown || item.Closure != CoverageComplete || item.ReadOutcome != "found" {
		t.Fatalf("empty complete relation graph: %+v found=%v", item, found)
	}
	if loader.Store.Current().Sources[source.ID].State != CoveragePartial {
		t.Fatal("read stage incorrectly certified source deletion coverage")
	}
	// A row whose initial hydration was dropped must be recoverable when selected.
	item.ReadOutcome = "summary-only"
	item.DependenciesKnown = false
	item.Closure = CoverageUnknown
	loader.Store.SeedSnapshot(Snapshot{Items: map[string]Item{item.Ref.Key(): item}})
	for range loader.HydrateEdges(context.Background(), source, []Ref{item.Ref}, nil, false) {
	}
	reloaded, found := loader.Store.Item(item.Ref)
	if !found || reloaded.ReadOutcome != "found" || !reloaded.DependenciesKnown {
		t.Fatalf("selected row remained summary-only: %+v", reloaded)
	}
}

func TestLinearRejectsPrincipalMismatch(t *testing.T) {
	t.Parallel()
	testkit.Home(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"viewer": map[string]any{"id": linearTestBlocker, "organization": map[string]any{"id": linearTestOrg}}, "team": map[string]any{"id": linearTestTeam}}})
	}))
	defer server.Close()
	adapter := NewLinearAdapter()
	adapter.APIBase = server.URL
	_, err := adapter.Resolve(context.Background(), map[string]string{"id": "bad", "organization": linearTestOrg, "team": linearTestTeam, "account": linearTestViewer, "credentialHelper": `["/bin/echo","fixture-token"]`})
	if err == nil || strings.Contains(err.Error(), "fixture-token") {
		t.Fatalf("unverified principal or leaked token: %v", err)
	}
	if _, ok := adapter.bindings["bad"]; ok {
		t.Fatal("bad principal bound")
	}
}
