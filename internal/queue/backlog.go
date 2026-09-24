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
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

// BacklogDiagnostic is safe to display: subprocess output and paths are never embedded.
type BacklogDiagnostic struct{ Code, Detail string }

func (d BacklogDiagnostic) Error() string { return d.Code + ": " + d.Detail }

// BacklogDirectory returns the project's backlog folder, the documented default
// `backlog-md` key source, so queue and CLI callers derive identical keys.
// It follows Backlog.md: a lexically contained backlog_directory from the root
// backlog.config.yml (default `backlog`), otherwise an existing backlog/ or
// .backlog/ folder.
func BacklogDirectory(checkout string) (string, error) {
	content, err := os.ReadFile(filepath.Join(checkout, "backlog.config.yml"))
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err == nil {
		var cfg struct {
			Directory *string `yaml:"backlog_directory"`
		}
		if err := yaml.Unmarshal(content, &cfg); err != nil {
			return "", err
		}
		if cfg.Directory == nil {
			return filepath.Join(checkout, "backlog"), nil
		}
		if dir := filepath.Clean(*cfg.Directory); *cfg.Directory != "" && filepath.IsLocal(dir) {
			return filepath.Join(checkout, dir), nil
		}
	}
	for _, name := range []string{"backlog", ".backlog"} {
		if info, err := os.Stat(filepath.Join(checkout, name)); err == nil && info.IsDir() {
			return filepath.Join(checkout, name), nil
		}
	}
	return "", BacklogDiagnostic{"not-backlog-project", "checkout has no Backlog.md project"}
}

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
	probeCtx, cancel := context.WithTimeout(context.Background(), a.timeout())
	defer cancel()
	branch, err := exec.CommandContext(probeCtx, backlogGitBinary, "-C", source.Locator, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		return "", "", "", false
	}
	head, err := exec.CommandContext(probeCtx, backlogGitBinary, "-C", source.Locator, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		return "", "", "", false
	}
	generation += ":" + strings.TrimSpace(string(branch)) + ":" + strings.TrimSpace(string(head))
	for _, name := range []string{"backlog.config.yml", filepath.Join("backlog", "config.yml")} {
		content, err := os.ReadFile(filepath.Join(source.Locator, name))
		if err == nil {
			sum := sha256.Sum256(content)
			generation += ":" + hex.EncodeToString(sum[:])
		} else if !os.IsNotExist(err) && !errors.Is(err, syscall.ENOTDIR) {
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

var backlogGitBinary = func() string {
	if path, err := exec.LookPath("git"); err == nil {
		return path
	}
	return "git"
}()

// BacklogAdapter reads only the configured checkout. TerminalStatuses is the caller's
// project status mapping; provider isReady is never used to infer generic readiness.
type BacklogAdapter struct {
	TerminalStatuses  map[string]bool
	Binary            string
	Timeout           time.Duration
	reconcileInterval time.Duration // test-only override; production reconciles every minute
	watcherFactory    func() (backlogWatcher, error)
	tickerFactory     func(time.Duration) backlogWatchTicker
	mu                sync.Mutex
	diagnostics       map[string]BacklogSourceDiagnostics
	consent           map[string]bool
	details           map[string]backlogTask
	// Edge observations are scoped to a checkout/configuration generation. A list
	// without dependencies never turns a metadata match into fresh edge evidence.
	edges      map[string]backlogEdges
	partitions map[string]string
	revisions  map[string]uint64
}

func NewBacklogAdapter(terminalStatuses ...string) *BacklogAdapter {
	statuses := make(map[string]bool, len(terminalStatuses))
	for _, s := range terminalStatuses {
		statuses[s] = true
	}
	return &BacklogAdapter{TerminalStatuses: statuses, diagnostics: map[string]BacklogSourceDiagnostics{}, consent: map[string]bool{}, details: map[string]backlogTask{}, edges: map[string]backlogEdges{}, partitions: map[string]string{}, revisions: map[string]uint64{}, watcherFactory: newNativeBacklogWatcher, tickerFactory: newNativeBacklogWatchTicker}
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
			len(args) == 5 && args[0] == "-c" && args[1] == "core.fsmonitor=false" && args[2] == "status" && args[3] == "--porcelain" && args[4] == "--untracked-files=normal"
	}
	if filepath.Base(binary) != "backlog" {
		return false
	}
	return len(args) == 1 && args[0] == "--version" ||
		len(args) == 3 && args[0] == "config" && args[1] == "get" && (args[2] == "remoteOperations" || args[2] == "checkActiveBranches" || args[2] == "autoCommit" || args[2] == "bypassGitHooks" || args[2] == "statuses") ||
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
	if requested, ok := ctx.Value(backlogPriorityKey{}).(RequestPriority); ok && requested == PriorityAction {
		priority = PriorityAction
	}
	if len(args) >= 2 && args[0] == "task" {
		priority = PriorityVisible
		if args[1] == "view" {
			priority = PriorityDetail
			if requested, ok := ctx.Value(backlogPriorityKey{}).(RequestPriority); ok {
				priority = requested
			}
		}
	}
	gate := quotaScheduler("backlog:"+cwd, 4)
	key := binary + "\x00" + strings.Join(args, "\x00")
	if revision, ok := ctx.Value(backlogListRevisionKey{}).(backlogListRevision); ok {
		key += "\x00list-revision\x00" + revision.sourceID + "\x00" + strconv.FormatUint(revision.revision, 10)
	}
	coalesce := priority != PriorityAction
	result, err := gate.schedule(ctx, priority, key, "", coalesce, func(workCtx context.Context) (any, error) {
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
	commandCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, binary, args...)
	prepareBacklogCommand(cmd)
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
	stdout.onLimit = cancel
	stderr.limit = 4096
	stderr.onLimit = cancel
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

type backlogPriorityKey struct{}
type backlogListRevisionKey struct{}

type backlogListRevision struct {
	sourceID string
	revision uint64
}

type limitedBuffer struct {
	data     bytes.Buffer
	limit    int
	exceeded bool
	onLimit  func()
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.data.Len() {
		b.exceeded = true
		if b.onLimit != nil {
			b.onLimit()
		}
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
	d.Branch, d.Head = "", "" // failed probes cannot retain the preceding partition
	if b, err := a.run(ctx, checkout, "git", "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		d.Branch = strings.TrimSpace(string(b))
	}
	if b, err := a.run(ctx, checkout, "git", "rev-parse", "HEAD"); err == nil {
		d.Head = strings.TrimSpace(string(b))
	}
	if b, err := a.run(ctx, checkout, "git", "-c", "core.fsmonitor=false", "status", "--porcelain", "--untracked-files=normal"); err == nil {
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

type backlogEdges struct {
	task      backlogTask
	partition string
	bulk      bool
	mtime     time.Time
	size      int64
}

func edgeFileStamp(checkout, path string) (time.Time, int64, bool) {
	if path == "" || filepath.IsAbs(path) || strings.HasPrefix(filepath.Clean(path), "..") {
		return time.Time{}, 0, false
	}
	stat, err := os.Stat(filepath.Join(checkout, path))
	if err != nil || !stat.Mode().IsRegular() {
		return time.Time{}, 0, false
	}
	return stat.ModTime(), stat.Size(), true
}

// InvalidateEdges is called on watch loss, overflow or uncertain filesystem
// events. The next list re-observes source state; it cannot validate old edges.
func (a *BacklogAdapter) InvalidateEdges(source Source) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.revisions[source.ID]++
	for key := range a.details {
		if strings.HasPrefix(key, source.ID+"\x00") {
			delete(a.details, key)
		}
	}
	for key := range a.edges {
		if strings.HasPrefix(key, source.ID+"\x00") {
			delete(a.edges, key)
		}
	}
}

// CachedEdges only returns observed edges from the current source partition.
// It is display evidence, never sufficient for an authoritative action check.
func (a *BacklogAdapter) CachedEdges(source Source, ref Ref) (DependencyPage, bool) {
	a.mu.Lock()
	cached, ok := a.edges[ref.Key()]
	partition := a.partitions[source.ID]
	a.mu.Unlock()
	if !ok || cached.partition != partition || ref.SourceID != source.ID || !cached.bulk && partition == "" {
		return DependencyPage{}, false
	}
	return a.dependencyPage(source, ref, cached.task), true
}

// RefreshActionClosure rereads every prerequisite directly from the provider.
// An action must use these observations rather than an indexed/display cache.
func (a *BacklogAdapter) RefreshActionClosure(ctx context.Context, source Source, ref Ref) (map[string]Item, error) {
	items := map[string]Item{}
	var visit func(Ref) error
	visit = func(current Ref) error {
		if _, seen := items[current.Key()]; seen {
			return nil
		}
		if current.SourceID != source.ID {
			return BacklogDiagnostic{"invalid-ref", "prerequisite outside source"}
		}
		task, err := a.view(context.WithValue(ctx, backlogPriorityKey{}, PriorityAction), source, current, false)
		if err != nil {
			return err
		}
		deps := a.dependencyPage(source, current, task)
		item := Item{Summary: a.summary(source, task), ReadOutcome: "found", ReadPermission: Allowed, TerminalKnown: true,
			DependenciesKnown: deps.Completeness == CoverageComplete, Closure: deps.Completeness, Relationships: deps.Edges,
			Observation: deps.Observation}
		items[current.Key()] = item
		for _, edge := range deps.Edges {
			if edge.Type == HardPrerequisite {
				if err := visit(edge.To); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(ref); err != nil {
		return nil, err
	}
	return Recompute(items, CoverageComplete), nil
}

// RefreshClosure bypasses cached edge observations for one item.
func (a *BacklogAdapter) RefreshClosure(ctx context.Context, source Source, ref Ref) (DependencyPage, error) {
	task, err := a.view(ctx, source, ref, false)
	if err != nil {
		return DependencyPage{}, err
	}
	return a.dependencyPage(source, ref, task), nil
}

type backlogTask struct {
	ID           string            `json:"id"`
	Title        string            `json:"title"`
	Status       string            `json:"status"`
	Priority     string            `json:"priority"`
	Ordinal      int               `json:"ordinal"`
	Assignees    []string          `json:"assignees"`
	UpdatedAt    time.Time         `json:"updatedAt"`
	IsReady      bool              `json:"isReady"`
	ParentTaskID string            `json:"parentTaskId"`
	Dependencies []string          `json:"dependencies"`
	Path         string            `json:"path"`
	Description  string            `json:"description"`
	Readiness    *backlogReadiness `json:"readiness"`
}

type backlogReadiness struct {
	MissingDependencies *[]string `json:"missingDependencies"`
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
	a.mu.Lock()
	revision := a.revisions[source.ID]
	a.mu.Unlock()
	listCtx := context.WithValue(ctx, backlogListRevisionKey{}, backlogListRevision{sourceID: source.ID, revision: revision})
	data, err := a.run(listCtx, source.Locator, a.binary(), "task", "list", "--json")
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
	_, checkout, config, valid := a.QueueCacheIdentity(source)
	// A failed identity or Git read is uncertain invalidation: retire all edges.
	partition := ""
	if valid && d.Branch != "" && d.Head != "" {
		partition = checkout + "\x00" + config + "\x00" + d.Branch + "\x00" + d.Head
	}
	a.mu.Lock()
	if revision != a.revisions[source.ID] {
		a.mu.Unlock()
		return SummaryPage{}, BacklogDiagnostic{"observation-invalidated", "provider changed during task list"}
	}
	a.revisions[source.ID]++ // list may race a view even when its metadata tuple is unchanged
	if partition == "" || a.partitions[source.ID] != partition {
		for key := range a.edges {
			if strings.HasPrefix(key, source.ID+"\x00") {
				delete(a.edges, key)
			}
		}
	}
	a.partitions[source.ID] = partition
	for key := range a.details {
		if strings.HasPrefix(key, source.ID+"\x00") {
			delete(a.details, key)
		}
	}
	for _, task := range payload.Tasks {
		key := (Ref{SourceID: source.ID, ItemID: task.ID}).Key()
		if task.Dependencies != nil { // A newer Backlog version can supply bulk edges.
			if task.Readiness == nil {
				// List output carries each task's complete dependency set but no
				// readiness evidence; the core detects missing prerequisite endpoints.
				none := []string{}
				task.Readiness = &backlogReadiness{MissingDependencies: &none}
			}
			a.edges[key] = backlogEdges{task: task, partition: partition, bulk: true}
		} else if cached, ok := a.edges[key]; ok {
			// A matching tuple does not establish freshness; only an uninterrupted
			// observation stream and periodic reconciliation can preserve evidence.
			mtime, size, stamped := edgeFileStamp(source.Locator, cached.task.Path)
			if cached.partition != partition || cached.task.UpdatedAt != task.UpdatedAt ||
				cached.task.Status != task.Status || cached.task.Path != "" && (!stamped || !mtime.Equal(cached.mtime) || size != cached.size) {
				delete(a.edges, key)
			}
		}
	}
	for key := range a.edges {
		if strings.HasPrefix(key, source.ID+"\x00") && !seen[strings.TrimPrefix(key, source.ID+"\x00")] {
			delete(a.edges, key)
		}
	}
	d.DuplicateIDs = d.DuplicateIDs[:0]
	for id := range duplicates {
		d.DuplicateIDs = append(d.DuplicateIDs, id)
		delete(a.edges, (Ref{SourceID: source.ID, ItemID: id}).Key())
	}
	if len(duplicates) > 0 {
		page.Coverage.State = CoverageUnknown
		page.Coverage.Reason = "duplicate-task-ids"
		page.Observation.Coverage = page.Coverage
	}
	sort.Strings(d.DuplicateIDs)
	a.diagnostics[source.ID] = d
	a.mu.Unlock()
	return page, nil
}
func (a *BacklogAdapter) view(ctx context.Context, source Source, ref Ref, retain bool) (backlogTask, error) {
	if err := a.authorizeRead(ctx, source); err != nil {
		return backlogTask{}, err
	}
	if ref.SourceID != source.ID || ref.ItemID == "" || strings.HasPrefix(ref.ItemID, "-") {
		return backlogTask{}, BacklogDiagnostic{"invalid-ref", "item not in this source"}
	}
	a.mu.Lock()
	revision := a.revisions[source.ID]
	a.mu.Unlock()
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
	if revision != a.revisions[source.ID] {
		a.mu.Unlock()
		return backlogTask{}, BacklogDiagnostic{"observation-invalidated", "provider changed during task read"}
	}
	// Only ReadItems retains its view for the next ReadDependencies call;
	// other reads must not leave a detail that a later read would consume.
	if retain {
		a.details[ref.Key()] = *payload.Task
	}
	if partition := a.partitions[source.ID]; partition != "" {
		mtime, size, _ := edgeFileStamp(source.Locator, payload.Task.Path)
		a.edges[ref.Key()] = backlogEdges{task: *payload.Task, partition: partition, mtime: mtime, size: size}
	}
	a.mu.Unlock()
	return *payload.Task, nil
}
func (a *BacklogAdapter) ReadItems(ctx context.Context, source Source, refs []Ref, _ []string, _ int) []ItemOutcome {
	outcomes := make([]ItemOutcome, len(refs))
	workers := min(4, len(refs))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				ref := refs[i]
				outcomes[i].Ref = ref
				task, err := a.view(ctx, source, ref, true)
				if err != nil {
					outcomes[i].Kind = "failed"
					outcomes[i].Err = err
					continue
				}
				s := a.summary(source, task)
				outcomes[i].Kind = "found"
				outcomes[i].Item = &Item{Summary: s, Body: task.Description, TerminalKnown: true, Assignment: Assignment{Owners: task.Assignees, Known: true, Assigned: len(task.Assignees) > 0}, Observation: Observation{ObservedAt: time.Now(), ConfigurationGeneration: source.Locator}}
			}
		}()
	}
	for i := range refs {
		jobs <- i
	}
	close(jobs)
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
	if !ok {
		if cached, found := a.edges[ref.Key()]; found && (cached.partition != "" || cached.bulk) && cached.partition == a.partitions[source.ID] {
			task, ok = cached.task, true
		}
	}
	a.mu.Unlock()
	if !ok {
		var err error
		task, err = a.view(ctx, source, ref, false)
		if err != nil {
			return DependencyPage{}, err
		}
	}
	return a.dependencyPage(source, ref, task), nil
}

func (a *BacklogAdapter) dependencyPage(source Source, ref Ref, task backlogTask) DependencyPage {
	edges := make([]Relationship, 0, len(task.Dependencies)+1)
	for _, id := range task.Dependencies {
		edges = append(edges, Relationship{Type: HardPrerequisite, Direction: DependentToPrerequisite, From: ref, To: Ref{SourceID: source.ID, ItemID: id}, Provenance: "backlog-md.dependencies", Condition: "terminal", RawOutcome: "dependency", Interpretation: "caller-declared terminal status", Fresh: true, Support: Supported})
	}
	if task.ParentTaskID != "" {
		edges = append(edges, Relationship{Type: ParentChild, Direction: ParentToChild, From: Ref{SourceID: source.ID, ItemID: task.ParentTaskID}, To: ref, Provenance: "backlog-md.parentTaskId", RawOutcome: "parent", Interpretation: "hierarchy only", Fresh: true, Support: Supported})
	}
	completeness := CoveragePartial
	if task.Readiness != nil && task.Readiness.MissingDependencies != nil && len(*task.Readiness.MissingDependencies) == 0 {
		completeness = CoverageComplete
	}
	return DependencyPage{Edges: edges, Completeness: completeness, Observation: Observation{ObservedAt: time.Now(), ConfigurationGeneration: source.Locator, Coverage: Coverage{State: completeness, Scope: ref.String(), TotalAccuracy: TotalExact}}}
}

var _ Adapter = (*BacklogAdapter)(nil)
var _ io.Writer = (*limitedBuffer)(nil)
