package queueui

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/reason"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// SnapshotMsg publishes immutable source state without changing the focused pane.
type SnapshotMsg struct {
	Snapshot           queue.Snapshot
	orderedKeys        []string
	sourceIDs          []string
	allRows            []queue.Item
	rowIndexes         map[string]int
	preparedRows       []queue.Item
	preparedRowIndexes map[string]int
	preparedCounts     map[string]int
	preparedObserved   time.Time
	preparedEdges      int
	projection         snapshotProjection
	hasProjection      bool
	prepared           bool
}

type snapshotProjection struct {
	ViewName    string
	Filter      string
	Me          string
	Sources     []queue.Source
	Views       []string
	ViewFilters map[string]queue.Filters
	ViewRules   map[string]ViewRule
	MeBySource  map[string][]string
}

func prepareSnapshot(snapshot queue.Snapshot, prepareAllRows bool, sources ...queue.Source) SnapshotMsg {
	prepared := snapshot.Clone()
	message := SnapshotMsg{Snapshot: prepared, prepared: true}
	if len(sources) > 0 {
		order := make([]string, 0, len(sources))
		for _, source := range sources {
			order = append(order, source.ID)
		}
		message.sourceIDs = order
		message.orderedKeys = queue.OrderedKeys(prepared.Items, order)
		if prepareAllRows {
			message.allRows = make([]queue.Item, 0, len(message.orderedKeys))
			message.rowIndexes = make(map[string]int, len(message.orderedKeys))
			seen := make(map[string]bool, len(message.orderedKeys))
			for _, key := range message.orderedKeys {
				item := prepared.Items[key]
				if id := identity(item); seen[id] {
					continue
				} else {
					seen[id] = true
				}
				message.rowIndexes[key] = len(message.allRows)
				message.allRows = append(message.allRows, item)
			}
		}
	}
	return message
}

// PrepareSnapshotForModel computes the selected configured-view projection and
// view counts on the producer worker. The event loop still reconciles newer
// claim observations before adopting the prepared projection.
func PrepareSnapshotForModel(snapshot queue.Snapshot, model Model) SnapshotMsg {
	message := prepareSnapshot(snapshot, false, model.Sources...)
	message.projection = projectionForModel(model)
	message.hasProjection = true

	preparedModel := model
	preparedModel.Snapshot = message.Snapshot
	preparedModel.orderedKeys = message.orderedKeys
	preparedModel.rowCache = nil
	message.preparedRows = preparedModel.rows()
	message.preparedRowIndexes = make(map[string]int, len(message.preparedRows))
	for index, item := range message.preparedRows {
		message.preparedRowIndexes[item.Ref.Key()] = index
	}
	message.preparedCounts = make(map[string]int, len(preparedModel.Views))
	for _, name := range preparedModel.Views {
		message.preparedCounts[name] = preparedModel.viewCount(name)
	}
	if _, ok := message.preparedCounts[preparedModel.ViewName]; !ok {
		message.preparedCounts[preparedModel.ViewName] = preparedModel.viewCount(preparedModel.ViewName)
	}
	message.preparedObserved, message.preparedEdges = snapshotRowMetrics(message.Snapshot)
	return message
}

func projectionForModel(model Model) snapshotProjection {
	projection := snapshotProjection{
		ViewName: model.ViewName,
		Filter:   model.Filter,
		Me:       model.Me,
		Sources:  append([]queue.Source(nil), model.Sources...),
		Views:    append([]string(nil), model.Views...),
	}
	if model.ViewFilters != nil {
		projection.ViewFilters = make(map[string]queue.Filters, len(model.ViewFilters))
		for name, filters := range model.ViewFilters {
			filters.SourceIDs = append([]string(nil), filters.SourceIDs...)
			filters.States = append([]queue.StateCategory(nil), filters.States...)
			projection.ViewFilters[name] = filters
		}
	}
	if model.ViewRules != nil {
		projection.ViewRules = make(map[string]ViewRule, len(model.ViewRules))
		for name, rule := range model.ViewRules {
			rule.Assigned = append([]string(nil), rule.Assigned...)
			projection.ViewRules[name] = rule
		}
	}
	if model.MeBySource != nil {
		projection.MeBySource = make(map[string][]string, len(model.MeBySource))
		for source, names := range model.MeBySource {
			projection.MeBySource[source] = append([]string(nil), names...)
		}
	}
	return projection
}

func snapshotRowMetrics(snapshot queue.Snapshot) (time.Time, int) {
	var observed time.Time
	edges := 0
	for _, item := range snapshot.Items {
		if item.Observation.ObservedAt.After(observed) {
			observed = item.Observation.ObservedAt
		}
		if item.DependenciesKnown && item.Closure == queue.CoverageComplete && item.Fresh {
			edges++
		}
	}
	return observed, edges
}

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
type RecoveryMsg struct {
	Entries []queue.RecoveryEntry
	Err     error
}
type StateChoice struct {
	Action            queue.Action
	Label, Transition string
}
type WritePreview struct {
	Identity                string
	AuthorityProfile, Scope string
	Intent                  queue.WriteIntent
	Effect                  string
	SideEffects             []string
	Races                   []string
}
type WritePreviewMsg struct {
	Preview *WritePreview
	Err     error
}
type WriteResultMsg struct {
	Result      queue.WriteResult
	Err         error
	OperationID string
}
type ReconcileResultMsg struct {
	OperationID string
	Status      string
	Err         error
}

type StartPreview struct {
	Claim                                                              ClaimPreview
	Source, Actor, Transition, TransitionValue, RequiredFields, Effect string
	SideEffects                                                        []string
}
type StartPreviewMsg struct {
	Identity string
	Item     queue.Item
	Preview  *StartPreview
	Err      error
}
type StartResultMsg struct {
	Claim                     ClaimResultMsg
	Write                     *WriteResultMsg
	ClaimStep, TransitionStep string
	Err                       error
}

type ClaimPreview struct {
	Identity, Title               string
	AuthorityProfile, AuthorityID string
	Scope, SessionID              string
	Resources                     []string
	TTL, Hold                     time.Duration
	CoordinationLimits            string
}
type ClaimPreviewMsg struct {
	Identity string
	Item     queue.Item
	Preview  *ClaimPreview
	Err      error
}
type ClaimResultMsg struct {
	Identity            string
	Item                queue.Item
	Claim               queue.ClaimObservation
	GrantedTTL          time.Duration
	HandlePath, ClaimID string
	Resources           []string
	NextRenewal         time.Time
	Err                 error
}
type OwnedClaimMsg struct {
	Path, ClaimID, LastResult string
	Gone                      bool
	Resources                 []string
	ExpiresAt, NextRenewal    time.Time
	Verified, Lost            bool
}
type CancelClaimMsg struct {
	Path string
	Err  error
}
type LaunchResultMsg struct {
	Name string
	Err  error
}

type ViewRule struct {
	Readiness, Claim string
	Assigned         []string
}
type viewSelection struct {
	Selected      string
	Index, Offset int
	Detail        bool
	DetailOffset  int
	Tab           int
}

type Model struct {
	Snapshot                       queue.Snapshot
	Views                          []string
	Claims                         ClaimsState
	viewSelections                 map[string]viewSelection
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
	// HighContrast renders without faint text or color, using bold,
	// underline and reverse video in the terminal's own colors.
	HighContrast                       bool
	History                            ledger.HistoryPage
	HistoryError                       string
	HistoryLoading                     bool
	HistoryIdentity                    string
	HistoryCursor                      string
	Comments                           []queue.GitHubComment
	CommentsCursor                     string
	CommentsIdentity                   string
	CommentsLoading                    bool
	CommentsError                      string
	ClaimFreshness                     string
	RebuildingClaims                   bool
	ClaimLoading, Claiming, Cancelling bool
	ClaimPreview                       *ClaimPreview
	StartPreview                       *StartPreview
	StartTransitions                   map[string]string
	LaunchOptions                      []queue.LaunchOption
	LaunchIdentity                     string
	LaunchRef                          queue.Ref
	LaunchIndex                        int
	Launching                          bool
	OwnedClaims                        map[string]OwnedClaimMsg
	Recovery                           []queue.RecoveryEntry
	RecoveryIndex                      int
	RecoveryEvidence                   bool
	// RecoveryEvidenceEntry is the operation selected when the evidence prompt
	// opened; a list refresh must not redirect typed evidence to another one.
	RecoveryEvidenceEntry                 queue.RecoveryEntry
	RecoveryError                         string
	LoadRecovery                          func() tea.Cmd
	RetryRecovery                         func(queue.RecoveryEntry) tea.Cmd
	ReconcileRecovery                     func(queue.RecoveryEntry, string) tea.Cmd
	AttestCheckpointMissing               func(queue.RecoveryEntry, string) tea.Cmd
	StateChoices                          map[string][]StateChoice
	WriteChoices                          []StateChoice
	WriteChoiceIndex                      int
	WriteInput                            bool
	WriteInputIdentity                    string
	WritePreview                          *WritePreview
	Writing, WriteLoading, UncertainWrite bool
	PreviewWrite                          func(queue.Item, queue.Action, string, string) tea.Cmd
	ConfirmWrite                          func(WritePreview) tea.Cmd
	Quitting                              bool
	CancelPreview                         string
	PreviewClaim                          func(queue.Item) tea.Cmd
	AcquireClaim                          func(queue.Item, ClaimPreview) tea.Cmd
	PreviewStart                          func(queue.Item) tea.Cmd
	StartWork                             func(queue.Item, StartPreview) tea.Cmd
	CancelClaim                           func(string) tea.Cmd
	Refresh                               func() tea.Cmd
	HydrateSelected                       func(queue.Item) tea.Cmd
	LoadHistory                           func(queue.Item, string, bool) tea.Cmd
	LoadComments                          func(queue.Item, string) tea.Cmd
	OpenURL                               func(queue.Item) tea.Cmd
	PreviewLaunch                         func(queue.Item) []queue.LaunchOption
	Launch                                func(queue.Item, string) tea.Cmd
	rowCache                              *rowCache
	orderedKeys                           []string
	// pendingG records a first g so a second g jumps to the top.
	pendingG bool
}

// mode is the layer that owns input and the screen. It is derived from the
// preview and prompt fields, which carry each layer's data, so there is one
// precedence order for key handling, rendering, and the key bar.
type mode int

const (
	modeList mode = iota
	modeRecovery
	modeClaims
	modeHelp
	modeFilter
	modePalette
	modeClaimPreview
	modeLaunch
	modeQuit
	modeCancel
	modeWriteInput
	modeRecoveryEvidence
	modeWriteChoices
	modeWritePreview
	modeStartPreview
)

func (m Model) mode() mode {
	switch {
	case m.StartPreview != nil:
		return modeStartPreview
	case m.WritePreview != nil:
		return modeWritePreview
	case m.WriteChoices != nil:
		return modeWriteChoices
	case m.RecoveryEvidence:
		return modeRecoveryEvidence
	case m.WriteInput:
		return modeWriteInput
	case m.CancelPreview != "":
		return modeCancel
	case m.Quitting:
		return modeQuit
	case m.LaunchOptions != nil:
		return modeLaunch
	case m.ClaimPreview != nil:
		return modeClaimPreview
	case m.Palette:
		return modePalette
	case m.Filtering:
		return modeFilter
	}
	return m.baseMode()
}

// baseMode is the screen drawn beneath any prompt or dialog.
func (m Model) baseMode() mode {
	switch {
	case m.Help:
		return modeHelp
	case m.ViewName == RecoveryViewID:
		return modeRecovery
	case m.ViewName == ClaimsViewID:
		return modeClaims
	}
	return modeList
}

type rowCache struct {
	mu       sync.Mutex
	key      string
	rows     []queue.Item
	counts   map[string]int
	observed time.Time
	edges    int
	// widths holds the widest cell seen per list column for these rows, so
	// columns fit their content without shifting while scrolling.
	widths map[string]int
}

// RecoveryViewID cannot collide with a caller-configured view named Recovery.
const RecoveryViewID = "\x00recovery"

// ClaimsViewID cannot collide with a caller-configured view named Claims.
const ClaimsViewID = "\x00claims"

func recoveryTime(at *time.Time) string {
	if at == nil {
		return "not dispatched"
	}
	return at.UTC().Format(time.RFC3339)
}

func viewLabel(name string) string {
	switch name {
	case RecoveryViewID:
		return "Recovery"
	case ClaimsViewID:
		return "Claims"
	default:
		return name
	}
}

func New(snapshot queue.Snapshot) Model {
	return Model{Snapshot: snapshot.Clone(), Width: 120, Height: 35, Views: []string{"All", "Ready", "Mine", "Claimed", RecoveryViewID, ClaimsViewID}, ViewName: "All", rowCache: &rowCache{}, OwnedClaims: map[string]OwnedClaimMsg{}}
}
func (m Model) Init() tea.Cmd {
	if m.ViewName == ClaimsViewID && m.Claims.Refresh != nil {
		return m.Claims.Refresh(m.Claims.Cursor)
	}
	return nil
}
func identity(i queue.Item) string {
	if i.CanonicalID != "" {
		return i.CanonicalID
	}
	return i.Ref.Key()
}
func sameSourceOrder(sources []queue.Source, ids []string) bool {
	if len(sources) != len(ids) {
		return false
	}
	for i, source := range sources {
		if source.ID != ids[i] {
			return false
		}
	}
	return true
}

func (m Model) rowKey() string {
	return fmt.Sprintf("%x:%d:%s:%s:%s:%v:%v:%v:%v", reflect.ValueOf(m.Snapshot.Items).Pointer(), m.Snapshot.Revision, m.ViewName, m.Filter, m.Me, m.Sources, m.Views, m.ViewFilters, m.ViewRules)
}

func (m Model) rows() []queue.Item {
	// The TUI event loop owns the model. Cache the sorted projection across
	// navigation/paint; provider, claim, filter and view changes invalidate it.
	key := m.rowKey()
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
	var rows []queue.Item
	if m.orderedKeys != nil {
		// Prepared keys were sorted on the producer worker. Claims may have
		// changed since preparation, so always read the current snapshot item.
		seen := make(map[string]bool, len(m.orderedKeys))
		rows = make([]queue.Item, 0, len(m.orderedKeys))
		for _, key := range m.orderedKeys {
			item, ok := m.Snapshot.Items[key]
			if !ok || !queue.MatchesFilters(item, filters) {
				continue
			}
			if id := identity(item); seen[id] {
				continue
			} else {
				seen[id] = true
			}
			rows = append(rows, item)
		}
	} else {
		rows = queue.EvaluateView(m.Snapshot.Items, queue.View{SourceOrder: order, Filters: filters})
	}
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
	m.cacheRows(key, out)
	return out
}

func (m Model) cacheRows(key string, out []queue.Item) {
	if m.rowCache != nil {
		m.rowCache.key, m.rowCache.rows = key, out
		m.rowCache.widths = nil
		m.rowCache.counts = make(map[string]int, len(m.Views))
		// The standard views share one scan with freshness and edge counts.
		// Configured views retain their full filtering semantics below.
		standard := len(m.Views) == 6 && m.Views[0] == "All" && m.Views[1] == "Ready" && m.Views[2] == "Mine" && m.Views[3] == "Claimed" && m.Views[4] == RecoveryViewID && m.Views[5] == ClaimsViewID && len(m.ViewFilters) == 0 && len(m.ViewRules) == 0
		if !standard {
			for _, name := range m.Views {
				m.rowCache.counts[name] = m.viewCount(name)
			}
		}
		m.rowCache.observed = time.Time{}
		m.rowCache.edges = 0
		for _, item := range m.Snapshot.Items {
			if standard {
				m.rowCache.counts["All"]++
				if item.Readiness.Status == queue.Ready {
					m.rowCache.counts["Ready"]++
				}
				if item.Claim.Active {
					m.rowCache.counts["Claimed"]++
				}
				if m.Me != "" {
					for _, owner := range item.AssignedTo {
						if m.isMe(item, owner) {
							m.rowCache.counts["Mine"]++
							break
						}
					}
				}
			}
			if item.Observation.ObservedAt.After(m.rowCache.observed) {
				m.rowCache.observed = item.Observation.ObservedAt
			}
			if item.DependenciesKnown && item.Closure == queue.CoverageComplete && item.Fresh {
				m.rowCache.edges++
			}
		}
	}
}

func (m Model) cachePreparedRows(key string, rows []queue.Item, counts map[string]int, observed time.Time, edges int) {
	if m.rowCache != nil {
		m.rowCache.key = key
		m.rowCache.widths = nil
		m.rowCache.rows = rows
		m.rowCache.counts = counts
		m.rowCache.observed = observed
		m.rowCache.edges = edges
	}
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
	m.selectIndex(rows, m.Index+delta)
	if m.Selected != previous {
		m.clearHistory()
	}
}

// selectIndex selects rows[index] (clamped) and scrolls it into view.
func (m *Model) selectIndex(rows []queue.Item, index int) {
	previous := m.Selected
	m.Index = max(0, min(index, len(rows)-1))
	m.Selected = identity(rows[m.Index])
	m.Offset = m.listOffset(len(rows), m.listCapacity(rows))
	if m.Selected != previous {
		m.clearHistory()
	}
}

// editInput applies a text-editing key to Input. Multi-rune events (paste,
// fast typing, IME) and non-ASCII runes are kept; controls are sanitized.
func (m *Model) editInput(key tea.KeyMsg) {
	switch key.Type {
	case tea.KeyBackspace:
		r := []rune(m.Input)
		if len(r) > 0 {
			m.Input = string(r[:len(r)-1])
		}
	case tea.KeyCtrlU:
		m.Input = ""
	case tea.KeyRunes, tea.KeySpace:
		if !key.Alt {
			m.Input += clean(string(key.Runes))
		}
	}
}

// setView switches views while preserving each queue view's independent selection.
func (m *Model) setView(name string) tea.Cmd {
	if name == m.ViewName {
		return nil
	}
	if m.ViewName != ClaimsViewID && m.ViewName != RecoveryViewID {
		if m.viewSelections == nil {
			m.viewSelections = make(map[string]viewSelection)
		}
		m.viewSelections[m.ViewName] = viewSelection{Selected: m.Selected, Index: m.Index, Offset: m.Offset, Detail: m.Detail, DetailOffset: m.DetailOffset, Tab: m.Tab}
	}
	m.ViewName = name
	if saved, ok := m.viewSelections[name]; ok {
		m.Selected, m.Index, m.Offset = saved.Selected, saved.Index, saved.Offset
		m.Detail, m.DetailOffset, m.Tab = saved.Detail, saved.DetailOffset, saved.Tab
	} else if name != ClaimsViewID && name != RecoveryViewID {
		m.Selected, m.Index, m.Offset = "", 0, 0
		m.Detail, m.DetailOffset, m.Tab = false, 0, 0
	}
	if name != ClaimsViewID {
		m.anchor(m.rows())
	}
	if name == RecoveryViewID && m.LoadRecovery != nil {
		return m.LoadRecovery()
	}
	if name == ClaimsViewID && m.Claims.Refresh != nil && !m.Claims.Loading {
		m.Claims.Loading = true
		return m.Claims.Refresh(m.Claims.Cursor)
	}
	return nil
}

// viewKey handles view switching keys shared by the list and Recovery view.
func (m *Model) viewKey(key string) (tea.Cmd, bool) {
	if len(m.Views) == 0 {
		return nil, false
	}
	current := max(0, slices.Index(m.Views, m.ViewName))
	switch {
	case key == "v":
		return m.setView(m.Views[(current+1)%len(m.Views)]), true
	case key == "V":
		return m.setView(m.Views[(current+len(m.Views)-1)%len(m.Views)]), true
	case len(key) == 1 && key[0] >= '1' && key[0] <= '9':
		if index := int(key[0] - '1'); index < len(m.Views) {
			return m.setView(m.Views[index]), true
		}
		return nil, true
	}
	return nil, false
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
		updated := v.Snapshot
		if !v.prepared {
			updated = v.Snapshot.Clone()
		}
		projectionUsable := v.hasProjection && reflect.DeepEqual(v.projection, projectionForModel(m)) && sameSourceOrder(m.Sources, v.sourceIDs)
		preparedRowsUsable := projectionUsable
		for key, item := range updated.Items {
			if prior, ok := m.Snapshot.Items[key]; ok && len(prior.Resources) > 0 && item.Ref == prior.Ref {
				beforeItem := item
				beforeClaim := item.Claim
				gapInvalidated := m.RebuildingClaims && prior.Claim.Stale && prior.Claim.Reason == "history-gap"
				if gapInvalidated || !prior.Claim.ObservedAt.Before(item.Claim.ObservedAt) {
					item.Claim, item.Resources, item.KeyInputs = prior.Claim, prior.Resources, prior.KeyInputs
				}
				if projectionUsable && beforeClaim != item.Claim {
					for name := range v.preparedCounts {
						beforeMatches := m.viewCountMatches(beforeItem, name)
						afterMatches := m.viewCountMatches(item, name)
						if beforeMatches != afterMatches {
							if afterMatches {
								v.preparedCounts[name]++
							} else {
								v.preparedCounts[name]--
							}
							if name == m.ViewName {
								preparedRowsUsable = false
							}
						}
					}
				}
				updated.Items[key] = item
				if index, ok := v.rowIndexes[key]; ok {
					v.allRows[index] = item
				}
				if index, ok := v.preparedRowIndexes[key]; ok {
					v.preparedRows[index] = item
				}
			}
		}
		m.Snapshot = updated
		m.orderedKeys = v.orderedKeys
		if !sameSourceOrder(m.Sources, v.sourceIDs) {
			m.orderedKeys = nil
		}
		if preparedRowsUsable {
			m.cachePreparedRows(m.rowKey(), v.preparedRows, v.preparedCounts, v.preparedObserved, v.preparedEdges)
		}
		if !preparedRowsUsable && v.allRows != nil && m.ViewName == "All" && m.Filter == "" && len(m.ViewFilters) == 0 && len(m.ViewRules) == 0 && sameSourceOrder(m.Sources, v.sourceIDs) {
			m.cacheRows(m.rowKey(), v.allRows)
		}
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
			if current, ok := m.Snapshot.Items[key]; ok && (item.Claim.Stale || item.Claim.Reason == "authority-mismatch" || !item.Claim.ObservedAt.Before(current.Claim.ObservedAt)) {
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
	case WritePreviewMsg:
		m.WriteLoading = false
		if v.Err != nil {
			m.Notice = "Write unavailable: " + v.Err.Error()
			m.WritePreview = nil
		} else if v.Preview != nil && v.Preview.Identity == m.Selected {
			m.WritePreview = v.Preview
			m.Notice = "Review provider write; press Enter to confirm"
		}
	case ReconcileResultMsg:
		m.Writing = false
		if v.Err != nil {
			m.Notice = "Reconciliation refused; claim held: " + v.Err.Error()
		} else if v.Status == "checkpoint-missing" {
			m.UncertainWrite = false
			m.Notice = "Provider verified; expired checkpoint missing; claim remains held · " + v.OperationID
		} else {
			m.Notice = "Reconciliation recorded; claim remains held · " + v.OperationID
		}
		if m.LoadRecovery != nil {
			return m, m.LoadRecovery()
		}
	case WriteResultMsg:
		m.Writing = false
		m.WritePreview = nil
		if v.Result.Outcome == queue.WriteVerified && v.Err == nil {
			m.Notice = "Provider write verified; claim remains held · " + v.OperationID
		} else {
			m.UncertainWrite = m.UncertainWrite || v.Result.ClaimHeld
			m.Notice = fmt.Sprintf("Provider write %s; claim held %t · recovery %s: %v", v.Result.Outcome, v.Result.ClaimHeld, v.OperationID, v.Err)
		}
		if m.LoadRecovery != nil {
			return m, m.LoadRecovery()
		}
	case RecoveryMsg:
		if v.Err != nil {
			m.RecoveryError = v.Err.Error()
		} else {
			m.RecoveryError = ""
			m.Recovery = v.Entries
			m.RecoveryIndex = max(0, min(m.RecoveryIndex, len(m.Recovery)-1))
		}
	case ClaimsRefreshMsg:
		history := m.applyClaimsRefresh(v)
		if m.ViewName == ClaimsViewID && m.Claims.Refresh != nil {
			return m, tea.Batch(history, tea.Tick(claimsPollInterval, func(time.Time) tea.Msg { return ClaimsTickMsg{} }))
		}
		return m, history
	case ClaimHistoryMsg:
		if v.ClaimID == m.Claims.Selected && v.Resource == m.Claims.HistoryResource {
			m.Claims.HistoryLoading = false
			if v.Err != nil {
				m.Claims.HistoryError = v.Err.Error()
			} else {
				m.Claims.HistoryError = ""
				m.Claims.History = v.Page
			}
		}
	case ClaimsTickMsg:
		if m.ViewName == ClaimsViewID && m.Claims.Refresh != nil && !m.Claims.Loading {
			m.Claims.Loading = true
			return m, m.Claims.Refresh(m.Claims.Cursor)
		}
	case RefreshedMsg:
		if v.Err != nil {
			m.Notice = "Refresh failed: " + v.Err.Error()
		} else {
			m.Notice = "Refreshed"
		}
	case StartPreviewMsg:
		m.ClaimLoading = false
		if v.Identity == m.Selected {
			if v.Err != nil || v.Preview == nil || v.Preview.Claim.Identity != v.Identity {
				m.StartPreview = nil
				m.Notice = "Start work unavailable; Claim only: " + fmt.Sprint(v.Err)
			} else {
				m.StartPreview = v.Preview
				m.Notice = "Review Start work preview; press Enter to confirm"
			}
		}
	case StartResultMsg:
		m.Claiming = false
		m.StartPreview = nil
		updated, _ := m.Update(v.Claim)
		m = updated.(Model)
		if v.Write != nil && v.TransitionStep == "unknown" {
			m.UncertainWrite = true
		}
		if v.ClaimStep == "applied" {
			m.Notice = "Start work: claim applied; transition " + v.TransitionStep
			if v.TransitionStep == "rejected" {
				m.Notice += " · Claim acquired; status unchanged; correct mapping or cancel if eligible"
			}
			if v.TransitionStep == "unknown" {
				m.UncertainWrite = true
				m.Notice += " · recovery required; do not retry or roll back"
			}
			if v.Err != nil {
				m.Notice += ": " + v.Err.Error()
			} else if v.Write != nil && v.Write.Err != nil {
				m.Notice += ": " + v.Write.Err.Error()
			}
		} else {
			m.Notice = "Start work: claim " + v.ClaimStep + "; transition not attempted · " + claimFailureNotice(v.Claim.Err)
		}
		if v.Write != nil && m.LoadRecovery != nil {
			return m, m.LoadRecovery()
		}
	case ClaimPreviewMsg:
		m.ClaimLoading = false
		if v.Identity == m.Selected {
			if v.Err != nil {
				m.ClaimPreview = nil
				m.Notice = "Claim unavailable: " + v.Err.Error()
				if classified := reason.As(v.Err); classified != nil && classified.Details["holder"] != nil {
					m.Notice = claimFailureNotice(v.Err)
				}
			} else if v.Preview == nil || v.Preview.Identity != v.Identity {
				m.ClaimPreview = nil
				m.Notice = "Claim unavailable: preview identity changed"
			} else {
				if current, ok := m.Snapshot.Items[v.Item.Ref.Key()]; ok {
					if current.Ref != v.Item.Ref || current.Order != v.Item.Order {
						m.orderedKeys = nil
					}
					v.Item.Claim, v.Item.Resources, v.Item.KeyInputs = current.Claim, current.Resources, current.KeyInputs
				}
				m.Snapshot.Items[v.Item.Ref.Key()] = v.Item
				m.rowCache = &rowCache{}
				m.ClaimPreview = v.Preview
				m.Notice = "Review claim preview; press Enter to confirm"
			}
		}
	case OwnedClaimMsg:
		if v.Path != "" {
			if v.Gone {
				delete(m.OwnedClaims, v.Path)
				break
			}
			if m.OwnedClaims == nil {
				m.OwnedClaims = map[string]OwnedClaimMsg{}
			}
			if previous, ok := m.OwnedClaims[v.Path]; ok && v.ClaimID == "" {
				previous.Verified = false
				previous.LastResult = v.LastResult
				v = previous
			}
			m.OwnedClaims[v.Path] = v
		}
	case LaunchResultMsg:
		m.Launching = false
		if v.Err != nil {
			m.Notice = "Launch failed: " + v.Err.Error()
		} else {
			m.Notice = "Launched " + clean(v.Name) + "; awaiting worker claim in overlay"
		}
	case CancelClaimMsg:
		m.Cancelling = false
		if v.Err != nil {
			m.Notice = "Cancellation refused: " + v.Err.Error()
		} else {
			delete(m.OwnedClaims, v.Path)
			m.Notice = "No-effect claim cancelled"
		}
	case ClaimResultMsg:
		m.Claiming = false
		m.ClaimPreview = nil
		if v.Err != nil {
			if classified := reason.As(v.Err); classified != nil {
				pendingPath, _ := classified.Details["pendingPath"].(string)
				if pendingPath != "" && (classified.Reason == reason.ReasonUnknownOutcome || classified.Details["commitState"] == "unknown" || classified.Details["commitState"] == "committed") {
					if m.OwnedClaims == nil {
						m.OwnedClaims = map[string]OwnedClaimMsg{}
					}
					m.OwnedClaims[pendingPath] = OwnedClaimMsg{Path: pendingPath, LastResult: "claim outcome uncertain; retain private recovery handle"}
				}
			}
			m.Notice = fmt.Sprintf("Claim %s: %s", clean(v.Identity), claimFailureNotice(v.Err))
		} else {
			if v.HandlePath != "" {
				if m.OwnedClaims == nil {
					m.OwnedClaims = map[string]OwnedClaimMsg{}
				}
				m.OwnedClaims[v.HandlePath] = OwnedClaimMsg{Path: v.HandlePath, ClaimID: v.ClaimID, Resources: v.Resources, ExpiresAt: v.Claim.ExpiresAt, NextRenewal: v.NextRenewal, Verified: true, LastResult: "acquired"}
			}
			if current, ok := m.Snapshot.Items[v.Item.Ref.Key()]; ok && !current.Claim.Stale && current.Claim.Reason != "authority-mismatch" {
				current.Claim = v.Claim
				m.Snapshot.Items[v.Item.Ref.Key()] = current
				m.rowCache = &rowCache{}
			}
			m.Notice = fmt.Sprintf("Claim %s acquired · TTL %s · expires %s", clean(v.Identity), v.GrantedTTL, v.Claim.ExpiresAt.UTC().Format(time.RFC3339))
		}
	case tea.KeyMsg:
		key := v.String()
		switch m.mode() {
		case modeStartPreview:
			switch key {
			case "enter", "y":
				preview := *m.StartPreview
				m.StartPreview = nil
				if item, ok := m.selected(m.rows()); ok && identity(item) == preview.Claim.Identity && m.StartWork != nil {
					m.Claiming = true
					m.Notice = "Revalidating and starting work…"
					return m, m.StartWork(item, preview)
				}
				m.Notice = "Start work preview expired because selection changed"
			case "esc", "n":
				m.StartPreview = nil
				m.Notice = "Start work cancelled before claiming"
			}
			return m, nil
		case modeWritePreview:
			switch key {
			case "enter", "y":
				preview := *m.WritePreview
				m.WritePreview = nil
				if m.Selected == preview.Identity && m.ConfirmWrite != nil {
					m.Writing = true
					m.Notice = "Revalidating provider write…"
					return m, m.ConfirmWrite(preview)
				}
				m.Notice = "Write preview expired or unavailable"
			case "esc", "n":
				m.WritePreview = nil
				m.Notice = "Provider write cancelled before dispatch"
			}
			return m, nil
		case modeWriteChoices:
			switch key {
			case "j", "down":
				m.WriteChoiceIndex = min(m.WriteChoiceIndex+1, len(m.WriteChoices)-1)
			case "k", "up":
				m.WriteChoiceIndex = max(0, m.WriteChoiceIndex-1)
			case "enter":
				choice := m.WriteChoices[m.WriteChoiceIndex]
				m.WriteChoices = nil
				if item, ok := m.selected(m.rows()); ok && m.PreviewWrite != nil {
					m.WriteLoading = true
					return m, m.PreviewWrite(item, choice.Action, choice.Transition, "")
				}
			case "esc":
				m.WriteChoices = nil
			}
			return m, nil
		case modeRecoveryEvidence:
			switch key {
			case "esc":
				m.RecoveryEvidence = false
				m.Input = ""
			case "enter":
				evidence := strings.TrimSpace(m.Input)
				m.Input, m.RecoveryEvidence = "", false
				selected := m.RecoveryEvidenceEntry
				current := slices.IndexFunc(m.Recovery, func(entry queue.RecoveryEntry) bool {
					return entry.OperationID == selected.OperationID && entry.Status == selected.Status
				})
				if current < 0 {
					m.Notice = "Recovery operation changed; review it and attest again"
					return m, nil
				}
				entry := m.Recovery[current]
				if entry.Status == "checkpoint-pending" && strings.HasPrefix(evidence, "PROVIDER VERIFIED; CHECKPOINT ABSENT; EXECUTOR STOPPED: ") && len(evidence) > len("PROVIDER VERIFIED; CHECKPOINT ABSENT; EXECUTOR STOPPED: ")+10 && m.AttestCheckpointMissing != nil {
					m.Writing = true
					return m, m.AttestCheckpointMissing(entry, evidence)
				}
				if entry.Status == "unknown" && strings.HasPrefix(evidence, "NO COMMIT; EXECUTOR STOPPED: ") && len(evidence) > len("NO COMMIT; EXECUTOR STOPPED: ")+10 && m.ReconcileRecovery != nil {
					m.Writing = true
					return m, m.ReconcileRecovery(entry, evidence)
				}
				m.Notice = "Type the matching attestations and audit evidence to reconcile"
			default:
				m.editInput(v)
			}
			return m, nil
		case modeWriteInput:
			switch key {
			case "esc":
				m.WriteInput = false
				m.Input = ""
			case "enter":
				text := strings.TrimSpace(m.Input)
				m.Input, m.WriteInput = "", false
				if item, ok := m.selected(m.rows()); ok && identity(item) == m.WriteInputIdentity && text != "" && m.PreviewWrite != nil {
					m.WriteLoading = true
					return m, m.PreviewWrite(item, queue.ActionRecordProgress, "", text)
				}
				m.Notice = "Progress empty or selection changed"
			default:
				m.editInput(v)
			}
			return m, nil
		case modeCancel:
			path := m.CancelPreview
			switch key {
			case "enter", "y":
				m.CancelPreview = ""
				if m.CancelClaim != nil {
					m.Cancelling = true
					return m, m.CancelClaim(path)
				}
			case "esc", "n":
				m.CancelPreview = ""
				m.Notice = "Cancellation dismissed"
			}
			return m, nil
		case modeQuit:
			switch key {
			case "enter", "y", "q":
				return m, tea.Quit
			case "esc", "n":
				m.Quitting = false
				m.Notice = "Exit cancelled"
			}
			return m, nil
		case modeFilter, modePalette:
			switch key {
			case "esc":
				m.Filtering = false
				m.Palette = false
				m.Input = ""
			case "enter":
				if m.Filtering {
					if m.ViewName == ClaimsViewID {
						m.Claims.ResourcePrefix = m.Input
						m.anchorClaims(m.claimRows(), max(1, m.frame(m.rows()).bodyHeight-1))
					} else {
						m.Filter = m.Input
						m.anchor(m.rows())
					}
				} else if strings.EqualFold(strings.TrimSpace(m.Input), "start work") {
					m.Palette = false
					m.Input = ""
					return m.startWorkPreview()
				} else {
					m.Notice = "Command unavailable: " + m.Input
				}
				m.Filtering = false
				m.Palette = false
				m.Input = ""
			default:
				m.editInput(v)
			}
			return m, nil
		case modeHelp:
			// Help covers the body, so other keys would act on hidden state.
			switch key {
			case "?", "esc", "q", "h", "left":
				m.Help = false
			case "ctrl+c":
				return m.requestQuit()
			}
			return m, nil
		case modeRecovery:
			if cmd, ok := m.viewKey(key); ok {
				return m, cmd
			}
			switch key {
			case "?":
				m.Help = true
			case "j", "down":
				m.RecoveryIndex = min(len(m.Recovery)-1, m.RecoveryIndex+1)
			case "k", "up":
				m.RecoveryIndex = max(0, m.RecoveryIndex-1)
			case "r":
				if !m.Writing && m.RetryRecovery != nil && len(m.Recovery) > 0 {
					m.Writing = true
					return m, m.RetryRecovery(m.Recovery[m.RecoveryIndex])
				}
			case "e":
				if len(m.Recovery) > 0 && len(m.Recovery[m.RecoveryIndex].Next) > 1 && !m.Writing {
					m.RecoveryEvidence = true
					m.RecoveryEvidenceEntry = m.Recovery[m.RecoveryIndex]
					m.Input = ""
				} else {
					m.Notice = "Attestation unavailable while dispatch or verification remains possible"
				}
			case "u":
				if m.LoadRecovery != nil {
					return m, m.LoadRecovery()
				}
			case "q", "ctrl+c":
				return m.requestQuit()
			}
			return m, nil
		case modeClaims:
			return m.updateClaimsKey(key)
		case modeLaunch:
			rows := m.rows()
			switch key {
			case "j", "down":
				m.LaunchIndex = min(m.LaunchIndex+1, len(m.LaunchOptions)-1)
			case "k", "up":
				m.LaunchIndex = max(0, m.LaunchIndex-1)
			case "enter", "y":
				option := m.LaunchOptions[m.LaunchIndex]
				if !option.Eligibility.Eligible {
					m.Notice = "Launch unavailable: " + strings.Join(option.Eligibility.Reasons, "; ")
				} else if item, ok := m.selected(rows); ok && identity(item) == m.LaunchIdentity && item.Ref == m.LaunchRef && m.Launch != nil {
					m.LaunchOptions = nil
					m.Launching = true
					m.Notice = "Revalidating launch…"
					return m, m.Launch(item, option.Name)
				} else {
					m.LaunchOptions = nil
					m.Notice = "Launch preview expired because the selected item changed"
				}
			case "esc", "n":
				m.LaunchOptions = nil
				m.Notice = "Launch dismissed"
			}
			return m, nil
		case modeClaimPreview:
			rows := m.rows()
			switch key {
			case "enter", "y":
				if m.AcquireClaim == nil {
					m.Notice = "Claim unavailable"
					m.ClaimPreview = nil
					return m, nil
				}
				if item, ok := m.selected(rows); ok && identity(item) == m.ClaimPreview.Identity {
					preview := *m.ClaimPreview
					m.ClaimPreview = nil
					m.Claiming = true
					m.Notice = "Revalidating and acquiring claim…"
					return m, m.AcquireClaim(item, preview)
				}
				m.ClaimPreview = nil
				m.Notice = "Claim preview expired because selection changed"
				return m, nil
			case "esc", "n":
				m.ClaimPreview = nil
				m.Notice = "Claim cancelled; no claim was sent"
				return m, nil
			default:
				return m, nil
			}
		}
		rows := m.rows()
		previous := m.Selected
		pendingG := m.pendingG
		m.pendingG = false
		if cmd, ok := m.viewKey(key); ok {
			if cmd != nil {
				return m, cmd
			}
			return m.afterNavigation(previous)
		}
		switch key {
		case "q", "ctrl+c":
			return m.requestQuit()
		case "j", "down":
			m.move(1)
		case "k", "up":
			m.move(-1)
		case "pgdown", "ctrl+d":
			m.DetailOffset = min(m.DetailOffset+max(1, m.Height/2), m.maxDetailOffset())
		case "pgup", "ctrl+u":
			m.DetailOffset = max(0, min(m.DetailOffset, m.maxDetailOffset())-max(1, m.Height/2))
		case "g":
			if pendingG {
				m.move(-len(rows))
			} else {
				m.pendingG = true
			}
		case "G":
			m.move(len(rows))
		case "l", "right", "enter":
			if len(rows) > 0 {
				m.Detail = true
			}
		case "h", "left":
			m.Detail = false
		case "esc":
			// Close one layer at a time: detail, then the applied filter.
			if m.Detail {
				m.Detail = false
			} else if m.Filter != "" {
				m.Filter, m.Notice = "", ""
				m.anchor(m.rows())
			}
		case "tab":
			if m.Detail {
				m.Tab = (m.Tab + 1) % len(tabNames)
				m.DetailOffset = 0
			} else {
				m.Detail = true
			}
		case "shift+tab":
			if m.Detail {
				m.Tab = (m.Tab + len(tabNames) - 1) % len(tabNames)
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
			if m.ViewName == RecoveryViewID && m.LoadRecovery != nil {
				return m, m.LoadRecovery()
			}
			if m.Refresh != nil {
				return m, m.Refresh()
			}
			m.Notice = "Refresh unavailable"
		case "o":
			if i, ok := m.selected(rows); ok && m.OpenURL != nil {
				return m, m.OpenURL(i)
			}
			m.Notice = "Provider URL unavailable"
		case "S":
			if m.Detail {
				return m.startWorkPreview()
			}
		case "c":
			if m.ClaimLoading || m.Claiming {
				m.Notice = "Claim request in progress"
			} else if m.PreviewClaim == nil {
				m.Notice = "Claim unavailable"
			} else if item, ok := m.selected(rows); ok {
				m.ClaimLoading = true
				m.Notice = "Checking claim eligibility…"
				return m, m.PreviewClaim(item)
			} else {
				m.Notice = "No item selected"
			}
		case "R":
			if item, ok := m.selected(rows); ok {
				for path, owned := range m.OwnedClaims {
					if !owned.Verified || owned.Lost || !sameResources(item.Resources, owned.Resources) {
						continue
					}
					if m.CancelClaim != nil {
						m.CancelPreview = path
						return m, nil
					}
				}
			}
			m.Notice = "Release unavailable: no verified queue-owned claim; provider checkpoint required for effected claims"
		case "x":
			if m.Launching {
				m.Notice = "Launch in progress"
			} else if item, ok := m.selected(rows); ok && m.PreviewLaunch != nil {
				m.LaunchOptions = m.PreviewLaunch(item)
				m.LaunchIdentity = identity(item)
				m.LaunchRef = item.Ref
				m.LaunchIndex = 0
				if len(m.LaunchOptions) == 0 {
					m.LaunchOptions = nil
					m.Notice = "No launch actions configured"
				}
			} else {
				m.Notice = "Launch unavailable: no item selected"
			}
		case "s":
			if item, ok := m.selected(rows); ok && !m.Writing && !m.WriteLoading {
				choices := m.StateChoices[item.Ref.SourceID]
				if len(choices) == 0 {
					m.Notice = "State change unavailable: no configured provider transitions"
				} else {
					m.WriteChoices = choices
					m.WriteChoiceIndex = 0
				}
			}
		case "p":
			if m.PreviewWrite == nil {
				m.Notice = "Progress unavailable"
				break
			}
			if item, ok := m.selected(rows); ok && !m.Writing && !m.WriteLoading {
				m.WriteInput = true
				m.WriteInputIdentity = identity(item)
				m.Input = ""
			}
		case "a":
			if m.PreviewWrite == nil {
				m.Notice = "Assignment unavailable"
				break
			}
			if item, ok := m.selected(rows); ok && !m.Writing && !m.WriteLoading {
				m.WriteLoading = true
				return m, m.PreviewWrite(item, queue.ActionAssignToMe, "", "")
			}
		}
		return m.afterNavigation(previous)
	case tea.MouseMsg:
		return m.mouse(v)
	}
	return m, nil
}

// afterNavigation loads lazily fetched detail for a new selection or tab.
func (m Model) afterNavigation(previous string) (tea.Model, tea.Cmd) {
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
	return m, nil
}
func (m Model) startWorkPreview() (tea.Model, tea.Cmd) {
	if m.ClaimLoading || m.Claiming || m.Writing || m.WriteLoading {
		m.Notice = "Start work request in progress"
		return m, nil
	}
	if item, ok := m.selected(m.rows()); ok && m.StartTransitions[item.Ref.SourceID] != "" && m.PreviewStart != nil {
		m.ClaimLoading = true
		m.Notice = "Checking Start work eligibility…"
		return m, m.PreviewStart(item)
	}
	m.Notice = "Start work unavailable: no supported provider mapping; Claim only"
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
func sameResources(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// requestQuit refuses exit while a request outcome is pending and shows the
// exit consequences whenever claims or recovery state remain.
func (m Model) requestQuit() (tea.Model, tea.Cmd) {
	if m.Claiming || m.ClaimLoading || m.Cancelling || m.Launching || m.Writing || m.WriteLoading {
		m.Notice = "Wait for claim, launch, or write request outcome before exiting"
		return m, nil
	}
	if len(m.OwnedClaims) > 0 || m.UncertainWrite || len(m.Recovery) > 0 || m.RecoveryError != "" {
		m.Quitting = true
		return m, nil
	}
	return m, tea.Quit
}

func claimFailureNotice(err error) string {
	if err == nil {
		return "Claim failed"
	}
	classified := reason.As(err)
	if classified == nil {
		return "Claim failed: " + err.Error()
	}
	if holder, ok := classified.Details["holder"]; ok {
		data, _ := json.Marshal(holder)
		var view map[string]any
		_ = json.Unmarshal(data, &view)
		agent, _ := view["agentId"].(string)
		expires, _ := view["expiresAt"].(string)
		if agent == "" {
			agent = "another holder"
		}
		if expires == "" {
			expires = "expiry unavailable"
		}
		return "Claim contended · " + agent + " · expires " + expires + " · not retried"
	}
	if classified.Reason == reason.ReasonInvalidArgument && classified.Details["commitState"] == "not-committed" {
		return "Authority rejected the claim request (invalid-argument); no claim was acquired"
	}
	if classified.Reason == reason.ReasonUnknownOutcome || classified.Details["commitState"] == "unknown" {
		path, _ := classified.Details["pendingPath"].(string)
		if path != "" {
			return "Claim outcome uncertain; pending request retained at " + path + " · recover before retrying"
		}
		return "Claim outcome uncertain; recover the pending request before retrying"
	}
	return "Claim failed: " + err.Error()
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
	if name == RecoveryViewID {
		return len(m.Recovery)
	}
	if name == ClaimsViewID {
		return len(m.Claims.Items)
	}
	n := 0
	for _, item := range m.Snapshot.Items {
		if m.viewCountMatches(item, name) {
			n++
		}
	}
	return n
}

func (m Model) viewCountMatches(item queue.Item, name string) bool {
	rule := m.ViewRules[name]
	filters := m.ViewFilters[name]
	if len(filters.SourceIDs) > 0 {
		found := false
		for _, id := range filters.SourceIDs {
			if id == item.Ref.SourceID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if rule.Readiness != "" && rule.Readiness != "all" && string(item.Readiness.Status) != rule.Readiness {
		return false
	}
	if rule.Claim != "" && rule.Claim != "all" && !matchesClaim(item, rule.Claim) {
		return false
	}
	if len(rule.Assigned) > 0 {
		found := false
		for _, kind := range rule.Assigned {
			if kind == "nobody" && len(item.AssignedTo) == 0 {
				found = true
			}
			for _, person := range item.AssignedTo {
				if (kind == "me" && m.isMe(item, person)) || kind == person {
					found = true
				}
			}
		}
		if !found {
			return false
		}
	}
	if _, configured := m.ViewRules[name]; !configured {
		switch name {
		case "Ready":
			if item.Readiness.Status != queue.Ready {
				return false
			}
		case "Mine":
			if m.Me == "" {
				return false
			}
			found := false
			for _, person := range item.AssignedTo {
				if m.isMe(item, person) {
					found = true
				}
			}
			if !found {
				return false
			}
		case "Claimed":
			if !item.Claim.Active {
				return false
			}
		}
	}
	return true
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
func projectStatusRaw(item queue.Item) string {
	if item.ProjectStatusRaw == "" {
		switch item.ProjectStatusReason {
		case "project-item-missing":
			return "(no project item)"
		case "project-status-unset":
			return "(unset)"
		default:
			return "(unknown)"
		}
	}
	return item.ProjectStatusRaw
}

func projectStatusState(item queue.Item) string {
	if item.ProjectStatusState == "" {
		return "unknown"
	}
	return item.ProjectStatusState
}

func projectStatusDisplay(item queue.Item) string {
	if !item.ProjectStatusBound {
		return item.RawStatus
	}
	return projectStatusRaw(item) + " → " + projectStatusState(item)
}

func (m Model) displayState(i queue.Item) string {
	if i.ReadPermission == queue.Denied {
		return "denied"
	}
	if !i.Fresh {
		return "stale"
	}
	// Finished work is never selectable; its readiness would only add noise.
	if i.Terminal && i.TerminalKnown && !i.Claim.Active {
		return "done"
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
