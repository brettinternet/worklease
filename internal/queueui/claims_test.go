package queueui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/queue"
	tea "github.com/charmbracelet/bubbletea"
)

func TestClaimsTabSwitchingPreservesPerViewSelection(t *testing.T) {
	t.Parallel()
	m := New(fixture())
	m.Views = []string{"All", ClaimsViewID}
	m.ViewName = "All"
	m.Width, m.Height = 120, 30
	m.Selected = "stable-1"
	m.Index = 0
	m, _ = press(m, "j")
	if m.Selected != "stable-2" {
		t.Fatalf("queue selection=%q, want stable-2", m.Selected)
	}
	m, _ = press(m, "v")
	if m.ViewName != ClaimsViewID {
		t.Fatalf("v selected %q, want Claims", viewLabel(m.ViewName))
	}
	m.Claims.Items = []lease.ClaimView{{ClaimID: "claim-1", Resources: []string{"resource:1"}}}
	m.Claims.Selected = "claim-1"
	m.Claims.Index = 0
	m, _ = press(m, "1")
	if m.ViewName != "All" || m.Selected != "stable-2" {
		t.Fatalf("digit switch lost queue selection: view=%q selection=%q", m.ViewName, m.Selected)
	}
	_, spans := m.viewTabs()
	updated, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: spans[1].start, Y: 1})
	m = updated.(Model)
	if m.ViewName != ClaimsViewID || m.Claims.Selected != "claim-1" {
		t.Fatalf("tab click did not restore Claims selection: view=%q selection=%q", m.ViewName, m.Claims.Selected)
	}
}

type claimsTestReader struct {
	claims []lease.ClaimView
	pages  map[string]ledger.EventsPage
	calls  []string
}

func (r *claimsTestReader) List(context.Context, string, *lease.RemoteActor) ([]lease.ClaimView, error) {
	return append([]lease.ClaimView(nil), r.claims...), nil
}

func (r *claimsTestReader) Events(_ context.Context, cursor string, _ int) (ledger.EventsPage, error) {
	r.calls = append(r.calls, cursor)
	return r.pages[cursor], nil
}

func (*claimsTestReader) History(context.Context, string, string, int, bool) (ledger.HistoryPage, error) {
	return ledger.HistoryPage{}, nil
}

func TestClaimsLiveRefreshResetsGapAndRetainsPublicEvents(t *testing.T) {
	t.Parallel()
	reader := &claimsTestReader{
		claims: []lease.ClaimView{{ClaimID: "claim-1", Resources: []string{"file:one"}, AgentID: "alice", Active: true}},
		pages: map[string]ledger.EventsPage{
			"old-cursor": {Gap: true, NextCursor: "reset"},
			"":           {Events: []ledger.Event{{Sequence: "9", ClaimID: "claim-1", Kind: "renewed"}}, NextCursor: "current"},
		},
	}
	msg := ClaimsRefreshCmd(context.Background(), reader, "old-cursor")().(ClaimsRefreshMsg)
	if msg.ClaimsErr != nil || msg.EventsErr != nil || !msg.Gap || msg.Cursor != "current" {
		t.Fatalf("refresh result=%+v", msg)
	}
	if len(reader.calls) != 2 || reader.calls[0] != "old-cursor" || reader.calls[1] != "" {
		t.Fatalf("event cursor calls=%v, want old cursor then reset", reader.calls)
	}
	m := New(queue.Snapshot{Items: map[string]queue.Item{}})
	m.Views, m.ViewName = []string{ClaimsViewID}, ClaimsViewID
	updated, _ := m.Update(msg)
	m = updated.(Model)
	if !m.Claims.Gap || m.Claims.Cursor != "current" || len(m.Claims.Items) != 1 || len(m.Claims.Events) != 1 {
		t.Fatalf("live claim state=%+v", m.Claims)
	}
	var polledCursor string
	m.Claims.Refresh = func(cursor string) tea.Cmd {
		polledCursor = cursor
		return func() tea.Msg {
			return ClaimsRefreshMsg{Claims: []lease.ClaimView{{ClaimID: "claim-2"}}, Events: []ledger.Event{{Sequence: "10", ClaimID: "claim-2", Kind: "acquired"}}, Cursor: "next"}
		}
	}
	updated, poll := m.Update(ClaimsTickMsg{})
	m = updated.(Model)
	if poll == nil || !m.Claims.Loading || polledCursor != "current" {
		t.Fatalf("live tick did not poll the current cursor: cmd=%t loading=%t cursor=%q", poll != nil, m.Claims.Loading, polledCursor)
	}
	updated, nextPoll := m.Update(poll())
	m = updated.(Model)
	if nextPoll == nil || len(m.Claims.Items) != 1 || m.Claims.Items[0].ClaimID != "claim-2" || len(m.Claims.Events) != 2 {
		t.Fatalf("live refresh was not applied and continued: state=%+v next=%t", m.Claims, nextPoll != nil)
	}
}

func TestClaimsCorrelationJumpAndRemotePublicRendering(t *testing.T) {
	t.Parallel()
	m := New(fixture())
	m.Views = []string{"All", ClaimsViewID}
	m.ViewName = ClaimsViewID
	m.Width, m.Height = 120, 30
	m.Claims.Now = func() time.Time { return time.Unix(100, 0).UTC() }
	m.Claims.Items = []lease.ClaimView{{ClaimID: "claim-1", Resources: []string{"resource:1"}, AgentID: "alice", SessionID: "session-1", AcquiredAt: time.Unix(50, 0), ExpiresAt: time.Unix(200, 0), Active: true, CheckpointPresent: true}}
	m.Claims.Selected = "claim-1"
	if screen := m.View(); !strings.Contains(screen, "first") || !strings.Contains(screen, "resource:1") {
		t.Fatalf("correlated row does not show title and raw resource: %s", screen)
	}
	m.Claims.Detail = true
	if screen := m.View(); !strings.Contains(screen, "alice") || !strings.Contains(screen, "session-1") || !strings.Contains(screen, "expires in") || !strings.Contains(screen, "Checkpoint") || !strings.Contains(screen, "present (public status only)") {
		t.Fatalf("claim detail lacks holder, session, countdown or checkpoint presence: %s", screen)
	}
	m.Claims.Events = []ledger.Event{{Sequence: "1", ClaimID: "claim-1", At: time.Unix(60, 0), Kind: "checkpointed", Detail: map[string]any{"token": "private-token", "checkpoint": "private-progress"}}}
	m.Claims.History = ledger.HistoryPage{Epochs: []ledger.Epoch{
		{ClaimID: "claim-1", Status: "open", Operations: []ledger.Operation{{OperationID: "private-operation-id", Kind: "checkpoint", State: "completed"}}},
		{ClaimID: "other-claim", AgentID: "other-agent", SessionID: "other-session"},
	}}
	m.Claims.Detail, m.Claims.DetailTab = true, 1
	if screen := m.View(); !strings.Contains(screen, "checkpointed") || !strings.Contains(screen, "checkpoint · completed") || strings.Contains(screen, "private-token") || strings.Contains(screen, "private-progress") || strings.Contains(screen, "private-operation-id") || strings.Contains(screen, "other-session") {
		t.Fatalf("history view leaked private session payload or hid lifecycle: %s", screen)
	}
	m.Help = true
	if help := m.View(); !strings.Contains(help, "authority-wide") || !strings.Contains(help, "Claimed filters items") || strings.Contains(help, "release verified") {
		t.Fatalf("Claims help does not distinguish the read-only tab: %s", help)
	}
	m.Help = false
	m, _ = press(m, "i")
	if m.ViewName != "All" || m.Selected != "stable-1" || !m.Detail {
		t.Fatalf("item jump failed: view=%q selected=%q detail=%t", m.ViewName, m.Selected, m.Detail)
	}
}

func TestClaimsRefreshReplacesPendingHistoryOnClaimChange(t *testing.T) {
	t.Parallel()
	m := New(queue.Snapshot{Items: map[string]queue.Item{}})
	m.Views, m.ViewName = []string{ClaimsViewID}, ClaimsViewID
	m.Claims.Items = []lease.ClaimView{{ClaimID: "old", Resources: []string{"file:one"}}}
	m.Claims.Selected, m.Claims.Detail = "old", true
	m.Claims.HistoryResource, m.Claims.HistoryLoading = "file:one", true
	m.Claims.LoadHistory = func(claimID, resource, cursor string) tea.Cmd {
		return func() tea.Msg {
			return ClaimHistoryMsg{ClaimID: claimID, Resource: resource, Page: ledger.HistoryPage{Epochs: []ledger.Epoch{{ClaimID: claimID}}}}
		}
	}
	updated, command := m.Update(ClaimsRefreshMsg{Claims: []lease.ClaimView{{ClaimID: "new", Resources: []string{"file:one"}}}})
	m = updated.(Model)
	if m.Claims.Selected != "new" || !m.Claims.HistoryLoading || command == nil {
		t.Fatalf("replacement did not load new claim history: selected=%q loading=%t command=%t", m.Claims.Selected, m.Claims.HistoryLoading, command != nil)
	}
	updated, _ = m.Update(ClaimHistoryMsg{ClaimID: "old", Resource: "file:one"})
	m = updated.(Model)
	if !m.Claims.HistoryLoading {
		t.Fatal("old history response completed new claim's history")
	}
	updated, _ = m.Update(command())
	m = updated.(Model)
	if m.Claims.HistoryLoading || len(m.Claims.History.Epochs) != 1 || m.Claims.History.Epochs[0].ClaimID != "new" {
		t.Fatalf("new claim history not loaded: %+v", m.Claims.History)
	}
}

func TestClaimsDetailLoadsHistoryForSelectedClaim(t *testing.T) {
	t.Parallel()
	m := New(queue.Snapshot{Items: map[string]queue.Item{}})
	m.Views, m.ViewName = []string{ClaimsViewID}, ClaimsViewID
	m.Claims.Items = []lease.ClaimView{{ClaimID: "claim-1", Resources: []string{"file:one"}}}
	m.Claims.LoadHistory = func(claimID, resource, cursor string) tea.Cmd {
		if claimID != "claim-1" || resource != "file:one" || cursor != "" {
			t.Errorf("history request=(%q,%q,%q)", claimID, resource, cursor)
		}
		return func() tea.Msg {
			return ClaimHistoryMsg{ClaimID: claimID, Resource: resource, Page: ledger.HistoryPage{Epochs: []ledger.Epoch{{ClaimID: claimID}}}}
		}
	}
	m, command := press(m, "enter")
	if command == nil || !m.Claims.HistoryLoading || m.Claims.HistoryResource != "file:one" {
		t.Fatalf("history request state: command=%t loading=%t resource=%q", command != nil, m.Claims.HistoryLoading, m.Claims.HistoryResource)
	}
	updated, _ := m.Update(command())
	m = updated.(Model)
	if m.Claims.HistoryLoading || len(m.Claims.History.Epochs) != 1 {
		t.Fatalf("history response not displayed: %+v", m.Claims.History)
	}
}

func TestClaimsJumpKeepsVisibleNoticeWhenQueueFilterHidesItem(t *testing.T) {
	t.Parallel()
	m := New(fixture())
	m.Views, m.ViewName = []string{"All", ClaimsViewID}, ClaimsViewID
	m.Filter = "does-not-match-any-item"
	m.Claims.Items = []lease.ClaimView{{ClaimID: "one", Resources: []string{"resource:1"}}}
	m.Claims.Selected = "one"
	m, _ = press(m, "i")
	if m.ViewName != ClaimsViewID || !strings.Contains(m.View(), "clear the queue filter") {
		t.Fatalf("hidden item jump lost failure notice or switched tabs: %q", m.ViewName)
	}
}

func TestClaimsRemoteHistoryUsesOnlyListFields(t *testing.T) {
	t.Parallel()
	m := New(queue.Snapshot{Items: map[string]queue.Item{}})
	m.Views, m.ViewName = []string{ClaimsViewID}, ClaimsViewID
	m.Claims.PublicOnly = true
	m.Claims.Items = []lease.ClaimView{{ClaimID: "other", Resources: []string{"file:one"}, AgentID: "bob", AcquiredAt: time.Unix(10, 0), HeartbeatAt: time.Unix(20, 0), ExpiresAt: time.Unix(30, 0)}}
	m.Claims.Selected, m.Claims.Detail, m.Claims.DetailTab = "other", true, 1
	m.Claims.Events = []ledger.Event{{ClaimID: "other", Kind: "checkpointed"}}
	m.Claims.History = ledger.HistoryPage{Epochs: []ledger.Epoch{{ClaimID: "other", Operations: []ledger.Operation{{Kind: "checkpoint", State: "completed"}}}}}
	m.Claims.LoadHistory = func(string, string, string) tea.Cmd { t.Fatal("remote history should not be requested"); return nil }
	if screen := m.View(); strings.Contains(screen, "checkpointed") || strings.Contains(screen, "checkpoint · completed") || !strings.Contains(screen, "Heartbeat") {
		t.Fatalf("remote history exposed events or omitted public timestamps: %s", screen)
	}
	m, command := press(m, "enter")
	if command != nil {
		t.Fatal("remote claim detail requested history")
	}
}

func TestClaimsFiltersMinePrefixExpiringAndStale(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0).UTC()
	m := New(queue.Snapshot{Items: map[string]queue.Item{}})
	m.Views, m.ViewName = []string{ClaimsViewID}, ClaimsViewID
	m.Claims.Now = func() time.Time { return now }
	m.Claims.MineAgentID, m.Claims.MineSessionID = "alice", "s1"
	m.Claims.Items = []lease.ClaimView{
		{ClaimID: "mine", AgentID: "alice", SessionID: "s1", Resources: []string{"file:src/a"}, Active: true, ExpiresAt: now.Add(time.Minute)},
		{ClaimID: "other", AgentID: "bob", SessionID: "s1", Resources: []string{"branch:main"}, Active: true, ExpiresAt: now.Add(time.Hour)},
	}
	if got := len(m.claimRows()); got != 2 {
		t.Fatalf("unfiltered claims=%d", got)
	}
	m, _ = press(m, "m")
	if got := m.claimRows(); len(got) != 1 || got[0].ClaimID != "mine" {
		t.Fatalf("mine filter included another agent with same session: %v", got)
	}
	m, _ = press(m, "/")
	m, _ = press(m, "file:")
	m, _ = press(m, "enter")
	m, _ = press(m, "e")
	if got := m.claimRows(); len(got) != 1 || got[0].ClaimID != "mine" || m.claimStateLabel(got[0], now) != "EXPIRING" {
		t.Fatalf("mine/prefix/expiring keyboard filter=%v", got)
	}
	m.Claims.Stale = true
	m, _ = press(m, "s")
	if got := m.claimRows(); len(got) != 1 || got[0].ClaimID != "mine" || m.claimStateLabel(got[0], now) != "STALE" {
		t.Fatalf("stale filter/status=%v", got)
	}
}
