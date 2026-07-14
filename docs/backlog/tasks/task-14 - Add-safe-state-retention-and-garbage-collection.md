---
id: TASK-14
title: Add safe state retention and garbage collection
status: To Do
assignee: []
created_date: '2026-07-14 02:34'
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
