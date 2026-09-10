---
id: TASK-59.1
title: Persist ownership epoch boundaries
status: Done
assignee:
  - '@brett'
created_date: '2026-09-07 15:00'
updated_date: '2026-09-07 16:36'
labels:
  - storage
dependencies: []
references:
  - src/worklease/acquisition.py
  - src/worklease/lifecycle.py
  - src/worklease/sqlite.py
  - src/worklease/garbage_collection.py
  - src/worklease/operations.py
  - tests/test_store.py
  - tests/test_gc.py
  - docs/claim-model.md
modified_files:
  - docs/claim-model.md
  - src/worklease/acquisition.py
  - src/worklease/claims.py
  - src/worklease/garbage_collection.py
  - src/worklease/lifecycle.py
  - src/worklease/sqlite.py
  - tests/test_gc.py
  - tests/test_store.py
parent_task_id: TASK-59
priority: medium
type: enhancement
ordinal: 61000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Persist truthful start ordering and a uniform terminal snapshot for ownership transitions recorded after migration.

Add a nullable acquisition revision to singleton and bundle epoch rows. New acquisitions and transfers populate it from the revision assigned to that epoch so ownership order does not depend on wall-clock timestamps. Older rows remain nullable and are reported as legacy-incomplete when their order or ending cannot be reconstructed.

Add a dedicated token-free `epoch_terminations` projection keyed by `(resource, claim_id)`. Keep `releases` unchanged as singleton release replay and clean-handoff state; it is not the canonical history model. Each termination stores reason (`released`, `transferred`, or `expired`), `effective_at`, `recorded_at`, final revision, last heartbeat time, expiry time, nullable canonical checkpoint, nullable successor claim ID, and nullable operation ID. It stores no token, token hash, request, or receipt.

For release and transfer, `effective_at` and `recorded_at` use one authority-clock value captured by the transaction. For expired replacement, `effective_at` is the prior `expires_at` and `recorded_at` is the replacement transaction time; `gc` uses `recorded_at` for retention. Merely reading an expired current claim does not write a termination.

Singleton and bundle ownership changes write the active-state mutation and every affected termination in the same transaction. Replacing any member of an expired bundle retires the complete prior bundle: record one termination for every old member, set the successor claim only on resources acquired by that successor, and leave it null on other retired members.

Increment and migrate the SQLite schema without fabricating unavailable history. Preserve existing epoch, operation, release, reconciliation, checkpoint-recovery, and replay data. Populate new fields only when the stored rows prove their value; otherwise leave them null for the history projection to identify as legacy-incomplete.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Each new singleton or bundle epoch persists its acquisition revision atomically with current ownership; legacy rows with no provable value remain null.
- [x] #2 Singleton expiry replacement atomically records reason `expired`, effective time equal to the prior expiry, replacement record time, final state, and successor claim ID.
- [x] #3 Singleton transfer and release atomically record reasons `transferred` and `released` with one captured transition time, final state, and the applicable successor and operation IDs.
- [x] #4 Bundle release and expired-bundle cleanup record exactly one termination per retired member; partial-overlap replacement retires the full prior bundle and links the successor only to reacquired resources.
- [x] #5 Termination rows contain no token, token hash, request, or receipt, while singleton release replay, bundle replay, and clean or expired checkpoint recovery retain current behavior.
- [x] #6 `gc` uses termination `recorded_at`, protects associated retained history inside the window, and atomically deletes eligible termination and epoch history without changing dry-run behavior.
- [x] #7 Schema migration is atomic, preserves every existing row, leaves unavailable historical fields null, and opens pre-migration databases without data loss.
- [x] #8 Tests cover singleton transitions, exact and partial-overlap bundle replacement, bundle release, rollback atomicity, migration, garbage collection, and unchanged public status, list, inspection, recovery, and replay behavior; `docs/claim-model.md` documents the boundary model.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Migrate SQLite to schema v3 atomically, adding nullable acquisition revisions and token-free epoch terminations without backfilling unprovable legacy values.
2. Record singleton and bundle acquisition revisions plus release, transfer, and expired-replacement terminations inside existing ownership transactions.
3. Make GC retention termination-aware while preserving dry-run semantics and atomic epoch/history deletion.
4. Add transition, rollback, migration, GC, regression, and redaction tests; document the boundary model.
5. Run focused tests, full repository quality gates, adversarial review, then commit, merge to main, finalize TASK-59.1, and clean the worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Selected as the earliest dependency-ready item; TASK-59.2 depends on it. Acquired the item-scoped local coordination lease and created HWT workspace w57 on branch task-59.1-epoch-boundaries.

Implemented schema v3 acquisition revisions and token-free epoch terminations for singleton and bundle expiry replacement, transfer, and release. GC now retains associated history through termination recorded_at and removes termination plus epoch atomically. Added migration, rollback, exact/partial bundle, lifecycle, retention, redaction, and boundary documentation coverage. Full test suite passed (261 core + 19 SDK); lint, format-check, and typecheck passed.

Post-merge validation on main passed: mise run lint, mise run format-check, mise run test (261 core + 19 SDK), and mise run typecheck. Independent adversarial review reported no actionable findings.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Persisted truthful acquisition ordering and token-free terminal snapshots for singleton and bundle ownership epochs, including release, transfer, expiry replacement, partial-overlap bundle retirement, and termination-aware garbage collection. Schema v3 migration is atomic and leaves unprovable legacy history null. Verified by lifecycle, migration, rollback, redaction, GC, and regression tests; all repository quality gates passed after merge, and independent review found no defects.
<!-- SECTION:FINAL_SUMMARY:END -->
