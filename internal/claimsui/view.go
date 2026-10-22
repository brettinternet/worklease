package claimsui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

type frame struct {
	top, footer         []string
	bodyTop, bodyHeight int
}

func (m Model) frame() frame {
	f := frame{top: []string{m.headerLine(), m.s().bold.Render("Claims")}}
	if m.Claims.Error != "" {
		f.top = append(f.top, m.s().warn.Render(clip(m.Claims.Error, m.Width)))
	}
	if m.Claims.EventsError != "" {
		f.top = append(f.top, m.s().warn.Render(clip(m.Claims.EventsError, m.Width)))
	}
	f.footer = m.footerLines()
	f.bodyTop = len(f.top)
	f.bodyHeight = max(0, m.Height-len(f.top)-len(f.footer))
	return f
}
func (m Model) paneWidths() (int, int) {
	if !m.Claims.Detail {
		return m.Width, 0
	}
	if m.Width < splitWidth {
		return 0, m.Width
	}
	detail := max(48, min(72, m.Width*2/5))
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
func (m Model) screen() []string {
	f := m.frame()
	body := m.claimsBody(f.bodyHeight)
	if m.Help {
		lines := m.helpLines()
		body = lines[max(0, min(m.HelpOffset, len(lines)-f.bodyHeight)):]
	}
	out := append([]string{}, f.top...)
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
	authority := shortIDs(valueOr(clean(m.Authority), "unset"))
	if scope := clean(m.Scope); scope != "" && !strings.HasPrefix(authority, scope+" ") && authority != scope {
		authority += " (" + scope + ")"
	}
	left := []seg{{"worklease", m.s().appTitle}, {"  authority ", m.s().faint}, {authority, lipgloss.NewStyle()}}
	var right []seg
	if !m.Claims.LastUpdated.IsZero() {
		right = append(right, seg{"updated " + ago(m.claimsNow().Sub(m.Claims.LastUpdated)), m.s().faint})
	}
	return spread(left, right, m.Width)
}
func (m Model) footerLines() []string {
	left, right := m.claimsStatus()
	var lines []string
	if len(left)+len(right) > 0 {
		lines = append(lines, spread(left, right, m.Width))
	}
	for _, notice := range []string{m.Notice, m.Claims.Notice} {
		if notice != "" {
			lines = append(lines, m.s().faint.Render(clip(notice, m.Width)))
		}
	}
	if m.Filtering {
		lines = append(lines, m.s().key.Render("/")+clip(m.Input, m.Width-3)+m.s().selected.Render(" "))
	}
	return lines
}
func (m Model) helpLines() []string {
	lines := []string{m.s().bold.Render("Claims · authority-wide"), "Keyboard help · ? or esc closes", ""}
	for _, b := range []binding{{"j/k ↓/↑", "move selection"}, {"gg / G", "first / last row"}, {"^f / ^b", "scroll page"}, {"^d / ^u", "scroll half page"}, {"enter", "toggle detail"}, {"tab", "next detail section"}, {"*", "matching private handle found"}, {"u", "preview renew; default TTL"}, {"R", "release reason, preview, confirm"}, {"supervised", "worklease runs stop; supervisor owns it"}, {"/", "filter by resource prefix"}, {"m", "toggle mine / all"}, {"e", "toggle expiring"}, {"s", "toggle stale"}, {"r", "refresh"}, {"esc", "close detail, then clear filters"}, {"q", "quit"}, {"mouse", "click selects; click again opens; wheel scrolls"}} {
		lines = append(lines, "  "+m.s().key.Render(fit(b.keys, 14))+" "+b.desc)
	}
	return lines
}
func (m Model) dialogTop(height int) int { return (m.Height - height) / 2 }

var confirmKeys = []binding{{"enter/y", "confirm"}, {"esc/n", "cancel"}}

func (m Model) dialogView() (dialogBox, bool) {
	switch m.mode() {
	case modeClaimsReleaseReason:
		return m.dialog("Release reason", "Reason recorded in claim history\n\n"+m.Input+"▏", []binding{{"enter", "review preview"}, {"ctrl+u", "clear"}, {"esc", "cancel"}}), true
	case modeClaimsActionPreview:
		return m.claimsActionPreviewView(*m.Claims.actionPreview), true
	}
	return dialogBox{}, false
}
