package queueui

import (
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/queue"
	tea "github.com/charmbracelet/bubbletea"
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

// claimColumns lays out the Claims table. A claim on a loaded queue item
// reads as that item's ID and title; other claims show a short resource.
// Expires carries the lease state: a countdown, bold when expiring soon,
// or "expired".
func (m Model) claimColumns(rows, visible []lease.ClaimView, items map[string]queue.Item, width int) []column[lease.ClaimView] {
	now := m.claimsNow()
	idWidth := 0
	for _, claim := range rows {
		if item, ok := items[claim.ClaimID]; ok {
			idWidth = max(idWidth, len(item.Ref.ItemID))
		}
	}
	if idWidth > 0 {
		idWidth = max(2, min(idWidth, 16)) // at least the header's width
	}
	cols := []column[lease.ClaimView]{
		{title: "ID", min: idWidth, max: idWidth, cell: func(c lease.ClaimView) (string, lipgloss.Style) {
			return items[c.ClaimID].Ref.ItemID, m.s().accent
		}},
		{title: "Claimed", min: 16, flex: true, cell: func(c lease.ClaimView) (string, lipgloss.Style) {
			label := shortResources(c.Resources)
			style := lipgloss.NewStyle()
			if item, ok := items[c.ClaimID]; ok {
				label = item.Title
			}
			if m.claimActionEligible(c) {
				return "* " + label, m.s().ready
			}
			return label, style
		}},
		{title: "Holder", min: 8, max: 28, cell: func(c lease.ClaimView) (string, lipgloss.Style) {
			if m.claimIsMine(c) {
				return "mine", m.s().ready
			}
			return valueOr(clean(c.AgentID), "unknown"), lipgloss.NewStyle()
		}},
		{title: "Expires", min: 7, max: 9, cell: func(c lease.ClaimView) (string, lipgloss.Style) {
			switch {
			case c.ExpiresAt.IsZero():
				return "\u2014", m.s().faint
			case !c.Active || !c.ExpiresAt.After(now):
				return "expired", m.s().faint
			}
			return shortDuration(c.ExpiresAt.Sub(now)), m.claimExpiryStyle(c)
		}},
	}
	if idWidth == 0 {
		cols = cols[1:]
	}
	fitContent(cols, cellWidths(cols, visible))
	return fitColumns(cols, width-2, []string{"Holder"})
}

func (m Model) claimListLines(rows []lease.ClaimView, width, height int) []string {
	if height <= 0 {
		return nil
	}
	width = max(1, width)
	capacity := max(0, height-1)
	start := max(0, min(m.Claims.Offset, len(rows)-capacity))
	visible := rows[start:min(len(rows), start+capacity)]
	items := m.claimItems(rows)
	cols := m.claimColumns(rows, visible, items, width)
	lines := []string{tableHeader(m.s(), cols)}
	now := m.claimsNow()
	for index, claim := range visible {
		muted := m.Claims.Stale || !claim.Active || !claim.ExpiresAt.After(now)
		lines = append(lines, tableRow(m.s(), cols, claim, index+start == m.Claims.Index, muted, width))
	}
	if len(rows) == 0 {
		message, hints := "No claims match these filters.", []string{"esc clears filters"}
		switch {
		case len(m.Claims.Items) == 0 && m.Claims.Error != "":
			message, hints = "Claims unavailable.", nil
		case len(m.Claims.Items) == 0:
			// First-run guidance: the footer carries no key hints.
			message, hints = "No active claims on this authority.", []string{"Start with worklease acquire --path README.md", "Press ? for help · q to quit"}
			if m.Claims.Loading {
				message = "Loading authority claims…"
			}
		}
		lines = append(lines, "", "  "+m.s().bold.Render(clip(message, width-2)))
		for _, hint := range hints {
			lines = append(lines, "  "+m.s().faint.Render(clip(hint, width-2)))
		}
	}
	return lines
}

func (m Model) claimsActionPreviewView(preview ClaimsActionPreview) dialogBox {
	claim := preview.Claim
	var body strings.Builder
	if m.Authority != "" {
		fmt.Fprintf(&body, "Authority %s · ID %s\n", clean(m.Authority), clean(claim.AuthorityID))
	} else {
		fmt.Fprintf(&body, "Authority %s\n", clean(claim.AuthorityID))
	}
	fmt.Fprintf(&body, "Claim ID %s\n", clean(claim.ClaimID))
	for _, resource := range claim.Resources {
		fmt.Fprintf(&body, "Resource %s\n", displayResource(resource))
	}
	fmt.Fprintf(&body, "Handle %s · %s\nCurrent expiry %s\n", clean(preview.Handle.Kind), clean(preview.Handle.Path), claimExpiryTimestamp(claim.ExpiresAt))
	if preview.Action == ClaimsActionRelease {
		fmt.Fprintf(&body, "Reason %s\nEffect resources become free and the private handle is removed.\n", clean(preview.Reason))
		return m.dialog("Confirm claim release", strings.TrimRight(body.String(), "\n"), confirmKeys)
	}
	if m.Claims.RenewTTL > 0 {
		projected := m.claimsNow().Add(m.Claims.RenewTTL)
		fmt.Fprintf(&body, "Effect renew with default TTL %s; projected expiry around %s.\n", m.Claims.RenewTTL.Round(time.Second), projected.UTC().Format(time.RFC3339))
	} else {
		body.WriteString("Effect renew with the default TTL; projected expiry unavailable because the TTL was not provided.\n")
	}
	return m.dialog("Confirm claim renewal", strings.TrimRight(body.String(), "\n"), confirmKeys)
}

func (m Model) claimStateLabel(claim lease.ClaimView, now time.Time) string {
	switch {
	case m.Claims.Stale:
		return "stale"
	case !claim.Active || !claim.ExpiresAt.IsZero() && !claim.ExpiresAt.After(now):
		return "expired"
	case claimExpiring(claim, now):
		return "expiring"
	default:
		return "active"
	}
}

// shortDuration is a compact remaining time: seconds under a minute, then
// minutes and seconds while it matters, then coarser units.
func shortDuration(d time.Duration) string {
	d = max(0, d).Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < 10*time.Minute:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// expiresIn describes a lease's remaining time for prose.
func expiresIn(remaining time.Duration) string {
	if remaining <= 0 {
		return "expired"
	}
	return "expires in " + shortDuration(remaining)
}

func claimExpiryLabel(claim lease.ClaimView, now time.Time) string {
	if claim.ExpiresAt.IsZero() {
		return "expiry unknown"
	}
	if !claim.Active || !claim.ExpiresAt.After(now) {
		return "ended " + ago(now.Sub(claim.ExpiresAt))
	}
	return expiresIn(claim.ExpiresAt.Sub(now))
}

// when renders a timestamp as relative age followed by the exact time.
func when(at, now time.Time) string {
	if at.IsZero() {
		return "\u2014"
	}
	return ago(now.Sub(at)) + " \u00b7 " + at.UTC().Format(time.RFC3339)
}

// shortResource abbreviates a resource key for tables: a path claim shows
// its repository-relative path, a host-local item key its repository and
// item, and an opaque coordination key its hash prefix. Details always
// show the full key.
func shortResource(resource string) string {
	parts := strings.SplitN(resource, ":", 3)
	fields := strings.Split(resource, ":")
	switch {
	case len(parts) == 3 && parts[0] == "path":
		return displayResource(parts[2])
	case len(parts) == 3 && parts[0] == "coordination" && len(parts[2]) > 12:
		return displayResource(parts[0] + ":" + parts[1] + ":" + parts[2][:8] + "…")
	case len(fields) >= 4 && (fields[0] == "backlog-md" || fields[0] == "markdown"):
		// PROVIDER:GIT-COMMON-DIR:LOCATOR:ITEM
		item := fields[len(fields)-1]
		if item == "__source__" {
			item = fields[len(fields)-2]
		}
		return repositoryName(strings.Join(fields[1:len(fields)-2], ":")) + " " + displayResource(item)
	}
	return displayResource(resource)
}

// repositoryName names a checkout by its Git common directory.
func repositoryName(commonDir string) string {
	commonDir = strings.TrimSuffix(displayResource(commonDir), "/")
	if path.Base(commonDir) == ".git" {
		commonDir = path.Dir(commonDir)
	}
	return strings.TrimSuffix(path.Base(commonDir), ".git")
}

func shortResources(resources []string) string {
	if len(resources) == 0 {
		return "\u2014"
	}
	text := shortResource(resources[0])
	if len(resources) > 1 {
		text += fmt.Sprintf(" +%d", len(resources)-1)
	}
	return text
}

func (m Model) claimDetailPane(claim lease.ClaimView, width, height int) []string {
	var title string
	var actions []binding
	if item, ok := m.claimItem(claim); ok {
		id := clip(item.Ref.ItemID, 30)
		title = m.s().accent.Bold(true).Render(id) + " " + m.s().bold.Render(clip(item.Title, max(1, width-len(id)-2)))
		actions = append(actions, binding{"i", "view item"})
	} else {
		title = m.s().bold.Render(clip(shortResources(claim.Resources), width-1))
	}
	if m.claimActionEligible(claim) && m.Claims.Mutate != nil {
		actions = append(actions, binding{"u", "renew"}, binding{"R", "release"})
	}
	tabs, _ := m.s().tabBar(claimDetailTabs, m.Claims.DetailTab, width-1)
	lines := []string{" " + title, " " + m.s().actionLine(actions, width-2), " " + tabs}
	return append(lines, m.s().scrollWindow(m.claimDetailContent(claim, width), m.Claims.DetailOffset, height-len(lines))...)
}

var claimDetailTabs = []tabItem{{label: "Summary"}, {label: "History"}}

func (m Model) claimDetailContent(claim lease.ClaimView, width int) []string {
	plainStyle := lipgloss.NewStyle()
	now := m.claimsNow()
	var lines []dline
	add := func(label, text string, style lipgloss.Style) { lines = append(lines, dline{label, text, style}) }
	if m.Claims.DetailTab == 0 {
		add("State", m.claimStateLabel(claim, now)+" \u00b7 "+claimExpiryLabel(claim, now), m.claimExpiryStyle(claim))
		holder := valueOr(clean(claim.AgentID), "unknown")
		if m.claimIsMine(claim) {
			holder += " (mine)"
		}
		add("Holder", holder, plainStyle)
		if item, ok := m.claimItem(claim); ok {
			add("Item", item.Ref.String(), m.s().accent)
		}
		add("Acquired", when(claim.AcquiredAt, now), plainStyle)
		add("Heartbeat", when(claim.HeartbeatAt, now), plainStyle)
		checkpoint := "none"
		if claim.CheckpointPresent {
			checkpoint = "present"
		}
		add("Checkpoint", checkpoint, plainStyle)
		if len(claim.UnknownOperations) > 0 {
			add("Progress", fmt.Sprintf("%d operation(s) need read-back", len(claim.UnknownOperations)), m.s().warn)
		}
		if m.Claims.Stale {
			add("Freshness", "stale \u00b7 "+valueOr(m.Claims.Error, "refresh unavailable"), m.s().warnBold)
		}
		handle := m.claimHandle(claim)
		handleLabel := strings.TrimSpace(strings.Join([]string{handle.Kind, handle.Path}, " · "))
		if handleLabel == "" {
			handleLabel = "not found"
		}
		add("Handle", handleLabel, m.s().faint)
		unavailable := m.claimActionUnavailable(claim)
		if unavailable == "" {
			unavailable = "u renew · R release"
		}
		add("Actions", unavailable, m.claimExpiryStyle(claim))
		// Exact identifiers for CLI use, after the facts people scan for.
		add("", "", plainStyle)
		add("Session", valueOr(claim.SessionID, "unknown"), m.s().faint)
		for n, resource := range claim.Resources {
			label := " "
			if n == 0 {
				label = "Resources"
			}
			add(label, displayResource(resource), m.s().faint)
		}
		if claim.WorkKey != "" && !slices.Equal(claim.Resources, []string{claim.WorkKey}) {
			add("Work key", claim.WorkKey, m.s().faint)
		}
		add("Claim ID", claim.ClaimID, m.s().faint)
	} else if m.Claims.PublicOnly {
		// The remote list exposes timestamps, but status/list do not expose
		// other sessions' lifecycle events or operation history.
		add("Acquired", when(claim.AcquiredAt, now), plainStyle)
		add("Heartbeat", when(claim.HeartbeatAt, now), plainStyle)
		add("Expires", claim.ExpiresAt.UTC().Format(time.RFC3339), plainStyle)
		add("", "", plainStyle)
		add("", "Remote authorities do not share claim history.", m.s().faint)
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
		for n, event := range events {
			label := " "
			if n == 0 {
				label = "Lifecycle"
			}
			add(label, fmt.Sprintf("%-10s %s", event.Kind, ago(now.Sub(event.At))), plainStyle)
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
			add("", "", plainStyle)
			add("Epoch", epoch.Status+" · acquired "+ago(now.Sub(epoch.AcquiredAt)), m.s().bold)
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
	if m.Claims.Stale {
		return m.s().warn
	}
	if !claim.Active {
		return m.s().faint
	}
	if claimExpiring(claim, m.claimsNow()) {
		return m.s().warnBold
	}
	return lipgloss.NewStyle()
}

// claimItems maps each claim to the loaded queue item sharing one of its
// resources, scanning the snapshot once. Ties go to the lowest item key.
func (m Model) claimItems(claims []lease.ClaimView) map[string]queue.Item {
	wanted := map[string]bool{}
	for _, claim := range claims {
		for _, resource := range claim.Resources {
			wanted[resource] = true
		}
	}
	byResource := map[string]string{}
	for key, item := range m.Snapshot.Items {
		for _, resource := range item.Resources {
			if current, ok := byResource[resource]; wanted[resource] && (!ok || key < current) {
				byResource[resource] = key
			}
		}
	}
	items := map[string]queue.Item{}
	for _, claim := range claims {
		var keys []string
		for _, resource := range claim.Resources {
			if key, ok := byResource[resource]; ok {
				keys = append(keys, key)
			}
		}
		if len(keys) > 0 {
			sort.Strings(keys)
			items[claim.ClaimID] = m.Snapshot.Items[keys[0]]
		}
	}
	return items
}

func (m Model) claimItem(claim lease.ClaimView) (queue.Item, bool) {
	item, ok := m.claimItems([]lease.ClaimView{claim})[claim.ClaimID]
	return item, ok
}

// itemClaim returns the loaded authority claim on an item's resources,
// preferring an active one.
func (m Model) itemClaim(item queue.Item) (lease.ClaimView, bool) {
	var found lease.ClaimView
	ok := false
	for _, claim := range m.Claims.Items {
		if !slices.ContainsFunc(claim.Resources, func(r string) bool { return slices.Contains(item.Resources, r) }) {
			continue
		}
		if !ok || claim.Active && !found.Active {
			found, ok = claim, true
		}
	}
	return found, ok
}

func (m Model) claimDetailMaxOffset(claim lease.ClaimView) int {
	_, width := m.paneWidths()
	if width == 0 {
		width = m.Width
	}
	content := m.claimDetailContent(claim, width)
	visible := max(1, m.frame(m.rows()).bodyHeight-detailChrome)
	return max(0, len(content)-visible)
}

func (m Model) claimDetailTabSpans(width int) []span {
	_, spans := m.s().tabBar(claimDetailTabs, m.Claims.DetailTab, width)
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
				m.Selected, m.Index = identity(row), index
				m.openDetail()
				m.anchor(m.rows())
				return m, true
			}
		}
	}
	m.Claims.Notice = "Matching item is not visible in the loaded queue views; clear the queue filter"
	return m, false
}

// itemClaimJump shows the selected item's claim in the Claims tab, the
// reverse of i on a claim. A claim the list has not caught up with yet is
// found by filtering on the item's resource while the tab refreshes.
func (m Model) itemClaimJump() (tea.Model, tea.Cmd) {
	item, ok := m.selected(m.rows())
	if !ok || !slices.Contains(m.Views, ClaimsViewID) || len(item.Resources) == 0 {
		m.Notice = "No claim to show"
		return m, nil
	}
	claim, found := m.itemClaim(item)
	if !found && !item.Claim.Active {
		m.Notice = "No current claim on " + clean(item.Ref.ItemID)
		return m, nil
	}
	cmd := m.setView(ClaimsViewID)
	m.Claims.MineOnly, m.Claims.ExpiringOnly, m.Claims.StaleOnly = false, false, false
	m.Claims.ResourcePrefix = ""
	if !found {
		m.Claims.ResourcePrefix = item.Resources[0]
		m.Claims.Detail = false
		m.anchorClaims(m.claimRows(), max(1, m.frame(m.rows()).bodyHeight-1))
		return m, cmd
	}
	m.Claims.Selected = claim.ClaimID
	m.Claims.Detail, m.Claims.DetailTab, m.Claims.DetailOffset = true, 0, 0
	rows := m.claimRows()
	m.anchorClaims(rows, max(1, m.frame(m.rows()).bodyHeight-1))
	return m, tea.Batch(cmd, m.loadSelectedClaimHistory(rows))
}

// claimFilters names the active Claims filters.
func (m Model) claimFilters() []string {
	var filters []string
	if m.Claims.MineOnly {
		filters = append(filters, "mine")
	}
	if m.Claims.ResourcePrefix != "" {
		filters = append(filters, "resource "+shortResource(m.Claims.ResourcePrefix))
	}
	if m.Claims.ExpiringOnly {
		filters = append(filters, "expiring")
	}
	if m.Claims.StaleOnly {
		filters = append(filters, "stale")
	}
	return filters
}

func (m Model) claimsStatus() (left, right []seg) {
	if filters := m.claimFilters(); len(filters) > 0 {
		left = []seg{{fmt.Sprintf("%d of %d claims", len(m.claimRows()), len(m.Claims.Items)), m.s().bold}, {" · " + strings.Join(filters, " · "), m.s().faint}}
	}
	if m.Claims.Gap {
		right = []seg{{"event history gap", m.s().warn}}
	}
	return left, right
}
