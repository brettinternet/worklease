---
id: TASK-4.1
title: Transfer active lease ownership atomically
status: To Do
assignee: []
created_date: '2026-07-14 02:34'
labels:
  - coordination
  - lease
  - handoff
dependencies:
  - TASK-4
references:
  - src/worklease/store.py
  - src/worklease/models.py
parent_task_id: TASK-4
priority: high
type: feature
ordinal: 15000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Extend TASK-4 with a true active-owner transfer that does not release the resource between owners. This is distinct from checkpointed release, expiry, and later acquisition: the current owner authorizes one atomic transition to a fresh successor ownership epoch.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The current active owner can atomically transfer one exact resource to supplied successor agent, session, owner, claim, and work identities using the current claim ID, token, and revision.
- [ ] #2 A successful transfer creates a fresh random bearer token and higher revision, invalidates the prior token immediately, preserves the latest TASK-4 bounded checkpoint, and never exposes a free or dual-owner interval.
- [ ] #3 Transfer requests and receipts are idempotent; replay returns the same successor result, while changed successor data, stale revisions, expired claims, wrong tokens, and reused claim IDs are rejected without changing ownership.
- [ ] #4 Read-only status and diagnostics show non-secret handoff metadata without exposing either credential, and only the authorized transfer response returns the successor token.
- [ ] #5 Automated concurrency and crash tests prove no contender can acquire during transfer, no stale owner can heartbeat/execute/release afterward, and interruption leaves exactly one valid ownership epoch.
<!-- AC:END -->
