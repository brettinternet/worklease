package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
)

// BacklogWriteAdapter is deliberately separate from the read-only BacklogAdapter.
// The caller owns the claim and supplies its verified identity through WritePipeline.
// Backlog.md has no conditional edit, so these writes provide local coordination only.
type BacklogWriteAdapter struct {
	*BacklogAdapter
	Me string // configured me.backlog-md identity, including the @ prefix
}

type backlogWriteTask struct {
	backlogTask
	ImplementationNotes string `json:"implementationNotes"`
	Comments            []struct {
		Body   string `json:"body"`
		Author string `json:"author"`
	} `json:"comments"`
	AcceptanceCriteria []struct {
		Checked bool `json:"checked"`
	} `json:"acceptanceCriteria"`
}

type BacklogWritePreview struct {
	Argv           []string `json:"argv"`
	AssignmentRace bool     `json:"assignmentRace"`
	CreatesCommit  bool     `json:"createsCommit"`
	RunsHooks      bool     `json:"runsHooks"`
}

// Prepare snapshots the targeted item before journaling. It never mutates the
// provider; the caller fills in claim/checkpoint fields and calls pipeline.Start.
func (a *BacklogWriteAdapter) Prepare(ctx context.Context, intent WriteIntent) (WriteIntent, BacklogWritePreview, error) {
	var preview BacklogWritePreview
	if intent.Source.Adapter != "backlog-md" || intent.Ref.SourceID != intent.Source.ID || intent.Ref.ItemID == "" || intent.OperationID == "" {
		return intent, preview, BacklogDiagnostic{"invalid-intent", "Backlog.md source and operation required"}
	}
	task, err := a.writeTask(ctx, intent)
	if err != nil {
		return intent, preview, err
	}
	if err := a.preparePatch(ctx, &intent, task); err != nil {
		return intent, preview, err
	}
	if intent.Append != "" {
		intent.Marker = "worklease-op:" + intent.OperationID
	}
	preview, err = a.Preview(ctx, intent)
	if err != nil {
		return intent, preview, err
	}
	intent.Precondition = backlogWriteVersion(task, preview.RunsHooks)
	if preview.CreatesCommit {
		head, err := a.gitHead(ctx, intent.Source)
		if err != nil {
			return intent, preview, err
		}
		intent.Effects = []string{"commit:" + head}
	}
	return intent, preview, nil
}

// Preview rechecks live project settings; hooks are disclosed but not claimed
// as verifiable provider receipts. With auto_commit, the task-file commit is
// independently checked before a provider checkpoint is recorded.
func (a *BacklogWriteAdapter) Preview(ctx context.Context, intent WriteIntent) (BacklogWritePreview, error) {
	var preview BacklogWritePreview
	if err := a.authorizeRead(ctx, intent.Source); err != nil {
		return preview, err
	}
	commit, err := a.setting(ctx, intent.Source, "autoCommit")
	if err != nil {
		return preview, err
	}
	bypass, err := a.setting(ctx, intent.Source, "bypassGitHooks")
	if err != nil {
		return preview, err
	}
	preview.CreatesCommit, preview.RunsHooks = commit, commit && !bypass
	preview.AssignmentRace = intent.Action == ActionAssignToMe
	preview.Argv, err = backlogEditArgs(intent)
	return preview, err
}

func (a *BacklogWriteAdapter) setting(ctx context.Context, source Source, key string) (bool, error) {
	data, err := a.run(ctx, source.Locator, a.binary(), "config", "get", key)
	if err != nil {
		return false, err
	}
	switch strings.TrimSpace(string(data)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, BacklogDiagnostic{"invalid-config", "invalid project effects setting"}
	}
}

func (a *BacklogWriteAdapter) statuses(ctx context.Context, source Source) ([]string, error) {
	data, err := a.run(ctx, source.Locator, a.binary(), "config", "get", "statuses")
	if err != nil {
		return nil, err
	}
	var statuses []string
	for _, status := range strings.Split(strings.TrimSpace(string(data)), ",") {
		if status = strings.TrimSpace(status); status != "" {
			statuses = append(statuses, status)
		}
	}
	if len(statuses) == 0 {
		return nil, BacklogDiagnostic{"invalid-config", "no configured statuses"}
	}
	return statuses, nil
}

func (a *BacklogWriteAdapter) ValidateTransition(ctx context.Context, source Source, _ Action, transition string) error {
	statuses, err := a.statuses(ctx, source)
	if err != nil {
		return err
	}
	if !slices.Contains(statuses, transition) {
		return BacklogDiagnostic{"invalid-status", "transition is not a configured project status"}
	}
	return nil
}

func (a *BacklogWriteAdapter) writeTask(ctx context.Context, intent WriteIntent) (backlogWriteTask, error) {
	if err := a.authorizeRead(ctx, intent.Source); err != nil {
		return backlogWriteTask{}, err
	}
	if intent.Ref.SourceID != intent.Source.ID || intent.Ref.ItemID == "" || strings.HasPrefix(intent.Ref.ItemID, "-") {
		return backlogWriteTask{}, BacklogDiagnostic{"invalid-ref", "item not in this source"}
	}
	data, err := a.run(ctx, intent.Source.Locator, a.binary(), "task", "view", intent.Ref.ItemID, "--json")
	if err != nil {
		return backlogWriteTask{}, err
	}
	var payload struct {
		Task *backlogWriteTask `json:"task"`
	}
	if err := decodeBacklog(data, "task-view", &payload); err != nil {
		return backlogWriteTask{}, err
	}
	if payload.Task == nil || payload.Task.ID != intent.Ref.ItemID {
		return backlogWriteTask{}, BacklogDiagnostic{"identity-mismatch", "view did not return requested task"}
	}
	return *payload.Task, nil
}

func backlogWriteVersion(task backlogWriteTask, runsHooks bool) string {
	data, _ := json.Marshal(struct {
		Status    string
		Assignees []string
		Notes     string
		Comments  any
		Criteria  any
		RunsHooks bool
	}{task.Status, task.Assignees, task.ImplementationNotes, task.Comments, task.AcceptanceCriteria, runsHooks})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (a *BacklogWriteAdapter) preparePatch(ctx context.Context, intent *WriteIntent, task backlogWriteTask) error {
	switch intent.Action {
	case ActionStart, ActionResume, ActionReportBlocked, ActionRequestReview, ActionComplete, ActionReopen:
		if intent.Append != "" || len(intent.Patch) != 1 || intent.Patch["status"] != intent.Transition {
			return BacklogDiagnostic{"invalid-intent", "state intent must change only the mapped status"}
		}
		return a.ValidateTransition(ctx, intent.Source, intent.Action, intent.Transition)
	case ActionRecordProgress:
		if intent.Append == "" || strings.Contains(intent.Append, "worklease-op:") || len(intent.Patch) != 1 || intent.Patch["append"] != "notes" && intent.Patch["append"] != "comment" {
			return BacklogDiagnostic{"invalid-intent", "progress requires notes or comment and nonempty content"}
		}
	case ActionAssignToMe:
		if !validBacklogAssignee(a.Me) || intent.Append != "" || len(intent.Patch) != 0 {
			return BacklogDiagnostic{"invalid-intent", "assignment requires configured me and no unrelated fields"}
		}
		assignees := slices.Clone(task.Assignees)
		for _, assignee := range assignees {
			if !validBacklogAssignee(assignee) {
				return BacklogDiagnostic{"invalid-intent", "unsafe existing assignee"}
			}
		}
		if !slices.Contains(assignees, a.Me) {
			assignees = append(assignees, a.Me)
		}
		encoded, _ := json.Marshal(assignees)
		intent.Patch = map[string]string{"assignees": string(encoded)}
	default:
		return BacklogDiagnostic{"unstable-criterion-target", "criterion indexes can change; check criterion is read-only"}
	}
	return nil
}

func backlogEditArgs(intent WriteIntent) ([]string, error) {
	if intent.Ref.ItemID == "" || strings.HasPrefix(intent.Ref.ItemID, "-") {
		return nil, BacklogDiagnostic{"invalid-ref", "invalid task ID"}
	}
	args := []string{"task", "edit", intent.Ref.ItemID}
	switch intent.Action {
	case ActionStart, ActionResume, ActionReportBlocked, ActionRequestReview, ActionComplete, ActionReopen:
		if len(intent.Patch) != 1 || intent.Patch["status"] == "" || intent.Patch["status"] != intent.Transition || intent.Append != "" {
			return nil, BacklogDiagnostic{"invalid-intent", "invalid status patch"}
		}
		args = append(args, "--status", intent.Transition)
	case ActionRecordProgress:
		if len(intent.Patch) != 1 || intent.Append == "" || intent.Marker != "worklease-op:"+intent.OperationID {
			return nil, BacklogDiagnostic{"invalid-intent", "invalid append patch"}
		}
		switch intent.Patch["append"] {
		case "notes":
			args = append(args, "--append-notes", intent.Append+"\n"+intent.Marker)
		case "comment":
			args = append(args, "--comment", intent.Append+"\n"+intent.Marker, "--comment-author", intent.Principal)
		default:
			return nil, BacklogDiagnostic{"invalid-intent", "invalid append target"}
		}
	case ActionAssignToMe:
		if len(intent.Patch) != 1 || intent.Append != "" {
			return nil, BacklogDiagnostic{"invalid-intent", "invalid assignment patch"}
		}
		var assignees []string
		if err := json.Unmarshal([]byte(intent.Patch["assignees"]), &assignees); err != nil || len(assignees) == 0 {
			return nil, BacklogDiagnostic{"invalid-intent", "invalid assignee set"}
		}
		for _, assignee := range assignees {
			if !validBacklogAssignee(assignee) {
				return nil, BacklogDiagnostic{"invalid-intent", "unsafe assignee"}
			}
		}
		args = append(args, "--assignee", strings.Join(assignees, ","))
	default:
		return nil, BacklogDiagnostic{"unstable-criterion-target", "criterion indexes can change; check criterion is read-only"}
	}
	return args, nil
}

func (a *BacklogWriteAdapter) Inspect(ctx context.Context, intent WriteIntent) (WritePreflight, error) {
	var pre WritePreflight
	task, err := a.writeTask(ctx, intent)
	if err != nil {
		return pre, err
	}
	preview, err := a.Preview(ctx, intent)
	if err != nil {
		return pre, err
	}
	if backlogWriteVersion(task, preview.RunsHooks) != intent.Precondition {
		return pre, BacklogDiagnostic{"conflict", "task or hook policy changed since preview"}
	}
	if preview.CreatesCommit != (len(intent.Effects) == 1 && strings.HasPrefix(intent.Effects[0], "commit:")) {
		return pre, BacklogDiagnostic{"conflict", "project Git effects changed since preview"}
	}
	if preview.CreatesCommit {
		head, err := a.gitHead(ctx, intent.Source)
		if err != nil {
			return pre, err
		}
		if intent.Effects[0] != "commit:"+head {
			return pre, BacklogDiagnostic{"conflict", "project HEAD changed since preview"}
		}
	}
	if intent.Action != ActionRecordProgress && intent.Action != ActionAssignToMe {
		if err := a.ValidateTransition(ctx, intent.Source, intent.Action, intent.Transition); err != nil {
			return pre, err
		}
	}
	if intent.Action == ActionAssignToMe {
		var expected []string
		if err := json.Unmarshal([]byte(intent.Patch["assignees"]), &expected); err != nil || !slices.Contains(expected, a.Me) || len(expected) != len(task.Assignees)+boolInt(!slices.Contains(task.Assignees, a.Me)) {
			return pre, BacklogDiagnostic{"conflict", "assignee set changed"}
		}
		for _, old := range task.Assignees {
			if !slices.Contains(expected, old) {
				return pre, BacklogDiagnostic{"conflict", "assignee set changed"}
			}
		}
	}
	pre = WritePreflight{Capability: true, Authorized: true, InScope: true, Fresh: true, NativeAvailable: true, Owner: true, Precondition: intent.Precondition}
	if intent.Action == ActionStart || intent.Action == ActionResume {
		items, err := a.RefreshActionClosure(ctx, intent.Source, intent.Ref)
		if err != nil {
			return WritePreflight{}, err
		}
		pre.Ready = items[intent.Ref.Key()].Readiness.Status == Ready
	}
	if intent.Action == ActionComplete {
		pre.CompletionEvidence = true
		for _, criterion := range task.AcceptanceCriteria {
			pre.CompletionEvidence = pre.CompletionEvidence && criterion.Checked
		}
	}
	return pre, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (a *BacklogWriteAdapter) Write(ctx context.Context, intent WriteIntent) (ProviderReceipt, error) {
	var receipt ProviderReceipt
	pre, err := a.Inspect(ctx, intent)
	if err != nil {
		return receipt, err
	}
	if !pre.Capability || !actionWriteEligible(intent.Action, pre) {
		return receipt, BacklogDiagnostic{"conflict", "write is no longer eligible"}
	}
	args, err := backlogEditArgs(intent)
	if err != nil {
		return receipt, err
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	// Unlike the read path, this is the sole permitted subprocess mutation:
	// an exact, validated edit argv, with no shell or task-file writes.
	if _, err := a.runCommand(ctx, intent.Source.Locator, a.binary(), args...); err != nil {
		return receipt, err
	}
	return ProviderReceipt{SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID}, nil
}

func (a *BacklogWriteAdapter) ReadReceipt(ctx context.Context, intent WriteIntent, receipt *ProviderReceipt) (ReceiptObservation, error) {
	observed := ReceiptObservation{Outcome: WriteUnknown, SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID, Precondition: intent.Precondition, Patch: map[string]string{}}
	task, err := a.writeTask(ctx, intent)
	if err != nil {
		return observed, err
	}
	switch intent.Action {
	case ActionStart, ActionResume, ActionReportBlocked, ActionRequestReview, ActionComplete, ActionReopen:
		observed.Patch["status"] = task.Status
	case ActionAssignToMe:
		expected := []string{}
		if err := json.Unmarshal([]byte(intent.Patch["assignees"]), &expected); err != nil {
			return observed, err
		}
		if !slices.Equal(expected, task.Assignees) {
			observed.Outcome = WriteConflict
			return observed, nil
		}
		observed.Patch["assignees"] = intent.Patch["assignees"]
	case ActionRecordProgress:
		observed.Patch["append"] = intent.Patch["append"]
		entry := intent.Append + "\n" + intent.Marker
		observed.MarkerCount = strings.Count(task.Description, intent.Marker) + strings.Count(task.ImplementationNotes, intent.Marker)
		if intent.Patch["append"] == "notes" && containsBacklogNote(task.ImplementationNotes, entry) {
			observed.AppendContent, observed.AppendProof = intent.Append, true
		}
		for _, comment := range task.Comments {
			observed.MarkerCount += strings.Count(comment.Body, intent.Marker)
			if intent.Patch["append"] == "comment" && comment.Body == entry {
				observed.AppendContent, observed.AppendProof, observed.Actor = intent.Append, true, comment.Author
			}
		}
	default:
		return observed, BacklogDiagnostic{"unstable-criterion-target", "criterion indexes can change; check criterion is read-only"}
	}
	if receipt == nil && intent.Append == "" {
		return observed, nil
	}
	for _, effect := range intent.Effects {
		if !strings.HasPrefix(effect, "commit:") {
			return observed, BacklogDiagnostic{"invalid-intent", "unknown Git effect"}
		}
		head, err := a.gitHead(ctx, intent.Source)
		if err != nil {
			return observed, err
		}
		if head != strings.TrimPrefix(effect, "commit:") {
			committed, err := a.gitTaskCommitted(ctx, intent.Source, strings.TrimPrefix(effect, "commit:"), task.Path)
			if err != nil {
				return observed, err
			}
			if committed {
				observed.Effects = map[string]bool{effect: true}
			}
		}
	}
	observed.Outcome = WriteVerified
	return observed, nil
}

func (a *BacklogWriteAdapter) gitHead(ctx context.Context, source Source) (string, error) {
	data, err := a.run(ctx, source.Locator, "git", "rev-parse", "HEAD")
	return strings.TrimSpace(string(data)), err
}

func validBacklogAssignee(name string) bool {
	return len(name) > 1 && name[0] == '@' && !strings.ContainsAny(name, ", \t\r\n")
}

func containsBacklogNote(notes, entry string) bool {
	return notes == entry || strings.HasPrefix(notes, entry+"\n\n") ||
		strings.Contains(notes, "\n\n"+entry+"\n\n") || strings.HasSuffix(notes, "\n\n"+entry)
}

func (a *BacklogWriteAdapter) gitTaskCommitted(ctx context.Context, source Source, before, path string) (bool, error) {
	if len(before) != 40 && len(before) != 64 {
		return false, BacklogDiagnostic{"invalid-intent", "invalid commit precondition"}
	}
	if _, err := hex.DecodeString(before); err != nil {
		return false, BacklogDiagnostic{"invalid-intent", "invalid commit precondition"}
	}
	if path == "" || !filepath.IsLocal(path) {
		return false, BacklogDiagnostic{"invalid-ref", "invalid task path"}
	}
	ctx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	if _, err := a.runCommand(ctx, source.Locator, backlogGitBinary, "merge-base", "--is-ancestor", before, "HEAD"); err != nil {
		return false, err
	}
	data, err := a.runCommand(ctx, source.Locator, backlogGitBinary, "log", "--format=%H", before+"..HEAD", "--", path)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(data)) != "", nil
}

var _ WriteAdapter = (*BacklogWriteAdapter)(nil)
