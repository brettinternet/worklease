package queueui

import (
	"context"
	"slices"
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

// ClaimsReader supplies the public, read-only claims and history views.
type ClaimsReader interface {
	List(context.Context, string, *lease.RemoteActor) ([]lease.ClaimView, error)
	Events(context.Context, string, int) (ledger.EventsPage, error)
	History(context.Context, string, string, int, bool) (ledger.HistoryPage, error)
}

type ClaimsHandle struct {
	Kind, Path, Unavailable string
	Revision                int64
	AuthorityID, RestoreID  string
}

func (h ClaimsHandle) available() bool {
	return h.Kind != "" && h.Path != "" && h.Unavailable == ""
}

type ClaimsAction string

const (
	ClaimsActionRenew   ClaimsAction = "renew"
	ClaimsActionRelease ClaimsAction = "release"
)

type ClaimsActionPreview struct {
	Action ClaimsAction
	Claim  lease.ClaimView
	Handle ClaimsHandle
	Reason string
}

type ClaimsMutationMsg struct {
	Action  ClaimsAction
	ClaimID string
	Claim   lease.ClaimView
	Err     error
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
	ResolveHandles  func([]lease.ClaimView) map[string]ClaimsHandle
	Mutate          func(context.Context, lease.ClaimView, ClaimsHandle, string, bool) (lease.ClaimView, error)
	RenewTTL        time.Duration
	Now             func() time.Time

	handles              map[string]ClaimsHandle
	actionPreview        *ClaimsActionPreview
	releaseReasonInput   bool
	mutating             bool
	pendingMutation      *ClaimsActionPreview
	refreshAfterMutation bool
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
	// A poll started before a mutation may finish after its definitive result.
	// Discard that stale snapshot and let the queued post-mutation refresh run.
	if m.Claims.refreshAfterMutation {
		m.Claims.Loading = false
		return nil
	}
	previous := m.Claims.Selected
	m.Claims.Loading = false
	if msg.ClaimsErr != nil {
		m.Claims.Stale = true
		m.Claims.Error = msg.ClaimsErr.Error()
	} else {
		m.Claims.Items = append([]lease.ClaimView(nil), msg.Claims...)
		m.resolveClaimHandles()
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

func (m *Model) resolveClaimHandles() {
	if m.Claims.ResolveHandles == nil {
		m.Claims.handles = nil
		return
	}
	m.Claims.handles = m.Claims.ResolveHandles(append([]lease.ClaimView(nil), m.Claims.Items...))
}

func (m Model) claimHandle(claim lease.ClaimView) ClaimsHandle {
	if handle, ok := m.Claims.handles[claim.ClaimID]; ok {
		return handle
	}
	if m.Claims.ResolveHandles == nil {
		return ClaimsHandle{Unavailable: "local handle discovery is unavailable"}
	}
	return ClaimsHandle{Unavailable: "no matching local private handle found"}
}

func claimsHandleUnavailable(claim lease.ClaimView, handle ClaimsHandle) string {
	if handle.Unavailable != "" {
		return handle.Unavailable
	}
	if handle.Kind == "" || handle.Path == "" {
		return "no matching local private handle found"
	}
	if handle.AuthorityID != claim.AuthorityID {
		return "authority mismatch"
	}
	if handle.RestoreID != claim.RestoreID {
		return "restore ID mismatch"
	}
	if handle.Revision != claim.Revision {
		return "claim revision mismatch"
	}
	return ""
}

func (m Model) claimActionEligible(claim lease.ClaimView) bool {
	handle := m.claimHandle(claim)
	return !m.Claims.Stale && claim.Active && (claim.ExpiresAt.IsZero() || claim.ExpiresAt.After(m.claimsNow())) && handle.available() && claimsHandleUnavailable(claim, handle) == ""
}

func (m Model) claimActionUnavailable(claim lease.ClaimView) string {
	handle := m.claimHandle(claim)
	if unavailable := claimsHandleUnavailable(claim, handle); unavailable != "" {
		return unavailable
	}
	switch {
	case m.Claims.Stale:
		return "authority claim list is stale; refresh before acting"
	case !claim.Active:
		return "claim is not active"
	case !claim.ExpiresAt.IsZero() && !claim.ExpiresAt.After(m.claimsNow()):
		return "claim has expired"
	}
	if !handle.available() {
		return "private handle is unavailable"
	}
	if m.Claims.Mutate == nil {
		return "claim mutation is unavailable"
	}
	return ""
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
	slices.SortFunc(rows, func(a, b lease.ClaimView) int {
		if order := b.AcquiredAt.Compare(a.AcquiredAt); order != 0 {
			return order
		}
		return strings.Compare(a.ClaimID, b.ClaimID)
	})
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

func (m Model) dispatchClaimsAction(preview ClaimsActionPreview) (tea.Model, tea.Cmd) {
	if m.Claims.mutating {
		m.Claims.Notice = "Claim action already in progress"
		return m, nil
	}
	if m.Claims.Mutate == nil {
		m.Claims.Notice = "Claim action unavailable: mutation callback is not configured"
		return m, nil
	}
	if m.Claims.Selected != preview.Claim.ClaimID {
		m.Claims.Notice = "Claim action preview expired because selection changed"
		return m, nil
	}
	if _, ok := m.selectedClaim(m.claimRows()); !ok {
		m.Claims.Notice = "Claim action preview expired because the claim is no longer listed"
		return m, nil
	}

	m.Claims.mutating = true
	m.Claims.pendingMutation = &preview
	action := "renewing"
	release := preview.Action == ClaimsActionRelease
	if release {
		action = "releasing"
	}
	m.Claims.Notice = "Revalidating and " + action + " claim…"
	mutate := m.Claims.Mutate
	return m, func() tea.Msg {
		claim, err := mutate(context.Background(), preview.Claim, preview.Handle, preview.Reason, release)
		return ClaimsMutationMsg{Action: preview.Action, ClaimID: preview.Claim.ClaimID, Claim: claim, Err: err}
	}
}

func (m *Model) applyClaimsMutation(msg ClaimsMutationMsg) tea.Cmd {
	pending := m.Claims.pendingMutation
	if !m.Claims.mutating || pending == nil || pending.Action != msg.Action || pending.Claim.ClaimID != msg.ClaimID {
		return nil
	}
	m.Claims.mutating = false
	m.Claims.pendingMutation = nil
	if msg.Err != nil {
		m.resolveClaimHandles()
		m.Claims.Notice = strings.ToUpper(string(msg.Action[:1])) + string(msg.Action[1:]) + ": " + msg.Err.Error()
		return m.refreshClaimsAfterMutation()
	}

	previous := m.Claims.Selected
	switch msg.Action {
	case ClaimsActionRenew:
		if msg.Claim.ClaimID != msg.ClaimID {
			m.Claims.Notice = "Renew refused: mutation returned a different claim ID"
			return nil
		}
		found := false
		for index := range m.Claims.Items {
			if m.Claims.Items[index].ClaimID == msg.ClaimID {
				m.Claims.Items[index] = msg.Claim
				found = true
				break
			}
		}
		if !found {
			m.Claims.Items = append(m.Claims.Items, msg.Claim)
		}
		m.resolveClaimHandles()
		m.Claims.Notice = "Claim renewed · expires " + claimExpiryTimestamp(msg.Claim.ExpiresAt)
	case ClaimsActionRelease:
		m.Claims.Items = slices.DeleteFunc(m.Claims.Items, func(claim lease.ClaimView) bool { return claim.ClaimID == msg.ClaimID })
		if m.Claims.handles != nil {
			delete(m.Claims.handles, msg.ClaimID)
		}
		m.resolveClaimHandles()
		m.Claims.Notice = "Claim released · resources are free"
	default:
		m.Claims.Notice = "Claim action result ignored: unknown action"
		return nil
	}
	m.Claims.LastUpdated = m.claimsNow()
	m.anchorClaims(m.claimRows(), max(1, m.frame(m.rows()).bodyHeight-1))
	var history tea.Cmd
	if m.Claims.Detail && m.Claims.Selected != previous {
		m.Claims.History = ledger.HistoryPage{}
		m.Claims.HistoryResource = ""
		m.Claims.HistoryLoading = false
		m.Claims.HistoryError = ""
		history = m.loadSelectedClaimHistory(m.claimRows())
	}
	return tea.Batch(history, m.refreshClaimsAfterMutation())
}

func (m *Model) refreshClaimsAfterMutation() tea.Cmd {
	if m.Claims.Refresh == nil {
		return nil
	}
	if m.Claims.Loading {
		m.Claims.refreshAfterMutation = true
		return nil
	}
	m.Claims.Loading = true
	return m.Claims.Refresh(m.Claims.Cursor)
}

func claimExpiryTimestamp(expiry time.Time) string {
	if expiry.IsZero() {
		return "unknown"
	}
	return expiry.UTC().Format(time.RFC3339)
}
