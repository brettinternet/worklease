package queueui

import (
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) updateClaimsKey(key string) (tea.Model, tea.Cmd) {
	if cmd, ok := m.viewKey(key); ok {
		return m, cmd
	}
	rows := m.claimRows()
	capacity := max(1, m.frame(m.rows()).bodyHeight-1)
	m.anchorClaims(rows, capacity)
	previous := m.Claims.Selected
	switch key {
	case "q", "ctrl+c":
		return m.requestQuit()
	case "j", "down", "n":
		m.selectClaimIndex(rows, m.Claims.Index+1, capacity)
	case "k", "up", "N":
		m.selectClaimIndex(rows, m.Claims.Index-1, capacity)
	case "g":
		m.selectClaimIndex(rows, 0, capacity)
	case "G":
		m.selectClaimIndex(rows, len(rows)-1, capacity)
	case "pgdown", "ctrl+d":
		if m.Claims.Detail {
			if claim, ok := m.selectedClaim(rows); ok {
				m.Claims.DetailOffset = min(m.Claims.DetailOffset+max(1, m.Height/2), m.claimDetailMaxOffset(claim))
			}
		} else {
			m.selectClaimIndex(rows, m.Claims.Index+capacity, capacity)
		}
	case "pgup", "ctrl+u":
		if m.Claims.Detail {
			if claim, ok := m.selectedClaim(rows); ok {
				m.Claims.DetailOffset = max(0, min(m.Claims.DetailOffset, m.claimDetailMaxOffset(claim))-max(1, m.Height/2))
			}
		} else {
			m.selectClaimIndex(rows, m.Claims.Index-capacity, capacity)
		}
	case "enter", "l", "right":
		if len(rows) > 0 {
			m.Claims.Detail = true
			m.Claims.DetailTab = 0
			m.Claims.DetailOffset = 0
			return m, m.loadSelectedClaimHistory(rows)
		}
	case "h", "left", "esc":
		if m.Claims.Detail {
			m.Claims.Detail = false
		} else if m.Claims.ResourcePrefix != "" {
			m.Claims.ResourcePrefix = ""
			m.Claims.Notice = ""
		} else {
			m.Claims.MineOnly, m.Claims.ExpiringOnly, m.Claims.StaleOnly = false, false, false
		}
		m.anchorClaims(m.claimRows(), capacity)
	case "tab":
		if m.Claims.Detail {
			m.Claims.DetailTab = (m.Claims.DetailTab + 1) % 2
			m.Claims.DetailOffset = 0
		}
	case "shift+tab":
		if m.Claims.Detail {
			m.Claims.DetailTab = (m.Claims.DetailTab + 1) % 2
			m.Claims.DetailOffset = 0
		}
	case "/":
		m.Filtering = true
		m.Input = m.Claims.ResourcePrefix
	case "m":
		m.Claims.MineOnly = !m.Claims.MineOnly
		m.anchorClaims(m.claimRows(), capacity)
	case "e":
		m.Claims.ExpiringOnly = !m.Claims.ExpiringOnly
		m.anchorClaims(m.claimRows(), capacity)
	case "s":
		m.Claims.StaleOnly = !m.Claims.StaleOnly
		m.anchorClaims(m.claimRows(), capacity)
	case "i":
		jumped, _ := m.claimItemJump()
		return jumped, nil
	case "r":
		if m.Claims.Loading {
			m.Claims.Notice = "Claims refresh already in progress"
		} else if m.Claims.Refresh != nil {
			m.Claims.Loading = true
			return m, m.Claims.Refresh(m.Claims.Cursor)
		} else {
			m.Claims.Notice = "Claims refresh unavailable"
		}
	case "?":
		m.Help = true
	}
	if m.Claims.Selected != previous && m.Claims.Detail {
		return m, m.loadSelectedClaimHistory(m.claimRows())
	}
	return m, nil
}

func (m *Model) selectClaimIndex(rows []lease.ClaimView, index, capacity int) {
	if len(rows) == 0 {
		m.Claims.Selected, m.Claims.Index, m.Claims.Offset = "", 0, 0
		return
	}
	index = max(0, min(index, len(rows)-1))
	previous := m.Claims.Selected
	m.Claims.Index = index
	m.Claims.Selected = rows[index].ClaimID
	m.anchorClaims(rows, capacity)
	if m.Claims.Selected != previous && m.Claims.Detail {
		m.Claims.DetailOffset = 0
		m.Claims.DetailTab = 0
		m.Claims.History = ledger.HistoryPage{}
		m.Claims.HistoryResource = ""
		m.Claims.HistoryLoading = false
		m.Claims.HistoryError = ""
	}
}

func (m *Model) loadSelectedClaimHistory(rows []lease.ClaimView) tea.Cmd {
	claim, ok := m.selectedClaim(rows)
	if !ok || len(claim.Resources) == 0 || m.Claims.LoadHistory == nil || m.Claims.PublicOnly {
		m.Claims.History = ledger.HistoryPage{}
		m.Claims.HistoryResource = ""
		m.Claims.HistoryLoading = false
		m.Claims.HistoryError = ""
		return nil
	}
	resource := claim.Resources[0]
	if m.Claims.HistoryResource == resource && (len(m.Claims.History.Epochs) > 0 || m.Claims.HistoryLoading || m.Claims.HistoryError != "") {
		return nil
	}
	m.Claims.History = ledger.HistoryPage{}
	m.Claims.HistoryResource = resource
	m.Claims.HistoryLoading = true
	m.Claims.HistoryError = ""
	return m.Claims.LoadHistory(claim.ClaimID, resource, "")
}
