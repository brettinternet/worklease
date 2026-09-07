---
id: TASK-59.1
title: Persist ownership epoch boundaries
status: To Do
assignee: []
created_date: '2026-09-07 15:00'
updated_date: '2026-09-07 15:25'
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
- [ ] #1 Each new singleton or bundle epoch persists its acquisition revision atomically with current ownership; legacy rows with no provable value remain null.
- [ ] #2 Singleton expiry replacement atomically records reason `expired`, effective time equal to the prior expiry, replacement record time, final state, and successor claim ID.
- [ ] #3 Singleton transfer and release atomically record reasons `transferred` and `released` with one captured transition time, final state, and the applicable successor and operation IDs.
- [ ] #4 Bundle release and expired-bundle cleanup record exactly one termination per retired member; partial-overlap replacement retires the full prior bundle and links the successor only to reacquired resources.
- [ ] #5 Termination rows contain no token, token hash, request, or receipt, while singleton release replay, bundle replay, and clean or expired checkpoint recovery retain current behavior.
- [ ] #6 `gc` uses termination `recorded_at`, protects associated retained history inside the window, and atomically deletes eligible termination and epoch history without changing dry-run behavior.
- [ ] #7 Schema migration is atomic, preserves every existing row, leaves unavailable historical fields null, and opens pre-migration databases without data loss.
- [ ] #8 Tests cover singleton transitions, exact and partial-overlap bundle replacement, bundle release, rollback atomicity, migration, garbage collection, and unchanged public status, list, inspection, recovery, and replay behavior; `docs/claim-model.md` documents the boundary model.
<!-- AC:END -->
