package queueui

import (
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/queue"
	tea "github.com/charmbracelet/bubbletea"
)

func fixture() queue.Snapshot {
	a := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "a", ItemID: "1"}, CanonicalID: "stable-1", Title: "first", RawStatus: "open", Fresh: true}, Body: "\x1b]8;;https://evil.invalid\aCLICK\x1b]8;;\a \x1b[31mred\x1b[0m", Resources: []string{"resource:1"}, Claim: queue.ClaimObservation{Known: true, Active: true, AgentID: "whole-agent", SessionID: "full-session-identity"}}
	b := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "a", ItemID: "2"}, CanonicalID: "stable-2", Title: "second", RawStatus: "open", Fresh: true}}
	return queue.Snapshot{Items: map[string]queue.Item{a.Ref.Key(): a, b.Ref.Key(): b}, Sources: map[string]queue.Coverage{"a": {State: queue.CoverageComplete, Total: 2, TotalAccuracy: queue.TotalExact}}}
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
	default:
		k.Type = tea.KeyRunes
		k.Runes = []rune(key)
	}
	next, cmd := m.Update(k)
	return next.(Model), cmd
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
			return CommentsMsg{Identity: "stable-1", Comments: []queue.GitHubComment{{Author: "alice", Body: "hello\x1b[31m world"}}, Cursor: map[bool]string{true: "next", false: ""}[calls == 1]}
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
			return HistoryMsg{Identity: "stable-1", Page: ledger.HistoryPage{Epochs: []ledger.Epoch{{AgentID: "past-agent", SessionID: "past-full-session", AcquiredAt: time.Now()}}}}
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
	for _, s := range []string{"authority: team abcdef (remote)", "sources 1/1", "Views:", "Sources:", "unknown dependencies", "assigned elsewhere", "Worklease", "2 loaded of 2 (exact)", "edges 0/2", "provider fresh", "claims loading"} {
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
	if strings.Contains(narrow, "ID     Title") {
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
		if !strings.Contains(m.Notice, "read-only") {
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
	m.HistoryIdentity = m.Selected
	m.History = ledger.HistoryPage{NextCursor: "newer-page", PreviousCursor: "older-page", Epochs: []ledger.Epoch{{AgentID: "first"}}}
	m.LoadHistory = func(i queue.Item, cursor string, before bool) tea.Cmd {
		if len(i.Resources) != 1 || cursor != "older-page" || !before {
			t.Fatal(i.Resources, cursor, before)
		}
		return func() tea.Msg {
			return HistoryMsg{Identity: "stable-1", Before: true, Page: ledger.HistoryPage{PreviousCursor: "older-page-2", Epochs: []ledger.Epoch{{AgentID: "second"}}}}
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
	if strings.Contains(view, "ID       Title") {
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
	m.HistoryIdentity = m.Selected
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
	m.HistoryIdentity = m.Selected
	m.History = ledger.HistoryPage{PreviousCursor: "page-2"}
	m.LoadHistory = func(queue.Item, string, bool) tea.Cmd { return func() tea.Msg { return nil } }
	m, _ = press(m, "n")
	if m.Selected != "stable-2" {
		t.Fatalf("n did not navigate to next item: %s", m.Selected)
	}
	m.Selected = "stable-1"
	m.HistoryIdentity = m.Selected
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
func TestDisabledActionsAndFilter(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	m, _ = press(m, "c")
	if !strings.Contains(m.Notice, "read-only") {
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
