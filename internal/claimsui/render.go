package claimsui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"slices"
	"strings"
	"time"
)

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
const splitWidth = 120

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

func spread(left, right []seg, width int) string {
	for len(left) > 1 && segsWidth(left)+2+segsWidth(right) > width {
		left = left[:len(left)-1]
		if n := len(left); n > 1 && strings.TrimSpace(left[n-1].text) != left[n-1].text {
			left = left[:n-1] // drop the now-dangling label
		}
	}
	if segsWidth(left)+2+segsWidth(right) > width {
		return renderSegs(left)
	}
	if len(right) == 0 {
		return renderSegs(left)
	}
	return renderSegs(left) + strings.Repeat(" ", width-segsWidth(left)-segsWidth(right)) + renderSegs(right)

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

type binding struct{ keys, desc string }

// actionLine lists the actions available here, in priority order, as many
// as fit in width.
func (p *palette) actionLine(bindings []binding, width int) string {
	var parts []string
	used := 0
	for _, b := range bindings {
		cost := ansi.StringWidth(b.keys) + 1 + ansi.StringWidth(b.desc)
		if len(parts) > 0 {
			cost += 2
		}
		if used+cost > width {
			break
		}
		used += cost
		parts = append(parts, p.key.Render(b.keys)+" "+p.faint.Render(b.desc))
	}
	return strings.Join(parts, "  ")
}

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

type column[T any] struct {
	title    string
	min, max int
	width    int
	flex     bool
	alias    map[string]string
	cell     func(T) (string, lipgloss.Style)
}

// fitColumns sizes cols to available cells so rows never wrap. The titles
// in drop go first, in order, until the flex column fits its minimum — a
// narrow table of whole values reads better than many clipped ones — then
// the remaining columns shrink toward their minimums from the right.
func fitColumns[T any](cols []column[T], available int, drop []string) []column[T] {
	flex := func() int { return slices.IndexFunc(cols, func(c column[T]) bool { return c.flex }) }
	fixed := func() int {
		n := 0
		for _, c := range cols {
			if !c.flex {
				n += c.width + columnGap
			}
		}
		return n
	}
	for n := range cols {
		cols[n].width = cols[n].max
	}
	for _, title := range drop {

		if available-fixed() >= cols[flex()].min {
			break
		}
		cols = slices.DeleteFunc(cols, func(c column[T]) bool { return c.title == title })
	}
	for need := cols[flex()].min - (available - fixed()); need > 0; {
		shrunk := false
		for n := len(cols) - 1; n >= 0 && need > 0; n-- {
			if c := cols[n]; !c.flex && c.width > c.min {
				cols[n].width--
				need--
				shrunk = true
			}
		}
		if !shrunk {
			break
		}
	}
	cols[flex()].width = max(1, available-fixed())
	return cols
}

// fitContent caps each sizable column at its widest title or seen value.
func fitContent[T any](cols []column[T], seen map[string]int) {
	for n, c := range cols {
		if c.flex || c.min == c.max {
			continue
		}
		cols[n].max = min(c.max, max(ansi.StringWidth(c.title), seen[c.title]))
		cols[n].min = min(c.min, cols[n].max)
	}
}

func cellWidths[T any](cols []column[T], rows []T) map[string]int {
	widths := map[string]int{}
	for _, row := range rows {
		for _, c := range cols {
			if !c.flex {
				text, _ := c.cell(row)
				widths[c.title] = max(widths[c.title], ansi.StringWidth(clean(text)))
			}
		}
	}
	return widths
}

func (c column[T]) text(value string) string {
	if alias, ok := c.alias[value]; ok && ansi.StringWidth(value) > c.width {
		value = alias
	}
	return fit(value, c.width)
}

func tableHeader[T any](p *palette, cols []column[T]) string {
	header := make([]string, 0, len(cols))
	for _, c := range cols {
		header = append(header, fit(c.title, c.width))
	}
	return p.bold.Render("  " + strings.Join(header, strings.Repeat(" ", columnGap)))
}

// tableRow renders one row. A selected row is reverse video with a textual
// marker; a muted row renders every cell faint.
func tableRow[T any](p *palette, cols []column[T], row T, selected, muted bool, width int) string {
	cells := make([]string, 0, len(cols))
	for _, c := range cols {
		text, style := c.cell(row)
		switch {
		case selected:
			cells = append(cells, c.text(text))
		case muted:
			cells = append(cells, p.faint.Render(c.text(text)))
		default:
			cells = append(cells, style.Render(c.text(text)))
		}
	}
	line := strings.Join(cells, strings.Repeat(" ", columnGap))
	if selected {
		return p.selected.Render(pad("> "+line, width))
	}
	return "  " + line
}

// columns fits the claims list list columns to width. Fixed columns size to their
// content up to a cap, so values stay whole as long as space allows.

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

func (p *palette) scrollWindow(content []string, offset, visible int) []string {
	visible = max(0, visible)
	offset = max(0, min(offset, len(content)-visible))
	end := min(len(content), offset+visible)
	shown := append([]string(nil), content[offset:end]...)
	if end < len(content) && len(shown) > 0 {
		shown[len(shown)-1] = p.faint.Render(fmt.Sprintf("  ↓ %d more lines", len(content)-end+1))
	}
	for n, line := range shown {
		shown[n] = " " + line
	}
	return shown
}

// itemActions lists the actions this claims list can take on the item, most
// common first. Claiming is offered only for unclaimed, unfinished work.

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

type dialogBox struct {
	title, body string
	keys        []binding
}

func (m Model) dialog(title, body string, keys []binding) dialogBox {
	return dialogBox{title, body, keys}
}

// maxDialogWidth keeps dialog lines short enough to read in one pass.
const maxDialogWidth = 96

// renderDialog draws the dialog as a bordered box over the dimmed claims list, so
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
