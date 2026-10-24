package claimsui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestClaimsListScrollKeepsVisibleSelection(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"ctrl+d", "ctrl+u", "ctrl+f", "ctrl+b", "pgdown", "pgup", "ctrl+e", "ctrl+y"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			m := New()
			m.Width, m.Height = 120, 20
			capacity := max(1, m.frame().bodyHeight-1)
			for index := range capacity * 5 {
				m.Claims.Items = append(m.Claims.Items, lease.ClaimView{ClaimID: fmt.Sprintf("claim-%02d", index)})
			}
			rows := m.claimRows()
			m.Claims.Offset = capacity * 2
			step := scrollStep(key, capacity)
			middle := capacity / 2
			if step < 0 {
				middle = (capacity - 1) / 2
			}
			m.selectClaimIndex(rows, m.Claims.Offset+middle, capacity)
			original := m.Claims.Index
			for move := range 2 {
				wantOffset := max(0, min(m.Claims.Offset+step, len(rows)-capacity))
				wantIndex := max(wantOffset, min(m.Claims.Index, wantOffset+capacity-1))
				m, _ = press(m, key)
				if m.Claims.Offset != wantOffset || m.Claims.Index != wantIndex || m.Claims.Selected != rows[wantIndex].ClaimID {
					t.Fatalf("offset=%d index=%d selected=%q, want offset=%d index=%d selected=%q", m.Claims.Offset, m.Claims.Index, m.Claims.Selected, wantOffset, wantIndex, rows[wantIndex].ClaimID)
				}
				if key == "ctrl+d" || key == "ctrl+u" {
					if move == 0 && m.Claims.Index != original {
						t.Fatal("first half-page scroll moved a visible selection")
					}
					if move == 1 && m.Claims.Index == original {
						t.Fatal("selection did not follow viewport after leaving view")
					}
				}
			}
		})
	}
}

func TestClaimsMouseMatchesRenderedRowsAndTabs(t *testing.T) {
	t.Parallel()
	for _, width := range []int{30, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			t.Parallel()
			m := New()
			m.Width = width
			m.Claims.PublicOnly = true
			m.Claims.Items = []lease.ClaimView{{ClaimID: "claim", Resources: []string{"resource:one"}}}
			m.anchorClaims(m.claimRows(), 10)
			row := -1
			for y, line := range strings.Split(ansi.Strip(m.View()), "\n") {
				if strings.Contains(line, "resource:one") {
					row = y
					break
				}
			}
			if row < 0 {
				t.Fatal("claim row not rendered")
			}
			click := tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 1, Y: row}
			next, _ := m.Update(click)
			m = next.(Model)
			if m.Claims.Detail {
				t.Fatal("mouse release opened detail without a second press")
			}
			click.Action = tea.MouseActionPress
			next, _ = m.Update(click)
			m = next.(Model)
			if !m.Claims.Detail {
				t.Fatal("clicking the rendered claim row did not open detail")
			}
			for _, label := range []string{"History", "Summary"} {
				found := false
				for y, line := range strings.Split(ansi.Strip(m.View()), "\n") {
					x := strings.Index(line, label)
					if x < 0 {
						continue
					}
					found = true
					click.X, click.Y = ansi.StringWidth(line[:x]), y
					next, _ = m.Update(click)
					m = next.(Model)
					want := "Checkpoint"
					if label == "History" {
						want = "Remote authorities do not share claim history."
					}
					if view := screenText(m.View()); !strings.Contains(view, want) {
						t.Fatalf("clicking %s did not show its content: %s", label, view)
					}
					break
				}
				if !found {
					t.Fatalf("%s tab not rendered", label)
				}
			}
		})
	}
}

func TestClaimsNewestAcquisitionFirstAndSelectionStableOnRefresh(t *testing.T) {
	t.Parallel()
	m := New()
	claims := []lease.ClaimView{
		{ClaimID: "old", AcquiredAt: time.Unix(10, 0), HeartbeatAt: time.Unix(90, 0)},
		{ClaimID: "same-b", AcquiredAt: time.Unix(20, 0)},
		{ClaimID: "same-a", AcquiredAt: time.Unix(20, 0)},
		{ClaimID: "new", AcquiredAt: time.Unix(30, 0)},
	}
	m.applyClaimsRefresh(ClaimsRefreshMsg{Claims: claims})
	for index, want := range []string{"new", "same-a", "same-b", "old"} {
		if got := m.claimRows()[index].ClaimID; got != want {
			t.Fatalf("row %d = %q, want %q", index, got, want)
		}
	}
	if m.Claims.Selected != "new" {
		t.Fatalf("initial selection = %q, want newest claim", m.Claims.Selected)
	}
	m.Claims.Selected = "same-b"
	claims[0].HeartbeatAt = time.Unix(100, 0)
	claims = append(claims, lease.ClaimView{ClaimID: "latest", AcquiredAt: time.Unix(40, 0)})
	m.applyClaimsRefresh(ClaimsRefreshMsg{Claims: claims})
	rows := m.claimRows()
	if m.Claims.Selected != "same-b" || m.Claims.Index != 3 || rows[0].ClaimID != "latest" || rows[len(rows)-1].ClaimID != "old" {
		t.Fatalf("refresh moved selection or sorted by heartbeat: selected=%q index=%d rows=%v", m.Claims.Selected, m.Claims.Index, rows)
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

func TestClaimsEmptyViewRemainsStableDuringPolling(t *testing.T) {
	t.Parallel()
	m := New()
	m.Claims.Now = func() time.Time { return time.Unix(100, 0).UTC() }
	m.Claims.Loading = true
	m.Claims.Refresh = func(string) tea.Cmd {
		return func() tea.Msg { return ClaimsRefreshMsg{} }
	}
	if view := screenText(m.View()); !strings.Contains(view, "Loading authority claims…") {
		t.Fatalf("initial read did not show loading:\n%s", view)
	}
	updated, _ := m.Update(ClaimsRefreshMsg{})
	m = updated.(Model)
	emptyView := screenText(m.View())
	if !strings.Contains(emptyView, "No active claims on this authority.") {
		t.Fatalf("successful empty read did not show empty state:\n%s", emptyView)
	}
	updated, poll := m.Update(ClaimsTickMsg{})
	m = updated.(Model)
	if poll == nil {
		t.Fatal("background poll did not start")
	}
	if view := screenText(m.View()); view != emptyView {
		t.Fatalf("background poll changed empty view:\n%s", view)
	}
	updated, _ = m.Update(poll())
	m = updated.(Model)
	if view := screenText(m.View()); view != emptyView {
		t.Fatalf("unchanged poll result changed empty view:\n%s", view)
	}
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
	m := New()
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

func TestClaimsPublicRendering(t *testing.T) {
	t.Parallel()
	m := New()
	m.Width, m.Height = 120, 30
	m.Claims.Now = func() time.Time { return time.Unix(100, 0).UTC() }
	m.Claims.Items = []lease.ClaimView{{ClaimID: "claim-1", Resources: []string{"resource:1"}, AgentID: "alice", SessionID: "session-1", AcquiredAt: time.Unix(50, 0), ExpiresAt: time.Unix(200, 0), Active: true, CheckpointPresent: true}}
	m.Claims.Selected = "claim-1"
	m.Claims.Detail = true
	if screen := m.View(); !strings.Contains(screen, "alice") || !strings.Contains(screen, "session-1") || !strings.Contains(screen, "expires in") || !strings.Contains(screen, "Checkpoint") || !strings.Contains(screen, "present") || !strings.Contains(screen, "resource:1") {
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
	if help := screenText(m.View()); !strings.Contains(help, "authority-wide") || !strings.Contains(help, "u preview renew") || !strings.Contains(help, "R release reason, preview") {
		t.Fatalf("Claims help omits authority-wide claim actions: %s", help)
	}
	m.Help = false

}

func TestClaimsDisplayDecodesResourcesWithoutChangingKeys(t *testing.T) {
	t.Parallel()
	resource := "github:owner%2Frepo#issue%3A1"
	m := New()
	m.Width, m.Height = 160, 30
	m.Claims.Items = []lease.ClaimView{{ClaimID: "claim-1", Resources: []string{resource}, Active: true}}
	m.Claims.Selected = "claim-1"
	if screen := screenText(m.View()); !strings.Contains(screen, "github:owner/repo#issue:1") || strings.Contains(screen, "%2F") {
		t.Fatalf("claims list resource not decoded: %s", screen)
	}
	m.Claims.ResourcePrefix = "github:owner%2F"
	if len(m.claimRows()) != 1 {
		t.Fatal("claim filtering must still use the exact resource key")
	}
	m.Claims.ResourcePrefix = ""
	m.Claims.LoadHistory = func(claimID, requested, cursor string) tea.Cmd {
		if claimID != "claim-1" || requested != resource || cursor != "" {
			t.Errorf("history requested with decoded key: %q, %q, %q", claimID, requested, cursor)
		}
		return func() tea.Msg { return ClaimHistoryMsg{ClaimID: claimID, Resource: requested} }
	}
	m, command := press(m, "enter")
	if command == nil || m.Claims.HistoryResource != resource {
		t.Fatalf("history key=%q, command=%t", m.Claims.HistoryResource, command != nil)
	}
	if screen := screenText(m.View()); !strings.Contains(screen, "github:owner/repo#issue:1") || strings.Contains(screen, "%2F") {
		t.Fatalf("claim detail resource not decoded: %s", screen)
	}
	command()
}

func TestClaimsRefreshReplacesPendingHistoryOnClaimChange(t *testing.T) {
	t.Parallel()
	m := New()
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

func TestClaimsDetailReloadsHistoryWhenFilterOrWheelChangesSelection(t *testing.T) {
	t.Parallel()
	newModel := func() Model {
		m := New()
		m.Width, m.Height = 120, 12
		for i := range 40 {
			m.Claims.Items = append(m.Claims.Items, lease.ClaimView{ClaimID: fmt.Sprintf("claim-%02d", i), Resources: []string{fmt.Sprintf("file:%02d", i)}})
		}
		m.Claims.Selected, m.Claims.Detail = "claim-00", true
		m.Claims.HistoryResource = "file:00"
		m.Claims.History = ledger.HistoryPage{Epochs: []ledger.Epoch{{ClaimID: "claim-00"}}}
		m.Claims.LoadHistory = func(claimID, resource, cursor string) tea.Cmd {
			return func() tea.Msg {
				return ClaimHistoryMsg{ClaimID: claimID, Resource: resource, Page: ledger.HistoryPage{Epochs: []ledger.Epoch{{ClaimID: claimID}}}}
			}
		}
		return m
	}
	t.Run("filter", func(t *testing.T) {
		t.Parallel()
		m := newModel()
		m.Filtering, m.Input = true, "file:01"
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		m = updated.(Model)
		if m.Claims.Selected != "claim-01" || cmd == nil || !m.Claims.HistoryLoading {
			t.Fatalf("filter selection=%q loading=%t cmd=%t", m.Claims.Selected, m.Claims.HistoryLoading, cmd != nil)
		}
		updated, _ = m.Update(cmd())
		m = updated.(Model)
		if len(m.Claims.History.Epochs) != 1 || m.Claims.History.Epochs[0].ClaimID != "claim-01" {
			t.Fatalf("filter history=%+v", m.Claims.History)
		}
	})
	t.Run("wheel", func(t *testing.T) {
		t.Parallel()
		m := newModel()
		rows := m.claimRows()
		capacity := max(1, m.frame().bodyHeight-1)
		m.Claims.Offset = capacity
		updated, cmd := m.claimsMouse(tea.MouseMsg{Button: tea.MouseButtonWheelDown, X: 0})
		m = updated.(Model)
		if m.Claims.Selected == "claim-00" || cmd == nil || !m.Claims.HistoryLoading {
			t.Fatalf("wheel selection=%q loading=%t cmd=%t capacity=%d rows=%d", m.Claims.Selected, m.Claims.HistoryLoading, cmd != nil, capacity, len(rows))
		}
		selected := m.Claims.Selected
		updated, _ = m.Update(cmd())
		m = updated.(Model)
		if len(m.Claims.History.Epochs) != 1 || m.Claims.History.Epochs[0].ClaimID != selected {
			t.Fatalf("wheel history=%+v selected=%q", m.Claims.History, selected)
		}
	})
}

func TestClaimsDetailLoadsHistoryForSelectedClaim(t *testing.T) {
	t.Parallel()
	m := New()
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
	if !m.Claims.Detail {
		t.Fatal("enter did not open claim detail")
	}
	selected := m.Claims.Selected
	m, command = press(m, "enter")
	if m.Claims.Detail || m.Claims.Selected != selected || command != nil {
		t.Fatal("enter did not return to the selected claim without reloading history")
	}
}

func TestClaimsRemoteHistoryUsesOnlyListFields(t *testing.T) {
	t.Parallel()
	m := New()
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
	m := New()
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
	if got := m.claimRows(); len(got) != 1 || got[0].ClaimID != "mine" || m.claimStateLabel(got[0], now) != "expiring" {
		t.Fatalf("mine/prefix/expiring keyboard filter=%v", got)
	}
	m.Claims.Stale = true
	m, _ = press(m, "s")
	if got := m.claimRows(); len(got) != 1 || got[0].ClaimID != "mine" || m.claimStateLabel(got[0], now) != "stale" {
		t.Fatalf("stale filter/status=%v", got)
	}
}

func TestClaimsRenewPreviewConfirmationAndImmediateUpdate(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0).UTC()
	claim := lease.ClaimView{ClaimID: "claim-1", AuthorityID: "authority-1", Revision: 4, RestoreID: "restore-1", Resources: []string{"coordination:task"}, Active: true, ExpiresAt: now.Add(5 * time.Minute)}
	handle := ClaimsHandle{Kind: "contextual", Path: "/home/me/handles/ctx-claim.json", Revision: 4, AuthorityID: "authority-1", RestoreID: "restore-1"}
	m := New()
	m.Width, m.Height = 160, 30
	m.Claims.Items = []lease.ClaimView{claim}
	m.Claims.Selected = claim.ClaimID
	m.Claims.Now = func() time.Time { return now }
	m.Claims.RenewTTL = 15 * time.Minute
	resolveCalls := 0
	m.Claims.ResolveHandles = func(claims []lease.ClaimView) map[string]ClaimsHandle {
		resolveCalls++
		resolved := handle
		if len(claims) > 0 {
			resolved.Revision = claims[0].Revision
		}
		return map[string]ClaimsHandle{claim.ClaimID: resolved}
	}
	updated, _ := m.Update(ClaimsRefreshMsg{Claims: []lease.ClaimView{claim}})
	m = updated.(Model)
	if resolveCalls != 1 {
		t.Fatalf("local handle resolver calls=%d, want one on refresh", resolveCalls)
	}
	calls := 0
	m.Claims.Mutate = func(ctx context.Context, expected lease.ClaimView, expectedHandle ClaimsHandle, reason string, release bool) (lease.ClaimView, error) {
		calls++
		if ctx == nil || expected.ClaimID != claim.ClaimID || expectedHandle != handle || reason != "" || release {
			t.Fatalf("renew callback arguments: ctx=%v claim=%+v handle=%+v reason=%q release=%t", ctx, expected, expectedHandle, reason, release)
		}
		expected.Revision++
		expected.ExpiresAt = now.Add(15 * time.Minute)
		return expected, nil
	}

	if screen := screenText(m.View()); !strings.Contains(screen, "* coordination:task") {
		t.Fatalf("eligible claim row lacks handle marker: %s", screen)
	}
	m.Claims.Detail = true
	if screen := screenText(m.View()); !strings.Contains(screen, "ctx-claim.json") || !strings.Contains(screen, "u renew") || !strings.Contains(screen, "R release") {
		t.Fatalf("eligible claim detail lacks handle or action keys: %s", screen)
	}
	m.Claims.Detail = false
	m, cmd := press(m, "u")
	if cmd != nil || m.Claims.actionPreview == nil || calls != 0 {
		t.Fatalf("renew key dispatched without preview: cmd=%t preview=%+v calls=%d", cmd != nil, m.Claims.actionPreview, calls)
	}
	preview := screenText(m.View())
	for _, text := range []string{"authority-1", "claim-1", "coordination:task", handle.Path, claim.ExpiresAt.Format(time.RFC3339), "default TTL 15m0s", now.Add(15 * time.Minute).Format(time.RFC3339)} {
		if !strings.Contains(preview, text) {
			t.Errorf("renew preview omitted %q: %s", text, preview)
		}
	}
	m, cmd = press(m, "enter")
	if cmd == nil || !m.Claims.mutating || calls != 0 || m.Claims.Items[0].Revision != claim.Revision {
		t.Fatalf("renew confirmation did not defer dispatch to command: cmd=%t busy=%t calls=%d claim=%+v", cmd != nil, m.Claims.mutating, calls, m.Claims.Items[0])
	}
	result, ok := cmd().(ClaimsMutationMsg)
	if !ok || result.Err != nil {
		t.Fatalf("renew callback result=%#v", result)
	}
	updated, refresh := m.Update(result)
	m = updated.(Model)
	if refresh != nil || m.Claims.mutating || len(m.Claims.Items) != 1 || m.Claims.Items[0].Revision != 5 || !m.Claims.Items[0].ExpiresAt.Equal(now.Add(15*time.Minute)) || m.Claims.Selected != claim.ClaimID || resolveCalls != 2 || m.Claims.handles[claim.ClaimID].Revision != 5 {
		t.Fatalf("renew result not applied immediately: state=%+v refresh=%t handle resolutions=%d", m.Claims, refresh != nil, resolveCalls)
	}
	m.Help = true
	if help := screenText(m.View()); !strings.Contains(help, "u preview renew") || !strings.Contains(help, "R release reason, preview") {
		t.Fatalf("Claims help omits action keys: %s", help)
	}
}

func TestClaimsReleaseReasonPreviewAndNeighborSelection(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0).UTC()
	claims := []lease.ClaimView{
		{ClaimID: "first", AuthorityID: "authority-1", Revision: 1, Resources: []string{"resource:first"}, Active: true, ExpiresAt: now.Add(time.Hour)},
		{ClaimID: "next", AuthorityID: "authority-1", Revision: 2, Resources: []string{"resource:next"}, Active: true, ExpiresAt: now.Add(time.Hour)},
	}
	m := New()
	m.Width, m.Height = 160, 30
	m.Claims.Items = claims
	m.Claims.Selected, m.Claims.Detail = "first", true
	m.Claims.Now = func() time.Time { return now }
	m.Claims.handles = map[string]ClaimsHandle{
		"first": {Kind: "MCP", Path: "/home/me/handles/mcp-first.json", Revision: 1, AuthorityID: "authority-1"},
		"next":  {Unavailable: "not held here"},
	}
	calls := 0
	m.Claims.Mutate = func(_ context.Context, claim lease.ClaimView, handle ClaimsHandle, reason string, release bool) (lease.ClaimView, error) {
		calls++
		if claim.ClaimID != "first" || handle.Path != "/home/me/handles/mcp-first.json" || reason != "released" || !release {
			t.Fatalf("release callback arguments: claim=%+v handle=%+v reason=%q release=%t", claim, handle, reason, release)
		}
		return lease.ClaimView{}, nil
	}
	m, cmd := press(m, "R")
	if cmd != nil || !m.Claims.releaseReasonInput || m.Input != "released" || calls != 0 {
		t.Fatalf("release did not open reason input with default: cmd=%t input=%q calls=%d", cmd != nil, m.Input, calls)
	}
	if prompt := screenText(m.View()); !strings.Contains(prompt, "Reason recorded in claim history") || !strings.Contains(prompt, "released") {
		t.Fatalf("release reason prompt missing default: %s", prompt)
	}
	m, cmd = press(m, "enter")
	if cmd != nil || m.Claims.releaseReasonInput || m.Claims.actionPreview == nil || calls != 0 {
		t.Fatalf("reason submission skipped confirmation: cmd=%t preview=%+v calls=%d", cmd != nil, m.Claims.actionPreview, calls)
	}
	preview := screenText(m.View())
	for _, text := range []string{"authority-1", "first", "resource:first", "/home/me/handles/mcp-first.json", "Current expiry", "Reason released", "resources become free", "private handle is removed"} {
		if !strings.Contains(preview, text) {
			t.Errorf("release preview omitted %q: %s", text, preview)
		}
	}
	m, cmd = press(m, "enter")
	if cmd == nil || !m.Claims.mutating || calls != 0 {
		t.Fatalf("release confirmation did not defer callback: cmd=%t busy=%t calls=%d", cmd != nil, m.Claims.mutating, calls)
	}
	result := cmd().(ClaimsMutationMsg)
	updated, refresh := m.Update(result)
	m = updated.(Model)
	if refresh != nil || calls != 1 || m.Claims.mutating || len(m.Claims.Items) != 1 || m.Claims.Items[0].ClaimID != "next" || m.Claims.Selected != "next" {
		t.Fatalf("release was not applied and selection moved to neighbor: claims=%+v selected=%q calls=%d refresh=%t", m.Claims.Items, m.Claims.Selected, calls, refresh != nil)
	}
}

func TestClaimsMutationIgnoresOlderInflightPoll(t *testing.T) {
	t.Parallel()
	for _, action := range []ClaimsAction{ClaimsActionRenew, ClaimsActionRelease} {
		t.Run(string(action), func(t *testing.T) {
			t.Parallel()
			old := lease.ClaimView{ClaimID: "claim", AuthorityID: "authority", Revision: 1, Resources: []string{"resource"}, Active: true, ExpiresAt: time.Now().Add(time.Minute)}
			m := New()
			m.Claims.Items, m.Claims.Selected = []lease.ClaimView{old}, old.ClaimID
			m.Claims.mutating = true
			m.Claims.Loading = true // refresh began before confirmation
			m.Claims.pendingMutation = &ClaimsActionPreview{Action: action, Claim: old}
			calls := 0
			m.Claims.Refresh = func(string) tea.Cmd {
				calls++
				return func() tea.Msg { return ClaimsRefreshMsg{Claims: nil} }
			}
			updated := old
			updated.Revision = 2
			updated.ExpiresAt = old.ExpiresAt.Add(time.Minute)
			result, _ := m.Update(ClaimsMutationMsg{Action: action, ClaimID: old.ClaimID, Claim: updated})
			m = result.(Model)
			if !m.Claims.refreshAfterMutation || calls != 0 {
				t.Fatalf("post-mutation refresh not queued: queued=%t calls=%d", m.Claims.refreshAfterMutation, calls)
			}
			result, _ = m.Update(ClaimsRefreshMsg{Claims: []lease.ClaimView{old}})
			m = result.(Model)
			if calls != 1 || m.Claims.refreshAfterMutation {
				t.Fatalf("post-mutation refresh not started: calls=%d queued=%t", calls, m.Claims.refreshAfterMutation)
			}
			if action == ClaimsActionRelease {
				if len(m.Claims.Items) != 0 {
					t.Fatalf("stale poll restored released claim: %+v", m.Claims.Items)
				}
			} else if len(m.Claims.Items) != 1 || m.Claims.Items[0].Revision != 2 {
				t.Fatalf("stale poll restored prior expiry: %+v", m.Claims.Items)
			}
		})
	}
}

func TestClaimsActionDismissalDriftAndBusyQuit(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0).UTC()
	claim := lease.ClaimView{ClaimID: "claim", AuthorityID: "authority-1", Revision: 2, Resources: []string{"resource"}, Active: true, ExpiresAt: now.Add(time.Hour)}
	handle := ClaimsHandle{Kind: "contextual", Path: "/home/me/handles/ctx-claim.json", Revision: 2, AuthorityID: "authority-1"}
	m := New()
	m.Claims.Items = []lease.ClaimView{claim}
	m.Claims.Selected = claim.ClaimID
	m.Claims.Now = func() time.Time { return now }
	m.Claims.handles = map[string]ClaimsHandle{claim.ClaimID: handle}
	calls := 0
	m.Claims.Mutate = func(_ context.Context, expected lease.ClaimView, expectedHandle ClaimsHandle, _ string, _ bool) (lease.ClaimView, error) {
		calls++
		if expected.Revision != 2 || expectedHandle.Revision != 2 {
			t.Fatalf("dispatch lost preview snapshot: claim revision=%d handle revision=%d", expected.Revision, expectedHandle.Revision)
		}
		if m.Claims.Items[0].Revision != expected.Revision {
			return lease.ClaimView{}, fmt.Errorf("claim revision changed from %d to %d", expected.Revision, m.Claims.Items[0].Revision)
		}
		return expected, nil
	}
	m, _ = press(m, "u")
	m, _ = press(m, "esc")
	if m.Claims.actionPreview != nil || calls != 0 {
		t.Fatalf("renew dismissal mutated or retained preview: preview=%+v calls=%d", m.Claims.actionPreview, calls)
	}
	m, _ = press(m, "R")
	m, _ = press(m, "esc")
	if m.Claims.actionPreview != nil || m.Claims.releaseReasonInput || calls != 0 {
		t.Fatalf("release dismissal mutated or retained state: preview=%+v input=%t calls=%d", m.Claims.actionPreview, m.Claims.releaseReasonInput, calls)
	}
	m, _ = press(m, "u")
	m.Claims.Items[0].Revision++
	m, cmd := press(m, "enter")
	if cmd == nil || calls != 0 {
		t.Fatalf("revision drift dispatched before command execution: cmd=%t calls=%d", cmd != nil, calls)
	}
	result := cmd().(ClaimsMutationMsg)
	updated, _ := m.Update(result)
	m = updated.(Model)
	if calls != 1 || m.Claims.Items[0].Revision != 3 || !strings.Contains(m.Claims.Notice, "claim revision changed") {
		t.Fatalf("drift refusal changed claim or lost cause: calls=%d state=%+v", calls, m.Claims)
	}
	m.Claims.mutating = true
	updated, quit := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = updated.(Model)
	if quit != nil || !strings.Contains(m.Notice, "Wait for") {
		t.Fatalf("quit did not wait for Claims mutation: cmd=%t notice=%q", quit != nil, m.Notice)
	}
}

func TestClaimsAmbiguousMutationKeepsRecoveryGuidance(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0).UTC()
	claim := lease.ClaimView{ClaimID: "claim", AuthorityID: "authority", Revision: 2, Resources: []string{"resource"}, Active: true, ExpiresAt: now.Add(time.Hour)}
	path := "/home/me/handles/ctx-claim.json"
	pending := false
	m := New()
	m.Claims.Items = []lease.ClaimView{claim}
	m.Claims.Selected = claim.ClaimID
	m.Claims.Now = func() time.Time { return now }
	m.Claims.ResolveHandles = func([]lease.ClaimView) map[string]ClaimsHandle {
		handle := ClaimsHandle{Kind: "contextual", Path: path, Revision: 2, AuthorityID: "authority"}
		if pending {
			handle.Unavailable = "pending; recover with worklease heartbeat --handle " + path
		}
		return map[string]ClaimsHandle{claim.ClaimID: handle}
	}
	m.Claims.Mutate = func(context.Context, lease.ClaimView, ClaimsHandle, string, bool) (lease.ClaimView, error) {
		pending = true
		return lease.ClaimView{}, fmt.Errorf("outcome uncertain; recover with worklease heartbeat --handle %s", path)
	}
	updated, _ := m.Update(ClaimsRefreshMsg{Claims: []lease.ClaimView{claim}})
	m = updated.(Model)
	m, _ = press(m, "u")
	m, cmd := press(m, "enter")
	if cmd == nil {
		t.Fatal("confirmation did not dispatch mutation")
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if !strings.Contains(m.Claims.Notice, "outcome uncertain") || !strings.Contains(m.Claims.Notice, "worklease heartbeat --handle "+path) || strings.Contains(m.Claims.Notice, "Claim renewed") {
		t.Fatalf("ambiguous result was reported incorrectly: %q", m.Claims.Notice)
	}
	if got := m.claimActionUnavailable(claim); !strings.Contains(got, "pending") || !strings.Contains(got, path) {
		t.Fatalf("pending handle recovery guidance not refreshed: %q", got)
	}
}

func TestClaimsHandleMetadataMismatchExplainsUnavailableActions(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0).UTC()
	claim := lease.ClaimView{ClaimID: "claim", AuthorityID: "authority-1", RestoreID: "restore-1", Revision: 4, Active: true, ExpiresAt: now.Add(time.Hour)}
	for _, test := range []struct {
		name, want string
		handle     ClaimsHandle
	}{
		{"authority", "authority mismatch", ClaimsHandle{Kind: "contextual", Path: "/handle", Revision: 4, AuthorityID: "authority-2", RestoreID: "restore-1"}},
		{"restore", "restore ID mismatch", ClaimsHandle{Kind: "contextual", Path: "/handle", Revision: 4, AuthorityID: "authority-1", RestoreID: "restore-2"}},
		{"revision", "claim revision mismatch", ClaimsHandle{Kind: "contextual", Path: "/handle", Revision: 3, AuthorityID: "authority-1", RestoreID: "restore-1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			m := New()
			m.Claims.Items = []lease.ClaimView{claim}
			m.Claims.Selected = claim.ClaimID
			m.Claims.Now = func() time.Time { return now }
			m.Claims.handles = map[string]ClaimsHandle{claim.ClaimID: test.handle}
			if m.claimActionEligible(claim) || m.claimActionUnavailable(claim) != test.want {
				t.Fatalf("handle mismatch action eligibility=%t reason=%q, want %q", m.claimActionEligible(claim), m.claimActionUnavailable(claim), test.want)
			}
			m.Claims.Mutate = func(context.Context, lease.ClaimView, ClaimsHandle, string, bool) (lease.ClaimView, error) {
				t.Fatal("mismatched handle must not dispatch")
				return lease.ClaimView{}, nil
			}
			m, cmd := press(m, "u")
			if cmd != nil || m.Claims.actionPreview != nil || !strings.Contains(m.Claims.Notice, test.want) {
				t.Fatalf("mismatched handle action not refused: cmd=%t preview=%+v notice=%q", cmd != nil, m.Claims.actionPreview, m.Claims.Notice)
			}
		})
	}
}

func TestClaimsMatchingHolderIdentityDoesNotEnableUnownedActions(t *testing.T) {
	t.Parallel()
	now := time.Unix(1000, 0).UTC()
	claim := lease.ClaimView{ClaimID: "foreign", AuthorityID: "authority", AgentID: "same-agent", SessionID: "same-session", Resources: []string{"resource"}, Active: true, ExpiresAt: now.Add(time.Hour)}
	m := New()
	m.Claims.MineAgentID, m.Claims.MineSessionID = claim.AgentID, claim.SessionID
	m.Claims.Now = func() time.Time { return now }
	m.Claims.Detail = true
	m.Claims.ResolveHandles = func([]lease.ClaimView) map[string]ClaimsHandle {
		return map[string]ClaimsHandle{claim.ClaimID: {Unavailable: "not held here"}}
	}
	m.Claims.Mutate = func(context.Context, lease.ClaimView, ClaimsHandle, string, bool) (lease.ClaimView, error) {
		t.Fatal("matching public identity must not dispatch a mutation")
		return lease.ClaimView{}, nil
	}
	updated, _ := m.Update(ClaimsRefreshMsg{Claims: []lease.ClaimView{claim}})
	m = updated.(Model)
	if screen := screenText(m.View()); !strings.Contains(screen, "Actions not held here") || strings.Contains(screen, "* resource") {
		t.Fatalf("public identity enabled or hid the unavailable handle reason: %s", screen)
	}
	m, cmd := press(m, "u")
	if cmd != nil || m.Claims.actionPreview != nil || !strings.Contains(m.Claims.Notice, "not held here") {
		t.Fatalf("unowned action not refused: cmd=%t preview=%+v notice=%q", cmd != nil, m.Claims.actionPreview, m.Claims.Notice)
	}
}

func TestShortResourceNamesHostLocalItems(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ key, want string }{
		{"backlog-md:%2FUsers%2Fme%2Fdev%2Fworklease%2F.git:docs%2Fbacklog:TASK-126", "worklease TASK-126"},
		{"backlog-md:/srv/repos/policyd.git:backlog:PD-066", "policyd PD-066"},
		{"markdown:/home/me/notes/.git:README.md:__source__", "notes README.md"},
		{"path:/home/me/repo:src%2Fmain.go", "src/main.go"},
	} {
		if got := shortResource(test.key); got != test.want {
			t.Errorf("shortResource(%q)=%q, want %q", test.key, got, test.want)
		}
	}
}

func TestClaimsListLabelsCallerWorkKey(t *testing.T) {
	t.Parallel()
	opaque := "coordination:generic:" + strings.Repeat("d", 64)
	other := "coordination:generic:" + strings.Repeat("e", 64)
	m := New()
	m.Width, m.Height = 60, 10
	m.Claims.Items = []lease.ClaimView{
		{ClaimID: "a", Resources: []string{opaque}, WorkKey: "github.com/brettinternet/backlog BL-76\x1b[31m " + strings.Repeat("x", 80)},
		{ClaimID: "b", Resources: []string{other}, WorkKey: other},
	}
	lines := m.claimListLines(m.claimRows(), m.Width, m.Height)
	text := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(text, "github.com/brettinternet/backlog BL-76 …") || strings.Contains(text, "coordination:generic:dddd") || strings.Contains(text, "[31m") {
		t.Fatalf("caller work key not shown in place of resource:\n%s", text)
	}
	if !strings.Contains(text, "coordination:generic:eeeeeeee…") {
		t.Fatalf("default work key changed resource label:\n%s", text)
	}
	for _, line := range lines {
		if width := ansi.StringWidth(line); width > m.Width {
			t.Fatalf("line width %d exceeds %d: %q", width, m.Width, line)
		}
	}
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

func screenText(s string) string { return strings.Join(strings.Fields(ansi.Strip(s)), " ") }
