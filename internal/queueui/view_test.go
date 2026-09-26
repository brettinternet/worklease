package queueui

import (
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/queue"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func layoutModel(width, height int) Model {
	m := New(benchSnapshot(200))
	m.Sources = []queue.Source{{ID: "source-0"}, {ID: "source-1"}, {ID: "source-2"}, {ID: "source-3"}, {ID: "source-4"}}
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = next.(Model)
	m.anchor(m.rows())
	return m
}

func TestViewLinesFitTerminal(t *testing.T) {
	t.Parallel()
	for _, width := range []int{60, 80, 100, 110, 119, 120, 160} {
		for _, detail := range []bool{false, true} {
			m := layoutModel(width, 24)
			m.Detail = detail
			lines := strings.Split(m.View(), "\n")
			if len(lines) > 24 {
				t.Errorf("width %d detail %v: %d lines exceed height 24", width, detail, len(lines))
			}
			for n, line := range lines {
				if w := ansi.StringWidth(line); w > width {
					t.Errorf("width %d detail %v: line %d is %d wide: %q", width, detail, n, w, ansi.Strip(line))
				}
			}
		}
	}
}

func TestSelectionStaysVisibleWhileScrolling(t *testing.T) {
	t.Parallel()
	for _, width := range []int{80, 110, 160} {
		m := layoutModel(width, 20)
		for step := 0; step < 60; step++ {
			m, _ = press(m, "j")
			if !strings.Contains(ansi.Strip(m.View()), "> "+m.rows()[m.Index].Ref.ItemID) {
				t.Fatalf("width %d: selected row %d scrolled off screen", width, m.Index)
			}
		}
	}
}
