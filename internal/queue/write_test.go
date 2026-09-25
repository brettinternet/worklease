package queue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/testkit"
)

type writeFixture struct {
	calls            int
	checks           int
	checkpointCalls  int
	checkpointEffect bool
	checkpointRef    string
	commitThenLose   bool
	staleAfterCommit bool
	pre              WritePreflight
	observation      ReceiptObservation
	writeErr         error
	readErr          error
	checkpointErr    error
	verifyErr        error
	transitionErr    error
	writeStarted     chan struct{}
	writeResume      chan struct{}
}

func (f *writeFixture) Inspect(context.Context, WriteIntent) (WritePreflight, error) {
	return f.pre, nil
}
func (f *writeFixture) ValidateTransition(context.Context, Source, Action, string) error {
	return f.transitionErr
}
func (f *writeFixture) Write(_ context.Context, intent WriteIntent) (ProviderReceipt, error) {
	f.calls++
	if f.writeStarted != nil {
		close(f.writeStarted)
		<-f.writeResume
	}
	return ProviderReceipt{SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID, ID: "receipt-1", Actor: intent.Principal}, f.writeErr
}
func (f *writeFixture) ReadReceipt(context.Context, WriteIntent, *ProviderReceipt) (ReceiptObservation, error) {
	f.checks++
	return f.observation, f.readErr
}
func (f *writeFixture) Verify(context.Context, WriteIntent) error {
	if f.checkpointEffect && f.staleAfterCommit {
		return errors.New("revision changed")
	}
	return f.verifyErr
}
func (f *writeFixture) CheckpointStatus(_ context.Context, intent WriteIntent, _ ProviderReceipt) (WriteVerification, error) {
	if f.checkpointEffect && (f.checkpointRef == "" || f.checkpointRef == intent.OperationRef) {
		return WriteVerified, nil
	}
	return WriteUnknown, nil
}
func (f *writeFixture) Checkpoint(_ context.Context, intent WriteIntent, _ ProviderReceipt) error {
	f.checkpointCalls++
	if f.checkpointErr == nil || f.commitThenLose {
		f.checkpointEffect = true
		f.checkpointRef = intent.OperationRef
	}
	if f.commitThenLose {
		return errors.New("checkpoint committed but response lost")
	}
	return f.checkpointErr
}

func writeSetup(t *testing.T) (WritePipeline, *writeFixture, WriteIntent) {
	t.Helper()
	_, env := testkit.Home(t)
	journal, err := NewWriteJournal(filepath.Join(env["XDG_STATE_HOME"], "worklease", "queue-recovery"), filepath.Join(env["HOME"], ".cache", "worklease", "queue"))
	if err != nil {
		t.Fatal(err)
	}
	intent := WriteIntent{OperationID: strings.Repeat("a", 32), Source: Source{ID: "local", Adapter: "fake"}, Ref: Ref{SourceID: "local", ItemID: "item"}, Principal: "alice", Patch: map[string]string{"status": "Doing"}, Precondition: "version-1", AuthorityID: "authority", ClaimID: "claim-1", ClaimRevision: 1, Resources: []string{"resource:local:item"}, OperationRef: strings.Repeat("c", 32), CheckpointTTL: time.Minute, CheckpointNotAfter: time.Now().Add(time.Hour), Action: ActionStart, Transition: "Doing"}
	f := &writeFixture{pre: WritePreflight{Capability: true, Authorized: true, InScope: true, Fresh: true, NativeAvailable: true, Ready: true, Precondition: "version-1"}, observation: ReceiptObservation{Outcome: WriteVerified, SourceID: "local", ItemID: "item", Patch: map[string]string{"status": "Doing"}, Precondition: "version-1", ReceiptID: "receipt-1", Actor: "alice"}}
	return WritePipeline{Adapter: f, Claim: f, Journal: journal, Workflow: map[string]string{"start": "Doing"}}, f, intent
}

func TestWritePipelinePreflightNeverDispatches(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*WritePipeline, *writeFixture, *WriteIntent)
	}{
		{"capability", func(_ *WritePipeline, f *writeFixture, _ *WriteIntent) { f.pre.Capability = false }},
		{"authority", func(_ *WritePipeline, f *writeFixture, _ *WriteIntent) { f.verifyErr = errors.New("expired") }},
		{"scope", func(_ *WritePipeline, f *writeFixture, _ *WriteIntent) { f.pre.InScope = false }},
		{"closure", func(_ *WritePipeline, f *writeFixture, _ *WriteIntent) { f.pre.Fresh = false }},
		{"readiness", func(_ *WritePipeline, f *writeFixture, _ *WriteIntent) { f.pre.Ready = false }},
		{"mapping", func(p *WritePipeline, _ *writeFixture, _ *WriteIntent) { p.Workflow = nil }},
		{"unsupported-provider-transition", func(_ *WritePipeline, f *writeFixture, _ *WriteIntent) {
			f.transitionErr = errors.New("unsupported transition")
		}},
		{"precondition", func(_ *WritePipeline, f *writeFixture, _ *WriteIntent) { f.pre.Precondition = "version-2" }},
		{"journal", func(p *WritePipeline, _ *writeFixture, _ *WriteIntent) { p.Journal.Dir = "relative" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, f, intent := writeSetup(t)
			tc.change(&p, f, &intent)
			result, err := p.Start(context.Background(), intent)
			if err == nil || f.calls != 0 || !result.SourceUnchanged || !result.SafeRelease {
				t.Fatalf("result=%+v err=%v dispatch=%d", result, err, f.calls)
			}
		})
	}
}

func TestWritePipelineRecoveryNeverRedispatches(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		modify func(*writeFixture)
	}{
		{"lost-response", func(f *writeFixture) { f.writeErr = errors.New("response lost") }},
		{"lagging-read", func(f *writeFixture) { f.observation.Outcome = WriteUnknown }},
		{"read-error", func(f *writeFixture) { f.readErr = errors.New("unavailable") }},
		{"checkpoint-error", func(f *writeFixture) { f.checkpointErr = errors.New("checkpoint unavailable") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, f, intent := writeSetup(t)
			tc.modify(f)
			if tc.name == "lost-response" {
				intent.Append = "progress"
				intent.Marker = "worklease-op:" + intent.OperationID
				f.observation.AppendProof = true
				f.observation.AppendContent = intent.Append
				f.observation.MarkerCount = 1
			}
			result, _ := p.Start(context.Background(), intent)
			if result.Outcome != WriteUnknown || f.calls != 1 {
				t.Fatalf("start=%+v calls=%d", result, f.calls)
			}
			if yes, err := p.Journal.CanCancel(intent.ClaimID); err != nil || yes {
				t.Fatalf("cancel allowed=%v err=%v", yes, err)
			}
			result, err := p.Start(context.Background(), intent)
			if err != nil || result.Outcome != WriteUnknown || f.calls != 1 {
				t.Fatalf("repeat=%+v err=%v calls=%d", result, err, f.calls)
			}
			intent.OperationID = strings.Repeat("b", 32)
			if intent.Append != "" {
				intent.Marker = "worklease-op:" + intent.OperationID
			}
			blocked, err := p.Start(context.Background(), intent)
			if err != nil || blocked.Outcome != WriteUnknown || f.calls != 1 {
				t.Fatalf("new ID dispatched: result=%+v err=%v calls=%d", blocked, err, f.calls)
			}
			intent.OperationID = strings.Repeat("a", 32)
			f.writeErr, f.readErr, f.checkpointErr = nil, nil, nil
			f.observation.Outcome = WriteVerified
			result, err = p.Recover(context.Background(), intent.OperationID)
			if err != nil || result.Outcome != WriteVerified || f.calls != 1 || f.checkpointCalls == 0 {
				t.Fatalf("recover=%+v err=%v calls=%d checkpoints=%d", result, err, f.calls, f.checkpointCalls)
			}
			record, err := p.Journal.Read(intent.OperationID)
			if err != nil || record.Status != "resolved" {
				t.Fatalf("journal=%+v err=%v", record, err)
			}
		})
	}
}

func TestWritePipelineCheckpointLostResponseDoesNotRepeat(t *testing.T) {
	t.Parallel()
	p, f, intent := writeSetup(t)
	f.commitThenLose = true
	f.staleAfterCommit = true
	result, err := p.Start(context.Background(), intent)
	if err == nil || result.Outcome != WriteUnknown || f.calls != 1 || f.checkpointCalls != 1 {
		t.Fatalf("lost checkpoint: %+v %v", result, err)
	}
	stored, err := p.Journal.Read(intent.OperationID)
	if err != nil || stored.Status != "checkpoint-pending" || stored.Intent.OperationRef != intent.OperationRef {
		t.Fatalf("pending checkpoint not durable: %+v %v", stored, err)
	}
	result, err = p.Recover(context.Background(), intent.OperationID)
	if err != nil || result.Outcome != WriteVerified || f.calls != 1 || f.checkpointCalls != 1 {
		t.Fatalf("checkpoint replayed or lost: %+v %v dispatch=%d checkpoint=%d", result, err, f.calls, f.checkpointCalls)
	}
}

func TestWritePipelineCrashAfterCommittedCheckpoint(t *testing.T) {
	t.Parallel()
	p, f, intent := writeSetup(t)
	f.checkpointEffect, f.staleAfterCommit = true, true
	f.readErr = errors.New("provider unavailable")
	receipt := &ProviderReceipt{SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID, ID: "receipt-1", Actor: intent.Principal}
	record := WriteRecord{Intent: intent, Receipt: receipt, Status: "checkpoint-pending", CreatedAt: time.Now()}
	if err := p.Journal.save(record, true); err != nil {
		t.Fatal(err)
	}
	result, err := p.Recover(context.Background(), intent.OperationID)
	if err != nil || result.Outcome != WriteVerified || f.calls != 0 || f.checkpointCalls != 0 {
		t.Fatalf("committed checkpoint repeated: %+v %v", result, err)
	}
}

func TestWritePipelineCrashAfterJournalOrReceipt(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"journal", "receipt"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			p, f, intent := writeSetup(t)
			intent.Append = "progress"
			intent.Marker = "worklease-op:" + intent.OperationID
			f.observation.AppendContent, f.observation.MarkerCount, f.observation.AppendProof = intent.Append, 1, true
			record := WriteRecord{Intent: intent, Status: "pending", CreatedAt: time.Now()}
			if stage == "receipt" {
				record.Status = "receipt"
				record.Receipt = &ProviderReceipt{SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID, ID: "receipt-1", Actor: intent.Principal}
			}
			if err := p.Journal.save(record, true); err != nil {
				t.Fatal(err)
			}
			if stage == "journal" {
				f.observation.Outcome = WriteUnknown
			}
			result, err := p.Recover(context.Background(), intent.OperationID)
			if err != nil || f.calls != 0 || result.Outcome != map[string]WriteVerification{"journal": WriteUnknown, "receipt": WriteVerified}[stage] {
				t.Fatalf("recover=%+v err=%v calls=%d", result, err, f.calls)
			}
		})
	}
}

func TestWritePipelineReconciliationRequiresExplicitEvidence(t *testing.T) {
	t.Parallel()
	p, f, intent := writeSetup(t)
	f.writeErr = errors.New("provider response lost")
	_, _ = p.Start(context.Background(), intent)
	if err := p.Reconcile(context.Background(), intent.OperationID, "brett", "", true, true); err == nil {
		t.Fatal("empty evidence accepted")
	}
	if err := p.Reconcile(context.Background(), intent.OperationID, "brett", "provider inspected", true, false); err == nil {
		t.Fatal("live executor accepted")
	}
	if err := p.Reconcile(context.Background(), intent.OperationID, "brett", "operator checked provider and stopped executor", true, true); err != nil {
		t.Fatal(err)
	}
	result, err := p.Recover(context.Background(), intent.OperationID)
	if err != nil || result.Outcome == WriteVerified || f.checkpointCalls != 0 || f.calls != 1 {
		t.Fatalf("reconciliation manufactured success: %+v %v", result, err)
	}
	if yes, err := p.Journal.CanCancel(intent.ClaimID); err != nil || yes {
		t.Fatalf("cancel allowed after reconciliation: %v %v", yes, err)
	}
}

func TestWriteJournalDoesNotOfferReconciliationDuringDispatch(t *testing.T) {
	t.Parallel()
	p, f, intent := writeSetup(t)
	f.writeStarted, f.writeResume = make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); _, _ = p.Start(context.Background(), intent) }()
	select {
	case <-f.writeStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("write never started")
	}
	entries, err := p.Journal.Recovery()
	if err != nil || len(entries) != 1 || entries[0].Status != "dispatching" || len(entries[0].Next) != 1 {
		t.Fatalf("reconciliation offered during dispatch: %+v %v", entries, err)
	}
	close(f.writeResume)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("write did not finish")
	}
}

func TestWriteJournalRecoveryProjection(t *testing.T) {
	t.Parallel()
	p, f, intent := writeSetup(t)
	f.writeErr = errors.New("lost response")
	if result, err := p.Start(context.Background(), intent); err == nil || result.Outcome != WriteUnknown {
		t.Fatalf("expected uncertain write: %+v %v", result, err)
	}
	entries, err := p.Journal.Recovery()
	if err != nil || len(entries) != 1 || entries[0].Dispatched == nil || entries[0].ClaimID != intent.ClaimID || len(entries[0].Next) != 2 {
		t.Fatalf("recovery entry: %+v %v", entries, err)
	}
	f.observation.Outcome = WriteUnknown
	if _, err := p.Recover(context.Background(), intent.OperationID); err != nil {
		t.Fatal(err)
	}
	entries, err = p.Journal.Recovery()
	if err != nil || len(entries) != 1 || entries[0].Readback != string(WriteUnknown) || f.calls != 1 {
		t.Fatalf("read-back did not persist without redispatch: %+v %v", entries, err)
	}
	if err := p.Reconcile(context.Background(), intent.OperationID, "", "checked", true, true); err == nil {
		t.Fatal("reconciliation without operator accepted")
	}
	if err := p.Reconcile(context.Background(), intent.OperationID, "brett", "checked", false, true); err == nil {
		t.Fatal("reconciliation without no-commit attestation accepted")
	}
	if err := p.Reconcile(context.Background(), intent.OperationID, "brett", "provider audit shows no commit and worker stopped", true, true); err != nil {
		t.Fatal(err)
	}
	record, err := p.Journal.Read(intent.OperationID)
	if err != nil || record.ReconciliationOperator != "brett" || record.Reconciliation == "" {
		t.Fatalf("operator evidence missing: %+v %v", record, err)
	}
	entries, err = p.Journal.Recovery()
	if err != nil || len(entries) != 0 {
		t.Fatalf("reconciled entry remains open: %+v %v", entries, err)
	}
}

func TestWritePipelineConcurrentAdmission(t *testing.T) {
	t.Parallel()
	p, f, intent := writeSetup(t)
	f.observation.Outcome = WriteUnknown
	var wg sync.WaitGroup
	for _, id := range []string{strings.Repeat("a", 32), strings.Repeat("b", 32)} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			candidate := intent
			candidate.OperationID = id
			_, _ = p.Start(context.Background(), candidate)
		}(id)
	}
	wg.Wait()
	if f.calls != 1 {
		t.Fatalf("concurrent admission dispatched %d writes", f.calls)
	}
}

func TestWritePipelineActionEligibility(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                   string
		action                 Action
		mapping, transition    string
		ready, owner, evidence bool
		want                   WriteVerification
	}{
		{"blocked-owner", ActionReportBlocked, "blocked", "Blocked", false, true, false, WriteVerified},
		{"progress-owner", ActionRecordProgress, "", "", false, true, false, WriteVerified},
		{"review-owner", ActionRequestReview, "review", "Review", false, true, false, WriteVerified},
		{"reopen-owner", ActionReopen, "reopen", "Open", false, true, false, WriteVerified},
		{"start-blocked", ActionStart, "start", "Doing", false, true, false, WriteConflict},
		{"complete-no-evidence", ActionComplete, "complete", "Done", false, true, false, WriteConflict},
		{"complete-evidenced", ActionComplete, "complete", "Done", false, true, true, WriteVerified},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, f, intent := writeSetup(t)
			intent.Action, intent.Transition = tc.action, tc.transition
			if tc.transition != "" {
				intent.Patch["status"] = tc.transition
				f.observation.Patch["status"] = tc.transition
			}
			p.Workflow[tc.mapping] = tc.transition
			f.pre.Ready, f.pre.Owner, f.pre.CompletionEvidence = tc.ready, tc.owner, tc.evidence
			result, _ := p.Start(context.Background(), intent)
			if result.Outcome != tc.want || f.calls != map[WriteVerification]int{WriteVerified: 1, WriteConflict: 0}[tc.want] {
				t.Fatalf("result=%+v calls=%d", result, f.calls)
			}
		})
	}
}

func TestWritePipelineReadReceiptConflict(t *testing.T) {
	t.Parallel()
	p, f, intent := writeSetup(t)
	f.observation.Outcome = WriteConflict
	result, err := p.Start(context.Background(), intent)
	if err != nil || result.Outcome != WriteConflict || f.calls != 1 || f.checkpointCalls != 0 {
		t.Fatalf("conflict=%+v err=%v", result, err)
	}
}

func TestWritePipelineLostStateResponseNeedsReconciliation(t *testing.T) {
	t.Parallel()
	p, f, intent := writeSetup(t)
	f.writeErr = errors.New("response lost")
	_, _ = p.Start(context.Background(), intent)
	result, err := p.Recover(context.Background(), intent.OperationID)
	if err != nil || result.Outcome != WriteUnknown || f.calls != 1 || f.checkpointCalls != 0 {
		t.Fatalf("state without receipt was attributed to this write: %+v %v", result, err)
	}
}

func TestWritePipelineAppendProvenanceAndSideEffects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		modify func(*ReceiptObservation)
	}{
		{"copied-marker", func(o *ReceiptObservation) { o.AppendProof = false }},
		{"changed-content", func(o *ReceiptObservation) { o.AppendContent = "different" }},
		{"duplicate-marker", func(o *ReceiptObservation) { o.MarkerCount = 2 }},
		{"wrong-item", func(o *ReceiptObservation) { o.ItemID = "other" }},
		{"wrong-actor", func(o *ReceiptObservation) { o.Actor = "mallory" }},
		{"wrong-provenance", func(o *ReceiptObservation) { o.OperationID = "other" }},
		{"unverified-effect", func(o *ReceiptObservation) { o.Effects = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p, f, intent := writeSetup(t)
			intent.Append = "progress"
			intent.Marker = "worklease-op:" + intent.OperationID
			intent.Effects = []string{"git-commit"}
			f.observation.AppendProof = true
			f.observation.AppendContent = "progress"
			f.observation.MarkerCount = 1
			f.observation.OperationID = intent.OperationID
			f.observation.Effects = map[string]bool{"git-commit": true}
			f.writeErr = errors.New("lost response")
			tc.modify(&f.observation)
			result, _ := p.Start(context.Background(), intent)
			if result.Outcome != WriteUnknown {
				t.Fatalf("start=%+v", result)
			}
			result, err := p.Recover(context.Background(), intent.OperationID)
			if err != nil || result.Outcome != WriteUnknown || f.calls != 1 || f.checkpointCalls != 0 {
				t.Fatalf("result=%+v err=%v calls=%d checkpoints=%d", result, err, f.calls, f.checkpointCalls)
			}
		})
	}
}

func TestWriteJournalRejectsCacheOverlap(t *testing.T) {
	t.Parallel()
	_, env := testkit.Home(t)
	cache := filepath.Join(env["HOME"], ".cache", "worklease", "queue")
	for _, journal := range []string{cache, filepath.Join(cache, "recovery"), filepath.Dir(cache)} {
		if _, err := NewWriteJournal(journal, cache); err == nil {
			t.Fatalf("accepted overlapping journal %q", journal)
		}
	}
}

func TestWriteJournalRetentionAndPrivacy(t *testing.T) {
	t.Parallel()
	p, f, intent := writeSetup(t)
	f.observation.Outcome = WriteUnknown
	_, _ = p.Start(context.Background(), intent)
	st, err := os.Stat(filepath.Join(p.Journal.Dir, intent.OperationID+".json"))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("private journal: %v %v", st, err)
	}
	p.Now = func() time.Time { return time.Now().Add(60 * 24 * time.Hour) }
	if err := p.Journal.Prune(p.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Journal.Read(intent.OperationID); err != nil {
		t.Fatal("unresolved intent pruned", err)
	}
	f.observation.Outcome = WriteVerified
	p.Now = time.Now
	if result, err := p.Recover(context.Background(), intent.OperationID); err != nil || result.Outcome != WriteVerified {
		t.Fatalf("recover: %+v %v", result, err)
	}
	if err := p.Journal.Prune(p.now().Add(31 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Journal.Read(intent.OperationID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("resolved record not pruned: %v", err)
	}
}
