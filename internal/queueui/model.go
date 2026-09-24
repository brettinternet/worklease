package queueui

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/queue"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// SnapshotMsg publishes immutable source state without changing the focused pane.
type SnapshotMsg struct{ Snapshot queue.Snapshot }
type ClaimOverlayMsg struct {
	Snapshot   queue.Snapshot
	Rebuilding bool
	Err        error
}
type HistoryMsg struct {
	Identity string
	Page     ledger.HistoryPage
	Before   bool
	Err      error
}
type CommentsMsg struct {
	Identity string
	Comments []queue.GitHubComment
	Cursor   string
	Err      error
}
type RefreshedMsg struct{ Err error }

type ViewRule struct {
	Readiness, Claim string
	Assigned         []string
}
type Model struct {
	Snapshot                       queue.Snapshot
	Views                          []string
	ViewFilters                    map[string]queue.Filters
	ViewRules                      map[string]ViewRule
	ViewName, Authority, Scope, Me string
	MeBySource                     map[string][]string
	Sources                        []queue.Source
	SourceErrors                   map[string]string
	Width, Height                  int
	Selected                       string
	Index, Offset                  int
	DetailOffset                   int
	Detail                         bool
	Tab                            int
	Filter, Input, Notice          string
	Filtering, Palette, Help       bool
	History                        ledger.HistoryPage
	HistoryError                   string
	HistoryLoading                 bool
	HistoryIdentity                string
	HistoryCursor                  string
	Comments                       []queue.GitHubComment
	CommentsCursor                 string
	CommentsIdentity               string
	CommentsLoading                bool
	CommentsError                  string
	ClaimFreshness                 string
	RebuildingClaims               bool
	Refresh                        func() tea.Cmd
	HydrateSelected                func(queue.Item) tea.Cmd
	LoadHistory                    func(queue.Item, string, bool) tea.Cmd
	LoadComments                   func(queue.Item, string) tea.Cmd
	OpenURL                        func(queue.Item) tea.Cmd
	rowCache                       *rowCache
}

type rowCache struct {
	mu       sync.Mutex
	key      string
	rows     []queue.Item
	counts   map[string]int
	observed time.Time
	edges    int
}

var tabs = []string{"Summary", "Dependencies", "Activity", "Claims"}

func New(snapshot queue.Snapshot) Model {
	return Model{Snapshot: snapshot.Clone(), Width: 120, Height: 35, Views: []string{"All", "Ready", "Mine", "Claimed"}, ViewName: "All", rowCache: &rowCache{}}
}
func (m Model) Init() tea.Cmd { return nil }
func identity(i queue.Item) string {
	if i.CanonicalID != "" {
		return i.CanonicalID
	}
	return i.Ref.Key()
}
func (m Model) rows() []queue.Item {
	// The TUI event loop owns the model. Cache the sorted projection across
	// navigation/paint; provider, claim, filter and view changes invalidate it.
	key := fmt.Sprintf("%x:%d:%s:%s:%s:%v:%v:%v:%v", reflect.ValueOf(m.Snapshot.Items).Pointer(), m.Snapshot.Revision, m.ViewName, m.Filter, m.Me, m.Sources, m.Views, m.ViewFilters, m.ViewRules)
	if m.rowCache != nil {
		m.rowCache.mu.Lock()
		defer m.rowCache.mu.Unlock()
		if m.rowCache.rows != nil && m.rowCache.key == key {
			return m.rowCache.rows
		}
	}
	order := make([]string, 0, len(m.Sources))
	for _, s := range m.Sources {
		order = append(order, s.ID)
	}
	if len(order) == 0 {
		for id := range m.Snapshot.Sources {
			order = append(order, id)
		}
		if len(order) == 0 {
			for _, item := range m.Snapshot.Items {
				order = append(order, item.Ref.SourceID)
			}
		}
	}
	filters := m.ViewFilters[m.ViewName]
	filters.Text = m.Filter
	rows := queue.EvaluateView(m.Snapshot.Items, queue.View{SourceOrder: order, Filters: filters})
	out := rows[:0]
	rule := m.ViewRules[m.ViewName]
	for _, i := range rows {
		if rule.Readiness != "" && rule.Readiness != "all" && string(i.Readiness.Status) != rule.Readiness {
			continue
		}
		if rule.Claim != "" && rule.Claim != "all" && !matchesClaim(i, rule.Claim) {
			continue
		}
		if len(rule.Assigned) > 0 {
			match := false
			for _, kind := range rule.Assigned {
				switch kind {
				case "me":
					for _, name := range i.AssignedTo {
						if m.isMe(i, name) {
							match = true
						}
					}
				case "nobody":
					if len(i.AssignedTo) == 0 {
						match = true
					}
				default:
					for _, name := range i.AssignedTo {
						if name == kind {
							match = true
						}
					}
				}
			}
			if !match {
				continue
			}
		}
		// Built-in shortcuts apply only to the model's default views. A
		// configured view with the same name obeys its explicit rules.
		if _, configured := m.ViewRules[m.ViewName]; !configured {
			switch m.ViewName {
			case "Ready":
				if i.Readiness.Status != queue.Ready {
					continue
				}
			case "Mine":
				found := false
				for _, name := range i.AssignedTo {
					if m.isMe(i, name) {
						found = true
					}
				}
				if !found {
					continue
				}
			case "Claimed":
				if !i.Claim.Active {
					continue
				}
			}
		}
		out = append(out, i)
	}
	if m.rowCache != nil {
		m.rowCache.key, m.rowCache.rows = key, out
		m.rowCache.counts = make(map[string]int, len(m.Views))
		for _, name := range m.Views {
			m.rowCache.counts[name] = m.viewCount(name)
		}
		m.rowCache.observed = time.Time{}
		m.rowCache.edges = 0
		for _, item := range m.Snapshot.Items {
			if item.Observation.ObservedAt.After(m.rowCache.observed) {
				m.rowCache.observed = item.Observation.ObservedAt
			}
			if item.DependenciesKnown && item.Closure == queue.CoverageComplete && item.Fresh {
				m.rowCache.edges++
			}
		}
	}
	return out
}
func (m Model) selected(rows []queue.Item) (queue.Item, bool) {
	for _, i := range rows {
		if identity(i) == m.Selected {
			return i, true
		}
	}
	return queue.Item{}, false
}
func (m *Model) anchor(rows []queue.Item) {
	if len(rows) == 0 {
		m.Selected = ""
		m.Index = 0
		m.Offset = 0
		return
	}
	found := false
	for j, i := range rows {
		if identity(i) == m.Selected {
			m.Index = j
			found = true
			break
		}
	}
	if !found && m.Index >= len(rows) {
		m.Index = len(rows) - 1
	}
	if m.Index < 0 {
		m.Index = 0
	}
	m.Selected = identity(rows[m.Index])
	if m.Offset > m.Index {
		m.Offset = m.Index
	}
	if m.Offset < 0 {
		m.Offset = 0
	}
}
func (m *Model) move(delta int) {
	previous := m.Selected
	rows := m.rows()
	m.anchor(rows)
	if len(rows) == 0 {
		return
	}
	m.Index += delta
	if m.Index < 0 {
		m.Index = 0
	}
	if m.Index >= len(rows) {
		m.Index = len(rows) - 1
	}
	m.Selected = identity(rows[m.Index])
	maxRows := m.Height - 8
	if maxRows < 1 {
		maxRows = 1
	}
	if m.Index < m.Offset {
		m.Offset = m.Index
	}
	if m.Index >= m.Offset+maxRows {
		m.Offset = m.Index - maxRows + 1
	}
	if m.Selected != previous {
		m.clearHistory()
	}
}
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width, m.Height = v.Width, v.Height
	case SnapshotMsg:
		previous := m.Selected
		if v.Snapshot.Revision < m.Snapshot.Revision {
			break
		}
		updated := v.Snapshot.Clone()
		for key, item := range updated.Items {
			if prior, ok := m.Snapshot.Items[key]; ok && len(prior.Resources) > 0 && item.Ref == prior.Ref {
				if !prior.Claim.ObservedAt.Before(item.Claim.ObservedAt) {
					item.Claim, item.Resources, item.KeyInputs = prior.Claim, prior.Resources, prior.KeyInputs
				}
				updated.Items[key] = item
			}
		}
		m.Snapshot = updated
		m.anchor(m.rows())
		if m.Selected != previous {
			m.clearHistory()
		}
		if m.Detail && m.Tab == 2 && m.Selected != previous && m.LoadComments != nil {
			if item, ok := m.selected(m.rows()); ok {
				m.Comments, m.CommentsCursor, m.CommentsError = nil, "", ""
				m.CommentsIdentity = identity(item)
				m.CommentsLoading = true
				return m, m.LoadComments(item, "")
			}
		}
		if m.Selected != previous && m.HydrateSelected != nil {
			if item, ok := m.selected(m.rows()); ok {
				return m, m.HydrateSelected(item)
			}
		}
	case CommentsMsg:
		if v.Identity == m.Selected {
			m.CommentsLoading = false
			if v.Err != nil {
				m.CommentsError = v.Err.Error()
			} else {
				m.CommentsError = ""
				m.Comments = append(m.Comments, v.Comments...)
				m.CommentsCursor = v.Cursor
				m.CommentsIdentity = v.Identity
			}
		}
	case ClaimOverlayMsg:
		m.rowCache = &rowCache{}
		for key, item := range v.Snapshot.Items {
			if current, ok := m.Snapshot.Items[key]; ok && !item.Claim.ObservedAt.Before(current.Claim.ObservedAt) {
				current.Claim, current.Resources, current.KeyInputs = item.Claim, item.Resources, item.KeyInputs
				m.Snapshot.Items[key] = current
			}
		}
		m.RebuildingClaims = v.Rebuilding
		switch {
		case v.Rebuilding:
			m.ClaimFreshness = "rebuilding"
		case v.Err != nil:
			m.ClaimFreshness = "stale"
			m.Notice = "Claim overlay unavailable: " + v.Err.Error()
		default:
			m.ClaimFreshness = "fresh"
		}
		m.anchor(m.rows())
	case HistoryMsg:
		if v.Identity == m.Selected {
			m.HistoryLoading = false
			m.HistoryError = ""
			if v.Err != nil {
				m.HistoryError = v.Err.Error()
			} else {
				if v.Before && m.HistoryCursor != "" && m.HistoryIdentity == v.Identity {
					m.History.Epochs = append(append([]ledger.Epoch(nil), v.Page.Epochs...), m.History.Epochs...)
					m.History.PreviousCursor = v.Page.PreviousCursor
					m.History.Gap = m.History.Gap || v.Page.Gap
				} else if m.HistoryCursor != "" && m.HistoryIdentity == v.Identity {
					m.History.Epochs = append(m.History.Epochs, v.Page.Epochs...)
					m.History.NextCursor = v.Page.NextCursor
					m.History.Gap = m.History.Gap || v.Page.Gap
				} else {
					m.History = v.Page
				}
				m.HistoryCursor = ""
				m.HistoryIdentity = v.Identity
			}
		}
	case RefreshedMsg:
		if v.Err != nil {
			m.Notice = "Refresh failed: " + v.Err.Error()
		} else {
			m.Notice = "Refreshed"
		}
	case tea.KeyMsg:
		key := v.String()
		if m.Filtering || m.Palette {
			switch key {
			case "esc":
				m.Filtering = false
				m.Palette = false
				m.Input = ""
			case "enter":
				if m.Filtering {
					m.Filter = m.Input
					m.anchor(m.rows())
				} else {
					m.Notice = "Command unavailable in read-only slice: " + m.Input
				}
				m.Filtering = false
				m.Palette = false
				m.Input = ""
			case "backspace":
				r := []rune(m.Input)
				if len(r) > 0 {
					m.Input = string(r[:len(r)-1])
				}
			default:
				if len(key) == 1 && key != "\x1b" {
					m.Input += key
				}
			}
			return m, nil
		}
		rows := m.rows()
		previous := m.Selected
		switch key {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			m.move(1)
		case "k", "up":
			m.move(-1)
		case "pgdown", "ctrl+d":
			m.DetailOffset += max(1, m.Height/2)
		case "pgup", "ctrl+u":
			m.DetailOffset = max(0, m.DetailOffset-max(1, m.Height/2))
		case "g":
			if m.Notice == "g" {
				m.move(-len(rows))
				m.Notice = ""
			} else {
				m.Notice = "g"
			}
		case "G":
			m.move(len(rows))
		case "v":
			for n, name := range m.Views {
				if name == m.ViewName {
					m.ViewName = m.Views[(n+1)%len(m.Views)]
					m.anchor(m.rows())
					break
				}
			}
		case "l", "right", "enter":
			if len(rows) > 0 {
				m.Detail = true
			}
		case "h", "left", "esc":
			if m.Help {
				m.Help = false
			} else {
				m.Detail = false
			}
		case "tab":
			if m.Detail {
				m.Tab = (m.Tab + 1) % len(tabs)
				m.DetailOffset = 0
			} else {
				m.Detail = true
			}
		case "shift+tab":
			if m.Detail {
				m.Tab = (m.Tab + len(tabs) - 1) % len(tabs)
				m.DetailOffset = 0
			}
		case "/":
			m.Filtering = true
			m.Input = m.Filter
		case "n":
			m.move(1)
		case "N":
			m.move(-1)
		case "m":
			// n/N stay row navigation; m pages detail activity (comments or claim epochs).
			if m.Detail && m.Tab == 2 && !m.CommentsLoading && m.CommentsCursor != "" && m.LoadComments != nil {
				if i, ok := m.selected(rows); ok {
					m.CommentsLoading = true
					return m, m.LoadComments(i, m.CommentsCursor)
				}
			}
			if m.Detail && m.Tab == 3 && !m.HistoryLoading && m.History.PreviousCursor != "" && m.LoadHistory != nil {
				if i, ok := m.selected(rows); ok && len(i.Resources) == 1 {
					m.HistoryLoading = true
					m.HistoryCursor = m.History.PreviousCursor
					return m, m.LoadHistory(i, m.HistoryCursor, true)
				}
			}
		case ":":
			m.Palette = true
			m.Input = ""
		case "?":
			m.Help = !m.Help
		case "r":
			if m.Refresh != nil {
				return m, m.Refresh()
			}
			m.Notice = "Refresh unavailable"
		case "o":
			if i, ok := m.selected(rows); ok && m.OpenURL != nil {
				return m, m.OpenURL(i)
			}
			m.Notice = "Provider URL unavailable"
		case "c", "R", "a", "s", "p", "x":
			m.Notice = "Unavailable: read-only slice (claim, release, assign, state, progress and launch arrive later)"
		}
		if m.Detail && m.Tab == 2 && m.LoadComments != nil {
			if i, ok := m.selected(m.rows()); ok && m.CommentsIdentity != identity(i) && !m.CommentsLoading {
				m.Comments, m.CommentsCursor, m.CommentsError = nil, "", ""
				m.CommentsIdentity = identity(i)
				m.CommentsLoading = true
				return m, m.LoadComments(i, "")
			}
		}
		if m.Detail && m.Tab == 3 {
			if i, ok := m.selected(m.rows()); ok && len(i.Resources) == 1 && m.HistoryIdentity != identity(i) && m.LoadHistory != nil {
				m.HistoryLoading = true
				m.HistoryIdentity = identity(i)
				m.HistoryCursor = ""
				m.History = ledger.HistoryPage{}
				if m.Selected != previous && m.HydrateSelected != nil {
					return m, tea.Batch(m.LoadHistory(i, "", false), m.HydrateSelected(i))
				}
				return m, m.LoadHistory(i, "", false)
			}
		}
		if m.Selected != previous && m.HydrateSelected != nil {
			if item, ok := m.selected(m.rows()); ok {
				return m, m.HydrateSelected(item)
			}
		}
	}
	return m, nil
}
func clean(s string) string {
	s = ansi.Strip(s)
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\t' {
			b.WriteRune(' ')
		} else if !unicode.IsControl(r) && r != 0x7f {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func clip(s string, n int) string {
	if n < 1 {
		return ""
	}
	return ansi.Truncate(clean(s), n, "…")
}
func (m Model) View() string {
	if m.Width < 30 {
		return "Resize terminal to at least 30 columns\n"
	}
	rows := m.rows()
	m.anchor(rows)
	var b, list strings.Builder
	age := "unknown"
	if m.rowCache != nil && !m.rowCache.observed.IsZero() {
		age = time.Since(m.rowCache.observed).Round(time.Second).String()
	}
	fmt.Fprintf(&b, "worklease queue  view: %s  authority: %s (%s)  me: %s  sources %d/%d  sync %s ago\n", clip(m.ViewName, 24), clip(m.Authority, 48), clip(m.Scope, 12), clip(m.Me, 24), healthy(m.Snapshot.Sources), len(m.Sources), age)
	if m.Help {
		b.WriteString("j/k/arrows move · gg/G top/bottom · h/l/Tab/Enter/Esc panes · / filter · n/N matches · PgUp/PgDn detail · r refresh · : palette · o open URL · q quit\n")
	}
	listWidth := m.Width
	if m.Width >= 100 {
		listWidth = m.Width * 2 / 3
	}
	if !m.Detail || m.Width >= 100 {
		fmt.Fprintf(&list, "Views: ")
		for _, v := range m.Views {
			fmt.Fprintf(&list, "%s %d  ", clip(v, 16), m.rowCache.counts[v])
		}
		list.WriteByte('\n')
		list.WriteString("Sources: ")
		for _, s := range m.Sources {
			c := m.Snapshot.Sources[s.ID]
			status := sourceState(c)
			if problem := m.SourceErrors[s.ID]; problem != "" {
				status = problem
			}
			fmt.Fprintf(&list, "%s %s  ", clip(s.Name, 16), clip(status, 32))
		}
		list.WriteByte('\n')
		list.WriteString(ansi.Wrap("ID     Title        State   Eligibility        Assigned Native  Worklease", listWidth, " ") + "\n")
		maxRows := m.Height - 8
		if maxRows < 1 {
			maxRows = 1
		}
		if m.Index < m.Offset {
			m.Offset = m.Index
		}
		if m.Index >= m.Offset+maxRows {
			m.Offset = m.Index - maxRows + 1
		}
		for _, i := range rows[m.Offset:min(len(rows), m.Offset+maxRows)] {
			marker := " "
			if identity(i) == m.Selected {
				marker = ">"
			}
			line := fmt.Sprintf("%s %-5s %-12s %-7s %-20s %-8s %-7s %s", marker, clip(i.Ref.ItemID, 5), clip(i.Title, 12), clip(i.RawStatus, 7), m.displayState(i), clip(strings.Join(i.AssignedTo, ","), 8), clip(i.NativeClaim, 7), claimState(i))
			list.WriteString(ansi.Wrap(clean(line), listWidth, " "))
			list.WriteByte('\n')
		}
	}
	if m.Detail && m.Width >= 100 {
		if i, ok := m.selected(rows); ok {
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(listWidth).Render(list.String()), lipgloss.NewStyle().Width(m.Width-listWidth-1).Render(clipLines(scrollDetail(detail(m, i), m.DetailOffset), m.Width-listWidth-1))))
		} else {
			b.WriteString(list.String())
		}
	} else if m.Detail {
		if i, ok := m.selected(rows); ok {
			b.WriteString(clipLines(scrollDetail(detail(m, i), m.DetailOffset), m.Width))
		} else {
			b.WriteString("No item selected\n")
		}
	} else {
		b.WriteString(list.String())
	}
	total, accuracy := 0, "exact"
	edges := 0
	if m.rowCache != nil {
		edges = m.rowCache.edges
	}
	if len(m.Snapshot.Sources) == 0 || len(m.SourceErrors) > 0 {
		accuracy = "unknown"
	}
	for _, source := range m.Sources {
		if _, resolved := m.Snapshot.Sources[source.ID]; !resolved {
			accuracy = "unknown"
		}
	}
	for _, c := range m.Snapshot.Sources {
		total += c.Total
		if c.TotalAccuracy == queue.TotalUnknown {
			accuracy = "unknown"
		} else if c.TotalAccuracy == queue.TotalEstimated && accuracy == "exact" {
			accuracy = "estimated"
		}
	}
	var footer strings.Builder
	fmt.Fprintf(&footer, "%d loaded of %d (%s) · %d shown · edges %d/%d · search: loaded rows · provider %s · claims %s · %s", len(m.Snapshot.Items), total, accuracy, len(rows), edges, total, sourceFreshness(m.Snapshot), freshnessLabel(m.ClaimFreshness), clip(m.Notice, 60))
	if m.Filtering {
		fmt.Fprintf(&footer, "\n/%s", clip(m.Input, m.Width-2))
	}
	if m.Palette {
		fmt.Fprintf(&footer, "\n:%s", clip(m.Input, m.Width-2))
	}
	footerText := lipgloss.NewStyle().MaxWidth(m.Width).Render(footer.String())
	bodyHeight := max(0, m.Height-lipgloss.Height(footerText))
	body := lipgloss.NewStyle().MaxWidth(m.Width).MaxHeight(bodyHeight).Render(strings.TrimRight(b.String(), "\n"))
	if bodyHeight == 0 {
		return footerText
	}
	return body + "\n" + footerText
}
func scrollDetail(s string, offset int) string {
	lines := strings.Split(s, "\n")
	if offset >= len(lines) {
		offset = max(0, len(lines)-1)
	}
	return strings.Join(lines[offset:], "\n")
}
func clipLines(s string, width int) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		b.WriteString(ansi.Wrap(clean(line), width, " "))
		b.WriteByte('\n')
	}
	return b.String()
}
func freshnessLabel(value string) string {
	if value == "" {
		return "loading"
	}
	return value
}
func sourceFreshness(snapshot queue.Snapshot) string {
	if len(snapshot.Sources) == 0 {
		return "unknown"
	}
	for _, coverage := range snapshot.Sources {
		if coverage.State != queue.CoverageComplete {
			return "stale"
		}
	}
	return "fresh"
}
func healthy(s map[string]queue.Coverage) int {
	n := 0
	for _, c := range s {
		if c.State == queue.CoverageComplete {
			n++
		}
	}
	return n
}
func (m Model) viewCount(name string) int {
	n := 0
	rule := m.ViewRules[name]
	filters := m.ViewFilters[name]
	for _, i := range m.Snapshot.Items {
		if len(filters.SourceIDs) > 0 {
			found := false
			for _, id := range filters.SourceIDs {
				if id == i.Ref.SourceID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if rule.Readiness != "" && rule.Readiness != "all" && string(i.Readiness.Status) != rule.Readiness {
			continue
		}
		if rule.Claim != "" && rule.Claim != "all" && !matchesClaim(i, rule.Claim) {
			continue
		}
		if len(rule.Assigned) > 0 {
			found := false
			for _, kind := range rule.Assigned {
				if kind == "nobody" && len(i.AssignedTo) == 0 {
					found = true
				}
				for _, person := range i.AssignedTo {
					if (kind == "me" && m.isMe(i, person)) || kind == person {
						found = true
					}
				}
			}
			if !found {
				continue
			}
		}
		if _, configured := m.ViewRules[name]; !configured {
			switch name {
			case "Ready":
				if i.Readiness.Status != queue.Ready {
					continue
				}
			case "Mine":
				if m.Me == "" {
					continue
				}
				found := false
				for _, person := range i.AssignedTo {
					if m.isMe(i, person) {
						found = true
					}
				}
				if !found {
					continue
				}
			case "Claimed":
				if !i.Claim.Active {
					continue
				}
			}
		}
		n++
	}
	return n
}
func (m Model) isMe(item queue.Item, owner string) bool {
	for _, name := range m.MeBySource[item.Ref.SourceID] {
		if strings.EqualFold(name, owner) {
			return true
		}
	}
	return m.MeBySource == nil && owner == m.Me && m.Me != ""
}
func matchesClaim(item queue.Item, filter string) bool {
	switch filter {
	case "held":
		return item.Claim.Active
	case "free":
		return item.Claim.Known && !item.Claim.Active
	default:
		return claimState(item) == filter
	}
}
func (m *Model) clearHistory() {
	m.History = ledger.HistoryPage{}
	m.HistoryError = ""
	m.HistoryLoading = false
	m.HistoryIdentity = ""
	m.HistoryCursor = ""
}
func sourceState(c queue.Coverage) string {
	if c.Reason != "" {
		status := clean(c.Reason)
		if !c.RetryAt.IsZero() {
			status += " retry " + c.RetryAt.UTC().Format("Jan 2 15:04:05Z")
		}
		return status
	}
	if c.State == "" {
		return "offline/unknown"
	}
	return string(c.State)
}
func readiness(i queue.Item) string {
	if i.ReadPermission == queue.Denied {
		return "denied"
	}
	if !i.Fresh {
		return "stale"
	}
	if i.ProviderBlocked {
		return "blocked"
	}
	if !i.DependenciesKnown && i.Readiness.Status == queue.ReadinessUnknown {
		return "unknown dependencies"
	}
	if i.Readiness.Status == "" {
		return "unknown"
	}
	return string(i.Readiness.Status)
}
func (m Model) displayState(i queue.Item) string {
	if i.ReadPermission == queue.Denied {
		return "denied"
	}
	if !i.Fresh {
		return "stale"
	}
	if i.ProviderBlocked {
		return "blocked"
	}
	if i.Claim.Active {
		return "occupied"
	}
	if m.Me != "" || len(m.MeBySource[i.Ref.SourceID]) > 0 {
		for _, name := range i.AssignedTo {
			if !m.isMe(i, name) {
				return "assigned elsewhere"
			}
		}
	}
	return readiness(i)
}
func claimState(i queue.Item) string {
	state := i.Claim.State
	if i.Claim.Reason != "" {
		state = i.Claim.Reason
	} else if i.Claim.Active {
		state = "occupied"
	} else if !i.Claim.Known {
		state = "unknown"
	}
	if i.Claim.Stale {
		state += " (stale)"
	}
	return clean(state)
}
func detail(m Model, i queue.Item) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s — %s\n", clip(i.Ref.ItemID, 30), clip(i.Title, m.Width-35))
	for j, t := range tabs {
		if j == m.Tab {
			fmt.Fprintf(&b, "[%s] ", t)
		} else {
			fmt.Fprintf(&b, "%s ", t)
		}
	}
	b.WriteByte('\n')
	switch m.Tab {
	case 0:
		fmt.Fprintf(&b, "State %s · Ready %s · Assigned %s · Native %s\n", clean(i.RawStatus), readiness(i), clip(strings.Join(i.AssignedTo, ","), 40), clip(i.NativeClaim, 30))
		b.WriteString(clip(i.Body, min(m.Width*6, 1200)) + "\n")
	case 1:
		fmt.Fprintf(&b, "Readiness %s: %s\nClosure coverage %s · freshness %s\n", i.Readiness.Status, clean(strings.Join(i.Readiness.Reasons, "; ")), i.Closure, i.Readiness.Freshness)
		for _, r := range i.Relationships {
			fmt.Fprintf(&b, "%s → %s\nRequires: %s · observed: %s\nEvidence: %s · provenance: %s · fresh: %t · support: %s\n", r.Type, clean(r.To.String()), clean(r.Condition), clean(r.RawOutcome), clean(r.Interpretation), clean(r.Provenance), r.Fresh, r.Support)
		}
	case 2:
		fmt.Fprintf(&b, "Observed %s · provider version %s · read %s · coverage %s\n", i.Observation.ObservedAt.Format(time.RFC3339), clip(i.Observation.ProviderVersion, 32), clip(i.ReadOutcome, 20), i.Coverage.State)
		if m.LoadComments == nil {
			b.WriteString("Comments: provider detail unavailable\n")
		} else {
			if m.CommentsLoading {
				b.WriteString("Loading comments…\n")
			}
			if m.CommentsError != "" {
				fmt.Fprintf(&b, "Comments unavailable: %s\n", clip(m.CommentsError, 100))
			}
			for _, comment := range m.Comments {
				fmt.Fprintf(&b, "%s · %s\n%s\n", clip(comment.Author, 32), comment.CreatedAt.Format(time.RFC3339), clip(comment.Body, min(m.Width*4, 800)))
			}
			if m.CommentsCursor != "" {
				b.WriteString("Press m for more comments\n")
			}
		}
	case 3:
		fmt.Fprintf(&b, "Authority %s (%s)\nResource: %s\n", clean(m.Authority), clean(m.Scope), clean(strings.Join(i.Resources, ",")))
		fmt.Fprintf(&b, "Current: %s\nagentId:\n%s\nsessionId:\n%s\n", claimState(i), clean(i.Claim.AgentID), clean(i.Claim.SessionID))
		if m.HistoryLoading {
			b.WriteString("Loading claim epochs…\n")
		}
		if m.HistoryError != "" {
			fmt.Fprintf(&b, "History unavailable: %s\n", clip(m.HistoryError, 100))
		}
		if m.HistoryIdentity == identity(i) {
			if m.History.Gap {
				b.WriteString("History gap: earlier epochs pruned\n")
			}
			for _, e := range m.History.Epochs {
				ended := "active"
				if e.EndedAt != nil {
					ended = e.EndedAt.Format(time.RFC3339)
				}
				fmt.Fprintf(&b, "%s · %s → %s\nagentId:\n%s\nsessionId:\n%s\nReason: %s\n", clip(e.Status, 20), e.AcquiredAt.Format(time.RFC3339), ended, clean(e.AgentID), clean(e.SessionID), clip(e.EndReason, 60))
			}
			if m.History.PreviousCursor != "" {
				b.WriteString("Older retained epochs available (m to load)\n")
			}
		}
	}
	return b.String()
}
