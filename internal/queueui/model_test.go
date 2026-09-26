package queueui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/reason"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// PrepareSnapshot keeps test and benchmark fixtures independent of producer mutations.
func PrepareSnapshot(snapshot queue.Snapshot, sources ...queue.Source) SnapshotMsg {
	return prepareSnapshot(snapshot, true, sources...)
}

func fixture() queue.Snapshot {
	a := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "a", ItemID: "1"}, CanonicalID: "stable-1", Title: "first", RawStatus: "open", Fresh: true}, Body: "\x1b]8;;https://evil.invalid\aCLICK\x1b]8;;\a \x1b[31mred\x1b[0m", Resources: []string{"resource:1"}, Claim: queue.ClaimObservation{Known: true, Active: true, AgentID: "whole-agent", SessionID: "full-session-identity"}}
	b := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "a", ItemID: "2"}, CanonicalID: "stable-2", Title: "second", RawStatus: "open", Fresh: true}}
	return queue.Snapshot{Items: map[string]queue.Item{a.Ref.Key(): a, b.Ref.Key(): b}, Sources: map[string]queue.Coverage{"a": {State: queue.CoverageComplete, Total: 2, TotalAccuracy: queue.TotalExact}}}
}

// screenText flattens a rendered screen to single-spaced words, dropping
// styling and box borders, so assertions do not depend on where text wraps.
func screenText(view string) string {
	view = ansi.Strip(view)
	view = strings.Map(func(r rune) rune {
		if strings.ContainsRune("│╭╮╰╯─", r) {
			return ' '
		}
		return r
	}, view)
	return strings.Join(strings.Fields(view), " ")
}

func press(m Model, key string) (Model, tea.Cmd) {
	type msg = tea.KeyMsg
	var k msg
	switch key {
	case "enter":
		k.Type = tea.KeyEnter
	case "tab":
		k.Type = tea.KeyTab
	case "esc":
		k.Type = tea.KeyEsc
	case "down":
		k.Type = tea.KeyDown
	case "pgdown":
		k.Type = tea.KeyPgDown
	case "pgup":
		k.Type = tea.KeyPgUp
	case "ctrl+f":
		k.Type = tea.KeyCtrlF
	case "ctrl+b":
		k.Type = tea.KeyCtrlB
	case "ctrl+d":
		k.Type = tea.KeyCtrlD
	case "ctrl+u":
		k.Type = tea.KeyCtrlU
	case "ctrl+e":
		k.Type = tea.KeyCtrlE
	case "ctrl+y":
		k.Type = tea.KeyCtrlY
	default:
		k.Type = tea.KeyRunes
		k.Runes = []rune(key)
	}
	next, cmd := m.Update(k)
	return next.(Model), cmd
}
func TestProviderActionsRequirePreviewConfirmation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		key              string
		action           queue.Action
		transition, text string
	}{
		{"s", queue.ActionStart, "Doing", ""},
		{"p", queue.ActionRecordProgress, "", "note"},
		{"a", queue.ActionAssignToMe, "", ""},
	} {
		t.Run(tc.key, func(t *testing.T) {
			m := New(fixture())
			m.anchor(m.rows())
			m.StateChoices = map[string][]StateChoice{"a": {{Action: queue.ActionStart, Label: "start", Transition: "Doing"}}}
			previews, commits := 0, 0
			m.PreviewWrite = func(item queue.Item, action queue.Action, transition, text string) tea.Cmd {
				previews++
				if action != tc.action || transition != tc.transition || text != tc.text {
					t.Errorf("wrong action: %s %q %q", action, transition, text)
				}
				return func() tea.Msg {
					return WritePreviewMsg{Preview: &WritePreview{Identity: identity(item), Intent: queue.WriteIntent{OperationID: "one", Action: action}}}
				}
			}
			m.ConfirmWrite = func(p WritePreview) tea.Cmd {
				commits++
				return func() tea.Msg {
					return WriteResultMsg{Result: queue.WriteResult{Outcome: queue.WriteUnknown, ClaimHeld: true}, OperationID: p.Intent.OperationID}
				}
			}
			var cmd tea.Cmd
			m, cmd = press(m, tc.key)
			if tc.key == "s" {
				m, cmd = press(m, "enter")
			}
			if tc.key == "p" {
				for _, char := range tc.text {
					m, _ = press(m, string(char))
				}
				m, cmd = press(m, "enter")
			}
			if cmd == nil || commits != 0 || previews != 1 {
				t.Fatalf("preview request missing or dispatched early: previews=%d commits=%d", previews, commits)
			}
			msg := cmd().(WritePreviewMsg)
			next, _ := m.Update(msg)
			m = next.(Model)
			if !strings.Contains(m.View(), "Confirm") || commits != 0 {
				t.Fatalf("preview hidden or dispatched early: %s", m.View())
			}
			m, cmd = press(m, "enter")
			if commits != 1 || cmd == nil {
				t.Fatal("confirmation did not dispatch")
			}
			next, _ = m.Update(cmd())
			m = next.(Model)
			if strings.Contains(m.Notice, "verified") || !strings.Contains(m.Notice, "claim held true") {
				t.Fatalf("uncertain write reported success or hid claim: %s", m.Notice)
			}
			next, _ = m.Update(RefreshedMsg{})
			m = next.(Model)
			if !strings.Contains(m.View(), "claims held") {
				t.Fatal("refresh hid an uncertain held claim")
			}
		})
	}
}

func TestRecoveryReconciliationRequiresTypedEvidenceAndNoActiveDispatch(t *testing.T) {
	t.Parallel()
	m := New(fixture())
	m.ViewName = RecoveryViewID
	entry := queue.RecoveryEntry{OperationID: "operation-1", Ref: queue.Ref{SourceID: "a", ItemID: "1"}, Status: "dispatching", Next: []string{"retry read-back"}}
	next, _ := m.Update(RecoveryMsg{Entries: []queue.RecoveryEntry{entry}})
	m = next.(Model)
	called := 0
	m.ReconcileRecovery = func(queue.RecoveryEntry, string) tea.Cmd {
		called++
		return func() tea.Msg { return ReconcileResultMsg{} }
	}
	m, _ = press(m, "e")
	if m.RecoveryEvidence || called != 0 {
		t.Fatal("reconciliation offered while dispatching")
	}
	entry.Status, entry.Next = "unknown", []string{"retry read-back", "operator reconciliation after cessation"}
	next, _ = m.Update(RecoveryMsg{Entries: []queue.RecoveryEntry{entry}})
	m = next.(Model)
	m, _ = press(m, "e")
	if !m.RecoveryEvidence {
		t.Fatal("unknown operation cannot accept evidence")
	}
	for _, char := range "provider audit inspected" {
		m, _ = press(m, string(char))
	}
	m, _ = press(m, "enter")
	if called != 0 {
		t.Fatal("unattested evidence dispatched")
	}
	m, _ = press(m, "e")
	for _, char := range "NO COMMIT; EXECUTOR STOPPED: provider audit confirms no effect" {
		m, _ = press(m, string(char))
	}
	m, cmd := press(m, "enter")
	if cmd == nil || called != 1 {
		t.Fatal("typed attestation did not dispatch")
	}
}

func TestRecoveryEvidenceStaysBoundToSelectedOperation(t *testing.T) {
	t.Parallel()
	m := New(fixture())
	m.ViewName = RecoveryViewID
	next := []string{"retry read-back", "operator reconciliation after cessation"}
	first := queue.RecoveryEntry{OperationID: "operation-1", Status: "unknown", Next: next}
	second := queue.RecoveryEntry{OperationID: "operation-2", Status: "unknown", Next: next}
	updated, _ := m.Update(RecoveryMsg{Entries: []queue.RecoveryEntry{first, second}})
	m = updated.(Model)
	var reconciled []string
	m.ReconcileRecovery = func(entry queue.RecoveryEntry, _ string) tea.Cmd {
		reconciled = append(reconciled, entry.OperationID)
		return func() tea.Msg { return ReconcileResultMsg{} }
	}
	m, _ = press(m, "e")
	for _, char := range "NO COMMIT; EXECUTOR STOPPED: provider audit for operation one" {
		m, _ = press(m, string(char))
	}
	// operation-1 resolves elsewhere; operation-2 moves into the selected row.
	updated, _ = m.Update(RecoveryMsg{Entries: []queue.RecoveryEntry{second}})
	m = updated.(Model)
	m, _ = press(m, "enter")
	if len(reconciled) != 0 {
		t.Fatalf("evidence for operation-1 reconciled %v", reconciled)
	}
}

func TestRecoveryCheckpointMissingAttestationAndTerminalNotice(t *testing.T) {
	t.Parallel()
	m := New(fixture())
	m.ViewName = RecoveryViewID
	entry := queue.RecoveryEntry{OperationID: "operation-1", Ref: queue.Ref{SourceID: "a", ItemID: "1"}, Status: "checkpoint-pending", Next: []string{"retry authority checkpoint status", "attest verified provider effect"}}
	next, _ := m.Update(RecoveryMsg{Entries: []queue.RecoveryEntry{entry}})
	m = next.(Model)
	called := 0
	m.AttestCheckpointMissing = func(queue.RecoveryEntry, string) tea.Cmd {
		called++
		return func() tea.Msg {
			return ReconcileResultMsg{OperationID: entry.OperationID, Status: "checkpoint-missing"}
		}
	}
	m, _ = press(m, "e")
	if !m.RecoveryEvidence || !strings.Contains(m.View(), "PROVIDER VERIFIED; CHECKPOINT ABSENT") {
		t.Fatalf("missing attestation prompt: %s", m.View())
	}
	for _, char := range "PROVIDER VERIFIED; CHECKPOINT ABSENT; EXECUTOR STOPPED: provider and authority audit evidence" {
		m, _ = press(m, string(char))
	}
	m, cmd := press(m, "enter")
	if called != 1 || cmd == nil {
		t.Fatal("checkpoint attestation did not dispatch")
	}
	m.UncertainWrite = true
	next, _ = m.Update(cmd())
	m = next.(Model)
	if !strings.Contains(m.Notice, "checkpoint missing") {
		t.Fatalf("terminal outcome hidden: %s", m.Notice)
	}
	next, _ = m.Update(RecoveryMsg{})
	m = next.(Model)
	if m.UncertainWrite || strings.Contains(m.View(), "RECOVERY REQUIRED") {
		t.Fatalf("terminal recovery still reported uncertain: %s", m.View())
	}
}

func TestProviderWritePreviewDisplaysConsentBoundary(t *testing.T) {
	t.Parallel()
	for _, action := range []queue.Action{queue.ActionStart, queue.ActionRecordProgress, queue.ActionAssignToMe} {
		m := New(fixture())
		m.Width = 240
		m.WritePreview = &WritePreview{AuthorityProfile: "local", Scope: "portable", Intent: queue.WriteIntent{Action: action, Ref: queue.Ref{SourceID: "a", ItemID: "1"}, AuthorityID: "authority-1", ClaimID: "claim-1", Resources: []string{"resource-1"}, Marker: "worklease-op:example"}, Effect: "provider mutation", SideEffects: []string{"watcher notification"}, Races: []string{"external writers"}}
		view := screenText(m.View())
		for _, expected := range []string{"authority-1", "claim claim-1", "Resource resource-1", "Provider effect provider mutation", "Side effects watcher notification", "Declared races external writers", "Marker worklease-op:example", "lost response requires read-back", "claim remains held"} {
			if !strings.Contains(strings.ToLower(view), strings.ToLower(expected)) {
				t.Fatalf("%s preview missing %q: %s", action, expected, view)
			}
		}
	}
}

func TestConfiguredRecoveryViewIsNotShadowed(t *testing.T) {
	t.Parallel()
	m := New(fixture())
	m.Views = []string{"Recovery", RecoveryViewID}
	m.ViewFilters = map[string]queue.Filters{"Recovery": {SourceIDs: []string{"a"}}}
	m.ViewName = "Recovery"
	if view := m.View(); !strings.Contains(view, "first") || strings.Contains(view, "unresolved writes") {
		t.Fatalf("configured view was shadowed: %s", view)
	}
	m.ViewName = RecoveryViewID
	if view := m.View(); !strings.Contains(view, "0 unresolved writes") {
		t.Fatalf("built-in recovery view missing: %s", view)
	}
}

func TestRecoveryViewAndItemTabShowSameUnresolvedWrite(t *testing.T) {
	t.Parallel()
	m := New(fixture())
	ref := queue.Ref{SourceID: "a", ItemID: "1"}
	entry := queue.RecoveryEntry{OperationID: "operation-1", Ref: ref, Action: queue.ActionRecordProgress, Status: "dispatching", ClaimID: "claim-1", Effect: "append progress", Readback: "unknown", Next: []string{"retry read-back"}}
	next, _ := m.Update(RecoveryMsg{Entries: []queue.RecoveryEntry{entry}})
	m = next.(Model)
	m.ViewName = RecoveryViewID
	if view := m.View(); !strings.Contains(view, "operation-1") || !strings.Contains(view, "claim held claim-1") || !strings.Contains(view, "retry read-back") {
		t.Fatalf("recovery view missing operation: %s", view)
	}
	m.ViewName, m.Detail, m.Tab = "All", true, 4
	if view := m.View(); !strings.Contains(view, "operation-1") || !regexp.MustCompile(`Next +retry read-back`).MatchString(view) {
		t.Fatalf("item Recovery tab missing operation: %s", view)
	}
}

func TestSnapshotRevisionAndIndependentClaimFreshness(t *testing.T) {
	m := New(fixture())
	ref := queue.Ref{SourceID: "a", ItemID: "1"}
	key := ref.Key()
	base := m.Snapshot.Items[key]
	base.Claim = queue.ClaimObservation{Known: true, State: "free", ObservedAt: time.Unix(100, 0)}
	m.Snapshot.Items[key] = base
	newer := fixture()
	newer.Revision = 2
	item := newer.Items[key]
	item.Title = "new provider state"
	item.Claim = queue.ClaimObservation{Known: true, Active: true, State: "held", ObservedAt: time.Unix(101, 0)}
	newer.Items[key] = item
	next, _ := m.Update(SnapshotMsg{Snapshot: newer})
	m = next.(Model)
	older := fixture()
	older.Revision = 1
	next, _ = m.Update(SnapshotMsg{Snapshot: older})
	m = next.(Model)
	if m.Snapshot.Items[key].Title != item.Title || m.Snapshot.Items[key].Claim.State != "held" {
		t.Fatalf("regressed snapshot: %+v", m.Snapshot.Items[key])
	}
	claims := newer.Clone()
	observed := claims.Items[key]
	observed.Claim = queue.ClaimObservation{Known: false, Stale: true, State: "unknown", ObservedAt: time.Unix(102, 0)}
	claims.Items[key] = observed
	next, _ = m.Update(ClaimOverlayMsg{Snapshot: claims, Rebuilding: true})
	m = next.(Model)
	if m.Snapshot.Items[key].Title != item.Title || m.Snapshot.Items[key].Claim.Known || !strings.Contains(m.View(), "claims rebuilding") {
		t.Fatalf("claim/provider freshness coupled: %+v", m.Snapshot.Items[key])
	}
}

func TestHistoryGapClaimSurvivesProviderSnapshotUntilRebaseline(t *testing.T) {
	t.Parallel()
	key := queue.Ref{SourceID: "a", ItemID: "1"}.Key()
	m := New(fixture())
	initial := m.Snapshot.Items[key]
	initial.Claim = queue.ClaimObservation{Known: true, Active: true, State: "held", ObservedAt: time.Unix(100, 0)}
	m.Snapshot.Items[key] = initial

	gap := m.Snapshot.Clone()
	invalidated := gap.Items[key]
	invalidated.Claim = queue.ClaimObservation{Stale: true, State: "unknown", Reason: "history-gap", ObservedAt: time.Unix(100, 0)}
	gap.Items[key] = invalidated
	next, _ := m.Update(ClaimOverlayMsg{Snapshot: gap, Rebuilding: true})
	m = next.(Model)

	provider := m.Snapshot.Clone()
	provider.Revision++
	providerItem := provider.Items[key]
	providerItem.Claim = queue.ClaimObservation{Known: true, State: "free", ObservedAt: time.Unix(200, 0)}
	provider.Items[key] = providerItem
	next, _ = m.Update(SnapshotMsg{Snapshot: provider})
	m = next.(Model)
	if claim := m.Snapshot.Items[key].Claim; claim.Known || !claim.Stale || claim.Reason != "history-gap" {
		t.Fatalf("provider snapshot undid gap invalidation: %+v", claim)
	}

	rebaseline := provider.Clone()
	observed := rebaseline.Items[key]
	observed.Claim = queue.ClaimObservation{Known: true, Active: true, State: "held", ObservedAt: time.Unix(300, 0)}
	rebaseline.Items[key] = observed
	next, _ = m.Update(ClaimOverlayMsg{Snapshot: rebaseline})
	m = next.(Model)
	if claim := m.Snapshot.Items[key].Claim; !claim.Known || claim.Stale || claim.State != "held" {
		t.Fatalf("successful live rebaseline not applied: %+v", claim)
	}
}

func TestPreparedSnapshotIsIndependentOfProducerAndPreservesFreshClaim(t *testing.T) {
	producer := fixture()
	message := PrepareSnapshot(producer)
	key := queue.Ref{SourceID: "a", ItemID: "1"}.Key()
	item := producer.Items[key]
	item.Title = "changed after preparation"
	producer.Items[key] = item
	m := New(fixture())
	current := m.Snapshot.Items[key]
	current.Claim = queue.ClaimObservation{Known: true, State: "free", ObservedAt: time.Unix(200, 0)}
	m.Snapshot.Items[key] = current
	message.Snapshot.Revision = 2
	next, _ := m.Update(message)
	m = next.(Model)
	if got := m.Snapshot.Items[key]; got.Title != "first" || got.Claim.State != "free" {
		t.Fatalf("prepared snapshot lost ownership or newer claim: %+v", got)
	}
	if producer.Items[key].Claim.State == "free" {
		t.Fatal("model mutated producer snapshot")
	}
}

func TestPreparedProjectionPreservesFilteringDeduplicationAndLiveClaims(t *testing.T) {
	snapshot := fixture()
	first := snapshot.Items[queue.Ref{SourceID: "a", ItemID: "1"}.Key()]
	first.Order = "01"
	snapshot.Items[first.Ref.Key()] = first
	duplicate := first
	duplicate.Ref.ItemID = "3"
	duplicate.Title = "matching duplicate"
	duplicate.Order = "02"
	snapshot.Items[duplicate.Ref.Key()] = duplicate
	other := snapshot.Items[queue.Ref{SourceID: "a", ItemID: "2"}.Key()]
	other.Order = "03"
	snapshot.Items[other.Ref.Key()] = other
	source := queue.Source{ID: "a"}
	m := New(fixture())
	m.Sources = []queue.Source{source}
	next, _ := m.Update(PrepareSnapshot(snapshot, source))
	m = next.(Model)
	if len(m.orderedKeys) != 3 || len(m.rows()) != 2 {
		t.Fatalf("prepared order/dedup: keys=%d rows=%d", len(m.orderedKeys), len(m.rows()))
	}
	m.Filter = "matching"
	if got := m.rows(); len(got) != 1 || got[0].Ref.ItemID != "3" {
		t.Fatalf("filter must precede canonical deduplication: %+v", got)
	}
	m.Filter = ""
	claims := snapshot.Clone()
	item := claims.Items[first.Ref.Key()]
	item.Claim = queue.ClaimObservation{Known: true, State: "free", ObservedAt: time.Now()}
	claims.Items[first.Ref.Key()] = item
	next, _ = m.Update(ClaimOverlayMsg{Snapshot: claims})
	m = next.(Model)
	if len(m.orderedKeys) != 3 || len(m.rows()) != 2 || m.rows()[0].Claim.State != "free" {
		t.Fatalf("overlay invalidated order or retained stale claim: %+v", m.rows())
	}
}

func TestPreparedConfiguredViewRowsAndCounts(t *testing.T) {
	t.Parallel()
	snapshot := fixture()
	ready := snapshot.Items[queue.Ref{SourceID: "a", ItemID: "1"}.Key()]
	ready.Readiness.Status = queue.Ready
	snapshot.Items[ready.Ref.Key()] = ready
	other := snapshot.Items[queue.Ref{SourceID: "a", ItemID: "2"}.Key()]
	other.Readiness.Status = queue.ReadinessUnknown
	snapshot.Items[other.Ref.Key()] = other

	m := New(snapshot)
	m.Sources = []queue.Source{{ID: "a"}}
	m.ViewName = "ReadyForReview"
	m.Views = []string{"ReadyForReview"}
	m.ViewFilters = map[string]queue.Filters{"ReadyForReview": {SourceIDs: []string{"a"}}}
	m.ViewRules = map[string]ViewRule{"ReadyForReview": {Readiness: string(queue.Ready)}}
	prepared := PrepareSnapshotForModel(snapshot, m)
	if len(prepared.preparedRows) != 1 || prepared.preparedRows[0].Ref.ItemID != "1" {
		t.Fatalf("configured-view worker rows = %+v, want only ready row", prepared.preparedRows)
	}
	newer := m.Snapshot.Items[ready.Ref.Key()]
	newer.Claim = queue.ClaimObservation{Known: true, Available: true, State: "free", ObservedAt: time.Unix(200, 0)}
	m.Snapshot.Items[ready.Ref.Key()] = newer
	if got := prepared.preparedCounts["ReadyForReview"]; got != 1 {
		t.Fatalf("configured-view worker count = %d, want 1", got)
	}
	next, _ := m.Update(prepared)
	m = next.(Model)
	if got := m.rowCache.counts["ReadyForReview"]; got != 1 {
		t.Fatalf("configured-view prepared count = %d, want 1", got)
	}
	if len(m.rowCache.rows) != 1 || &m.rowCache.rows[0] != &prepared.preparedRows[0] {
		t.Fatal("configured-view prepared rows were rebuilt on the event loop")
	}
	if got := m.rowCache.rows[0].Claim; got.State != "free" || !got.ObservedAt.Equal(time.Unix(200, 0)) {
		t.Fatalf("prepared row lost newer claim overlay: %+v", got)
	}
}

func TestPreparedProjectionFallsBackWhenViewChanges(t *testing.T) {
	t.Parallel()
	snapshot := fixture()
	ready := snapshot.Items[queue.Ref{SourceID: "a", ItemID: "1"}.Key()]
	ready.Readiness.Status = queue.Ready
	snapshot.Items[ready.Ref.Key()] = ready
	m := New(snapshot)
	m.Sources = []queue.Source{{ID: "a"}}
	m.ViewName = "ReadyForReview"
	m.Views = []string{"ReadyForReview"}
	m.ViewFilters = map[string]queue.Filters{"ReadyForReview": {SourceIDs: []string{"a"}}}
	m.ViewRules = map[string]ViewRule{"ReadyForReview": {Readiness: string(queue.Ready)}}
	prepared := PrepareSnapshotForModel(snapshot, m)
	m.ViewName = "All"

	next, _ := m.Update(prepared)
	m = next.(Model)
	if got := m.rows(); len(got) != 2 {
		t.Fatalf("changed view adopted stale prepared projection: got %d rows, want 2", len(got))
	}
	if &m.rowCache.rows[0] == &prepared.preparedRows[0] {
		t.Fatal("changed view reused rows prepared for the previous view")
	}
}

func TestPreparedConfiguredViewAppliesNewerClaimOverlay(t *testing.T) {
	t.Parallel()
	base := fixture()
	aKey := queue.Ref{SourceID: "a", ItemID: "1"}.Key()
	a := base.Items[aKey]
	a.Readiness.Status = queue.Ready
	base.Items[aKey] = a
	incoming := base.Clone()
	incoming.Revision = 2
	a = incoming.Items[aKey]
	a.Claim = queue.ClaimObservation{Known: true, State: "free", ObservedAt: time.Unix(100, 0)}
	incoming.Items[aKey] = a

	m := New(base)
	m.Sources = []queue.Source{{ID: "a"}}
	m.ViewName = "ReadyForReview"
	m.Views = []string{"ReadyForReview"}
	m.ViewFilters = map[string]queue.Filters{"ReadyForReview": {SourceIDs: []string{"a"}}}
	m.ViewRules = map[string]ViewRule{"ReadyForReview": {Readiness: string(queue.Ready), Claim: "held"}}
	a = m.Snapshot.Items[aKey]
	a.Claim = queue.ClaimObservation{Known: true, Active: true, State: "held", ObservedAt: time.Unix(200, 0)}
	m.Snapshot.Items[aKey] = a

	prepared := PrepareSnapshotForModel(incoming, m)
	if len(prepared.preparedRows) != 0 {
		t.Fatalf("worker projection unexpectedly included stale free claim: %v", prepared.preparedRows)
	}
	next, _ := m.Update(prepared)
	m = next.(Model)
	rows := m.rows()
	if len(rows) != 1 || rows[0].Ref.ItemID != "1" || rows[0].Claim.State != "held" {
		t.Fatalf("newer claim overlay missing from configured view: %+v", rows)
	}
	if got := m.rowCache.counts["ReadyForReview"]; got != 1 {
		t.Fatalf("configured-view count after newer claim overlay = %d, want 1", got)
	}
}

func TestPreparedClaimedViewUsesNewerClaimOverlay(t *testing.T) {
	t.Parallel()
	base := fixture()
	aKey := queue.Ref{SourceID: "a", ItemID: "1"}.Key()
	incoming := base.Clone()
	incoming.Revision = 2
	a := incoming.Items[aKey]
	a.Claim = queue.ClaimObservation{Known: true, State: "free", ObservedAt: time.Unix(100, 0)}
	incoming.Items[aKey] = a

	m := New(base)
	m.Sources = []queue.Source{{ID: "a"}}
	m.ViewName = "Claimed"
	a = m.Snapshot.Items[aKey]
	a.Claim = queue.ClaimObservation{Known: true, Active: true, State: "held", ObservedAt: time.Unix(200, 0)}
	m.Snapshot.Items[aKey] = a

	prepared := PrepareSnapshotForModel(incoming, m)
	if len(prepared.preparedRows) != 0 {
		t.Fatal("worker should not include the stale free claim in Claimed")
	}
	next, _ := m.Update(prepared)
	m = next.(Model)
	rows := m.rows()
	if len(rows) != 1 || rows[0].Ref.ItemID != "1" || !rows[0].Claim.Active {
		t.Fatalf("newer claim overlay missing from Claimed view: %+v", rows)
	}
	if got := m.rowCache.counts["Claimed"]; got != 1 {
		t.Fatalf("Claimed count after newer overlay = %d, want 1", got)
	}
}

func TestPreparedAllRowsKeepNewerOverlayAndSourceOrder(t *testing.T) {
	producer := fixture()
	key := queue.Ref{SourceID: "a", ItemID: "1"}.Key()
	m := New(producer)
	m.Sources = []queue.Source{{ID: "a"}}
	current := m.Snapshot.Items[key]
	current.Claim = queue.ClaimObservation{Known: true, State: "free", ObservedAt: time.Unix(200, 0)}
	m.Snapshot.Items[key] = current
	producer.Revision++
	next, _ := m.Update(PrepareSnapshot(producer, m.Sources...))
	m = next.(Model)
	if got := m.rows()[0].Claim.State; got != "free" {
		t.Fatalf("prepared All row lost newer claim: %s", got)
	}
	if got := m.rowCache.rows[0].Claim.State; got != "free" {
		t.Fatalf("cached All row lost newer claim: %s", got)
	}

	// A preparation for a different source order cannot seed the All cache.
	m.Sources = []queue.Source{{ID: "other"}}
	producer.Revision++
	next, _ = m.Update(PrepareSnapshot(producer, queue.Source{ID: "a"}))
	m = next.(Model)
	if len(m.rows()) != 0 {
		t.Fatal("prepared rows bypassed current source selection")
	}
}

func TestStandardViewCountsMatchConfiguredScan(t *testing.T) {
	snapshot := fixture()
	firstKey := queue.Ref{SourceID: "a", ItemID: "1"}.Key()
	first := snapshot.Items[firstKey]
	first.AssignedTo = []string{"other", "brett", "brett"}
	first.Readiness.Status = queue.Ready
	snapshot.Items[firstKey] = first
	m := New(snapshot)
	m.Me = "brett"
	m.MeBySource = map[string][]string{"a": {"brett"}}
	m.rows()
	for _, name := range m.Views {
		if got, want := m.rowCache.counts[name], m.viewCount(name); got != want {
			t.Errorf("%s count = %d, want %d", name, got, want)
		}
	}
	m.ViewFilters = map[string]queue.Filters{"Ready": {SourceIDs: []string{"different"}}}
	m.rows()
	if got, want := m.rowCache.counts["Ready"], m.viewCount("Ready"); got != want {
		t.Errorf("configured count = %d, want %d", got, want)
	}
}

func TestClaimUpdatesWhileProviderSnapshotIsStalled(t *testing.T) {
	m := New(fixture())
	stalled := m.Snapshot.Clone()
	stalled.Sources["a"] = queue.Coverage{State: queue.CoveragePartial, Total: 2, TotalAccuracy: queue.TotalExact}
	next, _ := m.Update(SnapshotMsg{Snapshot: stalled})
	m = next.(Model)
	claims := stalled.Clone()
	key := queue.Ref{SourceID: "a", ItemID: "1"}.Key()
	item := claims.Items[key]
	item.Claim = queue.ClaimObservation{Known: true, State: "free", ObservedAt: time.Now()}
	claims.Items[key] = item
	next, _ = m.Update(ClaimOverlayMsg{Snapshot: claims})
	m = next.(Model)
	if m.Snapshot.Items[key].Claim.State != "free" || m.ClaimFreshness != "fresh" || !strings.Contains(m.View(), "provider stale") {
		t.Fatalf("stalled provider suppressed live claim: %+v", m.Snapshot.Items[key].Claim)
	}
}

func TestHistoryGapOverridesPreviouslyObservedClaim(t *testing.T) {
	m := New(fixture())
	key := queue.Ref{SourceID: "a", ItemID: "1"}.Key()
	item := m.Snapshot.Items[key]
	item.Claim.ObservedAt = time.Unix(100, 0)
	m.Snapshot.Items[key] = item
	gap := m.Snapshot.Clone()
	item = gap.Items[key]
	item.Claim = queue.ClaimObservation{State: "unknown", Stale: true, Reason: "history-gap", ObservedAt: time.Unix(100, 0)}
	gap.Items[key] = item
	next, _ := m.Update(ClaimOverlayMsg{Snapshot: gap, Rebuilding: true})
	m = next.(Model)
	if m.Snapshot.Items[key].Claim.Known || !m.Snapshot.Items[key].Claim.Stale || !strings.Contains(m.View(), "claims rebuilding") {
		t.Fatalf("history gap left stale active claim visible: %+v", m.Snapshot.Items[key].Claim)
	}
}

func TestClaimStateDisplaysStaleWithoutHidingDenialReason(t *testing.T) {
	for _, tc := range []struct {
		claim queue.ClaimObservation
		want  string
	}{
		{claim: queue.ClaimObservation{State: "unknown", Stale: true}, want: "unknown (stale)"},
		{claim: queue.ClaimObservation{State: "unknown", Reason: "admission-unknown", Stale: true}, want: "admission-unknown (stale)"},
	} {
		if got := claimState(queue.Item{Claim: tc.claim}); got != tc.want {
			t.Errorf("claimState() = %q, want %q", got, tc.want)
		}
	}
}

func TestSourceReadFailureLabelsIncludeRateLimitDeadline(t *testing.T) {
	retryAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	labels := []string{
		sourceState(queue.Coverage{Reason: "rate-limited", RetryAt: retryAt}),
		sourceState(queue.Coverage{Reason: "permission-denied"}),
		sourceState(queue.Coverage{Reason: "offline"}),
	}
	if labels[0] != "rate-limited retry Jan 2 03:04:05Z" || labels[1] != "permission-denied" || labels[2] != "offline" {
		t.Fatalf("source failure labels: %v", labels)
	}
}

func TestDetailStaysVisibleWhileSelectedItemRevalidates(t *testing.T) {
	t.Parallel()
	initial := fixture()
	initial.Revision = 1
	ref := queue.Ref{SourceID: "a", ItemID: "1"}
	item := initial.Items[ref.Key()]
	item.Body = "Previously observed description"
	item.ReadOutcome = "found"
	item.Observation.ProviderVersion = "previous-version"
	item.Readiness.Status = queue.Ready
	item.Relationships = []queue.Relationship{{Type: queue.HardPrerequisite, To: queue.Ref{SourceID: "a", ItemID: "2"}, Fresh: true}}
	initial.Items[ref.Key()] = item
	m := New(initial)
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	m.Detail = true
	m.DetailOffset = 2
	m.Comments = []queue.GitHubComment{{Body: "Previously loaded comment"}}
	m.CommentsIdentity = DetailRequestIdentity(item)
	hydrates := 0
	m.HydrateSelected = func(queue.Item) tea.Cmd { hydrates++; return nil }

	refresh := initial.Clone()
	refresh.Revision = 2
	summary := queue.Item{Summary: item.Summary, ReadOutcome: "summary-only", Readiness: queue.Readiness{Status: queue.ReadinessUnknown}, Observation: item.Observation, Claim: queue.ClaimObservation{ObservedAt: time.Now()}}
	summary.Observation.ProviderVersion = ""
	refresh.Items[ref.Key()] = summary
	next, _ := m.Update(PrepareSnapshotForModel(refresh, m))
	m = next.(Model)
	if m.Selected != identity(item) || m.DetailOffset != 2 || hydrates != 1 {
		t.Fatalf("refresh interrupted selection or hydration: selected=%q offset=%d hydrates=%d", m.Selected, m.DetailOffset, hydrates)
	}
	if !strings.Contains(screenText(m.View()), item.Body) {
		t.Fatal("open detail pane lost its description during refresh")
	}
	current := m.Snapshot.Items[ref.Key()]
	if current.Body != "" || len(current.Relationships) != 0 || len(current.Resources) != 0 || queue.EvaluateAction(current, queue.ActionStart).Eligible {
		t.Fatalf("cached details leaked into current action evidence: %+v", current)
	}
	for tab, want := range map[int]string{0: item.Body, 1: "a:2", 2: "previous-version", 3: "resource:1"} {
		m.Tab = tab
		text := screenText(strings.Join(m.detailContent(current, 70), " "))
		if !strings.Contains(text, want) || !strings.Contains(strings.ToLower(text), "previously observed") {
			t.Fatalf("tab %d lost cached detail: %q", tab, text)
		}
	}
	if len(m.Comments) != 1 || m.Comments[0].Body != "Previously loaded comment" {
		t.Fatal("refresh lost previously loaded activity")
	}
	m.Tab = 0
	refresh.Revision = 3
	next, _ = m.Update(PrepareSnapshotForModel(refresh, m))
	m = next.(Model)
	if hydrates != 1 || !strings.Contains(screenText(strings.Join(m.detailContent(m.Snapshot.Items[ref.Key()], 70), " ")), item.Body) {
		t.Fatal("intermediate snapshot lost detail or repeated selected hydration")
	}

	verified := item
	verified.Body = "Updated description"
	verified.Readiness.Status = queue.Ready
	refresh.Revision = 4
	refresh.Items[ref.Key()] = verified
	next, _ = m.Update(PrepareSnapshotForModel(refresh, m))
	m = next.(Model)
	m.Tab = 0
	text := screenText(strings.Join(m.detailContent(m.Snapshot.Items[ref.Key()], 70), " "))
	if !strings.Contains(text, verified.Body) || strings.Contains(text, item.Body) || strings.Contains(text, "previously observed") {
		t.Fatalf("verified details failed to replace cached presentation: %q", text)
	}
	delete(refresh.Items, ref.Key())
	refresh.Revision = 5
	next, _ = m.Update(PrepareSnapshotForModel(refresh, m))
	m = next.(Model)
	if _, exists := m.displayDetails[ref.Key()]; exists {
		t.Fatal("removed item retained cached detail")
	}
}

func TestPreviouslyObservedDetailsDisappearOnRevocation(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"removed", "missing", "principal-changed", "identity-changed"} {
		t.Run(scenario, func(t *testing.T) {
			initial := fixture()
			initial.Revision = 1
			ref := queue.Ref{SourceID: "a", ItemID: "1"}
			item := initial.Items[ref.Key()]
			item.Body = "private previous description"
			item.Observation.Principal = "alice"
			item.ReadOutcome = "found"
			initial.Items[ref.Key()] = item
			m := New(initial)
			m.Sources = []queue.Source{{ID: "a"}}
			m.anchor(m.rows())
			m.Detail = true
			oldRequest := DetailRequestIdentity(item)
			m.CommentsIdentity = oldRequest
			m.Comments = []queue.GitHubComment{{Body: "private previous comment"}}
			m.HistoryIdentity = oldRequest
			m.History = ledger.HistoryPage{Epochs: []ledger.Epoch{{AgentID: "private previous agent"}}}
			refresh := initial.Clone()
			refresh.Revision = 2
			summary := queue.Item{Summary: item.Summary, ReadOutcome: "summary-only", Observation: item.Observation}
			refresh.Items[ref.Key()] = summary
			next, _ := m.Update(PrepareSnapshotForModel(refresh, m))
			m = next.(Model)
			if _, ok := m.displayDetails[ref.Key()]; !ok {
				t.Fatal("test did not cache previously observed detail")
			}
			refresh.Revision = 3
			switch scenario {
			case "removed":
				delete(refresh.Items, ref.Key())
			case "missing":
				summary.ReadOutcome = "missing"
				summary.Fresh = false
				refresh.Items[ref.Key()] = summary
			case "principal-changed":
				summary.Observation.Principal = "bob"
				refresh.Items[ref.Key()] = summary
			case "identity-changed":
				summary.CanonicalID = "replacement"
				refresh.Items[ref.Key()] = summary
			}
			next, _ = m.Update(PrepareSnapshotForModel(refresh, m))
			m = next.(Model)
			if _, ok := m.displayDetails[ref.Key()]; ok || strings.Contains(screenText(m.View()), item.Body) || len(m.Comments) != 0 || len(m.History.Epochs) != 0 {
				t.Fatal("private details survived item removal or identity change")
			}
			if scenario == "principal-changed" {
				oldOverlay := initial.Clone()
				stale := oldOverlay.Items[ref.Key()]
				stale.Claim.Stale = true
				oldOverlay.Items[ref.Key()] = stale
				next, _ = m.Update(ClaimOverlayMsg{Snapshot: oldOverlay})
				m = next.(Model)
				if len(m.Snapshot.Items[ref.Key()].Resources) != 0 {
					t.Fatal("late prior-principal claim overlay restored old resources")
				}
			}
			next, _ = m.Update(CommentsMsg{Identity: oldRequest, Comments: []queue.GitHubComment{{Body: "late private comment"}}})
			m = next.(Model)
			next, _ = m.Update(HistoryMsg{Identity: oldRequest, Page: ledger.HistoryPage{Epochs: []ledger.Epoch{{AgentID: "late private agent"}}}})
			m = next.(Model)
			m.Tab = 2
			activity := screenText(m.View())
			m.Tab = 3
			claims := screenText(m.View())
			if len(m.Comments) != 0 || len(m.History.Epochs) != 0 || strings.Contains(activity, "private") || strings.Contains(claims, "private") {
				t.Fatal("late response repopulated prior-principal activity or history")
			}
		})
	}
}

func TestLateDetailResponsesCannotReturnAfterOwnerCycle(t *testing.T) {
	t.Parallel()
	initial := fixture()
	initial.Revision = 1
	ref := queue.Ref{SourceID: "a", ItemID: "1"}
	item := initial.Items[ref.Key()]
	item.Observation.Principal = "alice"
	item.ReadOutcome = "found"
	initial.Items[ref.Key()] = item
	m := New(initial)
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	m.Detail, m.Tab = true, 1
	commentsCalls, historyCalls := 0, 0
	m.LoadComments = func(item queue.Item, cursor string) tea.Cmd {
		commentsCalls++
		call := commentsCalls
		return func() tea.Msg {
			return CommentsMsg{Identity: DetailRequestIdentity(item), Comments: []queue.GitHubComment{{Body: fmt.Sprintf("comment-%d", call)}}}
		}
	}
	m.LoadHistory = func(item queue.Item, cursor string, before bool) tea.Cmd {
		historyCalls++
		call := historyCalls
		return func() tea.Msg {
			return HistoryMsg{Identity: DetailRequestIdentity(item), Page: ledger.HistoryPage{Epochs: []ledger.Epoch{{AgentID: fmt.Sprintf("agent-%d", call)}}}}
		}
	}
	m, oldComments := press(m, "tab")
	m, oldHistory := press(m, "tab")
	if oldComments == nil || oldHistory == nil {
		t.Fatal("initial requests not dispatched")
	}
	refresh := initial.Clone()
	refresh.Revision = 2
	summary := queue.Item{Summary: item.Summary, Observation: item.Observation, ReadOutcome: "summary-only"}
	summary.Observation.Principal = "bob"
	refresh.Items[ref.Key()] = summary
	next, _ := m.Update(PrepareSnapshotForModel(refresh, m))
	m = next.(Model)
	if len(m.Comments) != 0 || len(m.History.Epochs) != 0 || len(m.Snapshot.Items[ref.Key()].Resources) != 0 {
		t.Fatal("prior-principal details survived owner change")
	}
	refresh.Revision = 3
	summary.Observation.Principal = "alice"
	refresh.Items[ref.Key()] = summary
	next, _ = m.Update(PrepareSnapshotForModel(refresh, m))
	m = next.(Model)
	m.Tab = 1
	m, newComments := press(m, "tab")
	if newComments == nil {
		t.Fatal("returning principal did not request fresh comments")
	}
	oldCommentMsg := oldComments().(CommentsMsg)
	newCommentMsg := newComments().(CommentsMsg)
	if oldCommentMsg.Identity != newCommentMsg.Identity || oldCommentMsg.Generation == newCommentMsg.Generation {
		t.Fatal("request generations did not distinguish A→B→A")
	}
	next, _ = m.Update(oldCommentMsg)
	m = next.(Model)
	if len(m.Comments) != 0 {
		t.Fatal("old comments response returned after owner cycle")
	}
	next, _ = m.Update(newCommentMsg)
	m = next.(Model)
	if len(m.Comments) != 1 || m.Comments[0].Body != "comment-2" {
		t.Fatal("new owner's response was not accepted")
	}
	refresh.Revision = 4
	refresh.Items[ref.Key()] = item
	next, _ = m.Update(PrepareSnapshotForModel(refresh, m))
	m = next.(Model)
	m, newHistory := press(m, "tab")
	if newHistory == nil {
		t.Fatal("returning principal did not request fresh history")
	}
	oldHistoryMsg := oldHistory().(HistoryMsg)
	newHistoryMsg := newHistory().(HistoryMsg)
	if oldHistoryMsg.Identity != newHistoryMsg.Identity || oldHistoryMsg.Generation == newHistoryMsg.Generation {
		t.Fatal("history request generations did not distinguish A→B→A")
	}
	next, _ = m.Update(oldHistoryMsg)
	m = next.(Model)
	if len(m.History.Epochs) != 0 {
		t.Fatal("old history response returned after owner cycle")
	}
	next, _ = m.Update(newHistoryMsg)
	m = next.(Model)
	if len(m.History.Epochs) != 1 || m.History.Epochs[0].AgentID != "agent-2" {
		t.Fatal("new owner's history response was not accepted")
	}
}

func TestSelectedSummaryHydratesOnOwnerChangeWithSameObservation(t *testing.T) {
	t.Parallel()
	for _, tab := range []int{0, 2} {
		t.Run(fmt.Sprint(tab), func(t *testing.T) {
			initial := fixture()
			initial.Revision = 1
			ref := queue.Ref{SourceID: "a", ItemID: "1"}
			item := initial.Items[ref.Key()]
			item.ReadOutcome = "summary-only"
			item.Observation.Principal = "alice"
			initial.Items[ref.Key()] = item
			m := New(initial)
			m.Sources = []queue.Source{{ID: "a"}}
			m.anchor(m.rows())
			m.Detail, m.Tab = true, tab
			hydrates, comments := 0, 0
			m.HydrateSelected = func(queue.Item) tea.Cmd { hydrates++; return func() tea.Msg { return nil } }
			m.LoadComments = func(item queue.Item, _ string) tea.Cmd {
				comments++
				return func() tea.Msg { return CommentsMsg{Identity: DetailRequestIdentity(item)} }
			}
			refresh := initial.Clone()
			refresh.Revision = 2
			item.Observation.Principal = "bob" // same timestamp, still summary-only
			item.Resources = nil
			item.KeyInputs = nil
			refresh.Items[ref.Key()] = item
			next, cmd := m.Update(PrepareSnapshotForModel(refresh, m))
			m = next.(Model)
			if hydrates != 1 || cmd == nil || comments != map[int]int{0: 0, 2: 1}[tab] || len(m.Snapshot.Items[ref.Key()].Resources) != 0 {
				t.Fatalf("owner change skipped hydration or retained stale claim inputs: hydrates=%d comments=%d", hydrates, comments)
			}
		})
	}
}

func TestReadyViewDoesNotCarryRecheckAcrossIdentity(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"principal", "canonical"} {
		for _, intermediate := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/intermediate=%t", field, intermediate), func(t *testing.T) {
				initial := fixture()
				initial.Revision = 1
				ref := queue.Ref{SourceID: "a", ItemID: "1"}
				item := initial.Items[ref.Key()]
				item.Readiness.Status = queue.Ready
				item.Observation.Principal = "alice"
				initial.Items[ref.Key()] = item
				m := New(initial)
				m.Sources = []queue.Source{{ID: "a"}}
				m.ViewName = "Ready"
				summary := item
				summary.ReadOutcome = "summary-only"
				summary.Readiness.Status = queue.ReadinessUnknown
				if intermediate {
					pending := initial.Clone()
					pending.Revision = 2
					pending.Items[ref.Key()] = summary
					next, _ := m.Update(PrepareSnapshotForModel(pending, m))
					m = next.(Model)
					if len(m.rows()) != 1 {
						t.Fatal("test did not establish a rechecking row")
					}
				}
				if field == "principal" {
					summary.Observation.Principal = "bob"
				} else {
					summary.CanonicalID = "replacement"
				}
				changed := initial.Clone()
				changed.Revision = 3
				changed.Items[ref.Key()] = summary
				next, _ := m.Update(PrepareSnapshotForModel(changed, m))
				m = next.(Model)
				if len(m.rows()) != 0 || m.viewCountFor("Ready") != 0 {
					t.Fatalf("unverified replacement survived in Ready: %+v", m.rows())
				}
			})
		}
	}
}

func TestAllViewUsesPreparedRowsDuringReadyRecheck(t *testing.T) {
	t.Parallel()
	initial := fixture()
	initial.Revision = 1
	ref := queue.Ref{SourceID: "a", ItemID: "1"}
	item := initial.Items[ref.Key()]
	item.Readiness.Status = queue.Ready
	initial.Items[ref.Key()] = item
	m := New(initial)
	m.Sources = []queue.Source{{ID: "a"}}
	refresh := initial.Clone()
	refresh.Revision = 2
	item.ReadOutcome = "summary-only"
	item.Readiness.Status = queue.ReadinessUnknown
	refresh.Items[ref.Key()] = item
	prepared := PrepareSnapshotForModel(refresh, m)
	next, _ := m.Update(prepared)
	m = next.(Model)
	if len(m.rows()) != 2 || m.viewCountFor("Ready") != 1 || len(m.rowCache.rows) != len(prepared.preparedRows) || &m.rowCache.rows[0] != &prepared.preparedRows[0] {
		t.Fatal("All view rebuilt worker-prepared rows or miscounted Ready during recheck")
	}
}

func TestSelectedOffscreenSummaryHydratesWithListOnly(t *testing.T) {
	t.Parallel()
	initial := queue.Snapshot{Revision: 1, Items: make(map[string]queue.Item), Sources: map[string]queue.Coverage{"a": {State: queue.CoverageComplete}}}
	for n := range 40 {
		ref := queue.Ref{SourceID: "a", ItemID: fmt.Sprintf("%03d", n)}
		initial.Items[ref.Key()] = queue.Item{Summary: queue.Summary{Ref: ref, Title: ref.ItemID, Fresh: true}, Body: "loaded detail", ReadOutcome: "found"}
	}
	m := New(initial)
	m.Sources = []queue.Source{{ID: "a"}}
	m.Height = 10
	m.selectIndex(m.rows(), 39)
	ref := queue.Ref{SourceID: "a", ItemID: "039"}
	if m.Selected != ref.Key() || m.Detail {
		t.Fatal("test did not select an offscreen row in list mode")
	}
	hydrates := 0
	m.HydrateSelected = func(item queue.Item) tea.Cmd {
		if item.Ref != ref {
			t.Fatalf("hydrated wrong row: %+v", item.Ref)
		}
		hydrates++
		return nil
	}
	refresh := initial.Clone()
	refresh.Revision = 2
	item := refresh.Items[ref.Key()]
	item.Body = ""
	item.ReadOutcome = "summary-only"
	refresh.Items[ref.Key()] = item
	next, _ := m.Update(PrepareSnapshotForModel(refresh, m))
	m = next.(Model)
	if hydrates != 1 || m.Selected != ref.Key() {
		t.Fatalf("list-only refresh lost selected hydration: calls=%d selected=%q", hydrates, m.Selected)
	}
	m.Height = 35
	m, _ = press(m, "enter")
	if !strings.Contains(screenText(m.View()), "loaded detail") || hydrates != 1 {
		t.Fatal("opening selected offscreen item lost cached description or repeated hydration")
	}
}

func TestReadyViewKeepsRowsDuringRecheckWithoutAllowingActions(t *testing.T) {
	t.Parallel()
	initial := fixture()
	initial.Revision = 1
	for key, item := range initial.Items {
		item.Claim = queue.ClaimObservation{}
		item.Readiness.Status = queue.Ready
		initial.Items[key] = item
	}
	m := New(initial)
	m.Sources = []queue.Source{{ID: "a"}}
	m.ViewName = "Ready"
	m.ViewRules = map[string]ViewRule{"Ready": {Readiness: string(queue.Ready)}}
	m.anchor(m.rows())
	selected := m.Selected

	refresh := initial.Clone()
	refresh.Revision = 2
	for key, item := range refresh.Items {
		item.ReadOutcome = "summary-only"
		item.Readiness.Status = queue.ReadinessUnknown
		refresh.Items[key] = item
	}
	msg := PrepareSnapshotForModel(refresh, m)
	if len(msg.preparedRows) != 0 {
		t.Fatal("fixture must reproduce the empty worker projection")
	}
	next, _ := m.Update(msg)
	m = next.(Model)
	if rows := m.rows(); len(rows) != 2 || m.Selected != selected || m.viewCountFor("Ready") != 2 {
		t.Fatalf("refresh displaced previously ready rows: rows=%d selected=%q count=%d", len(rows), m.Selected, m.viewCountFor("Ready"))
	}
	if !strings.Contains(screenText(m.View()), "rechecking") {
		t.Fatal("unverified rows were not visibly marked")
	}
	for _, item := range m.rows() {
		if queue.EvaluateAction(item, queue.ActionStart).Eligible {
			t.Fatalf("rechecking item became actionable: %+v", item)
		}
	}

	// Rows return to verified readiness independently without displacing
	// still-rechecking rows or changing the selected item.
	firstRef := queue.Ref{SourceID: "a", ItemID: "1"}
	first := refresh.Items[firstRef.Key()]
	first.ReadOutcome = "found"
	first.Readiness.Status = queue.Ready
	refresh.Items[firstRef.Key()] = first
	refresh.Revision = 3
	next, _ = m.Update(PrepareSnapshotForModel(refresh, m))
	m = next.(Model)
	if len(m.rows()) != 2 || m.Selected != selected || m.viewCountFor("Ready") != 2 {
		t.Fatalf("partial recheck displaced rows: selected=%q count=%d", m.Selected, m.viewCountFor("Ready"))
	}

	// A detail read can report missing while retaining a stale diagnostic
	// item in the snapshot. That is not an in-progress recheck.
	first = refresh.Items[firstRef.Key()]
	first.ReadOutcome = "missing"
	first.Fresh = false
	first.Readiness.Status = queue.ReadinessUnknown
	refresh.Items[firstRef.Key()] = first
	refresh.Revision = 4
	next, _ = m.Update(PrepareSnapshotForModel(refresh, m))
	m = next.(Model)
	if len(m.rows()) != 1 || m.Selected != "stable-2" {
		t.Fatalf("missing item remained visible: selected=%q count=%d", m.Selected, len(m.rows()))
	}

	// A deleted item is removed immediately. A fully read item with unknown
	// readiness also leaves the Ready view rather than lingering indefinitely.
	delete(refresh.Items, firstRef.Key())
	other := refresh.Items[(queue.Ref{SourceID: "a", ItemID: "2"}).Key()]
	other.ReadOutcome = "found"
	refresh.Items[other.Ref.Key()] = other
	refresh.Revision = 5
	next, _ = m.Update(PrepareSnapshotForModel(refresh, m))
	m = next.(Model)
	if len(m.rows()) != 0 || m.Selected != "" || m.viewCountFor("Ready") != 0 {
		t.Fatalf("removed or verified-ineligible rows lingered: selected=%q count=%d", m.Selected, m.viewCountFor("Ready"))
	}
}

func TestDefaultReadyViewCountsRecheckingRows(t *testing.T) {
	t.Parallel()
	initial := fixture()
	initial.Revision = 1
	first := initial.Items[(queue.Ref{SourceID: "a", ItemID: "1"}).Key()]
	first.Readiness.Status = queue.Ready
	initial.Items[first.Ref.Key()] = first
	m := New(initial)
	m.Sources = []queue.Source{{ID: "a"}}
	m.ViewName = "Ready"

	refresh := initial.Clone()
	refresh.Revision = 2
	first.ReadOutcome = "summary-only"
	first.Readiness.Status = queue.ReadinessUnknown
	refresh.Items[first.Ref.Key()] = first
	next, _ := m.Update(PrepareSnapshotForModel(refresh, m))
	m = next.(Model)
	if len(m.rows()) != 1 || m.viewCountFor("Ready") != 1 {
		t.Fatalf("default Ready view count disagrees with rechecking row: shown=%d tab=%d", len(m.rows()), m.viewCountFor("Ready"))
	}
}

func TestNavigationRefreshAnchorAndLateHistory(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	if m.Selected != "stable-1" {
		t.Fatal(m.Selected)
	}
	m, _ = press(m, "j")
	if m.Selected != "stable-2" {
		t.Fatal(m.Selected)
	}
	updated := fixture()
	delete(updated.Items, queue.Ref{SourceID: "a", ItemID: "1"}.Key())
	next, _ := m.Update(SnapshotMsg{Snapshot: updated})
	m = next.(Model)
	if m.Selected != "stable-2" {
		t.Fatal("selection changed across refresh", m.Selected)
	}
	m, _ = press(m, "enter")
	if !m.Detail {
		t.Fatal("detail did not open")
	}
	m, _ = press(m, "esc")
	if m.Detail {
		t.Fatal("detail did not close")
	}
	next, _ = m.Update(HistoryMsg{Identity: "stable-1", Page: ledger.HistoryPage{Epochs: []ledger.Epoch{{AgentID: "wrong"}}}})
	m = next.(Model)
	if len(m.History.Epochs) != 0 {
		t.Fatal("late response stole detail")
	}
}
func TestActivityLoadsCommentsOnDemandAndIgnoresLatePages(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	calls := 0
	m.LoadComments = func(item queue.Item, cursor string) tea.Cmd {
		calls++
		if calls == 1 && cursor != "" || calls == 2 && cursor != "next" {
			t.Fatalf("unexpected comments cursor: %q", cursor)
		}
		return func() tea.Msg {
			return CommentsMsg{Identity: DetailRequestIdentity(item), Comments: []queue.GitHubComment{{Author: "alice", Body: "hello\x1b[31m world"}}, Cursor: map[bool]string{true: "next", false: ""}[calls == 1]}
		}
	}
	if calls != 0 {
		t.Fatal("comments fetched before activity was opened")
	}
	m, _ = press(m, "enter")
	m, _ = press(m, "tab")
	m, cmd := press(m, "tab")
	if calls != 1 || cmd == nil {
		t.Fatal("activity did not request comments")
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if !strings.Contains(m.View(), "hello world") || strings.Contains(m.View(), "\x1b[31m") {
		t.Fatal("comment was not rendered safely")
	}
	m, cmd = press(m, "m")
	if calls != 2 || cmd == nil {
		t.Fatal("next page did not load on demand")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(m.Comments) != 2 || m.CommentsCursor != "" {
		t.Fatalf("comments pagination: %+v", m.Comments)
	}
	m, _ = press(m, "j")
	next, _ = m.Update(CommentsMsg{Identity: "stable-1", Comments: []queue.GitHubComment{{Body: "late"}}})
	if strings.Contains(next.(Model).View(), "late") {
		t.Fatal("late comments were shown on another item")
	}
}

func TestSelectedEdgeHydrationFollowsSelectionOnly(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	calls := 0
	m.HydrateSelected = func(item queue.Item) tea.Cmd {
		calls++
		if item.Ref.ItemID != "2" {
			t.Fatalf("unexpected selected item: %s", item.Ref.ItemID)
		}
		return func() tea.Msg { return nil }
	}
	m, cmd := press(m, "j")
	if calls != 1 || cmd == nil {
		t.Fatalf("selection did not schedule hydration: %d", calls)
	}
	next, _ := m.Update(SnapshotMsg{Snapshot: fixture()})
	if calls != 1 || next.(Model).Selected != "stable-2" {
		t.Fatalf("snapshot repeated hydration: %d", calls)
	}
}

func TestClaimsLazyHistoryAndFullIdentity(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	calls := 0
	m.LoadHistory = func(i queue.Item, cursor string, before bool) tea.Cmd {
		calls++
		if i.Ref.ItemID != "1" || cursor != "" || before {
			t.Fatal(i.Ref, cursor)
		}
		return func() tea.Msg {
			return HistoryMsg{Identity: DetailRequestIdentity(i), Page: ledger.HistoryPage{Epochs: []ledger.Epoch{{AgentID: "past-agent", SessionID: "past-full-session", AcquiredAt: time.Now()}}}}
		}
	}
	m, _ = press(m, "enter")
	for range 3 {
		var cmd tea.Cmd
		m, cmd = press(m, "tab")
		if m.Tab == 3 {
			if cmd == nil {
				t.Fatal("history command absent")
			}
			msg := cmd()
			next, _ := m.Update(msg)
			m = next.(Model)
		}
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	view := m.View()
	for _, s := range []string{"whole-agent", "full-session-identity", "past-agent", "past-full-session", "Authority"} {
		if !strings.Contains(view, s) {
			t.Errorf("missing %q in %s", s, view)
		}
	}
}
func TestHeaderViewsStatesCoverageAndResize(t *testing.T) {
	snap := fixture()
	i := snap.Items[queue.Ref{SourceID: "a", ItemID: "1"}.Key()]
	i.Title = "仕事🙂 \x1b[31munsafe\x1b[0m"
	i.AssignedTo = []string{"@other"}
	i.Claim.Active = false
	i.DependenciesKnown = false
	i.Readiness = queue.Readiness{Status: queue.ReadinessUnknown}
	snap.Items[i.Ref.Key()] = i
	other := snap.Items[queue.Ref{SourceID: "a", ItemID: "2"}.Key()]
	other.Readiness = queue.Readiness{Status: queue.ReadinessUnknown}
	snap.Items[other.Ref.Key()] = other
	m := New(snap)
	m.Sources = []queue.Source{{ID: "a", Name: "Backlog"}}
	m.Me = "@me"
	m.Authority = "team abcdef"
	m.Scope = "remote"
	m.anchor(m.rows())
	wide := m.View()
	for _, s := range []string{"authority: team abcdef (remote)", "sources 1/1", "[All 2]", "Ready 0", "unknown dependencies", "assigned elsewhere", "Claim", "2 loaded of 2 (exact)", "edges 0/2", "provider fresh", "claims loading"} {
		if !strings.Contains(wide, s) {
			t.Errorf("wide view missing %q: %s", s, wide)
		}
	}
	if strings.Contains(wide, "\x1b") {
		t.Fatal("provider title escape survived")
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	m = next.(Model)
	m.Detail = true
	narrow := m.View()
	if strings.Contains(narrow, "second") {
		t.Fatal("narrow detail did not switch")
	}
	if m.Width != 80 || m.Height != 25 {
		t.Fatal("resize lost")
	}
}
func TestScriptedKeyboardAndDisabledActions(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	m, _ = press(m, "G")
	if m.Selected != "stable-2" {
		t.Fatal("G did not jump to bottom")
	}
	m, _ = press(m, "g")
	m, _ = press(m, "g")
	if m.Selected != "stable-1" {
		t.Fatal("gg did not jump to top")
	}
	m, _ = press(m, "l")
	if !m.Detail {
		t.Fatal("l did not open")
	}
	m, _ = press(m, "tab")
	if m.Tab != 1 {
		t.Fatal("tab did not advance")
	}
	m, _ = press(m, "h")
	if m.Detail {
		t.Fatal("h did not close")
	}
	refreshed, opened := 0, 0
	m.Refresh = func() tea.Cmd { refreshed++; return func() tea.Msg { return RefreshedMsg{} } }
	m.OpenURL = func(queue.Item) tea.Cmd { opened++; return func() tea.Msg { return RefreshedMsg{} } }
	m, _ = press(m, "r")
	m, _ = press(m, "o")
	if refreshed != 1 || opened != 1 {
		t.Fatal(refreshed, opened)
	}
	for _, key := range []string{"c", "R", "a", "s", "p", "x"} {
		m, _ = press(m, key)
		if key == "c" {
			if !strings.Contains(m.Notice, "Claim unavailable") {
				t.Fatal(key, m.Notice)
			}
		} else if !strings.Contains(m.Notice, "unavailable") && !strings.Contains(m.Notice, "Unavailable") {
			t.Fatal(key, m.Notice)
		}
	}
	m, _ = press(m, ":")
	m, _ = press(m, "s")
	m, _ = press(m, "enter")
	if !strings.Contains(m.Notice, "unavailable") {
		t.Fatal(m.Notice)
	}
	m, _ = press(m, "?")
	if !m.Help {
		t.Fatal("help missing")
	}
}
func TestCoverageFooterVisibleWithLongClaimHistory(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	m.Detail = true
	m.Tab = 3
	m.Height = 12
	for range 20 {
		m.History.Epochs = append(m.History.Epochs, ledger.Epoch{AgentID: "worker", SessionID: "session"})
	}
	view := m.View()
	if !strings.Contains(view, "2 loaded of 2 (exact)") {
		t.Fatalf("coverage footer hidden by detail: %s", view)
	}
	if lines := len(strings.Split(strings.TrimRight(view, "\n"), "\n")); lines > m.Height {
		t.Fatalf("render grew beyond viewport: %d", lines)
	}
}
func TestClaimHistoryPagination(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	m.Detail = true
	m.Tab = 3
	m.HistoryIdentity = DetailRequestIdentity(m.Snapshot.Items[(queue.Ref{SourceID: "a", ItemID: "1"}).Key()])
	m.History = ledger.HistoryPage{NextCursor: "newer-page", PreviousCursor: "older-page", Epochs: []ledger.Epoch{{AgentID: "first"}}}
	m.LoadHistory = func(i queue.Item, cursor string, before bool) tea.Cmd {
		if len(i.Resources) != 1 || cursor != "older-page" || !before {
			t.Fatal(i.Resources, cursor, before)
		}
		return func() tea.Msg {
			return HistoryMsg{Identity: DetailRequestIdentity(i), Before: true, Page: ledger.HistoryPage{PreviousCursor: "older-page-2", Epochs: []ledger.Epoch{{AgentID: "second"}}}}
		}
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("pagination command missing")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(m.History.Epochs) != 2 || m.History.Epochs[0].AgentID != "second" || m.History.NextCursor != "newer-page" || m.History.PreviousCursor != "older-page-2" {
		t.Fatal(m.History)
	}
}
func TestDetailScrollUsesScriptedKeys(t *testing.T) {
	m := New(fixture())
	m.Height = 10
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	m.Detail = true
	m.Tab = 3
	for j := 0; j < 4; j++ {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgDown})
		m = next.(Model)
	}
	if m.DetailOffset == 0 {
		t.Fatal("detail did not scroll")
	}
	view := m.View()
	if strings.Contains(view, "worklease queue") == false {
		t.Fatal("header lost on scroll")
	}
}
func TestVimScrollKeysMoveVisibleContent(t *testing.T) {
	t.Parallel()
	for _, detail := range []bool{false, true} {
		for _, tc := range []struct {
			key  string
			step func(int) int
		}{
			{"ctrl+f", func(n int) int { return max(1, n-1) }},
			{"pgdown", func(n int) int { return max(1, n-1) }},
			{"ctrl+b", func(n int) int { return -max(1, n-1) }},
			{"pgup", func(n int) int { return -max(1, n-1) }},
			{"ctrl+d", func(n int) int { return max(1, n/2) }},
			{"ctrl+u", func(n int) int { return -max(1, n/2) }},
			{"ctrl+e", func(int) int { return 1 }},
			{"ctrl+y", func(int) int { return -1 }},
		} {
			t.Run(fmt.Sprintf("detail=%v/%s", detail, tc.key), func(t *testing.T) {
				m := layoutModel(80, 20)
				m.Detail = detail
				visible := m.listCapacity(m.rows())
				if detail {
					item := m.Snapshot.Items[m.rows()[0].Ref.Key()]
					item.Body = strings.Repeat("A long detail line for scrolling.\n", 120)
					m.Snapshot.Items[item.Ref.Key()] = item
					m.rowCache = nil
					m.anchor(m.rows())
					m.DetailOffset = 50
					visible = max(1, m.frame(m.rows()).bodyHeight-2)
					if m.maxDetailOffset() < 75 {
						t.Fatal("detail fixture is not long enough to scroll")
					}
				} else {
					m.selectIndex(m.rows(), 55)
					m.Offset = 50
				}
				before := m.Offset
				if detail {
					before = m.DetailOffset
				}
				selected := m.Selected
				m, _ = press(m, tc.key)
				got := m.Offset
				if detail {
					got = m.DetailOffset
				} else if m.Index < m.Offset || m.Index >= m.Offset+visible || m.Selected == "" {
					t.Fatalf("list selection left viewport: index=%d offset=%d", m.Index, m.Offset)
				}
				if want := before + tc.step(visible); got != want {
					t.Fatalf("%s: offset=%d want %d", tc.key, got, want)
				}
				if detail && m.Selected != selected {
					t.Fatal("scroll changed detail item")
				}
			})
		}
	}
}

func TestVimVisibleRowAndAlignmentKeys(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		key      string
		position func(int) int
	}{
		{"H", func(int) int { return 0 }},
		{"M", func(n int) int { return n / 2 }},
		{"L", func(n int) int { return n - 1 }},
	} {
		t.Run(tc.key, func(t *testing.T) {
			m := layoutModel(80, 20)
			m.selectIndex(m.rows(), 55)
			m.Offset = 50
			visible := m.listCapacity(m.rows())
			m, _ = press(m, tc.key)
			if want := 50 + tc.position(visible); m.Index != want {
				t.Fatalf("selected %d, want %d", m.Index, want)
			}
		})
	}
	for _, tc := range []struct {
		key      string
		position func(int) int
	}{
		{"zz", func(n int) int { return n / 2 }},
		{"zt", func(int) int { return 0 }},
		{"zb", func(n int) int { return n - 1 }},
	} {
		t.Run(tc.key, func(t *testing.T) {
			m := layoutModel(80, 20)
			m.selectIndex(m.rows(), 75)
			visible := m.listCapacity(m.rows())
			selected := m.Selected
			for _, key := range tc.key {
				m, _ = press(m, string(key))
			}
			if m.Offset != 75-tc.position(visible) || m.Selected != selected {
				t.Fatalf("alignment offset=%d selection=%s", m.Offset, m.Selected)
			}
		})
	}
	m := layoutModel(80, 20)
	m, _ = press(m, "z")
	m, _ = press(m, "j")
	m, _ = press(m, "z")
	if !m.pendingZ {
		t.Fatal("unrelated key completed stale z prefix")
	}
	m.Filtering, m.Input = true, "input"
	m, _ = press(m, "ctrl+u")
	if m.Input != "" || m.Offset != 0 {
		t.Fatal("input clear scrolled the list")
	}
	m = layoutModel(160, 20)
	m.Detail = true // the list is still visible in the split layout
	m.selectIndex(m.rows(), 55)
	m.Offset = 50
	m, _ = press(m, "H")
	if m.Index != 50 {
		t.Fatal("H did not select the visible list's top row in split view")
	}
	m = layoutModel(80, 20)
	m.Detail = true // the narrow layout has no visible list
	selected := m.Selected
	m, _ = press(m, "L")
	if m.Selected != selected {
		t.Fatal("L changed a selection in a hidden list")
	}
}

func TestVimScrollHelpAndBoundaries(t *testing.T) {
	t.Parallel()
	m := layoutModel(80, 20)
	m, _ = press(m, "?")
	if !strings.Contains(m.View(), "Navigate") {
		t.Fatal("help did not open at the top")
	}
	m, _ = press(m, "ctrl+f")
	if m.HelpOffset == 0 || strings.Contains(m.View(), "Navigate") {
		t.Fatal("help did not page to the hidden shortcuts")
	}
	m, _ = press(m, "ctrl+b")
	if m.HelpOffset != 0 {
		t.Fatal("help did not page back")
	}
	m, _ = press(m, "ctrl+y")
	if m.HelpOffset != 0 {
		t.Fatal("help scrolled past the top")
	}
	for range 10 {
		m, _ = press(m, "pgdown")
	}
	if want := max(0, len(m.helpLines())-m.frame(m.rows()).bodyHeight); m.HelpOffset != want {
		t.Fatalf("help scrolled past bottom: %d want %d", m.HelpOffset, want)
	}
	m, _ = press(m, "esc")
	m, _ = press(m, "?")
	if m.HelpOffset != 0 {
		t.Fatal("help did not reopen at the top")
	}
}

func TestRenderStripsTerminalControlsAndNarrowSwitch(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	m.Detail = true
	m.Width = 80
	view := m.View()
	if strings.Contains(view, "\x1b") || strings.Contains(view, "evil.invalid") {
		t.Fatal("terminal injection", view)
	}
	if !strings.Contains(view, "CLICK red") {
		t.Fatal("sanitized body missing", view)
	}
	if strings.Contains(view, "second") {
		t.Fatal("narrow layout squeezed list and detail")
	}
}
func TestDependencyEvidenceAndCoverageRendering(t *testing.T) {
	snap := fixture()
	i := snap.Items[queue.Ref{SourceID: "a", ItemID: "1"}.Key()]
	i.Closure = queue.CoveragePartial
	i.Readiness = queue.Readiness{Status: queue.ReadinessUnknown, Reasons: []string{"missing prerequisite"}, Freshness: queue.Stale}
	i.Relationships = []queue.Relationship{{Type: queue.HardPrerequisite, To: queue.Ref{SourceID: "a", ItemID: "0"}, Condition: "terminal", RawOutcome: "Open", Interpretation: "not terminal", Provenance: "Backlog", Fresh: false}}
	snap.Items[i.Ref.Key()] = i
	m := New(snap)
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	m.Detail = true
	m.Tab = 1
	m.Width = 80
	view := m.View()
	for _, s := range []string{"missing prerequisite", "partial", "hard-prerequisite", "terminal", "Open", "Backlog", "fresh: false"} {
		if !strings.Contains(view, s) {
			t.Errorf("missing %q: %s", s, view)
		}
	}
}
func TestHistoryClearsWhenSelectionChangesAndDoesNotRenderForOtherIdentity(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	m.Detail, m.Tab = true, 3
	m.HistoryIdentity = DetailRequestIdentity(m.Snapshot.Items[(queue.Ref{SourceID: "a", ItemID: "1"}).Key()])
	m.History = ledger.HistoryPage{Epochs: []ledger.Epoch{{AgentID: "first-agent"}}}
	m, _ = press(m, "j")
	if len(m.History.Epochs) != 0 || m.HistoryIdentity != "" {
		t.Fatal("selection change retained previous item's history")
	}
	m.History = ledger.HistoryPage{Epochs: []ledger.Epoch{{AgentID: "wrong-agent"}}}
	if strings.Contains(m.View(), "wrong-agent") {
		t.Fatal("history rendered for a different item")
	}
}
func TestConfiguredClaimAndSourceSpecificMeFilters(t *testing.T) {
	snap := fixture()
	items := []queue.Item{}
	for _, item := range snap.Items {
		item.Ref.SourceID = "github"
		item.AssignedTo = []string{"brett"}
		item.Claim = queue.ClaimObservation{Known: true, Active: true}
		items = append(items, item)
	}
	snap.Items = map[string]queue.Item{}
	for _, item := range items {
		snap.Items[item.Ref.Key()] = item
	}
	m := New(snap)
	m.ViewName = "Mine"
	m.Sources = []queue.Source{{ID: "github"}}
	m.Me = "@brett"
	m.MeBySource = map[string][]string{"backlog": {"@brett"}, "github": {"brett"}}
	m.ViewRules = map[string]ViewRule{"Mine": {Assigned: []string{"me"}, Claim: "held"}}
	if rows := m.rows(); len(rows) != 2 {
		t.Fatalf("source-specific assigned: [me] / held view = %d rows", len(rows))
	}
	for key, item := range m.Snapshot.Items {
		item.Claim.Active = false
		item.Claim.State = "expired"
		m.Snapshot.Items[key] = item
	}
	m.ViewRules["Mine"] = ViewRule{Assigned: []string{"me"}, Claim: "free"}
	if rows := m.rows(); len(rows) != 2 {
		t.Fatalf("expired known claims should satisfy free: %d rows", len(rows))
	}
}
func TestClaimsPaginationDoesNotTakeNavigationKeyAndSuppressesDuplicate(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	m.Detail, m.Tab = true, 3
	m.HistoryIdentity = DetailRequestIdentity(m.Snapshot.Items[(queue.Ref{SourceID: "a", ItemID: "1"}).Key()])
	m.History = ledger.HistoryPage{PreviousCursor: "page-2"}
	m.LoadHistory = func(queue.Item, string, bool) tea.Cmd { return func() tea.Msg { return nil } }
	m, _ = press(m, "n")
	if m.Selected != "stable-2" {
		t.Fatalf("n did not navigate to next item: %s", m.Selected)
	}
	m.Selected = "stable-1"
	m.HistoryIdentity = DetailRequestIdentity(m.Snapshot.Items[(queue.Ref{SourceID: "a", ItemID: "1"}).Key()])
	m.History.PreviousCursor = "page-2"
	m, cmd := press(m, "m")
	if cmd == nil || !m.HistoryLoading {
		t.Fatal("m did not begin history page load")
	}
	_, duplicate := press(m, "m")
	if duplicate != nil {
		t.Fatal("duplicate history page request while loading")
	}
}
func TestFilterAfterScrolledListDoesNotPanic(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.Height = 8
	m.anchor(m.rows())
	m, _ = press(m, "G")
	m.Filter = "first"
	next, _ := m.Update(SnapshotMsg{Snapshot: m.Snapshot})
	m = next.(Model)
	if !strings.Contains(m.View(), "first") {
		t.Fatal("selected row disappeared")
	}
}
func TestClaimFailuresExposeContentionUncertaintyAndDefinitiveRejection(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{name: "contention", err: reason.New(reason.ReasonAlreadyClaimed, "busy").With("holder", map[string]any{"agentId": "other", "expiresAt": "2026-09-24T06:00:00Z"}), want: []string{"other", "2026-09-24T06:00:00Z", "not retried"}},
		{name: "uncertain", err: reason.New(reason.ReasonUnknownOutcome, "remote request failed").With("commitState", "unknown").With("pendingPath", "/private/queue-handle.json"), want: []string{"uncertain", "/private/queue-handle.json", "recover"}},
		{name: "rejected", err: reason.New(reason.ReasonInvalidArgument, "remote request failed").With("commitState", "not-committed"), want: []string{"rejected", "no claim was acquired"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := claimFailureNotice(test.err)
			for _, want := range test.want {
				if !strings.Contains(got, want) {
					t.Errorf("notice %q does not contain %q", got, want)
				}
			}
		})
	}
}

func TestClaimContentionViewShowsHolderAndExpiry(t *testing.T) {
	t.Parallel()
	busy := reason.New(reason.ReasonAlreadyClaimed, "busy").With("holder", map[string]any{"agentId": "other-agent", "expiresAt": "2026-09-24T06:00:00Z"})
	base := New(fixture())
	base.Sources = []queue.Source{{ID: "a"}}
	base.anchor(base.rows())
	item := base.rows()[0]
	for _, msg := range []tea.Msg{ClaimPreviewMsg{Identity: identity(item), Err: busy}, ClaimResultMsg{Identity: identity(item), Item: item, Err: busy}} {
		next, _ := base.Update(msg)
		view := next.(Model).View()
		for _, want := range []string{"other-agent", "2026-09-24T06:00:00Z", "not retried"} {
			if !strings.Contains(view, want) {
				t.Errorf("%T view missing %q:\n%s", msg, want, view)
			}
		}
	}
}

func TestClaimPreviewResortsPreparedRowsWhenOrderChanges(t *testing.T) {
	t.Parallel()
	snapshot := fixture()
	aKey := queue.Ref{SourceID: "a", ItemID: "1"}.Key()
	bKey := queue.Ref{SourceID: "a", ItemID: "2"}.Key()
	a := snapshot.Items[aKey]
	a.Order = "1"
	snapshot.Items[aKey] = a
	b := snapshot.Items[bKey]
	b.Order = "2"
	snapshot.Items[bKey] = b

	m := New(snapshot)
	m.Sources = []queue.Source{{ID: "a"}}
	next, _ := m.Update(PrepareSnapshot(snapshot, m.Sources...))
	m = next.(Model)
	m.anchor(m.rows())
	a.Order = "3"
	preview := ClaimPreview{Identity: identity(a), Title: a.Title}
	next, _ = m.Update(ClaimPreviewMsg{Identity: identity(a), Item: a, Preview: &preview})
	m = next.(Model)

	want := queue.EvaluateView(m.Snapshot.Items, queue.View{SourceOrder: []string{"a"}})
	got := m.rows()
	if len(got) != len(want) {
		t.Fatalf("prepared rows count = %d, want %d", len(got), len(want))
	}
	for index := range want {
		if got[index].Ref != want[index].Ref {
			t.Fatalf("row %d = %s, want queue.EvaluateView row %s", index, got[index].Ref, want[index].Ref)
		}
	}
}

func TestClaimPreviewRequiresExplicitConfirmationAndShowsGrant(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	preview := ClaimPreview{Identity: m.Selected, Title: "first", AuthorityProfile: "team", AuthorityID: "authority-id", Scope: "remote", SessionID: "0123456789abcdef0123456789abcdef", Resources: []string{"coordination:test"}, TTL: 5 * time.Minute, Hold: time.Hour, CoordinationLimits: "server maximum unknown"}
	acquireCalls := 0
	m.PreviewClaim = func(item queue.Item) tea.Cmd {
		return func() tea.Msg { return ClaimPreviewMsg{Identity: identity(item), Item: item, Preview: &preview} }
	}
	m.AcquireClaim = func(item queue.Item, got ClaimPreview) tea.Cmd {
		acquireCalls++
		if identity(item) != preview.Identity || got.SessionID != preview.SessionID {
			t.Fatalf("confirmation changed its preview: item=%s preview=%+v", identity(item), got)
		}
		claim := queue.ClaimObservation{Known: true, Active: true, State: "held", AgentID: "brett", SessionID: preview.SessionID, AcquiredAt: time.Now(), ExpiresAt: time.Now().Add(4 * time.Minute)}
		return func() tea.Msg {
			return ClaimResultMsg{Identity: preview.Identity, Item: item, Claim: claim, GrantedTTL: 4 * time.Minute}
		}
	}
	var cmd tea.Cmd
	m, cmd = press(m, "c")
	if cmd == nil || acquireCalls != 0 {
		t.Fatal("c sent an acquisition before opening a preview")
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	for _, text := range []string{"team", "authority-id", "remote", "coordination:test", preview.SessionID, "5m0s", "1h0m0s", "Provider   unchanged", "server maximum unknown"} {
		if !strings.Contains(m.View(), text) {
			t.Errorf("preview missing %q: %s", text, m.View())
		}
	}
	m, cmd = press(m, "enter")
	if cmd == nil || acquireCalls != 1 || m.ClaimPreview != nil || !m.Claiming {
		t.Fatal("confirmation did not send exactly one acquisition")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if got := m.Snapshot.Items[queue.Ref{SourceID: "a", ItemID: "1"}.Key()].Claim; !got.Active || got.SessionID != preview.SessionID {
		t.Fatalf("grant observation not shown: %+v", got)
	}
	m, _ = press(m, "enter")
	for range 3 {
		m, _ = press(m, "tab")
	}
	view := m.View()
	for _, text := range []string{"Granted TTL 4m0s", "Expires "} {
		if !strings.Contains(view, text) {
			t.Errorf("grant detail missing %q: %s", text, view)
		}
	}
}

func TestUncertainAcquireCannotQuitSilently(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	pending := reason.New(reason.ReasonUnknownOutcome, "lost response").With("commitState", "unknown").With("pendingPath", "/private/queue/pending.json")
	next, _ := m.Update(ClaimResultMsg{Identity: m.Selected, Err: pending})
	m = next.(Model)
	m, cmd := press(m, "q")
	if cmd != nil || !m.Quitting || !strings.Contains(m.View(), "/private/queue/pending.json") {
		t.Fatalf("uncertain claim lost on exit: %s", m.View())
	}
}

func TestQueueOwnedClaimExitAndCancel(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	item := m.rows()[0]
	item.Resources = []string{"resource:one"}
	m.Snapshot.Items[item.Ref.Key()] = item
	path := "/private/queue/claim.json"
	next, _ := m.Update(ClaimResultMsg{Identity: identity(item), Item: item, HandlePath: path, ClaimID: "claim", Resources: item.Resources, NextRenewal: time.Now().Add(time.Minute), Claim: queue.ClaimObservation{Known: true, Active: true, ExpiresAt: time.Now().Add(5 * time.Minute)}})
	m = next.(Model)
	if len(m.OwnedClaims) != 1 {
		t.Fatal("new grant not tracked for exit")
	}
	m, cmd := press(m, "q")
	if cmd != nil || !m.Quitting || !strings.Contains(m.View(), "No claim is released automatically") || !strings.Contains(m.View(), path) {
		t.Fatalf("exit consequence missing: %s", m.View())
	}
	m, _ = press(m, "esc")
	if m.Quitting {
		t.Fatal("exit cannot be cancelled")
	}
	failed, _ := m.Update(OwnedClaimMsg{Path: path, LastResult: "handle directory unavailable"})
	m = failed.(Model)
	if m.OwnedClaims[path].Verified || m.OwnedClaims[path].ClaimID != "claim" {
		t.Fatal("scan failure erased or authorized the claim")
	}
	m, cmd = press(m, "q")
	if cmd != nil || !m.Quitting || !strings.Contains(m.View(), "handle directory unavailable") {
		t.Fatal("unverified claim did not warn on exit")
	}
	m, cmd = press(m, "enter")
	if cmd == nil {
		t.Fatal("confirmed exit did not quit")
	}
	m.Quitting = false
	owned := m.OwnedClaims[path]
	owned.Verified = true
	m.OwnedClaims[path] = owned
	called := false
	m.CancelClaim = func(got string) tea.Cmd {
		called = true
		if got != path {
			t.Fatalf("wrong handle %s", got)
		}
		return func() tea.Msg { return CancelClaimMsg{Path: path} }
	}
	m, cmd = press(m, "R")
	if cmd != nil || called || !strings.Contains(m.View(), "Only an authority-verified no-effect") {
		t.Fatal("cancellation preview missing")
	}
	m, cmd = press(m, "enter")
	if cmd == nil || !called {
		t.Fatal("verified no-effect cancellation not offered")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(m.OwnedClaims) != 0 {
		t.Fatal("cancelled claim still tracked")
	}
}

func TestLaunchPickerPreviewsAndDoesNotInventWorkerClaim(t *testing.T) {
	m := New(fixture())
	m.anchor(m.rows())
	m.PreviewLaunch = func(item queue.Item) []queue.LaunchOption {
		return []queue.LaunchOption{
			{Name: "blocked", Authority: "authority", Eligibility: queue.Eligibility{Reasons: []string{"queue-holds-claim", "release or cancel, then launch; another worker may acquire in between"}}, Argv: []string{"worker", "--", "1"}, Cwd: "/checkout", EnvNames: []string{"PATH", "WORKLEASE_PROFILE"}},
			{Name: "ready", Authority: "authority", Eligibility: queue.Eligibility{Eligible: true}, Argv: []string{"worker", "--", "1"}, Cwd: "/checkout", EnvNames: []string{"PATH"}},
		}
	}
	calls := 0
	m.Launch = func(item queue.Item, name string) tea.Cmd {
		calls++
		return func() tea.Msg { return LaunchResultMsg{Name: name} }
	}
	m, cmd := press(m, "x")
	if cmd != nil || !strings.Contains(m.View(), "queue-holds-claim") || !strings.Contains(m.View(), "another worker may acquire") || !strings.Contains(m.View(), "/checkout") || !strings.Contains(m.View(), "WORKLEASE_PROFILE") {
		t.Fatalf("launch picker: %s", m.View())
	}
	m, cmd = press(m, "enter")
	if cmd != nil || calls != 0 {
		t.Fatal("blocked action launched")
	}
	m, _ = press(m, "j")
	m, cmd = press(m, "enter")
	if cmd == nil || calls != 1 || !m.Launching {
		t.Fatal("available action not launched")
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if m.Launching || !strings.Contains(m.Notice, "awaiting worker claim") || m.Snapshot.Items[queue.Ref{SourceID: "a", ItemID: "1"}.Key()].Claim.SessionID != "full-session-identity" {
		t.Fatalf("process start was mistaken for a worker claim: %s", m.Notice)
	}
	failed, _ := m.Update(LaunchResultMsg{Name: "missing", Err: fmt.Errorf("executable missing")})
	m = failed.(Model)
	if !strings.Contains(m.Notice, "Launch failed: executable missing") || m.Snapshot.Items[queue.Ref{SourceID: "a", ItemID: "1"}.Key()].Claim.SessionID != "full-session-identity" {
		t.Fatalf("failed process start changed claim: %s", m.Notice)
	}
}

func TestSuccessfulLaunchDoesNotClaimUntilWorkerAppearsInOverlay(t *testing.T) {
	snapshot := fixture()
	key := queue.Ref{SourceID: "a", ItemID: "1"}.Key()
	item := snapshot.Items[key]
	item.Claim = queue.ClaimObservation{AuthorityID: "authority", Known: true, State: "free"}
	snapshot.Items = map[string]queue.Item{key: item}
	m := New(snapshot)
	m.anchor(m.rows())
	m.PreviewLaunch = func(queue.Item) []queue.LaunchOption {
		return []queue.LaunchOption{{Name: "exits-without-claim", Eligibility: queue.Eligibility{Eligible: true}}}
	}
	m.Launch = func(queue.Item, string) tea.Cmd {
		return func() tea.Msg { return LaunchResultMsg{Name: "exits-without-claim"} }
	}
	m, _ = press(m, "x")
	m, launch := press(m, "enter")
	if launch == nil {
		t.Fatal("launch did not start")
	}
	next, _ := m.Update(launch())
	m = next.(Model)
	if m.Snapshot.Items[key].Claim.Active || !strings.Contains(m.Notice, "awaiting worker claim") || len(m.OwnedClaims) != 0 {
		t.Fatalf("process start invented a claim: %+v %s", m.Snapshot.Items[key].Claim, m.Notice)
	}
	observed := snapshot.Clone()
	worker := observed.Items[key]
	worker.Claim = queue.ClaimObservation{AuthorityID: "authority", Known: true, Active: true, State: "held", SessionID: "independent-worker-session", ObservedAt: time.Now()}
	observed.Items[key] = worker
	next, _ = m.Update(ClaimOverlayMsg{Snapshot: observed})
	m = next.(Model)
	if !m.Snapshot.Items[key].Claim.Active || m.Snapshot.Items[key].Claim.SessionID != "independent-worker-session" || len(m.OwnedClaims) != 0 {
		t.Fatalf("worker overlay was not observed independently: %+v", m.Snapshot.Items[key].Claim)
	}
}

func TestLaunchConfirmationRejectsDifferentRefWithSameCanonicalIdentity(t *testing.T) {
	original := fixture().Items[queue.Ref{SourceID: "a", ItemID: "1"}.Key()]
	m := New(queue.Snapshot{Items: map[string]queue.Item{original.Ref.Key(): original}, Sources: map[string]queue.Coverage{"a": {State: queue.CoverageComplete}}})
	m.anchor(m.rows())
	m.PreviewLaunch = func(queue.Item) []queue.LaunchOption {
		return []queue.LaunchOption{{Name: "worker", Eligibility: queue.Eligibility{Eligible: true}}}
	}
	called := false
	m.Launch = func(queue.Item, string) tea.Cmd { called = true; return nil }
	m, _ = press(m, "x")
	replacement := original
	replacement.Ref = queue.Ref{SourceID: "b", ItemID: "2"}
	next, _ := m.Update(SnapshotMsg{Snapshot: queue.Snapshot{Items: map[string]queue.Item{replacement.Ref.Key(): replacement}, Sources: map[string]queue.Coverage{"b": {State: queue.CoverageComplete}}}})
	m = next.(Model)
	m, cmd := press(m, "enter")
	if cmd != nil || called || !strings.Contains(m.Notice, "selected item changed") {
		t.Fatalf("preview accepted a different ref: %s", m.Notice)
	}
}

func TestDisabledActionsAndFilter(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	m, _ = press(m, "c")
	if !strings.Contains(m.Notice, "Claim unavailable") {
		t.Fatal(m.Notice)
	}
	m, _ = press(m, "/")
	m, _ = press(m, "s")
	m, _ = press(m, "e")
	m, _ = press(m, "c")
	m, _ = press(m, "o")
	m, _ = press(m, "n")
	m, _ = press(m, "d")
	m, _ = press(m, "enter")
	if len(m.rows()) != 1 || m.Selected != "stable-2" {
		t.Fatal(m.Filter, m.Selected)
	}
}

func TestStartWorkPreviewAndSeparateOutcomes(t *testing.T) {
	t.Parallel()
	for _, outcome := range []struct {
		name, step string
		write      *WriteResultMsg
		want       string
	}{
		{"verified", "applied", &WriteResultMsg{Result: queue.WriteResult{Outcome: queue.WriteVerified, ClaimHeld: true}}, "transition applied"},
		{"rejected", "rejected", &WriteResultMsg{Result: queue.WriteResult{Outcome: queue.WriteConflict, SourceUnchanged: true, ClaimHeld: true}}, "Claim acquired; status unchanged"},
		{"unknown", "unknown", &WriteResultMsg{Result: queue.WriteResult{Outcome: queue.WriteUnknown, ClaimHeld: true}}, "recovery required"},
	} {
		t.Run(outcome.name, func(t *testing.T) {
			m := New(fixture())
			m.anchor(m.rows())
			m.StartTransitions = map[string]string{"a": "Doing"}
			m.PreviewStart = func(item queue.Item) tea.Cmd {
				return func() tea.Msg {
					return StartPreviewMsg{Identity: identity(item), Item: item, Preview: &StartPreview{Claim: ClaimPreview{Identity: identity(item), Title: item.Title, AuthorityID: "authority", Resources: []string{"resource:1"}}, Source: "a", Actor: "@worker", Transition: "Doing", RequiredFields: "status=Doing", SideEffects: []string{"Git commit", "Git hooks"}}}
				}
			}
			called := 0
			m.StartWork = func(item queue.Item, preview StartPreview) tea.Cmd {
				called++
				return func() tea.Msg {
					return StartResultMsg{Claim: ClaimResultMsg{Identity: identity(item), Item: item, HandlePath: "/private/handle", ClaimID: "claim", Resources: []string{"resource:1"}, Claim: queue.ClaimObservation{Known: true, Active: true, ExpiresAt: time.Now().Add(time.Minute)}}, ClaimStep: "applied", TransitionStep: outcome.step, Write: outcome.write}
				}
			}
			m, _ = press(m, ":")
			for _, ch := range "start work" {
				m, _ = press(m, string(ch))
			}
			m, cmd := press(m, "enter")
			if cmd == nil || called != 0 {
				t.Fatalf("palette skipped preview: %s", m.Notice)
			}
			next, _ := m.Update(cmd())
			m = next.(Model)
			if view := m.View(); !strings.Contains(view, "Provider actor @worker") || !strings.Contains(view, "status=Doing") || !strings.Contains(view, "resource:1") || !strings.Contains(view, "Git commit; Git hooks") || !strings.Contains(view, "no assignment") || strings.Contains(view, "Provider unchanged") || strings.Contains(view, "Provider   unchanged") {
				t.Fatalf("incomplete Start work preview: %s", view)
			}
			m, cmd = press(m, "enter")
			if cmd == nil || called != 1 {
				t.Fatalf("confirmation did not start: %s", m.Notice)
			}
			next, _ = m.Update(cmd())
			m = next.(Model)
			if !strings.Contains(m.Notice, outcome.want) || len(m.OwnedClaims) != 1 || m.UncertainWrite != (outcome.step == "unknown") {
				t.Fatalf("lost separate outcomes or claim: %s %+v", m.Notice, m.OwnedClaims)
			}
		})
	}
}

func TestStartWorkMissingMappingLeavesClaimOnly(t *testing.T) {
	t.Parallel()
	m := New(fixture())
	m.anchor(m.rows())
	m.StartTransitions = map[string]string{"a": ""}
	m, _ = press(m, ":")
	for _, ch := range "start work" {
		m, _ = press(m, string(ch))
	}
	m, cmd := press(m, "enter")
	if cmd != nil || !strings.Contains(m.Notice, "Claim only") || m.StartPreview != nil {
		t.Fatalf("unsupported mapping offered Start work: %s", m.Notice)
	}
}

func TestQueueQuitRefusesPendingCancellationInRecoveryView(t *testing.T) {
	m := New(fixture())
	m.OwnedClaims["/private/queue/claim.json"] = OwnedClaimMsg{Path: "/private/queue/claim.json", ClaimID: "claim", Verified: true}
	m.Cancelling = true
	m.ViewName = RecoveryViewID
	m, _ = press(m, "q")
	m, cmd := press(m, "enter")
	if cmd != nil || m.Quitting {
		t.Fatalf("recovery view quit while cancellation pending: %s", m.View())
	}
}

func TestQueueQuitWarnsOnUnreadableRecoveryJournal(t *testing.T) {
	m := New(fixture())
	m.RecoveryError = "journal corrupt"
	m, cmd := press(m, "q")
	if cmd != nil || !m.Quitting || !strings.Contains(m.View(), "recovery journal unreadable: journal corrupt") {
		t.Fatalf("unreadable journal did not warn on exit: %s", m.View())
	}
}

func TestFilterInputAcceptsPasteAndUnicode(t *testing.T) {
	t.Parallel()
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m, _ = press(m, "/")
	m, _ = press(m, "sec")
	m, _ = press(m, "ö")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(Model)
	m, _ = press(m, "ond")
	m, _ = press(m, "enter")
	if m.Filter != "second" || len(m.rows()) != 1 {
		t.Fatalf("filter = %q, rows = %d", m.Filter, len(m.rows()))
	}
}
