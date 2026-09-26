package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueindex"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/testkit"
	tea "github.com/charmbracelet/bubbletea"
	urfave "github.com/urfave/cli/v3"
)

func TestQueueSourceFailureShowsBacklogDiagnosticCode(t *testing.T) {
	t.Parallel()
	if got := queueSourceFailure(queue.BacklogDiagnostic{Code: "provider-failed", Detail: "provider command failed"}); got != "provider-failed" {
		t.Fatalf("source failure = %q", got)
	}
}

func TestQueueTUIMeBySourceUsesExternalAccount(t *testing.T) {
	t.Parallel()
	for _, adapter := range []string{"external", "linear"} {
		got := queueMeBySource(config.QueueConfig{}, config.QueueSource{ID: "planning", Adapter: adapter, Account: "alice"})
		if len(got) != 1 || got[0] != "alice" {
			t.Fatalf("%s TUI identity = %v, want configured account", adapter, got)
		}
	}
}

type cachedClaimStatus struct{}

func (cachedClaimStatus) Status(_ context.Context, selector lease.Selector) (lease.Status, error) {
	return lease.Status{Resources: []lease.ResourceStatus{{Resource: selector.Resources[0], State: "active", Claim: &lease.ClaimView{AuthorityID: selector.AuthorityID, AgentID: "worker", Active: true}}}}, nil
}

func TestQueueRecoveryJSONListsUnresolvedRecordsWithoutQueueConfig(t *testing.T) {
	_, env := testkit.Home(t)
	t.Setenv("XDG_STATE_HOME", env["XDG_STATE_HOME"])
	t.Setenv("XDG_CACHE_HOME", env["XDG_CACHE_HOME"])
	var out, errs bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "--json", "queue", "recovery"}, "test", "unknown", "unknown", &out, &errs); err != nil {
		t.Fatalf("recovery JSON: %v %s", err, errs.String())
	}
	if !strings.Contains(out.String(), `"recovery":[]`) {
		t.Fatalf("missing empty recovery list: %s", out.String())
	}
	journal, err := queueRecoveryJournal()
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 32)
	record := queue.WriteRecord{Intent: queue.WriteIntent{OperationID: id, Ref: queue.Ref{SourceID: "tasks", ItemID: "TASK-1"}, ClaimID: "claim-1", Resources: []string{"resource:tasks"}, Action: queue.ActionRecordProgress, Append: "note", Marker: "worklease-op:" + id}, Status: "unknown", DispatchedAt: time.Unix(100, 0), LastReadback: "unknown"}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.WriteOwnerPrivate(filepath.Join(journal.Dir, id+".json"), data, 1<<20); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errs.Reset()
	if err := Run(context.Background(), []string{"worklease", "--json", "queue", "recovery"}, "test", "unknown", "unknown", &out, &errs); err != nil {
		t.Fatalf("populated recovery: %v %s", err, errs.String())
	}
	for _, field := range []string{id, `"claimId":"claim-1"`, `"lastReadback":"unknown"`, `"action":"record-progress"`, `"allowedNextSteps"`, `"dispatchedAt"`, `"resources"`, `"effect"`} {
		if !strings.Contains(out.String(), field) {
			t.Errorf("JSON recovery missing %s: %s", field, out.String())
		}
	}
}

func TestQueueRecoveryReadbackFailureKeepsHeldClaimVisible(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	_, path, backend := claimedLifecycle(t)
	h, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := queueRecoveryJournal()
	if err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("a", 32)
	record := queue.WriteRecord{Intent: queue.WriteIntent{OperationID: id, Source: queue.Source{ID: "tasks", Adapter: "backlog-md", Locator: filepath.Join(t.TempDir(), "absent")}, Ref: queue.Ref{SourceID: "tasks", ItemID: "TASK-1"}, ClaimID: h.ClaimID, AuthorityID: backend.AuthorityID(), Resources: h.Resources}, Status: "receipt", Receipt: &queue.ProviderReceipt{SourceID: "tasks", ItemID: "TASK-1"}, CreatedAt: time.Now()}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.EnsureOwnerPrivateDir(journal.Dir); err != nil {
		t.Fatal(err)
	}
	if err := handle.WriteOwnerPrivate(filepath.Join(journal.Dir, id+".json"), data, 1<<20); err != nil {
		t.Fatal(err)
	}
	for _, jsonOutput := range []bool{false, true} {
		args := []string{"worklease", "--local"}
		if jsonOutput {
			args = append(args, "--json")
		}
		args = append(args, "queue", "recovery", "retry", "--operation-id", id, "--handle", path)
		var out, errs bytes.Buffer
		runErr := Run(context.Background(), args, "test", "unknown", "unknown", &out, &errs)
		if runErr == nil {
			t.Fatal("failed read-back reported success")
		}
		output := out.String() + errs.String() + runErr.Error()
		if !strings.Contains(output, "claim held true") && !strings.Contains(output, `"claimHeld":true`) {
			t.Fatalf("failed read-back hid held claim: %s", output)
		}
		if !strings.Contains(output, "unknown") {
			t.Fatalf("failed read-back hid outcome: %s", output)
		}
		if jsonOutput && !strings.Contains(out.String(), `"commitState":"unknown"`) {
			t.Fatalf("failed read-back claimed no commit: %s", out.String())
		}
	}
}

func TestQueueRecoveryRetryCheckpointWithoutConfiguredSource(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	_, path, backend := claimedLifecycle(t)
	h, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	// The provider effect was verified and the checkpoint committed, but the
	// response was lost and the source has since left queue.yaml.
	intent := queue.WriteIntent{OperationID: randomHex(16), OperationRef: randomHex(16), Source: queue.Source{ID: "removed", Adapter: "backlog-md", Locator: filepath.Join(t.TempDir(), "absent")}, Ref: queue.Ref{SourceID: "removed", ItemID: "TASK-1"}, Principal: "alice", AuthorityID: backend.AuthorityID(), ClaimID: h.ClaimID, ClaimRevision: h.Revision, Resources: h.Resources, CheckpointTTL: 30 * time.Second, CheckpointNotAfter: time.Now().Add(10 * time.Minute)}
	receipt := queue.ProviderReceipt{SourceID: "removed", ItemID: "TASK-1", ID: "provider-receipt", Actor: "alice"}
	if err := (queueWriteClaim{backend: backend, path: path}).Checkpoint(context.Background(), intent, receipt); err != nil {
		t.Fatal(err)
	}
	journal, err := queueRecoveryJournal()
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.EnsureOwnerPrivateDir(journal.Dir); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(queue.WriteRecord{Intent: intent, Receipt: &receipt, Status: "checkpoint-pending", LastReadback: string(queue.WriteVerified), CreatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.WriteOwnerPrivate(filepath.Join(journal.Dir, intent.OperationID+".json"), data, 1<<20); err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	if err := Run(context.Background(), []string{"worklease", "--local", "--json", "queue", "recovery", "retry", "--operation-id", intent.OperationID, "--handle", path}, "test", "unknown", "unknown", &out, &errs); err != nil {
		t.Fatalf("checkpoint recovery required the removed source: %v %s %s", err, out.String(), errs.String())
	}
	if !strings.Contains(out.String(), `"outcome":"verified"`) {
		t.Fatalf("committed checkpoint not verified: %s", out.String())
	}
}

func TestQueueRecoveryRetryMissingOperation(t *testing.T) {
	_, env := testkit.Home(t)
	t.Setenv("XDG_STATE_HOME", env["XDG_STATE_HOME"])
	t.Setenv("XDG_CACHE_HOME", env["XDG_CACHE_HOME"])
	var out, errs bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "--json", "queue", "recovery", "retry", "--operation-id", strings.Repeat("a", 32), "--handle", filepath.Join(t.TempDir(), "handle")}, "test", "unknown", "unknown", &out, &errs)
	if err == nil || !JSONErrorHandled(err) || errs.Len() != 0 {
		t.Fatalf("missing operation: %v, stdout=%q stderr=%q", err, out.String(), errs.String())
	}
	var result struct {
		Error struct {
			Reason  string `json:"reason"`
			Details struct {
				CommitState string `json:"commitState"`
			} `json:"details"`
		} `json:"error"`
	}
	if decodeErr := json.Unmarshal(out.Bytes(), &result); decodeErr != nil || result.Error.Reason != "operation-not-found" || result.Error.Details.CommitState != "unknown" {
		t.Fatalf("missing operation envelope: %v %s", decodeErr, out.String())
	}
}

func TestQueueRecoveryRetryRequiresOriginalHandle(t *testing.T) {
	t.Parallel()
	var out, errs bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "queue", "recovery", "retry", "--operation-id", strings.Repeat("a", 32)}, "test", "unknown", "unknown", &out, &errs)
	if err == nil || !strings.Contains(err.Error(), "original --handle") {
		t.Fatalf("retry without handle accepted: %v %s", err, out.String())
	}
}

func TestQueueRecoveryReconciliationRequiresEvidence(t *testing.T) {
	t.Parallel()
	var out, errs bytes.Buffer
	err := Run(context.Background(), []string{"worklease", "queue", "recovery", "reconcile", "--operation-id", strings.Repeat("a", 32), "--evidence", "checked provider"}, "test", "unknown", "unknown", &out, &errs)
	if err == nil || !strings.Contains(err.Error(), "--no-commit") {
		t.Fatalf("unattested reconciliation accepted: %v %s", err, out.String())
	}
}

func TestCachedSnapshotOverlaysHeldClaimWithoutRefresh(t *testing.T) {
	ref := queue.Ref{SourceID: "s", ItemID: "1"}
	cached := queue.Snapshot{Items: map[string]queue.Item{ref.Key(): {Summary: queue.Summary{Ref: ref}}}}
	sources := map[string]queue.ClaimSource{"s": {Source: queue.Source{ID: "s", Adapter: "generic", Locator: "project"}}}
	var stored sync.Map
	authority := func() (queue.ClaimAuthority, uint64) {
		return queue.ClaimAuthority{API: cachedClaimStatus{}, ID: "authority"}, 0
	}
	overlayCachedClaims(context.Background(), &cached, sources, authority, config.ProfilePaths{}, &stored)
	item := cached.Items[ref.Key()]
	if !item.Claim.Active || item.Claim.AgentID != "worker" {
		t.Fatalf("cached claim was not observed: %+v", item.Claim)
	}
	if applied, ok := stored.Load(ref.Key()); !ok || !applied.(queue.Item).Claim.Active {
		t.Fatalf("cached claim was not retained for hydration: %v %v", applied, ok)
	}
}

func TestOverlayRecomputesWhenAdmissionChangesMidOverlay(t *testing.T) {
	ref := queue.Ref{SourceID: "s", ItemID: "1"}
	items := []queue.Item{{Summary: queue.Summary{Ref: ref}}}
	sources := map[string]queue.ClaimSource{"s": {Source: queue.Source{ID: "s", Adapter: "generic", Locator: "project"}}}
	calls := 0
	authority := func() (queue.ClaimAuthority, uint64) {
		calls++
		// Metadata lands after the first overlay starts: version 0, then 1.
		if calls == 1 {
			return queue.ClaimAuthority{API: cachedClaimStatus{}, ID: "authority"}, 0
		}
		return queue.ClaimAuthority{API: cachedClaimStatus{}, ID: "authority"}, 1
	}
	observed := overlayCurrentClaims(context.Background(), items, sources, authority, config.ProfilePaths{})
	if calls != 4 || len(observed) != 1 || !observed[0].Claim.Active {
		t.Fatalf("overlay was not recomputed under the current authority: calls=%d %+v", calls, observed)
	}
}

func TestQueueFirstFrameUsesIndexBeforeProviderRefresh(t *testing.T) {
	ctx := context.Background()
	checkout := t.TempDir()
	if output, err := testkit.GitCommand("init", "-b", "main", checkout).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if output, err := testkit.GitCommand("-C", checkout, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
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

func TestQueueFirstFrameDoesNotWaitForRemoteMetadata(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("WORKLEASE_HOME", t.TempDir())
	metadataRequests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/worklease" {
			metadataRequests <- struct{}{}
			<-r.Context().Done()
		}
	}))
	defer server.Close()
	paths := config.UserProfilePaths(os.Getenv)
	profile := config.Profile{Name: "remote", Endpoint: server.URL, AuthorityID: strings.Repeat("a", 32), AllowInsecureHTTP: true, Credential: config.CredentialDescriptor{Path: filepath.Join(filepath.Dir(paths.Profiles), "credentials", "remote")}}
	if err := config.SaveProfiles(paths, []config.Profile{profile}, ""); err != nil {
		t.Fatal(err)
	}
	// Keep the metadata request cancellable without expiring fixture setup
	// (Git init, commit and SQLite open) under a busy test runner.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	backend, authorityView, err := queueAuthorityForViewWithMetadata(ctx, &urfave.Command{}, "remote", false)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("remote authority metadata blocked cached startup for %s", elapsed)
	}
	if !authorityView.Remote || authorityView.AdmittedPrefixes != nil {
		t.Fatalf("unexpected unresolved remote metadata: %+v", authorityView)
	}

	checkout := t.TempDir()
	if output, err := testkit.GitCommand("init", "-b", "main", checkout).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if output, err := testkit.GitCommand("-C", checkout, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
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
	metadataDone := make(chan error, 1)
	go func() {
		_, metadataErr := backend.HTTP.Metadata(ctx)
		metadataDone <- metadataErr
	}()
	select {
	case <-metadataRequests:
	case <-time.After(time.Second):
		t.Fatal("remote metadata request did not reach the stalled authority")
	}
	model := queueui.New(loader.Store.Current())
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	if rendered := updated.(queueui.Model).View(); !strings.Contains(rendered, "Cached firs") {
		t.Fatalf("cached first frame blocked by remote metadata: %s", rendered)
	}
	select {
	case err := <-metadataDone:
		t.Fatalf("metadata response unexpectedly completed before the cached frame: %v", err)
	default:
	}
	cancel()
	select {
	case <-metadataDone:
	case <-time.After(time.Second):
		t.Fatal("stalled metadata request did not cancel")
	}
}

func TestBeadsSelectedDetailCanHydrate(t *testing.T) {
	t.Parallel()
	if !queueSelectedHydrationEnabled("beads") || queueSelectedHydrationEnabled("unknown") {
		t.Fatal("selected Beads detail was rejected before HydrateDetail")
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

func TestQueueAliasAndViewShortFlag(t *testing.T) {
	h := newQueueQueryHarness(t)
	h.setTasks("[]")

	data, err := h.run("q", "-v", "Ready", "query", "--json")
	if err != nil || !strings.Contains(string(data), `"view":"Ready"`) {
		t.Fatalf("q -v Ready query: %v (%s)", err, data)
	}

	data, err = h.run("q", "-v", "Missing")
	if err == nil || !strings.Contains(err.Error(), "unknown queue view: Missing") {
		t.Fatalf("q -v Missing: %v (%s)", err, data)
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
