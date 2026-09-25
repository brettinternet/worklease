package queue

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// LinearAdapter exposes read-only Linear observations. Neither assignment nor
// workflow state confers a Worklease claim. APIBase is a local test seam.
type LinearAdapter struct {
	Client   *http.Client
	APIBase  string
	Helper   *CredentialHelper
	mu       sync.Mutex
	bindings map[string]linearBinding
}

type linearBinding struct {
	organization, team, project, account, token, generation string
	argv                                                    []string
}

type LinearDiagnostic struct{ Code, Detail string }

func (d LinearDiagnostic) Error() string { return d.Code + ": " + d.Detail }
func NewLinearAdapter() *LinearAdapter {
	return &LinearAdapter{Helper: new(CredentialHelper), bindings: make(map[string]linearBinding)}
}
func (a *LinearAdapter) QueueCacheIdentity(Source) (string, string, string, bool) {
	return "", "", "", false
}
func (a *LinearAdapter) ConfigurationGeneration(source Source) string {
	b, err := a.binding(source)
	if err != nil {
		return ""
	}
	return b.generation
}
func (a *LinearAdapter) binding(source Source) (linearBinding, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, ok := a.bindings[source.ID]
	if !ok || source.Adapter != "linear" || source.Locator != b.organization {
		return linearBinding{}, LinearDiagnostic{"invalid-source", "resolve the configured Linear source first"}
	}
	return b, nil
}
func (a *LinearAdapter) endpoint() string {
	if a.APIBase != "" {
		return a.APIBase
	}
	return "https://api.linear.app/graphql"
}
func (a *LinearAdapter) client() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

const linearIdentityQuery = `query($team:String!) { viewer { id organization { id } } team(id:$team) { id name } }`
const linearListQuery = `query($team:String!,$after:String,$count:Int!) { team(id:$team) { id issues(first:$count,after:$after,includeArchived:true,orderBy:updatedAt) { nodes { id identifier title updatedAt archivedAt trashed team { id } project { id } state { id name type } assignee { id } } pageInfo { hasNextPage endCursor } } } }`
const linearDetailQuery = `query($id:String!) { issue(id:$id) { id identifier title description updatedAt archivedAt trashed team { id } project { id } state { id name type } assignee { id } } }`
const linearRelationsQuery = `query($id:String!,$after:String,$count:Int!) { issue(id:$id) { id team { id } project { id } parent { id team { id } project { id } } relations(first:$count,after:$after) { nodes { type relatedIssue { id team { id } project { id } state { type } } } pageInfo { hasNextPage endCursor } } } }`
const linearInverseQuery = `query($id:String!,$after:String,$count:Int!) { issue(id:$id) { id team { id } project { id } inverseRelations(first:$count,after:$after) { nodes { type issue { id team { id } project { id } state { type } } } pageInfo { hasNextPage endCursor } } } }`

// Linear publishes both request and complexity reset times as Unix milliseconds.
// Never issue another request after either quota reaches zero.
func linearLimitFromHeaders(gate *quotaQueue, headers http.Header, limited bool) {
	reset := time.Time{}
	for _, quota := range []string{"Requests", "Complexity"} {
		if headers.Get("X-RateLimit-"+quota+"-Remaining") != "0" && !limited {
			continue
		}
		ms, err := strconv.ParseInt(headers.Get("X-RateLimit-"+quota+"-Reset"), 10, 64)
		if err == nil && ms > 0 {
			deadline := time.UnixMilli(ms)
			if deadline.After(reset) {
				reset = deadline
			}
		}
	}
	if limited && !reset.After(time.Now()) {
		reset = time.Now().Add(time.Second)
	}
	gate.limitUntil(reset)
}
func linearTokenGeneration(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}
func (a *LinearAdapter) query(ctx context.Context, b linearBinding, operation string, variables any, out any) error {
	switch operation {
	case linearIdentityQuery, linearListQuery, linearDetailQuery, linearRelationsQuery, linearInverseQuery:
	default:
		return LinearDiagnostic{"read-only", "unapproved provider query"}
	}
	payload, _ := json.Marshal(struct {
		Query     string `json:"query"`
		Variables any    `json:"variables"`
	}{operation, variables})
	// The quota scheduler serializes requests for the same account and organization
	// in this client. Sync's cross-page quota reconciliation is handled separately.
	gate := quotaScheduler("linear:"+b.organization+":"+b.account+":"+a.endpoint(), 1)
	value, err := gate.schedule(ctx, PriorityDetail, linearTokenGeneration(b.token)+":"+string(payload), "", true, func(workCtx context.Context) (any, error) {
		if err := gate.waitQuota(workCtx); err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(workCtx, http.MethodPost, a.endpoint(), bytes.NewReader(payload))
		if err != nil {
			return nil, LinearDiagnostic{"invalid-source", "invalid endpoint"}
		}
		request.Header.Set("Authorization", b.token)
		request.Header.Set("Content-Type", "application/json")
		response, err := a.client().Do(request)
		if err != nil {
			return nil, LinearDiagnostic{"offline", "Linear API unavailable"}
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20+1))
		if err != nil || len(data) > 8<<20 {
			return nil, LinearDiagnostic{"invalid-response", "Linear response unreadable"}
		}
		if response.StatusCode == 401 {
			return nil, LinearDiagnostic{"authentication", "Linear rejected the credential"}
		}
		if response.StatusCode == 403 {
			return nil, LinearDiagnostic{"permission-denied", "Linear denied access"}
		}
		if response.StatusCode == 429 {
			linearLimitFromHeaders(gate, response.Header, true)
			return nil, LinearDiagnostic{"rate-limited", "Linear quota exhausted"}
		}
		if response.StatusCode != 200 {
			return nil, LinearDiagnostic{"provider-error", "Linear request failed"}
		}
		var envelope struct {
			Data   json.RawMessage   `json:"data"`
			Errors []json.RawMessage `json:"errors"`
		}
		if json.Unmarshal(data, &envelope) != nil || len(envelope.Errors) != 0 || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
			return nil, LinearDiagnostic{"provider-error", "Linear GraphQL request failed or access unavailable"}
		}
		linearLimitFromHeaders(gate, response.Header, false)
		return envelope.Data, nil
	})
	if err != nil {
		return err
	}
	if json.Unmarshal(value.(json.RawMessage), out) != nil {
		return LinearDiagnostic{"invalid-response", "Linear data invalid"}
	}
	return nil
}

func (a *LinearAdapter) Resolve(ctx context.Context, options map[string]string) (Source, error) {
	organization, team, account, id := options["organization"], options["team"], options["account"], options["id"]
	// A failed credential or scope refresh must never leave an old authority
	// usable for this source ID.
	a.mu.Lock()
	delete(a.bindings, id)
	a.mu.Unlock()
	if !linearUUID(organization) || !linearUUID(team) || !linearUUID(account) || (options["project"] != "" && !linearUUID(options["project"])) || id == "" {
		return Source{}, LinearDiagnostic{"invalid-source", "organization, team, account UUIDs and source id required"}
	}
	var argv []string
	if json.Unmarshal([]byte(options["credentialHelper"]), &argv) != nil || len(argv) == 0 {
		return Source{}, LinearDiagnostic{"invalid-source", "credential helper argv required"}
	}
	b := linearBinding{organization: organization, team: team, project: options["project"], account: account, argv: argv}
	helper := a.Helper
	if helper == nil {
		helper = new(CredentialHelper)
	}
	token, err := helper.Resolve(ctx, argv, account, func(ctx context.Context, token string) (string, error) {
		b.token = token
		var result struct {
			Viewer *struct {
				ID           string `json:"id"`
				Organization *struct {
					ID string `json:"id"`
				} `json:"organization"`
			} `json:"viewer"`
			Team *struct{ ID, Name string } `json:"team"`
		}
		if err := a.query(ctx, b, linearIdentityQuery, map[string]any{"team": team}, &result); err != nil {
			return "", err
		}
		if result.Viewer == nil || result.Viewer.Organization == nil || result.Viewer.Organization.ID != organization || result.Team == nil || result.Team.ID != team {
			return "", LinearDiagnostic{"identity-changed", "organization or team identity could not be verified"}
		}
		return result.Viewer.ID, nil
	})
	if err != nil {
		return Source{}, LinearDiagnostic{"authentication", "credential or scope verification failed; check helper, account, organization and team"}
	}
	digest := sha256.Sum256([]byte(token))
	b.token = token
	b.generation = organization + ":" + team + ":" + b.project + ":" + account + ":" + hex.EncodeToString(digest[:])
	source := Source{ID: id, Name: team, Locator: organization, Adapter: "linear"}
	a.mu.Lock()
	if a.bindings == nil {
		a.bindings = make(map[string]linearBinding)
	}
	a.bindings[id] = b
	a.mu.Unlock()
	return source, nil
}
func linearUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
func (a *LinearAdapter) Capabilities(_ context.Context, source Source, _ string, _ *Ref) (CapabilitySet, error) {
	if _, err := a.binding(source); err != nil {
		return nil, err
	}
	read := Capability{Support: Supported, Permission: Allowed, Availability: Available}
	disabled := Capability{Support: Supported, Permission: Denied, Availability: Available, Reason: "Linear claims and writes require later adapter stages"}
	return CapabilitySet{"identity": read, "discovery": read, "dependencies": read, "state": read, "assignment": read, "authentication": read, "effects": {Support: SupportUnknown, Permission: PermissionUnknown, Availability: Available}, "native-claims": {Support: Unsupported, Permission: Denied, Availability: Available}, "mutation": disabled, "progress": disabled, "synchronization": {Support: Unsupported, Permission: Denied, Availability: Available}}, nil
}

type linearIssue struct {
	ID, Identifier, Title, Description string
	UpdatedAt                          time.Time
	ArchivedAt                         *time.Time
	Trashed                            bool
	Team                               *struct{ ID string }
	Project                            *struct{ ID string }
	State                              *struct{ ID, Name, Type string }
	Assignee                           *struct{ ID string }
}

func (a *LinearAdapter) within(b linearBinding, issue *linearIssue) bool {
	return issue != nil && linearUUID(issue.ID) && issue.Team != nil && issue.Team.ID == b.team && (b.project == "" || issue.Project != nil && issue.Project.ID == b.project)
}
func linearSummary(source Source, issue linearIssue) Summary {
	state, terminal := StateUnknown, false
	raw := "unknown"
	if issue.State != nil {
		raw = issue.State.ID + ":" + issue.State.Name + ":" + issue.State.Type
		switch issue.State.Type {
		case "backlog", "unstarted":
			state = StateOpen
		case "started":
			state = StateInProgress
		case "completed":
			state = StateComplete
			terminal = true
		case "duplicate", "canceled":
			state = StateComplete
			terminal = true
		}
	}
	if issue.ArchivedAt != nil || issue.Trashed {
		state = StateUnknown
		terminal = false
		raw += ";archived-or-trashed"
	}
	owners := []string(nil)
	if issue.Assignee != nil {
		owners = []string{issue.Assignee.ID}
	}
	return Summary{Ref: Ref{source.ID, issue.ID}, CanonicalID: issue.ID, Title: issue.Title, RawStatus: raw, State: state, Terminal: terminal, AssignedTo: owners, Order: issue.Identifier, UpdatedAt: issue.UpdatedAt, Fresh: true}
}
func linearObservation(b linearBinding, coverage Coverage) Observation {
	return Observation{Principal: b.account, ConfigurationGeneration: b.generation, ObservedAt: time.Now(), Coverage: coverage}
}
func linearCount(budget int) int {
	if budget <= 0 || budget > 250 {
		return 250
	}
	return budget
}

type linearPageInfo struct {
	HasNextPage bool
	EndCursor   string
}
type linearCursor struct {
	After     string `json:"after"`
	Pages     int    `json:"pages"`
	Direction string `json:"direction,omitempty"`
	Ref       string `json:"ref,omitempty"`
	Scope     string `json:"scope,omitempty"`
}

func parseLinearCursor(raw string) (linearCursor, error) {
	var c linearCursor
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(data) > 2048 || json.Unmarshal(data, &c) != nil || c.After == "" || c.Pages < 0 || c.Pages > 1000 {
		return c, LinearDiagnostic{"invalid-cursor", "invalid Linear cursor"}
	}
	return c, nil
}
func encodeLinearCursor(c linearCursor) string {
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}
func (a *LinearAdapter) List(ctx context.Context, source Source, query Query, cursor string) (SummaryPage, error) {
	b, err := a.binding(source)
	if err != nil {
		return SummaryPage{}, err
	}
	var c linearCursor
	if cursor != "" {
		c, err = parseLinearCursor(cursor)
		if err != nil || c.Direction != "" || c.Scope != b.generation {
			return SummaryPage{}, LinearDiagnostic{"invalid-cursor", "Linear list cursor or configuration changed"}
		}
	}
	var result struct {
		Team *struct {
			ID     string
			Issues struct {
				Nodes    []linearIssue
				PageInfo linearPageInfo
			}
		}
	}
	var after any
	if cursor != "" {
		after = c.After
	}
	if err := a.query(ctx, b, linearListQuery, map[string]any{"team": b.team, "after": after, "count": linearCount(query.Budget)}, &result); err != nil {
		return SummaryPage{}, err
	}
	if result.Team == nil || result.Team.ID != b.team {
		return SummaryPage{}, LinearDiagnostic{"not-found-or-inaccessible", "team unavailable or changed"}
	}
	coverage := Coverage{State: CoverageComplete, Scope: source.ID, TotalAccuracy: TotalUnknown}
	page := SummaryPage{Coverage: coverage}
	for _, issue := range result.Team.Issues.Nodes {
		if !linearUUID(issue.ID) || issue.Team == nil || issue.Team.ID != b.team {
			return SummaryPage{}, LinearDiagnostic{"identity-changed", "issue identity or team changed during scan"}
		}
		if a.within(b, &issue) {
			page.Items = append(page.Items, linearSummary(source, issue))
		}
	}
	if result.Team.Issues.PageInfo.HasNextPage {
		if result.Team.Issues.PageInfo.EndCursor == "" || result.Team.Issues.PageInfo.EndCursor == c.After {
			return SummaryPage{}, LinearDiagnostic{"invalid-response", "pagination cursor missing or repeated"}
		}
		page.NextCursor = encodeLinearCursor(linearCursor{After: result.Team.Issues.PageInfo.EndCursor, Pages: c.Pages + 1, Scope: b.generation})
	}
	// A moving updatedAt order can skip a row between pages. A multi-page
	// traversal is never a complete snapshot, even when its final cursor ends.
	// A visible scan cannot prove deletion under unprobed partial permissions;
	// moving updatedAt pages can also skip rows. Sync must reconcile first.
	coverage.State = CoveragePartial
	coverage.Reason = "visibility and moving-cursor scan require reconciliation"
	page.Coverage = coverage
	page.Observation = linearObservation(b, coverage)
	return page, nil
}
func (a *LinearAdapter) ReadItems(ctx context.Context, source Source, refs []Ref, _ []string, budget int) []ItemOutcome {
	out := make([]ItemOutcome, len(refs))
	b, err := a.binding(source)
	for i, ref := range refs {
		out[i].Ref = ref
		if err != nil {
			out[i].Kind = "failed"
			out[i].Err = err
			continue
		}
		if ref.SourceID != source.ID || !linearUUID(ref.ItemID) {
			out[i].Kind = "failed"
			out[i].Err = LinearDiagnostic{"invalid-ref", "issue outside source"}
			continue
		}
		if i >= linearCount(budget) {
			out[i].Kind = "failed"
			out[i].Err = LinearDiagnostic{"budget-exceeded", "item read budget exhausted"}
			continue
		}
		var result struct{ Issue *linearIssue }
		if readErr := a.query(ctx, b, linearDetailQuery, map[string]any{"id": ref.ItemID}, &result); readErr != nil {
			out[i].Kind = "failed"
			out[i].Err = readErr
			continue
		}
		if result.Issue == nil {
			out[i].Kind = "withheld"
			continue
		}
		if result.Issue.ID != ref.ItemID || !a.within(b, result.Issue) {
			out[i].Kind = "withheld"
			continue
		}
		summary := linearSummary(source, *result.Issue)
		item := Item{Summary: summary, Body: result.Issue.Description, TerminalKnown: summary.State != StateUnknown, Assignment: Assignment{Known: true, Assigned: len(summary.AssignedTo) > 0, Owners: summary.AssignedTo}, ReadPermission: Allowed, ReadOutcome: "found", Observation: linearObservation(b, Coverage{State: CoverageComplete, Scope: source.ID, TotalAccuracy: TotalUnknown})}
		out[i].Item = &item
		out[i].Kind = "found"
		out[i].Observation = item.Observation
	}
	return out
}

type linearRelation struct {
	Type         string
	RelatedIssue *struct {
		ID      string
		Team    *struct{ ID string }
		Project *struct{ ID string }
		State   *struct{ Type string }
	}
	Issue *struct {
		ID      string
		Team    *struct{ ID string }
		Project *struct{ ID string }
		State   *struct{ Type string }
	}
}
type linearRelations struct {
	Nodes    []linearRelation
	PageInfo linearPageInfo
}

func (a *LinearAdapter) ReadDependencies(ctx context.Context, source Source, ref Ref, cursor string, budget int) (DependencyPage, error) {
	b, err := a.binding(source)
	if err != nil {
		return DependencyPage{}, err
	}
	if ref.SourceID != source.ID || !linearUUID(ref.ItemID) {
		return DependencyPage{}, LinearDiagnostic{"invalid-ref", "issue outside source"}
	}
	c := linearCursor{Direction: "out", Ref: ref.ItemID, Scope: b.generation}
	if cursor != "" {
		c, err = parseLinearCursor(cursor)
		if err != nil || c.Ref != ref.ItemID || c.Scope != b.generation || (c.Direction != "out" && c.Direction != "in") {
			return DependencyPage{}, LinearDiagnostic{"invalid-cursor", "Linear relation cursor or scope changed"}
		}
	}
	operation := linearRelationsQuery
	if c.Direction == "in" {
		operation = linearInverseQuery
	}
	var after any
	if cursor != "" && c.After != "@start" {
		after = c.After
	}
	var result struct {
		Issue *struct {
			ID      string
			Team    *struct{ ID string }
			Project *struct{ ID string }
			Parent  *struct {
				ID      string
				Team    *struct{ ID string }
				Project *struct{ ID string }
			}
			Relations        linearRelations
			InverseRelations linearRelations
		}
	}
	if err := a.query(ctx, b, operation, map[string]any{"id": ref.ItemID, "after": after, "count": linearCount(budget)}, &result); err != nil {
		return DependencyPage{}, err
	}
	if result.Issue == nil || result.Issue.ID != ref.ItemID || result.Issue.Team == nil || result.Issue.Team.ID != b.team || (b.project != "" && (result.Issue.Project == nil || result.Issue.Project.ID != b.project)) {
		return DependencyPage{}, LinearDiagnostic{"not-found-or-inaccessible", "issue or source scope unavailable"}
	}
	relations := result.Issue.Relations
	if c.Direction == "in" {
		relations = result.Issue.InverseRelations
	}
	coverage := Coverage{State: CoverageComplete, Scope: ref.String(), TotalAccuracy: TotalUnknown}
	page := DependencyPage{Completeness: CoverageComplete}
	if c.Direction == "out" && cursor == "" && result.Issue.Parent != nil {
		parent := result.Issue.Parent
		if !linearUUID(parent.ID) || parent.Team == nil || parent.Team.ID != b.team || (b.project != "" && (parent.Project == nil || parent.Project.ID != b.project)) {
			page.Completeness = CoverageUnknown
		} else {
			page.Edges = append(page.Edges, Relationship{Type: ParentChild, Direction: ParentToChild, From: Ref{source.ID, parent.ID}, To: ref, Provenance: "linear.parent", Interpretation: "hierarchy only", Fresh: true, Support: Supported})
		}
	}
	for _, rel := range relations.Nodes {
		related := rel.RelatedIssue
		if c.Direction == "in" {
			related = rel.Issue
		}
		if related == nil || !linearUUID(related.ID) || related.Team == nil || related.Team.ID != b.team || (b.project != "" && (related.Project == nil || related.Project.ID != b.project)) {
			page.Completeness = CoverageUnknown
			continue
		}
		if rel.Type != "blocks" && rel.Type != "related" && rel.Type != "duplicate" && rel.Type != "similar" {
			page.Completeness = CoverageUnknown
			continue
		}
		target := Ref{source.ID, related.ID}
		edge := Relationship{Type: Related, Direction: NonBlockingDirection, From: ref, To: target, Provenance: "linear." + c.Direction + "." + rel.Type, Interpretation: "informational", Fresh: true, Support: Supported}
		if rel.Type == "blocks" && c.Direction == "in" {
			edge.Type = HardPrerequisite
			edge.Direction = DependentToPrerequisite
			edge.Condition = "terminal"
			edge.RawOutcome = "unknown"
			if related.State != nil {
				edge.RawOutcome = related.State.Type
			}
			edge.Interpretation = "blocking issue must be terminal; relation changes require independent reconciliation"
		}
		if rel.Type == "blocks" && c.Direction == "out" {
			edge.Interpretation = "blocks another issue; not this issue's prerequisite"
		}
		page.Edges = append(page.Edges, edge)
	}
	if relations.PageInfo.HasNextPage {
		if relations.PageInfo.EndCursor == "" || relations.PageInfo.EndCursor == c.After {
			return DependencyPage{}, LinearDiagnostic{"invalid-response", "relation cursor missing or repeated"}
		}
		c.After = relations.PageInfo.EndCursor
		c.Pages++
		page.NextCursor = encodeLinearCursor(c)
	} else if c.Direction == "out" {
		c.Direction = "in"
		c.After = "@start"
		c.Pages++
		page.NextCursor = encodeLinearCursor(c)
	}
	if page.NextCursor != "" && page.Completeness == CoverageComplete {
		page.Completeness = CoveragePartial
	}
	coverage.State = page.Completeness
	coverage.ObservedEdges = len(page.Edges)
	page.Observation = linearObservation(b, coverage)
	return page, nil
}

var _ Adapter = (*LinearAdapter)(nil)
