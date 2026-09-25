package queue

import (
	"reflect"
	"testing"

	"github.com/brettinternet/worklease/internal/resource"
)

func selectionItem(source, id string, priority int, order string) Item {
	return Item{Summary: Summary{Ref: Ref{SourceID: source, ItemID: id}, State: StateOpen, Fresh: true, Priority: priority, Order: order}, Readiness: Readiness{Status: Ready}, Claim: ClaimObservation{Known: true, Available: true, State: "free"}, Resources: []string{source + id}, KeyInputs: &resource.Input{Provider: "generic", Source: source, Item: id}}
}

func TestSelectWaveOrderingResumeAndResourceConflict(t *testing.T) {
	resume := selectionItem("first", "resume", 1, "2")
	resume.State = StateInProgress
	first := selectionItem("first", "priority", 1, "1")
	lower := selectionItem("first", "lower", 3, "0")
	second := selectionItem("second", "higher", 1, "0")
	items := []Item{second, lower, resume, first}
	got := SelectWave(items, append([]Item{}, items...), []string{"first", "second"}, nil, true, 4, func(Item, string) bool { return false })
	want := []string{"priority", "resume", "lower", "higher"}
	ids := []string{}
	for _, candidate := range got.Candidates {
		ids = append(ids, candidate.Ref.ItemID)
	}
	if got.Result != "ready" || !reflect.DeepEqual(ids, want) {
		t.Fatalf("ordering/resume: %+v; ids=%v", got, ids)
	}
	selected := SelectWave(items, append([]Item{}, items...), []string{"first", "second"}, []Ref{second.Ref, first.Ref}, true, 2, func(Item, string) bool { return false })
	if len(selected.Candidates) != 2 || selected.Candidates[0].Ref != second.Ref || selected.Candidates[1].Ref != first.Ref {
		t.Fatalf("explicit selector order ignored: %+v", selected)
	}
	first.Resources = []string{"shared"}
	resume.Resources = []string{"shared"}
	items = []Item{first, resume, second}
	got = SelectWave(items, append([]Item{}, items...), []string{"first", "second"}, nil, true, 3, func(Item, string) bool { return false })
	if len(got.Candidates) != 2 || got.Candidates[0].Ref != first.Ref || got.Candidates[1].Ref != second.Ref || len(got.Excluded) != 1 || got.Excluded[0].Reasons[0] != "resource-conflict" {
		t.Fatalf("shared key was selected twice: %+v", got)
	}
}

func TestSelectWaveNoWorkReasonsAndIncomplete(t *testing.T) {
	base := selectionItem("source", "one", 0, "")
	blocked := base
	blocked.Readiness = Readiness{Status: Blocked, Reasons: []string{"hard-condition-unsatisfied"}}
	claimed := base
	claimed.Claim = ClaimObservation{Known: true, Active: true, State: "held"}
	elsewhere := base
	elsewhere.AssignedTo = []string{"@other"}
	unknown := base
	unknown.Readiness = Readiness{Status: ReadinessUnknown, Reasons: []string{"incomplete-closure"}}
	terminal := base
	terminal.Terminal, terminal.State = true, StateComplete
	blockedUnknownClaim := blocked
	blockedUnknownClaim.Claim = ClaimObservation{Reason: "unavailable"}
	unknownClaim := base
	unknownClaim.Claim = ClaimObservation{Reason: "unavailable"}
	for _, tc := range []struct {
		name     string
		items    []Item
		complete bool
		want     string
	}{
		{"empty", nil, true, "complete-and-empty"},
		{"all terminal", []Item{terminal}, true, "complete-and-empty"},
		{"blocked", []Item{blocked}, true, "blocked"},
		{"blocked with unknown claim", []Item{blockedUnknownClaim}, true, "blocked"},
		{"ready with unknown claim", []Item{unknownClaim}, true, "incomplete"},
		{"claimed", []Item{claimed}, true, "active-claims"},
		{"assigned elsewhere", []Item{elsewhere}, true, "assigned-elsewhere"},
		{"incomplete scope", []Item{base}, false, "incomplete"},
		{"incomplete edge", []Item{unknown, base}, true, "incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := SelectWave(tc.items, append([]Item{}, tc.items...), []string{"source"}, nil, tc.complete, 1, func(Item, string) bool { return false })
			if got.Result != tc.want || len(got.Candidates) != 0 {
				t.Fatalf("got %+v, want %q without candidates", got, tc.want)
			}
			if tc.name == "blocked" && (len(got.Excluded) != 1 || !reflect.DeepEqual(got.Excluded[0].Reasons, []string{"hard-condition-unsatisfied"})) {
				t.Fatalf("missing prerequisite exclusion: %+v", got)
			}
		})
	}
}
