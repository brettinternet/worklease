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
	next, _ := m.Update(SnapshotMsg{updated})
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
func TestClaimsLazyHistoryAndFullIdentity(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	calls := 0
	m.LoadHistory = func(i queue.Item, cursor string) tea.Cmd {
		calls++
		if i.Ref.ItemID != "1" || cursor != "" {
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
	for _, s := range []string{"authority: team abcdef (remote)", "sources 1/1", "Views:", "Sources:", "unknown dependencies", "assigned elsewhere", "Worklease", "2 loaded of 2 (exact)", "edges 0/2", "search: loaded rows"} {
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
	m.History = ledger.HistoryPage{NextCursor: "page-2", Epochs: []ledger.Epoch{{AgentID: "first"}}}
	m.LoadHistory = func(i queue.Item, cursor string) tea.Cmd {
		if len(i.Resources) != 1 || cursor != "page-2" {
			t.Fatal(i.Resources, cursor)
		}
		return func() tea.Msg {
			return HistoryMsg{Identity: "stable-1", Page: ledger.HistoryPage{Epochs: []ledger.Epoch{{AgentID: "second"}}}}
		}
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("pagination command missing")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(m.History.Epochs) != 2 || m.History.Epochs[1].AgentID != "second" {
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
func TestFilterAfterScrolledListDoesNotPanic(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.Height = 8
	m.anchor(m.rows())
	m, _ = press(m, "G")
	m.Filter = "first"
	next, _ := m.Update(SnapshotMsg{m.Snapshot})
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
