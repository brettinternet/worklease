---
id: TASK-14
title: Add safe state retention and garbage collection
status: To Do
assignee:
  - '@codex-main'
created_date: '2026-07-14 02:34'
updated_date: '2026-07-14 03:33'
labels:
  - storage
  - maintenance
  - recovery
dependencies: []
references:
  - src/worklease/sqlite.py
  - src/worklease/store.py
priority: medium
type: enhancement
ordinal: 14000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Add an explicit retention policy and safe garbage-collection operation for durable epochs, operation receipts, release receipts, and obsolete resource metadata. TASK-7 owns read-only diagnostics and TASK-6 owns unknown-operation reconciliation; this task only removes records proven safe to forget.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A dry-run reports deterministic counts and age ranges for records eligible under an explicit retention cutoff without mutating state.
- [ ] #2 Applied garbage collection never deletes active or expired-but-unreclaimed claims, unresolved started operations, records required by current ownership, or receipts inside the documented idempotency and recovery window.
- [ ] #3 Collection runs safely with concurrent acquire, heartbeat, guarded operation, reconciliation, and release activity, leaving either the pre-collection or committed post-collection state after interruption.
- [ ] #4 Malformed cutoffs, unsupported retention settings, storage conflicts, and attempted removal of protected records fail with stable schema-versioned results and no partial deletion.
- [ ] #5 Automated tests cover retention boundaries, protected unknown outcomes, active and expired claims, concurrent lifecycle activity, dry-run parity, interrupted collection, and documented operational guidance.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. **[T1] Deterministic retention inventory (AC1, AC2, AC4).** Add a store/CLI `gc` dry-run that captures one clock/cutoff, defaults to a 30-day retention window, and reports stable per-record-class counts plus oldest/newest eligible timestamps. Protect every current claim (including expired unreclaimed), unresolved started operation, active-ownership epoch, and record newer than the cutoff.
2. **[T2] Atomic garbage collection (AC2-AC4).** Add explicit `gc --apply` using one immediate SQLite transaction so interruption yields the pre-state or committed post-state. Remove only eligible completed/reconciled operation receipts, releases, and unreferenced epochs; compact obsolete live-resource rows only while preserving each exact resource's last revision so future revisions remain monotonic. Reject unsupported policy values or concurrent state changes with stable schema-versioned errors and no partial deletion.
3. **[T3] Boundary, concurrency, and operations coverage (AC5).** Test exact cutoff boundaries, dry-run/apply parity, protected claims/unknown outcomes, idempotent repeat, concurrent acquire/heartbeat, injected interruption, migration, and revision preservation. Document defaults, explicit cutoff override, protected records, backup expectations, and TASK-6/TASK-7 exclusions.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
### Refinement snapshot

**Execution status:** available now.

**Goal and target area:** Extend `src/worklease/sqlite.py`, `store.py`, `models.py`, and `cli.py`; add retention tests and README operations guidance. No retention path exists today; any helper/migration file is an item-local creation.

**Resolved decisions:** `gc` is dry-run by default and requires `--apply` to delete. It computes one UTC cutoff from a default 30-day retention window or explicit cutoff override. Eligible records are completed/reconciled operations, releases, and unreferenced historical epochs strictly older than cutoff. Protect active claims, expired-but-unreclaimed claims, unresolved started operations, current ownership, and all records in the retention window. Apply uses one `BEGIN IMMEDIATE` transaction; dry-run and apply share the same selector. Resource revision continuity is invariant: obsolete live-resource rows may be compacted only into a minimal exact-resource/last-revision tombstone that restores revision+1 on reacquire.

**Non-goals:** TASK-6 reconciliation, TASK-7 diagnostics, automatic background cleanup, vacuuming, remote/provider retention, deleting the database, or weakening exact identity/revision/idempotency guarantees.

**Evidence and assumptions:** Current tables are epochs/resources/claims/operations/releases and existing transactions already provide atomic mutation patterns. A 30-day default is conservative relative to one-hour maximum leases while leaving operators an explicit longer cutoff.

**Task/acceptance map:** T1→AC1/2/4; T2→AC2-4; T3→AC5.

**Pending verification:** old/new boundary, protected unknown/claim records, dry-run/apply parity, concurrent ownership changes, injected rollback, repeated GC, tombstone revision restore, migrations, full quality gates.

**Next action:** implement the shared eligibility query and dry-run result first.

**Refinement checkpoint:** refined: TASK-14 specification complete; provider=backlog-md; providerVersion=1.48.0; claimId=e76c23e9-584d-42b0-af61-192a6067f065; claimRevision=1; refinement: complete.
<!-- SECTION:NOTES:END -->
