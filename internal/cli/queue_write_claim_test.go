package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
	"github.com/brettinternet/worklease/internal/queue"
	"github.com/brettinternet/worklease/internal/queueui"
)

func TestQueueWriteClaimReplaysExactCheckpointOnOriginalHandle(t *testing.T) {
	_, path, backend := claimedLifecycle(t)
	h, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	intent := queue.WriteIntent{OperationID: randomHex(16), OperationRef: randomHex(16), Source: queue.Source{ID: "tasks"}, Ref: queue.Ref{SourceID: "tasks", ItemID: "1"}, Principal: "alice", Patch: map[string]string{"status": "Doing"}, Precondition: "version-1", AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, ClaimRevision: h.Revision, Resources: h.Resources, CheckpointTTL: 30 * time.Second, CheckpointNotAfter: time.Now().Add(10 * time.Minute).Truncate(time.Microsecond).Add(123 * time.Nanosecond)}
	receipt := queue.ProviderReceipt{SourceID: "tasks", ItemID: "1", ID: "provider-receipt", Actor: "alice"}
	claim := queueWriteClaim{backend: backend, path: path, session: h.SessionID}
	wrongSession := claim
	wrongSession.session = "unrelated-session"
	if err := wrongSession.Verify(context.Background(), intent); err == nil {
		t.Fatal("different queue session adopted handle")
	}
	if err := claim.Verify(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	if status, err := claim.CheckpointStatus(context.Background(), intent, receipt); err != nil || status != queue.WriteUnknown {
		t.Fatalf("before checkpoint: %s %v", status, err)
	}
	if err := claim.Checkpoint(context.Background(), intent, receipt); err != nil {
		t.Fatal(err)
	}
	if status, err := claim.CheckpointStatus(context.Background(), intent, receipt); err != nil || status != queue.WriteVerified {
		t.Fatalf("completed operation with matching receipt not verified: %s %v", status, err)
	}
	updated, err := handle.Read(path)
	if err != nil || updated.Revision <= h.Revision || updated.State != "ready" {
		t.Fatalf("checkpoint handle: %+v %v", updated, err)
	}
	if err := claim.Checkpoint(context.Background(), intent, receipt); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	changedReceipt := receipt
	changedReceipt.ID = "different-receipt"
	if status, err := claim.CheckpointStatus(context.Background(), intent, changedReceipt); err != nil || status != queue.WriteConflict {
		t.Fatalf("wrong receipt status: %s %v", status, err)
	}
	if err := claim.Checkpoint(context.Background(), intent, changedReceipt); err == nil {
		t.Fatal("mismatched checkpoint replay accepted")
	}
	if changed, err := handle.Read(path); err != nil || changed.Revision != updated.Revision {
		t.Fatalf("replay changed revision: %+v %v", changed, err)
	}
	checkpointData, err := queueCheckpointData(intent, receipt)
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string]any{"kind": "checkpoint", "authorityId": intent.AuthorityID, "claimId": intent.ClaimID, "ttl": intent.CheckpointTTL.Microseconds(), "checkpoint": json.RawMessage(checkpointData), "requestNotAfter": intent.CheckpointNotAfter.UTC().UnixMicro()}
	if err := beginHandleMutation(path, &updated, "checkpoint", intent.OperationRef, intent.CheckpointNotAfter, inputs); err != nil {
		t.Fatal(err)
	}
	journal, err := queue.NewWriteJournal(filepath.Join(t.TempDir(), "recovery"), filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.EnsureOwnerPrivateDir(journal.Dir); err != nil {
		t.Fatal(err)
	}
	record := queue.WriteRecord{Intent: intent, Receipt: &receipt, Status: "checkpoint-pending", CreatedAt: time.Now()}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.WriteOwnerPrivate(filepath.Join(journal.Dir, intent.OperationID+".json"), data, 1<<20); err != nil {
		t.Fatal(err)
	}
	pipeline := queue.WritePipeline{Journal: journal, Adapter: checkpointRecoveryAdapter{}, Claim: claim, Now: func() time.Time { return intent.CheckpointNotAfter.Add(time.Second) }}
	result, err := pipeline.Recover(context.Background(), intent.OperationID)
	if err != nil || result.Outcome != queue.WriteVerified {
		t.Fatalf("committed checkpoint after replay deadline: %+v %v", result, err)
	}
	if repaired, err := handle.Read(path); err != nil || repaired.State != "ready" || repaired.PendingRequest != nil || repaired.Revision != updated.Revision {
		t.Fatalf("private handle was not recovered from completed operation: %+v %v", repaired, err)
	}
}

func TestQueueWriteClaimRemoteCheckpointRecovery(t *testing.T) {
	item := queueClaimItem("tasks", "remote")
	controller, backend, adapter := newRemoteQueueClaimController(t, 30*time.Second, item)
	preview := controller.Preview(context.Background(), item)().(queueui.ClaimPreviewMsg)
	if preview.Err != nil {
		t.Fatal(preview.Err)
	}
	if result := controller.AcquireClaim(context.Background(), item, *preview.Preview)().(queueui.ClaimResultMsg); result.Err != nil {
		t.Fatal(result.Err)
	}
	path, err := queueClaimHandlePath(backend.Config.Home, controller.queueSession, controller.profileName, adapter.source, item.Ref)
	if err != nil {
		t.Fatal(err)
	}
	h, err := handle.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	intent := queue.WriteIntent{OperationID: randomHex(16), OperationRef: randomHex(16), AuthorityID: h.AuthorityID, ClaimID: h.ClaimID, ClaimRevision: h.Revision, Resources: h.Resources, CheckpointTTL: 30 * time.Second, CheckpointNotAfter: time.Now().Add(10 * time.Minute)}
	receipt := queue.ProviderReceipt{SourceID: "tasks", ItemID: "remote", ID: "provider-receipt"}
	claim := queueWriteClaim{backend: backend, path: path, session: controller.queueSession}
	if err := claim.Verify(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	if err := claim.Checkpoint(context.Background(), intent, receipt); err != nil {
		t.Fatal(err)
	}
	if status, err := claim.CheckpointStatus(context.Background(), intent, receipt); err != nil || status != queue.WriteVerified {
		t.Fatalf("remote committed checkpoint: %s %v", status, err)
	}
}

type checkpointRecoveryAdapter struct{}

func (checkpointRecoveryAdapter) Inspect(context.Context, queue.WriteIntent) (queue.WritePreflight, error) {
	return queue.WritePreflight{}, nil
}
func (checkpointRecoveryAdapter) ValidateTransition(context.Context, queue.Source, queue.Action, string) error {
	return nil
}
func (checkpointRecoveryAdapter) Write(context.Context, queue.WriteIntent) (queue.ProviderReceipt, error) {
	panic("recovery redispatched provider write")
}
func (checkpointRecoveryAdapter) ReadReceipt(_ context.Context, intent queue.WriteIntent, receipt *queue.ProviderReceipt) (queue.ReceiptObservation, error) {
	return queue.ReceiptObservation{Outcome: queue.WriteVerified, SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID, Patch: intent.Patch, Precondition: intent.Precondition, ReceiptID: receipt.ID, Actor: intent.Principal}, nil
}
