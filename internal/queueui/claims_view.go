package queueui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) claimsBody(height int) []string {
	rows := m.claimRows()
	m.anchorClaims(rows, max(1, height-1))
	if !m.Claims.Detail {
		return m.claimListLines(rows, m.Width, height)
	}
	claim, ok := m.selectedClaim(rows)
	if !ok {
		return m.claimListLines(rows, m.Width, height)
	}
	listWidth, detailWidth := m.paneWidths()
	pane := m.claimDetailPane(claim, detailWidth, height)
	if listWidth == 0 {
		return pane
	}
	list := m.claimListLines(rows, listWidth-1, height)
	return splitBody(list, pane, listWidth, height, m.s().faint.Render("│"))
}

func (m Model) claimListLines(rows []lease.ClaimView, width, height int) []string {
	if height <= 0 {
		return nil
	}
	width = max(1, width)
	lines := []string{"  " + m.s().faint.Render(clip("Resource · holder / session · lease", width-2))}
	capacity := max(0, height-1)
	start := max(0, min(m.Claims.Offset, len(rows)-capacity))
	end := min(len(rows), start+capacity)
	now := m.claimsNow()
	for index, claim := range rows[start:end] {
		text := strings.Join(claim.Resources, ", ")
		if item, ok := m.claimItem(claim); ok {
			text += " · " + item.Title
		}
		text += " · " + valueOr(claim.AgentID, "unknown holder")
		if claim.SessionID != "" {
			text += " / " + claim.SessionID
		}
		text += " · " + claimExpiryLabel(claim, now)
		text += " · " + m.claimStateLabel(claim, now)
		if index+start == m.Claims.Index {
			lines = append(lines, m.s().selected.Render(pad("> "+clip(text, width-2), width)))
			continue
		}
		style := lipgloss.NewStyle()
		if m.Claims.Stale {
			style = m.s().warn
		} else if !claim.Active {
			style = m.s().faint
		} else if claimExpiring(claim, now) {
			style = m.s().warnBold
		}
		lines = append(lines, "  "+style.Render(clip(text, width-2)))
	}
	if len(rows) == 0 {
		message := "No authority claims match these filters."
		if len(m.Claims.Items) == 0 && m.Claims.Loading {
			message = "Loading current authority claims…"
		} else if len(m.Claims.Items) == 0 && m.Claims.Error == "" {
			message = "No current authority claims."
		}
		lines = append(lines, "", "  "+m.s().bold.Render(clip(message, width-2)))
		if len(m.Claims.Items) == 0 && m.Claims.Error == "" {
			lines = append(lines, "  "+clip("Start with worklease acquire --path README.md", width-2), "  "+clip("Press ? for help · q to quit", width-2))
		}
	}
	return lines
}

func (m Model) claimStateLabel(claim lease.ClaimView, now time.Time) string {
	switch {
	case m.Claims.Stale:
		return "STALE"
	case !claim.Active:
		return "EXPIRED"
	case claimExpiring(claim, now):
		return "EXPIRING"
	default:
		return "ACTIVE"
	}
}

func claimExpiryLabel(claim lease.ClaimView, now time.Time) string {
	if claim.ExpiresAt.IsZero() {
		return "expiry unknown"
	}
	remaining := claim.ExpiresAt.Sub(now)
	if remaining <= 0 || !claim.Active {
		return "expired"
	}
	return "expires in " + remaining.Round(time.Second).String()
}

func (m Model) claimDetailPane(claim lease.ClaimView, width, height int) []string {
	claimID := claim.ClaimID
	if len(claimID) > 12 {
		claimID = claimID[:8] + "…" + claimID[len(claimID)-3:]
	}
	title := m.s().accent.Bold(true).Render("Claim " + clip(claimID, 20))
	items := []tabItem{{label: "Summary"}, {label: "History"}}
	tabs, _ := m.s().tabBar(items, m.Claims.DetailTab, width-1)
	content := m.claimDetailContent(claim, width)
	lines := []string{" " + title, " " + tabs}
	visible := max(0, height-len(lines))
	offset := max(0, min(m.Claims.DetailOffset, len(content)-visible))
	end := min(len(content), offset+visible)
	shown := content[offset:end]
	if end < len(content) && len(shown) > 0 {
		shown = append(append([]string(nil), shown[:len(shown)-1]...), m.s().faint.Render(fmt.Sprintf("  ↓ %d more lines (pgdn)", len(content)-end+1)))
	}
	for _, line := range shown {
		lines = append(lines, " "+line)
	}
	return lines
}

func (m Model) claimDetailContent(claim lease.ClaimView, width int) []string {
	plainStyle := lipgloss.NewStyle()
	var lines []dline
	add := func(label, text string, style lipgloss.Style) { lines = append(lines, dline{label, text, style}) }
	if m.Claims.DetailTab == 0 {
		add("Resources", strings.Join(claim.Resources, ", "), plainStyle)
		add("Holder", valueOr(claim.AgentID, "unknown"), plainStyle)
		add("Session", valueOr(claim.SessionID, "unknown"), plainStyle)
		add("Work key", valueOr(claim.WorkKey, "—"), plainStyle)
		add("Acquired", claim.AcquiredAt.UTC().Format(time.RFC3339), plainStyle)
		add("Expiry", claimExpiryLabel(claim, m.claimsNow())+" · "+claim.ExpiresAt.UTC().Format(time.RFC3339), m.claimExpiryStyle(claim))
		checkpoint := "not present"
		if claim.CheckpointPresent {
			checkpoint = "present (public status only)"
		}
		add("Checkpoint", checkpoint, plainStyle)
		if len(claim.UnknownOperations) > 0 {
			add("Progress", fmt.Sprintf("%d operation(s) require read-back", len(claim.UnknownOperations)), m.s().warn)
		}
		if item, ok := m.claimItem(claim); ok {
			add("Queue item", item.Title+" (press i to jump)", m.s().accent)
		}
		if m.Claims.Stale {
			add("Freshness", "stale · "+valueOr(m.Claims.Error, "refresh unavailable"), m.s().warnBold)
		}
	} else if m.Claims.PublicOnly {
		// The remote list exposes timestamps, but status/list do not expose
		// other sessions' lifecycle events or operation history.
		add("Acquired", claim.AcquiredAt.UTC().Format(time.RFC3339), plainStyle)
		add("Heartbeat", claim.HeartbeatAt.UTC().Format(time.RFC3339), plainStyle)
		add("Expiry", claim.ExpiresAt.UTC().Format(time.RFC3339), plainStyle)
		add("History", "Remote claim history is not shown", m.s().faint)
	} else {
		if m.Claims.Gap {
			add("Events", "history gap · older lifecycle events may have been pruned", m.s().warn)
		}
		if m.Claims.EventsError != "" {
			add("Events", "unavailable: "+m.Claims.EventsError, m.s().warn)
		}
		var events []ledger.Event
		for _, event := range m.Claims.Events {
			if event.ClaimID == claim.ClaimID {
				events = append(events, event)
			}
		}
		if len(events) > 10 {
			events = events[len(events)-10:]
		}
		if len(events) == 0 {
			add("Lifecycle", "No recent public lifecycle events for this claim.", m.s().faint)
		}
		for _, event := range events {
			add("", event.At.UTC().Format(time.RFC3339)+" · "+event.Kind, plainStyle)
		}
		if m.Claims.HistoryLoading {
			add("History", "Loading current claim epoch…", m.s().faint)
		}
		if m.Claims.HistoryError != "" {
			add("History", "unavailable: "+m.Claims.HistoryError, m.s().warn)
		}
		if m.Claims.History.Gap {
			add("History", "earlier public history was pruned", m.s().warn)
		}
		found := false
		for _, epoch := range m.Claims.History.Epochs {
			if epoch.ClaimID != claim.ClaimID {
				continue
			}
			found = true
			add("Epoch", epoch.Status+" · acquired "+epoch.AcquiredAt.UTC().Format(time.RFC3339), m.s().bold)
			for _, operation := range epoch.Operations {
				add("Progress", operation.Kind+" · "+operation.State, plainStyle)
			}
		}
		if !found && !m.Claims.HistoryLoading && m.Claims.HistoryError == "" && len(claim.Resources) > 0 {
			add("Epoch", "No current-claim epoch in retained resource history.", m.s().faint)
		}
	}
	return m.s().renderDetail(lines, max(1, width-1))
}

func (m Model) claimExpiryStyle(claim lease.ClaimView) lipgloss.Style {
	if m.Claims.Stale || !claim.Active {
		return m.s().warn
	}
	if claimExpiring(claim, m.claimsNow()) {
		return m.s().warnBold
	}
	return lipgloss.NewStyle()
}

func (m Model) claimItem(claim lease.ClaimView) (queue.Item, bool) {
	keys := make([]string, 0, len(m.Snapshot.Items))
	for key := range m.Snapshot.Items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		item := m.Snapshot.Items[key]
		for _, resource := range item.Resources {
			for _, claimResource := range claim.Resources {
				if resource == claimResource {
					return item, true
				}
			}
		}
	}
	return queue.Item{}, false
}

func (m Model) claimDetailMaxOffset(claim lease.ClaimView) int {
	_, width := m.paneWidths()
	if width == 0 {
		width = m.Width
	}
	content := m.claimDetailContent(claim, width)
	visible := max(1, m.frame(m.rows()).bodyHeight-2)
	return max(0, len(content)-visible)
}

func (m Model) claimDetailTabSpans(width int) []span {
	_, spans := m.s().tabBar([]tabItem{{label: "Summary"}, {label: "History"}}, m.Claims.DetailTab, width)
	return spans
}

func (m Model) claimItemJump() (Model, bool) {
	claim, ok := m.selectedClaim(m.claimRows())
	if !ok {
		m.Claims.Notice = "No claim selected"
		return m, false
	}
	item, ok := m.claimItem(claim)
	if !ok {
		m.Claims.Notice = "No loaded queue item matches this claim's resources"
		return m, false
	}
	for _, name := range m.Views {
		if name == ClaimsViewID || name == RecoveryViewID || !m.viewCountMatches(item, name) {
			continue
		}
		candidate := m
		candidate.ViewName = name
		for index, row := range candidate.rows() {
			if identity(row) == identity(item) {
				m.setView(name)
				m.Selected, m.Index, m.Detail = identity(row), index, true
				m.anchor(m.rows())
				return m, true
			}
		}
	}
	m.Claims.Notice = "Matching item is not visible in the loaded queue views; clear the queue filter"
	return m, false
}

func (m Model) claimFilterSummary() string {
	var filters []string
	if m.Claims.MineOnly {
		filters = append(filters, "mine")
	} else {
		filters = append(filters, "all")
	}
	if m.Claims.ResourcePrefix != "" {
		filters = append(filters, "resource "+m.Claims.ResourcePrefix)
	}
	if m.Claims.ExpiringOnly {
		filters = append(filters, "expiring")
	}
	if m.Claims.StaleOnly {
		filters = append(filters, "stale")
	}
	return strings.Join(filters, " · ")
}

func (m Model) claimsFooterLines() []string {
	shown := len(m.claimRows())
	status := "fresh"
	statusStyle := m.s().good
	switch {
	case m.Claims.Loading:
		status, statusStyle = "loading", m.s().faint
	case m.Claims.Stale:
		status, statusStyle = "stale", m.s().warnBold
	case m.Claims.Gap:
		status, statusStyle = "history gap", m.s().warn
	}
	left := []seg{{fmt.Sprintf("%d shown / %d claims", shown, len(m.Claims.Items)), m.s().bold}, {" · " + m.claimFilterSummary(), m.s().faint}}
	right := []seg{{"authority claims ", m.s().faint}, {status, statusStyle}}
	gap := m.Width - segsWidth(left) - segsWidth(right)
	line := renderSegs(left)
	if gap >= 2 {
		line += strings.Repeat(" ", gap) + renderSegs(right)
	} else {
		line = renderSegs([]seg{left[0], {" · ", m.s().faint}, right[1]})
	}
	lines := []string{line}
	if m.Notice != "" {
		lines = append(lines, m.s().noticeStyle(m.Notice).Render(clip(m.Notice, m.Width)))
	}
	if m.Filtering || m.Palette {
		prompt := "/"
		if m.Palette {
			prompt = ":"
		}
		lines = append(lines, m.s().key.Render(prompt)+clip(m.Input, m.Width-3)+m.s().selected.Render(" "))
	}
	return append(lines, m.keyBar())
}
