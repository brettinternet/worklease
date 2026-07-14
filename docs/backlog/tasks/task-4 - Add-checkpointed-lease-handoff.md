---
id: TASK-4
title: Add checkpointed lease handoff
status: To Do
assignee: []
created_date: '2026-07-14 02:06'
labels:
  - coordination
  - lease
dependencies: []
references:
  - src/worklease/models.py
  - src/worklease/store.py
  - src/worklease/execution.py
  - 'https://github.com/aetomala/worklease'
priority: high
type: feature
ordinal: 4000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Add an optional, bounded checkpoint to the lease lifecycle so long-running work can resume safely after clean handoff or lease expiry. The checkpoint is coordination metadata only: the caller and provider remain authoritative for real work state, and this feature must not claim provider-side fencing or exactly-once external effects. A checkpoint write should renew ownership atomically and advance the claim revision.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 An active owner can write or replace a bounded checkpoint while atomically renewing the lease; stale, expired, and wrong-token callers are rejected without changing the checkpoint.
- [ ] #2 A subsequent acquire receives the last checkpoint plus an explicit clean-handoff versus expired-recovery indication, while read-only output never exposes bearer tokens.
- [ ] #3 Checkpoint updates, clean release, lease expiry, re-acquisition, stale-owner rejection, size limits, and idempotent retry behavior are covered by automated tests.
- [ ] #4 The Python API, CLI JSON schema, and README/workflow documentation define checkpoint size, serialization, retention, and the unchanged local-coordination guarantee.
<!-- AC:END -->
