---
id: TASK-132
title: 'Work queue S6: focused provider writes and Start work'
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-25 22:55'
labels:
  - work-queue
  - reviewed
milestone: m-1
dependencies:
  - TASK-132.1
  - TASK-132.2
  - TASK-132.3
  - TASK-132.4
  - TASK-132.5
  - TASK-132.6
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
- [x] #1 Every S6 child task is Done
- [x] #2 Lost responses and partial failures are injected at each write boundary, and lagging read-back stays unresolved without re-dispatch
- [x] #3 Start work never writes after contention, never invents a status, and reports claim and transition outcomes separately
- [x] #4 A newly blocked owner can report the blocker without becoming eligible for further implementation
- [x] #5 A marker with the wrong content or duplicate matches is not a verified result
- [x] #6 Cancellation is refused once any write or guarded operation has started
- [x] #7 Checklist and body writes stay disabled, and assignment-only or progress-unsupported sources show unavailable actions with reasons instead of faking them
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reverify each S6 criterion against child evidence and executable tests on current main. 2. Run repository quality gates and review integration findings. 3. Record objective evidence, finalize the parent, and commit provider state.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Verified on main HEAD 49314b2: all six children Done (all 45 child criteria checked). Focused race tests: go test -race -count=3 -run Test(WritePipeline|WriteJournal|BacklogWrite|GitHubWrite|GitHubLost|QueueStartWork|QueueLifecycleCancel) ./internal/queue ./internal/cli; and go test -race -count=3 -run Test(QueueNextStart|MCPQueueNextStart|StartWorkPreviewAndSeparateOutcomes|StartWorkMissingMappingLeavesClaimOnly|ScriptedKeyboardAndDisabledActions) ./internal/cli ./internal/queueui. Tests cover crash/lost-response/lagging read-back with no redispatch, state/append marker provenance, contention and mapping, blocked-owner maintenance, no-effect cancellation, and unsupported provider writes. mise run lint, format-check, test, typecheck passed. General integration review found no item-scoped defects.

Post-completion review: criterion #3 outcome reporting corrected via TASK-132.6 fix (6800074). Other criteria re-checked; no follow-up.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
S6 integrated: six children Done; provider write recovery, Start work, blocked-owner maintenance, append provenance, cancellation, and unsupported actions verified with focused race tests and repository quality gates.
<!-- SECTION:FINAL_SUMMARY:END -->
