package queueui

import tea "github.com/charmbracelet/bubbletea"

func (m Model) claimsMouse(v tea.MouseMsg) (tea.Model, tea.Cmd) {
	rows := m.claimRows()
	f := m.frame(m.rows())
	capacity := max(1, f.bodyHeight-1)
	listWidth, detailWidth := m.paneWidths()
	inDetail := detailWidth > 0 && v.X >= listWidth
	switch v.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		delta := wheelStep
		if v.Button == tea.MouseButtonWheelUp {
			delta = -wheelStep
		}
		if inDetail {
			if claim, ok := m.selectedClaim(rows); ok {
				m.Claims.DetailOffset = max(0, min(m.Claims.DetailOffset+delta, m.claimDetailMaxOffset(claim)))
			}
			return m, nil
		}
		start := max(0, min(m.Claims.Offset+delta/wheelStep, len(rows)-capacity))
		m.Claims.Offset = start
		m.selectClaimIndex(rows, max(start, min(m.Claims.Index, start+capacity-1)), capacity)
		return m, nil
	case tea.MouseButtonLeft:
		if v.Y == 1 {
			if index, ok := hit(f.tabs, v.X); ok {
				return m, m.setView(m.Views[index])
			}
			return m, nil
		}
		y := v.Y - f.bodyTop
		if y < 0 || y >= f.bodyHeight {
			return m, nil
		}
		if inDetail {
			if y == 1 {
				start := listWidth + 2
				if listWidth == 0 {
					start = 1
				}
				if tab, ok := hit(m.claimDetailTabSpans(detailWidth-1), v.X-start); ok {
					m.Claims.DetailTab = tab
					m.Claims.DetailOffset = 0
				}
			}
			return m, nil
		}
		row := y - 1
		if row < 0 || row >= capacity {
			return m, nil
		}
		index := m.Claims.Offset + row
		if index >= len(rows) {
			return m, nil
		}
		if rows[index].ClaimID == m.Claims.Selected {
			m.Claims.Detail = true
			m.Claims.DetailTab = 0
			m.Claims.DetailOffset = 0
			return m, m.loadSelectedClaimHistory(rows)
		}
		m.selectClaimIndex(rows, index, capacity)
		if m.Claims.Detail {
			return m, m.loadSelectedClaimHistory(m.claimRows())
		}
	}
	return m, nil
}
