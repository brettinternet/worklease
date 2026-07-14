---
id: TASK-6
title: Add explicit unknown-operation reconciliation
status: To Do
assignee:
  - '@codex-main'
created_date: '2026-07-14 02:06'
updated_date: '2026-07-14 03:33'
labels:
  - coordination
  - recovery
dependencies: []
references:
  - src/worklease/store.py
  - src/worklease/execution.py
  - tests/test_execution.py
  - 'https://github.com/aetomala/worklease'
priority: high
type: enhancement
ordinal: 6000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make operations that were durably started before an external side effect but never completed recoverable without unsafe automatic replay. Worklease must expose the unknown outcome, let an authorized current claimant record an observed result with evidence, and preserve an immutable audit trail. Reconciliation must not weaken claim ownership, revision checks, idempotency, or the distinction between local coordination and provider truth.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A caller can inspect an operation by resource and operation ID and distinguish started/unknown, completed, and explicitly reconciled outcomes without receiving bearer tokens.
- [ ] #2 An authorized current claimant can reconcile a prior unknown operation as observed success or observed failure with bounded caller-supplied evidence; stale claims and mismatched request fingerprints are rejected.
- [ ] #3 Reconciliation is idempotent, cannot silently rerun or mutate the original external operation, and preserves the original request, timestamps, result, and resolver identity.
- [ ] #4 A reconciled operation can be safely referenced by a later workflow so callers know whether to continue, stop, or issue a new operation ID for an explicitly approved retry.
- [ ] #5 Crash, storage-failure, stale-owner, duplicate-resolution, malformed-evidence, and read-only token-redaction cases are covered by automated tests and documented in the CLI/API contract.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. **[T1] Inspect immutable operation outcomes (AC1, AC3).** Add public/store inspection for one resource and operation ID, returning a redacted state projection for started/unknown, completed, or reconciled operations plus a stable request fingerprint. Preserve original operation rows and add focused migration/read tests.
2. **[T2] Authorized reconciliation (AC2, AC3, AC4).** Add an append-only reconciliation record and flat CLI commands `inspect-operation` and `reconcile-operation`. Require the active claim, a distinct reconciliation operation ID, target operation ID, expected request SHA-256, outcome `observed-success|observed-failure`, and strict bounded JSON evidence. Make replay idempotent and reject changed, stale, expired, or already-conflicting resolutions without rerunning work.
3. **[T3] Recovery tests and contract documentation (AC4, AC5).** Cover crash/storage failure, stale owners, fingerprint mismatch, duplicate/changed resolutions, malformed or oversized evidence, migration, and redaction. Document how later workflows branch on reconciled outcome and require a new operation ID for an explicitly approved retry.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
### Refinement snapshot

**Execution status:** available now.

**Goal and target area:** Extend existing operation intent/receipt behavior in `src/worklease/store.py`, `execution.py`, `sqlite.py`, `models.py`, and `cli.py`; add migrations and behavioral tests. Existing started operations already surface `unknown-outcome`; no path is missing.

**Resolved decisions:** Inspection is read-only and keyed by exact resource plus target operation ID. It returns a SHA-256 fingerprint of the persisted canonical request, never the raw request/receipt. Reconciliation is append-only in a dedicated table and records target identity, observed-success/observed-failure, strict JSON evidence capped at 8 KiB, resolver claim/owner/work-key identity, request fingerprint, reconciliation operation ID, and timestamps. Reconciliation never edits or reruns the original operation. Replaying the same resolution is idempotent; changed evidence/outcome/fingerprint conflicts. A later workflow branches on the reconciled outcome and uses a fresh operation ID for any explicitly approved retry.

**Non-goals:** no automatic retry, provider truth inference, force adoption, remote/provider fencing, or TASK-7 verbose projection.

**Evidence and assumptions:** Reuse the existing 8 KiB bounded strict-JSON checkpoint serialization pattern and current operation request canonicalization. Existing crash/storage tests establish the no-rerun invariant.

**Task/acceptance map:** T1→AC1/3; T2→AC2/3/4; T3→AC4/5.

**Pending verification:** migration from current schema; unknown/completed/reconciled reads; stale/fingerprint/idempotency failures; crash/storage and redaction regressions; full quality gates.

**Next action:** implement T1 without changing existing exec/replace-file replay behavior.

**Refinement checkpoint:** refined: TASK-6 specification complete; provider=backlog-md; providerVersion=1.48.0; claimId=dc2b456c-0c06-45bb-84e2-b52322386480; claimRevision=3; refinement: complete.
<!-- SECTION:NOTES:END -->
