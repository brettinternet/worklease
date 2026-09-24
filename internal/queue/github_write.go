package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// GitHubWriteAdapter is an explicit, interactive-only write surface layered on
// the read adapter. Interactive must be enabled only after a user confirms the
// operation; there is intentionally no unattended write mode.
type GitHubWriteAdapter struct {
	*GitHubAdapter
	Interactive bool
}

type GitHubWritePreview struct {
	Operation               string `json:"operation"`
	Marker                  string `json:"marker,omitempty"`
	ConditionalBodyWrite    bool   `json:"conditionalBodyWrite"`
	PreservesOtherAssignees bool   `json:"preservesOtherAssignees,omitempty"`
}

const githubIssueCommentQuery = `query($owner:String!,$repo:String!,$number:Int!,$after:String,$count:Int!) { rateLimit { remaining resetAt } repository(owner:$owner,name:$repo) { nameWithOwner issue(number:$number) { number repository { nameWithOwner } comments(first:$count,after:$after) { nodes { id body createdAt author { login } } pageInfo { hasNextPage endCursor } } } } }`

func NewGitHubWriteAdapter(read *GitHubAdapter, interactive bool) *GitHubWriteAdapter {
	return &GitHubWriteAdapter{GitHubAdapter: read, Interactive: interactive}
}

func (a *GitHubWriteAdapter) interactiveAllowed() error {
	if a == nil || a.GitHubAdapter == nil {
		return GitHubDiagnostic{"invalid-source", "GitHub read adapter required"}
	}
	if !a.Interactive {
		return GitHubDiagnostic{"interactive-required", "GitHub writes require an explicitly enabled interactive session"}
	}
	return nil
}

// Prepare fills the provider-specific minimal patch, marker, and fresh issue
// precondition. It never mutates GitHub.
func (a *GitHubWriteAdapter) Prepare(ctx context.Context, intent WriteIntent) (WriteIntent, GitHubWritePreview, error) {
	var preview GitHubWritePreview
	if err := a.interactiveAllowed(); err != nil {
		return intent, preview, err
	}
	if !validOperationID(intent.OperationID) {
		return intent, preview, GitHubDiagnostic{"invalid-intent", "operation ID required"}
	}
	b, err := a.binding(intent.Source)
	if err != nil {
		return intent, preview, err
	}
	if intent.Principal != b.account {
		return intent, preview, GitHubDiagnostic{"authentication", "write principal does not match configured account"}
	}
	switch intent.Action {
	case ActionComplete:
		if len(intent.Patch) == 0 {
			intent.Patch = map[string]string{"state": "CLOSED", "stateReason": "COMPLETED"}
		}
	case ActionReopen:
		if len(intent.Patch) == 0 {
			intent.Patch = map[string]string{"state": "OPEN", "stateReason": "REOPENED"}
		}
	case ActionRecordProgress:
		if len(intent.Patch) == 0 {
			intent.Patch = map[string]string{"append": "comment"}
		}
		if intent.Marker == "" {
			intent.Marker = "worklease-op:" + intent.OperationID
		}
	case ActionAssignToMe:
		if len(intent.Patch) == 0 {
			intent.Patch = map[string]string{"assignee": b.account}
		}
	default:
		return intent, preview, GitHubDiagnostic{"no-workflow-mapping", "GitHub Issues has no supported mapping for this action"}
	}
	if err := validateGitHubWriteIntent(intent, b.account); err != nil {
		return intent, preview, err
	}
	if intent.Action != ActionRecordProgress && intent.Action != ActionAssignToMe {
		if err := a.ValidateTransition(ctx, intent.Source, intent.Action, intent.Transition); err != nil {
			return intent, preview, err
		}
	}
	issue, err := a.writeIssue(ctx, b, intent)
	if err != nil {
		return intent, preview, err
	}
	intent.Precondition = githubWriteVersion(issue)
	preview.ConditionalBodyWrite = false
	switch intent.Action {
	case ActionComplete:
		preview.Operation = "close-completed"
	case ActionReopen:
		preview.Operation = "reopen"
	case ActionRecordProgress:
		preview.Operation, preview.Marker = "append-comment", "<!-- "+intent.Marker+" -->"
	case ActionAssignToMe:
		preview.Operation = "add-assignee"
		preview.PreservesOtherAssignees = true
	}
	return intent, preview, nil
}

func validateGitHubWriteIntent(intent WriteIntent, account string) error {
	if !validOperationID(intent.OperationID) || intent.Source.Adapter != "github" || intent.Ref.SourceID != intent.Source.ID || intent.Ref.ItemID == "" || intent.Principal != account {
		return GitHubDiagnostic{"invalid-intent", "GitHub source, issue, and configured principal required"}
	}
	number, err := strconv.Atoi(intent.Ref.ItemID)
	if err != nil || number < 1 || strconv.Itoa(number) != intent.Ref.ItemID {
		return GitHubDiagnostic{"invalid-ref", "issue number is invalid"}
	}
	switch intent.Action {
	case ActionComplete:
		if intent.Transition != "closed" || !exactStringMap(intent.Patch, map[string]string{"state": "CLOSED", "stateReason": "COMPLETED"}) || intent.Append != "" || intent.Marker != "" {
			return GitHubDiagnostic{"invalid-intent", "completion must close the issue with completed reason only"}
		}
	case ActionReopen:
		if intent.Transition != "open" || !exactStringMap(intent.Patch, map[string]string{"state": "OPEN", "stateReason": "REOPENED"}) || intent.Append != "" || intent.Marker != "" {
			return GitHubDiagnostic{"invalid-intent", "reopen must open the issue with reopened reason only"}
		}
	case ActionRecordProgress:
		if len(intent.Patch) != 1 || intent.Patch["append"] != "comment" {
			return GitHubDiagnostic{"no-conditional-body-write", "issue body and checklist edits are unavailable"}
		}
		if strings.TrimSpace(intent.Append) == "" || strings.Contains(intent.Append, "worklease-op:") || intent.Marker != "worklease-op:"+intent.OperationID {
			return GitHubDiagnostic{"invalid-intent", "progress requires a nonempty comment and operation marker"}
		}
	case ActionAssignToMe:
		if !exactStringMap(intent.Patch, map[string]string{"assignee": account}) || intent.Append != "" || intent.Marker != "" {
			return GitHubDiagnostic{"invalid-intent", "assignment must add only the configured account"}
		}
	default:
		return GitHubDiagnostic{"no-workflow-mapping", "GitHub Issues has no supported mapping for this action"}
	}
	return nil
}

func exactStringMap(actual, expected map[string]string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for key, value := range expected {
		if actual[key] != value {
			return false
		}
	}
	return true
}

func (a *GitHubWriteAdapter) ValidateTransition(_ context.Context, source Source, action Action, transition string) error {
	if source.Adapter != "github" {
		return GitHubDiagnostic{"invalid-source", "GitHub source required"}
	}
	switch action {
	case ActionComplete:
		if transition == "closed" {
			return nil
		}
	case ActionReopen:
		if transition == "open" {
			return nil
		}
	}
	return GitHubDiagnostic{"no-workflow-mapping", "GitHub Issues supports only completion close and reopen state transitions"}
}

func (a *GitHubWriteAdapter) writeIssue(ctx context.Context, b *githubBinding, intent WriteIntent) (githubIssue, error) {
	parts := strings.Split(b.repository, "/")
	number, _ := strconv.Atoi(intent.Ref.ItemID)
	var result struct {
		Repository *struct {
			NameWithOwner string       `json:"nameWithOwner"`
			Issue         *githubIssue `json:"issue"`
		} `json:"repository"`
	}
	if err := a.query(ctx, b, githubDetailQuery, map[string]any{"owner": parts[0], "repo": parts[1], "number": number}, &result); err != nil {
		return githubIssue{}, err
	}
	if result.Repository == nil || result.Repository.Issue == nil {
		return githubIssue{}, GitHubDiagnostic{"not-found-or-inaccessible", "issue not found or access unavailable"}
	}
	issue := *result.Repository.Issue
	if result.Repository.NameWithOwner != b.repository || issue.Repository.NameWithOwner != b.repository || issue.Number != number {
		a.driftTo(b, issue.Repository.NameWithOwner)
		return githubIssue{}, GitHubDiagnostic{"identity-changed", "issue identity changed; claims unavailable until rebind"}
	}
	a.rememberNodeID(b, issue)
	return issue, nil
}

func githubWriteVersion(issue githubIssue) string {
	assignees := make([]string, 0, len(issue.Assignees.Nodes))
	for _, assignee := range issue.Assignees.Nodes {
		assignees = append(assignees, assignee.Login)
	}
	sort.Strings(assignees)
	data, _ := json.Marshal(struct {
		ID, Repository, State, StateReason, Title, Body string
		Number                                          int
		UpdatedAt                                       time.Time
		Assignees                                       []string
	}{issue.ID, issue.Repository.NameWithOwner, strings.ToUpper(issue.State), strings.ToUpper(issue.StateReason), issue.Title, issue.Body, issue.Number, issue.UpdatedAt, assignees})
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func (a *GitHubWriteAdapter) Inspect(ctx context.Context, intent WriteIntent) (WritePreflight, error) {
	pre, _, err := a.inspect(ctx, intent)
	return pre, err
}

func (a *GitHubWriteAdapter) inspect(ctx context.Context, intent WriteIntent) (WritePreflight, *githubBinding, error) {
	var pre WritePreflight
	if err := a.interactiveAllowed(); err != nil {
		return pre, nil, err
	}
	b, err := a.binding(intent.Source)
	if err != nil {
		return pre, nil, err
	}
	if err := validateGitHubWriteIntent(intent, b.account); err != nil {
		return pre, nil, err
	}
	if intent.Action != ActionRecordProgress && intent.Action != ActionAssignToMe {
		if err := a.ValidateTransition(ctx, intent.Source, intent.Action, intent.Transition); err != nil {
			return pre, nil, err
		}
	}
	viewer, err := a.freshViewer(ctx, b)
	if err != nil {
		return pre, nil, err
	}
	if viewer != b.account {
		return pre, nil, GitHubDiagnostic{"authentication", "authenticated principal does not match configured account"}
	}
	issue, err := a.writeIssue(ctx, b, intent)
	if err != nil {
		return pre, nil, err
	}
	version := githubWriteVersion(issue)
	if intent.Precondition == "" || version != intent.Precondition {
		return pre, nil, GitHubDiagnostic{"conflict", "issue changed since write preview"}
	}
	pre = WritePreflight{Capability: true, Authorized: true, InScope: true, Fresh: true, NativeAvailable: true, Owner: true, Precondition: version}
	if intent.Action == ActionComplete {
		// GitHub's explicit COMPLETED close reason is the declared completion
		// evidence. NOT_PLANNED and every other reason are never equivalent.
		pre.CompletionEvidence = intent.Patch["stateReason"] == "COMPLETED"
	}
	return pre, b, nil
}

func (a *GitHubWriteAdapter) Write(ctx context.Context, intent WriteIntent) (ProviderReceipt, error) {
	var empty ProviderReceipt
	pre, binding, err := a.inspect(ctx, intent)
	if err != nil {
		return empty, err
	}
	if !pre.Capability || !actionWriteEligible(intent.Action, pre) {
		return empty, GitHubDiagnostic{"conflict", "write is no longer eligible"}
	}
	gate := quotaScheduler(githubQuotaIdentity(binding.host, binding.account, a.APIBase), 1)
	value, err := gate.schedule(ctx, PriorityAction, "", "", false, func(workCtx context.Context) (any, error) {
		current, err := a.binding(intent.Source)
		if err != nil {
			return nil, err
		}
		if current != binding || current.generation != binding.generation {
			return nil, GitHubDiagnostic{"credential-changed", "GitHub credential changed during write preparation"}
		}
		viewer, err := a.freshViewerHTTP(workCtx, current, gate)
		if err != nil {
			return nil, err
		}
		if viewer != current.account {
			return nil, GitHubDiagnostic{"authentication", "authenticated principal does not match configured account"}
		}
		// Space the actual REST mutations, not their preceding viewer checks.
		if err := gate.mutationSlot(workCtx); err != nil {
			return nil, err
		}
		a.mu.Lock()
		latest := a.bindings[intent.Source.ID]
		if latest != current || latest == nil || latest.identityChanged || latest.repository != intent.Source.Locator {
			a.mu.Unlock()
			return nil, GitHubDiagnostic{"credential-changed", "GitHub credential changed before dispatch"}
		}
		receipt, dispatchErr := a.dispatch(workCtx, current, gate, intent, viewer)
		a.mu.Unlock()
		return receipt, dispatchErr
	})
	if err != nil {
		return empty, githubWriteScheduleError(err)
	}
	return value.(ProviderReceipt), nil
}

func githubWriteScheduleError(err error) error {
	if diagnostic, ok := err.(ScheduleDiagnostic); ok {
		if diagnostic.Code == "rate-limited" {
			return GitHubRateDiagnostic{GitHubDiagnostic{"rate-limited", "GitHub quota exhausted; retry later"}, diagnostic.RetryAt}
		}
		return GitHubDiagnostic{diagnostic.Code, "provider request capacity unavailable"}
	}
	return err
}

func (a *GitHubWriteAdapter) dispatch(ctx context.Context, b *githubBinding, gate *quotaQueue, intent WriteIntent, actor string) (ProviderReceipt, error) {
	var payload any
	var method, endpoint string
	parts := strings.Split(b.repository, "/")
	base := githubRESTBase(b.host, a.APIBase)
	issuePath := "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/issues/" + url.PathEscape(intent.Ref.ItemID)
	switch intent.Action {
	case ActionComplete:
		method, endpoint = http.MethodPatch, base+issuePath
		payload = map[string]string{"state": "closed", "state_reason": "completed"}
	case ActionReopen:
		method, endpoint = http.MethodPatch, base+issuePath
		payload = map[string]string{"state": "open", "state_reason": "reopened"}
	case ActionRecordProgress:
		method, endpoint = http.MethodPost, base+issuePath+"/comments"
		payload = map[string]string{"body": intent.Append + "\n\n<!-- " + intent.Marker + " -->"}
	case ActionAssignToMe:
		method, endpoint = http.MethodPost, base+issuePath+"/assignees"
		payload = map[string][]string{"assignees": {b.account}}
	default:
		return ProviderReceipt{}, GitHubDiagnostic{"no-workflow-mapping", "GitHub Issues has no supported mapping for this action"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ProviderReceipt{}, GitHubDiagnostic{"invalid-intent", "GitHub write payload invalid"}
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return ProviderReceipt{}, GitHubDiagnostic{"invalid-source", "invalid GitHub API endpoint"}
	}
	request.Header.Set("Authorization", "Bearer "+b.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/vnd.github+json")
	response, err := a.client().Do(request) // A mutation request is never retried.
	if err != nil {
		return ProviderReceipt{}, GitHubDiagnostic{"offline", "GitHub write outcome unknown; recovery required"}
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 8<<20+1))
	response.Body.Close()
	a.observeWriteRate(gate, response, raw)
	if readErr != nil || len(raw) > 8<<20 {
		return ProviderReceipt{}, GitHubDiagnostic{"invalid-response", "GitHub write response unreadable; recovery required"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ProviderReceipt{}, githubWriteHTTPError(response, raw, gate)
	}
	receipt := ProviderReceipt{SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID}
	if intent.Action == ActionRecordProgress {
		var comment struct {
			ID   string `json:"node_id"`
			Body string `json:"body"`
			User *struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if err := json.Unmarshal(raw, &comment); err != nil || comment.Body != intent.Append+"\n\n<!-- "+intent.Marker+" -->" || comment.User == nil || comment.User.Login != actor {
			return ProviderReceipt{}, GitHubDiagnostic{"invalid-response", "GitHub comment receipt could not be verified; recovery required"}
		}
		receipt.ID, receipt.Actor = comment.ID, actor
	}
	return receipt, nil
}

func githubRESTBase(host, apiBase string) string {
	if apiBase != "" {
		return strings.TrimRight(apiBase, "/")
	}
	if strings.EqualFold(host, "github.com") {
		return "https://api.github.com"
	}
	return "https://" + host + "/api/v3"
}

func (a *GitHubWriteAdapter) freshViewer(ctx context.Context, b *githubBinding) (string, error) {
	gate := quotaScheduler(githubQuotaIdentity(b.host, b.account, a.APIBase), 1)
	value, err := gate.schedule(ctx, PriorityAction, "", "", false, func(workCtx context.Context) (any, error) {
		return a.queryHTTP(workCtx, b, githubGraphQLBody(githubViewerQuery), gate)
	})
	if err != nil {
		return "", githubWriteScheduleError(err)
	}
	return decodeGitHubViewer(value.([]byte))
}

func (a *GitHubWriteAdapter) freshViewerHTTP(ctx context.Context, b *githubBinding, gate *quotaQueue) (string, error) {
	data, err := a.queryHTTP(ctx, b, githubGraphQLBody(githubViewerQuery), gate)
	if err != nil {
		return "", err
	}
	return decodeGitHubViewer(data)
}

func githubGraphQLBody(query string) []byte {
	body, _ := json.Marshal(struct {
		Query string `json:"query"`
	}{query})
	return body
}

func decodeGitHubViewer(data []byte) (string, error) {
	var result struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	if err := json.Unmarshal(data, &result); err != nil || result.Viewer.Login == "" {
		return "", GitHubDiagnostic{"invalid-response", "GitHub viewer response invalid"}
	}
	return result.Viewer.Login, nil
}

func (a *GitHubWriteAdapter) observeWriteRate(gate *quotaQueue, response *http.Response, raw []byte) {
	remaining := response.Header.Get("X-RateLimit-Remaining")
	limited := response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden && (response.Header.Get("Retry-After") != "" || remaining == "0" || strings.Contains(strings.ToLower(string(raw)), "rate limit"))
	if remaining != "0" && !limited {
		return
	}
	until := time.Now().Add(time.Second)
	if value := response.Header.Get("Retry-After"); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
			until = time.Now().Add(time.Duration(seconds) * time.Second)
		} else if date, err := http.ParseTime(value); err == nil && date.After(until) {
			until = date
		}
	}
	if reset, err := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && remaining == "0" {
		if deadline := time.Unix(reset, 0); deadline.After(until) {
			until = deadline
		}
	}
	gate.limitUntil(until)
}

func githubWriteHTTPError(response *http.Response, raw []byte, gate *quotaQueue) error {
	lower := strings.ToLower(string(raw))
	limited := response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusForbidden && (response.Header.Get("Retry-After") != "" || response.Header.Get("X-RateLimit-Remaining") == "0" || strings.Contains(lower, "rate limit"))
	if limited {
		return GitHubRateDiagnostic{GitHubDiagnostic{"rate-limited", "GitHub quota exhausted; write outcome unknown; recovery required"}, gate.retryDeadline()}
	}
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return GitHubDiagnostic{"authentication", "GitHub rejected the credential; write outcome unknown"}
	case http.StatusForbidden:
		if response.Header.Get("X-GitHub-SSO") != "" || strings.Contains(lower, "saml") {
			return GitHubDiagnostic{"saml-sso", "SAML authorization required; write outcome unknown"}
		}
		return GitHubDiagnostic{"permission-denied", "GitHub denied the write; outcome unknown"}
	case http.StatusNotFound:
		return GitHubDiagnostic{"not-found-or-inaccessible", "issue not found or access unavailable; write outcome unknown"}
	default:
		return GitHubDiagnostic{"provider-error", fmt.Sprintf("GitHub write failed with HTTP %d; outcome unknown", response.StatusCode)}
	}
}

func (a *GitHubWriteAdapter) ReadReceipt(ctx context.Context, intent WriteIntent, receipt *ProviderReceipt) (ReceiptObservation, error) {
	observed := ReceiptObservation{Outcome: WriteUnknown, SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID, Precondition: intent.Precondition, Patch: map[string]string{}}
	// Recovery only reads provider evidence; it must work after the interactive
	// session that dispatched the write has ended.
	if a == nil || a.GitHubAdapter == nil {
		return observed, GitHubDiagnostic{"invalid-source", "GitHub read adapter required"}
	}
	b, err := a.binding(intent.Source)
	if err != nil {
		return observed, err
	}
	if err := validateGitHubWriteIntent(intent, b.account); err != nil {
		return observed, err
	}
	issue, err := a.writeIssue(ctx, b, intent)
	if err != nil {
		return observed, err
	}
	switch intent.Action {
	case ActionComplete, ActionReopen:
		observed.Patch["state"] = strings.ToUpper(issue.State)
		observed.Patch["stateReason"] = strings.ToUpper(issue.StateReason)
		if observed.Patch["state"] != intent.Patch["state"] || observed.Patch["stateReason"] != intent.Patch["stateReason"] {
			// Read-back can lag a committed write; do not close recovery yet.
			return observed, nil
		}
	case ActionAssignToMe:
		found := false
		for _, assignee := range issue.Assignees.Nodes {
			if assignee.Login == b.account {
				found = true
				break
			}
		}
		if !found {
			return observed, nil
		}
		observed.Patch["assignee"] = b.account
	case ActionRecordProgress:
		observed.Patch["append"] = "comment"
		comments, err := a.readAllWriteComments(ctx, b, intent)
		if err != nil {
			return observed, err
		}
		expected := intent.Append + "\n\n<!-- " + intent.Marker + " -->"
		matches := 0
		for _, comment := range comments {
			observed.MarkerCount += strings.Count(comment.Body, intent.Marker)
			if comment.Body == expected {
				matches++
				observed.ReceiptID = comment.ID
				observed.Actor = comment.Author
			}
		}
		if observed.MarkerCount == 1 && matches == 1 && observed.Actor == intent.Principal {
			observed.AppendContent = intent.Append
			observed.AppendProof = true
			observed.OperationID = intent.OperationID
		}
	default:
		return observed, GitHubDiagnostic{"no-workflow-mapping", "GitHub Issues has no supported mapping for this action"}
	}
	if receipt != nil && receipt.Actor != "" && observed.Actor != "" && receipt.Actor != observed.Actor {
		observed.Outcome = WriteConflict
		return observed, nil
	}
	observed.Outcome = WriteVerified
	return observed, nil
}

func (a *GitHubWriteAdapter) readAllWriteComments(ctx context.Context, b *githubBinding, intent WriteIntent) ([]GitHubComment, error) {
	parts := strings.Split(b.repository, "/")
	number, _ := strconv.Atoi(intent.Ref.ItemID)
	var cursor string
	var all []GitHubComment
	const maxEvidenceBytes = 8 << 20
	totalBytes := 0
	for page := 0; page < 100; page++ {
		var result struct {
			Repository *struct {
				NameWithOwner string `json:"nameWithOwner"`
				Issue         *struct {
					Number     int `json:"number"`
					Repository struct {
						NameWithOwner string `json:"nameWithOwner"`
					} `json:"repository"`
					Comments struct {
						Nodes []struct {
							ID        string    `json:"id"`
							Body      string    `json:"body"`
							CreatedAt time.Time `json:"createdAt"`
							Author    *struct {
								Login string `json:"login"`
							} `json:"author"`
						} `json:"nodes"`
						PageInfo struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
					} `json:"comments"`
				} `json:"issue"`
			} `json:"repository"`
		}
		var after any
		if cursor != "" {
			after = cursor
		}
		if err := a.query(ctx, b, githubIssueCommentQuery, map[string]any{"owner": parts[0], "repo": parts[1], "number": number, "after": after, "count": 100}, &result); err != nil {
			return nil, err
		}
		if result.Repository == nil || result.Repository.Issue == nil || result.Repository.NameWithOwner != b.repository || result.Repository.Issue.Repository.NameWithOwner != b.repository || result.Repository.Issue.Number != number {
			return nil, GitHubDiagnostic{"not-found-or-inaccessible", "issue comments unavailable"}
		}
		for _, node := range result.Repository.Issue.Comments.Nodes {
			totalBytes += len(node.ID) + len(node.Body)
			if node.Author != nil {
				totalBytes += len(node.Author.Login)
			}
			if totalBytes > maxEvidenceBytes {
				return nil, GitHubDiagnostic{"incomplete", "comment provenance exceeds the verification limit"}
			}
			comment := GitHubComment{ID: node.ID, Body: node.Body, CreatedAt: node.CreatedAt}
			if node.Author != nil {
				comment.Author = node.Author.Login
			}
			all = append(all, comment)
		}
		if !result.Repository.Issue.Comments.PageInfo.HasNextPage {
			return all, nil
		}
		cursor = result.Repository.Issue.Comments.PageInfo.EndCursor
		if cursor == "" {
			return nil, GitHubDiagnostic{"invalid-response", "comment pagination cursor missing"}
		}
	}
	return nil, GitHubDiagnostic{"incomplete", "comment provenance exceeds the verification limit"}
}

var _ WriteAdapter = (*GitHubWriteAdapter)(nil)
