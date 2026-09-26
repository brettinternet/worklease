package queue

import (
	"context"
	"errors"
	"strings"
	"time"
)

type githubProjectBinding struct {
	Owner       string            `json:"owner"`
	Number      int               `json:"number"`
	ID          string            `json:"id"`
	FieldID     string            `json:"fieldId"`
	Options     map[string]string `json:"options"`
	AllowWrites bool              `json:"allowWrites"`
}

type githubProjectItem struct {
	itemID     string
	issueID    string
	number     int
	repository string
	fieldID    string
	optionID   string
	name       string
	updatedAt  time.Time
}

type githubProjectStatus struct {
	itemID   string
	fieldID  string
	optionID string
	name     string
	states   map[string]string
}

const githubProjectDiscoveryQuery = `query($projectID:ID!,$after:String,$count:Int!) { rateLimit { remaining resetAt } node(id:$projectID) { ... on ProjectV2 { id number owner { __typename ... on User { login } ... on Organization { login } } fields(first:$count,after:$after) { nodes { __typename ... on ProjectV2SingleSelectField { id name options { id name } } } pageInfo { hasNextPage endCursor } } } } }`
const githubProjectItemsQuery = `query($projectID:ID!,$after:String,$count:Int!,$fieldName:String!) { rateLimit { remaining resetAt } node(id:$projectID) { ... on ProjectV2 { id number owner { __typename ... on User { login } ... on Organization { login } } items(first:$count,after:$after) { totalCount nodes { id content { __typename ... on Issue { id number repository { nameWithOwner } } } fieldValueByName(name:$fieldName) { ... on ProjectV2ItemFieldSingleSelectValue { name optionId field { ... on ProjectV2SingleSelectField { id } } } } } pageInfo { hasNextPage endCursor } } } } }`
const githubProjectItemQuery = `query($itemID:ID!,$fieldName:String!) { rateLimit { remaining resetAt } node(id:$itemID) { ... on ProjectV2Item { id updatedAt project { id number owner { __typename ... on User { login } ... on Organization { login } } } content { __typename ... on Issue { id number repository { nameWithOwner } } } fieldValueByName(name:$fieldName) { ... on ProjectV2ItemFieldSingleSelectValue { name optionId field { ... on ProjectV2SingleSelectField { id } } } } } } }`

func validGitHubProjectBinding(project githubProjectBinding) bool {
	if project.Owner == "" || project.Owner == "." || project.Owner == ".." || strings.ContainsAny(project.Owner, " /\\:@?#\t\r\n") || project.Number < 1 || !strings.HasPrefix(project.ID, "PVT_") || !strings.HasPrefix(project.FieldID, "PVTSSF_") || len(project.Options) == 0 {
		return false
	}
	for id, state := range project.Options {
		if strings.TrimSpace(id) != id || id == "" || !map[string]bool{"open": true, "in-progress": true, "blocked": true, "review": true, "complete": true}[state] {
			return false
		}
	}
	return true
}

func applyGitHubProjectStatus(summary *Summary, project githubProjectStatus, terminal bool) {
	summary.ProjectStatusBound = true
	summary.ProjectStatusKnown = true
	summary.ProjectStatusState = ""
	summary.ProjectStatusReason = ""
	summary.ProjectFieldID = project.fieldID
	summary.ProjectItemID = project.itemID
	summary.ProjectOptionID = project.optionID
	summary.ProjectStatusRaw = project.name
	if project.itemID == "" {
		summary.ProjectStatusReason = "project-item-missing"
	} else if project.optionID == "" {
		summary.ProjectStatusReason = "project-status-unset"
	} else if state, mapped := project.states[project.optionID]; mapped {
		summary.ProjectStatusState = state
	} else {
		summary.ProjectStatusState = "unknown"
		summary.ProjectStatusReason = "project-option-unmapped"
	}
	if summary.ProjectStatusState == "" {
		summary.ProjectStatusState = "unknown"
	}
	if summary.ProjectStatusState == "unknown" && summary.ProjectStatusReason == "" {
		summary.ProjectStatusReason = "project-item-missing"
	}
	summary.ProjectStatusConflict = terminal && summary.ProjectStatusState != "complete" && summary.ProjectStatusState != "unknown" || !terminal && summary.ProjectStatusState == "complete"
}

func (a *GitHubAdapter) ProjectStatusBound(source Source) bool {
	binding, err := a.binding(source)
	return err == nil && binding.project != nil
}

// RefreshProjectStatus obtains one complete, independent item scan for this refresh.
func (a *GitHubAdapter) RefreshProjectStatus(ctx context.Context, source Source) error {
	binding, err := a.binding(source)
	if err != nil {
		return err
	}
	if binding.project == nil {
		return nil
	}
	a.mu.Lock()
	binding.projectRefreshReady = false
	a.mu.Unlock()
	items, err := a.readProjectItems(ctx, binding, 100)
	if err != nil {
		return err
	}
	a.mu.Lock()
	binding.projectItems = items
	binding.projectScans = map[string]map[string]githubProjectItem{}
	binding.projectRefreshReady = true
	a.mu.Unlock()
	return nil
}

// ApplyProjectStatus refreshes only the project fields; it never changes issue state or identity.
func (a *GitHubAdapter) ApplyProjectStatus(source Source, item Item) Item {
	binding, err := a.binding(source)
	if err != nil || binding.project == nil {
		return item
	}
	a.mu.Lock()
	status := githubProjectStatus{states: binding.project.Options}
	if projectItem, ok := binding.projectItems[item.CanonicalID]; ok {
		status.itemID, status.fieldID, status.optionID, status.name = projectItem.itemID, projectItem.fieldID, projectItem.optionID, projectItem.name
	}
	a.mu.Unlock()
	applyGitHubProjectStatus(&item.Summary, status, item.Terminal)
	return item
}

func (a *GitHubAdapter) projectStatus(source Source, issue githubIssue) (githubProjectStatus, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	binding := a.bindings[source.ID]
	if binding == nil || binding.project == nil {
		return githubProjectStatus{}, false
	}
	status := githubProjectStatus{states: binding.project.Options}
	if item, ok := binding.projectItems[issue.ID]; ok {
		status.itemID, status.fieldID, status.optionID, status.name = item.itemID, item.fieldID, item.optionID, item.name
	}
	return status, true
}

func (a *GitHubAdapter) discoverProject(ctx context.Context, binding *githubBinding) error {
	project := binding.project
	if project == nil {
		return nil
	}
	var after any
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		var result struct {
			Node *struct {
				ID     string `json:"id"`
				Number int    `json:"number"`
				Owner  *struct {
					Login string `json:"login"`
				} `json:"owner"`
				Fields struct {
					Nodes []struct {
						TypeName string `json:"__typename"`
						ID       string `json:"id"`
						Name     string `json:"name"`
						Options  []struct {
							ID   string `json:"id"`
							Name string `json:"name"`
						} `json:"options"`
					} `json:"nodes"`
					PageInfo *struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"fields"`
			} `json:"node"`
		}
		if err := a.query(ctx, binding, githubProjectDiscoveryQuery, map[string]any{"projectID": project.ID, "after": after, "count": 100}, &result); err != nil {
			return githubProjectDiagnostic(err)
		}
		if result.Node == nil || result.Node.ID != project.ID || result.Node.Number != project.Number || result.Node.Owner == nil || !strings.EqualFold(result.Node.Owner.Login, project.Owner) {
			return GitHubDiagnostic{"project-binding-changed", "configured Projects v2 owner, number, or project ID no longer matches"}
		}
		for _, field := range result.Node.Fields.Nodes {
			if field.ID != project.FieldID {
				continue
			}
			if field.TypeName != "ProjectV2SingleSelectField" || field.Name == "" {
				return GitHubDiagnostic{"project-field-changed", "configured Projects v2 field is no longer a single-select field"}
			}
			optionNames := make(map[string]string, len(field.Options))
			for _, option := range field.Options {
				if option.ID == "" || option.Name == "" {
					return GitHubDiagnostic{"project-field-invalid", "Projects v2 field option identity is incomplete"}
				}
				optionNames[option.ID] = option.Name
			}
			for optionID := range project.Options {
				if _, ok := optionNames[optionID]; !ok {
					return GitHubDiagnostic{"project-option-missing", "a configured Projects v2 option is no longer available"}
				}
			}
			a.mu.Lock()
			binding.projectFieldName = field.Name
			binding.projectOptionNames = optionNames
			a.mu.Unlock()
			return nil
		}
		if result.Node.Fields.PageInfo == nil {
			return GitHubDiagnostic{"project-discovery-incomplete", "Projects v2 field discovery pagination was incomplete"}
		}
		if !result.Node.Fields.PageInfo.HasNextPage {
			return GitHubDiagnostic{"project-field-missing", "configured Projects v2 single-select field was not found"}
		}
		cursor := result.Node.Fields.PageInfo.EndCursor
		if cursor == "" || seen[cursor] {
			return GitHubDiagnostic{"project-discovery-incomplete", "Projects v2 field discovery pagination was incomplete"}
		}
		seen[cursor] = true
		after = cursor
	}
	return GitHubDiagnostic{"project-discovery-incomplete", "Projects v2 field discovery exceeded its page limit"}
}

func (a *GitHubAdapter) projectItemsForScan(ctx context.Context, binding *githubBinding, scanID string, count int) (map[string]githubProjectItem, error) {
	if binding.project == nil {
		return nil, nil
	}
	a.mu.Lock()
	if existing := binding.projectScans[scanID]; existing != nil {
		binding.projectItems = existing
		a.mu.Unlock()
		return existing, nil
	}
	if binding.projectRefreshReady {
		items := binding.projectItems
		if binding.projectScans == nil {
			binding.projectScans = map[string]map[string]githubProjectItem{}
		}
		binding.projectScans[scanID] = items
		binding.projectRefreshReady = false
		a.mu.Unlock()
		return items, nil
	}
	a.mu.Unlock()
	items, err := a.readProjectItems(ctx, binding, count)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if binding.projectScans == nil {
		binding.projectScans = map[string]map[string]githubProjectItem{}
	}
	binding.projectScans[scanID] = items
	a.mu.Unlock()
	return items, nil
}

func (a *GitHubAdapter) readProjectItems(ctx context.Context, binding *githubBinding, count int) (map[string]githubProjectItem, error) {
	if err := a.discoverProject(ctx, binding); err != nil {
		return nil, err
	}
	if count < 1 || count > 100 {
		count = 100
	}
	a.mu.Lock()
	fieldName := binding.projectFieldName
	a.mu.Unlock()
	if fieldName == "" {
		return nil, GitHubDiagnostic{"project-field-missing", "configured Projects v2 single-select field was not found"}
	}
	items := map[string]githubProjectItem{}
	var after any
	seen := map[string]bool{}
	for page := 0; page < 100; page++ {
		var result struct {
			Node *struct {
				ID     string `json:"id"`
				Number int    `json:"number"`
				Owner  *struct {
					Login string `json:"login"`
				} `json:"owner"`
				Items struct {
					Nodes []struct {
						ID      string `json:"id"`
						Content *struct {
							TypeName   string `json:"__typename"`
							ID         string `json:"id"`
							Number     int    `json:"number"`
							Repository struct {
								NameWithOwner string `json:"nameWithOwner"`
							} `json:"repository"`
						} `json:"content"`
						FieldValue *struct {
							Name     string `json:"name"`
							OptionID string `json:"optionId"`
							Field    *struct {
								ID string `json:"id"`
							} `json:"field"`
						} `json:"fieldValueByName"`
					} `json:"nodes"`
					PageInfo *struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
				} `json:"items"`
			} `json:"node"`
		}
		if err := a.query(ctx, binding, githubProjectItemsQuery, map[string]any{"projectID": binding.project.ID, "after": after, "count": count, "fieldName": fieldName}, &result); err != nil {
			return nil, githubProjectDiagnostic(err)
		}
		project := result.Node
		if project == nil || project.ID != binding.project.ID || project.Number != binding.project.Number || project.Owner == nil || !strings.EqualFold(project.Owner.Login, binding.project.Owner) {
			return nil, GitHubDiagnostic{"project-binding-changed", "configured Projects v2 owner, number, or project ID no longer matches"}
		}
		for _, node := range project.Items.Nodes {
			if node.Content == nil || node.Content.TypeName != "Issue" || !strings.EqualFold(node.Content.Repository.NameWithOwner, binding.repository) {
				continue
			}
			if node.ID == "" || node.Content.ID == "" || node.Content.Number < 1 {
				return nil, GitHubDiagnostic{"project-item-invalid", "Projects v2 item identity is incomplete"}
			}
			item := githubProjectItem{itemID: node.ID, issueID: node.Content.ID, number: node.Content.Number, repository: node.Content.Repository.NameWithOwner}
			if node.FieldValue != nil {
				if node.FieldValue.Field == nil || node.FieldValue.Field.ID != binding.project.FieldID {
					return nil, GitHubDiagnostic{"project-field-changed", "configured Projects v2 field no longer matches the item value"}
				}
				item.fieldID = node.FieldValue.Field.ID
				item.optionID = node.FieldValue.OptionID
				item.name = node.FieldValue.Name
			}
			if _, exists := items[item.issueID]; exists {
				return nil, GitHubDiagnostic{"project-item-ambiguous", "the bound Projects v2 project contains duplicate items for an issue"}
			}
			items[item.issueID] = item
		}
		if project.Items.PageInfo == nil {
			return nil, GitHubDiagnostic{"project-pagination-incomplete", "Projects v2 item pagination did not complete"}
		}
		if !project.Items.PageInfo.HasNextPage {
			a.mu.Lock()
			binding.projectItems = items
			a.mu.Unlock()
			return items, nil
		}
		cursor := project.Items.PageInfo.EndCursor
		if cursor == "" || seen[cursor] {
			return nil, GitHubDiagnostic{"project-pagination-incomplete", "Projects v2 item pagination did not complete"}
		}
		seen[cursor] = true
		after = cursor
	}
	return nil, GitHubDiagnostic{"project-pagination-incomplete", "Projects v2 item pagination exceeded its page limit"}
}

func githubProjectDiagnostic(err error) error {
	var rate GitHubRateDiagnostic
	if errors.As(err, &rate) {
		return rate
	}
	var diagnostic GitHubDiagnostic
	if errors.As(err, &diagnostic) {
		switch diagnostic.Code {
		case "permission-denied":
			return GitHubDiagnostic{"project-permission-denied", "Projects v2 access denied; confirm read:project scope"}
		case "not-found-or-inaccessible":
			return GitHubDiagnostic{"project-not-found-or-inaccessible", "configured Projects v2 project not found or access unavailable"}
		}
	}
	return GitHubDiagnostic{"project-read-failed", "Projects v2 status could not be refreshed"}
}

func (a *GitHubAdapter) readProjectItem(ctx context.Context, binding *githubBinding, itemID string) (githubProjectItem, error) {
	if err := a.discoverProject(ctx, binding); err != nil {
		return githubProjectItem{}, err
	}
	a.mu.Lock()
	fieldName := binding.projectFieldName
	a.mu.Unlock()
	var result struct {
		Node *struct {
			ID        string    `json:"id"`
			UpdatedAt time.Time `json:"updatedAt"`
			Project   *struct {
				ID     string `json:"id"`
				Number int    `json:"number"`
				Owner  *struct {
					Login string `json:"login"`
				} `json:"owner"`
			} `json:"project"`
			Content *struct {
				TypeName   string `json:"__typename"`
				ID         string `json:"id"`
				Number     int    `json:"number"`
				Repository struct {
					NameWithOwner string `json:"nameWithOwner"`
				} `json:"repository"`
			} `json:"content"`
			FieldValue *struct {
				Name     string `json:"name"`
				OptionID string `json:"optionId"`
				Field    *struct {
					ID string `json:"id"`
				} `json:"field"`
			} `json:"fieldValueByName"`
		} `json:"node"`
	}
	if err := a.query(ctx, binding, githubProjectItemQuery, map[string]any{"itemID": itemID, "fieldName": fieldName}, &result); err != nil {
		return githubProjectItem{}, githubProjectDiagnostic(err)
	}
	node := result.Node
	if node == nil || node.ID != itemID || node.UpdatedAt.IsZero() || node.Project == nil || node.Project.ID != binding.project.ID || node.Project.Number != binding.project.Number || node.Project.Owner == nil || !strings.EqualFold(node.Project.Owner.Login, binding.project.Owner) || node.Content == nil || node.Content.TypeName != "Issue" || !strings.EqualFold(node.Content.Repository.NameWithOwner, binding.repository) {
		return githubProjectItem{}, GitHubDiagnostic{"project-item-changed", "exact Projects v2 item no longer matches the configured issue"}
	}
	item := githubProjectItem{itemID: node.ID, issueID: node.Content.ID, number: node.Content.Number, repository: node.Content.Repository.NameWithOwner, updatedAt: node.UpdatedAt}
	if node.FieldValue != nil {
		if node.FieldValue.Field == nil || node.FieldValue.Field.ID != binding.project.FieldID {
			return githubProjectItem{}, GitHubDiagnostic{"project-field-changed", "configured Projects v2 field no longer matches the item value"}
		}
		item.fieldID, item.optionID, item.name = node.FieldValue.Field.ID, node.FieldValue.OptionID, node.FieldValue.Name
	}
	return item, nil
}

func githubProjectWriteVersion(issue githubIssue, item githubProjectItem) string {
	return githubWriteVersion(issue) + ":" + item.itemID + ":" + item.fieldID + ":" + item.optionID + ":" + item.updatedAt.UTC().Format(time.RFC3339Nano)
}

func githubProjectItemByIssue(ctx context.Context, adapter *GitHubAdapter, binding *githubBinding, issue githubIssue) (githubProjectItem, error) {
	items, err := adapter.readProjectItems(ctx, binding, 100)
	if err != nil {
		return githubProjectItem{}, err
	}
	item, ok := items[issue.ID]
	if !ok || item.number != issue.Number {
		return githubProjectItem{}, GitHubDiagnostic{"project-item-missing", "issue has no item in the bound Projects v2 project"}
	}
	exact, err := adapter.readProjectItem(ctx, binding, item.itemID)
	if err != nil {
		return githubProjectItem{}, err
	}
	if exact.issueID != issue.ID || exact.number != issue.Number {
		return githubProjectItem{}, GitHubDiagnostic{"project-item-changed", "exact Projects v2 item no longer matches the configured issue"}
	}
	return exact, nil
}

func isGitHubProjectStatusAction(action Action) bool {
	return githubProjectStateForAction(action) != ""
}

func githubProjectStateForAction(action Action) string {
	switch action {
	case ActionStart, ActionResume:
		return "in-progress"
	case ActionReportBlocked:
		return "blocked"
	case ActionRequestReview:
		return "review"
	default:
		return ""
	}
}
