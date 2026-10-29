// Package claimsui presents authority-wide claims without interpreting work items.
package claimsui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Model struct {
	Claims           ClaimsState
	Authority, Scope string
	Width, Height    int
	Input, Notice    string
	Filtering, Help  bool
	HighContrast     bool
	HelpOffset       int
	pendingG         bool
}

type mode int

const (
	modeClaims mode = iota
	modeHelp
	modeFilter
	modeClaimsReleaseReason
	modeClaimsActionPreview
)
const (
	detailChrome = 3
	detailTabRow = 2
	wheelStep    = 3
)

func New() Model { return Model{Width: 120, Height: 35} }

func (m Model) mode() mode {
	switch {
	case m.Claims.releaseReasonInput:
		return modeClaimsReleaseReason
	case m.Claims.actionPreview != nil:
		return modeClaimsActionPreview
	case m.Filtering:
		return modeFilter
	case m.Help:
		return modeHelp
	default:
		return modeClaims
	}
}

func (m Model) Init() tea.Cmd {
	if m.Claims.Refresh != nil {
		return m.Claims.Refresh(m.Claims.Cursor)
	}
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width, m.Height = v.Width, v.Height
	case ClaimsRefreshMsg:
		history := m.applyClaimsRefresh(v)
		var refresh tea.Cmd
		if m.Claims.refreshAfterMutation {
			m.Claims.refreshAfterMutation = false
			refresh = m.refreshClaimsAfterMutation()
		}
		if m.Claims.Refresh != nil {
			return m, tea.Batch(history, refresh, tea.Tick(claimsPollInterval, func(time.Time) tea.Msg { return ClaimsTickMsg{} }))
		}
		return m, tea.Batch(history, refresh)
	case ClaimsMutationMsg:
		return m, m.applyClaimsMutation(v)
	case ClaimContactMsg:
		if v.ClaimID == m.Claims.ContactClaimID {
			m.Claims.ContactLoading = false
			m.Claims.Contact = v.Results
		}
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
		if m.Claims.Refresh != nil && !m.Claims.Loading {
			m.Claims.Loading = true
			return m, m.Claims.Refresh(m.Claims.Cursor)
		}

	case tea.MouseMsg:
		if v.Action != tea.MouseActionPress || m.Width < 30 {
			return m, nil
		}
		if m.mode() == modeHelp && v.Button == tea.MouseButtonLeft {
			m.Help = false
		} else if m.mode() == modeClaims {
			return m.claimsMouse(v)
		}
	case tea.KeyMsg:
		key := v.String()
		switch m.mode() {
		case modeClaimsReleaseReason:
			switch key {
			case "esc":
				m.Claims.releaseReasonInput = false
				m.Claims.actionPreview = nil
				m.Input = ""
				m.Claims.Notice = "Release cancelled before confirmation"
			case "enter":
				reason := strings.TrimSpace(m.Input)
				if reason == "" {
					m.Claims.Notice = "Release reason cannot be empty"
					return m, nil
				}
				m.Claims.actionPreview.Reason = reason
				m.Claims.releaseReasonInput = false
				m.Input = ""
				m.Claims.Notice = "Review release preview; press Enter to confirm"
			case "ctrl+u":
				m.Input = ""
			default:
				m.editInput(v)
			}
			return m, nil
		case modeClaimsActionPreview:
			preview := m.Claims.actionPreview
			if preview == nil {
				return m, nil
			}
			switch key {
			case "enter", "y":
				action := *preview
				m.Claims.actionPreview = nil
				return m.dispatchClaimsAction(action)
			case "esc", "n":
				m.Claims.actionPreview = nil
				if preview.Action == ClaimsActionRelease {
					m.Claims.Notice = "Release cancelled; no request sent"
				} else {
					m.Claims.Notice = "Renewal cancelled; no request sent"
				}
			}
			return m, nil

		case modeFilter:
			switch key {
			case "esc":
				m.Filtering = false
				m.Input = ""
			case "enter":
				previous := m.Claims.Selected
				m.Claims.ResourcePrefix = m.Input
				m.Filtering = false
				m.Input = ""
				m.anchorClaims(m.claimRows(), max(1, m.frame().bodyHeight-1))
				if m.Claims.Selected != previous {
					m.clearClaimDetail()
					if m.Claims.Detail {
						return m, m.loadSelectedClaimDetail(m.claimRows())
					}
				}
			default:
				m.editInput(v)
			}
			return m, nil
		case modeHelp:
			switch key {
			case "?", "esc", "q", "h", "left":
				m.Help = false
			case "pgdown", "ctrl+f", "pgup", "ctrl+b", "ctrl+d", "ctrl+u", "ctrl+e", "ctrl+y":
				visible := max(1, m.frame().bodyHeight)
				m.HelpOffset = max(0, min(m.HelpOffset+scrollStep(key, visible), len(m.helpLines())-visible))
			case "ctrl+c":
				return m.requestQuit()
			}
			return m, nil
		default:
			return m.updateClaimsKey(key)
		}
	}
	return m, nil
}

func (m Model) requestQuit() (tea.Model, tea.Cmd) {
	if m.Claims.mutating {
		m.Notice = "Wait for Claims action outcome before exiting"
		return m, nil
	}
	return m, tea.Quit
}

func scrollStep(key string, visible int) int {
	step := max(1, visible-1)
	switch key {
	case "ctrl+d", "ctrl+u":
		step = max(1, visible/2)
	case "ctrl+e", "ctrl+y":
		step = 1
	}
	if key == "pgup" || key == "ctrl+b" || key == "ctrl+u" || key == "ctrl+y" {
		return -step
	}
	return step
}

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

func displayResource(resource string) string {
	decoded, err := url.PathUnescape(resource)
	if err != nil || !utf8.ValidString(decoded) || strings.IndexFunc(decoded, unicode.IsControl) >= 0 {
		return clean(resource)
	}
	return clean(decoded)
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
