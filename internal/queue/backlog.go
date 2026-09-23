package queue

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BacklogDiagnostic is safe to display: subprocess output and paths are never embedded.
type BacklogDiagnostic struct{ Code, Detail string }

func (d BacklogDiagnostic) Error() string { return d.Code + ": " + d.Detail }

func (a *BacklogAdapter) QueueCacheIdentity(source Source) (string, string, string, bool) {
	current, err := osuser.Current()
	if err != nil || current.Uid == "" || source.Locator == "" {
		return "", "", "", false
	}
	// A checkout replaced at the same path must not inherit its predecessor's cache.
	gitDir, err := os.Stat(filepath.Join(source.Locator, ".git"))
	if err != nil {
		return "", "", "", false
	}
	generation, ok := checkoutInstance(gitDir)
	if !ok {
		return "", "", "", false
	}
	for _, name := range []string{"backlog.config.yml", filepath.Join("backlog", "config.yml")} {
		content, err := os.ReadFile(filepath.Join(source.Locator, name))
		if err == nil {
			sum := sha256.Sum256(content)
			generation += ":" + hex.EncodeToString(sum[:])
		} else if !os.IsNotExist(err) {
			return "", "", "", false
		}
	}
	return current.Uid, filepath.Clean(source.Locator), generation, true
}

type BacklogSourceDiagnostics struct {
	Branch         string   `json:"branch"`
	Head           string   `json:"head"`
	Dirty          bool     `json:"dirty"`
	DuplicateIDs   []string `json:"duplicateIds,omitempty"`
	NetworkEffects bool     `json:"networkEffects"`
	CommitEffects  bool     `json:"commitEffects"`
	HookEffects    bool     `json:"hookEffects"`
}

const backlogOutputLimit = 8 << 20
const backlogTimeout = 15 * time.Second

// BacklogAdapter reads only the configured checkout. TerminalStatuses is the caller's
// project status mapping; provider isReady is never used to infer generic readiness.
type BacklogAdapter struct {
	TerminalStatuses map[string]bool
	Binary           string
	Timeout          time.Duration
	mu               sync.Mutex
	diagnostics      map[string]BacklogSourceDiagnostics
	consent          map[string]bool
	details          map[string]backlogTask
}

func NewBacklogAdapter(terminalStatuses ...string) *BacklogAdapter {
	statuses := make(map[string]bool, len(terminalStatuses))
	for _, s := range terminalStatuses {
		statuses[s] = true
	}
	return &BacklogAdapter{TerminalStatuses: statuses, diagnostics: map[string]BacklogSourceDiagnostics{}, consent: map[string]bool{}, details: map[string]backlogTask{}}
}

// OnDemandDetails prevents the loader from scanning every task with a view subprocess.
func (*BacklogAdapter) OnDemandDetails() {}

func (a *BacklogAdapter) Diagnostics(source Source) BacklogSourceDiagnostics {
	a.mu.Lock()
	defer a.mu.Unlock()
	d := a.diagnostics[source.ID]
	d.DuplicateIDs = append([]string(nil), d.DuplicateIDs...)
	return d
}
func (a *BacklogAdapter) binary() string {
	if a.Binary != "" {
		return a.Binary
	}
	return "backlog"
}
func (a *BacklogAdapter) timeout() time.Duration {
	if a.Timeout > 0 {
		return a.Timeout
	}
	return backlogTimeout
}

// backlogReadCommand restricts the adapter subprocess boundary, including
// dynamically assembled commands. No provider mutation can cross this seam.
func backlogReadCommand(binary string, args []string) bool {
	if binary == "git" {
		return len(args) == 2 && args[0] == "rev-parse" && (args[1] == "HEAD" || args[1] == "--is-inside-work-tree") ||
			len(args) == 3 && args[0] == "rev-parse" && args[1] == "--abbrev-ref" && args[2] == "HEAD" ||
			len(args) == 3 && args[0] == "status" && args[1] == "--porcelain" && args[2] == "--untracked-files=normal"
	}
	if filepath.Base(binary) != "backlog" {
		return false
	}
	return len(args) == 1 && args[0] == "--version" ||
		len(args) == 3 && args[0] == "config" && args[1] == "get" && (args[2] == "remoteOperations" || args[2] == "checkActiveBranches" || args[2] == "autoCommit" || args[2] == "bypassGitHooks") ||
		len(args) == 3 && args[0] == "task" && args[1] == "list" && args[2] == "--json" ||
		len(args) == 4 && args[0] == "task" && args[1] == "view" && args[2] != "" && args[3] == "--json"
}

// run bounds wall time and stdout/stderr, kills the process on cancellation,
// and never returns raw stderr (which can contain secrets or terminal controls).
func (a *BacklogAdapter) run(ctx context.Context, cwd, binary string, args ...string) ([]byte, error) {
	if !backlogReadCommand(binary, args) {
		return nil, BacklogDiagnostic{"read-only", "queue provider command is not a permitted read"}
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	priority := PriorityBackground
	if len(args) >= 2 && args[0] == "task" {
		priority = PriorityVisible
		if args[1] == "view" {
			priority = PriorityDetail
		}
	}
	gate := quotaScheduler("backlog:"+cwd, 4)
	key := binary + "\x00" + strings.Join(args, "\x00")
	result, err := gate.schedule(ctx, priority, key, "", true, func(workCtx context.Context) (any, error) {
		return a.runCommand(workCtx, cwd, binary, args...)
	})
	if err != nil {
		if _, ok := err.(ScheduleDiagnostic); ok {
			return nil, BacklogDiagnostic{"overloaded", "provider request capacity unavailable"}
		}
		if ctx.Err() != nil {
			return nil, BacklogDiagnostic{"cancelled", "provider read cancelled or timed out"}
		}
		return nil, err
	}
	return result.([]byte), nil
}

func (a *BacklogAdapter) runCommand(ctx context.Context, cwd, binary string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = cwd
	// Ignore inherited directory overrides: only the configured checkout is a source.
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "BACKLOG_CWD" && !strings.HasPrefix(key, "GIT_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	// A CLI may spawn children that inherit stdout; bound pipe draining after cancellation.
	cmd.WaitDelay = time.Second
	var stdout, stderr limitedBuffer
	stdout.limit = backlogOutputLimit
	stderr.limit = 4096
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if stdout.exceeded || stderr.exceeded {
		return nil, BacklogDiagnostic{"output-limit", "provider output exceeded limit"}
	}
	if ctx.Err() != nil {
		return nil, BacklogDiagnostic{"cancelled", "provider read cancelled or timed out"}
	}
	if err != nil {
		return nil, BacklogDiagnostic{"provider-failed", "provider command failed"}
	}
	return stdout.data.Bytes(), nil
}

type limitedBuffer struct {
	data     bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.data.Len() {
		b.exceeded = true
		return 0, errors.New("output limit")
	}
	return b.data.Write(p)
}

func (a *BacklogAdapter) Resolve(ctx context.Context, options map[string]string) (Source, error) {
	checkout := options["checkout"]
	if !filepath.IsAbs(checkout) {
		return Source{}, BacklogDiagnostic{"invalid-checkout", "absolute checkout required"}
	}
	checkout, err := filepath.EvalSymlinks(checkout)
	if err != nil {
		return Source{}, BacklogDiagnostic{"invalid-checkout", "checkout unavailable"}
	}
	rootConfig, rootErr := os.Stat(filepath.Join(checkout, "backlog.config.yml"))
	folderConfig, folderErr := os.Stat(filepath.Join(checkout, "backlog", "config.yml"))
	if (rootErr != nil || rootConfig.IsDir()) && (folderErr != nil || folderConfig.IsDir()) {
		return Source{}, BacklogDiagnostic{"not-backlog-project", "checkout has no Backlog.md project"}
	}
	binary, err := exec.LookPath(a.binary())
	if err != nil {
		return Source{}, BacklogDiagnostic{"cli-missing", "backlog CLI unavailable"}
	}
	version, err := a.run(ctx, checkout, binary, "--version")
	if err != nil {
		return Source{}, err
	}
	match := regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)`).FindStringSubmatch(strings.TrimSpace(string(version)))
	if len(match) != 4 || match[1] != "1" {
		return Source{}, BacklogDiagnostic{"unsupported-version", "Backlog.md 1.52.x required"}
	}
	minor, _ := strconv.Atoi(match[2])
	if minor != 52 {
		return Source{}, BacklogDiagnostic{"unsupported-version", "Backlog.md 1.52.x required"}
	}
	effects := BacklogSourceDiagnostics{}
	for _, setting := range []struct {
		key  string
		flag *bool
	}{{"remoteOperations", &effects.NetworkEffects}, {"checkActiveBranches", &effects.NetworkEffects}, {"autoCommit", &effects.CommitEffects}, {"bypassGitHooks", &effects.HookEffects}} {
		value, err := a.run(ctx, checkout, binary, "config", "get", setting.key)
		if err != nil {
			return Source{}, err
		}
		flag, err := strconv.ParseBool(strings.TrimSpace(string(value)))
		if err != nil {
			return Source{}, BacklogDiagnostic{"invalid-config", "invalid project effects setting"}
		}
		if setting.key == "checkActiveBranches" {
			effects.NetworkEffects = effects.NetworkEffects || flag
		} else {
			*setting.flag = flag
		}
	}
	effects.HookEffects = effects.CommitEffects && !effects.HookEffects
	if effects.NetworkEffects && options["allowGitNetwork"] != "true" {
		return Source{}, BacklogDiagnostic{"git-network-consent", "allowGitNetwork: true required for this project's Git settings"}
	}
	if _, err := exec.LookPath("git"); err != nil {
		return Source{}, BacklogDiagnostic{"git-missing", "Git unavailable"}
	}
	a.gitFreshness(ctx, checkout, &effects)
	source := Source{ID: options["id"], Name: options["id"], Locator: checkout, Adapter: "backlog-md"}
	if source.ID == "" {
		source.ID = checkout
		source.Name = filepath.Base(checkout)
	}
	a.mu.Lock()
	a.diagnostics[source.ID] = effects
	a.consent[source.ID] = options["allowGitNetwork"] == "true"
	a.mu.Unlock()
	return source, nil
}

// Check settings again before task reads: project configuration can change after Resolve.
func (a *BacklogAdapter) authorizeRead(ctx context.Context, source Source) error {
	a.mu.Lock()
	consent, known := a.consent[source.ID]
	a.mu.Unlock()
	if !known {
		return BacklogDiagnostic{"invalid-source", "source must be resolved first"}
	}
	if consent {
		return nil
	}
	for _, key := range []string{"remoteOperations", "checkActiveBranches"} {
		value, err := a.run(ctx, source.Locator, a.binary(), "config", "get", key)
		if err != nil {
			return err
		}
		enabled, err := strconv.ParseBool(strings.TrimSpace(string(value)))
		if err != nil {
			return BacklogDiagnostic{"invalid-config", "invalid project effects setting"}
		}
		if enabled {
			return BacklogDiagnostic{"git-network-consent", "allowGitNetwork: true required for this project's Git settings"}
		}
	}
	return nil
}

func (a *BacklogAdapter) gitFreshness(ctx context.Context, checkout string, d *BacklogSourceDiagnostics) {
	if b, err := a.run(ctx, checkout, "git", "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		d.Branch = strings.TrimSpace(string(b))
	}
	if b, err := a.run(ctx, checkout, "git", "rev-parse", "HEAD"); err == nil {
		d.Head = strings.TrimSpace(string(b))
	}
	if b, err := a.run(ctx, checkout, "git", "status", "--porcelain", "--untracked-files=normal"); err == nil {
		d.Dirty = len(b) > 0
	}
}

func (a *BacklogAdapter) Capabilities(_ context.Context, source Source, _ string, _ *Ref) (CapabilitySet, error) {
	d := a.Diagnostics(source)
	supported := func(semantics map[string]string) Capability {
		return Capability{Support: Supported, Permission: Allowed, Availability: Available, Semantics: semantics}
	}
	readonly := func(semantics map[string]string) Capability {
		return Capability{Support: Supported, Permission: Denied, Availability: Available, Semantics: semantics, Reason: "queue adapter is read-only"}
	}
	return CapabilitySet{
		"identity":        supported(map[string]string{"scope": "explicit-checkout", "keyPolicy": "backlog-md or explicit generic binding", "duplicateRepair": "renumbers IDs"}),
		"discovery":       supported(map[string]string{"pagination": "single complete list", "total": "exact"}),
		"dependencies":    supported(map[string]string{"scope": "intra-project per-item view", "readiness": "provider-reported only"}),
		"state":           supported(map[string]string{"mapping": "caller-declared terminal statuses"}),
		"progress":        readonly(map[string]string{"operations": "append notes, comments; criterion index disabled"}),
		"assignment":      readonly(map[string]string{"cardinality": "multiple", "write": "replace-all"}),
		"native-claims":   {Support: Unsupported, Permission: Denied, Availability: Available},
		"mutation":        readonly(map[string]string{"conditional": "false", "method": "CLI edit"}),
		"synchronization": supported(map[string]string{"watch": "task list --json --watch", "updatedAt": "minute resolution; not lossless"}),
		"effects":         supported(map[string]string{"network": strconv.FormatBool(d.NetworkEffects), "commit": strconv.FormatBool(d.CommitEffects), "hooks": strconv.FormatBool(d.HookEffects)}),
		"authentication":  supported(map[string]string{"method": "local OS checkout access"}),
	}, nil
}

type backlogTask struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Status       string    `json:"status"`
	Priority     string    `json:"priority"`
	Ordinal      int       `json:"ordinal"`
	Assignees    []string  `json:"assignees"`
	UpdatedAt    time.Time `json:"updatedAt"`
	IsReady      bool      `json:"isReady"`
	ParentTaskID string    `json:"parentTaskId"`
	Dependencies []string  `json:"dependencies"`
	Description  string    `json:"description"`
	Readiness    struct {
		MissingDependencies []string `json:"missingDependencies"`
	} `json:"readiness"`
}

func decodeBacklog(data []byte, kind string, dest any) error {
	var header struct {
		Kind          string `json:"kind"`
		SchemaVersion int    `json:"schemaVersion"`
	}
	if err := json.Unmarshal(data, &header); err != nil || header.Kind != kind || header.SchemaVersion != 1 {
		return BacklogDiagnostic{"schema-mismatch", "unexpected Backlog.md JSON kind or version"}
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return BacklogDiagnostic{"schema-mismatch", "invalid Backlog.md JSON payload"}
	}
	return nil
}
func (a *BacklogAdapter) summary(source Source, task backlogTask) Summary {
	state := StateOpen
	if a.TerminalStatuses[task.Status] {
		state = StateComplete
	} else if task.Status == "In Progress" {
		state = StateInProgress
	}
	ready := task.IsReady
	return Summary{Ref: Ref{SourceID: source.ID, ItemID: task.ID}, Title: task.Title, RawStatus: task.Status, State: state, Order: fmt.Sprintf("%012d", task.Ordinal), Priority: map[string]int{"high": 1, "medium": 2, "low": 3}[task.Priority], CanonicalID: source.ID + "\x00" + task.ID, ProviderReady: &ready, AssignedTo: task.Assignees, UpdatedAt: task.UpdatedAt, Fresh: true, Terminal: state == StateComplete}
}
func (a *BacklogAdapter) List(ctx context.Context, source Source, _ Query, cursor string) (SummaryPage, error) {
	if err := a.authorizeRead(ctx, source); err != nil {
		return SummaryPage{}, err
	}
	if cursor != "" {
		return SummaryPage{}, BacklogDiagnostic{"invalid-cursor", "Backlog.md list has no cursor"}
	}
	data, err := a.run(ctx, source.Locator, a.binary(), "task", "list", "--json")
	if err != nil {
		return SummaryPage{}, err
	}
	var payload struct {
		Tasks []backlogTask `json:"tasks"`
	}
	if err := decodeBacklog(data, "task-list", &payload); err != nil {
		return SummaryPage{}, err
	}
	if payload.Tasks == nil {
		return SummaryPage{}, BacklogDiagnostic{"schema-mismatch", "missing tasks array"}
	}
	page := SummaryPage{Items: make([]Summary, 0, len(payload.Tasks)), Coverage: Coverage{State: CoverageComplete, Scope: source.ID, Total: len(payload.Tasks), TotalAccuracy: TotalExact}}
	page.Observation = Observation{AccessScope: source.Locator, ObservedAt: time.Now(), Coverage: page.Coverage, ConfigurationGeneration: source.Locator}
	seen := map[string]bool{}
	duplicates := map[string]bool{}
	for _, task := range payload.Tasks {
		if task.ID == "" {
			return SummaryPage{}, BacklogDiagnostic{"schema-mismatch", "task without ID"}
		}
		if seen[task.ID] {
			duplicates[task.ID] = true
		}
		seen[task.ID] = true
		page.Items = append(page.Items, a.summary(source, task))
	}
	a.mu.Lock()
	d := a.diagnostics[source.ID]
	a.mu.Unlock()
	a.gitFreshness(ctx, source.Locator, &d)
	a.mu.Lock()
	d.DuplicateIDs = d.DuplicateIDs[:0]
	for id := range duplicates {
		d.DuplicateIDs = append(d.DuplicateIDs, id)
	}
	sort.Strings(d.DuplicateIDs)
	a.diagnostics[source.ID] = d
	a.mu.Unlock()
	return page, nil
}
func (a *BacklogAdapter) view(ctx context.Context, source Source, ref Ref) (backlogTask, error) {
	if err := a.authorizeRead(ctx, source); err != nil {
		return backlogTask{}, err
	}
	if ref.SourceID != source.ID || ref.ItemID == "" || strings.HasPrefix(ref.ItemID, "-") {
		return backlogTask{}, BacklogDiagnostic{"invalid-ref", "item not in this source"}
	}
	data, err := a.run(ctx, source.Locator, a.binary(), "task", "view", ref.ItemID, "--json")
	if err != nil {
		return backlogTask{}, err
	}
	var payload struct {
		Task *backlogTask `json:"task"`
	}
	if err := decodeBacklog(data, "task-view", &payload); err != nil {
		return backlogTask{}, err
	}
	if payload.Task == nil || payload.Task.ID != ref.ItemID {
		return backlogTask{}, BacklogDiagnostic{"identity-mismatch", "view did not return requested task"}
	}
	a.mu.Lock()
	a.details[ref.Key()] = *payload.Task
	a.mu.Unlock()
	return *payload.Task, nil
}
func (a *BacklogAdapter) ReadItems(ctx context.Context, source Source, refs []Ref, _ []string, _ int) []ItemOutcome {
	outcomes := make([]ItemOutcome, len(refs))
	var wg sync.WaitGroup
	for i, ref := range refs {
		i, ref := i, ref
		wg.Add(1)
		go func() {
			defer wg.Done()
			outcomes[i].Ref = ref
			task, err := a.view(ctx, source, ref)
			if err != nil {
				outcomes[i].Kind = "failed"
				outcomes[i].Err = err
				return
			}
			s := a.summary(source, task)
			outcomes[i].Kind = "found"
			outcomes[i].Item = &Item{Summary: s, Body: task.Description, TerminalKnown: true, Assignment: Assignment{Owners: task.Assignees, Known: true, Assigned: len(task.Assignees) > 0}, Observation: Observation{ObservedAt: time.Now(), ConfigurationGeneration: source.Locator}}
		}()
	}
	wg.Wait()
	return outcomes
}
func (a *BacklogAdapter) ReadDependencies(ctx context.Context, source Source, ref Ref, cursor string, _ int) (DependencyPage, error) {
	if err := a.authorizeRead(ctx, source); err != nil {
		return DependencyPage{}, err
	}
	if cursor != "" {
		return DependencyPage{}, BacklogDiagnostic{"invalid-cursor", "Backlog.md dependencies have no cursor"}
	}
	a.mu.Lock()
	task, ok := a.details[ref.Key()]
	delete(a.details, ref.Key())
	a.mu.Unlock()
	if !ok {
		var err error
		task, err = a.view(ctx, source, ref)
		if err != nil {
			return DependencyPage{}, err
		}
	}
	edges := make([]Relationship, 0, len(task.Dependencies)+1)
	for _, id := range task.Dependencies {
		edges = append(edges, Relationship{Type: HardPrerequisite, Direction: DependentToPrerequisite, From: ref, To: Ref{SourceID: source.ID, ItemID: id}, Provenance: "backlog-md.dependencies", Condition: "terminal", RawOutcome: "dependency", Interpretation: "caller-declared terminal status", Fresh: true, Support: Supported})
	}
	if task.ParentTaskID != "" {
		edges = append(edges, Relationship{Type: ParentChild, Direction: ParentToChild, From: Ref{SourceID: source.ID, ItemID: task.ParentTaskID}, To: ref, Provenance: "backlog-md.parentTaskId", RawOutcome: "parent", Interpretation: "hierarchy only", Fresh: true, Support: Supported})
	}
	completeness := CoverageComplete
	if len(task.Readiness.MissingDependencies) > 0 {
		completeness = CoveragePartial
	}
	return DependencyPage{Edges: edges, Completeness: completeness, Observation: Observation{ObservedAt: time.Now(), ConfigurationGeneration: source.Locator, Coverage: Coverage{State: completeness, Scope: ref.String(), TotalAccuracy: TotalExact}}}, nil
}

var _ Adapter = (*BacklogAdapter)(nil)
var _ io.Writer = (*limitedBuffer)(nil)
