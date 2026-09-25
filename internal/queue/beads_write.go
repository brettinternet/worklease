package queue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// BeadsWriteAdapter uses only documented bd update/comment flags. --sandbox
// disables remote auto-push; --dolt-auto-commit=on commits the issue to Dolt.
type BeadsWriteAdapter struct {
	*BeadsAdapter
	Me string
}
type BeadsWritePreview struct {
	Argv       []string `json:"argv"`
	DoltCommit bool     `json:"doltCommit"`
	AutoExport bool     `json:"autoExport"`
}
type beadsCommit struct {
	Branch string `json:"branch"`
	Commit string `json:"commit"`
}
type beadsComment struct {
	ID      string `json:"id"`
	IssueID string `json:"issue_id"`
	Author  string `json:"author"`
	Text    string `json:"text"`
}

func (a *BeadsWriteAdapter) commit(ctx context.Context, source Source) (beadsCommit, error) {
	data, err := a.run(ctx, source.Locator, false, "vc", "status")
	if err != nil {
		return beadsCommit{}, err
	}
	var status beadsCommit
	if err = beadsJSON(data, &status); err != nil || status.Commit == "" || status.Branch == "" {
		return beadsCommit{}, BeadsDiagnostic{"schema-mismatch", "bd vc status omitted commit or branch"}
	}
	return status, nil
}
func (a *BeadsWriteAdapter) comments(ctx context.Context, intent WriteIntent) ([]beadsComment, error) {
	data, err := a.run(ctx, intent.Source.Locator, false, "comments", intent.Ref.ItemID)
	if err != nil {
		return nil, err
	}
	var comments []beadsComment
	if err = beadsJSON(data, &comments); err != nil || comments == nil {
		return nil, BeadsDiagnostic{"schema-mismatch", "bd comments omitted array"}
	}
	return comments, nil
}
func (a *BeadsWriteAdapter) version(ctx context.Context, intent WriteIntent, issue beadsIssue) (string, error) {
	commit, err := a.commit(ctx, intent.Source)
	if err != nil {
		return "", err
	}
	export, err := a.exportAuto(ctx, intent.Source)
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(struct {
		Revision, Status, Assignee, Notes, Commit, Branch string
		CommentCount                                      int
		AutoExport                                        bool
	}{issue.Revision, issue.Status, issue.Assignee, issue.Notes, commit.Commit, commit.Branch, issue.CommentCount, export})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func (a *BeadsWriteAdapter) ValidateTransition(ctx context.Context, source Source, _ Action, transition string) error {
	if transition == "" || strings.HasPrefix(transition, "-") {
		return BeadsDiagnostic{"invalid-status", "invalid transition"}
	}
	data, err := a.run(ctx, source.Locator, false, "statuses")
	if err != nil {
		return err
	}
	var statuses struct {
		BuiltIn []struct {
			Name string `json:"name"`
		} `json:"built_in_statuses"`
		Custom []struct {
			Name string `json:"name"`
		} `json:"custom_statuses"`
	}
	if err = beadsJSON(data, &statuses); err != nil || statuses.BuiltIn == nil {
		return BeadsDiagnostic{"schema-mismatch", "bd statuses missing built-in statuses"}
	}
	for _, s := range append(statuses.BuiltIn, statuses.Custom...) {
		if s.Name == transition {
			return nil
		}
	}
	return BeadsDiagnostic{"invalid-status", "transition is not a Beads status"}
}
func beadsWriteArgs(intent WriteIntent) ([]string, error) {
	if intent.Ref.ItemID == "" || strings.HasPrefix(intent.Ref.ItemID, "-") {
		return nil, BeadsDiagnostic{"invalid-ref", "invalid issue ID"}
	}
	switch intent.Action {
	case ActionStart, ActionResume, ActionReportBlocked, ActionRequestReview, ActionComplete, ActionReopen:
		if len(intent.Patch) != 1 || intent.Patch["status"] != intent.Transition || intent.Transition == "" || strings.HasPrefix(intent.Transition, "-") || intent.Append != "" {
			return nil, BeadsDiagnostic{"invalid-intent", "invalid status patch"}
		}
		return []string{"update", intent.Ref.ItemID, "--status", intent.Transition}, nil
	case ActionAssignToMe:
		if len(intent.Patch) != 1 || intent.Patch["assignee"] != intent.Principal || intent.Principal == "" || strings.HasPrefix(intent.Principal, "-") || intent.Append != "" {
			return nil, BeadsDiagnostic{"invalid-intent", "invalid assignment patch"}
		}
		return []string{"update", intent.Ref.ItemID, "--assignee", intent.Principal}, nil
	case ActionRecordProgress:
		if len(intent.Patch) != 1 || intent.Patch["append"] != "comment" || intent.Append == "" || intent.Marker != "worklease-op:"+intent.OperationID || strings.Contains(intent.Append, "worklease-op:") {
			return nil, BeadsDiagnostic{"invalid-intent", "progress requires a marked comment"}
		}
		return []string{"comment", intent.Ref.ItemID, intent.Append + "\n" + intent.Marker}, nil
	default:
		return nil, BeadsDiagnostic{"unsupported-action", "unsupported Beads mutation"}
	}
}
func (a *BeadsWriteAdapter) Prepare(ctx context.Context, intent WriteIntent) (WriteIntent, BeadsWritePreview, error) {
	preview := BeadsWritePreview{DoltCommit: true}
	if intent.Source.Adapter != "beads" || intent.Ref.SourceID != intent.Source.ID || intent.OperationID == "" || a.Me == "" || intent.Principal != a.Me {
		return intent, preview, BeadsDiagnostic{"invalid-intent", "Beads source, actor and operation required"}
	}
	issue, err := a.readIssue(ctx, intent.Source, intent.Ref)
	if err != nil {
		return intent, preview, err
	}
	switch intent.Action {
	case ActionStart, ActionResume, ActionReportBlocked, ActionRequestReview, ActionComplete, ActionReopen:
		if err = a.ValidateTransition(ctx, intent.Source, intent.Action, intent.Transition); err != nil {
			return intent, preview, err
		}
	case ActionAssignToMe:
		if intent.Append != "" || len(intent.Patch) != 0 {
			return intent, preview, BeadsDiagnostic{"invalid-intent", "assignment only"}
		}
		intent.Patch = map[string]string{"assignee": a.Me}
	case ActionRecordProgress:
		if intent.Append == "" || strings.Contains(intent.Append, "worklease-op:") || intent.Patch["append"] != "comment" {
			return intent, preview, BeadsDiagnostic{"invalid-intent", "marked comment required"}
		}
		intent.Marker = "worklease-op:" + intent.OperationID
	default:
		return intent, preview, BeadsDiagnostic{"unsupported-action", "unsupported Beads mutation"}
	}
	preview.Argv, err = beadsWriteArgs(intent)
	if err != nil {
		return intent, preview, err
	}
	preview.AutoExport, err = a.exportAuto(ctx, intent.Source)
	if err != nil {
		return intent, preview, err
	}
	intent.Precondition, err = a.version(ctx, intent, issue)
	if err != nil {
		return intent, preview, err
	}
	head, err := a.commit(ctx, intent.Source)
	if err != nil {
		return intent, preview, err
	}
	intent.Effects = []string{"dolt-commit:" + head.Commit}
	return intent, preview, nil
}
func (a *BeadsWriteAdapter) Inspect(ctx context.Context, intent WriteIntent) (WritePreflight, error) {
	var pre WritePreflight
	if intent.Source.Adapter != "beads" || intent.Principal != a.Me || len(intent.Effects) != 1 || !strings.HasPrefix(intent.Effects[0], "dolt-commit:") {
		return pre, BeadsDiagnostic{"invalid-intent", "Beads write binding or effect missing"}
	}
	if _, err := beadsWriteArgs(intent); err != nil {
		return pre, err
	}
	issue, err := a.readIssue(ctx, intent.Source, intent.Ref)
	if err != nil {
		return pre, err
	}
	version, err := a.version(ctx, intent, issue)
	if err != nil {
		return pre, err
	}
	if version != intent.Precondition {
		return pre, BeadsDiagnostic{"conflict", "issue or Dolt commit changed after preview"}
	}
	if intent.Action != ActionRecordProgress && intent.Action != ActionAssignToMe {
		if err := a.ValidateTransition(ctx, intent.Source, intent.Action, intent.Transition); err != nil {
			return pre, err
		}
	}
	pre = WritePreflight{Capability: true, Authorized: true, InScope: true, Fresh: true, NativeAvailable: true, Owner: true, Precondition: intent.Precondition, CompletionEvidence: true}
	if intent.Action == ActionStart || intent.Action == ActionResume {
		closure, err := a.RefreshActionClosure(ctx, intent.Source, intent.Ref)
		if err != nil {
			return WritePreflight{}, err
		}
		pre.Ready = closure[intent.Ref.Key()].Readiness.Status == Ready
	}
	return pre, nil
}
func (a *BeadsWriteAdapter) Write(ctx context.Context, intent WriteIntent) (ProviderReceipt, error) {
	pre, err := a.Inspect(ctx, intent)
	if err != nil {
		return ProviderReceipt{}, err
	}
	if !actionWriteEligible(intent.Action, pre) {
		return ProviderReceipt{}, BeadsDiagnostic{"conflict", "write no longer eligible"}
	}
	argv, err := beadsWriteArgs(intent)
	if err != nil {
		return ProviderReceipt{}, err
	}
	// The sole provider write boundary: validated update or comment argv, no shell.
	ctx, cancel := context.WithTimeout(ctx, a.timeout())
	defer cancel()
	// A forced Dolt commit is declared in the preview. --sandbox forbids remote auto-push.
	cmd := append([]string{}, argv...)
	data, err := a.runWrite(ctx, intent.Source.Locator, cmd...)
	if err != nil {
		return ProviderReceipt{}, err
	}
	receipt := ProviderReceipt{SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID}
	if intent.Action == ActionRecordProgress {
		var comment beadsComment
		if err = beadsJSON(data, &comment); err != nil || comment.ID == "" || comment.IssueID != intent.Ref.ItemID {
			return ProviderReceipt{}, BeadsDiagnostic{"schema-mismatch", "bd comment omitted receipt identity"}
		}
		receipt.ID = comment.ID
		receipt.Actor = comment.Author
	}
	return receipt, nil
}
func (a *BeadsWriteAdapter) runWrite(ctx context.Context, checkout string, argv ...string) ([]byte, error) {
	// a.run adds --sandbox and safe environment. The override forces a durable
	// local Dolt commit even when the project's default is batch/off.
	// a.run itself never permits a write on its read-only path.
	if len(argv) < 2 || (argv[0] != "update" && argv[0] != "comment") {
		return nil, BeadsDiagnostic{"read-only", "write command not allowed"}
	}
	return a.run(ctx, checkout, true, append([]string{"--actor", a.Me}, argv...)...)
}
func (a *BeadsWriteAdapter) ReadReceipt(ctx context.Context, intent WriteIntent, receipt *ProviderReceipt) (ReceiptObservation, error) {
	obs := ReceiptObservation{Outcome: WriteUnknown, SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID, Precondition: intent.Precondition, Patch: map[string]string{}}
	issue, err := a.readIssue(ctx, intent.Source, intent.Ref)
	if err != nil {
		return obs, err
	}
	switch intent.Action {
	case ActionStart, ActionResume, ActionReportBlocked, ActionRequestReview, ActionComplete, ActionReopen:
		obs.Patch["status"] = issue.Status
	case ActionAssignToMe:
		obs.Patch["assignee"] = issue.Assignee
	case ActionRecordProgress:
		obs.Patch["append"] = "comment"
		comments, err := a.comments(ctx, intent)
		if err != nil {
			return obs, err
		}
		for _, comment := range comments {
			obs.MarkerCount += strings.Count(comment.Text, intent.Marker)
			if comment.IssueID == intent.Ref.ItemID && comment.Text == intent.Append+"\n"+intent.Marker && comment.Author == intent.Principal && (receipt == nil || receipt.ID == comment.ID) {
				obs.AppendProof = true
				obs.AppendContent = intent.Append
				obs.Actor = comment.Author
				obs.ReceiptID = comment.ID
			}
		}
	default:
		return obs, BeadsDiagnostic{"unsupported-action", "unsupported Beads mutation"}
	}
	if receipt == nil && intent.Append == "" {
		return obs, nil
	}
	if len(intent.Effects) != 1 || !strings.HasPrefix(intent.Effects[0], "dolt-commit:") {
		return obs, BeadsDiagnostic{"invalid-intent", "missing Dolt commit effect"}
	}
	old := strings.TrimPrefix(intent.Effects[0], "dolt-commit:")
	data, err := a.run(ctx, intent.Source.Locator, false, "history", intent.Ref.ItemID)
	if err != nil {
		return obs, err
	}
	var history []struct {
		CommitHash string `json:"CommitHash"`
	}
	if err = beadsJSON(data, &history); err != nil || len(history) == 0 {
		return obs, BeadsDiagnostic{"schema-mismatch", "bd history missing commits"}
	}
	current, err := a.commit(ctx, intent.Source)
	if err != nil {
		return obs, err
	}
	// A later unrelated Dolt commit is ambiguous, not proof of this operation.
	if current.Commit != old && history[0].CommitHash == current.Commit {
		obs.Effects = map[string]bool{intent.Effects[0]: true}
	}
	obs.Outcome = WriteVerified
	return obs, nil
}

var _ WriteAdapter = (*BeadsWriteAdapter)(nil)
