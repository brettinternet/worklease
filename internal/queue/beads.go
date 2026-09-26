package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
)

// BeadsDiagnostic never includes provider output, paths, or issue content.
type BeadsDiagnostic struct{ Code, Detail string }

func (d BeadsDiagnostic) Error() string { return d.Code + ": " + d.Detail }

const beadsVersion = "1.3.0"
const beadsOutputLimit = 64 << 20

type beadsDependency struct {
	IssueID     string `json:"issue_id"`
	DependsOnID string `json:"depends_on_id"`
	Type        string `json:"type"`
}
type beadsIssue struct {
	ID              string            `json:"id"`
	Title           string            `json:"title"`
	Description     string            `json:"description"`
	Notes           string            `json:"notes"`
	Status          string            `json:"status"`
	Assignee        string            `json:"assignee"`
	Priority        int               `json:"priority"`
	UpdatedAt       time.Time         `json:"updated_at"`
	Dependencies    []beadsDependency `json:"dependencies"`
	DependencyCount int               `json:"dependency_count"`
	Revision        string            `json:"revision"`
	CommentCount    int               `json:"comment_count"`
}

// BeadsAdapter reads a single explicit checkout using the pinned bd JSON CLI.
// Bulk list includes complete dependencies. Reads are forced readonly and sandboxed:
// neither Dolt auto-commit nor remote auto-push is permitted on this path.
type BeadsAdapter struct {
	Binary     string
	Timeout    time.Duration
	mu         sync.Mutex
	scanMu     sync.Mutex
	bulk       map[string]map[string]beadsIssue
	ready      map[string]map[string]bool
	generation map[string]uint64
	consent    map[string]bool
	owners     map[string]string
	terminal   map[string]string
}

func NewBeadsAdapter() *BeadsAdapter {
	return &BeadsAdapter{bulk: map[string]map[string]beadsIssue{}, ready: map[string]map[string]bool{}, generation: map[string]uint64{}, consent: map[string]bool{}, owners: map[string]string{}, terminal: map[string]string{}}
}
func (*BeadsAdapter) OnDemandDetails() {}
func (a *BeadsAdapter) binary() string {
	if a.Binary != "" {
		return a.Binary
	}
	return "bd"
}
func (a *BeadsAdapter) timeout() time.Duration {
	if a.Timeout > 0 {
		return a.Timeout
	}
	return 20 * time.Second
}
func (a *BeadsAdapter) QueueCacheIdentity(source Source) (string, string, string, bool) {
	user, err := osuser.Current()
	if err != nil || user.Uid == "" {
		return "", "", "", false
	}
	git, err := os.Stat(filepath.Join(source.Locator, ".git"))
	if err != nil {
		return "", "", "", false
	}
	instance, ok := checkoutInstance(git)
	if !ok {
		return "", "", "", false
	}
	metadata, err := os.Stat(filepath.Join(source.Locator, ".beads", "metadata.json"))
	if err != nil {
		return "", "", "", false
	}
	a.mu.Lock()
	terminal := a.terminal[source.ID]
	a.mu.Unlock()
	return user.Uid, source.Locator, fmt.Sprintf("%s:%d:%d:%s", instance, metadata.Size(), metadata.ModTime().UnixNano(), terminal), true
}
func (a *BeadsAdapter) run(ctx context.Context, checkout string, write bool, args ...string) ([]byte, error) {
	if !(len(args) == 1 && args[0] == "version") && !(len(args) == 3 && args[0] == "config" && args[1] == "get" && args[2] == "sync.remote") {
		if err := a.authorizeRead(ctx, checkout); err != nil {
			return nil, err
		}
	}
	if !write {
		if len(args) == 1 && args[0] == "version" { /* version does not access the source */
		} else if len(args) == 3 && args[0] == "list" && args[1] == "--all" && args[2] == "--limit" {
			return nil, BeadsDiagnostic{"read-only", "invalid list command"}
		} else if !(len(args) == 3 && args[0] == "config" && args[1] == "get" && (args[2] == "export.auto" || args[2] == "sync.remote") || len(args) == 2 && args[0] == "vc" && args[1] == "status" || len(args) == 1 && args[0] == "statuses" || len(args) == 2 && (args[0] == "history" || args[0] == "comments") && args[1] != "" && !strings.HasPrefix(args[1], "-") || len(args) == 5 && args[0] == "list" && args[1] == "--all" && args[2] == "--limit" && args[3] == "0" && args[4] == "--brief" || len(args) == 6 && args[0] == "list" && args[1] == "--all" && args[2] == "--limit" && args[3] == "0" && args[4] == "--brief" && args[5] == "--ready" || len(args) == 2 && args[0] == "show" && args[1] != "" && !strings.HasPrefix(args[1], "-")) {
			return nil, BeadsDiagnostic{"read-only", "provider command is not a permitted read"}
		}
	}
	commandCtx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	argv := []string{"--json", "--sandbox", "-C", checkout}
	if write {
		argv = append(argv, "--dolt-auto-commit=on")
	} else {
		argv = append(argv, "--readonly")
	}
	argv = append(argv, args...)
	cmd := exec.CommandContext(commandCtx, a.binary(), argv...)
	prepareBacklogCommand(cmd)
	cmd.Dir = checkout
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "BEADS_") && !strings.HasPrefix(key, "BD_") && !strings.HasPrefix(key, "DOLT_") && !strings.HasPrefix(key, "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "BD_NON_INTERACTIVE=1", "BD_LAST_TOUCHED_FALLBACK=0")
	cmd.WaitDelay = time.Second
	var stdout, stderr limitedBuffer
	stdout.limit = beadsOutputLimit
	stdout.onLimit = cancel
	stderr.limit = 4096
	stderr.onLimit = cancel
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if stdout.exceeded || stderr.exceeded {
		return nil, BeadsDiagnostic{"output-limit", "provider output exceeded limit"}
	}
	if commandCtx.Err() != nil {
		return nil, BeadsDiagnostic{"cancelled", "provider command cancelled or timed out"}
	}
	if err != nil {
		return nil, BeadsDiagnostic{"provider-failed", "bd command failed"}
	}
	return stdout.data.Bytes(), nil
}
func (a *BeadsAdapter) Resolve(ctx context.Context, options map[string]string) (Source, error) {
	checkout := options["checkout"]
	if !filepath.IsAbs(checkout) {
		return Source{}, BeadsDiagnostic{"invalid-checkout", "absolute checkout required"}
	}
	checkout, err := filepath.EvalSymlinks(checkout)
	if err != nil {
		return Source{}, BeadsDiagnostic{"invalid-checkout", "checkout unavailable"}
	}
	if err := beadsLocalMetadata(checkout); err != nil {
		return Source{}, err
	}
	if _, err := exec.LookPath(a.binary()); err != nil {
		return Source{}, BeadsDiagnostic{"cli-missing", "bd CLI unavailable"}
	}
	if err := a.checkVersion(ctx, checkout); err != nil {
		return Source{}, err
	}
	source := Source{ID: options["id"], Name: options["id"], Locator: checkout, Adapter: "beads"}
	if source.ID == "" {
		source.ID = checkout
		source.Name = filepath.Base(checkout)
	}
	a.mu.Lock()
	if owner := a.owners[checkout]; owner != "" && owner != source.ID {
		a.mu.Unlock()
		return Source{}, BeadsDiagnostic{"duplicate-checkout", "one Beads checkout cannot have multiple source bindings"}
	}
	a.owners[checkout] = source.ID
	a.consent[checkout] = options["allowGitNetwork"] == "true"
	a.terminal[source.ID] = options["completeStatus"]
	a.mu.Unlock()
	if err := a.authorizeRead(ctx, checkout); err != nil {
		return Source{}, err
	}
	return source, nil
}
func beadsLocalMetadata(checkout string) error {
	path := filepath.Join(checkout, ".beads", "metadata.json")
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 16384 {
		return BeadsDiagnostic{"not-beads-project", "checkout has no safe Beads metadata"}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return BeadsDiagnostic{"not-beads-project", "Beads metadata unreadable"}
	}
	var metadata struct {
		Mode string `json:"dolt_mode"`
	}
	if err = json.Unmarshal(data, &metadata); err != nil || metadata.Mode != "embedded" {
		return BeadsDiagnostic{"unsupported-backend", "only embedded local Dolt is supported"}
	}
	return nil
}
func (a *BeadsAdapter) authorizeRead(ctx context.Context, checkout string) error {
	if err := beadsLocalMetadata(checkout); err != nil {
		return err
	}
	a.mu.Lock()
	consent, known := a.consent[checkout]
	a.mu.Unlock()
	if !known {
		return BeadsDiagnostic{"invalid-source", "source must be resolved first"}
	}
	data, err := a.run(ctx, checkout, false, "config", "get", "sync.remote")
	if err != nil {
		return err
	}
	var setting struct {
		Value string `json:"value"`
	}
	if err := beadsJSON(data, &setting); err != nil {
		return err
	}
	if setting.Value != "" && !consent {
		return BeadsDiagnostic{"git-network-consent", "allowGitNetwork: true required for a configured Dolt remote"}
	}
	return nil
}
func (a *BeadsAdapter) checkVersion(ctx context.Context, checkout string) error {
	data, err := a.run(ctx, checkout, false, "version")
	if err != nil {
		return err
	}
	var info struct {
		Version string `json:"version"`
	}
	if err := beadsJSON(data, &info); err != nil {
		return err
	}
	if info.Version != beadsVersion {
		return BeadsDiagnostic{"unsupported-version", "bd 1.3.0 required"}
	}
	return nil
}
func (a *BeadsAdapter) exportAuto(ctx context.Context, source Source) (bool, error) {
	data, err := a.run(ctx, source.Locator, false, "config", "get", "export.auto")
	if err != nil {
		return false, err
	}
	var setting struct {
		Value string `json:"value"`
	}
	if err = beadsJSON(data, &setting); err != nil {
		return false, err
	}
	if setting.Value != "true" && setting.Value != "false" {
		return false, BeadsDiagnostic{"invalid-config", "export.auto is not boolean"}
	}
	return setting.Value == "true", nil
}
func (a *BeadsAdapter) Capabilities(ctx context.Context, source Source, _ string, _ *Ref) (CapabilitySet, error) {
	export, err := a.exportAuto(ctx, source)
	if err != nil {
		return nil, err
	}
	remoteData, err := a.run(ctx, source.Locator, false, "config", "get", "sync.remote")
	if err != nil {
		return nil, err
	}
	var remote struct {
		Value string `json:"value"`
	}
	if err = beadsJSON(remoteData, &remote); err != nil {
		return nil, err
	}
	supported := func(m map[string]string) Capability {
		return Capability{Support: Supported, Permission: Allowed, Availability: Available, Semantics: m}
	}
	return CapabilitySet{
		"identity":        supported(map[string]string{"scope": "explicit-checkout", "keyPolicy": "explicit generic binding"}),
		"discovery":       supported(map[string]string{"pagination": "single complete bulk list", "total": "exact"}),
		"dependencies":    supported(map[string]string{"scope": "bulk typed edges", "readiness": "provider-reported only"}),
		"state":           supported(map[string]string{"mapping": "configured terminal statuses"}),
		"progress":        supported(map[string]string{"operations": "append notes or comment"}),
		"assignment":      supported(map[string]string{"cardinality": "single"}),
		"native-claims":   {Support: Unsupported, Permission: Denied, Availability: Available},
		"mutation":        supported(map[string]string{"conditional": "preflight and read-back", "method": "bd update/comment"}),
		"synchronization": supported(map[string]string{"invalidation": "complete bulk list each refresh"}),
		"effects":         supported(map[string]string{"network": fmt.Sprint(remote.Value != ""), "commit": "Dolt on writes only", "hooks": "false (no Git hooks)", "git-tracked-export": fmt.Sprint(export)}),
	}, nil
}
func beadsJSON(data []byte, result any) error {
	if err := json.Unmarshal(data, result); err != nil {
		return BeadsDiagnostic{"schema-mismatch", "bd returned invalid JSON"}
	}
	return nil
}
func (a *BeadsAdapter) summary(source Source, issue beadsIssue, ready *bool) Summary {
	state := StateOpen
	a.mu.Lock()
	terminal := a.terminal[source.ID]
	a.mu.Unlock()
	switch {
	case issue.Status == "closed" || issue.Status == "tombstone" || terminal != "" && issue.Status == terminal:
		state = StateComplete
	case issue.Status == "in_progress":
		state = StateInProgress
	case issue.Status == "blocked" || issue.Status == "deferred":
		state = StateBlocked
	}
	var assigned []string
	if issue.Assignee != "" {
		assigned = []string{issue.Assignee}
	}
	return Summary{Ref: Ref{SourceID: source.ID, ItemID: issue.ID}, Title: issue.Title, RawStatus: issue.Status, State: state, Priority: issue.Priority, CanonicalID: source.ID + "\x00" + issue.ID, ProviderReady: ready, AssignedTo: assigned, UpdatedAt: issue.UpdatedAt, Fresh: true, Terminal: state == StateComplete, ProviderBlocked: state == StateBlocked}
}
func (a *BeadsAdapter) listCommit(ctx context.Context, source Source) (string, error) {
	data, err := a.run(ctx, source.Locator, false, "vc", "status")
	if err != nil {
		return "", err
	}
	var status struct {
		Commit string `json:"commit"`
	}
	if err = beadsJSON(data, &status); err != nil || status.Commit == "" {
		return "", BeadsDiagnostic{"schema-mismatch", "bd vc status omitted commit"}
	}
	return status.Commit, nil
}
func (a *BeadsAdapter) List(ctx context.Context, source Source, query Query, cursor string) (SummaryPage, error) {
	a.scanMu.Lock()
	defer a.scanMu.Unlock()
	return a.list(ctx, source, query, cursor)
}

func (a *BeadsAdapter) list(ctx context.Context, source Source, _ Query, cursor string) (SummaryPage, error) {
	if err := a.checkVersion(ctx, source.Locator); err != nil {
		return SummaryPage{}, err
	}
	if cursor != "" {
		return SummaryPage{}, BeadsDiagnostic{"invalid-cursor", "bd list has no cursor"}
	}
	before, err := a.listCommit(ctx, source)
	if err != nil {
		return SummaryPage{}, err
	}
	data, err := a.run(ctx, source.Locator, false, "list", "--all", "--limit", "0", "--brief")
	if err != nil {
		return SummaryPage{}, err
	}
	var issues []beadsIssue
	if err = beadsJSON(data, &issues); err != nil || issues == nil {
		return SummaryPage{}, BeadsDiagnostic{"schema-mismatch", "bd list missing issue array"}
	}
	readyData, err := a.run(ctx, source.Locator, false, "list", "--all", "--limit", "0", "--brief", "--ready")
	if err != nil {
		return SummaryPage{}, err
	}
	after, err := a.listCommit(ctx, source)
	if err != nil {
		return SummaryPage{}, err
	}
	if before != after {
		return SummaryPage{}, BeadsDiagnostic{"observation-invalidated", "Dolt commit changed during bulk read"}
	}
	// The commit stays fixed during another client's batch-mode writes. A
	// second bulk observation must agree before its edges can be published.
	checkData, err := a.run(ctx, source.Locator, false, "list", "--all", "--limit", "0", "--brief")
	if err != nil {
		return SummaryPage{}, err
	}
	var check []beadsIssue
	if err = beadsJSON(checkData, &check); err != nil || check == nil || !reflect.DeepEqual(issues, check) {
		return SummaryPage{}, BeadsDiagnostic{"observation-invalidated", "Beads issues changed during bulk read"}
	}
	var readyIssues []beadsIssue
	if err = beadsJSON(readyData, &readyIssues); err != nil || readyIssues == nil {
		return SummaryPage{}, BeadsDiagnostic{"schema-mismatch", "bd ready list missing issue array"}
	}
	ready := make(map[string]bool, len(readyIssues))
	for _, issue := range readyIssues {
		ready[issue.ID] = true
	}
	page := SummaryPage{Items: make([]Summary, 0, len(issues)), Coverage: Coverage{State: CoverageComplete, Scope: source.ID, Total: len(issues), TotalAccuracy: TotalExact}}
	page.Observation = Observation{AccessScope: source.Locator, ObservedAt: time.Now(), ProviderVersion: beadsVersion, Coverage: page.Coverage, ConfigurationGeneration: source.Locator}
	seen := make(map[string]bool, len(issues))
	bulk := make(map[string]beadsIssue, len(issues))
	for _, issue := range issues {
		if issue.ID == "" || issue.Title == "" || issue.Status == "" || issue.DependencyCount > 0 && issue.Dependencies == nil {
			return SummaryPage{}, BeadsDiagnostic{"schema-mismatch", "bd list omitted required issue or dependency fields"}
		}
		if seen[issue.ID] {
			return SummaryPage{}, BeadsDiagnostic{"duplicate-item-id", "bd list contains duplicate IDs"}
		}
		for _, dep := range issue.Dependencies {
			if dep.IssueID != issue.ID || dep.DependsOnID == "" || dep.Type == "" {
				return SummaryPage{}, BeadsDiagnostic{"schema-mismatch", "bd list contained malformed dependency"}
			}
		}
		seen[issue.ID] = true
		bulk[issue.ID] = issue
		r := ready[issue.ID]
		page.Items = append(page.Items, a.summary(source, issue, &r))
	}
	for id := range ready {
		if !seen[id] {
			return SummaryPage{}, BeadsDiagnostic{"observation-invalidated", "ready list and bulk list disagree"}
		}
	}
	a.mu.Lock()
	a.bulk[source.ID] = bulk
	a.ready[source.ID] = ready
	a.generation[source.ID]++
	a.mu.Unlock()
	return page, nil
}
func (a *BeadsAdapter) readIssue(ctx context.Context, source Source, ref Ref) (beadsIssue, error) {
	if err := a.checkVersion(ctx, source.Locator); err != nil {
		return beadsIssue{}, err
	}
	if ref.SourceID != source.ID || ref.ItemID == "" || strings.HasPrefix(ref.ItemID, "-") {
		return beadsIssue{}, BeadsDiagnostic{"invalid-ref", "item outside source"}
	}
	data, err := a.run(ctx, source.Locator, false, "show", ref.ItemID)
	if err != nil {
		return beadsIssue{}, err
	}
	var issues []beadsIssue
	if err = beadsJSON(data, &issues); err != nil || len(issues) != 1 || issues[0].ID != ref.ItemID || issues[0].Status == "" {
		return beadsIssue{}, BeadsDiagnostic{"identity-mismatch", "bd show did not return requested issue"}
	}
	return issues[0], nil
}
func (a *BeadsAdapter) ReadItems(ctx context.Context, source Source, refs []Ref, _ []string, _ int) []ItemOutcome {
	out := make([]ItemOutcome, len(refs))
	for i, ref := range refs {
		out[i].Ref = ref
		issue, err := a.readIssue(ctx, source, ref)
		if err != nil {
			out[i].Kind = "failed"
			out[i].Err = err
			continue
		}
		a.mu.Lock()
		ready := a.ready[source.ID][issue.ID]
		a.mu.Unlock()
		summary := a.summary(source, issue, &ready)
		out[i].Kind = "found"
		out[i].Item = &Item{Summary: summary, Body: issue.Description, TerminalKnown: true, Assignment: Assignment{Owners: summary.AssignedTo, Assigned: len(summary.AssignedTo) > 0, Known: true}, Observation: Observation{ObservedAt: time.Now(), ProviderVersion: beadsVersion, ConfigurationGeneration: source.Locator}}
	}
	return out
}
func (a *BeadsAdapter) dependencyPage(source Source, ref Ref, issue beadsIssue) DependencyPage {
	edges := make([]Relationship, 0, len(issue.Dependencies))
	for _, dep := range issue.Dependencies {
		if dep.IssueID != issue.ID || dep.DependsOnID == "" {
			continue
		}
		other := Ref{SourceID: source.ID, ItemID: dep.DependsOnID}
		relation := Relationship{From: ref, To: other, Type: Related, Direction: NonBlockingDirection, Provenance: "beads.dependencies", RawOutcome: dep.Type, Interpretation: "informational", Fresh: true, Support: Supported}
		switch dep.Type {
		case "blocks":
			relation.Type = HardPrerequisite
			relation.Direction = DependentToPrerequisite
			relation.Condition = "terminal"
			relation.Interpretation = "configured completion status"
		case "parent-child":
			relation.Type = ParentChild
			relation.Direction = ParentToChild
			relation.From = other
			relation.To = ref
		case "related", "discovered-from", "tracks", "caused-by", "validates", "relates-to", "supersedes":
		default:
			relation.Support = SupportUnknown
		}
		edges = append(edges, relation)
	}
	return DependencyPage{Edges: edges, Completeness: CoverageComplete, Observation: Observation{ObservedAt: time.Now(), ProviderVersion: beadsVersion, Coverage: Coverage{State: CoverageComplete, Scope: ref.String(), TotalAccuracy: TotalExact}}}
}
func (a *BeadsAdapter) CachedEdges(source Source, ref Ref) (DependencyPage, bool) {
	a.mu.Lock()
	issue, ok := a.bulk[source.ID][ref.ItemID]
	a.mu.Unlock()
	if !ok {
		return DependencyPage{}, false
	}
	return a.dependencyPage(source, ref, issue), true
}
func (a *BeadsAdapter) ReadDependencies(ctx context.Context, source Source, ref Ref, cursor string, _ int) (DependencyPage, error) {
	if cursor != "" {
		return DependencyPage{}, BeadsDiagnostic{"invalid-cursor", "bd dependencies have no cursor"}
	}
	if page, ok := a.CachedEdges(source, ref); ok {
		return page, nil
	}
	if _, err := a.readIssue(ctx, source, ref); err != nil {
		return DependencyPage{}, err
	}
	// show uses dependency_type and nested issues, not the bulk edge schema.
	// Read the complete bulk source instead of interpreting a lossy show result.
	if _, err := a.List(ctx, source, Query{}, ""); err != nil {
		return DependencyPage{}, err
	}
	if page, ok := a.CachedEdges(source, ref); ok {
		return page, nil
	}
	return DependencyPage{}, BeadsDiagnostic{"identity-changed", "issue absent from complete bulk list"}
}
func (a *BeadsAdapter) RefreshActionClosure(ctx context.Context, source Source, ref Ref) (map[string]Item, error) {
	a.scanMu.Lock()
	defer a.scanMu.Unlock()
	page, err := a.list(ctx, source, Query{}, "")
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Summary, len(page.Items))
	for _, summary := range page.Items {
		byID[summary.Ref.ItemID] = summary
	}
	a.mu.Lock()
	bulk := a.bulk[source.ID]
	a.mu.Unlock()
	result := make(map[string]Item)
	visited := make(map[string]bool)
	var walk func(Ref) error
	walk = func(current Ref) error {
		if visited[current.ItemID] {
			return nil
		}
		visited[current.ItemID] = true
		summary, ok := byID[current.ItemID]
		if !ok {
			return BeadsDiagnostic{"dependency-unknown", "prerequisite absent from complete list"}
		}
		issue, ok := bulk[current.ItemID]
		if !ok {
			return BeadsDiagnostic{"observation-invalidated", "Beads dependency snapshot is incomplete"}
		}
		edges := a.dependencyPage(source, current, issue)
		item := Item{Summary: summary, ReadOutcome: "found", TerminalKnown: true, DependenciesKnown: true, Closure: CoverageComplete, Relationships: edges.Edges, ReadPermission: Allowed, Observation: edges.Observation}
		for _, edge := range edges.Edges {
			if edge.Type == HardPrerequisite {
				item.Dependencies = append(item.Dependencies, edge.To)
				if err := walk(edge.To); err != nil {
					return err
				}
			}
		}
		result[current.Key()] = item
		return nil
	}
	if err := walk(ref); err != nil {
		return nil, err
	}
	// The generic evaluator handles cycles, terminal state, and unsupported conditions.
	return Recompute(result, CoverageComplete), nil
}

// Poll the complete bulk source periodically; uncommitted batch writes and
// concurrent Dolt updates cannot be inferred from a Git file timestamp.
func (*BeadsAdapter) WatchChanges(ctx context.Context, _ Source, changed func()) error {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			changed()
		}
	}
}
func (a *BeadsAdapter) IdentityChange(Source) string { return "" }

var _ Adapter = (*BeadsAdapter)(nil)
