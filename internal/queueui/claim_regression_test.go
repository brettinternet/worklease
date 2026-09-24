package queueui

import (
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/reason"
)

func TestClaimResultAfterNavigationStillUpdatesClaimAndReportsUncertainty(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	first := m.Snapshot.Items[queue.Ref{SourceID: "a", ItemID: "1"}.Key()]
	m.Claiming = true
	m, _ = press(m, "j")
	if m.Selected == identity(first) {
		t.Fatal("selection did not move")
	}
	grant := queue.ClaimObservation{Known: true, Active: true, State: "held", AcquiredAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}
	next, _ := m.Update(ClaimResultMsg{Identity: identity(first), Item: first, Claim: grant, GrantedTTL: time.Minute})
	m = next.(Model)
	if !m.Snapshot.Items[first.Ref.Key()].Claim.Active || !strings.Contains(m.Notice, identity(first)) {
		t.Fatalf("background grant lost: %+v, %s", m.Snapshot.Items[first.Ref.Key()].Claim, m.Notice)
	}
	m.Claiming = true
	pending := reason.New(reason.ReasonUnknownOutcome, "request outcome unknown").With("pendingPath", "/private/claim.json")
	next, _ = m.Update(ClaimResultMsg{Identity: identity(first), Item: first, Err: pending})
	m = next.(Model)
	if m.Claiming || !strings.Contains(m.Notice, "/private/claim.json") || !strings.Contains(m.Notice, identity(first)) {
		t.Fatalf("background uncertainty lost: %s", m.Notice)
	}
}

func TestStaleAuthorityOverlaySupersedesEarlierGrantTimestamp(t *testing.T) {
	m := New(fixture())
	m.Sources = []queue.Source{{ID: "a"}}
	m.anchor(m.rows())
	first := m.Snapshot.Items[queue.Ref{SourceID: "a", ItemID: "1"}.Key()]
	now := time.Now().UTC()
	first.Claim = queue.ClaimObservation{Known: true, Active: true, OwnerVerified: true, ObservedAt: now}
	m.Snapshot.Items[first.Ref.Key()] = first
	stale := m.Snapshot.Clone()
	item := stale.Items[first.Ref.Key()]
	item.Claim = queue.ClaimObservation{Stale: true, Reason: "authority-mismatch", ObservedAt: now.Add(-time.Minute)}
	stale.Items[first.Ref.Key()] = item
	next, _ := m.Update(ClaimOverlayMsg{Snapshot: stale, Err: reason.New(reason.ReasonAuthorityMismatch, "restored")})
	m = next.(Model)
	got := m.Snapshot.Items[first.Ref.Key()].Claim
	if !got.Stale || got.OwnerVerified || got.Active || got.Reason != "authority-mismatch" {
		t.Fatalf("older invalidation left grant actionable: %+v", got)
	}
}

func TestClaimPreviewShowsFullResourcesAndLimitsAtNarrowWidth(t *testing.T) {
	m := New(fixture())
	m.Width = 40
	key := "coordination:generic:" + strings.Repeat("x", 70) + "-distinct-suffix"
	limits := "remote admission and coordination: " + strings.Repeat("y", 65) + "-limit-suffix"
	m.ClaimPreview = &ClaimPreview{Title: "item", Resources: []string{key}, CoordinationLimits: limits}
	view := m.View()
	if !strings.Contains(view, "-distinct-suffix") || !strings.Contains(view, "-limit-suffix") || !strings.Contains(strings.ReplaceAll(view, "\n", ""), key) {
		t.Fatalf("preview hides exact inputs: %s", view)
	}
}
