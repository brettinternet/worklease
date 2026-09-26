package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type projectAPIFixture struct {
	id        string
	number    int
	owner     string
	fieldID   string
	fieldName string
	options   map[string]string
	items     []projectAPIItem
}

type projectAPIItem struct {
	id        string
	issueID   string
	number    int
	repo      string
	kind      string
	optionID  string
	name      string
	updatedAt time.Time
}

type projectAPIState struct {
	mu                    sync.Mutex
	projects              map[string]*projectAPIFixture
	issueState            string
	denyProjects          bool
	readOnlyScope         bool
	interruptItemsPageTwo bool
	loseMutationResponse  bool
	mutationCalls         int
}

func projectFixture() *projectAPIState {
	options := map[string]string{"option-open": "Open", "option-progress": "In Progress", "option-blocked": "Blocked", "option-review": "In Review", "option-done": "Done"}
	return &projectAPIState{
		issueState: "OPEN",
		projects: map[string]*projectAPIFixture{
			"PVT_project5": {id: "PVT_project5", number: 5, owner: "acme", fieldID: "PVTSSF_status", fieldName: "Workflow", options: options, items: []projectAPIItem{
				{id: "PVTI_project5_issue6", issueID: "ISSUE_6", number: 6, repo: "org/repo", kind: "Issue", optionID: "option-review", name: "In Review", updatedAt: time.Date(2026, 9, 25, 17, 30, 13, 0, time.UTC)},
				{id: "PVTI_draft", number: 0, kind: "DraftIssue"},
				{id: "PVTI_pr", number: 2, repo: "org/repo", kind: "PullRequest"},
				{id: "PVTI_foreign", issueID: "OTHER_1", number: 1, repo: "other/repo", kind: "Issue"},
				{id: "PVTI_project5_issue7", issueID: "ISSUE_7", number: 7, repo: "org/repo", kind: "Issue", optionID: "option-open", name: "Open", updatedAt: time.Date(2026, 9, 25, 17, 30, 13, 0, time.UTC)},
			}},
			"PVT_project6": {id: "PVT_project6", number: 6, owner: "acme", fieldID: "PVTSSF_status", fieldName: "Workflow", options: options, items: []projectAPIItem{
				{id: "PVTI_project6_issue6", issueID: "ISSUE_6", number: 6, repo: "org/repo", kind: "Issue", optionID: "option-blocked", name: "Blocked", updatedAt: time.Date(2026, 9, 25, 17, 30, 13, 0, time.UTC)},
			}},
		},
	}
}

func projectOptions(project *projectAPIFixture) []map[string]string {
	options := make([]map[string]string, 0, len(project.options))
	for id, name := range project.options {
		options = append(options, map[string]string{"id": id, "name": name})
	}
	return options
}

func projectItemJSON(project *projectAPIFixture, item projectAPIItem) map[string]any {
	var content any
	if item.kind == "Issue" || item.kind == "PullRequest" {
		content = map[string]any{"__typename": item.kind, "id": item.issueID, "number": item.number, "repository": map[string]any{"nameWithOwner": item.repo}}
	}
	var fieldValue any
	if item.optionID != "" {
		fieldValue = map[string]any{"name": item.name, "optionId": item.optionID, "field": map[string]any{"id": project.fieldID}}
	}
	return map[string]any{"id": item.id, "content": content, "fieldValueByName": fieldValue}
}

func projectAPIHandler(t *testing.T, state *projectAPIState) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		query, variables := githubRequest(t, r)
		state.mu.Lock()
		defer state.mu.Unlock()
		if state.readOnlyScope {
			w.Header().Set("X-OAuth-Scopes", "repo, read:project")
		} else {
			w.Header().Set("X-OAuth-Scopes", "repo, project")
		}
		projectID := ""
		_ = json.Unmarshal(variables["projectID"], &projectID)
		project := state.projects[projectID]
		switch {
		case strings.Contains(query, "viewer {"):
			writeGitHubFakeJSON(w, map[string]any{"data": map[string]any{"viewer": map[string]any{"login": "tester"}}})
		case strings.Contains(query, "fields(first:$count"):
			if state.denyProjects {
				writeGitHubFakeJSON(w, map[string]any{"errors": []any{map[string]any{"type": "FORBIDDEN", "message": "private project denied"}}})
				return
			}
			if project == nil {
				writeGitHubFakeJSON(w, map[string]any{"data": map[string]any{"node": nil}})
				return
			}
			field := map[string]any{"__typename": "ProjectV2SingleSelectField", "id": project.fieldID, "name": project.fieldName, "options": projectOptions(project)}
			writeGitHubFakeJSON(w, map[string]any{"data": map[string]any{"node": map[string]any{"id": project.id, "number": project.number, "owner": map[string]any{"login": project.owner}, "fields": map[string]any{"nodes": []any{field}, "pageInfo": map[string]any{"hasNextPage": false}}}}})
		case strings.Contains(query, "items(first:$count"):
			if state.denyProjects {
				writeGitHubFakeJSON(w, map[string]any{"errors": []any{map[string]any{"type": "FORBIDDEN", "message": "private project denied"}}})
				return
			}
			if project == nil {
				writeGitHubFakeJSON(w, map[string]any{"data": map[string]any{"node": nil}})
				return
			}
			var after string
			_ = json.Unmarshal(variables["after"], &after)
			if after == "project-items-2" && state.interruptItemsPageTwo {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			start, end, next := 0, len(project.items), ""
			if len(project.items) > 2 && after == "" {
				end, next = 2, "project-items-2"
			} else if after == "project-items-2" {
				start = 2
			}
			nodes := make([]any, 0, end-start)
			for _, item := range project.items[start:end] {
				nodes = append(nodes, projectItemJSON(project, item))
			}
			writeGitHubFakeJSON(w, map[string]any{"data": map[string]any{"node": map[string]any{"id": project.id, "number": project.number, "owner": map[string]any{"login": project.owner}, "items": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": next != "", "endCursor": next}}}}})
		case strings.Contains(query, "node(id:$itemID)"):
			itemID := ""
			_ = json.Unmarshal(variables["itemID"], &itemID)
			var item *projectAPIItem
			project = nil
			for _, candidateProject := range state.projects {
				for _, candidate := range candidateProject.items {
					if candidate.id == itemID {
						copy := candidate
						item = &copy
						project = candidateProject
						break
					}
				}
				if item != nil {
					break
				}
			}
			if project == nil || item == nil {
				writeGitHubFakeJSON(w, map[string]any{"data": map[string]any{"node": nil}})
				return
			}
			node := projectItemJSON(project, *item)
			node["updatedAt"] = item.updatedAt
			node["project"] = map[string]any{"id": project.id, "number": project.number, "owner": map[string]any{"login": project.owner}}
			writeGitHubFakeJSON(w, map[string]any{"data": map[string]any{"node": node}})
		case strings.Contains(query, "updateProjectV2ItemFieldValue"):
			projectIDValue := ""
			itemID, optionID := "", ""
			_ = json.Unmarshal(variables["projectID"], &projectIDValue)
			_ = json.Unmarshal(variables["itemID"], &itemID)
			_ = json.Unmarshal(variables["optionID"], &optionID)
			project := state.projects[projectIDValue]
			if project == nil {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			state.mutationCalls++
			for i := range project.items {
				if project.items[i].id == itemID {
					project.items[i].optionID = optionID
					project.items[i].name = project.options[optionID]
					project.items[i].updatedAt = project.items[i].updatedAt.Add(time.Second)
				}
			}
			if state.loseMutationResponse {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Errorf("hijack lost project response: %v", err)
					return
				}
				_ = connection.Close()
				return
			}
			writeGitHubFakeJSON(w, map[string]any{"data": map[string]any{"updateProjectV2ItemFieldValue": map[string]any{"projectV2Item": map[string]any{"id": itemID}}}})
		case strings.Contains(query, "issue(number:"):
			var number int
			_ = json.Unmarshal(variables["number"], &number)
			issueID := fmt.Sprintf("ISSUE_%d", number)
			issue := map[string]any{"id": issueID, "number": number, "title": fmt.Sprintf("Issue %d", number), "body": "", "state": state.issueState, "stateReason": "", "updatedAt": time.Date(2026, 9, 25, 17, 29, 33, 0, time.UTC), "repository": map[string]any{"nameWithOwner": "org/repo"}, "assignees": map[string]any{"nodes": []any{}}}
			writeGitHubFakeJSON(w, map[string]any{"data": map[string]any{"repository": map[string]any{"nameWithOwner": "org/repo", "issue": issue}}})
		case strings.Contains(query, "issues(first:$count"):
			var after string
			_ = json.Unmarshal(variables["after"], &after)
			items := []any{map[string]any{"id": "ISSUE_6", "number": 6, "title": "Issue 6", "state": state.issueState, "stateReason": "", "updatedAt": "2026-09-25T17:29:33Z", "repository": map[string]any{"nameWithOwner": "org/repo"}, "assignees": map[string]any{"nodes": []any{}}}}
			hasNext, endCursor := true, "issue-list-2"
			if after == "issue-list-2" {
				items = []any{map[string]any{"id": "ISSUE_7", "number": 7, "title": "Issue 7", "state": state.issueState, "stateReason": "", "updatedAt": "2026-09-25T17:29:33Z", "repository": map[string]any{"nameWithOwner": "org/repo"}, "assignees": map[string]any{"nodes": []any{}}}}
				hasNext, endCursor = false, ""
			}
			writeGitHubFakeJSON(w, map[string]any{"data": map[string]any{"repository": map[string]any{"nameWithOwner": "org/repo", "issues": map[string]any{"totalCount": 2, "nodes": items, "pageInfo": map[string]any{"hasNextPage": hasNext, "endCursor": endCursor}}}}})
		default:
			t.Errorf("unexpected Projects v2 fake query: %s", query)
			w.WriteHeader(http.StatusBadRequest)
		}
	}
}

func mustProjectJSON(t *testing.T, options map[string]string) string {
	t.Helper()
	data, err := json.Marshal(githubProjectBinding{Owner: "acme", Number: 5, ID: "PVT_project5", FieldID: "PVTSSF_status", Options: options, AllowWrites: true})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func resolveProjectFixture(t *testing.T, adapter *GitHubAdapter, projectID string, options map[string]string) Source {
	return resolveProjectFixtureWithWrite(t, adapter, projectID, options, true)
}

func resolveProjectFixtureWithWrite(t *testing.T, adapter *GitHubAdapter, projectID string, options map[string]string, allowWrites bool) Source {
	t.Helper()
	projectNumber := 5
	if projectID == "PVT_project6" {
		projectNumber = 6
	}
	project := githubProjectBinding{Owner: "acme", Number: projectNumber, ID: projectID, FieldID: "PVTSSF_status", Options: options, AllowWrites: allowWrites}
	data, err := json.Marshal(project)
	if err != nil {
		t.Fatal(err)
	}
	source, err := adapter.Resolve(context.Background(), map[string]string{"id": "issues-" + strings.TrimPrefix(projectID, "PVT_"), "host": "github.com", "repository": "org/repo", "account": "tester", "project": string(data)})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestGitHubProjectPaginationMappingAndIssueConflict(t *testing.T) {
	t.Parallel()
	state := projectFixture()
	adapter, _ := fakeGitHub(t, projectAPIHandler(t, state))
	options := map[string]string{"option-open": "open", "option-progress": "in-progress", "option-blocked": "blocked", "option-review": "review", "option-done": "complete"}
	source := resolveProjectFixture(t, adapter, "PVT_project5", options)
	first, err := adapter.List(context.Background(), source, Query{Budget: 100}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first issue page: %+v", first)
	}
	item := first.Items[0]
	if item.ProjectItemID != "PVTI_project5_issue6" || item.ProjectOptionID != "option-review" || item.ProjectStatusRaw != "In Review" || item.ProjectStatusState != "review" || item.ProjectStatusConflict {
		t.Fatalf("project state not mapped from bound item: %+v", item)
	}
	second, err := adapter.List(context.Background(), source, Query{Budget: 100}, first.NextCursor)
	if err != nil || len(second.Items) != 1 || second.Items[0].ProjectItemID != "PVTI_project5_issue7" || second.Items[0].ProjectStatusState != "open" {
		t.Fatalf("second issue page: %+v err=%v", second, err)
	}

	state.mu.Lock()
	state.projects["PVT_project5"].items[0].optionID = "option-done"
	state.projects["PVT_project5"].items[0].name = "Done"
	state.mu.Unlock()
	if err := adapter.RefreshProjectStatus(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	updated := adapter.summary(source, githubIssue{ID: "ISSUE_6", Number: 6, State: "OPEN"})
	if updated.State != StateOpen || updated.Terminal || updated.ProjectStatusState != "complete" || !updated.ProjectStatusConflict {
		t.Fatalf("project Done overrode issue state or hid conflict: %+v", updated)
	}
	unknown := adapter.summary(source, githubIssue{ID: "ISSUE_99", Number: 99, State: "OPEN"})
	readiness := Recompute(map[string]Item{unknown.Ref.Key(): {Summary: unknown}}, CoverageComplete)[unknown.Ref.Key()].Readiness
	if unknown.ProjectStatusState != "unknown" || unknown.ProjectStatusReason != "project-item-missing" || readiness.Status != ReadinessUnknown || !containsString(readiness.Reasons, "project-status-unmapped") {
		t.Fatalf("issue without project item became ready: %+v readiness=%+v", unknown, readiness)
	}
}

func TestGitHubProjectReprojectionClearsStaleState(t *testing.T) {
	t.Parallel()
	status := Summary{ProjectStatusState: "blocked", ProjectStatusReason: "project-option-unmapped"}
	applyGitHubProjectStatus(&status, githubProjectStatus{states: map[string]string{}}, false)
	if status.ProjectStatusState != "unknown" || status.ProjectStatusReason != "project-item-missing" {
		t.Fatalf("old mapping survived item removal: %+v", status)
	}
	applyGitHubProjectStatus(&status, githubProjectStatus{itemID: "item", optionID: "open", states: map[string]string{"open": "open"}}, false)
	if status.ProjectStatusState != "open" || status.ProjectStatusReason != "" {
		t.Fatalf("missing-item reason survived remapping: %+v", status)
	}
}

func TestGitHubProjectRateDiagnosticPreservesRetry(t *testing.T) {
	t.Parallel()
	deadline := time.Now().Add(time.Minute)
	got := githubProjectDiagnostic(GitHubRateDiagnostic{GitHubDiagnostic: GitHubDiagnostic{Code: "rate-limited", Detail: "retry later"}, RetryAt: deadline})
	rate, ok := got.(GitHubRateDiagnostic)
	if !ok || rate.Code != "rate-limited" || !rate.RetryAt.Equal(deadline) {
		t.Fatalf("project read erased retry deadline: %#v", got)
	}
}

func TestGitHubProjectTerminalDependencyIgnoresAdvisoryConflict(t *testing.T) {
	t.Parallel()
	prerequisite := Ref{SourceID: "github", ItemID: "6"}
	dependent := Ref{SourceID: "github", ItemID: "7"}
	items := map[string]Item{
		prerequisite.Key(): {Summary: Summary{Ref: prerequisite, Fresh: true, Terminal: true, State: StateComplete, ProjectStatusBound: true, ProjectStatusKnown: true, ProjectStatusState: "blocked", ProjectStatusConflict: true}, TerminalKnown: true, DependenciesKnown: true, Closure: CoverageComplete},
		dependent.Key():    {Summary: Summary{Ref: dependent, Fresh: true, State: StateOpen}, TerminalKnown: true, DependenciesKnown: true, Closure: CoverageComplete, Dependencies: []Ref{prerequisite}},
	}
	resolved := Recompute(items, CoverageComplete)
	if resolved[dependent.Key()].Readiness.Status != Ready || resolved[prerequisite.Key()].Readiness.Status != Blocked {
		t.Fatalf("advisory project conflict changed terminal dependency evidence: dependent=%+v prerequisite=%+v", resolved[dependent.Key()].Readiness, resolved[prerequisite.Key()].Readiness)
	}
}

func TestGitHubProjectItemRefreshIsIndependentOfIssueUpdatedAt(t *testing.T) {
	t.Parallel()
	state := projectFixture()
	adapter, _ := fakeGitHub(t, projectAPIHandler(t, state))
	options := map[string]string{"option-open": "open", "option-progress": "in-progress", "option-blocked": "blocked", "option-review": "review", "option-done": "complete"}
	source := resolveProjectFixture(t, adapter, "PVT_project5", options)
	registry := &Registry{adapters: map[string]Adapter{"github": adapter}}
	loader := NewLoader(registry)
	loader.DeferDetails = true
	refresh := func() {
		t.Helper()
		for range loader.Refresh(context.Background(), []Source{source}) {
		}
	}
	refresh()
	ref := Ref{SourceID: source.ID, ItemID: "6"}
	before, ok := loader.Store.Item(ref)
	if !ok || before.ProjectStatusState != "review" {
		t.Fatalf("initial project projection missing: %+v", before)
	}
	state.mu.Lock()
	state.projects["PVT_project5"].items[0].optionID = "option-done"
	state.projects["PVT_project5"].items[0].name = "Done"
	state.projects["PVT_project5"].items[0].updatedAt = state.projects["PVT_project5"].items[0].updatedAt.Add(time.Second)
	state.mu.Unlock()
	refresh()
	after, ok := loader.Store.Item(ref)
	if !ok || after.ProjectStatusState != "complete" || !after.ProjectStatusConflict || !after.UpdatedAt.Equal(before.UpdatedAt) || after.State != StateOpen {
		t.Fatalf("project-only update was missed or treated as issue completion: before=%+v after=%+v", before, after)
	}
}

func TestGitHubProjectOptionRenamePermissionLossAndInterruptedPagination(t *testing.T) {
	t.Parallel()
	state := projectFixture()
	adapter, _ := fakeGitHub(t, projectAPIHandler(t, state))
	options := map[string]string{"option-open": "open", "option-progress": "in-progress", "option-blocked": "blocked", "option-review": "review", "option-done": "complete"}
	source := resolveProjectFixture(t, adapter, "PVT_project5", options)
	state.mu.Lock()
	state.projects["PVT_project5"].options["option-review"] = "Recently renamed"
	state.projects["PVT_project5"].items[0].name = "Recently renamed"
	state.mu.Unlock()
	if err := adapter.RefreshProjectStatus(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	renamed := adapter.summary(source, githubIssue{ID: "ISSUE_6", Number: 6, State: "OPEN"})
	if renamed.ProjectStatusRaw != "Recently renamed" || renamed.ProjectStatusState != "review" {
		t.Fatalf("option rename lost stable-ID mapping: %+v", renamed)
	}
	state.mu.Lock()
	delete(state.projects["PVT_project5"].options, "option-review")
	state.mu.Unlock()
	if _, err := adapter.Resolve(context.Background(), map[string]string{"id": "issues", "host": "github.com", "repository": "org/repo", "account": "tester", "project": mustProjectJSON(t, options)}); !githubProjectDiag(err, "project-option-missing") {
		t.Fatalf("deleted configured option did not disable mapping: %v", err)
	}
	state.mu.Lock()
	state.projects["PVT_project5"].options["option-review"] = "Recently renamed"
	state.denyProjects = true
	state.mu.Unlock()
	if err := adapter.RefreshProjectStatus(context.Background(), source); !githubProjectDiag(err, "project-permission-denied") {
		t.Fatalf("project permission loss was not diagnosed safely: %v", err)
	}
	state.mu.Lock()
	state.denyProjects = false
	state.interruptItemsPageTwo = true
	state.mu.Unlock()
	if _, err := adapter.List(context.Background(), source, Query{Budget: 100}, ""); !githubProjectDiag(err, "project-read-failed") {
		t.Fatalf("interrupted project pagination was accepted: %v", err)
	}
	binding, err := adapter.binding(source)
	if err != nil || len(binding.projectItems) != 2 {
		t.Fatalf("interrupted scan replaced complete status snapshot: items=%d err=%v", len(binding.projectItems), err)
	}
}

func TestGitHubProjectBindingsDoNotCrossProjectsAndExcludeDraftsAndPullRequests(t *testing.T) {
	t.Parallel()
	state := projectFixture()
	adapter, _ := fakeGitHub(t, projectAPIHandler(t, state))
	options := map[string]string{"option-open": "open", "option-progress": "in-progress", "option-blocked": "blocked", "option-review": "review", "option-done": "complete"}
	project5 := resolveProjectFixture(t, adapter, "PVT_project5", options)
	project6 := resolveProjectFixture(t, adapter, "PVT_project6", options)
	page5, err := adapter.List(context.Background(), project5, Query{Budget: 100}, "")
	if err != nil {
		t.Fatal(err)
	}
	page6, err := adapter.List(context.Background(), project6, Query{Budget: 100}, "")
	if err != nil {
		t.Fatal(err)
	}
	if page5.Items[0].ProjectItemID != "PVTI_project5_issue6" || page6.Items[0].ProjectItemID != "PVTI_project6_issue6" || page5.Items[0].ProjectStatusState != "review" || page6.Items[0].ProjectStatusState != "blocked" {
		t.Fatalf("project bindings crossed: project5=%+v project6=%+v", page5.Items[0], page6.Items[0])
	}
	if len(page5.Items) != 1 {
		t.Fatalf("draft, pull request or foreign-repository item became an issue: %+v", page5.Items)
	}
}

type projectResumeStore struct {
	syncTestStore
	projection []Item
}

func (s *projectResumeStore) ReadGitHubSyncProjection(context.Context, Source) ([]Item, error) {
	return s.projection, nil
}

func TestGitHubProjectResumedReconciliationReprojectsOlderIssues(t *testing.T) {
	t.Parallel()
	state := projectFixture()
	options := map[string]string{"option-open": "open", "option-progress": "in-progress", "option-blocked": "blocked", "option-review": "review", "option-done": "complete"}
	firstAdapter, _ := fakeGitHub(t, projectAPIHandler(t, state))
	firstSource := resolveProjectFixture(t, firstAdapter, "PVT_project5", options)
	first, err := firstAdapter.List(context.Background(), firstSource, Query{Budget: 100}, "")
	if err != nil || first.NextCursor == "" {
		t.Fatalf("initial reconciliation page: %+v %v", first, err)
	}
	old := Item{Summary: first.Items[0], ReadOutcome: "found", ReadPermission: Allowed, TerminalKnown: true, DependenciesKnown: true, Closure: CoverageComplete}
	state.mu.Lock()
	state.projects["PVT_project5"].items[0].optionID = "option-done"
	state.projects["PVT_project5"].items[0].name = "Done"
	state.mu.Unlock()
	adapter, _ := fakeGitHub(t, projectAPIHandler(t, state))
	source := resolveProjectFixture(t, adapter, "PVT_project5", options)
	store := &projectResumeStore{projection: []Item{old}}
	store.state = SyncCheckpoint{ReconciliationCursor: first.NextCursor, ReconciliationStarted: time.Now().UTC(), ReconciliationGeneration: 1}
	loader := NewLoader(&Registry{adapters: map[string]Adapter{"github": adapter}})
	loader.DeferDetails = true
	loader.GitHubSync = store
	for range loader.Refresh(context.Background(), []Source{source}) {
	}
	item, ok := loader.Store.Item(old.Ref)
	if !ok || item.ProjectStatusState != "complete" || !item.ProjectStatusConflict || item.Fresh {
		t.Fatalf("resumed issue retained previous project status or freshness: %+v found=%t", item, ok)
	}
}

func TestGitHubProjectIncompleteScanEvictsItemSnapshots(t *testing.T) {
	t.Parallel()
	state := projectFixture()
	adapter, _ := fakeGitHub(t, projectAPIHandler(t, state))
	source := resolveProjectFixture(t, adapter, "PVT_project5", map[string]string{"option-open": "open", "option-progress": "in-progress", "option-blocked": "blocked", "option-review": "review", "option-done": "complete"})
	for range 101 {
		page, err := adapter.List(context.Background(), source, Query{Budget: 100}, "")
		if err != nil || page.NextCursor == "" {
			t.Fatalf("incomplete issue scan: %+v %v", page, err)
		}
	}
	binding, err := adapter.binding(source)
	if err != nil || len(binding.projectScans) > 100 || len(binding.projectScans) != len(binding.scans) {
		t.Fatalf("project snapshots leaked after scan eviction: scans=%d projects=%d err=%v", len(binding.scans), len(binding.projectScans), err)
	}
}

func TestGitHubProjectStatusWritesUseRecoveryAndNeverRedispatchLostResponse(t *testing.T) {
	for _, lostResponse := range []bool{false, true} {
		lostResponse := lostResponse
		t.Run(fmt.Sprintf("lost-response-%t", lostResponse), func(t *testing.T) {
			t.Parallel()
			state := projectFixture()
			state.loseMutationResponse = lostResponse
			adapter, _ := fakeGitHub(t, projectAPIHandler(t, state))
			options := map[string]string{"option-open": "open", "option-progress": "in-progress", "option-blocked": "blocked", "option-review": "review", "option-done": "complete"}
			source := resolveProjectFixture(t, adapter, "PVT_project5", options)
			writer := NewGitHubWriteAdapter(adapter, true)
			writer.AllowProjectWrites = true
			intent := WriteIntent{
				OperationID: strings.Repeat("a", 32), Source: source, Ref: Ref{SourceID: source.ID, ItemID: "6"}, Principal: "tester",
				AuthorityID: "authority", ClaimID: "claim", ClaimRevision: 1, Resources: []string{"resource:github:6"}, OperationRef: strings.Repeat("b", 32),
				CheckpointTTL: time.Minute, CheckpointNotAfter: time.Now().Add(time.Hour), Action: ActionStart, Transition: "option-progress", Patch: map[string]string{"projectOptionID": "option-progress"},
			}
			intent, preview, err := writer.Prepare(context.Background(), intent)
			if err != nil || !preview.Unconditional || !strings.Contains(preview.Operation, "In Progress") || intent.Patch["projectItemID"] != "PVTI_project5_issue6" {
				t.Fatalf("project status preview: intent=%+v preview=%+v err=%v", intent, preview, err)
			}
			dir := t.TempDir()
			journal, err := NewWriteJournal(dir+"/state", dir+"/cache")
			if err != nil {
				t.Fatal(err)
			}
			claim := &projectWriteClaim{}
			pipeline := WritePipeline{Adapter: writer, Claim: claim, Journal: journal, Workflow: map[string]string{"start": "option-progress"}}
			result, err := pipeline.Start(context.Background(), intent)
			if lostResponse {
				if err == nil || result.Outcome != WriteUnknown {
					t.Fatalf("lost write response outcome=%+v err=%v", result, err)
				}
				writer.AllowProjectWrites = false // Revoking write opt-in must not block read-only recovery.
				binding, bindErr := adapter.binding(source)
				if bindErr != nil {
					t.Fatal(bindErr)
				}
				binding.project.AllowWrites = false
				recovered, recoverErr := pipeline.Recover(context.Background(), intent.OperationID)
				if recoverErr != nil || recovered.Outcome != WriteUnknown {
					t.Fatalf("lost response recovery outcome=%+v err=%v", recovered, recoverErr)
				}
			} else if err != nil || result.Outcome != WriteVerified {
				t.Fatalf("status write outcome=%+v err=%v", result, err)
			}
			state.mu.Lock()
			calls, currentOption := state.mutationCalls, state.projects["PVT_project5"].items[0].optionID
			state.mu.Unlock()
			if calls != 1 || currentOption != "option-progress" {
				t.Fatalf("write dispatched more than once or wrong option: calls=%d option=%q", calls, currentOption)
			}
		})
	}
}

func TestGitHubProjectWritesRequireBothConfigurationGates(t *testing.T) {
	for _, tc := range []struct {
		name             string
		configuredWrites bool
		adapterWrites    bool
		readOnlyScope    bool
	}{
		{name: "project binding disabled", configuredWrites: false, adapterWrites: true},
		{name: "write controller disabled", configuredWrites: true, adapterWrites: false},
		{name: "read-only token", configuredWrites: true, adapterWrites: true, readOnlyScope: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := projectFixture()
			state.readOnlyScope = tc.readOnlyScope
			adapter, _ := fakeGitHub(t, projectAPIHandler(t, state))
			options := map[string]string{"option-open": "open", "option-progress": "in-progress", "option-blocked": "blocked", "option-review": "review", "option-done": "complete"}
			source := resolveProjectFixtureWithWrite(t, adapter, "PVT_project5", options, tc.configuredWrites)
			writer := NewGitHubWriteAdapter(adapter, true)
			writer.AllowProjectWrites = tc.adapterWrites
			_, _, err := writer.Prepare(context.Background(), WriteIntent{OperationID: strings.Repeat("a", 32), Source: source, Ref: Ref{SourceID: source.ID, ItemID: "6"}, Principal: "tester", Action: ActionStart, Transition: "option-progress"})
			if !githubProjectDiag(err, "project-write-disabled") {
				t.Fatalf("project write gate was bypassed: %v", err)
			}
			state.mu.Lock()
			calls := state.mutationCalls
			state.mu.Unlock()
			if calls != 0 {
				t.Fatalf("disabled project write dispatched %d mutations", calls)
			}
		})
	}
}

func TestGitHubProjectWriteScopeIsRediscovered(t *testing.T) {
	t.Parallel()
	state := projectFixture()
	state.readOnlyScope = true
	adapter, _ := fakeGitHub(t, projectAPIHandler(t, state))
	source := resolveProjectFixture(t, adapter, "PVT_project5", map[string]string{"option-open": "open", "option-progress": "in-progress", "option-blocked": "blocked", "option-review": "review", "option-done": "complete"})
	writer := NewGitHubWriteAdapter(adapter, true)
	writer.AllowProjectWrites = true
	capabilities, err := adapter.Capabilities(context.Background(), source, "", nil)
	if err != nil || capabilities["project-status-write"].Permission == Allowed {
		t.Fatalf("read-only token allowed status writes: %v %+v", err, capabilities)
	}
	state.mu.Lock()
	state.readOnlyScope = false
	state.mu.Unlock()
	if err := adapter.RefreshProjectStatus(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	capabilities, err = adapter.Capabilities(context.Background(), source, "", nil)
	if err != nil || capabilities["project-status-write"].Permission != Allowed {
		t.Fatalf("write scope not rediscovered: %v %+v", err, capabilities)
	}
	state.mu.Lock()
	state.readOnlyScope = true
	state.mu.Unlock()
	if err := adapter.RefreshProjectStatus(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	_, _, err = writer.Prepare(context.Background(), WriteIntent{OperationID: strings.Repeat("a", 32), Source: source, Ref: Ref{SourceID: source.ID, ItemID: "6"}, Principal: "tester", Action: ActionStart, Transition: "option-progress"})
	if !githubProjectDiag(err, "project-write-disabled") {
		t.Fatalf("revoked project scope still allowed a write preview: %v", err)
	}
}

type projectWriteClaim struct {
	committed bool
}

func (*projectWriteClaim) Verify(context.Context, WriteIntent) error { return nil }
func (c *projectWriteClaim) Checkpoint(context.Context, WriteIntent, ProviderReceipt) error {
	c.committed = true
	return nil
}
func (c *projectWriteClaim) CheckpointStatus(context.Context, WriteIntent, ProviderReceipt) (WriteVerification, error) {
	if c.committed {
		return WriteVerified, nil
	}
	return WriteUnknown, nil
}

func githubProjectDiag(err error, code string) bool {
	diagnostic, ok := err.(GitHubDiagnostic)
	return ok && diagnostic.Code == code
}
