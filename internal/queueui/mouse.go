package queueui

import tea "github.com/charmbracelet/bubbletea"

// wheelStep is the number of rows or detail lines one wheel notch scrolls.
const wheelStep = 3

// overlay reports whether a dialog or prompt owns input.
func (m Model) overlay() bool { return m.mode() >= modeFilter }

// mouse handles clicks and the wheel. Hit-testing uses the same frame and
// pane widths as View, so a click maps to the row or tab under the pointer.
func (m Model) mouse(v tea.MouseMsg) (tea.Model, tea.Cmd) {
	if v.Action != tea.MouseActionPress || m.overlay() || m.Width < 30 {
		return m, nil
	}
	if m.Help {
		if v.Button == tea.MouseButtonLeft {
			m.Help = false
		}
		return m, nil
	}
	rows := m.rows()
	m.anchor(rows)
	f := m.frame(rows)
	previous := m.Selected
	listWidth, detailWidth := m.paneWidths()
	inDetail := detailWidth > 0 && v.X >= listWidth
	capacity := max(1, f.bodyHeight-1)
	switch v.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		delta := wheelStep
		if v.Button == tea.MouseButtonWheelUp {
			delta = -wheelStep
		}
		switch {
		case m.ViewName == RecoveryViewID:
			m.RecoveryIndex = max(0, min(len(m.Recovery)-1, m.RecoveryIndex+delta/wheelStep))
			return m, nil
		case inDetail:
			m.DetailOffset = max(0, min(m.DetailOffset+delta, m.maxDetailOffset()))
			return m, nil
		case len(rows) > 0:
			// Scroll the viewport; the selection moves only to stay visible.
			m.Offset = max(0, min(m.listOffset(len(rows), capacity)+delta, len(rows)-capacity))
			m.selectIndex(rows, max(m.Offset, min(m.Index, m.Offset+capacity-1)))
		}
	case tea.MouseButtonLeft:
		if v.Y == 1 {
			if index, ok := hit(f.tabs, v.X); ok {
				if cmd := m.setView(m.Views[index]); cmd != nil {
					return m, cmd
				}
			}
			return m.afterNavigation(previous)
		}
		y := v.Y - f.bodyTop
		if y < 0 || y >= f.bodyHeight || m.ViewName == RecoveryViewID || m.Help {
			return m, nil
		}
		if inDetail {
			if y == 1 {
				// The detail pane follows the separator and a one-cell margin.
				start := 1
				if listWidth > 0 {
					start = listWidth + 2
				}
				_, spans := m.detailTabs(m.Tab, detailWidth-1)
				if tab, ok := hit(spans, v.X-start); ok {
					m.Tab, m.DetailOffset = tab, 0
				}
			}
			return m.afterNavigation(previous)
		}
		row := y - 1
		if row < 0 || row >= capacity {
			return m, nil
		}
		index := m.listOffset(len(rows), capacity) + row
		if index >= len(rows) {
			return m, nil
		}
		if identity(rows[index]) == m.Selected {
			m.Detail = true
		} else {
			m.selectIndex(rows, index)
		}
	}
	return m.afterNavigation(previous)
}
