---
id: TASK-6
title: Add explicit unknown-operation reconciliation
status: To Do
assignee: []
created_date: '2026-07-14 02:06'
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
