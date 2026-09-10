---
id: TASK-6
title: Add explicit unknown-operation reconciliation
status: Done
assignee:
  - '@codex-main'
created_date: '2026-07-14 02:06'
updated_date: '2026-07-14 05:22'
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
- [x] #1 A caller can inspect an operation by resource and operation ID and distinguish started/unknown, completed, and explicitly reconciled outcomes without receiving bearer tokens.
- [x] #2 An authorized current claimant can reconcile a prior unknown operation as observed success or observed failure with bounded caller-supplied evidence; stale claims and mismatched request fingerprints are rejected.
- [x] #3 Reconciliation is idempotent, cannot silently rerun or mutate the original external operation, and preserves the original request, timestamps, result, and resolver identity.
- [x] #4 A reconciled operation can be safely referenced by a later workflow so callers know whether to continue, stop, or issue a new operation ID for an explicitly approved retry.
- [x] #5 Crash, storage-failure, stale-owner, duplicate-resolution, malformed-evidence, and read-only token-redaction cases are covered by automated tests and documented in the CLI/API contract.
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

Implementation checkpoint (T1 inspect immutable operation outcomes): commit fcb936eb0603922b2d2871a8ab8c9d10e494c20b adds read-only LeaseStore.inspect_operation projections for unknown, completed, and reconciled outcomes with canonical request SHA-256 fingerprints, no token/request/receipt/evidence exposure, and operation-not-found validation; adds reconciliation schema initialization/migration coverage. Targeted verification: mise exec -- uv run python -m unittest tests.test_store passed (24 tests); mise run lint passed; mise run format-check passed; mise run test passed (69 tests); mise run typecheck passed; mise run hooks passed. Progress: T1 complete. Next task: T2 authorized reconciliation and inspect-operation/reconcile-operation CLI wiring. Remaining acceptance: #2-#5 and T2/T3.,cwd:/Users/brett/dev/me/worklease/.worktrees/task-6-t1,timeout:120}

T1 verifier correction: commit 4546c5cec2c14893a4ebaa66c8ef0216c4c84482 rejects ambiguous exact resource/operation-id lookups when an operation ID was reused across reclaimed claims, with focused coverage (25 store tests). Independent verifier PASS: unknown/completed/reconciled states, redacted projections, persisted-request SHA-256, original-row preservation, reconciliation-table initialization/migration, ambiguity guard, and unchanged exec/replace replay paths. Corrected verification: mise run lint, format-check, test (70 tests), typecheck, and hooks all passed. T1 remains complete; next task T2. ,cwd:/Users/brett/dev/me/worklease,timeout:120}

Implementation checkpoint (T2 authorized reconciliation): commit c082a9bf6b37c57829ba74434ecbb68ab8c167dc adds append-only reconciliation records with strict bounded JSON evidence, active-claim/revision/fingerprint checks, idempotent changed-replay rejection, redacted receipts, migration support, and inspect-operation/reconcile-operation CLI commands. Verification: mise run lint, format-check, test (74 tests), typecheck, and hooks passed. Progress: T2 complete. Next task: T3 recovery tests and contract documentation. Remaining acceptance: #5.

T2 review fix: commit e548a029016befacd0497bfb4efbf975dab60c4b rejects reconciliation replays after a later heartbeat or claim expiry while preserving immediate idempotent replay. Added focused stale-revision and claim-expired coverage; full quality gates and hooks passed (74 tests). T2 remains complete; next task T3.

Independent verifier found and confirmed one defect: direct LeaseStore reconciliation accepted arbitrary outcomes despite CLI choices. Fixed in commit c5cb74663f4fbe5f9fccfdd9739f145b16d352bb with store-side observed-success/observed-failure validation and focused regression coverage. Targeted store+CLI verification passed 41 tests; lint, format-check, typecheck, hooks passed. Verifier recheck criteria otherwise PASS; T2 complete, T3 next.

Implementation checkpoint (T3 recovery tests and contract documentation): commit e1010b0b3ef06c709d6296d93f6a4c59de1cd582 adds fingerprint-mismatch, malformed/oversized-evidence, and reconciliation storage-rollback tests; documents inspect-operation/reconcile-operation recovery, immutable audit, bounded evidence, idempotent replay, and new-operation retry guidance in README and CLI contract. Targeted verification: mise exec -- uv run python -m unittest tests.test_store tests.test_cli passed (43 tests); mise run lint passed; mise run format-check passed; mise run test passed; mise run typecheck passed; mise run hooks passed. Progress: T3 complete. Next task: review complete accumulated TASK-6 implementation. Remaining acceptance: review evidence.

Verifier correction: migration coverage was incomplete because the test did not exercise ALTER TABLE for a legacy reconciliations table. Commit e411ce8df41b36d2684129818ae7dd6af7bd8226 constructs a receipt-less legacy table, triggers inspection migration, and asserts the receipt column is restored. Reverification: focused tests.test_store and tests.test_cli passed (43 tests); mise run lint, format-check, test, typecheck, and hooks passed. T3 remains complete; next task: review complete accumulated TASK-6 implementation including e1010b0b3ef06c709d6296d93f6a4c59de1cd582 and e411ce8df41b36d2684129818ae7dd6af7bd8226.

Review checkpoint: reviewed the complete TASK-6 implementation at commits 723f414, 4fefe69, c082a9b, e548a02, c5cb746, e1010b0, plus review-fix commit e411ce8. Full state/data-integrity/concurrency/API review found no remaining correctness, security, compatibility, performance, or maintainability findings after the migration-coverage fix. Verification: mise run lint, mise run format-check, mise run test (70 tests), mise run typecheck, and mise run hooks passed on the exact review snapshot. Review depth: full implementation-review rubric; findings/fixes: migration test coverage fixed in e411ce8. All acceptance criteria verified against implementation and tests; ready for integration.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented and reviewed explicit unknown-operation reconciliation. Added redacted inspect-operation projections, authorized append-only observed outcome reconciliation with bounded strict JSON evidence, fingerprint and claim/revision validation, idempotent replay, migration support, CLI wiring, recovery tests, and operator documentation. Review covered the accumulated implementation and e411ce8 migration-coverage fix under the full implementation-review rubric. Integrated to main at e411ce8; full quality gates passed (70 tests).
<!-- SECTION:FINAL_SUMMARY:END -->
