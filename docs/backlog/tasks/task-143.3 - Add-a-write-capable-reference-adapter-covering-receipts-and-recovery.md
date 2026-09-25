---
id: TASK-143.3
title: Add a write-capable reference adapter covering receipts and recovery
status: To Do
assignee: []
created_date: '2026-09-25 16:38'
labels:
  - work-queue
  - external-adapter
milestone: m-1
dependencies:
  - TASK-143.1
documentation:
  - docs/external-adapter-protocol.md
  - skills/worklease-workflow/references/external-adapter-authoring.md
parent_task_id: TASK-143
priority: medium
type: feature
ordinal: 75000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The only example (`cmd/worklease-sample-adapter`) is read-only. Writes are where adapter authors are most likely to break Worklease guarantees: treating HTTP success as proof, replaying a non-idempotent append after a lost response, or claiming conditional writes and fencing they do not have. Authors need a working example of the write half of the protocol.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A reference adapter, runnable without network access against a local fixture store, implements a configured state transition, an appended progress comment carrying the D7 operation marker, and assign-to-me, each returning a provider receipt with honest `conditionalWrite` and `fencingEvidence` values.
- [ ] #2 `readReceipt` recovers after a lost response using the journaled intent and marker, returns `unknown` when it cannot distinguish its own write from another actor's, and never redispatches.
- [ ] #3 The reference adapter passes `worklease queue adapter check` including mutation checks, and host tests drive it through claim, write, lost-response, and recovery flows.
- [ ] #4 The authoring guide walks through the write methods and states which guarantees the example does not provide.
<!-- AC:END -->
