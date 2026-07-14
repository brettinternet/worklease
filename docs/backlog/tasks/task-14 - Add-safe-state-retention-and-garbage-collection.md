---
id: TASK-14
title: Add safe state retention and garbage collection
status: In Progress
assignee:
  - '@codex-loop-fresh-20260714-worklease-review-task14'
created_date: '2026-07-14 02:34'
updated_date: '2026-07-14 22:57'
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

Implementation pass T1 claimed under claim 97F686B0-F198-4509-9550-387221E39DC1; canonical in-progress checkpoint refreshed.

Implementation checkpoint (T1): commit e6e598cc6c92a33c2df1cf2d98ef81894c7b75cb adds deterministic gc dry-run inventory and CLI command. Files: src/worklease/cli.py, src/worklease/store.py, tests/test_gc.py. Verification: mise run lint passed; mise run format-check passed; mise run test (126 tests) passed; mise run typecheck passed; mise run hooks passed. Next task: T2 atomic garbage collection. Remaining acceptance criteria: #1-#5.

T1 verification fix claimed under claim A5239979-2980-4C4C-ACB3-3584B2D826EF; unresolved started-operation resource protection under review.

Implementation follow-up (T1 safety fix): commit 317337b4fffe6b36ea97e495e381b8b7505af31f protects resource inventory from unresolved direct and bundle started operations and adds regression coverage. Verification: focused GC tests 4/4 passed; independent verifier PASS including direct/bundle probes, expired-claim protection, strict cutoff, schema-versioned validation errors; mise run lint, mise run format-check, mise run test (127 tests), mise run typecheck, and mise run hooks passed. T1 complete; next task T2 atomic garbage collection; remaining acceptance criteria #1-#5.

Implementation pass T2: commit e6ce9008f753cf81d876bfffcc897b2369bae6a6 adds atomic gc --apply using one BEGIN IMMEDIATE transaction, shared eligibility selectors, protected-record conflict errors, resource revision tombstone preservation, and CLI --apply wiring. Files: src/worklease/store.py, src/worklease/cli.py, tests/test_gc.py. Verification: mise run format-check, mise run lint, mise run test, mise run typecheck, and mise run hooks passed; focused GC tests 6/6 passed. Next task: T3 boundary, concurrency, and operations coverage. Remaining acceptance criteria: #1-#5.

Implementation pass T3 complete under claim D91D3763-F82C-427E-8D1C-5AEDFC3D5952: commit d84d7884f93bc54a51027ac5c8c0b31132873bb adds strict cutoff/boundary, active and expired claim, unresolved bundle operation, concurrent acquire/heartbeat, migration, interruption rollback, and dry-run/apply regression coverage; protects epochs with unresolved started operations and documents gc retention/apply operations in README.md. Verification: focused GC tests 11/11 passed; mise run lint passed; mise run format-check passed; mise run test passed; mise run typecheck passed; mise run hooks passed. All refined implementation tasks complete; next pass REVIEW. Remaining acceptance criteria require review evidence.

T3 verification follow-up: commits d84d7884f93bc54a51027ac5c8c0b31132873bb4 and f90d98dc7c412cdbd6ec567cf948b5b382bf6f1d cover strict cutoff boundaries, age ranges, active/expired claims, unresolved direct and bundle operations, bundle revision tombstones, concurrent acquire/heartbeat/guarded operation/reconciliation/release, migration, interruption rollback, dry-run/apply parity, and repeat collection. README documents defaults, explicit cutoff/apply, protections, backup guidance, and TASK-6/TASK-7 exclusions. Final verification: mise run lint, mise run format-check, mise run test (all tests passed, including 13 GC tests), mise run typecheck, and mise run hooks passed.

T3 coverage follow-ups committed as f90d98d (concurrent operation/reconciliation/release lifecycle, direct unresolved epoch, injected SQLite interruption, bundle tombstone regression) and df1dbcbd0192cf457e42e9d2f07791edefc13ff4/1a6d7450e07db8b96f5da956e321f2b489c5758c (all record-class cutoff fixtures including bundle epochs). Final verification: focused GC tests 14/14 passed; mise run lint passed; mise run format-check passed; mise run test passed; mise run typecheck passed; mise run hooks passed. Independent verifier ran focused GC tests 14/14 and confirmed SQL/runtime protections; its earlier stale boundary finding was closed by 1a6d7450. Implementation tasks remain complete; next pass REVIEW at accumulated commits d84d7884, f90d98d, df1dbcbd, 1a6d7450.

Verifier findings fixed in commit 380c81e2858d8b12ecbdc41d46cb11ff0674f069: GC reconciliation matching now includes claim and kind identity so reused operation IDs cannot hide newer unresolved started operations; commands/index schemas and test_schemas register and validate gc; active/expired claim coverage is isolated; dry-run snapshot proves no mutation. Post-fix verification: uv run python -m unittest tests.test_gc tests.test_schemas (19 passed), mise run lint, mise run format-check, mise run test, mise run typecheck, and mise run hooks passed.

Independent verifier PASS after fixes: AC1-AC5 all PASS; targeted reused-operation regression and packaged gc schema validation pass; focused tests 19/19 pass with no remaining concrete defect.

Review pass claimed for accumulated implementation at e6e598c, 317337b, e6ce900, d84d788, f90d98d, df1dbcb, 1a6d745, 380c81e; canonical review checkpoint refreshed.

Review marker: reviewed implementation commits e6e598cc6c92a33c2df1cf2d98ef81894c7b75cb, 317337b4fffe6b36ea97e495e381b8b7505af31f, e6ce9008f753cf81d876bfffcc897b2369bae6a6, d84d7884f93bc54a51027ac5c8c0b31132873bb4, f90d98dc7c412cdbd6ec567cf948b5b382bf6f1d, df1dbcbd0192cf457e42e9d2f07791edefc13ff4, 1a6d7450e07db8b96f5da956e321f2b489c5758c, 380c81e2858d8b12ecbdc41d46cb11ff0674f069; review-fix commit 63f182387a22cd17bb17aea85d7fcd9ca1a47791. Full implementation-review applied for SQLite transaction atomicity, retention/protection predicates, revision continuity, schema/CLI compatibility, concurrency, migration, failure paths, and operational documentation. Findings fixed: resource protection predicates now match reconciliation claim/kind identity; reused-operation regression expects the monotonic post-reacquire revision. Focused GC/schema tests 19/19 passed; mise run lint, format-check, test, typecheck, and hooks passed. Review is clean; integration deferred because canonical main has overlapping staged TASK-14 schema/test changes and provider notes.

Finalization evidence (review branch HEAD 63f182387a22cd17bb17aea85d7fcd9ca1a47791; accumulated commits e6e598cc6c92a33c2df1cf2d98ef81894c7b75cb, 317337b4fffe6b36ea97e495e381b8b7505af31f, e6ce9008f753cf81d876bfffcc897b2369bae6a6, d84d7884f93bc54a51027ac5c8c0b31132873bb4, f90d98dc7c412cdbd6ec567cf948b5b382bf6f1d, df1dbcbd0192cf457e42e9d2f07791edefc13ff4, 1a6d7450e07db8b96f5da956e321f2b489c5758c, 380c81e2858d8b12ecbdc41d46cb11ff0674f069, review fix 63f182387a22cd17bb17aea85d7fcd9ca1a47791). AC1: focused GC tests verify deterministic dry-run counts, strict cutoff, and age ranges. AC2: focused tests verify active and expired claim protection, unresolved direct/bundle operations, protected records, and revision tombstones. AC3: focused tests verify concurrent acquire/heartbeat/complete/reconcile/release serialization and injected interruption rollback. AC4: focused tests verify malformed cutoff, protected-record rollback, schema-versioned CLI output, migration, and no partial deletion. AC5: focused GC/schema tests 19/19 pass; README operational guidance covers defaults, cutoff, apply, protections, backups, and TASK-6/TASK-7 exclusions. Gates: mise run lint, format-check, test, typecheck, hooks all passed. Task remains In Progress because verified commits are not yet integrated into canonical main.
<!-- SECTION:NOTES:END -->
