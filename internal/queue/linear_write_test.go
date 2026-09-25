package queue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/testkit"
)

const linearStartedState = "4c6e838c-e1d0-4672-8e40-fc1a1f78f752"
const linearOpenState = "cf992607-419b-467f-91f6-37440fc00869"
const linearCompleteState = "c90e56a1-a40d-45ee-a6d8-798cb696026b"

type linearWriteFixture struct {
	mu sync.Mutex

	viewer              string
	mismatchViewerAt    int
	identityCalls       int
	stateID             string
	stateType           string
	stateName           string
	targetType          string
	targetName          string
	assignee            string
	updatedAt           string
	mutationCalls       int
	comments            []linearComment
	commentGraphQLError bool
	hideCommentReads    int
	activeRequests      int
	maxActiveRequests   int
	blockIdentity       bool
	identityStarted     chan struct{}
	releaseIdentity     chan struct{}
	listStarted         chan struct{}
}

func newLinearWriteFixture(t *testing.T) (*LinearAdapter, *LinearWriteAdapter, Source, *linearWriteFixture) {
	t.Helper()
	testkit.Home(t)
	state := &linearWriteFixture{
		viewer: linearTestViewer, stateID: linearOpenState, stateType: "unstarted", stateName: "To Do",
		targetType: "started", targetName: "In Progress", updatedAt: "2026-09-25T12:00:00Z",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		state.activeRequests++
		if state.activeRequests > state.maxActiveRequests {
			state.maxActiveRequests = state.activeRequests
		}
		state.mu.Unlock()
		defer func() {
			state.mu.Lock()
			state.activeRequests--
			state.mu.Unlock()
		}()

		var request struct {
			Query     string
			Variables map[string]json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		variable := func(key string) string {
			var value string
			_ = json.Unmarshal(request.Variables[key], &value)
			return value
		}
		var data any
		switch request.Query {
		case linearIdentityQuery:
			state.mu.Lock()
			state.identityCalls++
			viewer := state.viewer
			if state.mismatchViewerAt > 0 && state.identityCalls >= state.mismatchViewerAt {
				viewer = linearTestBlocker
			}
			block := state.blockIdentity
			started, release := state.identityStarted, state.releaseIdentity
			state.mu.Unlock()
			if block {
				if started != nil {
					select {
					case started <- struct{}{}:
					default:
					}
				}
				if release != nil {
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
			}
			data = map[string]any{"viewer": map[string]any{"id": viewer, "organization": map[string]any{"id": linearTestOrg}}, "team": map[string]any{"id": linearTestTeam, "name": "TEST"}}
		case linearWorkflowStatesQuery:
			state.mu.Lock()
			states := []any{
				map[string]any{"id": linearStartedState, "name": state.targetName, "type": state.targetType},
				map[string]any{"id": linearOpenState, "name": state.stateName, "type": state.stateType},
				map[string]any{"id": linearCompleteState, "name": "Done", "type": "completed"},
			}
			state.mu.Unlock()
			data = map[string]any{"team": map[string]any{"id": linearTestTeam, "states": map[string]any{"nodes": states}}}
		case linearListQuery:
			if state.listStarted != nil {
				select {
				case state.listStarted <- struct{}{}:
				default:
				}
			}
			data = map[string]any{"team": map[string]any{"id": linearTestTeam, "issues": map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": false}}}}
		case linearDetailQuery:
			data = map[string]any{"issue": state.issue(variable("id"))}
		case linearIssueCommentsQuery:
			if state.listStarted != nil {
				select {
				case state.listStarted <- struct{}{}:
				default:
				}
			}
			state.mu.Lock()
			comments := append([]linearComment(nil), state.comments...)
			if state.hideCommentReads > 0 {
				state.hideCommentReads--
				comments = nil
			}
			state.mu.Unlock()
			items := make([]any, 0, len(comments))
			for _, comment := range comments {
				items = append(items, map[string]any{"id": comment.ID, "body": comment.Body, "user": map[string]any{"id": comment.User.ID}})
			}
			data = map[string]any{"issue": map[string]any{"id": variable("id"), "team": map[string]any{"id": linearTestTeam}, "comments": map[string]any{"nodes": items, "pageInfo": map[string]any{"hasNextPage": false}}}}
		case linearIssueUpdateStateMutation:
			if !strings.Contains(request.Query, "issueUpdate(id:$id,input:{stateId:$stateId})") {
				t.Error("state update must pass issue id outside the update input")
			}
			state.mu.Lock()
			state.mutationCalls++
			state.stateID, state.stateType, state.stateName = variable("stateId"), "started", "In Progress"
			state.updatedAt = "2026-09-25T12:01:00Z"
			state.mu.Unlock()
			data = map[string]any{"issueUpdate": map[string]any{"success": true, "issue": map[string]any{"id": variable("id")}}}
		case linearIssueUpdateAssigneeMutation:
			if !strings.Contains(request.Query, "issueUpdate(id:$id,input:{assigneeId:$assigneeId})") {
				t.Error("assignee update must pass issue id outside the update input")
			}
			state.mu.Lock()
			state.mutationCalls++
			state.assignee = variable("assigneeId")
			state.updatedAt = "2026-09-25T12:01:00Z"
			state.mu.Unlock()
			data = map[string]any{"issueUpdate": map[string]any{"success": true, "issue": map[string]any{"id": variable("id")}}}
		case linearCommentCreateMutation:
			state.mu.Lock()
			state.mutationCalls++
			comment := linearComment{ID: "comment-1", Body: variable("body"), User: &struct{ ID string }{ID: state.viewer}}
			state.comments = append(state.comments, comment)
			fail := state.commentGraphQLError
			state.mu.Unlock()
			if fail {
				_ = json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "synthetic partial response"}}, "data": map[string]any{"commentCreate": nil}})
				return
			}
			data = map[string]any{"commentCreate": map[string]any{"success": true, "comment": map[string]any{"id": comment.ID, "body": comment.Body, "user": map[string]any{"id": comment.User.ID}}}}
		default:
			t.Errorf("unexpected Linear operation: %s", request.Query)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(server.Close)
	adapter := NewLinearAdapter()
	adapter.APIBase = server.URL
	source, err := adapter.Resolve(context.Background(), map[string]string{"id": "linear-write-test", "organization": linearTestOrg, "team": linearTestTeam, "account": linearTestViewer, "credentialHelper": `["/bin/echo","fixture-token"]`})
	if err != nil {
		t.Fatalf("resolve Linear source: %v", err)
	}
	return adapter, NewLinearWriteAdapter(adapter), source, state
}

func (s *linearWriteFixture) issue(id string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != linearTestIssue {
		return nil
	}
	var assignee any
	if s.assignee != "" {
		assignee = map[string]any{"id": s.assignee}
	}
	return map[string]any{
		"id": id, "identifier": "TEST-1", "title": "synthetic task", "description": "fixture",
		"updatedAt": s.updatedAt, "archivedAt": nil, "trashed": false,
		"team": map[string]any{"id": linearTestTeam}, "project": nil,
		"state": map[string]any{"id": s.stateID, "name": s.stateName, "type": s.stateType}, "assignee": assignee,
	}
}

func linearWriteIntent(source Source, action Action, transition, text string) WriteIntent {
	return WriteIntent{
		OperationID: strings.Repeat("a", 32), Source: source,
		Ref: Ref{SourceID: source.ID, ItemID: linearTestIssue}, Principal: linearTestViewer,
		Action: action, Transition: transition, Append: text,
	}
}

func TestLinearWriteRequiresFreshConfiguredStartedStateAndViewer(t *testing.T) {
	t.Parallel()
	t.Run("state type not display name", func(t *testing.T) {
		t.Parallel()
		_, writer, source, state := newLinearWriteFixture(t)
		state.targetType, state.targetName = "unstarted", "In Progress"
		_, _, err := writer.Prepare(context.Background(), linearWriteIntent(source, ActionStart, linearStartedState, ""))
		if err == nil || !strings.Contains(err.Error(), "wrong workflow type") {
			t.Fatalf("Start accepted a non-started state with a matching display name: %v", err)
		}
		state.mu.Lock()
		writes := state.mutationCalls
		state.mu.Unlock()
		if writes != 0 {
			t.Fatalf("preparation dispatched %d writes", writes)
		}
	})
	t.Run("viewer refreshed before write", func(t *testing.T) {
		t.Parallel()
		_, writer, source, state := newLinearWriteFixture(t)
		intent, _, err := writer.Prepare(context.Background(), linearWriteIntent(source, ActionStart, linearStartedState, ""))
		if err != nil {
			t.Fatal(err)
		}
		state.mu.Lock()
		state.mismatchViewerAt = state.identityCalls + 1
		state.mu.Unlock()
		_, err = writer.Write(context.Background(), intent)
		state.mu.Lock()
		writes := state.mutationCalls
		state.mu.Unlock()
		if err == nil || strings.Contains(err.Error(), "fixture-token") || writes != 0 {
			t.Fatalf("write did not fail closed on fresh viewer mismatch: err=%v writes=%d", err, writes)
		}
	})
}

func TestLinearWriteConfiguredStateAndConfirmedSingleAssignee(t *testing.T) {
	t.Parallel()
	t.Run("state write", func(t *testing.T) {
		t.Parallel()
		_, writer, source, state := newLinearWriteFixture(t)
		intent, _, err := writer.Prepare(context.Background(), linearWriteIntent(source, ActionStart, linearStartedState, ""))
		if err != nil {
			t.Fatal(err)
		}
		pre, err := writer.Inspect(context.Background(), intent)
		if err != nil || !pre.Ready {
			t.Fatalf("Start preflight: %+v %v", pre, err)
		}
		receipt, err := writer.Write(context.Background(), intent)
		if err != nil || receipt.ID != linearTestIssue {
			t.Fatalf("Linear state write: %+v %v", receipt, err)
		}
		state.mu.Lock()
		got, writes := state.stateID, state.mutationCalls
		state.mu.Unlock()
		if got != linearStartedState || writes != 1 {
			t.Fatalf("state=%s writes=%d", got, writes)
		}
	})
	t.Run("replace assignee only after preview confirmation", func(t *testing.T) {
		t.Parallel()
		_, writer, source, state := newLinearWriteFixture(t)
		state.assignee = linearTestBlocker
		intent, preview, err := writer.Prepare(context.Background(), linearWriteIntent(source, ActionAssignToMe, "", ""))
		if err != nil || intent.ExpectedAssignee != linearTestBlocker || !strings.Contains(preview.Operation, linearTestBlocker) || !strings.Contains(preview.Operation, linearTestViewer) {
			t.Fatalf("replacement not disclosed: intent=%+v preview=%+v err=%v", intent, preview, err)
		}
		if _, err := writer.Inspect(context.Background(), intent); err == nil || !strings.Contains(err.Error(), "confirm replacing") {
			t.Fatalf("replacement proceeded without explicit confirmation: %v", err)
		}
		state.mu.Lock()
		if state.mutationCalls != 0 {
			state.mu.Unlock()
			t.Fatalf("unconfirmed assignment dispatched: %d", state.mutationCalls)
		}
		state.mu.Unlock()
		intent.AssigneeReplacementConfirmed = true
		receipt, err := writer.Write(context.Background(), intent)
		if err != nil || receipt.ID != linearTestIssue {
			t.Fatalf("confirmed assignment failed: %+v %v", receipt, err)
		}
		state.mu.Lock()
		got, writes := state.assignee, state.mutationCalls
		state.mu.Unlock()
		if got != linearTestViewer || writes != 1 {
			t.Fatalf("assignee=%s writes=%d", got, writes)
		}
	})
}

func linearPipeline(t *testing.T, writer *LinearWriteAdapter, source Source, intent WriteIntent) (WritePipeline, *writeFixture, WriteIntent) {
	t.Helper()
	pipeline, claim, base := writeSetup(t)
	intent.AuthorityID, intent.ClaimID, intent.ClaimRevision = base.AuthorityID, base.ClaimID, base.ClaimRevision
	intent.Resources, intent.OperationRef = base.Resources, base.OperationRef
	intent.CheckpointTTL, intent.CheckpointNotAfter = base.CheckpointTTL, base.CheckpointNotAfter
	pipeline.Adapter = writer
	pipeline.Workflow = map[string]string{"start": linearStartedState, "blocked": linearStartedState, "review": linearStartedState, "complete": linearCompleteState, "reopen": linearOpenState}
	pipeline.Claim = claim
	return pipeline, claim, intent
}

func TestLinearCommentRecoveryHandlesPartialEffectsLagAndAmbiguousMarkers(t *testing.T) {
	t.Parallel()
	t.Run("partial effect and lagged readback", func(t *testing.T) {
		t.Parallel()
		_, writer, source, state := newLinearWriteFixture(t)
		state.commentGraphQLError = true
		state.hideCommentReads = 1
		intent, _, err := writer.Prepare(context.Background(), linearWriteIntent(source, ActionRecordProgress, "", "progress update"))
		if err != nil {
			t.Fatal(err)
		}
		pipeline, _, intent := linearPipeline(t, writer, source, intent)
		result, err := pipeline.Start(context.Background(), intent)
		if err == nil || result.Outcome != WriteUnknown {
			t.Fatalf("partial comment effect should be unknown: %+v %v", result, err)
		}
		first, err := pipeline.Recover(context.Background(), intent.OperationID)
		if err != nil || first.Outcome != WriteUnknown {
			t.Fatalf("lagged comment read-back should remain unresolved: %+v %v", first, err)
		}
		second, err := pipeline.Recover(context.Background(), intent.OperationID)
		if err != nil || second.Outcome != WriteVerified {
			t.Fatalf("visible exact marker should recover without redispatch: %+v %v", second, err)
		}
		state.mu.Lock()
		writes := state.mutationCalls
		state.mu.Unlock()
		if writes != 1 {
			t.Fatalf("recovery redispatched comment %d times", writes)
		}
	})
	t.Run("ambiguous duplicate marker", func(t *testing.T) {
		t.Parallel()
		_, writer, source, state := newLinearWriteFixture(t)
		state.commentGraphQLError = true
		intent, _, err := writer.Prepare(context.Background(), linearWriteIntent(source, ActionRecordProgress, "", "progress update"))
		if err != nil {
			t.Fatal(err)
		}
		pipeline, _, intent := linearPipeline(t, writer, source, intent)
		_, _ = pipeline.Start(context.Background(), intent)
		state.mu.Lock()
		state.comments = append(state.comments, linearComment{ID: "copied-marker", Body: "copied <!-- " + intent.Marker + " -->", User: &struct{ ID string }{ID: linearTestBlocker}})
		state.mu.Unlock()
		result, err := pipeline.Recover(context.Background(), intent.OperationID)
		if err != nil || result.Outcome != WriteUnknown {
			t.Fatalf("duplicate marker was treated as proof: %+v %v", result, err)
		}
		state.mu.Lock()
		writes := state.mutationCalls
		state.mu.Unlock()
		if writes != 1 {
			t.Fatalf("ambiguous recovery redispatched comment %d times", writes)
		}
	})
}

func TestLinearRequestsShareSerializedOrganizationAccountQuota(t *testing.T) {
	t.Parallel()
	adapter, writer, source, state := newLinearWriteFixture(t)
	intent, _, err := writer.Prepare(context.Background(), linearWriteIntent(source, ActionStart, linearStartedState, ""))
	if err != nil {
		t.Fatal(err)
	}
	state.identityStarted = make(chan struct{}, 1)
	state.releaseIdentity = make(chan struct{})
	state.listStarted = make(chan struct{}, 1)
	state.blockIdentity = true
	writeDone := make(chan error, 1)
	go func() {
		_, err := writer.Write(context.Background(), intent)
		writeDone <- err
	}()
	select {
	case <-state.identityStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("write did not reach its serialized identity check")
	}
	listDone := make(chan error, 1)
	go func() {
		_, err := adapter.List(context.Background(), source, Query{Budget: 1}, "")
		listDone <- err
	}()
	select {
	case <-state.listStarted:
		close(state.releaseIdentity)
		t.Fatal("read interleaved with serialized Linear write preflight")
	case <-time.After(25 * time.Millisecond):
	}
	close(state.releaseIdentity)
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatalf("Linear write: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Linear write did not finish")
	}
	select {
	case err := <-listDone:
		if err != nil {
			t.Fatalf("Linear list: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Linear list did not finish")
	}
	state.mu.Lock()
	maxActive := state.maxActiveRequests
	state.mu.Unlock()
	if maxActive != 1 {
		t.Fatalf("concurrent Linear requests for one org/account: max=%d", maxActive)
	}
}
