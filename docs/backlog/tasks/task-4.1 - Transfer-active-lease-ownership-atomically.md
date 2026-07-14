---
id: TASK-4.1
title: Transfer active lease ownership atomically
status: In Progress
assignee:
  - '@codex-loop-pass-20260714-worklease'
created_date: '2026-07-14 02:34'
updated_date: '2026-07-14 18:11'
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation pass claimed acceptance-1: add the atomic active-owner transfer API and persistence path with focused lifecycle tests. Remaining acceptance criteria: #1-#5.

Implementation checkpoint (acceptance-1): commit b099d27 integrates the atomic TransferRequest/LeaseStore.transfer API, CLI transfer command, public export, version-1 schema entries, and focused lifecycle/CLI tests. The mutation updates one claim row inside one SQLite transaction, creates the successor epoch, preserves the checkpoint, advances revision, and returns the successor token only in the authorized response. Verification: mise run test (106 tests), mise run lint, mise run format-check, mise run typecheck, mise run hooks all passed. Next task: acceptance-2 successor token/revision/checkpoint guarantees and lifecycle/concurrency coverage. Remaining acceptance criteria: #2-#5.
<!-- SECTION:NOTES:END -->
