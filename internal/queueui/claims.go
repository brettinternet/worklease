package queueui

import (
	"context"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	claimsPollInterval = time.Second
	claimsExpiringSoon = 2 * time.Minute
	claimsEventLimit   = 100
)

// ClaimsReader is the public, read-only authority surface used by the Claims tab.
type ClaimsReader interface {
	List(context.Context, string, *lease.RemoteActor) ([]lease.ClaimView, error)
	Events(context.Context, string, int) (ledger.EventsPage, error)
	History(context.Context, string, string, int, bool) (ledger.HistoryPage, error)
}

type ClaimsState struct {
	Items           []lease.ClaimView
	Events          []ledger.Event
	Cursor          string
	Gap             bool
	Stale           bool
	Loading         bool
	Error           string
	EventsError     string
	Notice          string
	LastUpdated     time.Time
	ResourcePrefix  string
	MineOnly        bool
	ExpiringOnly    bool
	StaleOnly       bool
	MineAgentID     string
	MineSessionID   string
	PublicOnly      bool
	Selected        string
	Index           int
	Offset          int
	Detail          bool
	DetailTab       int
	DetailOffset    int
	History         ledger.HistoryPage
	HistoryResource string
	HistoryLoading  bool
	HistoryError    string
	Refresh         func(string) tea.Cmd
	LoadHistory     func(string, string, string) tea.Cmd
	Now             func() time.Time
}

type ClaimsRefreshMsg struct {
	Claims    []lease.ClaimView
	Events    []ledger.Event
	Cursor    string
	Gap       bool
	ClaimsErr error
	EventsErr error
}

type ClaimHistoryMsg struct {
	ClaimID  string
	Resource string
	Page     ledger.HistoryPage
	Err      error
}

type ClaimsTickMsg struct{}

// ClaimsRefreshCmd reads the public claims list and advances the durable event
// cursor. A pruned cursor is reset to a fresh recent-events window, never
// treated as a complete history.
func ClaimsRefreshCmd(ctx context.Context, reader ClaimsReader, cursor string) tea.Cmd {
	return func() tea.Msg {
		page, eventsErr := reader.Events(ctx, cursor, claimsEventLimit)
		gap := false
		if eventsErr == nil && page.Gap {
			gap = true
			page, eventsErr = reader.Events(ctx, "", claimsEventLimit)
		}
		claims, claimsErr := reader.List(ctx, "", nil)
		return ClaimsRefreshMsg{Claims: claims, Events: page.Events, Cursor: page.NextCursor, Gap: gap, ClaimsErr: claimsErr, EventsErr: eventsErr}
	}
}

// ClaimHistoryCmd reads only the public epoch and operation-status history.
func ClaimHistoryCmd(ctx context.Context, reader ClaimsReader, claimID, resource, cursor string) tea.Cmd {
	return func() tea.Msg {
		page, err := reader.History(ctx, resource, cursor, 20, false)
		return ClaimHistoryMsg{ClaimID: claimID, Resource: resource, Page: page, Err: err}
	}
}

func (m *Model) applyClaimsRefresh(msg ClaimsRefreshMsg) tea.Cmd {
	previous := m.Claims.Selected
	m.Claims.Loading = false
	if msg.ClaimsErr != nil {
		m.Claims.Stale = true
		m.Claims.Error = msg.ClaimsErr.Error()
	} else {
		m.Claims.Items = append([]lease.ClaimView(nil), msg.Claims...)
		m.Claims.Stale = false
		m.Claims.Error = ""
		m.Claims.LastUpdated = m.claimsNow()
	}
	if msg.EventsErr != nil {
		m.Claims.EventsError = msg.EventsErr.Error()
	} else {
		if msg.Gap {
			m.Claims.Events = append([]ledger.Event(nil), msg.Events...)
		} else {
			m.Claims.Events = mergeClaimEvents(m.Claims.Events, msg.Events)
		}
		m.Claims.Cursor = msg.Cursor
		m.Claims.EventsError = ""
	}
	m.Claims.Gap = m.Claims.Gap || msg.Gap
	m.anchorClaims(m.claimRows(), max(1, m.frame(m.rows()).bodyHeight-1))
	if m.Claims.Selected != previous {
		m.Claims.History = ledger.HistoryPage{}
		m.Claims.HistoryResource = ""
		m.Claims.HistoryLoading = false
		m.Claims.HistoryError = ""
		if m.Claims.Detail {
			return m.loadSelectedClaimHistory(m.claimRows())
		}
	}
	return nil
}

func mergeClaimEvents(previous, next []ledger.Event) []ledger.Event {
	seen := make(map[string]bool, len(previous)+len(next))
	merged := make([]ledger.Event, 0, len(previous)+len(next))
	for _, event := range append(append([]ledger.Event(nil), previous...), next...) {
		if event.Sequence != "" && seen[event.Sequence] {
			continue
		}
		seen[event.Sequence] = true
		merged = append(merged, event)
	}
	if len(merged) > claimsEventLimit {
		merged = append([]ledger.Event(nil), merged[len(merged)-claimsEventLimit:]...)
	}
	return merged
}

func (m Model) claimsNow() time.Time {
	if m.Claims.Now != nil {
		return m.Claims.Now().UTC()
	}
	return time.Now().UTC()
}

func (m Model) claimRows() []lease.ClaimView {
	rows := make([]lease.ClaimView, 0, len(m.Claims.Items))
	now := m.claimsNow()
	for _, claim := range m.Claims.Items {
		if m.Claims.MineOnly && !m.claimIsMine(claim) {
			continue
		}
		if m.Claims.ResourcePrefix != "" && !claimHasResourcePrefix(claim, m.Claims.ResourcePrefix) {
			continue
		}
		if m.Claims.ExpiringOnly && !claimExpiring(claim, now) {
			continue
		}
		if m.Claims.StaleOnly && !m.Claims.Stale {
			continue
		}
		rows = append(rows, claim)
	}
	return rows
}

func (m Model) claimIsMine(claim lease.ClaimView) bool {
	return m.Claims.MineAgentID != "" && strings.EqualFold(claim.AgentID, m.Claims.MineAgentID)
}

func claimHasResourcePrefix(claim lease.ClaimView, prefix string) bool {
	for _, resource := range claim.Resources {
		if strings.HasPrefix(resource, prefix) {
			return true
		}
	}
	return false
}

func claimExpiring(claim lease.ClaimView, now time.Time) bool {
	remaining := claim.ExpiresAt.Sub(now)
	return claim.Active && remaining > 0 && remaining <= claimsExpiringSoon
}

func (m *Model) anchorClaims(rows []lease.ClaimView, capacity int) {
	if len(rows) == 0 {
		m.Claims.Selected = ""
		m.Claims.Index = 0
		m.Claims.Offset = 0
		return
	}
	found := false
	for index, claim := range rows {
		if claim.ClaimID == m.Claims.Selected {
			m.Claims.Index = index
			found = true
			break
		}
	}
	if !found {
		m.Claims.Index = max(0, min(m.Claims.Index, len(rows)-1))
		m.Claims.Selected = rows[m.Claims.Index].ClaimID
	}
	capacity = max(1, capacity)
	if m.Claims.Index < m.Claims.Offset {
		m.Claims.Offset = m.Claims.Index
	}
	if m.Claims.Index >= m.Claims.Offset+capacity {
		m.Claims.Offset = m.Claims.Index - capacity + 1
	}
	m.Claims.Offset = max(0, min(m.Claims.Offset, len(rows)-capacity))
}

func (m Model) selectedClaim(rows []lease.ClaimView) (lease.ClaimView, bool) {
	for _, claim := range rows {
		if claim.ClaimID == m.Claims.Selected {
			return claim, true
		}
	}
	return lease.ClaimView{}, false
}
