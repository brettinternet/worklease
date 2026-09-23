package queue

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GitHubDiagnostic never contains a provider response, request body, URL, or credential.
type GitHubDiagnostic struct {
	Code   string
	Detail string
}

func (d GitHubDiagnostic) Error() string { return d.Code + ": " + d.Detail }

type githubAccountGate struct {
	mu   sync.Mutex
	next time.Time
}

var githubAccountLocks sync.Map // host/account -> *githubAccountGate, shared by adapter instances

func githubLock(host, account string) *githubAccountGate {
	key := strings.ToLower(host + "\x00" + account)
	value, _ := githubAccountLocks.LoadOrStore(key, &githubAccountGate{})
	return value.(*githubAccountGate)
}

type githubBinding struct {
	host, repository, account, token, endpoint string
	generation                                 string
	dependencies                               bool
	identityChanged                            bool
	scans                                      map[string]map[string]bool
}

// GitHubAdapter is a read-only adapter. Client and APIBase are test seams; production
// always uses HTTPS on the configured host and never inherits ambient GitHub tokens.
type GitHubAdapter struct {
	Client   *http.Client
	Binary   string
	APIBase  string
	mu       sync.Mutex
	bindings map[string]*githubBinding
}

func NewGitHubAdapter() *GitHubAdapter  { return &GitHubAdapter{bindings: map[string]*githubBinding{}} }
func (*GitHubAdapter) OnDemandDetails() {}
func (a *GitHubAdapter) binary() string {
	if a.Binary != "" {
		return a.Binary
	}
	return "gh"
}
func (a *GitHubAdapter) client() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func (a *GitHubAdapter) binding(source Source) (*githubBinding, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b := a.bindings[source.ID]
	if b == nil || b.repository != source.Locator {
		return nil, GitHubDiagnostic{"invalid-source", "resolve the configured source first"}
	}
	if b.identityChanged {
		return nil, GitHubDiagnostic{"identity-changed", "configured repository identity changed; claims unavailable until rebind"}
	}
	return b, nil
}

// ConfigurationGeneration binds read-only cursor state to the resolved account credential.
func (a *GitHubAdapter) ConfigurationGeneration(source Source) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if binding := a.bindings[source.ID]; binding != nil {
		return binding.generation
	}
	return ""
}
func (a *GitHubAdapter) drift(b *githubBinding) {
	a.mu.Lock()
	b.identityChanged = true
	a.mu.Unlock()
}
func (a *GitHubAdapter) Resolve(ctx context.Context, options map[string]string) (Source, error) {
	host, repository, account := options["host"], options["repository"], options["account"]
	parts := strings.Split(repository, "/")
	if host == "" || strings.ContainsAny(host, "/\\ :@?#\t\r\n") || len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(repository, " :@?#\t\r\n") || account == "" || strings.ContainsAny(account, " \t\r\n") {
		return Source{}, GitHubDiagnostic{"invalid-source", "host, owner/repository, and account required"}
	}
	endpoint := "https://" + host + "/api/graphql"
	if strings.EqualFold(host, "github.com") {
		endpoint = "https://api.github.com/graphql"
	}
	if a.APIBase != "" {
		endpoint = a.APIBase
	} // local httptest only
	u, err := url.Parse(endpoint)
	if err != nil || (a.APIBase == "" && u.Scheme != "https") {
		return Source{}, GitHubDiagnostic{"invalid-source", "HTTPS API required"}
	}
	credentialContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(credentialContext, a.binary(), "auth", "token", "--hostname", host, "--user", account)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN":
			continue
		}
		command.Env = append(command.Env, entry)
	}
	var output limitedBuffer
	output.limit = 4096
	command.Stdout = &output
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	if err := command.Run(); err != nil || output.exceeded {
		return Source{}, GitHubDiagnostic{"authentication", "credential helper failed"}
	}
	token := strings.TrimSpace(output.data.String())
	if token == "" {
		return Source{}, GitHubDiagnostic{"authentication", "credential helper returned no token"}
	}
	credentialDigest := sha256.Sum256([]byte(token))
	b := &githubBinding{host: host, repository: repository, account: account, token: token, endpoint: endpoint, generation: host + "/" + repository + ":" + hex.EncodeToString(credentialDigest[:]), dependencies: true}
	// Verify the authenticated principal before any repository or issue data request.
	var viewer struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	if err := a.query(ctx, b, githubViewerQuery, nil, &viewer); err != nil {
		return Source{}, err
	}
	if viewer.Viewer.Login != account {
		return Source{}, GitHubDiagnostic{"authentication", "authenticated principal does not match configured account"}
	}
	id := options["id"]
	if id == "" {
		id = host + "/" + repository
	}
	source := Source{ID: id, Name: repository, Locator: repository, Adapter: "github"}
	a.mu.Lock()
	a.bindings[id] = b
	a.mu.Unlock()
	return source, nil
}

// query serializes both HTTP calls and quota waits for a configured account.
const githubViewerQuery = `query { viewer { login } }`

func (a *GitHubAdapter) query(ctx context.Context, b *githubBinding, query string, variables any, dest any) error {
	// Only the four vetted read operations can reach the provider. This also
	// rejects dynamically assembled GraphQL mutations before any network I/O.
	switch query {
	case githubViewerQuery, githubListQuery, githubDetailQuery, githubDependencyQuery:
	default:
		return GitHubDiagnostic{"read-only", "queue provider query is not a permitted read"}
	}
	gate := githubLock(b.host, b.account)
	gate.mu.Lock()
	defer gate.mu.Unlock()
	body, _ := json.Marshal(struct {
		Query     string `json:"query"`
		Variables any    `json:"variables,omitempty"`
	}{query, variables})
	for attempt := 0; attempt < 3; attempt++ {
		if delay := time.Until(gate.next); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return GitHubDiagnostic{"rate-limited", "GitHub quota exhausted; retry later"}
			case <-timer.C:
			}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(body))
		if err != nil {
			return GitHubDiagnostic{"invalid-source", "invalid API endpoint"}
		}
		request.Header.Set("Authorization", "Bearer "+b.token)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/vnd.github+json")
		response, err := a.client().Do(request)
		if err != nil {
			return GitHubDiagnostic{"offline", "GitHub API unavailable"}
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 8<<20+1))
		response.Body.Close()
		if readErr != nil || len(raw) > 8<<20 {
			return GitHubDiagnostic{"invalid-response", "GitHub response unreadable"}
		}
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			return GitHubDiagnostic{"identity-changed", "GitHub redirected the configured repository; claims unavailable"}
		}
		var payload struct {
			Data   json.RawMessage `json:"data"`
			Errors []struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"errors"`
		}
		_ = json.Unmarshal(raw, &payload)
		lower := strings.ToLower(string(raw))
		limited := response.StatusCode == 429 || (response.StatusCode == 403 && (response.Header.Get("Retry-After") != "" || response.Header.Get("X-RateLimit-Remaining") == "0" || strings.Contains(lower, "rate limit")))
		for _, issue := range payload.Errors {
			if strings.Contains(strings.ToLower(issue.Message), "rate limit") {
				limited = true
			}
		}
		if limited {
			if attempt == 2 {
				return GitHubDiagnostic{"rate-limited", "GitHub quota exhausted; retry later"}
			}
			delay := time.Duration(1<<attempt)*time.Second + time.Duration(rand.Int63n(int64(250*time.Millisecond)))
			if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 {
				delay = time.Duration(seconds) * time.Second
			}
			if reset, err := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && response.Header.Get("X-RateLimit-Remaining") == "0" {
				until := time.Until(time.Unix(reset, 0))
				if until > delay {
					delay = until
				}
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return GitHubDiagnostic{"rate-limited", "GitHub quota exhausted; retry later"}
			case <-timer.C:
			}
			continue
		}
		switch response.StatusCode {
		case 401:
			return GitHubDiagnostic{"authentication", "GitHub rejected the credential"}
		case 403:
			if strings.Contains(lower, "saml") || response.Header.Get("X-GitHub-SSO") != "" {
				return GitHubDiagnostic{"saml-sso", "SAML authorization required"}
			}
			return GitHubDiagnostic{"permission-denied", "GitHub denied access"}
		case 404:
			return GitHubDiagnostic{"not-found-or-inaccessible", "resource not found or access unavailable"}
		case 410:
			return GitHubDiagnostic{"gone", "GitHub resource unavailable"}
		}
		if response.StatusCode != 200 {
			return GitHubDiagnostic{"provider-error", "GitHub request failed"}
		}
		if len(payload.Errors) > 0 {
			for _, issue := range payload.Errors {
				msg := strings.ToLower(issue.Message)
				switch strings.ToUpper(issue.Type) {
				case "FORBIDDEN":
					return GitHubDiagnostic{"permission-denied", "GitHub denied access"}
				case "NOT_FOUND":
					return GitHubDiagnostic{"not-found-or-inaccessible", "resource not found or access unavailable"}
				}
				if strings.Contains(msg, "rate limit") {
					return GitHubDiagnostic{"rate-limited", "GitHub quota exhausted; retry later"}
				}
				if strings.Contains(msg, "saml") {
					return GitHubDiagnostic{"saml-sso", "SAML authorization required"}
				}
				if strings.Contains(msg, "blockedby") || strings.Contains(msg, "subissues") || strings.Contains(msg, "parent") {
					return GitHubDiagnostic{"dependency-unsupported", "GitHub dependency fields unavailable"}
				}
			}
			return GitHubDiagnostic{"provider-error", "GitHub GraphQL request failed"}
		}
		if len(payload.Data) == 0 {
			return GitHubDiagnostic{"invalid-response", "GitHub GraphQL data missing"}
		}
		if err := json.Unmarshal(payload.Data, dest); err != nil {
			return GitHubDiagnostic{"invalid-response", "GitHub GraphQL data invalid"}
		}
		// A successful response may consume the final unit of either quota.
		var quota struct {
			RateLimit struct {
				Remaining *int      `json:"remaining"`
				ResetAt   time.Time `json:"resetAt"`
			} `json:"rateLimit"`
		}
		_ = json.Unmarshal(payload.Data, &quota)
		reset := time.Time{}
		if response.Header.Get("X-RateLimit-Remaining") == "0" {
			if unix, parseErr := strconv.ParseInt(response.Header.Get("X-RateLimit-Reset"), 10, 64); parseErr == nil {
				reset = time.Unix(unix, 0)
			}
		}
		if quota.RateLimit.Remaining != nil && *quota.RateLimit.Remaining == 0 && quota.RateLimit.ResetAt.After(reset) {
			reset = quota.RateLimit.ResetAt
		}
		if reset.After(gate.next) {
			gate.next = reset.Add(time.Duration(rand.Int63n(int64(250 * time.Millisecond))))
		}
		return nil
	}
	return GitHubDiagnostic{"rate-limited", "GitHub quota exhausted; retry later"}
}

type githubIssue struct {
	ID          string    `json:"id"`
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	StateReason string    `json:"stateReason"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Repository  struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Assignees struct {
		Nodes []struct {
			Login string `json:"login"`
		} `json:"nodes"`
	} `json:"assignees"`
	BlockedBy githubConnection `json:"blockedBy"`
	SubIssues githubConnection `json:"subIssues"`
	Parent    *struct {
		ID         string `json:"id"`
		Number     int    `json:"number"`
		Repository struct {
			NameWithOwner string `json:"nameWithOwner"`
		} `json:"repository"`
	} `json:"parent"`
}
type githubConnection struct {
	TotalCount int             `json:"totalCount"`
	Nodes      []githubRelated `json:"nodes"`
	PageInfo   struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
}
type githubRelated struct {
	ID         string `json:"id"`
	Number     int    `json:"number"`
	State      string `json:"state"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

func (a *GitHubAdapter) relatedRef(host string, issue githubRelated) Ref {
	id := host + "/" + issue.Repository.NameWithOwner
	a.mu.Lock()
	for sourceID, binding := range a.bindings {
		if binding.host == host && binding.repository == issue.Repository.NameWithOwner {
			id = sourceID
			break
		}
	}
	a.mu.Unlock()
	return Ref{SourceID: id, ItemID: strconv.Itoa(issue.Number)}
}
func (a *GitHubAdapter) summary(source Source, issue githubIssue) Summary {
	terminal := issue.State == "CLOSED"
	state := StateOpen
	if terminal {
		state = StateComplete
	}
	owners := make([]string, 0, len(issue.Assignees.Nodes))
	for _, person := range issue.Assignees.Nodes {
		owners = append(owners, person.Login)
	}
	return Summary{Ref: Ref{source.ID, strconv.Itoa(issue.Number)}, Title: issue.Title, RawStatus: issue.State, State: state, Order: fmt.Sprintf("%012d", issue.Number), CanonicalID: issue.ID, AssignedTo: owners, UpdatedAt: issue.UpdatedAt, Fresh: true, Terminal: terminal}
}
func (a *GitHubAdapter) Capabilities(_ context.Context, source Source, _ string, _ *Ref) (CapabilitySet, error) {
	b, err := a.binding(source)
	if err != nil {
		return nil, err
	}
	read := func() Capability { return Capability{Support: Supported, Permission: Allowed, Availability: Available} }
	denied := func() Capability {
		return Capability{Support: Supported, Permission: Denied, Availability: Available, Reason: "queue adapter is read-only"}
	}
	deps := read()
	a.mu.Lock()
	dependencySupport := b.dependencies
	a.mu.Unlock()
	if !dependencySupport {
		deps.Support = Unsupported
		deps.Reason = "dependency fields unavailable on this host"
	}
	return CapabilitySet{"identity": read(), "discovery": read(), "dependencies": deps, "state": read(), "progress": denied(), "assignment": denied(), "native-claims": {Support: Unsupported, Permission: Denied, Availability: Available}, "mutation": denied(), "synchronization": {Support: Unsupported, Permission: Denied, Availability: Available}, "effects": read(), "authentication": read()}, nil
}

const githubListQuery = `query($owner:String!,$repo:String!,$after:String,$count:Int!) { rateLimit { remaining resetAt } repository(owner:$owner,name:$repo) { nameWithOwner issues(first:$count,after:$after,orderBy:{field:CREATED_AT,direction:ASC},states:[OPEN,CLOSED]) { totalCount pageInfo { hasNextPage endCursor } nodes { id number title state stateReason updatedAt repository { nameWithOwner } assignees(first:100) { nodes { login } } } } } }`

func (a *GitHubAdapter) List(ctx context.Context, source Source, query Query, cursor string) (SummaryPage, error) {
	b, err := a.binding(source)
	if err != nil {
		return SummaryPage{}, err
	}
	parts := strings.Split(b.repository, "/")
	count := query.Budget
	if count <= 0 || count > 100 {
		count = 100
	}
	var after any
	var scanID string
	if cursor != "" {
		var state struct {
			ID    string `json:"id"`
			After string `json:"after"`
		}
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(cursor)
		if decodeErr != nil || json.Unmarshal(decoded, &state) != nil || state.ID == "" || state.After == "" {
			return SummaryPage{}, GitHubDiagnostic{"invalid-cursor", "invalid GitHub scan cursor"}
		}
		a.mu.Lock()
		_, exists := b.scans[state.ID]
		a.mu.Unlock()
		if !exists {
			return SummaryPage{}, GitHubDiagnostic{"invalid-cursor", "GitHub scan expired"}
		}
		scanID, after = state.ID, state.After
	} else {
		id := make([]byte, 16)
		if _, randomErr := crand.Read(id); randomErr != nil {
			return SummaryPage{}, GitHubDiagnostic{"invalid-cursor", "scan unavailable"}
		}
		scanID = hex.EncodeToString(id)
		a.mu.Lock()
		if b.scans == nil {
			b.scans = map[string]map[string]bool{}
		}
		b.scans[scanID] = map[string]bool{}
		a.mu.Unlock()
	}
	keepScan := false
	defer func() {
		if !keepScan {
			a.mu.Lock()
			delete(b.scans, scanID)
			a.mu.Unlock()
		}
	}()
	var result struct {
		Repository *struct {
			NameWithOwner string `json:"nameWithOwner"`
			Issues        struct {
				TotalCount int           `json:"totalCount"`
				Nodes      []githubIssue `json:"nodes"`
				PageInfo   struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"issues"`
		} `json:"repository"`
	}
	err = a.query(ctx, b, githubListQuery, map[string]any{"owner": parts[0], "repo": parts[1], "after": after, "count": count}, &result)
	if err != nil {
		if d, ok := err.(GitHubDiagnostic); ok && d.Code == "identity-changed" {
			a.drift(b)
		}
		return SummaryPage{}, err
	}
	if result.Repository == nil {
		return SummaryPage{}, GitHubDiagnostic{"not-found-or-inaccessible", "repository not found or access unavailable"}
	}
	if result.Repository.NameWithOwner != b.repository {
		a.drift(b)
		return SummaryPage{}, GitHubDiagnostic{"identity-changed", "configured repository identity changed; claims unavailable until rebind"}
	}
	coverage := Coverage{State: CoverageComplete, Scope: source.ID, Total: result.Repository.Issues.TotalCount, TotalAccuracy: TotalExact}
	page := SummaryPage{Coverage: coverage, Observation: Observation{Principal: b.account, ObservedAt: time.Now(), Coverage: coverage, ConfigurationGeneration: b.generation}}
	for _, issue := range result.Repository.Issues.Nodes {
		if issue.ID == "" || issue.Number < 1 {
			return SummaryPage{}, GitHubDiagnostic{"invalid-response", "issue identity missing"}
		}
		if issue.Repository.NameWithOwner != b.repository {
			a.drift(b)
			return SummaryPage{}, GitHubDiagnostic{"identity-changed", "issue transferred; claims unavailable until rebind"}
		}
		a.mu.Lock()
		duplicate := b.scans[scanID][issue.ID]
		b.scans[scanID][issue.ID] = true
		a.mu.Unlock()
		if !duplicate {
			page.Items = append(page.Items, a.summary(source, issue))
		}
	}
	if result.Repository.Issues.PageInfo.HasNextPage {
		apiCursor := result.Repository.Issues.PageInfo.EndCursor
		if apiCursor == "" {
			return SummaryPage{}, GitHubDiagnostic{"invalid-response", "pagination cursor missing"}
		}
		encoded, _ := json.Marshal(struct {
			ID    string `json:"id"`
			After string `json:"after"`
		}{scanID, apiCursor})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
		page.Coverage.State = CoveragePartial
		keepScan = true
	}
	return page, nil
}

const githubDetailQuery = `query($owner:String!,$repo:String!,$number:Int!) { rateLimit { remaining resetAt } repository(owner:$owner,name:$repo) { nameWithOwner issue(number:$number) { id number title body state stateReason updatedAt repository { nameWithOwner } assignees(first:100) { nodes { login } } } } }`

func (a *GitHubAdapter) ReadItems(ctx context.Context, source Source, refs []Ref, _ []string, _ int) []ItemOutcome {
	outcomes := make([]ItemOutcome, len(refs))
	b, err := a.binding(source)
	for i, ref := range refs {
		outcomes[i].Ref = ref
		if err != nil {
			outcomes[i].Err = err
			outcomes[i].Kind = "failed"
			continue
		}
		number, parseErr := strconv.Atoi(ref.ItemID)
		if ref.SourceID != source.ID || parseErr != nil || number < 1 {
			outcomes[i].Err = GitHubDiagnostic{"invalid-ref", "issue outside source"}
			outcomes[i].Kind = "failed"
			continue
		}
		parts := strings.Split(b.repository, "/")
		var result struct {
			Repository *struct {
				NameWithOwner string       `json:"nameWithOwner"`
				Issue         *githubIssue `json:"issue"`
			} `json:"repository"`
		}
		readErr := a.query(ctx, b, githubDetailQuery, map[string]any{"owner": parts[0], "repo": parts[1], "number": number}, &result)
		if readErr != nil {
			if d, ok := readErr.(GitHubDiagnostic); ok && d.Code == "identity-changed" {
				a.drift(b)
			}
			outcomes[i].Err = readErr
			outcomes[i].Kind = "failed"
			continue
		}
		if result.Repository == nil || result.Repository.Issue == nil {
			outcomes[i].Kind = "inaccessible"
			continue
		}
		issue := *result.Repository.Issue
		if result.Repository.NameWithOwner != b.repository || issue.Repository.NameWithOwner != b.repository || issue.Number != number {
			a.drift(b)
			outcomes[i].Err = GitHubDiagnostic{"identity-changed", "issue identity changed; claims unavailable"}
			outcomes[i].Kind = "failed"
			continue
		}
		summary := a.summary(source, issue)
		outcomes[i].Kind = "found"
		outcomes[i].Item = &Item{Summary: summary, Body: issue.Body, TerminalKnown: true, Assignment: Assignment{Owners: summary.AssignedTo, Known: true, Assigned: len(summary.AssignedTo) > 0}, Observation: Observation{Principal: b.account, ObservedAt: time.Now(), ConfigurationGeneration: b.generation}}
	}
	return outcomes
}

const githubDependencyQuery = `query($owner:String!,$repo:String!,$number:Int!,$after:String,$count:Int!) { rateLimit { remaining resetAt } repository(owner:$owner,name:$repo) { nameWithOwner issue(number:$number) { id number repository { nameWithOwner } blockedBy(first:$count,after:$after) { totalCount pageInfo { hasNextPage endCursor } nodes { id number state repository { nameWithOwner } } } subIssues(first:100) { totalCount pageInfo { hasNextPage endCursor } nodes { id number state repository { nameWithOwner } } } parent { id number repository { nameWithOwner } } } } }`

func (a *GitHubAdapter) ReadDependencies(ctx context.Context, source Source, ref Ref, cursor string, budget int) (DependencyPage, error) {
	b, err := a.binding(source)
	if err != nil {
		return DependencyPage{}, err
	}
	n, err := strconv.Atoi(ref.ItemID)
	if err != nil || n < 1 || ref.SourceID != source.ID {
		return DependencyPage{}, GitHubDiagnostic{"invalid-ref", "issue outside source"}
	}
	count := budget
	if count <= 0 || count > 100 {
		count = 100
	}
	parts := strings.Split(b.repository, "/")
	var after any
	var state struct {
		After            string `json:"after"`
		Count            int    `json:"count"`
		HierarchyPartial bool   `json:"hierarchyPartial"`
	}
	if cursor != "" {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(cursor)
		if decodeErr != nil || json.Unmarshal(decoded, &state) != nil || state.After == "" || state.Count < 0 {
			return DependencyPage{}, GitHubDiagnostic{"invalid-cursor", "invalid dependency cursor"}
		}
		after = state.After
	}
	var result struct {
		Repository *struct {
			NameWithOwner string       `json:"nameWithOwner"`
			Issue         *githubIssue `json:"issue"`
		} `json:"repository"`
	}
	err = a.query(ctx, b, githubDependencyQuery, map[string]any{"owner": parts[0], "repo": parts[1], "number": n, "after": after, "count": count}, &result)
	if d, ok := err.(GitHubDiagnostic); ok && d.Code == "dependency-unsupported" {
		a.mu.Lock()
		b.dependencies = false
		a.mu.Unlock()
		return DependencyPage{Completeness: CoverageUnknown, Observation: Observation{Principal: b.account, ObservedAt: time.Now(), Coverage: Coverage{State: CoverageUnknown, Reason: "dependency-unsupported", TotalAccuracy: TotalUnknown}}}, nil
	}
	if err != nil {
		if d, ok := err.(GitHubDiagnostic); ok && d.Code == "identity-changed" {
			a.drift(b)
		}
		return DependencyPage{}, err
	}
	if result.Repository == nil || result.Repository.Issue == nil {
		return DependencyPage{}, GitHubDiagnostic{"not-found-or-inaccessible", "issue not found or access unavailable"}
	}
	issue := result.Repository.Issue
	if result.Repository.NameWithOwner != b.repository || issue.Repository.NameWithOwner != b.repository || issue.Number != n {
		a.drift(b)
		return DependencyPage{}, GitHubDiagnostic{"identity-changed", "issue identity changed; claims unavailable"}
	}
	page := DependencyPage{Completeness: CoverageComplete}
	for _, related := range issue.BlockedBy.Nodes {
		if related.Number < 1 || related.Repository.NameWithOwner == "" {
			page.Completeness = CoverageUnknown
			continue
		}
		to := a.relatedRef(b.host, related)
		if related.Repository.NameWithOwner == b.repository {
			to.SourceID = source.ID
		}
		kind := HardPrerequisite
		if to.SourceID != source.ID {
			kind = CrossSourcePrerequisite
		}
		page.Edges = append(page.Edges, Relationship{Type: kind, Direction: DependentToPrerequisite, From: ref, To: to, Provenance: "github.blockedBy", Condition: "terminal", RawOutcome: related.State, Interpretation: "closed issue satisfies terminal", Fresh: true, Support: Supported})
	}
	state.Count += len(issue.BlockedBy.Nodes)
	if issue.BlockedBy.TotalCount != state.Count && !issue.BlockedBy.PageInfo.HasNextPage {
		page.Completeness = CoveragePartial
	}
	if issue.BlockedBy.PageInfo.HasNextPage {
		page.Completeness = CoveragePartial
		state.After = issue.BlockedBy.PageInfo.EndCursor
		if state.After == "" {
			page.Completeness = CoverageUnknown
		}
	}
	if cursor == "" {
		for _, child := range issue.SubIssues.Nodes {
			to := a.relatedRef(b.host, child)
			if child.Repository.NameWithOwner == b.repository {
				to.SourceID = source.ID
			}
			page.Edges = append(page.Edges, Relationship{Type: ParentChild, Direction: ParentToChild, From: ref, To: to, Provenance: "github.subIssues", Interpretation: "hierarchy only", Fresh: true, Support: Supported})
		}
		if issue.Parent != nil {
			parent := a.relatedRef(b.host, githubRelated{Number: issue.Parent.Number, Repository: issue.Parent.Repository})
			if issue.Parent.Repository.NameWithOwner == b.repository {
				parent.SourceID = source.ID
			}
			page.Edges = append(page.Edges, Relationship{Type: ParentChild, Direction: ParentToChild, From: parent, To: ref, Provenance: "github.parent", Interpretation: "hierarchy only", Fresh: true, Support: Supported})
		}
		if issue.SubIssues.PageInfo.HasNextPage || issue.SubIssues.TotalCount > len(issue.SubIssues.Nodes) {
			state.HierarchyPartial = true
			page.Completeness = CoveragePartial
		}
	}
	if state.HierarchyPartial {
		page.Completeness = CoveragePartial
	}
	if issue.BlockedBy.PageInfo.HasNextPage && state.After != "" {
		encoded, _ := json.Marshal(state)
		page.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
	}
	page.Observation = Observation{Principal: b.account, ObservedAt: time.Now(), Coverage: Coverage{State: page.Completeness, Scope: ref.String(), Total: issue.BlockedBy.TotalCount, TotalAccuracy: TotalExact}}
	return page, nil
}

var _ Adapter = (*GitHubAdapter)(nil)
