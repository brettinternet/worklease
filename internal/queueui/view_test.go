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

func TestResourceDisplayDecodesOnlyForRendering(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ key, want string }{
		{"github:owner%2Frepo#item%3A1", "github:owner/repo#item:1"},
		{"path:src%2Ffile%252Fname.go", "path:src/file%2Fname.go"},
		{"path:bad%ZZ", "path:bad%ZZ"},
		{"path:src%2F%1B%5B2Jfile", "path:src%2F%1B%5B2Jfile"},
	} {
		if got := displayResource(test.key); got != test.want {
			t.Errorf("displayResource(%q)=%q, want %q", test.key, got, test.want)
		}
	}

	resource := "github:owner%2Frepo#item%3A1"
	snapshot := fixture()
	item := snapshot.Items[queue.Ref{SourceID: "a", ItemID: "1"}.Key()]
	item.Resources = []string{resource}
	snapshot.Items[item.Ref.Key()] = item
	m := New(snapshot)
	m.Width, m.Height = 160, 30
	m.Selected, m.Detail, m.Tab = item.CanonicalID, true, 3
	if screen := screenText(m.View()); !strings.Contains(screen, "github:owner/repo#item:1") || strings.Contains(screen, "%2F") {
		t.Fatalf("item claim detail resource not decoded: %s", screen)
	}
	if body := m.claimPreviewView(ClaimPreview{Resources: []string{resource}}).body; !strings.Contains(body, "github:owner/repo#item:1") || strings.Contains(body, "%2F") {
		t.Fatalf("claim preview resource not decoded: %s", body)
	}
	if got := snapshot.Items[item.Ref.Key()].Resources[0]; got != resource {
		t.Fatalf("rendering changed exact claim key: %q", got)
	}
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
