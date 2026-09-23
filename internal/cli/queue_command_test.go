package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueindex"
	"github.com/brettinternet/worklease/internal/queueui"
	tea "github.com/charmbracelet/bubbletea"
)

type cachedClaimStatus struct{}

func (cachedClaimStatus) Status(_ context.Context, selector lease.Selector) (lease.Status, error) {
	return lease.Status{Resources: []lease.ResourceStatus{{Resource: selector.Resources[0], State: "active", Claim: &lease.ClaimView{AuthorityID: selector.AuthorityID, AgentID: "worker", Active: true}}}}, nil
}

func TestCachedSnapshotOverlaysHeldClaimWithoutRefresh(t *testing.T) {
	ref := queue.Ref{SourceID: "s", ItemID: "1"}
	cached := queue.Snapshot{Items: map[string]queue.Item{ref.Key(): {Summary: queue.Summary{Ref: ref}}}}
	sources := map[string]queue.ClaimSource{"s": {Source: queue.Source{ID: "s", Adapter: "generic", Locator: "project"}}}
	var stored sync.Map
	overlayCachedClaims(context.Background(), &cached, sources, queue.ClaimAuthority{API: cachedClaimStatus{}, ID: "authority"}, config.ProfilePaths{}, &stored)
	item := cached.Items[ref.Key()]
	if !item.Claim.Active || item.Claim.AgentID != "worker" {
		t.Fatalf("cached claim was not observed: %+v", item.Claim)
	}
	if applied, ok := stored.Load(ref.Key()); !ok || !applied.(queue.Item).Claim.Active {
		t.Fatalf("cached claim was not retained for hydration: %v %v", applied, ok)
	}
}

func TestQueueFirstFrameUsesIndexBeforeProviderRefresh(t *testing.T) {
	ctx := context.Background()
	checkout := t.TempDir()
	if err := os.Mkdir(filepath.Join(checkout, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	index, err := queueindex.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	registry := queue.NewRegistry()
	source := queue.Source{ID: "local", Adapter: "backlog-md", Locator: checkout}
	adapter, _ := registry.Get(source.Adapter)
	partition, ok := queueindex.ForSource(adapter, source)
	if !ok {
		t.Fatal("local source did not supply a stable cache identity")
	}
	item := queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: source.ID, ItemID: "1"}, Title: "Cached first frame"}}
	if err := index.Replace(ctx, partition, []queue.Item{item}, true); err != nil {
		t.Fatal(err)
	}
	loader := queue.NewLoader(registry)
	if _, err := seedQueueIndex(ctx, index, registry, []queue.Source{source}, loader); err != nil {
		t.Fatal(err)
	}
	model := queueui.New(loader.Store.Current())
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	model = updated.(queueui.Model)
	if rendered := model.View(); !strings.Contains(rendered, "Cached firs") {
		t.Fatalf("cached first frame not rendered before provider refresh: %s", rendered)
	}
}

func TestHydratedSnapshotsReuseClaimOverlayWithoutAuthorityReads(t *testing.T) {
	var stored sync.Map
	ref := queue.Ref{SourceID: "s", ItemID: "a"}
	stored.Store(ref.Key(), queue.Item{Summary: queue.Summary{Ref: ref}, Resources: []string{"resource:a"}, Claim: queue.ClaimObservation{Known: true, Active: true, State: "active"}})
	for range 100 {
		snapshot := queue.Snapshot{Items: map[string]queue.Item{ref.Key(): {Summary: queue.Summary{Ref: ref, Title: "updated"}}}}
		applyStoredClaims(&snapshot, &stored)
		item := snapshot.Items[ref.Key()]
		if item.Title != "updated" || !item.Claim.Active || len(item.Resources) != 1 || item.Resources[0] != "resource:a" {
			t.Fatalf("hydration lost claim or new detail: %+v", item)
		}
	}
}

func TestRefreshCompletionWaitsForFailure(t *testing.T) {
	started := make(chan struct{})
	finish := make(chan error, 1)
	model := queueui.New(queue.Snapshot{})
	model.Refresh = func() tea.Cmd {
		return refreshCompletionCmd(func() <-chan error {
			close(started)
			return finish
		})
	}
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("refresh command missing")
	}
	model = next.(queueui.Model)
	completed := make(chan tea.Msg, 1)
	go func() { completed <- cmd() }()
	<-started
	select {
	case msg := <-completed:
		t.Fatalf("refresh completed before its blocked work: %#v", msg)
	case <-time.After(25 * time.Millisecond):
	}
	finish <- errors.New("source-read-failed")
	msg := <-completed
	next, _ = model.Update(msg)
	if got := next.(queueui.Model).Notice; got != "Refresh failed: source-read-failed" {
		t.Fatalf("refresh outcome = %q", got)
	}
}

func TestQueueCommandRejectsJSONWithoutEnteringTerminal(t *testing.T) {
	var out, errs bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "--json", "queue"}, "test", "unknown", "unknown", &out, &errs)
	if err == nil || !strings.Contains(out.String(), "text-only") {
		t.Fatalf("err=%v output=%s", err, out.String())
	}
}
func TestQueueSourceFailureLabels(t *testing.T) {
	for _, tt := range []struct{ message, want string }{{"API rate limit reached", "rate-limited"}, {"permission denied", "permission denied"}, {"connection refused", "offline/unavailable"}} {
		if got := queueSourceFailure(errString(tt.message)); got != tt.want {
			t.Errorf("%s: %s", tt.message, got)
		}
	}
	if !sourceSubset([]string{"a"}, []string{"a", "b"}) || sourceSubset([]string{"c"}, []string{"a", "b"}) {
		t.Fatal("view source subset mismatch")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
