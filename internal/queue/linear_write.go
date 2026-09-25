package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

const linearWorkflowStatesQuery = `query($team:String!) { team(id:$team) { id states { nodes { id name type } } } }`
const linearIssueCommentsQuery = `query($id:String!,$after:String,$count:Int!) { issue(id:$id) { id team { id } project { id } comments(first:$count,after:$after) { nodes { id body user { id } } pageInfo { hasNextPage endCursor } } } }`
const linearIssueUpdateStateMutation = `mutation($id:String!,$stateId:String!) { issueUpdate(id:$id,input:{stateId:$stateId}) { success issue { id } } }`
const linearIssueUpdateAssigneeMutation = `mutation($id:String!,$assigneeId:String!) { issueUpdate(id:$id,input:{assigneeId:$assigneeId}) { success issue { id } } }`
const linearCommentCreateMutation = `mutation($issueId:String!,$body:String!) { commentCreate(input:{issueId:$issueId,body:$body}) { success comment { id body user { id } } } }`

// LinearWriteAdapter adds only the focused mutations supported by the probe.
// The read adapter retains ownership of credential resolution and quota policy.
type LinearWriteAdapter struct{ *LinearAdapter }

type LinearWritePreview struct {
	Operation string `json:"operation"`
	Marker    string `json:"marker,omitempty"`
}

type linearWorkflowState struct{ ID, Name, Type string }

type linearComment struct {
	ID   string
	Body string
	User *struct{ ID string }
}

func NewLinearWriteAdapter(read *LinearAdapter) *LinearWriteAdapter {
	return &LinearWriteAdapter{LinearAdapter: read}
}

func (a *LinearWriteAdapter) refreshBinding(ctx context.Context, source Source, gate *quotaQueue) (linearBinding, error) {
	b, err := a.binding(source)
	if err != nil {
		return linearBinding{}, err
	}
	helper := a.Helper
	if helper == nil {
		helper = new(CredentialHelper)
	}
	token, err := helper.Resolve(ctx, append([]string(nil), b.argv...), b.account, func(checkCtx context.Context, token string) (string, error) {
		candidate := b
		candidate.token = token
		var result struct {
			Viewer *struct {
				ID           string `json:"id"`
				Organization *struct {
					ID string `json:"id"`
				} `json:"organization"`
			} `json:"viewer"`
			Team *struct {
				ID string `json:"id"`
			} `json:"team"`
		}
		if gate == nil {
			err = a.query(checkCtx, candidate, linearIdentityQuery, map[string]any{"team": b.team}, &result)
		} else {
			var data json.RawMessage
			data, err = a.request(checkCtx, candidate, linearIdentityQuery, map[string]any{"team": b.team}, gate)
			if err == nil {
				err = json.Unmarshal(data, &result)
			}
		}
		if err != nil || result.Viewer == nil || result.Viewer.Organization == nil || result.Viewer.Organization.ID != b.organization || result.Team == nil || result.Team.ID != b.team {
			return "", LinearDiagnostic{"identity-changed", "organization, team, or viewer could not be verified"}
		}
		return result.Viewer.ID, nil
	})
	if err != nil {
		return linearBinding{}, LinearDiagnostic{"authentication", "credential or scope verification failed; check helper, account, organization and team"}
	}
	b.token = token
	b.generation = linearBindingGeneration(b.configGeneration, token)
	a.mu.Lock()
	current, ok := a.bindings[source.ID]
	if !ok || current.configGeneration != b.configGeneration || current.organization != b.organization || current.team != b.team || current.project != b.project || current.account != b.account {
		a.mu.Unlock()
		return linearBinding{}, LinearDiagnostic{"source-changed", "Linear source binding changed; reopen the write preview"}
	}
	current.token, current.generation = token, b.generation
	a.bindings[source.ID] = current
	a.mu.Unlock()
	return b, nil
}

func linearExpectedStateTypes(action Action) []string {
	switch action {
	case ActionStart, ActionResume, ActionReportBlocked, ActionRequestReview:
		return []string{"started"}
	case ActionComplete:
		return []string{"completed"}
	case ActionReopen:
		return []string{"backlog", "unstarted"}
	default:
		return nil
	}
}

func (a *LinearWriteAdapter) workflowState(ctx context.Context, b linearBinding, source Source, action Action, id string, request func(context.Context, linearBinding, string, any, any) error) (linearWorkflowState, error) {
	if !linearUUID(id) || len(linearExpectedStateTypes(action)) == 0 {
		return linearWorkflowState{}, LinearDiagnostic{"no-workflow-mapping", "configured Linear transition must be a workflow-state UUID"}
	}
	var result struct {
		Team *struct {
			ID     string
			States struct {
				Nodes []linearWorkflowState
			}
		}
	}
	if err := request(ctx, b, linearWorkflowStatesQuery, map[string]any{"team": b.team}, &result); err != nil {
		return linearWorkflowState{}, err
	}
	if result.Team == nil || result.Team.ID != b.team {
		return linearWorkflowState{}, LinearDiagnostic{"not-found-or-inaccessible", "bound Linear team workflow is unavailable"}
	}
	for _, state := range result.Team.States.Nodes {
		if state.ID != id {
			continue
		}
		for _, expected := range linearExpectedStateTypes(action) {
			if state.Type == expected {
				return state, nil
			}
		}
		return linearWorkflowState{}, LinearDiagnostic{"no-workflow-mapping", "configured Linear state has the wrong workflow type for this action"}
	}
	return linearWorkflowState{}, LinearDiagnostic{"no-workflow-mapping", "configured Linear state is not a state of the bound team"}
}

func (a *LinearWriteAdapter) ValidateTransition(ctx context.Context, source Source, action Action, transition string) error {
	if source.Adapter != "linear" {
		return LinearDiagnostic{"invalid-source", "Linear source required"}
	}
	b, err := a.binding(source)
	if err != nil {
		return err
	}
	_, err = a.workflowState(ctx, b, source, action, transition, func(ctx context.Context, binding linearBinding, operation string, variables any, out any) error {
		return a.query(ctx, binding, operation, variables, out)
	})
	return err
}

func (a *LinearWriteAdapter) Prepare(ctx context.Context, intent WriteIntent) (WriteIntent, LinearWritePreview, error) {
	var preview LinearWritePreview
	if a == nil || a.LinearAdapter == nil || !validOperationID(intent.OperationID) || intent.Source.Adapter != "linear" || intent.Ref.SourceID != intent.Source.ID || !linearUUID(intent.Ref.ItemID) {
		return intent, preview, LinearDiagnostic{"invalid-intent", "Linear source, issue UUID, and operation ID required"}
	}
	b, err := a.refreshBinding(ctx, intent.Source, nil)
	if err != nil {
		return intent, preview, err
	}
	if intent.Principal != b.account {
		return intent, preview, LinearDiagnostic{"authentication", "write principal does not match configured account"}
	}
	issue, err := a.readWriteIssue(ctx, b, intent.Source, intent.Ref.ItemID, func(ctx context.Context, binding linearBinding, operation string, variables any, out any) error {
		return a.query(ctx, binding, operation, variables, out)
	})
	if err != nil {
		return intent, preview, err
	}
	switch intent.Action {
	case ActionStart, ActionResume, ActionReportBlocked, ActionRequestReview, ActionComplete, ActionReopen:
		if intent.Append != "" || intent.Marker != "" {
			return intent, preview, LinearDiagnostic{"invalid-intent", "state transition cannot include a comment"}
		}
		if intent.Transition == "" {
			return intent, preview, LinearDiagnostic{"no-workflow-mapping", "configure a Linear workflow-state UUID"}
		}
		if len(intent.Patch) == 0 {
			intent.Patch = map[string]string{"stateId": intent.Transition}
		}
		if !exactStringMap(intent.Patch, map[string]string{"stateId": intent.Transition}) {
			return intent, preview, LinearDiagnostic{"invalid-intent", "Linear state write must change only the configured state"}
		}
		state, err := a.workflowState(ctx, b, intent.Source, intent.Action, intent.Transition, func(ctx context.Context, binding linearBinding, operation string, variables any, out any) error {
			return a.query(ctx, binding, operation, variables, out)
		})
		if err != nil {
			return intent, preview, err
		}
		preview.Operation = "set Linear state " + state.Name + " (" + state.ID + ")"
	case ActionAssignToMe:
		if intent.Append != "" || intent.Transition != "" || len(intent.Patch) != 0 || intent.Marker != "" {
			return intent, preview, LinearDiagnostic{"invalid-intent", "assignment must not include unrelated changes"}
		}
		intent.Patch = map[string]string{"assigneeId": b.account}
		intent.ExpectedAssignee = linearAssigneeID(issue)
		if intent.ExpectedAssignee != "" && intent.ExpectedAssignee != b.account {
			preview.Operation = "replace Linear assignee " + intent.ExpectedAssignee + " with " + b.account
		} else {
			preview.Operation = "assign Linear issue to " + b.account
		}
	case ActionRecordProgress:
		if strings.TrimSpace(intent.Append) == "" || strings.Contains(intent.Append, "worklease-op:") || intent.Transition != "" || intent.Marker != "" && intent.Marker != "worklease-op:"+intent.OperationID {
			return intent, preview, LinearDiagnostic{"invalid-intent", "progress requires nonempty comment content without an operation marker"}
		}
		if len(intent.Patch) == 0 {
			intent.Patch = map[string]string{"append": "comment"}
		}
		if !exactStringMap(intent.Patch, map[string]string{"append": "comment"}) {
			return intent, preview, LinearDiagnostic{"invalid-intent", "Linear progress supports marked comments only"}
		}
		intent.Marker = "worklease-op:" + intent.OperationID
		preview.Operation, preview.Marker = "append Linear comment", "<!-- "+intent.Marker+" -->"
	default:
		return intent, preview, LinearDiagnostic{"unsupported-action", "Linear supports configured state, marked comment, and assign-to-me writes only"}
	}
	intent.SourceGeneration = b.configGeneration
	intent.Precondition = linearWriteVersion(b, issue)
	return intent, preview, nil
}

func linearAssigneeID(issue linearIssue) string {
	if issue.Assignee == nil {
		return ""
	}
	return issue.Assignee.ID
}

func linearWriteVersion(binding linearBinding, issue linearIssue) string {
	data, _ := json.Marshal(struct {
		SourceGeneration  string
		ID, Team, Project string
		State, Assignee   string
		UpdatedAt         string
		ArchivedAt        string
		Trashed           bool
	}{
		binding.configGeneration, issue.ID, linearTeamID(issue), linearProjectID(issue), linearStateID(issue), linearAssigneeID(issue), issue.UpdatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), linearArchivedAt(issue), issue.Trashed,
	})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func linearTeamID(issue linearIssue) string {
	if issue.Team != nil {
		return issue.Team.ID
	}
	return ""
}
func linearProjectID(issue linearIssue) string {
	if issue.Project != nil {
		return issue.Project.ID
	}
	return ""
}
func linearStateID(issue linearIssue) string {
	if issue.State != nil {
		return issue.State.ID
	}
	return ""
}
func linearArchivedAt(issue linearIssue) string {
	if issue.ArchivedAt != nil {
		return issue.ArchivedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
	}
	return ""
}

// ValidateRecoveryBinding rejects a journaled intent from a different resolved
// source even when recovery only needs to replay a pending checkpoint.
func (a *LinearWriteAdapter) ValidateRecoveryBinding(intent WriteIntent) error {
	if a == nil || a.LinearAdapter == nil {
		return LinearDiagnostic{"invalid-source", "Linear read adapter required"}
	}
	binding, err := a.binding(intent.Source)
	if err != nil {
		return err
	}
	return a.validateIntent(intent, binding)
}

func (a *LinearWriteAdapter) validateIntent(intent WriteIntent, binding linearBinding) error {
	if intent.Source.Adapter != "linear" || intent.Source.Locator != binding.organization || intent.Ref.SourceID != intent.Source.ID || !linearUUID(intent.Ref.ItemID) || !validOperationID(intent.OperationID) || intent.Principal != binding.account || intent.SourceGeneration == "" || intent.SourceGeneration != binding.configGeneration {
		return LinearDiagnostic{"source-changed", "Linear source, principal, or credential-helper binding changed"}
	}
	switch intent.Action {
	case ActionStart, ActionResume, ActionReportBlocked, ActionRequestReview, ActionComplete, ActionReopen:
		if intent.Transition == "" || !exactStringMap(intent.Patch, map[string]string{"stateId": intent.Transition}) || intent.Append != "" || intent.Marker != "" || intent.ExpectedAssignee != "" || intent.AssigneeReplacementConfirmed {
			return LinearDiagnostic{"invalid-intent", "Linear state write must target exactly one configured state"}
		}
	case ActionAssignToMe:
		if !exactStringMap(intent.Patch, map[string]string{"assigneeId": binding.account}) || intent.Transition != "" || intent.Append != "" || intent.Marker != "" {
			return LinearDiagnostic{"invalid-intent", "Linear assignment must replace only the configured account"}
		}
		if intent.ExpectedAssignee != "" && intent.ExpectedAssignee != binding.account && !intent.AssigneeReplacementConfirmed {
			return LinearDiagnostic{"assignee-confirmation-required", "confirm replacing the existing Linear assignee in the write preview"}
		}
		if (intent.ExpectedAssignee == "" || intent.ExpectedAssignee == binding.account) && intent.AssigneeReplacementConfirmed {
			return LinearDiagnostic{"invalid-intent", "assignee replacement confirmation does not match the preview"}
		}
	case ActionRecordProgress:
		if !exactStringMap(intent.Patch, map[string]string{"append": "comment"}) || strings.TrimSpace(intent.Append) == "" || strings.Contains(intent.Append, "worklease-op:") || intent.Marker != "worklease-op:"+intent.OperationID || intent.Transition != "" {
			return LinearDiagnostic{"invalid-intent", "Linear progress requires a marked comment"}
		}
	default:
		return LinearDiagnostic{"unsupported-action", "unsupported Linear mutation"}
	}
	return nil
}

func (a *LinearWriteAdapter) readWriteIssue(ctx context.Context, binding linearBinding, source Source, id string, request func(context.Context, linearBinding, string, any, any) error) (linearIssue, error) {
	if source.Adapter != "linear" || source.Locator != binding.organization || !linearUUID(id) {
		return linearIssue{}, LinearDiagnostic{"invalid-ref", "issue is outside the configured Linear source"}
	}
	var result struct{ Issue *linearIssue }
	if err := request(ctx, binding, linearDetailQuery, map[string]any{"id": id}, &result); err != nil {
		return linearIssue{}, err
	}
	if result.Issue == nil || result.Issue.ID != id || !a.within(binding, result.Issue) {
		return linearIssue{}, LinearDiagnostic{"not-found-or-inaccessible", "issue not found or outside the configured Linear scope"}
	}
	return *result.Issue, nil
}

func (a *LinearWriteAdapter) inspectWith(ctx context.Context, binding linearBinding, intent WriteIntent, request func(context.Context, linearBinding, string, any, any) error, requireReplacementConfirmation bool) (WritePreflight, linearIssue, error) {
	var pre WritePreflight
	if err := a.validateIntent(intent, binding); err != nil {
		return pre, linearIssue{}, err
	}
	issue, err := a.readWriteIssue(ctx, binding, intent.Source, intent.Ref.ItemID, request)
	if err != nil {
		return pre, linearIssue{}, err
	}
	if issue.ArchivedAt != nil || issue.Trashed {
		return pre, linearIssue{}, LinearDiagnostic{"not-found-or-inaccessible", "archived or trashed Linear issues cannot be written"}
	}
	if version := linearWriteVersion(binding, issue); intent.Precondition == "" || version != intent.Precondition {
		return pre, linearIssue{}, LinearDiagnostic{"conflict", "Linear issue changed since write preview"}
	}
	if intent.Action == ActionAssignToMe && linearAssigneeID(issue) != intent.ExpectedAssignee {
		return pre, linearIssue{}, LinearDiagnostic{"conflict", "Linear assignee changed since write preview"}
	}
	var target linearWorkflowState
	if len(linearExpectedStateTypes(intent.Action)) > 0 {
		target, err = a.workflowState(ctx, binding, intent.Source, intent.Action, intent.Transition, request)
		if err != nil {
			return pre, linearIssue{}, err
		}
	}
	pre = WritePreflight{Capability: true, Authorized: true, InScope: true, Fresh: true, NativeAvailable: true, Owner: true, Precondition: intent.Precondition}
	if intent.Action == ActionStart || intent.Action == ActionResume {
		pre.Ready = issue.State != nil && issue.State.Type != "completed" && issue.State.Type != "duplicate" && issue.State.Type != "canceled"
		if intent.Action == ActionResume {
			pre.Ready = issue.State != nil && issue.State.Type == "started"
		}
	}
	if intent.Action == ActionComplete {
		pre.CompletionEvidence = target.Type == "completed"
	}
	if requireReplacementConfirmation && intent.Action == ActionAssignToMe && intent.ExpectedAssignee != "" && intent.ExpectedAssignee != binding.account && !intent.AssigneeReplacementConfirmed {
		return WritePreflight{}, linearIssue{}, LinearDiagnostic{"assignee-confirmation-required", "confirm replacing the existing Linear assignee in the write preview"}
	}
	return pre, issue, nil
}

func (a *LinearWriteAdapter) Inspect(ctx context.Context, intent WriteIntent) (WritePreflight, error) {
	if a == nil || a.LinearAdapter == nil {
		return WritePreflight{}, LinearDiagnostic{"invalid-source", "Linear read adapter required"}
	}
	binding, err := a.refreshBinding(ctx, intent.Source, nil)
	if err != nil {
		return WritePreflight{}, err
	}
	pre, _, err := a.inspectWith(ctx, binding, intent, func(ctx context.Context, b linearBinding, operation string, variables any, out any) error {
		return a.query(ctx, b, operation, variables, out)
	}, true)
	return pre, err
}

func (a *LinearWriteAdapter) Write(ctx context.Context, intent WriteIntent) (ProviderReceipt, error) {
	var receipt ProviderReceipt
	pre, err := a.Inspect(ctx, intent)
	if err != nil {
		return receipt, err
	}
	if !pre.Capability || !actionWriteEligible(intent.Action, pre) {
		return receipt, LinearDiagnostic{"conflict", "Linear write is no longer eligible"}
	}
	binding, err := a.binding(intent.Source)
	if err != nil {
		return receipt, err
	}
	gate := quotaScheduler(linearQuotaIdentity(binding, a.endpoint()), 1)
	value, err := gate.schedule(ctx, PriorityAction, "", "", false, func(workCtx context.Context) (any, error) {
		fresh, err := a.refreshBinding(workCtx, intent.Source, gate)
		if err != nil {
			return nil, err
		}
		if fresh.configGeneration != intent.SourceGeneration {
			return nil, LinearDiagnostic{"source-changed", "Linear source binding changed before dispatch"}
		}
		request := func(ctx context.Context, b linearBinding, operation string, variables any, out any) error {
			data, err := a.request(ctx, b, operation, variables, gate)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, out); err != nil {
				return LinearDiagnostic{"invalid-response", "Linear data invalid"}
			}
			return nil
		}
		current, _, err := a.inspectWith(workCtx, fresh, intent, request, true)
		if err != nil {
			return nil, err
		}
		if !current.Capability || !actionWriteEligible(intent.Action, current) {
			return nil, LinearDiagnostic{"conflict", "Linear write is no longer eligible"}
		}
		variables := map[string]any{"id": intent.Ref.ItemID}
		operation := ""
		switch intent.Action {
		case ActionStart, ActionResume, ActionReportBlocked, ActionRequestReview, ActionComplete, ActionReopen:
			operation = linearIssueUpdateStateMutation
			variables["stateId"] = intent.Transition
		case ActionAssignToMe:
			operation = linearIssueUpdateAssigneeMutation
			variables["assigneeId"] = fresh.account
		case ActionRecordProgress:
			operation = linearCommentCreateMutation
			operationBody := intent.Append + "\n\n<!-- " + intent.Marker + " -->"
			variables = map[string]any{"issueId": intent.Ref.ItemID, "body": operationBody}
		default:
			return nil, LinearDiagnostic{"unsupported-action", "unsupported Linear mutation"}
		}
		data, err := a.request(workCtx, fresh, operation, variables, gate)
		if err != nil {
			return nil, LinearDiagnostic{"provider-outcome-unknown", "Linear write outcome unknown; recovery required"}
		}
		if intent.Action == ActionRecordProgress {
			var result struct {
				CommentCreate *struct {
					Success bool           `json:"success"`
					Comment *linearComment `json:"comment"`
				} `json:"commentCreate"`
			}
			if json.Unmarshal(data, &result) != nil || result.CommentCreate == nil || !result.CommentCreate.Success || result.CommentCreate.Comment == nil || result.CommentCreate.Comment.ID == "" || result.CommentCreate.Comment.Body != intent.Append+"\n\n<!-- "+intent.Marker+" -->" || result.CommentCreate.Comment.User == nil || result.CommentCreate.Comment.User.ID != fresh.account {
				return nil, LinearDiagnostic{"provider-outcome-unknown", "Linear comment receipt could not be verified; recovery required"}
			}
			return ProviderReceipt{SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID, ID: result.CommentCreate.Comment.ID, Actor: fresh.account}, nil
		}
		var result struct {
			IssueUpdate *struct {
				Success bool `json:"success"`
				Issue   *struct {
					ID string `json:"id"`
				} `json:"issue"`
			} `json:"issueUpdate"`
		}
		if json.Unmarshal(data, &result) != nil || result.IssueUpdate == nil || !result.IssueUpdate.Success || result.IssueUpdate.Issue == nil || result.IssueUpdate.Issue.ID != intent.Ref.ItemID {
			return nil, LinearDiagnostic{"provider-outcome-unknown", "Linear issue receipt could not be verified; recovery required"}
		}
		return ProviderReceipt{SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID, ID: intent.Ref.ItemID}, nil
	})
	if err != nil {
		return receipt, err
	}
	return value.(ProviderReceipt), nil
}

func (a *LinearWriteAdapter) ReadReceipt(ctx context.Context, intent WriteIntent, receipt *ProviderReceipt) (ReceiptObservation, error) {
	observed := ReceiptObservation{Outcome: WriteUnknown, SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID, Precondition: intent.Precondition, Patch: map[string]string{}}
	if a == nil || a.LinearAdapter == nil {
		return observed, LinearDiagnostic{"invalid-source", "Linear read adapter required"}
	}
	binding, err := a.binding(intent.Source)
	if err != nil {
		return observed, err
	}
	if err := a.validateIntent(intent, binding); err != nil {
		return observed, err
	}
	issue, err := a.readWriteIssue(ctx, binding, intent.Source, intent.Ref.ItemID, func(ctx context.Context, b linearBinding, operation string, variables any, out any) error {
		return a.query(ctx, b, operation, variables, out)
	})
	if err != nil {
		return observed, err
	}
	switch intent.Action {
	case ActionStart, ActionResume, ActionReportBlocked, ActionRequestReview, ActionComplete, ActionReopen:
		observed.Patch["stateId"] = linearStateID(issue)
		observed.ReceiptID = issue.ID
	case ActionAssignToMe:
		observed.Patch["assigneeId"] = linearAssigneeID(issue)
		observed.ReceiptID = issue.ID
	case ActionRecordProgress:
		observed.Patch["append"] = "comment"
		comments, err := a.readAllComments(ctx, binding, intent.Source, intent.Ref.ItemID)
		if err != nil {
			return observed, err
		}
		for _, comment := range comments {
			observed.MarkerCount += strings.Count(comment.Body, intent.Marker)
			if comment.Body == intent.Append+"\n\n<!-- "+intent.Marker+" -->" && comment.User != nil && comment.User.ID == intent.Principal && (receipt == nil || receipt.ID == comment.ID) {
				observed.AppendProof = true
				observed.AppendContent = intent.Append
				observed.Actor = comment.User.ID
				observed.ReceiptID = comment.ID
			}
		}
	default:
		return observed, LinearDiagnostic{"unsupported-action", "unsupported Linear mutation"}
	}
	observed.Outcome = WriteVerified
	return observed, nil
}

func (a *LinearWriteAdapter) readAllComments(ctx context.Context, binding linearBinding, source Source, issueID string) ([]linearComment, error) {
	const pageSize = 50
	const maxPages = 100
	var comments []linearComment
	var after any
	seen := make(map[string]bool)
	for page := 0; page < maxPages; page++ {
		var result struct {
			Issue *struct {
				ID       string
				Team     *struct{ ID string }
				Project  *struct{ ID string }
				Comments struct {
					Nodes    []linearComment
					PageInfo linearPageInfo
				}
			}
		}
		if err := a.query(ctx, binding, linearIssueCommentsQuery, map[string]any{"id": issueID, "after": after, "count": pageSize}, &result); err != nil {
			return nil, err
		}
		if result.Issue == nil || result.Issue.ID != issueID || result.Issue.Team == nil || result.Issue.Team.ID != binding.team || (binding.project != "" && (result.Issue.Project == nil || result.Issue.Project.ID != binding.project)) {
			return nil, LinearDiagnostic{"not-found-or-inaccessible", "Linear issue comments are unavailable in the configured scope"}
		}
		comments = append(comments, result.Issue.Comments.Nodes...)
		if !result.Issue.Comments.PageInfo.HasNextPage {
			return comments, nil
		}
		cursor := result.Issue.Comments.PageInfo.EndCursor
		if cursor == "" || seen[cursor] {
			return nil, LinearDiagnostic{"invalid-response", "Linear comment pagination cursor missing or repeated"}
		}
		seen[cursor] = true
		after = cursor
	}
	return nil, LinearDiagnostic{"budget-exceeded", "Linear comment history exceeds the bounded recovery scan"}
}

var _ WriteAdapter = (*LinearWriteAdapter)(nil)
