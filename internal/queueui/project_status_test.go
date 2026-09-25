package queueui

import (
	"strings"
	"testing"

	"github.com/brettinternet/worklease/internal/queue"
)

func TestProjectStatusRawMappingAndConflictAreVisible(t *testing.T) {
	t.Parallel()
	item := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "issues", ItemID: "6"}, RawStatus: "OPEN", ProjectStatusBound: true, ProjectStatusKnown: true, ProjectStatusRaw: "Recently renamed", ProjectStatusState: "review", ProjectStatusConflict: true}}
	if got := projectStatusDisplay(item); got != "Recently renamed → review" {
		t.Fatalf("project raw option and mapping not displayed: %q", got)
	}
	details := detail(Model{Width: 120}, item)
	for _, want := range []string{"Issue state OPEN", "Project status Recently renamed → review", "CONFLICT: issue and project status disagree"} {
		if !strings.Contains(details, want) {
			t.Fatalf("detail does not show %q: %s", want, details)
		}
	}
	missing := item
	missing.ProjectStatusRaw = ""
	missing.ProjectStatusReason = "project-item-missing"
	if got := projectStatusRaw(missing); got != "(no project item)" {
		t.Fatalf("missing project item was rendered as %q", got)
	}
}
