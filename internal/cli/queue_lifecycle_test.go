package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/config"
	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/lease"
	"github.com/brettinternet/worklease/internal/ledger"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueindex"
	"github.com/brettinternet/worklease/internal/queueui"
	"github.com/brettinternet/worklease/internal/reason"
)

func claimedLifecycle(t *testing.T) (queueLifecycle, string, *authorityContext) {
	t.Helper()
	item := queueClaimItem("tasks", "1")
	controller, backend, adapter := newLocalQueueClaimController(t, 30*time.Second, item)
	preview := controller.Preview(context.Background(), item)().(queueui.ClaimPreviewMsg)
	if preview.Err != nil {
		t.Fatal(preview.Err)
	}
	acquired := controller.AcquireClaim(context.Background(), item, *preview.Preview)().(queueui.ClaimResultMsg)
	if acquired.Err != nil {
		t.Fatal(acquired.Err)
	}
	path, err := queueClaimHandlePath(backend.Config.Home, controller.queueSession, config.LocalProfileName, adapter.source, item.Ref)
	if err != nil {
		t.Fatal(err)
	}
	return queueLifecycle{controller: controller, now: time.Now}, path, backend
}

func TestQueueLifecycleRenewalFakeClockAndReopen(t *testing.T) {
	lifecycle, path, _ := claimedLifecycle(t)
	ctx := context.Background()
	first := lifecycle.inspect(ctx, path, false)
	if !first.Verified || first.NextRenewal.IsZero() || !first.NextRenewal.Before(first.ExpiresAt.Add(-15*time.Second)) {
		t.Fatalf("renewal must precede half TTL: %+v", first)
	}
	// Reopen through the same private handle: username or overlay occupancy is insufficient.
	reopened := lifecycle.inspect(ctx, path, false)
	if !reopened.Verified || reopened.ClaimID != first.ClaimID {
		t.Fatalf("handle reattach failed: %+v", reopened)
	}
	lifecycle.now = func() time.Time { return first.NextRenewal.Add(time.Millisecond) }
	renewed := lifecycle.inspect(ctx, path, true)
	if !renewed.Verified || !strings.HasPrefix(renewed.LastResult, "renewed at") || !renewed.ExpiresAt.After(first.ExpiresAt) {
		t.Fatalf("renewal: %+v", renewed)
	}
	h, err := handle.Read(path)
	if err != nil || h.Revision < 2 || h.State != "ready" {
		t.Fatalf("persisted renewal: %+v %v", h, err)
	}
}

func TestQueueLifecycleSuspendLostAndNoFallback(t *testing.T) {
	lifecycle, path, backend := claimedLifecycle(t)
	first := lifecycle.inspect(context.Background(), path, false)
	lifecycle.now = func() time.Time { return first.ExpiresAt.Add(time.Second) }
	if late := lifecycle.inspect(context.Background(), path, true); strings.HasPrefix(late.LastResult, "renewed") {
		t.Fatalf("late wake renewed: %+v", late)
	}
	// An unavailable selected authority cannot fall back to a second local authority.
	original := lifecycle.controller.current
	lifecycle.controller.current = func() (queue.ClaimAuthority, uint64) { return queue.ClaimAuthority{}, 2 }
	unavailable := lifecycle.inspect(context.Background(), path, true)
	if unavailable.Verified || !strings.Contains(unavailable.LastResult, "authority unavailable") {
		t.Fatalf("authority outage: %+v", unavailable)
	}
	lifecycle.controller.current = original
	h, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = backend.API.Release(context.Background(), lifecycle.credentials(path, h), lease.ReleaseRequest{OperationID: randomHex(16), Reason: "external release", RequestNotAfter: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if lost := lifecycle.inspect(context.Background(), path, false); !lost.Lost || lost.Verified {
		t.Fatalf("lost claim remained actionable: %+v", lost)
	}
}

func TestQueueLifecycleCancelOnlyNoEffect(t *testing.T) {
	t.Run("no effect", func(t *testing.T) {
		lifecycle, path, _ := claimedLifecycle(t)
		if err := lifecycle.Cancel(context.Background(), path); err != nil {
			t.Fatal(err)
		}
		if _, err := handle.Read(path); err == nil {
			t.Fatal("released handle retained")
		}
		if err := lifecycle.Cancel(context.Background(), path); err == nil {
			t.Fatal("second cancellation succeeded")
		}
	})
	t.Run("journaled provider intent", func(t *testing.T) {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		lifecycle, path, _ := claimedLifecycle(t)
		h, err := handle.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		journal := queue.WriteJournal{Dir: config.QueueRecoveryDir(os.Getenv)}
		if err := handle.EnsureOwnerPrivateDir(journal.Dir); err != nil {
			t.Fatal(err)
		}
		id := randomHex(16)
		record, err := json.Marshal(queue.WriteRecord{Intent: queue.WriteIntent{OperationID: id, ClaimID: h.ClaimID}, Status: "pending", CreatedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		if err := handle.WriteOwnerPrivateNoReplace(filepath.Join(journal.Dir, id+".json"), record, 1<<20); err != nil {
			t.Fatal(err)
		}
		if err := lifecycle.Cancel(context.Background(), path); err == nil || !strings.Contains(err.Error(), "provider write intent") {
			t.Fatalf("journaled write allowed cancellation: %v", err)
		}
	})
	t.Run("guarded operation", func(t *testing.T) {
		lifecycle, path, backend := claimedLifecycle(t)
		h, err := handle.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = backend.API.BeginOperation(context.Background(), lifecycle.credentials(path, h), lease.OperationIntent{OperationID: randomHex(16), Kind: "exec", Request: map[string]any{"argv": []string{"true"}}, TTL: time.Minute, RequestNotAfter: time.Now().Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if err = lifecycle.Cancel(context.Background(), path); err == nil {
			t.Fatal("operation started but cancellation allowed")
		}
		if _, err = handle.Read(path); err != nil {
			t.Fatalf("recovery handle lost: %v", err)
		}
	})
	t.Run("pending", func(t *testing.T) {
		lifecycle, path, _ := claimedLifecycle(t)
		lock, err := handle.AcquireLock(context.Background(), path+".lock")
		if err != nil {
			t.Fatal(err)
		}
		h, err := lock.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(time.Hour)
		inputs := map[string]any{"kind": "heartbeat", "authorityId": h.AuthorityID, "claimId": h.ClaimID, "ttl": int64(time.Minute / time.Microsecond), "requestNotAfter": deadline.UTC().UnixMicro()}
		if err = beginHandleMutation(path, &h, "heartbeat", randomHex(16), deadline, inputs, lock); err != nil {
			t.Fatal(err)
		}
		lock.Close()
		if err = lifecycle.Cancel(context.Background(), path); reason.As(err) == nil {
			t.Fatalf("pending request was cancelled: %v", err)
		}
	})
}

type pausedHistoryAuthority struct {
	commandAuthority
	entered         chan struct{}
	continueHistory chan struct{}
}

func (a *pausedHistoryAuthority) History(ctx context.Context, resource, cursor string, limit int, full bool) (ledger.HistoryPage, error) {
	close(a.entered)
	select {
	case <-a.continueHistory:
	case <-ctx.Done():
		return ledger.HistoryPage{}, ctx.Err()
	}
	return a.commandAuthority.History(ctx, resource, cursor, limit, full)
}

type cancellationWriteAdapter struct{ writes int }

func (*cancellationWriteAdapter) Inspect(context.Context, queue.WriteIntent) (queue.WritePreflight, error) {
	return queue.WritePreflight{Capability: true, Authorized: true, InScope: true, Fresh: true, NativeAvailable: true, Ready: true, Precondition: "original"}, nil
}
func (*cancellationWriteAdapter) ValidateTransition(context.Context, queue.Source, queue.Action, string) error {
	return nil
}
func (a *cancellationWriteAdapter) Write(_ context.Context, intent queue.WriteIntent) (queue.ProviderReceipt, error) {
	a.writes++
	return queue.ProviderReceipt{SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID}, nil
}
func (*cancellationWriteAdapter) ReadReceipt(context.Context, queue.WriteIntent, *queue.ProviderReceipt) (queue.ReceiptObservation, error) {
	return queue.ReceiptObservation{Outcome: queue.WriteUnknown}, nil
}

type cancellationWriteClaim struct {
	backend     *authorityContext
	credentials lease.Credentials
	resources   []string
}

func (c cancellationWriteClaim) Verify(ctx context.Context, intent queue.WriteIntent) error {
	v, err := c.backend.API.Verify(ctx, c.credentials, c.resources)
	if err != nil {
		return err
	}
	if !v.Claim.Active || v.Claim.ClaimID != intent.ClaimID {
		return fmt.Errorf("claim released")
	}
	return nil
}
func (c cancellationWriteClaim) Checkpoint(context.Context, queue.WriteIntent, queue.ProviderReceipt) error {
	return fmt.Errorf("unexpected checkpoint")
}
func (c cancellationWriteClaim) CheckpointStatus(context.Context, queue.WriteIntent, queue.ProviderReceipt) (queue.WriteVerification, error) {
	return queue.WriteUnknown, nil
}

func TestQueueLifecycleCancelSerializesJournalAdmission(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	lifecycle, path, backend := claimedLifecycle(t)
	h, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	cacheDir, err := queueindex.CacheDir(os.Getenv, os.Getenv("HOME"))
	if err != nil {
		t.Fatal(err)
	}
	journal, err := queue.NewWriteJournal(config.QueueRecoveryDir(os.Getenv), cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	pause := &pausedHistoryAuthority{commandAuthority: backend.API, entered: make(chan struct{}), continueHistory: make(chan struct{})}
	backend.API = pause
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cancelDone := make(chan error, 1)
	go func() { cancelDone <- lifecycle.Cancel(ctx, path) }()
	select {
	case <-pause.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	adapter := &cancellationWriteAdapter{}
	intent := queue.WriteIntent{OperationID: randomHex(16), Source: queue.Source{ID: "tasks"}, Ref: queue.Ref{SourceID: "tasks", ItemID: "1"}, Principal: "alice", Patch: map[string]string{"status": "Doing"}, Precondition: "original", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, ClaimRevision: h.Revision, Resources: h.Resources, CheckpointTTL: time.Minute, Action: queue.ActionStart, Transition: "Doing"}
	pipeline := queue.WritePipeline{Adapter: adapter, Claim: cancellationWriteClaim{backend: backend, credentials: lifecycle.credentials(path, h), resources: h.Resources}, Journal: journal, Workflow: map[string]string{"start": "Doing"}}
	writeDone := make(chan error, 1)
	go func() { _, writeErr := pipeline.Start(ctx, intent); writeDone <- writeErr }()
	close(pause.continueHistory)
	select {
	case err := <-cancelDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-writeDone:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if adapter.writes != 0 {
		t.Fatalf("write dispatched during cancellation: %d", adapter.writes)
	}
}

func TestQueueLifecycleRemoteRenewAndCancel(t *testing.T) {
	item := queueClaimItem("tasks", "remote")
	controller, _, adapter := newRemoteQueueClaimController(t, 30*time.Second, item)
	preview := controller.Preview(context.Background(), item)().(queueui.ClaimPreviewMsg)
	if preview.Err != nil {
		t.Fatal(preview.Err)
	}
	result := controller.AcquireClaim(context.Background(), item, *preview.Preview)().(queueui.ClaimResultMsg)
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	path, err := queueClaimHandlePath(controller.home, controller.queueSession, controller.profileName, adapter.source, item.Ref)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := queueLifecycle{controller: controller, now: time.Now}
	first := lifecycle.inspect(context.Background(), path, false)
	if !first.Verified {
		t.Fatalf("remote handle verification: %+v", first)
	}
	selected, _ := controller.current()
	selected.Now = func() (time.Time, error) { return first.NextRenewal.Add(time.Millisecond), nil }
	controller.current = func() (queue.ClaimAuthority, uint64) { return selected, 1 }
	renewed := lifecycle.inspect(context.Background(), path, true)
	if !renewed.Verified || !strings.HasPrefix(renewed.LastResult, "renewed at") {
		t.Fatalf("remote renewal: %+v", renewed)
	}
	reopened := lifecycle.inspect(context.Background(), path, false)
	if !reopened.Verified || !reopened.ExpiresAt.Equal(renewed.ExpiresAt) {
		t.Fatalf("remote persisted expiry/revision: %+v after %+v", reopened, renewed)
	}
	if err = lifecycle.Cancel(context.Background(), path); err != nil {
		t.Fatalf("remote no-effect cancellation: %v", err)
	}
}

type slowQueueAuthority struct {
	commandAuthority
	slow map[string]bool
}

func (a slowQueueAuthority) Verify(ctx context.Context, c lease.Credentials, resources []string) (lease.Verification, error) {
	if a.slow[c.ClaimID] {
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return lease.Verification{}, ctx.Err()
		}
	}
	return a.commandAuthority.Verify(ctx, c, resources)
}

// Opt-in measured control-path companion to TASK-129.6's PTY, rate-limit,
// stalled-adapter and index fixture. It uses the actual queue-owned renewal
// runner while a 50k-edge graph and 10k-row SQLite replacement are in flight.
func TestQueueLifecycleCombinedLoadMargin(t *testing.T) {
	if os.Getenv("QUEUE_LIFECYCLE_COMBINED") != "1" {
		t.Skip("set QUEUE_LIFECYCLE_COMBINED=1")
	}
	lifecycle, path, _ := claimedLifecycle(t)
	first := lifecycle.inspect(context.Background(), path, false)
	if !first.Verified {
		t.Fatal(first.LastResult)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	messages := make(chan queueui.OwnedClaimMsg, 32)
	done := make(chan struct{})
	go func() {
		lifecycle.run(ctx, func(msg queueui.OwnedClaimMsg) {
			select {
			case messages <- msg:
			case <-ctx.Done():
			}
		})
		close(done)
	}()
	index, err := queueindex.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	partition := queueindex.Partition{Source: "stress", Principal: "tester", Scope: "local", Generation: "1"}
	indexed := make([]queue.Item, 10000)
	for i := range indexed {
		indexed[i] = queue.Item{Summary: queue.Summary{Ref: queue.Ref{SourceID: "stress", ItemID: fmt.Sprintf("%06d", i)}, Fresh: true, Terminal: true}}
	}
	graph := make(map[string]queue.Item, 50000)
	var prior queue.Ref
	for i := 0; i < 50000; i++ {
		ref := queue.Ref{SourceID: "graph", ItemID: fmt.Sprintf("%06d", i)}
		item := queue.Item{Summary: queue.Summary{Ref: ref, Fresh: true, Terminal: true}, DependenciesKnown: true, TerminalKnown: true, Closure: queue.CoverageComplete}
		if i > 0 {
			item.Dependencies = []queue.Ref{prior}
		}
		graph[ref.Key()] = item
		prior = ref
	}
	finished := make(chan error, 2)
	go func() { finished <- index.Replace(ctx, partition, indexed, true) }()
	go func() { queue.Recompute(graph, queue.CoverageComplete); finished <- nil }()
	var margin time.Duration
	deadline := time.After(28 * time.Second)
	for margin == 0 {
		select {
		case msg := <-messages:
			if msg.Path == path && strings.HasPrefix(msg.LastResult, "renewed at") {
				margin = time.Until(first.ExpiresAt)
				if !msg.Verified || margin < renewalMargin(30*time.Second) {
					t.Fatalf("combined load missed renewal margin %s: %+v", margin, msg)
				}
			}
		case <-deadline:
			t.Fatal("renewal missed under combined load")
		}
	}
	for range 2 {
		select {
		case err := <-finished:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("combined workload did not finish")
		}
	}
	cancel()
	<-done
	t.Logf("queue-owned renewal previous-lease margin=%s (target >=%s) with 50k graph and 10k indexed rows", margin, renewalMargin(30*time.Second))
}

func TestQueueLifecycleIndependentOfStalledClaims(t *testing.T) {
	items := make([]queue.Item, 6)
	for i := range items {
		items[i] = queueClaimItem("tasks", fmt.Sprint(i))
	}
	controller, backend, adapter := newLocalQueueClaimController(t, 30*time.Second, items...)
	slow := map[string]bool{}
	var lastPath string
	for i, item := range items {
		preview := controller.Preview(context.Background(), item)().(queueui.ClaimPreviewMsg)
		if preview.Err != nil {
			t.Fatal(preview.Err)
		}
		acquired := controller.AcquireClaim(context.Background(), item, *preview.Preview)().(queueui.ClaimResultMsg)
		if acquired.Err != nil {
			t.Fatal(acquired.Err)
		}
		path, err := queueClaimHandlePath(controller.home, controller.queueSession, controller.profileName, adapter.source, item.Ref)
		if err != nil {
			t.Fatal(err)
		}
		h, err := handle.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		if i < 5 {
			slow[h.ClaimID] = true
		} else {
			lastPath = path
		}
	}
	realAPI := backend.API
	backend.API = slowQueueAuthority{commandAuthority: realAPI, slow: slow}
	lifecycle := queueLifecycle{controller: controller, now: time.Now}
	sixth := lifecycle.inspect(context.Background(), lastPath, false)
	lifecycle.now = func() time.Time { return sixth.NextRenewal.Add(time.Millisecond) }
	ctx, cancel := context.WithCancel(context.Background())
	messages := make(chan queueui.OwnedClaimMsg, 20)
	done := make(chan struct{})
	go func() {
		lifecycle.run(ctx, func(msg queueui.OwnedClaimMsg) {
			select {
			case messages <- msg:
			case <-ctx.Done():
			}
		})
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case msg := <-messages:
			if msg.Path == lastPath {
				cancel()
				<-done
				if !msg.Verified || !strings.HasPrefix(msg.LastResult, "renewed at") {
					t.Fatalf("unblocked claim not renewed: %+v", msg)
				}
				return
			}
		case <-deadline:
			cancel()
			<-done
			t.Fatal("five stalled verifications delayed sixth renewal")
		}
	}
}

func TestQueueLifecycleRemoteUsesAuthorityClockAcrossClientSkew(t *testing.T) {
	for _, offset := range []time.Duration{-2 * time.Minute, 2 * time.Minute} {
		t.Run(offset.String(), func(t *testing.T) {
			item := queueClaimItem("tasks", "offset")
			controller, _, adapter := newRemoteQueueClaimController(t, 30*time.Second, item)
			preview := controller.Preview(context.Background(), item)().(queueui.ClaimPreviewMsg)
			if preview.Err != nil {
				t.Fatal(preview.Err)
			}
			acquired := controller.AcquireClaim(context.Background(), item, *preview.Preview)().(queueui.ClaimResultMsg)
			if acquired.Err != nil {
				t.Fatal(acquired.Err)
			}
			path, err := queueClaimHandlePath(controller.home, controller.queueSession, controller.profileName, adapter.source, item.Ref)
			if err != nil {
				t.Fatal(err)
			}
			lifecycle := queueLifecycle{controller: controller, now: func() time.Time { return time.Now().Add(offset) }}
			first := lifecycle.inspect(context.Background(), path, false)
			if !first.Verified {
				t.Fatalf("initial verify: %+v", first)
			}
			selected, _ := controller.current()
			selected.Now = func() (time.Time, error) { return first.NextRenewal.Add(time.Millisecond), nil }
			controller.current = func() (queue.ClaimAuthority, uint64) { return selected, 1 }
			renewed := lifecycle.inspect(context.Background(), path, true)
			if !renewed.Verified || !strings.HasPrefix(renewed.LastResult, "renewed at") {
				t.Fatalf("client skew %s blocked authority-time renewal: %+v", offset, renewed)
			}
		})
	}
}

func TestQueueLifecycleLocalHoldCeiling(t *testing.T) {
	lifecycle, path, _ := claimedLifecycle(t)
	h, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if h.HoldUntil.IsZero() || time.Until(h.HoldUntil) < 59*time.Minute {
		t.Fatalf("previewed hold not retained: %+v", h)
	}
	// Bring the admitted ceiling into the next renewal window: the authority
	// must cap renewal at it, rather than granting the full TTL past the hold.
	h.HoldUntil = time.Now().Add(20 * time.Second)
	lock, err := handle.AcquireLock(context.Background(), path+".lock")
	if err != nil {
		t.Fatal(err)
	}
	if err = lock.Write(path, h); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	first := lifecycle.inspect(context.Background(), path, false)
	lifecycle.now = func() time.Time { return first.NextRenewal.Add(time.Millisecond) }
	renewed := lifecycle.inspect(context.Background(), path, true)
	if !renewed.Verified || renewed.ExpiresAt.After(h.HoldUntil) {
		t.Fatalf("renewal exceeded persisted hold ceiling: %+v / %s", renewed, h.HoldUntil)
	}
}

func TestQueueLifecycleDoesNotRemoveSuccessorHandle(t *testing.T) {
	_, path, _ := claimedLifecycle(t)
	original, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	successor := original
	successor.ClaimID = randomHex(16)
	successor.Token = randomHex(32)
	successor.Revision = 1
	lock, err := handle.AcquireLock(context.Background(), path+".lock")
	if err != nil {
		t.Fatal(err)
	}
	if err = lock.ReplaceReady(path, original, successor); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	if err = removeReleasedQueueHandle(context.Background(), path, original.ClaimID, original.Revision+1); err != nil {
		t.Fatal(err)
	}
	retained, err := handle.Read(path)
	if err != nil || retained.ClaimID != successor.ClaimID {
		t.Fatalf("successor credential lost: %+v %v", retained, err)
	}
}

func TestQueueLifecycleRetainsExitWarningOnDirectoryError(t *testing.T) {
	lifecycle, path, _ := claimedLifecycle(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	messages := make(chan queueui.OwnedClaimMsg, 20)
	done := make(chan struct{})
	go func() {
		lifecycle.run(ctx, func(msg queueui.OwnedClaimMsg) {
			select {
			case messages <- msg:
			case <-ctx.Done():
			}
		})
		close(done)
	}()
	select {
	case first := <-messages:
		if first.Path != path || !first.Verified {
			t.Fatalf("initial ownership: %+v", first)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("initial inspection timed out")
	}
	if err := os.Chmod(lifecycle.directory(), 0755); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(lifecycle.directory(), 0700)
	select {
	case failed := <-messages:
		if failed.Path != path || failed.Gone || failed.Verified || !strings.Contains(failed.LastResult, "unavailable") {
			t.Fatalf("scan failure discarded claim: %+v", failed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("scan error not published")
	}
	cancel()
	<-done
}

func TestQueueLifecyclePrivateDirectory(t *testing.T) {
	lifecycle, path, _ := claimedLifecycle(t)
	paths, err := lifecycle.paths()
	if err != nil || len(paths) != 1 || paths[0] != path || filepath.Dir(path) != lifecycle.directory() {
		t.Fatalf("private handle discovery: %v %v", paths, err)
	}
}
