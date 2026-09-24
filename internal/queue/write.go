package queue

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brettinternet/worklease/internal/handle"
)

// WriteIntent is the exact provider-side mutation, not a Worklease guarded operation.
// A direct provider write has only cooperative claim protection.
type WriteIntent struct {
	OperationID        string            `json:"operationId"`
	Source             Source            `json:"source"`
	Ref                Ref               `json:"ref"`
	Principal          string            `json:"principal"`
	Patch              map[string]string `json:"patch"`
	Precondition       string            `json:"precondition"`
	AuthorityID        string            `json:"authorityId"`
	ClaimID            string            `json:"claimId"`
	ClaimRevision      int64             `json:"claimRevision"`
	Resources          []string          `json:"resources"`
	OperationRef       string            `json:"operationRef"`
	CheckpointTTL      time.Duration     `json:"checkpointTtl"`
	CheckpointNotAfter time.Time         `json:"checkpointNotAfter"`
	ClaimHoldUntil     time.Time         `json:"claimHoldUntil,omitempty"`
	Action             Action            `json:"action"`
	Transition         string            `json:"transition,omitempty"`
	Append             string            `json:"append,omitempty"`
	Marker             string            `json:"marker,omitempty"`
	Effects            []string          `json:"effects,omitempty"`
}

// ProviderReceipt is evidence returned by the provider write, not a Worklease receipt.
type ProviderReceipt struct {
	SourceID string `json:"sourceId"`
	ItemID   string `json:"itemId"`
	ID       string `json:"id,omitempty"`
	Actor    string `json:"actor,omitempty"`
	Version  string `json:"version,omitempty"`
}

type WriteVerification string

const (
	WriteVerified WriteVerification = "verified"
	WriteConflict WriteVerification = "conflict"
	WriteUnknown  WriteVerification = "unknown"
)

// ReceiptObservation is source evidence. AppendProof must come from the
// provider's append provenance, not merely a search for the marker in a body.
type ReceiptObservation struct {
	Outcome       WriteVerification
	SourceID      string
	ItemID        string
	Patch         map[string]string
	Precondition  string
	MarkerCount   int
	AppendContent string
	AppendProof   bool
	ReceiptID     string
	OperationID   string // provider append provenance, when exposed
	Actor         string
	Effects       map[string]bool
}

type WriteAdapter interface {
	// Inspect refreshes the item, its dependency closure, actor and capability.
	Inspect(context.Context, WriteIntent) (WritePreflight, error)
	ValidateTransition(context.Context, Source, Action, string) error
	Write(context.Context, WriteIntent) (ProviderReceipt, error)
	ReadReceipt(context.Context, WriteIntent, *ProviderReceipt) (ReceiptObservation, error)
}

type WritePreflight struct {
	Capability         bool
	Authorized         bool
	InScope            bool
	Fresh              bool
	NativeAvailable    bool
	Ready              bool
	Owner              bool
	CompletionEvidence bool
	Precondition       string
}

type WriteClaim interface {
	Verify(context.Context, WriteIntent) error
	// Checkpoint and CheckpointStatus use the exact journaled OperationRef and
	// canonical receipt payload. Checkpoint must replay idempotently under that
	// reference; it must never generate a new operation ID on recovery.
	Checkpoint(context.Context, WriteIntent, ProviderReceipt) error
	CheckpointStatus(context.Context, WriteIntent, ProviderReceipt) (WriteVerification, error)
}

type WriteResult struct {
	Outcome         WriteVerification `json:"outcome"`
	Detail          string            `json:"detail,omitempty"`
	ClaimHeld       bool              `json:"claimHeld"`
	SourceUnchanged bool              `json:"sourceUnchanged,omitempty"`
	SafeRelease     bool              `json:"safeRelease,omitempty"`
}

// WriteRecord lives in owner-private state, not the disposable queue index.
type WriteRecord struct {
	Intent         WriteIntent      `json:"intent"`
	Receipt        *ProviderReceipt `json:"receipt,omitempty"`
	Status         string           `json:"status"`
	CreatedAt      time.Time        `json:"createdAt"`
	ResolvedAt     time.Time        `json:"resolvedAt,omitempty"`
	Reconciliation string           `json:"reconciliation,omitempty"`
}

const maxWriteRecord = 1 << 20
const resolvedWriteRetention = 30 * 24 * time.Hour

type WriteJournal struct{ Dir string }

// NewWriteJournal rejects state placement within (or overlapping) a disposable cache.
func NewWriteJournal(stateDir, cacheDir string) (WriteJournal, error) {
	if !filepath.IsAbs(stateDir) || !filepath.IsAbs(cacheDir) || filepath.Clean(stateDir) == filepath.Clean(cacheDir) {
		return WriteJournal{}, fmt.Errorf("journal and cache need separate absolute directories")
	}
	if rel, err := filepath.Rel(cacheDir, stateDir); err != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return WriteJournal{}, fmt.Errorf("journal must be outside the cache")
	}
	if rel, err := filepath.Rel(stateDir, cacheDir); err != nil || rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return WriteJournal{}, fmt.Errorf("cache must be outside the journal")
	}
	return WriteJournal{Dir: filepath.Clean(stateDir)}, nil
}

func validOperationID(id string) bool {
	if len(id) != 32 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func NewWriteOperationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (j WriteJournal) path(id string) (string, error) {
	if !filepath.IsAbs(j.Dir) || !validOperationID(id) {
		return "", fmt.Errorf("invalid journal path or operation ID")
	}
	return filepath.Join(j.Dir, id+".json"), nil
}

// ClaimLock is shared with queue cancellation. Hold it through dispatch or
// cancellation release. Acquire it before the item/claim-handle locks.
func (j WriteJournal) ClaimLock(ctx context.Context, authorityID, claimID string) (*handle.Lock, error) {
	if authorityID == "" || claimID == "" || !filepath.IsAbs(j.Dir) {
		return nil, fmt.Errorf("invalid journal claim")
	}
	if err := handle.EnsureOwnerPrivateDir(j.Dir); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(authorityID + "\x00" + claimID))
	return handle.AcquireLock(ctx, filepath.Join(j.Dir, "claim-"+hex.EncodeToString(sum[:])+".lock"))
}

// lockItem serializes journal admission for one provider item across processes.
func (j WriteJournal) lockItem(ctx context.Context, ref Ref) (*handle.Lock, error) {
	if !validRef(ref) || !filepath.IsAbs(j.Dir) {
		return nil, fmt.Errorf("invalid journal item")
	}
	if err := handle.EnsureOwnerPrivateDir(j.Dir); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(ref.Key()))
	return handle.AcquireLock(ctx, filepath.Join(j.Dir, hex.EncodeToString(sum[:])+".lock"))
}

func (j WriteJournal) Read(id string) (WriteRecord, error) {
	path, err := j.path(id)
	if err != nil {
		return WriteRecord{}, err
	}
	data, err := handle.ReadOwnerPrivate(path, maxWriteRecord)
	if err != nil {
		return WriteRecord{}, err
	}
	var record WriteRecord
	if err = json.Unmarshal(data, &record); err != nil || record.Intent.OperationID != id || record.Status == "" {
		return WriteRecord{}, fmt.Errorf("invalid recovery record")
	}
	return record, nil
}

func (j WriteJournal) save(record WriteRecord, create bool) error {
	path, err := j.path(record.Intent.OperationID)
	if err != nil {
		return err
	}
	if err = handle.EnsureOwnerPrivateDir(j.Dir); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if create {
		return handle.WriteOwnerPrivateNoReplace(path, data, maxWriteRecord)
	}
	return handle.WriteOwnerPrivate(path, data, maxWriteRecord)
}

func (j WriteJournal) Records() ([]WriteRecord, error) {
	if err := handle.EnsureOwnerPrivateDir(j.Dir); err != nil {
		return nil, err
	}
	names, err := handle.ListOwnerPrivateNames(j.Dir)
	if err != nil {
		return nil, err
	}
	var records []WriteRecord
	for _, name := range names {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		record, err := j.Read(id)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

// Cancellation is denied for any journaled intent under this claim, even if
// it was later resolved; an effect might already have occurred.
func (j WriteJournal) CanCancel(claimID string) (bool, error) {
	if err := j.Prune(time.Now()); err != nil {
		return false, err
	}
	records, err := j.Records()
	if err != nil {
		return false, err
	}
	for _, record := range records {
		if record.Intent.ClaimID == claimID {
			return false, nil
		}
	}
	return true, nil
}

func (j WriteJournal) Prune(now time.Time) error {
	records, err := j.Records()
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.Status != "resolved" && record.Status != "reconciled" || record.ResolvedAt.IsZero() || now.Sub(record.ResolvedAt) < resolvedWriteRetention {
			continue
		}
		path, err := j.path(record.Intent.OperationID)
		if err != nil {
			return err
		}
		if err := handle.RemoveOwnerPrivate(path); err != nil {
			return err
		}
	}
	return nil
}

// WritePipeline dispatches at most once. Recovery only reads and checkpoints.
type WritePipeline struct {
	Adapter  WriteAdapter
	Claim    WriteClaim
	Journal  WriteJournal
	Workflow map[string]string // configured transitions for this source; no implicit statuses
	Now      func() time.Time
}

func (p WritePipeline) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

func (p WritePipeline) Start(ctx context.Context, intent WriteIntent) (WriteResult, error) {
	if p.Adapter == nil || p.Claim == nil || !validOperationID(intent.OperationID) || intent.Ref.SourceID != intent.Source.ID || intent.Ref.ItemID == "" || intent.Principal == "" || intent.ClaimID == "" || intent.AuthorityID == "" || intent.ClaimRevision < 1 || len(intent.Resources) == 0 || len(intent.Patch) == 0 && intent.Append == "" {
		return heldUnchanged(), fmt.Errorf("incomplete write intent")
	}
	if intent.Append != "" && intent.Marker != "worklease-op:"+intent.OperationID {
		return heldUnchanged(), fmt.Errorf("invalid append marker")
	}
	if intent.Action != ActionRecordProgress && intent.Action != ActionAssignToMe {
		mapping := map[Action]string{ActionStart: "start", ActionResume: "start", ActionReportBlocked: "blocked", ActionRequestReview: "review", ActionComplete: "complete", ActionReopen: "reopen"}[intent.Action]
		if mapping == "" || p.Workflow[mapping] == "" || p.Workflow[mapping] != intent.Transition {
			return heldUnchanged(), fmt.Errorf("no-workflow-mapping")
		}
		if err := p.Adapter.ValidateTransition(ctx, intent.Source, intent.Action, intent.Transition); err != nil {
			return heldUnchanged(), err
		}
	}
	if err := p.Journal.Prune(p.now()); err != nil {
		return heldUnchanged(), err
	}
	if intent.CheckpointTTL <= 0 {
		return heldUnchanged(), fmt.Errorf("checkpoint TTL required")
	}
	if intent.CheckpointNotAfter.IsZero() {
		intent.CheckpointNotAfter = p.now().Add(time.Hour)
	}
	if !intent.CheckpointNotAfter.After(p.now()) {
		return heldUnchanged(), fmt.Errorf("checkpoint request deadline expired")
	}
	if intent.OperationRef == "" {
		var err error
		intent.OperationRef, err = NewWriteOperationID()
		if err != nil {
			return heldUnchanged(), err
		}
	}
	if !validOperationID(intent.OperationRef) || intent.OperationRef == intent.OperationID {
		return heldUnchanged(), fmt.Errorf("invalid checkpoint operation reference")
	}
	claimLock, err := p.Journal.ClaimLock(ctx, intent.AuthorityID, intent.ClaimID)
	if err != nil {
		return heldUnchanged(), err
	}
	defer claimLock.Close()
	lock, err := p.Journal.lockItem(ctx, intent.Ref)
	if err != nil {
		return heldUnchanged(), err
	}
	defer lock.Close()
	if _, err := p.Journal.Read(intent.OperationID); err == nil {
		return WriteResult{Outcome: WriteUnknown, ClaimHeld: true, Detail: "operation already journaled; recover without redispatch"}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return heldUnchanged(), err
	}
	if records, err := p.Journal.Records(); err != nil {
		return heldUnchanged(), err
	} else {
		for _, record := range records {
			if record.Status != "resolved" && record.Status != "reconciled" && record.Intent.Ref == intent.Ref {
				return WriteResult{Outcome: WriteUnknown, ClaimHeld: true, Detail: "item has unresolved provider write; recovery required"}, nil
			}
		}
	}
	if err := p.Claim.Verify(ctx, intent); err != nil {
		return heldUnchanged(), err
	}
	pre, err := p.Adapter.Inspect(ctx, intent)
	if err != nil {
		return heldUnchanged(), err
	}
	if !pre.Capability || !pre.Authorized || !pre.InScope || !pre.Fresh || !pre.NativeAvailable || pre.Precondition != intent.Precondition || !actionWriteEligible(intent.Action, pre) {
		return heldUnchanged(), fmt.Errorf("provider write preflight rejected")
	}
	if err := p.Claim.Verify(ctx, intent); err != nil {
		return heldUnchanged(), err
	}
	record := WriteRecord{Intent: intent, Status: "pending", CreatedAt: p.now()}
	if err := p.Journal.save(record, true); err != nil {
		// A failed directory fsync may follow a successful atomic install.
		// Do not offer cancellation until the journal outcome is known.
		return WriteResult{Outcome: WriteUnknown, ClaimHeld: true, Detail: "journal persistence uncertain; inspect recovery before release"}, err
	}
	// Recheck after persisting, so a cancelled claim is not used to dispatch.
	if err := p.Claim.Verify(ctx, intent); err != nil {
		return WriteResult{Outcome: WriteUnknown, ClaimHeld: false, Detail: "claim changed after journaling; recovery required"}, err
	}
	// The fsynced intent is the point of no cancellation. Even a failed
	// dispatch is unknown unless the source proves it made no effect.
	receipt, writeErr := p.Adapter.Write(ctx, intent)
	if writeErr != nil {
		return WriteResult{Outcome: WriteUnknown, ClaimHeld: true, Detail: "provider outcome unknown; recovery required"}, writeErr
	}
	if receipt.SourceID != intent.Ref.SourceID || receipt.ItemID != intent.Ref.ItemID {
		return WriteResult{Outcome: WriteUnknown, ClaimHeld: true, Detail: "receipt identity mismatch"}, nil
	}
	record.Receipt = &receipt
	record.Status = "receipt"
	if err := p.Journal.save(record, false); err != nil {
		return WriteResult{Outcome: WriteUnknown, ClaimHeld: true}, err
	}
	return p.verify(ctx, record)
}

func heldUnchanged() WriteResult {
	return WriteResult{Outcome: WriteConflict, Detail: "claim held / source unchanged", ClaimHeld: true, SourceUnchanged: true, SafeRelease: true}
}

func actionWriteEligible(action Action, pre WritePreflight) bool {
	switch action {
	case ActionStart, ActionResume:
		return pre.Ready
	case ActionReportBlocked, ActionRecordProgress, ActionAssignToMe, ActionRequestReview, ActionReopen:
		return pre.Owner
	case ActionComplete:
		return pre.Owner && pre.CompletionEvidence
	default:
		return false
	}
}

// Recover never calls Write, regardless of whether read-back lags or the
// original provider request returned an error, timed out, or lost its response.
func (p WritePipeline) Recover(ctx context.Context, id string) (WriteResult, error) {
	record, err := p.Journal.Read(id)
	if err != nil {
		return WriteResult{}, err
	}
	lock, err := p.Journal.lockItem(ctx, record.Intent.Ref)
	if err != nil {
		return WriteResult{}, err
	}
	defer lock.Close()
	record, err = p.Journal.Read(id)
	if err != nil {
		return WriteResult{}, err
	}
	if record.Status == "resolved" {
		return WriteResult{Outcome: WriteVerified, ClaimHeld: true}, nil
	}
	if record.Status == "reconciled" {
		return WriteResult{Outcome: WriteConflict, ClaimHeld: true, Detail: "operator reconciliation recorded; no automatic checkpoint"}, nil
	}
	return p.verify(ctx, record)
}

// Reconcile closes unknown provider intent only on an explicit operator
// attestation that the previous executor ceased and the provider outcome was
// investigated. It never manufactures a provider checkpoint or verification.
func (p WritePipeline) Reconcile(ctx context.Context, id, evidence string, executorCeased bool) error {
	if strings.TrimSpace(evidence) == "" || !executorCeased {
		return fmt.Errorf("reconciliation requires evidence and executor cessation")
	}
	record, err := p.Journal.Read(id)
	if err != nil {
		return err
	}
	lock, err := p.Journal.lockItem(ctx, record.Intent.Ref)
	if err != nil {
		return err
	}
	defer lock.Close()
	record, err = p.Journal.Read(id)
	if err != nil {
		return err
	}
	if record.Status == "resolved" || record.Status == "reconciled" {
		return fmt.Errorf("provider write already closed")
	}
	record.Status = "reconciled"
	record.Reconciliation = evidence
	record.ResolvedAt = p.now()
	return p.Journal.save(record, false)
}

func (p WritePipeline) verify(ctx context.Context, record WriteRecord) (WriteResult, error) {
	intent := record.Intent
	observation, err := p.Adapter.ReadReceipt(ctx, intent, record.Receipt)
	if err != nil {
		return WriteResult{Outcome: WriteUnknown, ClaimHeld: true, Detail: "read-back unavailable"}, err
	}
	result := checkWriteEvidence(intent, record.Receipt, observation)
	if result != WriteVerified {
		return WriteResult{Outcome: result, ClaimHeld: true, Detail: "read-back not verified; recovery required"}, nil
	}
	if record.Status == "checkpoint-pending" {
		return p.finishCheckpoint(ctx, record)
	}
	if err := p.Claim.Verify(ctx, intent); err != nil {
		return WriteResult{Outcome: WriteUnknown, ClaimHeld: true}, err
	}
	if record.Receipt == nil { // Lost response: retain independently verified source identity.
		record.Receipt = &ProviderReceipt{SourceID: intent.Ref.SourceID, ItemID: intent.Ref.ItemID, ID: observation.ReceiptID, Actor: observation.Actor}
		if err := p.Journal.save(record, false); err != nil {
			return WriteResult{Outcome: WriteUnknown, ClaimHeld: true}, err
		}
	}
	record.Status = "checkpoint-pending"
	if err := p.Journal.save(record, false); err != nil {
		return WriteResult{Outcome: WriteUnknown, ClaimHeld: true, Detail: "checkpoint preparation uncertain"}, err
	}
	return p.finishCheckpoint(ctx, record)
}

func (p WritePipeline) finishCheckpoint(ctx context.Context, record WriteRecord) (WriteResult, error) {
	if record.Receipt == nil || !validOperationID(record.Intent.OperationRef) {
		return WriteResult{Outcome: WriteUnknown, ClaimHeld: true}, fmt.Errorf("checkpoint request incomplete")
	}
	status, err := p.Claim.CheckpointStatus(ctx, record.Intent, *record.Receipt)
	if err != nil {
		return WriteResult{Outcome: WriteUnknown, ClaimHeld: true, Detail: "checkpoint outcome unknown"}, err
	}
	if status == WriteConflict {
		return WriteResult{Outcome: WriteConflict, ClaimHeld: true}, nil
	}
	if status == WriteUnknown {
		if !p.now().Before(record.Intent.CheckpointNotAfter) {
			return WriteResult{Outcome: WriteUnknown, ClaimHeld: true, Detail: "checkpoint replay deadline expired; reconciliation required"}, nil
		}
		// Exact idempotent replay is safe even if the first response was lost.
		if err := p.Claim.Checkpoint(ctx, record.Intent, *record.Receipt); err != nil {
			return WriteResult{Outcome: WriteUnknown, ClaimHeld: true, Detail: "checkpoint outcome unknown"}, err
		}
	}
	record.Status = "resolved"
	record.ResolvedAt = p.now()
	if err := p.Journal.save(record, false); err != nil {
		return WriteResult{Outcome: WriteUnknown, ClaimHeld: true, Detail: "checkpoint recorded; journal resolution pending"}, err
	}
	return WriteResult{Outcome: WriteVerified, ClaimHeld: true}, nil
}

func checkWriteEvidence(intent WriteIntent, receipt *ProviderReceipt, observed ReceiptObservation) WriteVerification {
	if observed.Outcome != WriteVerified {
		return observed.Outcome
	}
	// A matching state alone cannot attribute a lost non-append write to this
	// operation; it may have been independently changed by another writer.
	if receipt == nil && intent.Append == "" {
		return WriteUnknown
	}
	if observed.SourceID != intent.Ref.SourceID || observed.ItemID != intent.Ref.ItemID || observed.Precondition != intent.Precondition {
		return WriteUnknown
	}
	for key, value := range intent.Patch {
		if observed.Patch[key] != value {
			return WriteUnknown
		}
	}
	if receipt != nil && (receipt.SourceID != intent.Ref.SourceID || receipt.ItemID != intent.Ref.ItemID || receipt.ID != "" && receipt.ID != observed.ReceiptID || receipt.Actor != "" && receipt.Actor != observed.Actor) {
		return WriteUnknown
	}
	if observed.Actor != "" && observed.Actor != intent.Principal {
		return WriteUnknown
	}
	if intent.Append != "" && (observed.MarkerCount != 1 || observed.AppendContent != intent.Append || !observed.AppendProof || observed.OperationID != "" && observed.OperationID != intent.OperationID) {
		return WriteUnknown
	}
	for _, effect := range intent.Effects {
		if !observed.Effects[effect] {
			return WriteUnknown
		}
	}
	return WriteVerified
}
