---
id: TASK-132.4
title: Add mutation previews and the recovery view
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-24 18:14'
labels:
  - work-queue
  - tui
milestone: m-1
dependencies:
  - TASK-132.1
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-132
priority: high
type: feature
ordinal: 40000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Every user-initiated claim or provider action opens a preview first (plan section 13); heartbeats follow the already authorized session policy without repeated prompts. The preview shows the authority, resources, exact provider effect, side effects (such as Git commits or watcher notifications), declared races, the disclosed operation marker, and limits. Unresolved writes need a home: the Recovery view and tab in the TUI, with matching JSON, so no partial-success toast can hide a held claim or an uncertain write.

Apart from verification, operator reconciliation is the only way out of `unknown`. It requires the operator to record evidence that the write did not commit and that no executor can still perform it (plan section 8).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Change state (`s`), record progress (`p`), and assign to me (`a`) each open a preview listing the exact provider effect, side effects, declared races, the marker, the claim they run under, and what happens if the response is lost. Nothing is dispatched before confirmation
- [ ] #2 The Recovery view lists every unresolved or unknown operation across sources with its intent, dispatch time, last read-back result, and allowed next steps, and the detail pane's Recovery tab shows the operations for that item
- [ ] #3 A JSON command documented in docs/queue.md lists recovery records with the same fields as the TUI
- [ ] #4 Retry read-back is always available. Operator reconciliation requires typed evidence text, is recorded in the journal with the operator identity, and is never offered while the operation could still be executing
- [ ] #5 No toast or status line reports success for an unverified write or hides a held claim, as tested with injected failures
- [ ] #6 Tests cover the previews for each operation and every recovery state transition
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Wire journal-backed provider write previews to the queue TUI, preserving claim ownership and requiring confirmation. 2. Expose unresolved records in TUI Recovery view/detail and JSON CLI; add bounded read-back and evidenced operator reconciliation with identity and execution-safety checks. 3. Exercise preview/recovery failure transitions, update docs, run required gates, commit and integrate.
<!-- SECTION:PLAN:END -->
