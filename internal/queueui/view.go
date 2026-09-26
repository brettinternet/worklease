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

// palette holds every style the queue renders with. Every state is also
// spelled out in text; styles only reinforce it, and NO_COLOR or a dumb
// terminal removes all styling.
type palette struct {
	faint, bold, accent, appTitle, key, good, ready, warn, warnBold, bad, held, selected, activeTab, alert, dialog lipgloss.Style
}

func style() lipgloss.Style { return lipgloss.NewStyle() }

// standardPalette uses the terminal's 16 ANSI colors so contrast follows the
// user's theme.
var standardPalette = palette{
	faint:     style().Faint(true),
	bold:      style().Bold(true),
	accent:    style().Foreground(lipgloss.Color("6")),
	appTitle:  style().Bold(true).Foreground(lipgloss.Color("6")),
	key:       style().Bold(true).Foreground(lipgloss.Color("6")),
	good:      style().Foreground(lipgloss.Color("2")),
	ready:     style().Bold(true).Foreground(lipgloss.Color("2")),
	warn:      style().Foreground(lipgloss.Color("3")),
	warnBold:  style().Bold(true).Foreground(lipgloss.Color("3")),
	bad:       style().Bold(true).Foreground(lipgloss.Color("1")),
	held:      style().Foreground(lipgloss.Color("5")),
	selected:  style().Reverse(true).Bold(true),
	activeTab: style().Reverse(true).Bold(true).Foreground(lipgloss.Color("6")),
	alert:     style().Reverse(true).Bold(true).Foreground(lipgloss.Color("1")),
	dialog:    style().Reverse(true).Bold(true).Foreground(lipgloss.Color("6")),
}

// highContrastPalette drops faint text and color, which can fall below
// readable contrast on some themes, and marks emphasis with bold, underline
// and reverse video in the theme's own foreground and background.
var highContrastPalette = palette{
	faint:     style(),
	bold:      style().Bold(true),
	accent:    style(),
	appTitle:  style().Bold(true),
	key:       style().Bold(true),
	good:      style(),
	ready:     style().Bold(true),
	warn:      style().Bold(true),
	warnBold:  style().Bold(true).Underline(true),
	bad:       style().Bold(true).Underline(true),
	held:      style().Bold(true),
	selected:  style().Reverse(true).Bold(true),
	activeTab: style().Reverse(true).Bold(true),
	alert:     style().Reverse(true).Bold(true),
	dialog:    style().Reverse(true).Bold(true),
}

// s returns the palette for the current display mode.
func (m Model) s() *palette {
	if m.HighContrast {
		return &highContrastPalette
	}
	return &standardPalette
}

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
func (p *palette) tabBar(items []tabItem, active, width int) (string, []span) {
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
		b.WriteString(p.faint.Render("‹ "))
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
			b.WriteString(p.activeTab.Render(text + " "))
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
		b.WriteString(p.faint.Render(" ›"))
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

func (m Model) split() bool {
	detail := m.Detail
	if m.ViewName == ClaimsViewID {
		detail = m.Claims.Detail
	}
	return detail && m.Width >= splitWidth
}

// paneWidths returns the list and detail widths; one separator column sits
// between them when split.
func (m Model) paneWidths() (int, int) {
	detailOpen := m.Detail
	if m.ViewName == ClaimsViewID {
		detailOpen = m.Claims.Detail
	}
	if !detailOpen {
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
	if box, ok := m.dialogView(); ok {
		return m.renderDialog(box)
	}
	return strings.Join(m.screen(), "\n")
}

// screen renders the queue without any dialog, one string per terminal line.
func (m Model) screen() []string {
	rows := m.rows()
	if m.ViewName != ClaimsViewID {
		m.anchor(rows)
	}
	f := m.frame(rows)
	var body []string
	switch m.baseMode() {
	case modeHelp:
		lines := m.helpLines()
		body = lines[max(0, min(m.HelpOffset, len(lines)-f.bodyHeight)):]
	case modeRecovery:
		body = m.recoveryLines(f.bodyHeight)
	case modeClaims:
		body = m.claimsBody(f.bodyHeight)
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
	return out
}

func (m Model) headerLine() string {
	authority := valueOr(clean(m.Authority), "unset")
	scope := valueOr(clean(m.Scope), "unknown scope")
	age := "not synced"
	ageStyle := m.s().warn
	if m.rowCache != nil && !m.rowCache.observed.IsZero() {
		age, ageStyle = "synced "+ago(time.Since(m.rowCache.observed)), m.s().faint
	}
	healthyCount := healthy(m.Snapshot.Sources)
	sourceStyle := m.s().good
	if healthyCount < len(m.Sources) {
		sourceStyle = m.s().warnBold
	}
	build := func(authority string, withMe, withAge bool) []seg {
		label := "  authority: "
		if !withMe {
			label = "  "
		}
		segs := []seg{{"worklease queue", m.s().appTitle}, {label, m.s().faint}, {authority + " (" + scope + ")", lipgloss.NewStyle()}}
		if withMe && m.Me != "" {
			segs = append(segs, seg{"  me: ", m.s().faint}, seg{clean(m.Me), lipgloss.NewStyle()})
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
	if name == ClaimsViewID {
		return len(m.Claims.Items)
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
		style := m.s().faint
		if name == RecoveryViewID && count > 0 {
			style = m.s().bad
		} else if name == ClaimsViewID && m.Claims.Stale {
			style = m.s().warnBold
		}
		items = append(items, tabItem{label: clip(viewLabel(name), 20), suffix: fmt.Sprint(count), suffixStyle: style})
		if name == m.ViewName {
			active = n
		}
	}
	return m.s().tabBar(items, active, m.Width)
}

var sourceHints = map[string]string{
	"cached-index":            "showing cached rows while refreshing",
	"observation-invalidated": "provider changed during read; r retries",
}

func (m Model) banners() []string {
	var out []string
	if m.ViewName == ClaimsViewID && m.Claims.Notice != "" {
		out = append(out, m.s().accent.Render(clip(m.Claims.Notice, m.Width)))
	}
	if m.ViewName == ClaimsViewID && m.Claims.Stale {
		text := "Claims are stale"
		if m.Claims.Error != "" {
			text += ": " + m.Claims.Error
		}
		out = append(out, m.s().warnBold.Render(clip(text, m.Width)))
	}
	if m.ViewName == ClaimsViewID && m.Claims.EventsError != "" {
		out = append(out, m.s().warn.Render(clip("Lifecycle events unavailable: "+m.Claims.EventsError, m.Width)))
	}
	if m.UncertainWrite || len(m.Recovery) > 0 {
		text := fmt.Sprintf(" RECOVERY REQUIRED: %d unresolved writes; claims held (do not release until verified)", len(m.Recovery))
		if m.UncertainWrite {
			text += "; last write unverified"
		}
		out = append(out, m.s().alert.Render(pad(text, m.Width)))
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
		out = append(out, m.s().warnBold.Render("Sources ")+m.s().warn.Render(strings.Join(problems, " · ")))
	}
	if len(m.recheckingReady) > 0 && (m.ViewName == "Ready" || m.ViewRules[m.ViewName].Readiness == string(queue.Ready)) {
		out = append(out, m.s().warn.Render("Previously ready rows are rechecking; actions require current readiness."))
	}
	return out
}

func (m Model) footerLines(rows []queue.Item) []string {
	if m.ViewName == ClaimsViewID {
		return m.claimsFooterLines()
	}
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
	left := []seg{{fmt.Sprintf("%d shown", len(rows)), m.s().bold}, {" · " + coverage + fmt.Sprintf(" · edges %d/%d", edges, loaded), m.s().faint}}
	if m.Filter != "" {
		left = append(left, seg{" · ", m.s().faint}, seg{"filter ", m.s().accent}, seg{fmt.Sprintf("%q", clip(m.Filter, 32)), m.s().bold}, seg{" (loaded rows)", m.s().faint})
	}
	provider, claims := sourceFreshness(m.Snapshot), freshnessLabel(m.ClaimFreshness)
	right := []seg{{"provider ", m.s().faint}, {provider, m.s().freshnessStyle(provider)}, {" · claims ", m.s().faint}, {claims, m.s().freshnessStyle(claims)}}
	var status string
	if gap := m.Width - segsWidth(left) - segsWidth(right); gap >= 2 {
		status = renderSegs(left) + strings.Repeat(" ", gap) + renderSegs(right)
	} else {
		// Narrow: keep the shown count and freshness; coverage detail is in wider layouts.
		status = renderSegs(append([]seg{left[0], {" · ", m.s().faint}}, right...))
	}
	lines := []string{status}
	if m.Notice != "" {
		// Notices get their own line so holder, expiry, and recovery paths stay readable.
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

func (p *palette) freshnessStyle(value string) lipgloss.Style {
	switch value {
	case "fresh":
		return p.good
	case "loading", "rebuilding":
		return p.faint
	default:
		return p.warnBold
	}
}

// noticeStyle calls out notices that report a failure or an unresolved
// outcome; the notice text itself always carries the meaning.
func (p *palette) noticeStyle(notice string) lipgloss.Style {
	lower := strings.ToLower(notice)
	for _, word := range []string{"fail", "refused", "unavailable", "uncertain", "unknown", "lost", "recovery", "contended", "expired", "rejected"} {
		if strings.Contains(lower, word) {
			return p.warnBold
		}
	}
	return p.accent
}

type binding struct{ keys, desc string }

func (p *palette) keyHints(bindings []binding, width int) string {
	render := func(b binding) string { return p.key.Render(b.keys) + " " + p.faint.Render(b.desc) }
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
	return m.s().keyHints(m.bindings(), m.Width)
}

// bindings lists the keys that act in the current mode, most useful first.
func (m Model) bindings() []binding {
	var bindings []binding
	switch m.mode() {
	case modeFilter:
		if m.ViewName == ClaimsViewID {
			bindings = []binding{{"enter", "apply prefix"}, {"ctrl+u", "clear"}, {"esc", "cancel"}}
		} else {
			bindings = []binding{{"enter", "apply"}, {"ctrl+u", "clear"}, {"esc", "cancel"}}
		}
	case modePalette:
		bindings = []binding{{"start work", "command"}, {"enter", "run"}, {"esc", "cancel"}}
	case modeHelp:
		bindings = []binding{{"^f/b d/u e/y", "scroll help"}, {"esc/?", "close help"}}
	case modeRecovery:
		bindings = []binding{{"j/k", "select"}, {"r", "retry read-back"}, {"e", "attest"}, {"u", "reload"}, {"v/1-9", "view"}, {"q", "quit"}, {"?", "help"}}
	case modeClaims:
		if m.Claims.Detail {
			bindings = []binding{{"j/k", "select"}, {"tab", "section"}, {"pgup/pgdn", "scroll"}, {"i", "open queue item"}, {"esc", "back"}, {"v/1-9", "view"}, {"q", "quit"}, {"?", "help"}}
		} else {
			bindings = []binding{{"j/k", "select"}, {"enter", "detail"}, {"/", "resource prefix"}, {"m", "mine/all"}, {"e", "expiring"}, {"s", "stale"}, {"i", "queue item"}, {"r", "refresh"}, {"v/1-9", "view"}, {"q", "quit"}, {"?", "help"}}
		}
	case modeList:
		if m.Detail {
			return []binding{{"tab", "section"}, {"^f/b d/u e/y", "scroll text"}, {"c", "claim"}, {"S", "start"}, {"s", "state"}, {"p", "progress"}, {"a", "assign"}, {"o", "open"}, {"esc", "back"}, {"?", "help"}}
		}
		bindings = []binding{{"j/k", "move"}, {"^f/b d/u e/y", "scroll list"}, {"enter", "open"}, {"/", "filter"}, {"v/1-9", "view"}, {"c", "claim"}, {"x", "launch"}, {"r", "refresh"}, {"q", "quit"}, {"?", "help"}}
		if m.Filter != "" {
			bindings = slices.Insert(bindings, 3, binding{"esc", "clear filter"})
		}
	default:
		box, _ := m.dialogView()
		bindings = box.keys
	}
	return bindings
}

var helpGroups = []struct {
	title    string
	bindings []binding
}{
	{"Navigate", []binding{{"j/k ↓/↑", "move selection"}, {"n / N", "next / previous row"}, {"gg / G", "first / last row"}, {"H / M / L", "top / middle / bottom visible row"}, {"zz / zt / zb", "center / top / bottom selected row"}, {"^f / ^b", "scroll one page forward / back"}, {"^d / ^u", "scroll half page down / up"}, {"^e / ^y", "scroll one line down / up"}, {"pgdn / pgup", "scroll one page"}, {"enter l →", "open detail"}, {"esc h ←", "back / close"}, {"tab ⇧tab", "next / previous detail section"}}},
	{"Views and filter", []binding{{"v / V", "next / previous view"}, {"1-9", "jump to view"}, {"/", "filter loaded rows or Claims resource prefix"}, {"Claims", "authority-wide; Claimed filters items"}, {"esc", "clear filter"}}},
	{"Item actions (preview first)", []binding{{"c", "claim for me"}, {"S", "start work: claim + transition (detail)"}, {"s", "change provider state"}, {"p", "record progress note"}, {"a", "assign to me"}, {"R", "release verified no-effect claim"}, {"x", "launch worker"}, {"o", "open provider URL"}, {"m", "load more comments / claim history"}}},
	{"Queue", []binding{{"r", "refresh sources"}, {":", "command palette (start work)"}, {"?", "toggle help"}, {"q", "quit"}}},
	{"Mouse", []binding{{"click", "select row; click again to open"}, {"click tab", "switch view or detail section"}, {"wheel", "scroll list or detail"}, {"shift+drag", "select text (option+drag in iTerm2)"}}},
}

func (m Model) helpLines() []string {
	var groups [][]string
	for _, group := range helpGroups {
		if m.ViewName == ClaimsViewID && (group.title == "Item actions (preview first)" || group.title == "Queue") {
			continue
		}
		lines := []string{m.s().bold.Render(group.title)}
		for _, b := range group.bindings {
			lines = append(lines, "  "+m.s().key.Render(fit(b.keys, 11))+" "+b.desc)
		}
		groups = append(groups, append(lines, ""))
	}
	if m.ViewName == ClaimsViewID {
		claims := []string{m.s().bold.Render("Claims tab · authority-wide, read-only")}
		for _, b := range []binding{{"j/k", "select claim"}, {"enter", "open holder, session, expiry and checkpoint status"}, {"/", "filter by resource prefix"}, {"m", "toggle mine / all"}, {"e", "toggle expiring"}, {"s", "toggle stale"}, {"i", "jump to matching queue item"}, {"r", "refresh list and event cursor"}, {"q", "quit"}} {
			claims = append(claims, "  "+m.s().key.Render(fit(b.keys, 11))+" "+b.desc)
		}
		groups = append(groups, append(claims, ""))
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
		{title: "ID", min: idWidth, max: idWidth, cell: func(i queue.Item) (string, lipgloss.Style) { return i.Ref.ItemID, m.s().accent }},
		{title: "Title", min: 16, cell: func(i queue.Item) (string, lipgloss.Style) { return i.Title, lipgloss.NewStyle() }},
		{title: "Status", min: 6, max: 16, cell: func(i queue.Item) (string, lipgloss.Style) { return projectStatusDisplay(i), m.s().stateStyle(i.State) }},
		{title: "Ready", min: 8, max: 20, cell: m.readyCell},
		{title: "Assigned", min: 6, max: 14, cell: m.assignedCell},
		{title: "Native", min: 6, max: 12, cell: func(i queue.Item) (string, lipgloss.Style) { return i.NativeClaim, m.s().faint }},
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

func (p *palette) stateStyle(state queue.StateCategory) lipgloss.Style {
	switch state {
	case queue.StateInProgress:
		return p.accent
	case queue.StateBlocked:
		return p.bad
	case queue.StateComplete:
		return p.faint
	}
	return lipgloss.NewStyle()
}

var readyAliases = map[string]string{"assigned elsewhere": "elsewhere", "unknown dependencies": "deps unknown"}

func (m Model) readyCell(i queue.Item) (string, lipgloss.Style) {
	if m.readyDuringRecheck(i) {
		return "rechecking", m.s().warn
	}
	if i.Claim.Active && m.ownsClaim(i) {
		return "mine", m.s().ready
	}
	state := m.displayState(i)
	switch state {
	case "ready":
		return state, m.s().ready
	case "blocked", "denied":
		return state, m.s().bad
	case "occupied":
		return state, m.s().held
	case "assigned elsewhere":
		return state, m.s().faint
	case "stale", "unknown dependencies", "unknown":
		return state, m.s().warn
	}
	return state, lipgloss.NewStyle()
}

func (m Model) assignedCell(i queue.Item) (string, lipgloss.Style) {
	if len(i.AssignedTo) == 0 {
		return "—", m.s().faint
	}
	for _, name := range i.AssignedTo {
		if m.isMe(i, name) {
			return strings.Join(i.AssignedTo, ","), m.s().bold
		}
	}
	return strings.Join(i.AssignedTo, ","), lipgloss.NewStyle()
}

func (m Model) claimCell(i queue.Item) (string, lipgloss.Style) {
	if m.ownsClaim(i) {
		return "mine", m.s().ready
	}
	state := claimState(i)
	switch {
	case i.Claim.Stale || i.Claim.Reason != "":
		return state, m.s().warn
	case i.Claim.Active:
		return state, m.s().held
	}
	return state, m.s().faint
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
	lines := []string{m.s().bold.Render("  " + strings.Join(header, strings.Repeat(" ", columnGap)))}
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
			lines = append(lines, m.s().selected.Render(pad("> "+strings.Join(cells, gap), width)))
			continue
		}
		cells := make([]string, 0, len(cols))
		for _, c := range cols {
			text, style := c.cell(item)
			if item.Terminal {
				style = m.s().faint
			}
			cells = append(cells, style.Render(m.cellText(c, text)))
		}
		lines = append(lines, "  "+strings.Join(cells, gap))
	}
	return lines
}

// emptyLines explains why a view has no rows instead of showing a blank list.
func (m Model) emptyLines(width int) []string {
	lines := []string{"", "  " + m.s().bold.Render("No items in "+clip(viewLabel(m.ViewName), 24)+".")}
	if len(m.Snapshot.Items) == 0 {
		lines = append(lines, "  "+m.s().faint.Render("Nothing loaded yet; source status is shown above."))
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
		lines = append(lines, "  "+m.s().accent.Render(clip(fmt.Sprintf("Filter %q is active; esc clears it.", m.Filter), width-2)))
	}
	if counts["stale"] > 0 || sourceFreshness(m.Snapshot) != "fresh" {
		lines = append(lines, "  "+m.s().warn.Render("Stale rows are never ready; r refreshes sources."))
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
	return splitBody(list, pane, listWidth, height, m.s().faint.Render("│"))
}

func splitBody(list, pane []string, listWidth, height int, separator string) []string {
	out := make([]string, height)
	for n := range out {
		left, right := "", ""
		if n < len(list) {
			left = list[n]
		}
		if n < len(pane) {
			right = pane[n]
		}
		out[n] = pad(left, listWidth) + separator + right
	}
	return out
}

var (
	tabNames      = []string{"Summary", "Dependencies", "Activity", "Claims", "Recovery"}
	shortTabNames = []string{"Summary", "Deps", "Activity", "Claims", "Recovery"}
)

// detailTabs prefers full section names, then short names, then a window.
func (m Model) detailTabs(active, width int) (string, []span) {
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
	return m.s().tabBar(items, active, width)
}

// detailContent renders the scrollable part of the detail pane.
func (m Model) detailContent(item queue.Item, width int) []string {
	cached, rechecking := m.displayDetails[item.Ref.Key()]
	if rechecking {
		// Fill only the presentation copy. The snapshot item used by action
		// handlers remains unverified and cannot borrow these older details.
		if item.Body == "" {
			item.Body = cached.Body
		}
		if len(item.Relationships) == 0 {
			item.Relationships = append([]queue.Relationship(nil), cached.Relationships...)
			for n := range item.Relationships {
				item.Relationships[n].Fresh = false
			}
		}
		if len(item.Resources) == 0 {
			item.Resources = cached.Resources
		}
		if item.Observation.ProviderVersion == "" {
			item.Observation.ProviderVersion = cached.Observation.ProviderVersion
		}
	}
	lines := detail(m, item)
	if rechecking {
		lines = append([]dline{{text: "Previously observed details · rechecking; actions require current evidence", style: m.s().warn}}, lines...)
	}
	return m.s().renderDetail(lines, max(1, width-1))
}

func (m Model) detailPane(item queue.Item, width, height int) []string {
	title := m.s().accent.Bold(true).Render(clip(item.Ref.ItemID, 30)) + " " + m.s().bold.Render(clip(item.Title, max(1, width-ansi.StringWidth(clip(item.Ref.ItemID, 30))-2)))
	tabs, _ := m.detailTabs(m.Tab, width-1)
	lines := []string{" " + title, " " + tabs}
	content := m.detailContent(item, width)
	visible := max(0, height-len(lines))
	offset := max(0, min(m.DetailOffset, len(content)-visible))
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

func (p *palette) renderDetail(lines []dline, width int) []string {
	labelWidth := 0
	for _, l := range lines {
		labelWidth = max(labelWidth, ansi.StringWidth(l.label))
	}
	labelWidth = min(labelWidth, 16)
	var out []string
	for _, l := range lines {
		if l.label == "" || width-labelWidth-1 < 12 {
			if l.label != "" {
				out = append(out, p.bold.Render(clip(l.label, width)))
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
				prefix = p.bold.Render(fit(l.label, labelWidth)) + " "
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
			add("Issue state", i.RawStatus, m.s().stateStyle(i.State))
			add("Project status", projectStatusRaw(i)+" → "+projectStatusState(i), plainStyle)
			if i.ProjectStatusConflict {
				add("", "CONFLICT: issue and project status disagree", m.s().bad)
			}
		} else {
			add("Status", valueOr(i.RawStatus, "unknown"), m.s().stateStyle(i.State))
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
			add("Actions", "S start work (claim + provider transition) · c claim only · : start work", m.s().accent)
		} else {
			add("Actions", "c claim only (no supported Start work mapping)", m.s().accent)
		}
	case 1:
		add("Readiness", fmt.Sprintf("%s: %s", i.Readiness.Status, strings.Join(i.Readiness.Reasons, "; ")), plainStyle)
		add("Closure", fmt.Sprintf("coverage %s · freshness %s", i.Closure, i.Readiness.Freshness), plainStyle)
		if len(i.Relationships) == 0 {
			add("", "", plainStyle)
			add("", "No dependency relationships observed.", m.s().faint)
		}
		for _, r := range i.Relationships {
			style := plainStyle
			if !r.Fresh {
				style = m.s().warn
			}
			add("", "", plainStyle)
			add("", fmt.Sprintf("%s → %s", r.Type, clean(r.To.String())), m.s().bold)
			add("Requires", fmt.Sprintf("%s · observed: %s", r.Condition, r.RawOutcome), plainStyle)
			add("Evidence", fmt.Sprintf("%s · provenance: %s · fresh: %t · support: %s", r.Interpretation, r.Provenance, r.Fresh, r.Support), style)
		}
	case 2:
		add("Observed", i.Observation.ObservedAt.Format(time.RFC3339), plainStyle)
		add("Version", valueOr(clip(i.Observation.ProviderVersion, 32), "—"), plainStyle)
		add("Read", fmt.Sprintf("%s · coverage %s", valueOr(clip(i.ReadOutcome, 20), "—"), i.Coverage.State), plainStyle)
		add("", "", plainStyle)
		if m.LoadComments == nil {
			add("", "Comments: provider detail unavailable", m.s().faint)
			break
		}
		if m.CommentsLoading {
			add("", "Loading comments…", m.s().faint)
		}
		if m.CommentsError != "" {
			add("", "Comments unavailable: "+clip(m.CommentsError, 100), m.s().warn)
		}
		if m.CommentsIdentity == DetailRequestIdentity(i) {
			for _, comment := range m.Comments {
				add("", clip(comment.Author, 32)+" · "+comment.CreatedAt.Format(time.RFC3339), m.s().bold)
				add("", clip(comment.Body, min(m.Width*4, 800)), plainStyle)
				add("", "", plainStyle)
			}
		}
		if m.CommentsCursor != "" {
			add("", "Press m for more comments", m.s().accent)
		}
	case 3:
		add("Authority", fmt.Sprintf("%s (%s)", m.Authority, m.Scope), plainStyle)
		add("Resource", displayResources(i.Resources), plainStyle)
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
				add("", "Claim lost; actions disabled", m.s().bad)
			case !owned.Verified:
				add("", "Ownership unverified; actions disabled", m.s().warn)
			default:
				if _, rechecking := m.displayDetails[i.Ref.Key()]; rechecking {
					add("", "Ownership rechecking; actions require current evidence", m.s().warn)
				} else {
					add("", "R: cancel if no operation or provider write started", m.s().accent)
				}
			}
		}
		if m.HistoryLoading {
			add("", "Loading claim epochs…", m.s().faint)
		}
		if m.HistoryError != "" {
			add("", "History unavailable: "+clip(m.HistoryError, 100), m.s().warn)
		}
		if m.HistoryIdentity == DetailRequestIdentity(i) {
			if m.History.Gap {
				add("", "History gap: earlier epochs pruned", m.s().warn)
			}
			for _, e := range m.History.Epochs {
				ended := "active"
				if e.EndedAt != nil {
					ended = e.EndedAt.Format(time.RFC3339)
				}
				add("", "", plainStyle)
				add("", fmt.Sprintf("%s · %s → %s", clip(e.Status, 20), e.AcquiredAt.Format(time.RFC3339), ended), m.s().bold)
				add("Agent", e.AgentID, plainStyle)
				add("Session", e.SessionID, plainStyle)
				add("Reason", clip(e.EndReason, 60), plainStyle)
			}
			if m.History.PreviousCursor != "" {
				add("", "Older retained epochs available (m to load)", m.s().accent)
			}
		}
	case 4:
		if m.RecoveryError != "" {
			add("", "Recovery unavailable: "+clip(m.RecoveryError, 100), m.s().bad)
		}
		found := false
		for _, entry := range m.Recovery {
			if entry.Ref != i.Ref {
				continue
			}
			found = true
			add("", fmt.Sprintf("%s · %s · %s · claim held %s", entry.OperationID, entry.Action, entry.Status, entry.ClaimID), m.s().warnBold)
			add("Resources", fmt.Sprintf("%s · actor %s · effect %s", displayResources(entry.Resources), entry.Principal, entry.Effect), plainStyle)
			add("Marker", fmt.Sprintf("%s · required effects %s", entry.Marker, strings.Join(entry.Effects, ", ")), plainStyle)
			add("Dispatched", fmt.Sprintf("%s · read-back %s", recoveryTime(entry.Dispatched), entry.Readback), plainStyle)
			add("Next", strings.Join(entry.Next, "; "), m.s().accent)
			add("", "", plainStyle)
		}
		if !found && m.RecoveryError == "" {
			add("", "No unresolved writes for this item.", m.s().faint)
		}
	}
	return d
}

func (m Model) recoveryLines(height int) []string {
	heading := m.s().bold.Render(fmt.Sprintf("%d unresolved writes", len(m.Recovery)))
	if m.RecoveryError != "" {
		heading += " · " + m.s().bad.Render(clean(m.RecoveryError))
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
			first = m.s().selected.Render(pad(clip(first, m.Width), m.Width))
		} else {
			first = m.s().warnBold.Render(clip(first, m.Width))
		}
		block := []string{first}
		for _, detail := range []string{
			fmt.Sprintf("resources %s · actor %s · effect %s", displayResources(entry.Resources), entry.Principal, entry.Effect),
			fmt.Sprintf("marker %s · required effects %s", entry.Marker, strings.Join(entry.Effects, ", ")),
			fmt.Sprintf("dispatched %s · read-back %s", recoveryTime(entry.Dispatched), entry.Readback),
		} {
			for _, text := range wrap(detail, m.Width-4) {
				block = append(block, "    "+text)
			}
		}
		for _, text := range wrap("next "+strings.Join(entry.Next, "; "), m.Width-4) {
			block = append(block, "    "+m.s().accent.Render(text))
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

// dialogBox is a confirmation, picker, or prompt. Its body keeps exact
// text; rendering only wraps it.
type dialogBox struct {
	title, body string
	keys        []binding
}

func (m Model) dialog(title, body string, keys []binding) dialogBox {
	return dialogBox{title, body, keys}
}

// maxDialogWidth keeps dialog lines short enough to read in one pass.
const maxDialogWidth = 96

// renderDialog draws the dialog as a bordered box over the dimmed queue, so
// the list and the selected row stay in view. When the box cannot fit, it
// falls back to a full-screen dialog.
func (m Model) renderDialog(box dialogBox) string {
	width := min(m.Width-4, maxDialogWidth)
	inner := width - 4
	body := strings.Split(strings.TrimRight(clipLines(box.body, max(1, inner)), "\n"), "\n")
	height := len(body) + 4 // borders, blank line, key hints
	if inner < 40 || height > m.Height-2 {
		lines := []string{m.s().dialog.Render(fit(" "+box.title, m.Width))}
		lines = append(lines, strings.Split(strings.TrimRight(clipLines(box.body, m.Width), "\n"), "\n")...)
		lines = append(lines, "", m.s().keyHints(box.keys, m.Width))
		return strings.Join(lines, "\n") + "\n"
	}
	border := m.s().bold
	title := " " + clip(box.title, inner-2) + " "
	boxLines := []string{border.Render("╭─") + m.s().appTitle.Render(title) + border.Render(strings.Repeat("─", width-3-ansi.StringWidth(title))+"╮")}
	side := border.Render("│")
	for _, line := range append(body, "", m.s().keyHints(box.keys, inner)) {
		boxLines = append(boxLines, side+" "+pad(line, inner)+" "+side)
	}
	boxLines = append(boxLines, border.Render("╰"+strings.Repeat("─", width-2)+"╯"))

	background := m.screen()
	for len(background) < m.Height {
		background = append(background, "")
	}
	top := m.dialogTop(len(boxLines))
	left := (m.Width - width) / 2
	for n, line := range background {
		plain := pad(ansi.Strip(line), m.Width)
		if n < top || n >= top+len(boxLines) {
			background[n] = m.s().faint.Render(plain)
			continue
		}
		background[n] = m.s().faint.Render(ansi.Cut(plain, 0, left)) + boxLines[n-top] + m.s().faint.Render(ansi.Cut(plain, left+width, m.Width))
	}
	return strings.Join(background, "\n")
}

// dialogTop centers a dialog of height lines, moving it off the selected
// row when there is room above or below.
func (m Model) dialogTop(height int) int {
	top := (m.Height - height) / 2
	if m.baseMode() != modeList {
		return top
	}
	rows := m.rows()
	f := m.frame(rows)
	selected := f.bodyTop + 1 + m.Index - m.listOffset(len(rows), m.listCapacity(rows))
	if len(rows) == 0 || selected < top || selected >= top+height {
		return top
	}
	if selected+1+height <= m.Height {
		return selected + 1
	}
	return max(0, selected-height)
}

var confirmKeys = []binding{{"enter/y", "confirm"}, {"esc/n", "cancel"}}

func (m Model) dialogView() (dialogBox, bool) {
	switch m.mode() {
	case modeWritePreview:
		p := m.WritePreview
		var b strings.Builder
		fmt.Fprintf(&b, "Confirm %s on %s\nAuthority %s %s (%s) · claim %s (remains held)\n", clean(string(p.Intent.Action)), clean(p.Intent.Ref.String()), clean(p.AuthorityProfile), clean(p.Intent.AuthorityID), clean(p.Scope), clean(p.Intent.ClaimID))
		for _, resource := range p.Intent.Resources {
			fmt.Fprintf(&b, "Resource %s\n", displayResource(resource))
		}
		marker := p.Intent.Marker
		if marker == "" {
			marker = "none (non-append write)"
		}
		fmt.Fprintf(&b, "Provider effect %s\nSide effects %s\nDeclared races %s\nMarker %s\nLimits: no provider idempotency; lost response requires read-back, never redispatch. Claim remains held.", clean(p.Effect), clean(strings.Join(p.SideEffects, "; ")), clean(strings.Join(p.Races, "; ")), clean(marker))
		return m.dialog("Confirm provider write", b.String(), confirmKeys), true
	case modeWriteChoices:
		var b strings.Builder
		for index, choice := range m.WriteChoices {
			marker := " "
			if index == m.WriteChoiceIndex {
				marker = ">"
			}
			fmt.Fprintf(&b, "%s %s → %s\n", marker, clean(choice.Label), clean(choice.Transition))
		}
		return m.dialog("Select configured provider transition", strings.TrimRight(b.String(), "\n"), []binding{{"j/k", "select"}, {"enter", "preview"}, {"esc", "dismiss"}}), true
	case modeWriteInput:
		return m.dialog("Progress note (provider append)", m.Input+"▏", []binding{{"enter", "preview"}, {"ctrl+u", "clear"}, {"esc", "cancel"}}), true
	case modeRecoveryEvidence:
		prompt := "Type NO COMMIT; EXECUTOR STOPPED: followed by provider audit evidence."
		if m.RecoveryEvidenceEntry.Status == "checkpoint-pending" {
			prompt = "Type PROVIDER VERIFIED; CHECKPOINT ABSENT; EXECUTOR STOPPED: followed by provider and authority audit evidence."
		}
		return m.dialog("Recovery attestation", prompt+"\n\n"+m.Input+"▏", []binding{{"enter", "record"}, {"esc", "cancel"}}), true
	case modeLaunch:
		return m.launchPickerView(), true
	case modeClaimPreview:
		return m.claimPreviewView(*m.ClaimPreview), true
	case modeStartPreview:
		p := m.StartPreview
		var b strings.Builder
		fmt.Fprintf(&b, "Start work on %s (%s)\nProvider actor %s · transition %s · required %s\nEffect %s\nSide effects %s\n", clean(p.Source), clean(p.Claim.Title), clean(p.Actor), clean(p.Transition), clean(p.RequiredFields), clean(p.Effect), clean(strings.Join(p.SideEffects, "; ")))
		fmt.Fprintf(&b, "Authority %s %s (%s)\n", clean(p.Claim.AuthorityProfile), clean(p.Claim.AuthorityID), clean(p.Claim.Scope))
		for _, resource := range p.Claim.Resources {
			fmt.Fprintf(&b, "Resource %s\n", displayResource(resource))
		}
		fmt.Fprintf(&b, "Session %s · TTL %s · hold %s\nLimits %s\n", clean(p.Claim.SessionID), p.Claim.TTL, p.Claim.Hold, clean(p.Claim.CoordinationLimits))
		b.WriteString("Claim and provider transition are separate outcomes; no assignment or cross-system atomicity.")
		return m.dialog("Confirm Start work", b.String(), confirmKeys), true
	case modeQuit:
		return m.exitView(), true
	case modeCancel:
		owned := m.OwnedClaims[m.CancelPreview]
		body := fmt.Sprintf("Cancel queue-owned claim %s?\nOnly an authority-verified no-effect epoch may be released. An unverified provider checkpoint or started operation prevents cancellation.\nHandle: %s", clean(owned.ClaimID), clean(m.CancelPreview))
		return m.dialog("Cancel claim", body, confirmKeys), true
	}
	return dialogBox{}, false
}

func (m Model) exitView() dialogBox {
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

func (m Model) launchPickerView() dialogBox {
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

func (m Model) claimPreviewView(preview ClaimPreview) dialogBox {
	var b strings.Builder
	fmt.Fprintf(&b, "Claim %s for me\n", clip(preview.Title, m.Width-10))
	fmt.Fprintf(&b, "  Authority  %s %s (%s)\n", clip(preview.AuthorityProfile, 24), clip(preview.AuthorityID, 40), clean(preview.Scope))
	for _, key := range preview.Resources {
		fmt.Fprintf(&b, "  Resource   %s\n", displayResource(key))
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
