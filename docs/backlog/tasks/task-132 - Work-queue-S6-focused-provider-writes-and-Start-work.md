---
id: TASK-132
title: 'Work queue S6: focused provider writes and Start work'
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-132.1
  - TASK-132.2
  - TASK-132.3
  - TASK-132.4
  - TASK-132.5
documentation:
  - docs/work-queue-tui-proposal.md
priority: high
type: feature
ordinal: 36000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The queue helps with doing work, not only finding it, once a worker can make the small coordinating updates without leaving it: enter In Progress, report Blocked, request review, mark complete, assign, or record progress (D26). Neither initial provider supports conditional writes, so every write is coordination-only. Every write goes through intent, dispatch, receipt, and read-back, and only then a checkpoint (D7), and a lost response must never cause a second write. Start work is an explicit composition of claim plus transition, never a new claim primitive. Full body editing, arbitrary fields, checklist writes, and bulk or dependency editing are out of scope. See plan sections 8, 10, and 16 (S6), and D5, D6, D7, and D26.

This parent is an integration checklist, not an implementation lane. It depends on every child, so it becomes ready only after they are Done. Complete it by re-verifying the criteria below on main and recording the evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every S6 child task is Done
- [ ] #2 Lost responses and partial failures are injected at each write boundary, and lagging read-back stays unresolved without re-dispatch
- [ ] #3 Start work never writes after contention, never invents a status, and reports claim and transition outcomes separately
- [ ] #4 A newly blocked owner can report the blocker without becoming eligible for further implementation
- [ ] #5 A marker with the wrong content or duplicate matches is not a verified result
- [ ] #6 Cancellation is refused once any write or guarded operation has started
- [ ] #7 Checklist and body writes stay disabled, and assignment-only or progress-unsupported sources show unavailable actions with reasons instead of faking them
<!-- AC:END -->
