---
id: TASK-142.7
title: Add recoverable focused Linear writes and Start work
status: To Do
assignee: []
created_date: '2026-09-25 16:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-142.1
  - TASK-142.2
  - TASK-142.3
  - TASK-142.4
  - TASK-142.5
  - TASK-142.6
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-142
priority: medium
type: feature
ordinal: 71000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Linear provider mutations are separate from Worklease claims. Ship only after probe, read, sync, and claim behavior is established; never infer write safety from a displayed workflow state.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Configured state transitions including Start work, comments carrying a verifiable operation marker, and assign-to-me follow §8 intent/dispatch/receipt/read-back/checkpoint recovery
- [ ] #2 Verify viewer against configured account before every write and on credential changes; a different current assignee requires explicit confirmation of single-assignee replacement
- [ ] #3 Lost responses and lagging read-back never cause an unsafe redispatch; tests cover principal mismatch, partial effects and ambiguous markers
<!-- AC:END -->
