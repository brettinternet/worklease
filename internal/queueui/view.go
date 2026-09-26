package queueui

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/queue"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// The palette uses the terminal's 16 ANSI colors so contrast follows the
// user's theme. Every state is also spelled out in text; color only
// reinforces it, and NO_COLOR or a dumb terminal removes all styling.
var (
	styleFaint     = lipgloss.NewStyle().Faint(true)
	styleBold      = lipgloss.NewStyle().Bold(true)
	styleAccent    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleAppTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleKey       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleGood      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleReady     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	styleWarn      = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleWarnBold  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	styleBad       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
	styleHeld      = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	styleSelected  = lipgloss.NewStyle().Reverse(true).Bold(true)
	styleActiveTab = lipgloss.NewStyle().Reverse(true).Bold(true).Foreground(lipgloss.Color("6"))
	styleAlert     = lipgloss.NewStyle().Reverse(true).Bold(true).Foreground(lipgloss.Color("1"))
	styleDialog    = lipgloss.NewStyle().Reverse(true).Bold(true).Foreground(lipgloss.Color("6"))
)

// splitWidth is the narrowest terminal that shows list and detail side by side.
const splitWidth = 100

const columnGap = 2

// plain reports whether styling is disabled, so selection and active tabs
// need textual markers instead of reverse video.
func plain() bool { return lipgloss.ColorProfile() == termenv.Ascii }

// fit clips s to width cells and pads it with spaces to exactly width.
func fit(s string, width int) string {
	s = clip(s, width)
	return pad(s, width)
}

// pad right-pads an already sanitized (possibly styled) string to width cells.
func pad(s string, width int) string {
	s = ansi.Truncate(s, width, "")
	if w := ansi.StringWidth(s); w < width {
		s += strings.Repeat(" ", width-w)
	}
	return s
}

func clipLines(s string, width int) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		b.WriteString(ansi.Wrap(clean(line), width, " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// wrap sanitizes and wraps one logical line into physical lines.
func wrap(s string, width int) []string {
	return strings.Split(ansi.Wrap(clean(s), max(1, width), " "), "\n")
}

type seg struct {
	text  string
	style lipgloss.Style
}

func renderSegs(segs []seg) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.style.Render(s.text))
	}
	return b.String()
}

func segsWidth(segs []seg) int {
	n := 0
	for _, s := range segs {
		n += ansi.StringWidth(s.text)
	}
	return n
}

// span is a clickable horizontal range on one line.
type span struct{ start, end, index int }

func hit(spans []span, x int) (int, bool) {
	for _, s := range spans {
		if x >= s.start && x < s.end {
			return s.index, true
		}
	}
	return 0, false
}

type tabItem struct {
	label, suffix string
	suffixStyle   lipgloss.Style
}

func tabCellWidth(item tabItem) int {
	n := ansi.StringWidth(item.label) + 2
	if item.suffix != "" {
		n += 1 + ansi.StringWidth(item.suffix)
	}
	return n
}

// tabBar renders tabs with the active one highlighted. When the tabs do not
// fit, it shows a window around the active tab with clickable ‹ › markers.
func tabBar(items []tabItem, active, width int) (string, []span) {
	cell := tabCellWidth
	total := func(lo, hi int) int {
		n := 0
		for i := lo; i <= hi; i++ {
			n += cell(items[i]) + 1
		}
		n-- // no gap after the last tab
		if lo > 0 {
			n += 2
		}
		if hi < len(items)-1 {
			n += 2
		}
		return n
	}
	if len(items) == 0 {
		return "", nil
	}
	active = max(0, min(active, len(items)-1))
	lo, hi := active, active
	for grown := true; grown; {
		grown = false
		if hi+1 < len(items) && total(lo, hi+1) <= width {
			hi++
			grown = true
		}
		if lo > 0 && total(lo-1, hi) <= width {
			lo--
			grown = true
		}
	}
	var b strings.Builder
	var spans []span
	x := 0
	if lo > 0 {
		b.WriteString(styleFaint.Render("‹ "))
		spans = append(spans, span{0, 2, lo - 1})
		x = 2
	}
	for i := lo; i <= hi; i++ {
		item := items[i]
		w := cell(item)
		switch {
		case i == active && plain():
			text := item.label
			if item.suffix != "" {
				text += " " + item.suffix
			}
			b.WriteString("[" + text + "]")
		case i == active:
			text := " " + item.label
			if item.suffix != "" {
				text += " " + item.suffix
			}
			b.WriteString(styleActiveTab.Render(text + " "))
		default:
			b.WriteString(" " + item.label)
			if item.suffix != "" {
				b.WriteString(" " + item.suffixStyle.Render(item.suffix))
			}
			b.WriteString(" ")
		}
		spans = append(spans, span{x, x + w, i})
		b.WriteByte(' ')
		x += w + 1
	}
	if hi < len(items)-1 {
		b.WriteString(styleFaint.Render(" ›"))
		spans = append(spans, span{x, x + 2, hi + 1})
	}
	return b.String(), spans
}

// frame is the fixed chrome around the body. Mouse hit-testing uses the same
// frame as rendering, so a click always maps to what is on screen.
type frame struct {
	top, footer         []string
	tabs                []span
	bodyTop, bodyHeight int
}

func (m Model) frame(rows []queue.Item) frame {
	f := frame{}
	f.top = append(f.top, m.headerLine())
	tabs, spans := m.viewTabs()
	f.top = append(f.top, tabs)
	f.tabs = spans
	f.top = append(f.top, m.banners()...)
	f.footer = m.footerLines(rows)
	f.bodyTop = len(f.top)
	f.bodyHeight = m.Height - len(f.top) - len(f.footer)
	if f.bodyHeight < 3 && len(f.footer) > 1 {
		// Keep the coverage line; the key bar is the first thing to go.
		f.footer = f.footer[:len(f.footer)-1]
		f.bodyHeight++
	}
	f.bodyHeight = max(0, f.bodyHeight)
	return f
}

// listCapacity is the number of rows visible below the column header.
func (m Model) listCapacity(rows []queue.Item) int {
	return max(1, m.frame(rows).bodyHeight-1)
}

// listOffset returns the first visible row, keeping the selection on screen.
func (m Model) listOffset(n, capacity int) int {
	offset := m.Offset
	if m.Index < offset {
		offset = m.Index
	}
	if m.Index >= offset+capacity {
		offset = m.Index - capacity + 1
	}
	return max(0, min(offset, n-capacity))
}

func (m Model) split() bool { return m.Detail && m.Width >= splitWidth }

// paneWidths returns the list and detail widths; one separator column sits
// between them when split.
func (m Model) paneWidths() (int, int) {
	if !m.Detail {
		return m.Width, 0
	}
	if !m.split() {
		return 0, m.Width
	}
	detail := max(40, min(80, m.Width*2/5))
	return m.Width - detail - 1, detail
}

func (m Model) View() string {
	if m.Width < 30 {
		return "Resize terminal to at least 30 columns\n"
	}
	if dialog, ok := m.dialogView(); ok {
		return dialog
	}
	rows := m.rows()
	m.anchor(rows)
	f := m.frame(rows)
	var body []string
	switch {
	case m.Help:
		body = m.helpLines()
	case m.ViewName == RecoveryViewID:
		body = m.recoveryLines(f.bodyHeight)
	default:
		body = m.mainBody(rows, f.bodyHeight)
	}
	out := make([]string, 0, m.Height)
	out = append(out, f.top...)
	for n := range f.bodyHeight {
		if n < len(body) {
			out = append(out, body[n])
		} else {
			out = append(out, "")
		}
	}
	out = append(out, f.footer...)
	if m.Height > 0 && len(out) > m.Height {
		out = out[:m.Height]
	}
	for n := range out {
		out[n] = ansi.Truncate(out[n], m.Width, "")
	}
	return strings.Join(out, "\n")
}

func (m Model) headerLine() string {
	authority := valueOr(clean(m.Authority), "unset")
	scope := valueOr(clean(m.Scope), "unknown scope")
	age := "not synced"
	ageStyle := styleWarn
	if m.rowCache != nil && !m.rowCache.observed.IsZero() {
		age, ageStyle = "synced "+ago(time.Since(m.rowCache.observed)), styleFaint
	}
	healthyCount := healthy(m.Snapshot.Sources)
	sourceStyle := styleGood
	if healthyCount < len(m.Sources) {
		sourceStyle = styleWarnBold
	}
	build := func(authority string, withMe, withAge bool) []seg {
		label := "  authority: "
		if !withMe {
			label = "  "
		}
		segs := []seg{{"worklease queue", styleAppTitle}, {label, styleFaint}, {authority + " (" + scope + ")", lipgloss.NewStyle()}}
		if withMe && m.Me != "" {
			segs = append(segs, seg{"  me: ", styleFaint}, seg{clean(m.Me), lipgloss.NewStyle()})
		}
		segs = append(segs, seg{"  ", lipgloss.NewStyle()}, seg{fmt.Sprintf("sources %d/%d", healthyCount, len(m.Sources)), sourceStyle})
		if withAge {
			segs = append(segs, seg{"  " + age, ageStyle})
		}
		return segs
	}
	// Prefer dropping low-value facts over truncating the authority, which
	// identifies whom claims coordinate with.
	candidates := [][]seg{build(authority, true, true), build(shortIDs(authority), true, true), build(shortIDs(authority), true, false), build(shortIDs(authority), false, false)}
	for _, segs := range candidates {
		if segsWidth(segs) <= m.Width {
			return renderSegs(segs)
		}
	}
	return renderSegs(candidates[len(candidates)-1])
}

// shortIDs abbreviates long opaque tokens such as authority IDs.
func shortIDs(s string) string {
	fields := strings.Fields(s)
	for n, field := range fields {
		if len(field) > 12 {
			fields[n] = field[:8] + "…"
		}
	}
	return strings.Join(fields, " ")
}

func ago(d time.Duration) string {
	switch {
	case d < 10*time.Second:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func (m Model) viewCountFor(name string) int {
	if name == RecoveryViewID {
		return len(m.Recovery)
	}
	if m.rowCache == nil || m.rowCache.counts == nil {
		return m.viewCount(name)
	}
	return m.rowCache.counts[name]
}

func (m Model) viewTabs() (string, []span) {
	items := make([]tabItem, 0, len(m.Views))
	active := 0
	for n, name := range m.Views {
		count := m.viewCountFor(name)
		style := styleFaint
		if name == RecoveryViewID && count > 0 {
			style = styleBad
		}
		items = append(items, tabItem{label: clip(viewLabel(name), 20), suffix: fmt.Sprint(count), suffixStyle: style})
		if name == m.ViewName {
			active = n
		}
	}
	return tabBar(items, active, m.Width)
}

var sourceHints = map[string]string{
	"cached-index":            "showing cached rows while refreshing",
	"observation-invalidated": "provider changed during read; r retries",
}

func (m Model) banners() []string {
	var out []string
	if m.UncertainWrite || len(m.Recovery) > 0 {
		text := fmt.Sprintf(" RECOVERY REQUIRED: %d unresolved writes; claims held (do not release until verified)", len(m.Recovery))
		if m.UncertainWrite {
			text += "; last write unverified"
		}
		out = append(out, styleAlert.Render(pad(text, m.Width)))
	}
	var problems []string
	for _, source := range m.Sources {
		coverage, resolved := m.Snapshot.Sources[source.ID]
		status := "not read yet"
		if resolved {
			status = sourceState(coverage)
		}
		problem := m.SourceErrors[source.ID]
		if problem != "" {
			status = problem
		}
		if resolved && problem == "" && coverage.State == queue.CoverageComplete && coverage.Reason == "" {
			continue
		}
		if hint := sourceHints[status]; hint != "" {
			status += " (" + hint + ")"
		}
		name := source.Name
		if name == "" {
			name = source.ID
		}
		problems = append(problems, clip(name, 24)+": "+clean(status))
	}
	if len(problems) > 0 {
		out = append(out, styleWarnBold.Render("Sources ")+styleWarn.Render(strings.Join(problems, " · ")))
	}
	return out
}

func (m Model) footerLines(rows []queue.Item) []string {
	loaded := len(m.Snapshot.Items)
	total, accuracy := 0, "exact"
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
	edges := 0
	if m.rowCache != nil {
		edges = m.rowCache.edges
	}
	coverage := fmt.Sprintf("%d loaded of %d (%s)", loaded, total, accuracy)
	if total < loaded {
		coverage = fmt.Sprintf("%d loaded (total unknown)", loaded)
	}
	left := []seg{{fmt.Sprintf("%d shown", len(rows)), styleBold}, {" · " + coverage + fmt.Sprintf(" · edges %d/%d", edges, loaded), styleFaint}}
	if m.Filter != "" {
		left = append(left, seg{" · ", styleFaint}, seg{"filter ", styleAccent}, seg{fmt.Sprintf("%q", clip(m.Filter, 32)), styleBold}, seg{" (loaded rows)", styleFaint})
	}
	provider, claims := sourceFreshness(m.Snapshot), freshnessLabel(m.ClaimFreshness)
	right := []seg{{"provider ", styleFaint}, {provider, freshnessStyle(provider)}, {" · claims ", styleFaint}, {claims, freshnessStyle(claims)}}
	var status string
	if gap := m.Width - segsWidth(left) - segsWidth(right); gap >= 2 {
		status = renderSegs(left) + strings.Repeat(" ", gap) + renderSegs(right)
	} else {
		// Narrow: keep the shown count and freshness; coverage detail is in wider layouts.
		status = renderSegs(append([]seg{left[0], {" · ", styleFaint}}, right...))
	}
	lines := []string{status}
	if m.Notice != "" {
		// Notices get their own line so holder, expiry, and recovery paths stay readable.
		lines = append(lines, noticeStyle(m.Notice).Render(clip(m.Notice, m.Width)))
	}
	if m.Filtering || m.Palette {
		prompt := "/"
		if m.Palette {
			prompt = ":"
		}
		lines = append(lines, styleKey.Render(prompt)+clip(m.Input, m.Width-3)+styleSelected.Render(" "))
	}
	return append(lines, m.keyBar())
}

func freshnessStyle(value string) lipgloss.Style {
	switch value {
	case "fresh":
		return styleGood
	case "loading", "rebuilding":
		return styleFaint
	default:
		return styleWarnBold
	}
}

// noticeStyle calls out notices that report a failure or an unresolved
// outcome; the notice text itself always carries the meaning.
func noticeStyle(notice string) lipgloss.Style {
	lower := strings.ToLower(notice)
	for _, word := range []string{"fail", "refused", "unavailable", "uncertain", "unknown", "lost", "recovery", "contended", "expired", "rejected"} {
		if strings.Contains(lower, word) {
			return styleWarnBold
		}
	}
	return styleAccent
}

type binding struct{ keys, desc string }

func keyHints(bindings []binding, width int) string {
	render := func(b binding) string { return styleKey.Render(b.keys) + " " + styleFaint.Render(b.desc) }
	cost := func(b binding) int { return ansi.StringWidth(b.keys) + 1 + ansi.StringWidth(b.desc) + 2 }
	// The last binding (help or cancel) is kept even when earlier ones drop.
	last := bindings[len(bindings)-1]
	used := cost(last)
	var parts []string
	for _, b := range bindings[:len(bindings)-1] {
		if used+cost(b) > width {
			break
		}
		used += cost(b)
		parts = append(parts, render(b))
	}
	return strings.Join(append(parts, render(last)), "  ")
}

func (m Model) keyBar() string {
	var bindings []binding
	switch {
	case m.Filtering:
		bindings = []binding{{"enter", "apply"}, {"ctrl+u", "clear"}, {"esc", "cancel"}}
	case m.Palette:
		bindings = []binding{{"start work", "command"}, {"enter", "run"}, {"esc", "cancel"}}
	case m.Help:
		bindings = []binding{{"esc/?", "close help"}}
	case m.ViewName == RecoveryViewID:
		bindings = []binding{{"j/k", "select"}, {"r", "retry read-back"}, {"e", "attest"}, {"u", "reload"}, {"v/1-9", "view"}, {"q", "quit"}, {"?", "help"}}
	case m.Detail:
		bindings = []binding{{"tab", "section"}, {"pgup/pgdn", "scroll"}, {"c", "claim"}, {"S", "start"}, {"s", "state"}, {"p", "progress"}, {"a", "assign"}, {"o", "open"}, {"esc", "back"}, {"?", "help"}}
	default:
		bindings = []binding{{"j/k", "move"}, {"enter", "open"}, {"/", "filter"}, {"v/1-9", "view"}, {"c", "claim"}, {"x", "launch"}, {"r", "refresh"}, {"q", "quit"}, {"?", "help"}}
		if m.Filter != "" {
			bindings = slices.Insert(bindings, 3, binding{"esc", "clear filter"})
		}
	}
	return keyHints(bindings, m.Width)
}

var helpGroups = []struct {
	title    string
	bindings []binding
}{
	{"Navigate", []binding{{"j/k ↓/↑", "move selection"}, {"n / N", "next / previous match"}, {"gg / G", "top / bottom"}, {"enter l →", "open detail"}, {"esc h ←", "back / close"}, {"tab ⇧tab", "next / previous detail section"}, {"pgdn pgup", "scroll detail"}}},
	{"Views and filter", []binding{{"v / V", "next / previous view"}, {"1-9", "jump to view"}, {"/", "filter loaded rows"}, {"esc", "clear filter"}}},
	{"Item actions (preview first)", []binding{{"c", "claim for me"}, {"S", "start work: claim + transition (detail)"}, {"s", "change provider state"}, {"p", "record progress note"}, {"a", "assign to me"}, {"R", "release verified no-effect claim"}, {"x", "launch worker"}, {"o", "open provider URL"}, {"m", "load more comments / claim history"}}},
	{"Queue", []binding{{"r", "refresh sources"}, {":", "command palette (start work)"}, {"?", "toggle help"}, {"q", "quit"}}},
	{"Mouse", []binding{{"click", "select row; click again to open"}, {"click tab", "switch view or detail section"}, {"wheel", "scroll list or detail"}, {"shift+drag", "select text (option+drag in iTerm2)"}}},
}

func (m Model) helpLines() []string {
	var groups [][]string
	for _, group := range helpGroups {
		lines := []string{styleBold.Render(group.title)}
		for _, b := range group.bindings {
			lines = append(lines, "  "+styleKey.Render(fit(b.keys, 11))+" "+b.desc)
		}
		groups = append(groups, append(lines, ""))
	}
	const columnWidth = 56
	if m.Width < columnWidth*2 {
		var out []string
		for _, group := range groups {
			out = append(out, group...)
		}
		return out
	}
	// Two columns: split the groups where the taller column is shortest.
	best, bestHeight := 1, 1<<30
	for split := 1; split < len(groups); split++ {
		left, right := 0, 0
		for n, group := range groups {
			if n < split {
				left += len(group)
			} else {
				right += len(group)
			}
		}
		if height := max(left, right); height < bestHeight {
			best, bestHeight = split, height
		}
	}
	var left, right []string
	for n, group := range groups {
		if n < best {
			left = append(left, group...)
		} else {
			right = append(right, group...)
		}
	}
	out := make([]string, max(len(left), len(right)))
	for n := range out {
		l, r := "", ""
		if n < len(left) {
			l = left[n]
		}
		if n < len(right) {
			r = right[n]
		}
		out[n] = pad(l, columnWidth) + r
	}
	return out
}

// column describes one list column. Title is the flexible column.
type column struct {
	title    string
	min, max int
	width    int
	cell     func(queue.Item) (string, lipgloss.Style)
}

// columns fits the list columns to width. Fixed columns size to their
// content up to a cap; when space runs out they shrink toward their minimums,
// then low-priority columns drop, so rows never wrap.
func (m Model) columns(rows, visible []queue.Item, width int) []column {
	idWidth, native := 2, false
	for _, item := range rows {
		idWidth = max(idWidth, len(item.Ref.ItemID))
		if !native && item.NativeClaim != "" && item.NativeClaim != "not-exposed" {
			native = true
		}
	}
	idWidth = min(idWidth, 16)
	all := []column{
		{title: "ID", min: idWidth, max: idWidth, cell: func(i queue.Item) (string, lipgloss.Style) { return i.Ref.ItemID, styleAccent }},
		{title: "Title", min: 16, cell: func(i queue.Item) (string, lipgloss.Style) { return i.Title, lipgloss.NewStyle() }},
		{title: "Status", min: 6, max: 16, cell: func(i queue.Item) (string, lipgloss.Style) { return projectStatusDisplay(i), stateStyle(i.State) }},
		{title: "Ready", min: 8, max: 20, cell: m.readyCell},
		{title: "Assigned", min: 6, max: 14, cell: m.assignedCell},
		{title: "Native", min: 6, max: 12, cell: func(i queue.Item) (string, lipgloss.Style) { return i.NativeClaim, styleFaint }},
		{title: "Claim", min: 6, max: 16, cell: m.claimCell},
	}
	if !native {
		all = slices.Delete(all, 5, 6)
	}
	seen := m.contentWidths(all, visible)
	for n, c := range all {
		if c.title == "ID" || c.title == "Title" {
			continue
		}
		all[n].max = min(c.max, max(len(c.title), seen[c.title]))
		all[n].min = min(c.min, all[n].max)
	}
	// Drop low-priority columns before shrinking the rest; a narrow list of
	// whole values reads better than many clipped ones. ID never shrinks.
	available := width - 2 // selection marker
	fixedWidth := func() int {
		n := 0
		for _, c := range all {
			if c.title != "Title" {
				n += c.width + columnGap
			}
		}
		return n
	}
	for n := range all {
		all[n].width = all[n].max
	}
	for _, drop := range []string{"Native", "Assigned", "Status", "Claim"} {
		if available-fixedWidth() >= all[1].min {
			break
		}
		all = slices.DeleteFunc(all, func(c column) bool { return c.title == drop })
	}
	for need := all[1].min - (available - fixedWidth()); need > 0; {
		shrunk := false
		for n := len(all) - 1; n >= 0 && need > 0; n-- {
			if c := all[n]; c.title != "Title" && c.title != "ID" && c.width > c.min {
				all[n].width--
				need--
				shrunk = true
			}
		}
		if !shrunk {
			break
		}
	}
	all[1].width = max(1, available-fixedWidth())
	return all
}

// contentWidths returns the widest cell per fixed column among the visible
// rows, merged with widths already seen for the same rows so columns only
// grow while scrolling.
func (m Model) contentWidths(cols []column, visible []queue.Item) map[string]int {
	widths := map[string]int{}
	for _, item := range visible {
		for _, c := range cols {
			if c.title == "ID" || c.title == "Title" {
				continue
			}
			text, _ := c.cell(item)
			widths[c.title] = max(widths[c.title], ansi.StringWidth(clean(text)))
		}
	}
	if m.rowCache == nil {
		return widths
	}
	m.rowCache.mu.Lock()
	defer m.rowCache.mu.Unlock()
	if m.rowCache.widths == nil {
		m.rowCache.widths = map[string]int{}
	}
	for title, width := range widths {
		m.rowCache.widths[title] = max(m.rowCache.widths[title], width)
	}
	return maps.Clone(m.rowCache.widths)
}

func stateStyle(state queue.StateCategory) lipgloss.Style {
	switch state {
	case queue.StateInProgress:
		return styleAccent
	case queue.StateBlocked:
		return styleBad
	case queue.StateComplete:
		return styleFaint
	}
	return lipgloss.NewStyle()
}

var readyAliases = map[string]string{"assigned elsewhere": "elsewhere", "unknown dependencies": "deps unknown"}

func (m Model) readyCell(i queue.Item) (string, lipgloss.Style) {
	if i.Claim.Active && m.ownsClaim(i) {
		return "mine", styleReady
	}
	state := m.displayState(i)
	switch state {
	case "ready":
		return state, styleReady
	case "blocked", "denied":
		return state, styleBad
	case "occupied":
		return state, styleHeld
	case "assigned elsewhere":
		return state, styleFaint
	case "stale", "unknown dependencies", "unknown":
		return state, styleWarn
	}
	return state, lipgloss.NewStyle()
}

func (m Model) assignedCell(i queue.Item) (string, lipgloss.Style) {
	if len(i.AssignedTo) == 0 {
		return "—", styleFaint
	}
	for _, name := range i.AssignedTo {
		if m.isMe(i, name) {
			return strings.Join(i.AssignedTo, ","), styleBold
		}
	}
	return strings.Join(i.AssignedTo, ","), lipgloss.NewStyle()
}

func (m Model) claimCell(i queue.Item) (string, lipgloss.Style) {
	if m.ownsClaim(i) {
		return "mine", styleReady
	}
	state := claimState(i)
	switch {
	case i.Claim.Stale || i.Claim.Reason != "":
		return state, styleWarn
	case i.Claim.Active:
		return state, styleHeld
	}
	return state, styleFaint
}

// ownsClaim reports whether this queue holds a verified claim on the item.
func (m Model) ownsClaim(i queue.Item) bool {
	if len(i.Resources) == 0 {
		return false
	}
	for _, owned := range m.OwnedClaims {
		if owned.Verified && !owned.Lost && sameResources(i.Resources, owned.Resources) {
			return true
		}
	}
	return false
}

func (m Model) cellText(c column, text string) string {
	if c.title == "Ready" && ansi.StringWidth(text) > c.width {
		if alias, ok := readyAliases[text]; ok {
			text = alias
		}
	}
	return fit(text, c.width)
}

func (m Model) listLines(rows []queue.Item, width, height int) []string {
	if height <= 0 {
		return nil
	}
	capacity := max(1, height-1)
	offset := m.listOffset(len(rows), capacity)
	visible := rows[offset:min(len(rows), offset+capacity)]
	cols := m.columns(rows, visible, width)
	header := make([]string, 0, len(cols))
	for _, c := range cols {
		header = append(header, fit(c.title, c.width))
	}
	lines := []string{styleBold.Render("  " + strings.Join(header, strings.Repeat(" ", columnGap)))}
	if len(rows) == 0 {
		return append(lines, m.emptyLines(width)...)
	}
	gap := strings.Repeat(" ", columnGap)
	for _, item := range visible {
		selected := identity(item) == m.Selected
		if selected {
			cells := make([]string, 0, len(cols))
			for _, c := range cols {
				text, _ := c.cell(item)
				cells = append(cells, m.cellText(c, text))
			}
			lines = append(lines, styleSelected.Render(pad("> "+strings.Join(cells, gap), width)))
			continue
		}
		cells := make([]string, 0, len(cols))
		for _, c := range cols {
			text, style := c.cell(item)
			if item.Terminal {
				style = styleFaint
			}
			cells = append(cells, style.Render(m.cellText(c, text)))
		}
		lines = append(lines, "  "+strings.Join(cells, gap))
	}
	return lines
}

// emptyLines explains why a view has no rows instead of showing a blank list.
func (m Model) emptyLines(width int) []string {
	lines := []string{"", "  " + styleBold.Render("No items in "+clip(viewLabel(m.ViewName), 24)+".")}
	if len(m.Snapshot.Items) == 0 {
		lines = append(lines, "  "+styleFaint.Render("Nothing loaded yet; source status is shown above."))
		return lines
	}
	counts := map[string]int{}
	for _, item := range m.Snapshot.Items {
		counts[m.displayState(item)]++
	}
	states := make([]string, 0, len(counts))
	for state := range counts {
		states = append(states, state)
	}
	sort.Slice(states, func(a, b int) bool {
		if counts[states[a]] != counts[states[b]] {
			return counts[states[a]] > counts[states[b]]
		}
		return states[a] < states[b]
	})
	parts := make([]string, 0, len(states))
	for _, state := range states {
		parts = append(parts, fmt.Sprintf("%d %s", counts[state], state))
	}
	lines = append(lines, "  "+clip(fmt.Sprintf("Loaded %d: %s", len(m.Snapshot.Items), strings.Join(parts, ", ")), width-2))
	if m.Filter != "" {
		lines = append(lines, "  "+styleAccent.Render(clip(fmt.Sprintf("Filter %q is active; esc clears it.", m.Filter), width-2)))
	}
	if counts["stale"] > 0 || sourceFreshness(m.Snapshot) != "fresh" {
		lines = append(lines, "  "+styleWarn.Render("Stale rows are never ready; r refreshes sources."))
	}
	return lines
}

func (m Model) mainBody(rows []queue.Item, height int) []string {
	listWidth, detailWidth := m.paneWidths()
	item, ok := m.selected(rows)
	if !m.Detail || !ok {
		return m.listLines(rows, m.Width, height)
	}
	pane := m.detailPane(item, detailWidth, height)
	if listWidth == 0 {
		return pane
	}
	list := m.listLines(rows, listWidth-1, height)
	sep := styleFaint.Render("│")
	out := make([]string, height)
	for n := range out {
		l, d := "", ""
		if n < len(list) {
			l = list[n]
		}
		if n < len(pane) {
			d = pane[n]
		}
		out[n] = pad(l, listWidth) + sep + d
	}
	return out
}

var (
	tabNames      = []string{"Summary", "Dependencies", "Activity", "Claims", "Recovery"}
	shortTabNames = []string{"Summary", "Deps", "Activity", "Claims", "Recovery"}
)

// detailTabs prefers full section names, then short names, then a window.
func detailTabs(active, width int) (string, []span) {
	var items []tabItem
	for _, names := range [][]string{tabNames, shortTabNames} {
		items = make([]tabItem, len(names))
		total := 0
		for n, name := range names {
			items[n] = tabItem{label: name}
			total += tabCellWidth(items[n]) + 1
		}
		if total-1 <= width {
			break
		}
	}
	return tabBar(items, active, width)
}

// detailContent renders the scrollable part of the detail pane.
func (m Model) detailContent(item queue.Item, width int) []string {
	return renderDetail(detail(m, item), max(1, width-1))
}

func (m Model) detailPane(item queue.Item, width, height int) []string {
	title := styleAccent.Bold(true).Render(clip(item.Ref.ItemID, 30)) + " " + styleBold.Render(clip(item.Title, max(1, width-ansi.StringWidth(clip(item.Ref.ItemID, 30))-2)))
	tabs, _ := detailTabs(m.Tab, width-1)
	lines := []string{" " + title, " " + tabs}
	content := m.detailContent(item, width)
	visible := max(0, height-len(lines))
	offset := max(0, min(m.DetailOffset, len(content)-visible))
	end := min(len(content), offset+visible)
	shown := content[offset:end]
	if end < len(content) && len(shown) > 0 {
		shown = append(append([]string(nil), shown[:len(shown)-1]...), styleFaint.Render(fmt.Sprintf("  ↓ %d more lines (pgdn)", len(content)-end+1)))
	}
	for _, line := range shown {
		lines = append(lines, " "+line)
	}
	return lines
}

// maxDetailOffset bounds detail scrolling to the rendered content.
func (m Model) maxDetailOffset() int {
	rows := m.rows()
	item, ok := m.selected(rows)
	if !ok {
		return 0
	}
	_, width := m.paneWidths()
	if width == 0 {
		width = m.Width
	}
	visible := max(1, m.frame(rows).bodyHeight-2)
	return max(0, len(m.detailContent(item, width))-visible)
}

// dline is one logical detail line: an optional bold label and wrapped text.
type dline struct {
	label, text string
	style       lipgloss.Style
}

func renderDetail(lines []dline, width int) []string {
	labelWidth := 0
	for _, l := range lines {
		labelWidth = max(labelWidth, ansi.StringWidth(l.label))
	}
	labelWidth = min(labelWidth, 16)
	var out []string
	for _, l := range lines {
		if l.label == "" || width-labelWidth-1 < 12 {
			if l.label != "" {
				out = append(out, styleBold.Render(clip(l.label, width)))
			}
			for _, text := range wrap(l.text, width) {
				out = append(out, l.style.Render(text))
			}
			continue
		}
		indent := strings.Repeat(" ", labelWidth+1)
		for n, text := range wrap(l.text, width-labelWidth-1) {
			prefix := indent
			if n == 0 {
				prefix = styleBold.Render(fit(l.label, labelWidth)) + " "
			}
			out = append(out, prefix+l.style.Render(text))
		}
	}
	return out
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func detail(m Model, i queue.Item) []dline {
	plainStyle := lipgloss.NewStyle()
	var d []dline
	add := func(label, text string, style lipgloss.Style) { d = append(d, dline{label, text, style}) }
	switch m.Tab {
	case 0:
		if i.ProjectStatusBound {
			add("Issue state", i.RawStatus, stateStyle(i.State))
			add("Project status", projectStatusRaw(i)+" → "+projectStatusState(i), plainStyle)
			if i.ProjectStatusConflict {
				add("", "CONFLICT: issue and project status disagree", styleBad)
			}
		} else {
			add("Status", valueOr(i.RawStatus, "unknown"), stateStyle(i.State))
		}
		ready, readyStyle := m.readyCell(i)
		add("Ready", ready, readyStyle)
		add("Assigned", valueOr(strings.Join(i.AssignedTo, ", "), "nobody"), plainStyle)
		add("Native", valueOr(i.NativeClaim, "—"), plainStyle)
		claim, claimStyle := m.claimCell(i)
		add("Claim", claim, claimStyle)
		add("", "", plainStyle)
		body := []rune(i.Body)
		if len(body) > 4000 {
			body = append(body[:4000], '…')
		}
		blank := false
		for _, paragraph := range strings.Split(string(body), "\n") {
			if strings.TrimSpace(paragraph) == "" {
				if !blank {
					add("", "", plainStyle)
				}
				blank = true
				continue
			}
			blank = false
			add("", paragraph, plainStyle)
		}
		add("", "", plainStyle)
		if m.StartTransitions[i.Ref.SourceID] != "" {
			add("Actions", "S start work (claim + provider transition) · c claim only · : start work", styleAccent)
		} else {
			add("Actions", "c claim only (no supported Start work mapping)", styleAccent)
		}
	case 1:
		add("Readiness", fmt.Sprintf("%s: %s", i.Readiness.Status, strings.Join(i.Readiness.Reasons, "; ")), plainStyle)
		add("Closure", fmt.Sprintf("coverage %s · freshness %s", i.Closure, i.Readiness.Freshness), plainStyle)
		if len(i.Relationships) == 0 {
			add("", "", plainStyle)
			add("", "No dependency relationships observed.", styleFaint)
		}
		for _, r := range i.Relationships {
			style := plainStyle
			if !r.Fresh {
				style = styleWarn
			}
			add("", "", plainStyle)
			add("", fmt.Sprintf("%s → %s", r.Type, clean(r.To.String())), styleBold)
			add("Requires", fmt.Sprintf("%s · observed: %s", r.Condition, r.RawOutcome), plainStyle)
			add("Evidence", fmt.Sprintf("%s · provenance: %s · fresh: %t · support: %s", r.Interpretation, r.Provenance, r.Fresh, r.Support), style)
		}
	case 2:
		add("Observed", i.Observation.ObservedAt.Format(time.RFC3339), plainStyle)
		add("Version", valueOr(clip(i.Observation.ProviderVersion, 32), "—"), plainStyle)
		add("Read", fmt.Sprintf("%s · coverage %s", valueOr(clip(i.ReadOutcome, 20), "—"), i.Coverage.State), plainStyle)
		add("", "", plainStyle)
		if m.LoadComments == nil {
			add("", "Comments: provider detail unavailable", styleFaint)
			break
		}
		if m.CommentsLoading {
			add("", "Loading comments…", styleFaint)
		}
		if m.CommentsError != "" {
			add("", "Comments unavailable: "+clip(m.CommentsError, 100), styleWarn)
		}
		for _, comment := range m.Comments {
			add("", clip(comment.Author, 32)+" · "+comment.CreatedAt.Format(time.RFC3339), styleBold)
			add("", clip(comment.Body, min(m.Width*4, 800)), plainStyle)
			add("", "", plainStyle)
		}
		if m.CommentsCursor != "" {
			add("", "Press m for more comments", styleAccent)
		}
	case 3:
		add("Authority", fmt.Sprintf("%s (%s)", m.Authority, m.Scope), plainStyle)
		add("Resource", strings.Join(i.Resources, ", "), plainStyle)
		claim, claimStyle := m.claimCell(i)
		add("Current", claim, claimStyle)
		add("Agent", valueOr(i.Claim.AgentID, "—"), plainStyle)
		add("Session", valueOr(i.Claim.SessionID, "—"), plainStyle)
		if !i.Claim.ExpiresAt.IsZero() {
			if !i.Claim.AcquiredAt.IsZero() && i.Claim.ExpiresAt.After(i.Claim.AcquiredAt) {
				add("Granted TTL", i.Claim.ExpiresAt.Sub(i.Claim.AcquiredAt).Round(time.Second).String(), plainStyle)
			}
			add("Expires", i.Claim.ExpiresAt.UTC().Format(time.RFC3339), plainStyle)
		}
		for _, owned := range m.OwnedClaims {
			if !sameResources(i.Resources, owned.Resources) {
				continue
			}
			add("", "", plainStyle)
			add("Queue-owned", fmt.Sprintf("%s · next renewal %s · last result %s", owned.ClaimID, owned.NextRenewal.UTC().Format(time.RFC3339), clip(owned.LastResult, 100)), plainStyle)
			switch {
			case owned.Lost:
				add("", "Claim lost; actions disabled", styleBad)
			case !owned.Verified:
				add("", "Ownership unverified; actions disabled", styleWarn)
			default:
				add("", "R: cancel if no operation or provider write started", styleAccent)
			}
		}
		if m.HistoryLoading {
			add("", "Loading claim epochs…", styleFaint)
		}
		if m.HistoryError != "" {
			add("", "History unavailable: "+clip(m.HistoryError, 100), styleWarn)
		}
		if m.HistoryIdentity == identity(i) {
			if m.History.Gap {
				add("", "History gap: earlier epochs pruned", styleWarn)
			}
			for _, e := range m.History.Epochs {
				ended := "active"
				if e.EndedAt != nil {
					ended = e.EndedAt.Format(time.RFC3339)
				}
				add("", "", plainStyle)
				add("", fmt.Sprintf("%s · %s → %s", clip(e.Status, 20), e.AcquiredAt.Format(time.RFC3339), ended), styleBold)
				add("Agent", e.AgentID, plainStyle)
				add("Session", e.SessionID, plainStyle)
				add("Reason", clip(e.EndReason, 60), plainStyle)
			}
			if m.History.PreviousCursor != "" {
				add("", "Older retained epochs available (m to load)", styleAccent)
			}
		}
	case 4:
		if m.RecoveryError != "" {
			add("", "Recovery unavailable: "+clip(m.RecoveryError, 100), styleBad)
		}
		found := false
		for _, entry := range m.Recovery {
			if entry.Ref != i.Ref {
				continue
			}
			found = true
			add("", fmt.Sprintf("%s · %s · %s · claim held %s", entry.OperationID, entry.Action, entry.Status, entry.ClaimID), styleWarnBold)
			add("Resources", fmt.Sprintf("%s · actor %s · effect %s", strings.Join(entry.Resources, ", "), entry.Principal, entry.Effect), plainStyle)
			add("Marker", fmt.Sprintf("%s · required effects %s", entry.Marker, strings.Join(entry.Effects, ", ")), plainStyle)
			add("Dispatched", fmt.Sprintf("%s · read-back %s", recoveryTime(entry.Dispatched), entry.Readback), plainStyle)
			add("Next", strings.Join(entry.Next, "; "), styleAccent)
			add("", "", plainStyle)
		}
		if !found && m.RecoveryError == "" {
			add("", "No unresolved writes for this item.", styleFaint)
		}
	}
	return d
}

func (m Model) recoveryLines(height int) []string {
	heading := styleBold.Render(fmt.Sprintf("%d unresolved writes", len(m.Recovery)))
	if m.RecoveryError != "" {
		heading += " · " + styleBad.Render(clean(m.RecoveryError))
	}
	lines := []string{" " + heading, ""}
	var blocks [][]string
	for index, entry := range m.Recovery {
		marker := "  "
		if index == m.RecoveryIndex {
			marker = "> "
		}
		first := fmt.Sprintf("%s%s %s %s · %s · claim held %s", marker, clean(entry.OperationID), clean(entry.Ref.String()), clean(entry.Status), clean(string(entry.Action)), clean(entry.ClaimID))
		if index == m.RecoveryIndex {
			first = styleSelected.Render(pad(clip(first, m.Width), m.Width))
		} else {
			first = styleWarnBold.Render(clip(first, m.Width))
		}
		block := []string{first}
		for _, detail := range []string{
			fmt.Sprintf("resources %s · actor %s · effect %s", strings.Join(entry.Resources, ", "), entry.Principal, entry.Effect),
			fmt.Sprintf("marker %s · required effects %s", entry.Marker, strings.Join(entry.Effects, ", ")),
			fmt.Sprintf("dispatched %s · read-back %s", recoveryTime(entry.Dispatched), entry.Readback),
		} {
			for _, text := range wrap(detail, m.Width-4) {
				block = append(block, "    "+text)
			}
		}
		for _, text := range wrap("next "+strings.Join(entry.Next, "; "), m.Width-4) {
			block = append(block, "    "+styleAccent.Render(text))
		}
		blocks = append(blocks, block)
	}
	// Start from the first entry unless that would push the selection off screen.
	start := 0
	for start < m.RecoveryIndex {
		used := len(lines)
		for _, block := range blocks[start : m.RecoveryIndex+1] {
			used += len(block)
		}
		if used <= height {
			break
		}
		start++
	}
	for _, block := range blocks[start:] {
		lines = append(lines, block...)
	}
	return lines
}

// dialog renders a full-screen confirmation or picker with a title bar and
// key hints. The body keeps its exact text and wraps at the terminal width.
func (m Model) dialog(title, body string, bindings []binding) string {
	lines := []string{styleDialog.Render(fit(" "+title, m.Width))}
	lines = append(lines, strings.Split(strings.TrimRight(clipLines(body, m.Width), "\n"), "\n")...)
	lines = append(lines, "", keyHints(bindings, m.Width))
	return strings.Join(lines, "\n") + "\n"
}

var confirmKeys = []binding{{"enter/y", "confirm"}, {"esc/n", "cancel"}}

func (m Model) dialogView() (string, bool) {
	switch {
	case m.WritePreview != nil:
		p := m.WritePreview
		var b strings.Builder
		fmt.Fprintf(&b, "Confirm %s on %s\nAuthority %s %s (%s) · claim %s (remains held)\n", clean(string(p.Intent.Action)), clean(p.Intent.Ref.String()), clean(p.AuthorityProfile), clean(p.Intent.AuthorityID), clean(p.Scope), clean(p.Intent.ClaimID))
		for _, resource := range p.Intent.Resources {
			fmt.Fprintf(&b, "Resource %s\n", clean(resource))
		}
		marker := p.Intent.Marker
		if marker == "" {
			marker = "none (non-append write)"
		}
		fmt.Fprintf(&b, "Provider effect %s\nSide effects %s\nDeclared races %s\nMarker %s\nLimits: no provider idempotency; lost response requires read-back, never redispatch. Claim remains held.", clean(p.Effect), clean(strings.Join(p.SideEffects, "; ")), clean(strings.Join(p.Races, "; ")), clean(marker))
		return m.dialog("Confirm provider write", b.String(), confirmKeys), true
	case m.WriteChoices != nil:
		var b strings.Builder
		for index, choice := range m.WriteChoices {
			marker := " "
			if index == m.WriteChoiceIndex {
				marker = ">"
			}
			fmt.Fprintf(&b, "%s %s → %s\n", marker, clean(choice.Label), clean(choice.Transition))
		}
		return m.dialog("Select configured provider transition", strings.TrimRight(b.String(), "\n"), []binding{{"j/k", "select"}, {"enter", "preview"}, {"esc", "dismiss"}}), true
	case m.WriteInput:
		return m.dialog("Progress note (provider append)", m.Input+"▏", []binding{{"enter", "preview"}, {"ctrl+u", "clear"}, {"esc", "cancel"}}), true
	case m.RecoveryEvidence:
		prompt := "Type NO COMMIT; EXECUTOR STOPPED: followed by provider audit evidence."
		if m.RecoveryEvidenceEntry.Status == "checkpoint-pending" {
			prompt = "Type PROVIDER VERIFIED; CHECKPOINT ABSENT; EXECUTOR STOPPED: followed by provider and authority audit evidence."
		}
		return m.dialog("Recovery attestation", prompt+"\n\n"+m.Input+"▏", []binding{{"enter", "record"}, {"esc", "cancel"}}), true
	case m.LaunchOptions != nil:
		return m.launchPickerView(), true
	case m.ClaimPreview != nil:
		return m.claimPreviewView(*m.ClaimPreview), true
	case m.StartPreview != nil:
		p := m.StartPreview
		var b strings.Builder
		fmt.Fprintf(&b, "Start work on %s (%s)\nProvider actor %s · transition %s · required %s\nEffect %s\nSide effects %s\n", clean(p.Source), clean(p.Claim.Title), clean(p.Actor), clean(p.Transition), clean(p.RequiredFields), clean(p.Effect), clean(strings.Join(p.SideEffects, "; ")))
		fmt.Fprintf(&b, "Authority %s %s (%s)\n", clean(p.Claim.AuthorityProfile), clean(p.Claim.AuthorityID), clean(p.Claim.Scope))
		for _, resource := range p.Claim.Resources {
			fmt.Fprintf(&b, "Resource %s\n", clean(resource))
		}
		fmt.Fprintf(&b, "Session %s · TTL %s · hold %s\nLimits %s\n", clean(p.Claim.SessionID), p.Claim.TTL, p.Claim.Hold, clean(p.Claim.CoordinationLimits))
		b.WriteString("Claim and provider transition are separate outcomes; no assignment or cross-system atomicity.")
		return m.dialog("Confirm Start work", b.String(), confirmKeys), true
	case m.Quitting:
		return m.exitView(), true
	case m.CancelPreview != "":
		owned := m.OwnedClaims[m.CancelPreview]
		body := fmt.Sprintf("Cancel queue-owned claim %s?\nOnly an authority-verified no-effect epoch may be released. An unverified provider checkpoint or started operation prevents cancellation.\nHandle: %s", clean(owned.ClaimID), clean(m.CancelPreview))
		return m.dialog("Cancel claim", body, confirmKeys), true
	}
	return "", false
}

func (m Model) exitView() string {
	var b strings.Builder
	b.WriteString("Renewal stops when this process exits. No claim is released automatically.\n")
	for _, owned := range m.OwnedClaims {
		claim, expiry := owned.ClaimID, "unknown"
		if claim == "" {
			claim = "outcome unknown"
		}
		if !owned.ExpiresAt.IsZero() {
			expiry = owned.ExpiresAt.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(&b, "  %s expires %s · %s · private handle %s\n", clean(claim), expiry, clean(owned.LastResult), clean(owned.Path))
	}
	if len(m.Recovery) > 0 || m.UncertainWrite {
		fmt.Fprintf(&b, "  %d unresolved writes remain in recovery; their claims stay held\n", len(m.Recovery))
	}
	if m.RecoveryError != "" {
		fmt.Fprintf(&b, "  recovery journal unreadable: %s\n", clean(m.RecoveryError))
	}
	b.WriteString("Leaving keeps leases and recovery state intact; R cancels a verified no-effect claim.")
	return m.dialog("Quit queue?", b.String(), []binding{{"enter/y", "quit"}, {"esc/n", "keep working"}})
}

func (m Model) launchPickerView() string {
	var b strings.Builder
	for i, option := range m.LaunchOptions {
		marker := " "
		if i == m.LaunchIndex {
			marker = ">"
		}
		status := "available"
		if !option.Eligibility.Eligible {
			status = strings.Join(option.Eligibility.Reasons, "; ")
		}
		fmt.Fprintf(&b, "%s %s: %s\n", marker, clean(option.Name), clean(status))
	}
	option := m.LaunchOptions[m.LaunchIndex]
	fmt.Fprintf(&b, "  Authority %s\n  Cwd %s\n  Argv %q\n  Environment names %s", clean(option.Authority), clean(option.Cwd), option.Argv, strings.Join(option.EnvNames, ", "))
	return m.dialog("Launch worker (process start is not a claim)", b.String(), []binding{{"j/k", "select"}, {"enter/y", "launch"}, {"esc/n", "dismiss"}})
}

func (m Model) claimPreviewView(preview ClaimPreview) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Claim %s for me\n", clip(preview.Title, m.Width-10))
	fmt.Fprintf(&b, "  Authority  %s %s (%s)\n", clip(preview.AuthorityProfile, 24), clip(preview.AuthorityID, 40), clean(preview.Scope))
	for _, key := range preview.Resources {
		fmt.Fprintf(&b, "  Resource   %s\n", clean(key))
	}
	fmt.Fprintf(&b, "  Session    %s, TTL %s, hold %s\n", clean(preview.SessionID), preview.TTL, preview.Hold)
	b.WriteString("  Provider   unchanged (no assignment or state change)\n")
	fmt.Fprintf(&b, "  Limits     %s\n", clean(preview.CoordinationLimits))
	b.WriteString("\nNo request is sent until confirmation.")
	return m.dialog("Confirm claim", b.String(), confirmKeys)
}

func freshnessLabel(value string) string {
	if value == "" {
		return "loading"
	}
	return value
}
