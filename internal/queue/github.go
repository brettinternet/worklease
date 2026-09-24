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

// GitHubRateDiagnostic carries a safe, machine-readable retry deadline.
type GitHubRateDiagnostic struct {
	GitHubDiagnostic
	RetryAt time.Time `json:"retryAt"`
}

type githubBinding struct {
	host, repository, account, token, endpoint string
	repositoryID                               string
	generation                                 string
	dependencies                               bool
	identityChanged                            bool
	identityDetail                             string
	scans                                      map[string]map[string]string
	scanOrder                                  []string
	nodeIDs                                    map[string]string
	newestETags                                map[string]string
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
// QueueCacheIdentity deliberately disables persistent caching: this adapter has no provider-issued permission-scope fingerprint.
func (a *GitHubAdapter) QueueCacheIdentity(Source) (string, string, string, bool) {
	return "", "", "", false
}

func (a *GitHubAdapter) ConfigurationGeneration(source Source) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if binding := a.bindings[source.ID]; binding != nil {
		return binding.generation
	}
	return ""
}

func (a *GitHubAdapter) GitHubSyncIdentity(source Source) (string, string, string, string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	binding := a.bindings[source.ID]
	if binding == nil || binding.repository != source.Locator {
		return "", "", "", "", false
	}
	repositoryID := binding.repositoryID
	if repositoryID == "" {
		return "", "", "", "", false
	}
	return binding.account, strings.ToLower(binding.host), repositoryID, binding.generation, true
}
func (a *GitHubAdapter) drift(b *githubBinding) {
	a.mu.Lock()
	b.identityChanged = true
	a.mu.Unlock()
}

func (a *GitHubAdapter) driftTo(b *githubBinding, newLocator string) {
	a.mu.Lock()
	b.identityChanged = true
	b.identityDetail = b.host + "/" + b.repository + " -> " + b.host + "/" + newLocator
	a.mu.Unlock()
}

// IdentityChange reports only public repository locators, never provider payloads.
func (a *GitHubAdapter) IdentityChange(source Source) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if b := a.bindings[source.ID]; b != nil {
		return b.identityDetail
	}
	return ""
}
func (a *GitHubAdapter) Resolve(ctx context.Context, options map[string]string) (Source, error) {
	host, repository, account := options["host"], options["repository"], options["account"]
	parts := strings.Split(repository, "/")
	if host == "" || strings.ContainsAny(host, "/\\ :@?#\t\r\n") || len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(repository, " :@?#\t\r\n") || account == "" || strings.ContainsAny(account, " \t\r\n") {
		return Source{}, GitHubDiagnostic{"invalid-source", "host, owner/repository, and account required"}
	}
	id := options["id"]
	if id == "" {
		id = host + "/" + repository
	}
	a.mu.Lock()
	delete(a.bindings, id)
	a.mu.Unlock()
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
	if a.APIBase != "" {
		// The local fake API has no repository node resolver; production always resolves
		// and persists the provider's immutable repository node ID below.
		b.repositoryID = strings.ToLower(host) + "/" + strings.ToLower(repository)
	} else {
		parts := strings.Split(repository, "/")
		var repoIdentity struct {
			Repository *struct {
				ID            string `json:"id"`
				NameWithOwner string `json:"nameWithOwner"`
			} `json:"repository"`
		}
		if err := a.query(ctx, b, githubRepositoryIdentityQuery, map[string]any{"owner": parts[0], "repo": parts[1]}, &repoIdentity); err != nil {
			return Source{}, err
		}
		if repoIdentity.Repository == nil || repoIdentity.Repository.ID == "" || !strings.EqualFold(repoIdentity.Repository.NameWithOwner, repository) {
			return Source{}, GitHubDiagnostic{"not-found-or-inaccessible", "repository not found or access unavailable"}
		}
		b.repositoryID = repoIdentity.Repository.ID
	}
	if id == "" {
		id = host + "/" + repository
	}
	source := Source{ID: id, Name: repository, Locator: repository, Adapter: "github"}
	a.mu.Lock()
	a.bindings[id] = b
	a.mu.Unlock()
	return source, nil
}

// query schedules safe reads by account quota; claims and heartbeats never use this path.
const githubViewerQuery = `query { viewer { login } }`
const githubRepositoryIdentityQuery = `query($owner:String!,$repo:String!) { repository(owner:$owner,name:$repo) { id nameWithOwner } }`

func (a *GitHubAdapter) query(ctx context.Context, b *githubBinding, query string, variables any, dest any) error {
	// Only the four vetted read operations can reach the provider. This also
	// rejects dynamically assembled GraphQL mutations before any network I/O.
	switch query {
	case githubViewerQuery, githubRepositoryIdentityQuery, githubListQuery, githubIncrementalQuery, githubNodesQuery, githubDetailQuery, githubDependencyQuery, githubCommentsQuery:
	default:
		return GitHubDiagnostic{"read-only", "queue provider query is not a permitted read"}
	}
	identity := "github:" + strings.ToLower(b.host+"\x00"+b.account)
	if a.APIBase != "" {
		identity += "\x00" + a.APIBase
	} // independent test servers do not share quota
	gate := quotaScheduler(identity, 1)
	body, _ := json.Marshal(struct {
		Query     string `json:"query"`
		Variables any    `json:"variables,omitempty"`
	}{query, variables})
	priority := PriorityVisible
	if query == githubViewerQuery || query == githubRepositoryIdentityQuery || query == githubDetailQuery || query == githubDependencyQuery || query == githubCommentsQuery {
		priority = PriorityDetail
	}
	key := b.generation + ":" + string(body)
	value, err := gate.schedule(ctx, priority, key, "", true, func(workCtx context.Context) (any, error) {
		return a.queryHTTP(workCtx, b, body, gate)
	})
	if err != nil {
		if diagnostic, ok := err.(ScheduleDiagnostic); ok {
			if diagnostic.Code == "rate-limited" {
				return GitHubRateDiagnostic{GitHubDiagnostic{"rate-limited", "provider quota exhausted; retry later"}, diagnostic.RetryAt}
			}
			return GitHubDiagnostic{diagnostic.Code, "provider request capacity unavailable"}
		}
		if ctx.Err() != nil {
			gate.mu.Lock()
			limited := gate.retryAt.After(time.Now())
			gate.mu.Unlock()
			if limited {
				return GitHubDiagnostic{"rate-limited", "GitHub quota exhausted; retry later"}
			}
		}
		return err
	}
	return json.Unmarshal(value.([]byte), dest)
}

func (a *GitHubAdapter) queryHTTP(ctx context.Context, b *githubBinding, body []byte, gate *quotaQueue) ([]byte, error) {
	for attempt := 0; attempt < 3; attempt++ {
		if err := gate.waitQuota(ctx); err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, GitHubDiagnostic{"invalid-source", "invalid API endpoint"}
		}
		request.Header.Set("Authorization", "Bearer "+b.token)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/vnd.github+json")
		response, err := a.client().Do(request)
		if err != nil {
			return nil, GitHubDiagnostic{"offline", "GitHub API unavailable"}
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 8<<20+1))
		response.Body.Close()
		if readErr != nil || len(raw) > 8<<20 {
			return nil, GitHubDiagnostic{"invalid-response", "GitHub response unreadable"}
		}
		if response.StatusCode >= 300 && response.StatusCode < 400 {
			return nil, GitHubDiagnostic{"identity-changed", "GitHub redirected the configured repository; claims unavailable"}
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
				return nil, ScheduleDiagnostic{Code: "rate-limited", RetryAt: gate.retryDeadline()}
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
			gate.limitUntil(time.Now().Add(delay))
			if attempt == 0 && delay < 2*time.Second {
				timer := time.NewTimer(delay)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, ScheduleDiagnostic{Code: "rate-limited", RetryAt: time.Now().Add(delay)}
				case <-timer.C:
				}
				// The retry is safe; reset the local backoff after waiting.
				gate.mu.Lock()
				gate.retryAt = time.Time{}
				gate.mu.Unlock()
				continue
			}
			return nil, ScheduleDiagnostic{Code: "rate-limited", RetryAt: time.Now().Add(delay)}
		}
		switch response.StatusCode {
		case 401:
			return nil, GitHubDiagnostic{"authentication", "GitHub rejected the credential"}
		case 403:
			if strings.Contains(lower, "saml") || response.Header.Get("X-GitHub-SSO") != "" {
				return nil, GitHubDiagnostic{"saml-sso", "SAML authorization required"}
			}
			return nil, GitHubDiagnostic{"permission-denied", "GitHub denied access"}
		case 404:
			return nil, GitHubDiagnostic{"not-found-or-inaccessible", "resource not found or access unavailable"}
		case 410:
			return nil, GitHubDiagnostic{"gone", "GitHub resource unavailable"}
		}
		if response.StatusCode != 200 {
			return nil, GitHubDiagnostic{"provider-error", "GitHub request failed"}
		}
		if len(payload.Errors) > 0 {
			for _, issue := range payload.Errors {
				msg := strings.ToLower(issue.Message)
				switch strings.ToUpper(issue.Type) {
				case "INVALID_CURSOR":
					return nil, GitHubDiagnostic{"invalid-cursor", "GitHub pagination cursor expired"}
				case "FORBIDDEN":
					return nil, GitHubDiagnostic{"permission-denied", "GitHub denied access"}
				case "NOT_FOUND":
					return nil, GitHubDiagnostic{"not-found-or-inaccessible", "resource not found or access unavailable"}
				}
				if strings.Contains(msg, "cursor") && (strings.Contains(msg, "invalid") || strings.Contains(msg, "expired")) {
					return nil, GitHubDiagnostic{"invalid-cursor", "GitHub pagination cursor expired"}
				}
				if strings.Contains(msg, "rate limit") {
					return nil, GitHubDiagnostic{"rate-limited", "GitHub quota exhausted; retry later"}
				}
				if strings.Contains(msg, "saml") {
					return nil, GitHubDiagnostic{"saml-sso", "SAML authorization required"}
				}
				if strings.Contains(msg, "blockedby") || strings.Contains(msg, "subissues") || strings.Contains(msg, "parent") {
					return nil, GitHubDiagnostic{"dependency-unsupported", "GitHub dependency fields unavailable"}
				}
			}
			return nil, GitHubDiagnostic{"provider-error", "GitHub GraphQL request failed"}
		}
		if len(payload.Data) == 0 {
			return nil, GitHubDiagnostic{"invalid-response", "GitHub GraphQL data missing"}
		}
		if !json.Valid(payload.Data) {
			return nil, GitHubDiagnostic{"invalid-response", "GitHub GraphQL data invalid"}
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
		gate.limitUntil(reset)
		return payload.Data, nil
	}
	return nil, ScheduleDiagnostic{Code: "rate-limited", RetryAt: gate.retryDeadline()}
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
func (a *GitHubAdapter) item(source Source, issue githubIssue) Item {
	summary := a.summary(source, issue)
	return Item{Summary: summary, Body: issue.Body, TerminalKnown: true, Assignment: Assignment{Owners: summary.AssignedTo, Known: true, Assigned: len(summary.AssignedTo) > 0}, Observation: Observation{Principal: func() string {
		binding, _ := a.binding(source)
		if binding != nil {
			return binding.account
		}
		return ""
	}(), ObservedAt: time.Now(), ConfigurationGeneration: a.ConfigurationGeneration(source)}}
}

func (a *GitHubAdapter) BatchHydration() {}

func (a *GitHubAdapter) rememberNodeID(b *githubBinding, issue githubIssue) {
	a.mu.Lock()
	if b.nodeIDs == nil {
		b.nodeIDs = make(map[string]string)
	}
	b.nodeIDs[strconv.Itoa(issue.Number)] = issue.ID
	a.mu.Unlock()
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

// pollNewestHint is never authoritative: even a valid 304 only describes this
// exact REST page, not older issues, permissions, or dependency edges.
func (a *GitHubAdapter) pollNewestHint(ctx context.Context, source Source) {
	b, err := a.binding(source)
	if err != nil {
		return
	}
	origin := "https://api.github.com"
	if !strings.EqualFold(b.host, "github.com") {
		origin = "https://" + b.host + "/api/v3"
	}
	if a.APIBase != "" {
		origin = strings.TrimRight(a.APIBase, "/")
	}
	parts := strings.Split(b.repository, "/")
	endpoint := origin + "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/issues?state=all&sort=updated&direction=desc&per_page=1&page=1"
	const accept = "application/vnd.github+json"
	key := endpoint + "\x00" + b.account + "\x00" + accept + "\x00" + b.generation
	a.mu.Lock()
	etag := b.newestETags[key]
	a.mu.Unlock()
	gate := quotaScheduler("github-rest:"+strings.ToLower(b.host+"\x00"+b.account), 1)
	_, _ = gate.schedule(ctx, PriorityBackground, key+"\x00"+etag, "", true, func(workCtx context.Context) (any, error) {
		request, err := http.NewRequestWithContext(workCtx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Authorization", "Bearer "+b.token)
		request.Header.Set("Accept", accept)
		if etag != "" {
			request.Header.Set("If-None-Match", etag)
		}
		client := *a.client()
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		response, err := client.Do(request)
		if err != nil {
			return nil, err
		}
		response.Body.Close() // the representation is only a hint, never ingested
		if response.StatusCode == http.StatusOK {
			a.mu.Lock()
			if b.newestETags == nil {
				b.newestETags = make(map[string]string)
			}
			b.newestETags[key] = response.Header.Get("ETag")
			a.mu.Unlock()
		}
		return nil, nil
	})
}

const githubListQuery = `query($owner:String!,$repo:String!,$after:String,$count:Int!) { rateLimit { remaining resetAt } repository(owner:$owner,name:$repo) { nameWithOwner issues(first:$count,after:$after,orderBy:{field:CREATED_AT,direction:ASC},states:[OPEN,CLOSED]) { totalCount pageInfo { hasNextPage endCursor } nodes { id number title state stateReason updatedAt repository { nameWithOwner } assignees(first:100) { nodes { login } } } } } }`
const githubIncrementalQuery = `query($owner:String!,$repo:String!,$after:String,$count:Int!,$since:DateTime!) { rateLimit { remaining resetAt } repository(owner:$owner,name:$repo) { nameWithOwner issues(first:$count,after:$after,orderBy:{field:UPDATED_AT,direction:DESC},filterBy:{since:$since},states:[OPEN,CLOSED]) { totalCount pageInfo { hasNextPage endCursor } nodes { id number title state stateReason updatedAt repository { nameWithOwner } assignees(first:100) { nodes { login } } } } } }`

func (a *GitHubAdapter) ListIncremental(ctx context.Context, source Source, query Query, cursor string, committedWatermark, scanWatermark time.Time) (SummaryPage, error) {
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
	if cursor != "" {
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(cursor)
		if decodeErr != nil || len(decoded) == 0 {
			return SummaryPage{}, GitHubDiagnostic{"invalid-cursor", "invalid incremental cursor"}
		}
		after = string(decoded)
	}
	if scanWatermark.IsZero() {
		scanWatermark = time.Now().UTC()
	}
	since := committedWatermark
	if since.IsZero() {
		since = scanWatermark.Add(-30 * 24 * time.Hour)
	}
	// Revisit a small overlap so timestamp ties and updates around page boundaries
	// are observed again; node IDs are de-duplicated before returning each page.
	since = since.Add(-5 * time.Minute)
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
	err = a.query(ctx, b, githubIncrementalQuery, map[string]any{"owner": parts[0], "repo": parts[1], "after": after, "count": count, "since": since.UTC().Format(time.RFC3339Nano)}, &result)
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
		a.driftTo(b, result.Repository.NameWithOwner)
		return SummaryPage{}, GitHubDiagnostic{"identity-changed", "configured repository identity changed; claims unavailable until rebind"}
	}
	coverage := Coverage{State: CoverageComplete, Scope: source.ID, Total: result.Repository.Issues.TotalCount, TotalAccuracy: TotalExact}
	page := SummaryPage{Coverage: coverage, Observation: Observation{Principal: b.account, ObservedAt: time.Now(), Coverage: coverage, ConfigurationGeneration: b.generation}, Incremental: true}
	seen := make(map[string]bool, len(result.Repository.Issues.Nodes))
	for _, issue := range result.Repository.Issues.Nodes {
		if issue.ID == "" || issue.Number < 1 {
			return SummaryPage{}, GitHubDiagnostic{"invalid-response", "issue identity missing"}
		}
		if issue.Repository.NameWithOwner != b.repository {
			a.driftTo(b, issue.Repository.NameWithOwner)
			return SummaryPage{}, GitHubDiagnostic{"identity-changed", "issue transferred; claims unavailable until rebind"}
		}
		a.rememberNodeID(b, issue)
		if !seen[issue.ID] {
			seen[issue.ID] = true
			page.Items = append(page.Items, a.summary(source, issue))
		}
	}
	if result.Repository.Issues.PageInfo.HasNextPage {
		if result.Repository.Issues.PageInfo.EndCursor == "" {
			return SummaryPage{}, GitHubDiagnostic{"invalid-response", "pagination cursor missing"}
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(result.Repository.Issues.PageInfo.EndCursor))
		page.Coverage.State = CoveragePartial
	}
	return page, nil
}

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
	var scanStarted time.Time
	if cursor != "" {
		var state struct {
			ID      string `json:"id"`
			After   string `json:"after"`
			Started int64  `json:"started"`
		}
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(cursor)
		if decodeErr != nil || json.Unmarshal(decoded, &state) != nil || state.ID == "" || state.After == "" || state.Started == 0 {
			return SummaryPage{}, GitHubDiagnostic{"invalid-cursor", "invalid GitHub scan cursor"}
		}
		scanStarted = time.Unix(0, state.Started)
		if time.Since(scanStarted) > 24*time.Hour || scanStarted.After(time.Now().Add(time.Minute)) {
			return SummaryPage{}, GitHubDiagnostic{"invalid-cursor", "GitHub scan expired"}
		}
		a.mu.Lock()
		if b.scans == nil {
			// The scan cursor is durable but the seen-ID set is process-local.
			// Restore a valid cursor on a fresh adapter; the index deduplicates
			// persisted pages by immutable node ID. Still reject cursors evicted
			// from an already active adapter's bounded scan set.
			b.scans = map[string]map[string]string{state.ID: {}}
			b.scanOrder = append(b.scanOrder, state.ID)
		}
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
		scanStarted = time.Now().UTC()
		a.mu.Lock()
		if b.scans == nil {
			b.scans = map[string]map[string]string{}
		}
		const maxGitHubScans = 100
		for len(b.scanOrder) >= maxGitHubScans {
			delete(b.scans, b.scanOrder[0])
			b.scanOrder = b.scanOrder[1:]
		}
		b.scans[scanID] = map[string]string{}
		b.scanOrder = append(b.scanOrder, scanID)
		a.mu.Unlock()
	}
	keepScan := false
	defer func() {
		if !keepScan {
			a.mu.Lock()
			delete(b.scans, scanID)
			for i, id := range b.scanOrder {
				if id == scanID {
					b.scanOrder = append(b.scanOrder[:i], b.scanOrder[i+1:]...)
					break
				}
			}
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
		a.driftTo(b, result.Repository.NameWithOwner)
		return SummaryPage{}, GitHubDiagnostic{"identity-changed", "configured repository identity changed; claims unavailable until rebind"}
	}
	coverage := Coverage{State: CoverageComplete, Scope: source.ID, Total: result.Repository.Issues.TotalCount, TotalAccuracy: TotalExact}
	page := SummaryPage{Coverage: coverage, Observation: Observation{Principal: b.account, ObservedAt: time.Now(), Coverage: coverage, ConfigurationGeneration: b.generation}}
	for _, issue := range result.Repository.Issues.Nodes {
		if issue.ID == "" || issue.Number < 1 {
			return SummaryPage{}, GitHubDiagnostic{"invalid-response", "issue identity missing"}
		}
		if issue.Repository.NameWithOwner != b.repository {
			a.driftTo(b, issue.Repository.NameWithOwner)
			return SummaryPage{}, GitHubDiagnostic{"identity-changed", "issue transferred; claims unavailable until rebind"}
		}
		a.mu.Lock()
		seen, exists := b.scans[scanID]
		if !exists {
			a.mu.Unlock()
			return SummaryPage{}, GitHubDiagnostic{"invalid-cursor", "GitHub scan expired"}
		}
		// A node may move to a new issue number between pages. Suppress
		// identical repeats, but publish a changed reference so the index
		// and live snapshot replace the old identity atomically.
		ref := strconv.Itoa(issue.Number)
		duplicate := seen[issue.ID] == ref
		seen[issue.ID] = ref
		a.mu.Unlock()
		a.rememberNodeID(b, issue)
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
			ID      string `json:"id"`
			After   string `json:"after"`
			Started int64  `json:"started"`
		}{scanID, apiCursor, scanStarted.UnixNano()})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
		page.Coverage.State = CoveragePartial
		keepScan = true
	}
	return page, nil
}

// Comments are fetched only for an explicitly opened activity page; neither
// summary listing nor batch detail hydration requests them.
type GitHubComment struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
}

const githubCommentsQuery = `query($owner:String!,$repo:String!,$number:Int!,$after:String,$count:Int!) { rateLimit { remaining resetAt } repository(owner:$owner,name:$repo) { nameWithOwner issue(number:$number) { comments(first:$count,after:$after) { nodes { id body createdAt author { login } } pageInfo { hasNextPage endCursor } } } } }`

func (a *GitHubAdapter) ReadComments(ctx context.Context, source Source, ref Ref, cursor string, limit int) ([]GitHubComment, string, error) {
	b, err := a.binding(source)
	if err != nil {
		return nil, "", err
	}
	number, err := strconv.Atoi(ref.ItemID)
	if ref.SourceID != source.ID || err != nil || number < 1 {
		return nil, "", GitHubDiagnostic{"invalid-ref", "issue reference is invalid"}
	}
	if limit < 1 || limit > 100 {
		limit = 100
	}
	parts := strings.Split(b.repository, "/")
	var result struct {
		Repository *struct {
			NameWithOwner string `json:"nameWithOwner"`
			Issue         *struct {
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
	if err := a.query(ctx, b, githubCommentsQuery, map[string]any{"owner": parts[0], "repo": parts[1], "number": number, "after": after, "count": limit}, &result); err != nil {
		return nil, "", err
	}
	if result.Repository == nil || result.Repository.NameWithOwner != b.repository || result.Repository.Issue == nil {
		return nil, "", GitHubDiagnostic{"not-found-or-inaccessible", "issue not found or access unavailable"}
	}
	comments := result.Repository.Issue.Comments
	if comments.PageInfo.HasNextPage && comments.PageInfo.EndCursor == "" {
		return nil, "", GitHubDiagnostic{"invalid-response", "comments pagination cursor missing"}
	}
	out := make([]GitHubComment, 0, len(comments.Nodes))
	for _, node := range comments.Nodes {
		comment := GitHubComment{ID: node.ID, Body: node.Body, CreatedAt: node.CreatedAt}
		if node.Author != nil {
			comment.Author = node.Author.Login
		}
		out = append(out, comment)
	}
	if comments.PageInfo.HasNextPage {
		return out, comments.PageInfo.EndCursor, nil
	}
	return out, "", nil
}

const githubDetailQuery = `query($owner:String!,$repo:String!,$number:Int!) { rateLimit { remaining resetAt } repository(owner:$owner,name:$repo) { nameWithOwner issue(number:$number) { id number title body state stateReason updatedAt repository { nameWithOwner } assignees(first:100) { nodes { login } } } } }`
const githubNodesQuery = `query($ids:[ID!]!) { rateLimit { remaining resetAt } nodes(ids:$ids) { ... on Issue { id number title body state stateReason updatedAt repository { nameWithOwner } assignees(first:100) { nodes { login } } } } }`

func (a *GitHubAdapter) ReadItems(ctx context.Context, source Source, refs []Ref, _ []string, _ int) []ItemOutcome {
	outcomes := make([]ItemOutcome, len(refs))
	b, err := a.binding(source)
	batchIssues := map[string]githubIssue{}
	requestedIDs := map[string]string{}
	batchUsed := false
	var batchErr error
	if err == nil && len(refs) > 0 && len(refs) <= 100 {
		ids := make([]string, 0, len(refs))
		allKnown := true
		a.mu.Lock()
		for _, ref := range refs {
			id := b.nodeIDs[ref.ItemID]
			if id == "" {
				allKnown = false
				break
			}
			ids = append(ids, id)
			requestedIDs[ref.ItemID] = id
		}
		a.mu.Unlock()
		if allKnown {
			batchUsed = true
			var result struct {
				Nodes []*githubIssue `json:"nodes"`
			}
			batchErr = a.query(ctx, b, githubNodesQuery, map[string]any{"ids": ids}, &result)
			for _, issue := range result.Nodes {
				if issue != nil {
					batchIssues[issue.ID] = *issue
				}
			}
		}
	}
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
		if batchUsed {
			if batchErr != nil {
				if diagnostic, ok := batchErr.(GitHubDiagnostic); ok && (diagnostic.Code == "not-found-or-inaccessible" || diagnostic.Code == "permission-denied") {
					outcomes[i].Kind = "withheld"
				} else {
					outcomes[i].Err = batchErr
					outcomes[i].Kind = "failed"
				}
				continue
			}
			issue, exists := batchIssues[requestedIDs[ref.ItemID]]
			if !exists {
				outcomes[i].Kind = "withheld"
				continue
			}
			if issue.Repository.NameWithOwner != b.repository || issue.Number != number {
				a.driftTo(b, issue.Repository.NameWithOwner)
				outcomes[i].Kind = "withheld"
				continue
			}
			item := a.item(source, issue)
			outcomes[i].Kind = "found"
			outcomes[i].Item = &item
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
			if d, ok := readErr.(GitHubDiagnostic); ok {
				if d.Code == "identity-changed" {
					a.drift(b)
				}
				if d.Code == "not-found-or-inaccessible" || d.Code == "permission-denied" {
					outcomes[i].Kind = "withheld"
					continue
				}
			}
			outcomes[i].Err = readErr
			outcomes[i].Kind = "failed"
			continue
		}
		if result.Repository == nil || result.Repository.Issue == nil {
			outcomes[i].Kind = "withheld"
			continue
		}
		issue := *result.Repository.Issue
		if result.Repository.NameWithOwner != b.repository || issue.Repository.NameWithOwner != b.repository || issue.Number != number {
			a.driftTo(b, issue.Repository.NameWithOwner)
			outcomes[i].Kind = "withheld"
			continue
		}
		outcomes[i].Kind = "found"
		item := a.item(source, issue)
		outcomes[i].Item = &item
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
		After            string   `json:"after"`
		Count            int      `json:"count"`
		IDs              []string `json:"ids,omitempty"`
		HierarchyPartial bool     `json:"hierarchyPartial"`
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
		a.driftTo(b, issue.Repository.NameWithOwner)
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
		page.Edges = append(page.Edges, Relationship{Type: kind, Direction: DependentToPrerequisite, From: ref, To: to, Provenance: "github.blockedBy", Condition: "terminal", RawOutcome: related.State, Interpretation: "closed issue satisfies terminal; dependency removal may not change updatedAt, so this observation requires reconciliation or a fresh closure read", Fresh: true, Support: Supported})
	}
	seenIDs := make(map[string]bool, len(state.IDs)+len(issue.BlockedBy.Nodes))
	for _, id := range state.IDs {
		seenIDs[id] = true
	}
	for _, related := range issue.BlockedBy.Nodes {
		if related.ID != "" && !seenIDs[related.ID] {
			seenIDs[related.ID] = true
			state.IDs = append(state.IDs, related.ID)
		}
	}
	state.Count = len(state.IDs)
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
