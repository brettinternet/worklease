package queue

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeGitHubWriteComment struct {
	ID, Body, Author string
}

type fakeGitHubWriteState struct {
	mu                  sync.Mutex
	viewer              string
	issueState          string
	stateReason         string
	forceReason         string
	assignees           []string
	comments            []fakeGitHubWriteComment
	mutationCalls       int
	commentReadLags     int
	issueReadLags       int
	lagNextWrite        bool
	previousState       string
	previousReason      string
	previousAssignees   []string
	loseCommentResponse bool
	rateLimitWrites     bool
	lastToken           string
	viewerCalls         int
}

func fakeGitHubWriteHandler(t *testing.T, state *fakeGitHubWriteState) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		state.lastToken = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		state.mu.Unlock()
		if r.Method == http.MethodPost && r.URL.Path == "/" {
			var request struct {
				Query string `json:"query"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode GraphQL request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			switch {
			case strings.Contains(request.Query, "viewer"):
				state.mu.Lock()
				state.viewerCalls++
				viewer := state.viewer
				state.mu.Unlock()
				writeGitHubFakeJSON(w, map[string]any{"data": map[string]any{"viewer": map[string]any{"login": viewer}}})
			case strings.Contains(request.Query, "comments(first:$count"):
				state.mu.Lock()
				comments := append([]fakeGitHubWriteComment(nil), state.comments...)
				lag := state.commentReadLags > 0
				if lag {
					state.commentReadLags--
				}
				state.mu.Unlock()
				if lag {
					comments = nil
				}
				nodes := make([]map[string]any, 0, len(comments))
				for _, comment := range comments {
					nodes = append(nodes, map[string]any{"id": comment.ID, "body": comment.Body, "createdAt": "2025-01-02T03:04:05Z", "author": map[string]any{"login": comment.Author}})
				}
				issue := map[string]any{
					"number":     1,
					"repository": map[string]any{"nameWithOwner": "org/repo"},
					"comments":   map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": false, "endCursor": nil}},
				}
				writeGitHubFakeJSON(w, map[string]any{"data": map[string]any{"repository": map[string]any{"nameWithOwner": "org/repo", "issue": issue}}})
			case strings.Contains(request.Query, "issue(number:"):
				state.mu.Lock()
				issueState, reason := state.issueState, state.stateReason
				assignees := append([]string(nil), state.assignees...)
				if state.issueReadLags > 0 {
					state.issueReadLags--
					issueState, reason = state.previousState, state.previousReason
					assignees = append([]string(nil), state.previousAssignees...)
				}
				state.mu.Unlock()
				owners := make([]map[string]any, 0, len(assignees))
				for _, assignee := range assignees {
					owners = append(owners, map[string]any{"login": assignee})
				}
				issue := map[string]any{
					"id": "ISSUE_1", "number": 1, "title": "test issue", "body": "",
					"state": issueState, "stateReason": reason, "updatedAt": "2025-01-02T03:04:05Z",
					"repository": map[string]any{"nameWithOwner": "org/repo"},
					"assignees":  map[string]any{"nodes": owners},
				}
				writeGitHubFakeJSON(w, map[string]any{"data": map[string]any{"repository": map[string]any{"nameWithOwner": "org/repo", "issue": issue}}})
			default:
				t.Errorf("unexpected GraphQL query %q", request.Query)
				w.WriteHeader(http.StatusBadRequest)
			}
			return
		}
		if strings.HasPrefix(r.URL.Path, "/repos/org/repo/issues/1") {
			state.mu.Lock()
			state.mutationCalls++
			limit := state.rateLimitWrites
			state.mu.Unlock()
			if limit {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			switch {
			case r.Method == http.MethodPatch && r.URL.Path == "/repos/org/repo/issues/1":
				var patch struct {
					State       string `json:"state"`
					StateReason string `json:"state_reason"`
				}
				if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
					t.Errorf("decode state patch: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				state.mu.Lock()
				state.previousState, state.previousReason = state.issueState, state.stateReason
				state.previousAssignees = append([]string(nil), state.assignees...)
				if state.lagNextWrite {
					state.issueReadLags = 1
				}
				state.issueState = strings.ToUpper(patch.State)
				state.stateReason = strings.ToUpper(patch.StateReason)
				if state.forceReason != "" {
					state.stateReason = state.forceReason
				}
				state.mu.Unlock()
				writeGitHubFakeJSON(w, map[string]any{"number": 1, "state": patch.State, "state_reason": patch.StateReason})
			case r.Method == http.MethodPost && r.URL.Path == "/repos/org/repo/issues/1/comments":
				var payload struct {
					Body string `json:"body"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode comment: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				state.mu.Lock()
				comment := fakeGitHubWriteComment{ID: "COMMENT_1", Body: payload.Body, Author: "tester"}
				state.comments = append(state.comments, comment)
				lose := state.loseCommentResponse
				state.mu.Unlock()
				if lose {
					connection, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Errorf("hijack lost response: %v", err)
						return
					}
					_ = connection.Close()
					return
				}
				writeGitHubFakeJSON(w, map[string]any{"id": 1, "node_id": "COMMENT_1", "body": payload.Body, "user": map[string]any{"login": "tester"}})
			case r.Method == http.MethodPost && r.URL.Path == "/repos/org/repo/issues/1/assignees":
				var payload struct {
					Assignees []string `json:"assignees"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode assignees: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				state.mu.Lock()
				state.previousState, state.previousReason = state.issueState, state.stateReason
				state.previousAssignees = append([]string(nil), state.assignees...)
				if state.lagNextWrite {
					state.issueReadLags = 1
				}
				for _, added := range payload.Assignees {
					found := false
					for _, existing := range state.assignees {
						found = found || existing == added
					}
					if !found {
						state.assignees = append(state.assignees, added)
					}
				}
				assignees := append([]string(nil), state.assignees...)
				state.mu.Unlock()
				users := make([]map[string]any, 0, len(assignees))
				for _, assignee := range assignees {
					users = append(users, map[string]any{"login": assignee})
				}
				writeGitHubFakeJSON(w, users)
			default:
				t.Errorf("unexpected GitHub write %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
			return
		}
		t.Errorf("unexpected GitHub request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func githubWriteDiag(err error, code string) bool {
	var diagnostic GitHubDiagnostic
	return errors.As(err, &diagnostic) && diagnostic.Code == code
}

func writeGitHubFakeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		panic(err)
	}
}

func githubWriteSetup(t *testing.T, state *fakeGitHubWriteState) (WritePipeline, *writeFixture, *GitHubWriteAdapter, Source, WriteIntent) {
	t.Helper()
	adapter, _ := fakeGitHub(t, fakeGitHubWriteHandler(t, state))
	source, err := adapter.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"})
	if err != nil {
		t.Fatal(err)
	}
	writer := NewGitHubWriteAdapter(adapter, true)
	pipeline, claim, intent := writeSetup(t)
	intent.Source = source
	intent.Ref = Ref{SourceID: source.ID, ItemID: "1"}
	intent.Principal = "tester"
	intent.Resources = []string{"github:org/repo#1"}
	pipeline.Adapter = writer
	pipeline.Workflow = map[string]string{"complete": "closed", "reopen": "open"}
	return pipeline, claim, writer, source, intent
}

func TestGitHubWriteCloseAndReopenStateReason(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		action     Action
		transition string
		initial    string
		reason     string
		want       string
		wantReason string
	}{
		{"close-completed", ActionComplete, "closed", "OPEN", "", "CLOSED", "COMPLETED"},
		{"reopen", ActionReopen, "open", "CLOSED", "COMPLETED", "OPEN", "REOPENED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := &fakeGitHubWriteState{viewer: "tester", issueState: test.initial, stateReason: test.reason}
			pipeline, claim, writer, source, intent := githubWriteSetup(t, state)
			intent.Action, intent.Transition = test.action, test.transition
			intent.Patch = nil
			intent, preview, err := writer.Prepare(context.Background(), intent)
			if err != nil || preview.ConditionalBodyWrite || preview.Operation == "" {
				t.Fatalf("prepare: %+v %v", preview, err)
			}
			intent.Source = source
			result, err := pipeline.Start(context.Background(), intent)
			if err != nil || result.Outcome != WriteVerified || claim.checkpointCalls != 1 {
				record, _ := pipeline.Journal.Read(intent.OperationID)
				observation, readErr := writer.ReadReceipt(context.Background(), intent, record.Receipt)
				t.Fatalf("write: %+v %v checkpoints=%d observation=%+v readErr=%v intent=%+v receipt=%+v", result, err, claim.checkpointCalls, observation, readErr, intent, record.Receipt)
			}
			state.mu.Lock()
			gotState, gotReason, writes := state.issueState, state.stateReason, state.mutationCalls
			state.mu.Unlock()
			if gotState != test.want || gotReason != test.wantReason || writes != 1 {
				t.Fatalf("state=%s reason=%s writes=%d", gotState, gotReason, writes)
			}
		})
	}
}

func TestGitHubNotPlannedCloseNeverVerifiesCompletion(t *testing.T) {
	t.Parallel()
	state := &fakeGitHubWriteState{viewer: "tester", issueState: "OPEN", forceReason: "NOT_PLANNED"}
	pipeline, claim, writer, _, intent := githubWriteSetup(t, state)
	intent.Action, intent.Transition, intent.Patch = ActionComplete, "closed", nil
	intent, _, err := writer.Prepare(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	result, err := pipeline.Start(context.Background(), intent)
	if err != nil || result.Outcome != WriteUnknown || claim.checkpointCalls != 0 {
		t.Fatalf("not-planned completion=%+v err=%v checkpoints=%d", result, err, claim.checkpointCalls)
	}
	state.mu.Lock()
	writes := state.mutationCalls
	state.mu.Unlock()
	if writes != 1 {
		t.Fatalf("mutation count=%d", writes)
	}
}

func TestGitHubWriteCommentMarkerAndAssigneePreservation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		action    Action
		append    string
		assignees []string
	}{
		{"progress-comment", ActionRecordProgress, "progress update", nil},
		{"assign-to-me", ActionAssignToMe, "", []string{"alice"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			state := &fakeGitHubWriteState{viewer: "tester", issueState: "OPEN", assignees: test.assignees}
			pipeline, claim, writer, _, intent := githubWriteSetup(t, state)
			intent.Action, intent.Append, intent.Patch = test.action, test.append, nil
			intent, preview, err := writer.Prepare(context.Background(), intent)
			if err != nil {
				t.Fatal(err)
			}
			if test.action == ActionRecordProgress && (!strings.HasPrefix(preview.Marker, "<!-- worklease-op:") || intent.Marker != "worklease-op:"+intent.OperationID) {
				t.Fatalf("marker preview=%q intent=%q", preview.Marker, intent.Marker)
			}
			result, err := pipeline.Start(context.Background(), intent)
			if err != nil || result.Outcome != WriteVerified || claim.checkpointCalls != 1 {
				t.Fatalf("write: %+v %v checkpoints=%d", result, err, claim.checkpointCalls)
			}
			state.mu.Lock()
			defer state.mu.Unlock()
			if test.action == ActionRecordProgress {
				if len(state.comments) != 1 || state.comments[0].Body != test.append+"\n\n<!-- "+intent.Marker+" -->" || state.comments[0].Author != intent.Principal {
					t.Fatalf("comment provenance: %+v", state.comments)
				}
			} else if strings.Join(state.assignees, ",") != "alice,tester" {
				t.Fatalf("unrelated assignee lost: %v", state.assignees)
			}
		})
	}
}

func TestGitHubLostCommentResponseWithLaggingReadBackNeverRedispatches(t *testing.T) {
	t.Parallel()
	state := &fakeGitHubWriteState{viewer: "tester", issueState: "OPEN", commentReadLags: 1, loseCommentResponse: true}
	pipeline, claim, writer, _, intent := githubWriteSetup(t, state)
	intent.Action, intent.Append, intent.Patch = ActionRecordProgress, "progress update", nil
	intent, _, err := writer.Prepare(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	result, err := pipeline.Start(context.Background(), intent)
	if err == nil || result.Outcome != WriteUnknown {
		t.Fatalf("lost response: %+v %v", result, err)
	}
	result, err = pipeline.Recover(context.Background(), intent.OperationID)
	if err != nil || result.Outcome != WriteUnknown || claim.checkpointCalls != 0 {
		t.Fatalf("lagging read-back: %+v %v checkpoints=%d", result, err, claim.checkpointCalls)
	}
	// A new read-only session must still be able to resolve the old write.
	writer.Interactive = false
	result, err = pipeline.Recover(context.Background(), intent.OperationID)
	if err != nil || result.Outcome != WriteVerified || claim.checkpointCalls != 1 {
		t.Fatalf("visible append recovery: %+v %v checkpoints=%d", result, err, claim.checkpointCalls)
	}
	state.mu.Lock()
	writes := state.mutationCalls
	state.mu.Unlock()
	if writes != 1 {
		t.Fatalf("uncertain append dispatched %d times", writes)
	}
}

func TestGitHubWriteStateAndAssignmentLagRemainRecoverable(t *testing.T) {
	t.Parallel()
	for _, action := range []Action{ActionComplete, ActionAssignToMe} {
		t.Run(string(action), func(t *testing.T) {
			t.Parallel()
			state := &fakeGitHubWriteState{viewer: "tester", issueState: "OPEN"}
			pipeline, claim, writer, _, intent := githubWriteSetup(t, state)
			intent.Action, intent.Patch = action, nil
			if action == ActionComplete {
				intent.Transition = "closed"
			}
			var err error
			intent, _, err = writer.Prepare(context.Background(), intent)
			if err != nil {
				t.Fatal(err)
			}
			state.mu.Lock()
			state.lagNextWrite = true
			state.mu.Unlock()
			result, err := pipeline.Start(context.Background(), intent)
			if err != nil || result.Outcome != WriteUnknown || claim.checkpointCalls != 0 {
				t.Fatalf("lagging read-back: %+v %v checkpoints=%d", result, err, claim.checkpointCalls)
			}
			writer.Interactive = false
			result, err = pipeline.Recover(context.Background(), intent.OperationID)
			if err != nil || result.Outcome != WriteVerified || claim.checkpointCalls != 1 {
				t.Fatalf("recovered write: %+v %v checkpoints=%d", result, err, claim.checkpointCalls)
			}
			state.mu.Lock()
			writes := state.mutationCalls
			state.mu.Unlock()
			if writes != 1 {
				t.Fatalf("mutation dispatched %d times", writes)
			}
		})
	}
}

func TestGitHubWritePrincipalAndCredentialChangeMustReverify(t *testing.T) {
	t.Parallel()
	state := &fakeGitHubWriteState{viewer: "tester", issueState: "OPEN"}
	pipeline, _, writer, _, intent := githubWriteSetup(t, state)
	intent.Action, intent.Append, intent.Patch = ActionRecordProgress, "progress", nil
	intent, _, err := writer.Prepare(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(writer.Binary, []byte("#!/bin/sh\nprintf 'rotated-token\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	// Resolving a rotated credential verifies its account; the write path must
	// independently verify that same credential immediately before dispatch.
	if _, err := writer.Resolve(context.Background(), map[string]string{"host": "github.com", "repository": "org/repo", "account": "tester"}); err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	state.viewer = "someone-else"
	state.mu.Unlock()
	result, err := pipeline.Start(context.Background(), intent)
	if err == nil || !result.SourceUnchanged {
		t.Fatalf("principal mismatch did not fail preflight: %+v %v", result, err)
	}
	state.mu.Lock()
	writes, token := state.mutationCalls, state.lastToken
	state.mu.Unlock()
	if writes != 0 || token != "rotated-token" {
		t.Fatalf("mismatch dispatched mutation=%d last token=%q", writes, token)
	}
}

func TestGitHubWritesRequireInteractiveModeAndMappedOperations(t *testing.T) {
	t.Parallel()
	state := &fakeGitHubWriteState{viewer: "tester", issueState: "OPEN"}
	_, _, writer, _, intent := githubWriteSetup(t, state)
	writer.Interactive = false
	if _, _, err := writer.Prepare(context.Background(), intent); !githubWriteDiag(err, "interactive-required") {
		t.Fatalf("unattended write was enabled: %v", err)
	}
	writer.Interactive = true
	for _, action := range []Action{ActionStart, ActionResume, ActionReportBlocked, ActionRequestReview, Action("check-criterion")} {
		if err := writer.ValidateTransition(context.Background(), intent.Source, action, "mapped-value"); !githubWriteDiag(err, "no-workflow-mapping") {
			t.Errorf("action %s got workflow mapping: %v", action, err)
		}
	}
	intent.Action, intent.Transition, intent.Patch = ActionRecordProgress, "", map[string]string{"body": "replace issue body"}
	intent.Append = "unsafe body edit"
	if _, _, err := writer.Prepare(context.Background(), intent); !githubWriteDiag(err, "no-conditional-body-write") {
		t.Fatalf("conditional body write accepted: %v", err)
	}
	state.mu.Lock()
	writes := state.mutationCalls
	state.mu.Unlock()
	if writes != 0 {
		t.Fatalf("unsupported request dispatched %d writes", writes)
	}
}

func TestAdapterConformanceGitHubWriteQuota(t *testing.T) {
	t.Parallel()
	state := &fakeGitHubWriteState{viewer: "tester", issueState: "OPEN", rateLimitWrites: true}
	pipeline, _, writer, _, intent := githubWriteSetup(t, state)
	intent.Action, intent.Transition, intent.Patch = ActionComplete, "closed", nil
	intent, _, err := writer.Prepare(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	result, err := pipeline.Start(context.Background(), intent)
	if err == nil || result.Outcome != WriteUnknown {
		t.Fatalf("rate-limited write result=%+v err=%v", result, err)
	}
	var diagnostic GitHubRateDiagnostic
	if !errors.As(err, &diagnostic) || diagnostic.RetryAt.Before(time.Now().Add(50*time.Second)) {
		t.Fatalf("rate limit deadline not honored: %v", err)
	}
	state.mu.Lock()
	writes := state.mutationCalls
	state.mu.Unlock()
	if writes != 1 {
		t.Fatalf("rate-limited mutation retried %d times", writes)
	}
}
