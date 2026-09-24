---
id: TASK-132.4
title: Add mutation previews and the recovery view
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-24 20:28'
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
- [x] #1 Change state (`s`), record progress (`p`), and assign to me (`a`) each open a preview listing the exact provider effect, side effects, declared races, the marker, the claim they run under, and what happens if the response is lost. Nothing is dispatched before confirmation
- [x] #2 The Recovery view lists every unresolved or unknown operation across sources with its intent, dispatch time, last read-back result, and allowed next steps, and the detail pane's Recovery tab shows the operations for that item
- [x] #3 A JSON command documented in docs/queue.md lists recovery records with the same fields as the TUI
- [x] #4 Retry read-back is always available. Operator reconciliation requires typed evidence text, is recorded in the journal with the operator identity, and is never offered while the operation could still be executing
- [x] #5 No toast or status line reports success for an unverified write or hides a held claim, as tested with injected failures
- [x] #6 Tests cover the previews for each operation and every recovery state transition
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend the existing provider-write controller with gated previews and claim verification for state, progress, and assignment actions. 2. Project durable journal records into the TUI Recovery view and item tab and a JSON recovery CLI, with typed operator reconciliation and retained held-claim errors. 3. Verify exact local/remote checkpoint identity and repair private handles from completed operations after the replay deadline. 4. Cover failure paths and previews in focused tests, run quality gates, and update queue docs and proposal.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation and focused/race checks are passing. Review findings led to canonical checkpoint request hashing (including remote actor identity), committed-operation verification after replay expiry, JSON/plain held-claim errors, and a distinct built-in Recovery view identity.

Verified: three-action Backlog preview and gated dispatch in TestQueueWriteControllerPreviewsAndVerifiesBacklogMutation; consent content in TestProviderWritePreviewDisplaysConsentBoundary; Recovery view/item parity and configured-name collision in queueui tests; JSON recovery and held-claim failures in queue_command tests; all journal transition/reconciliation/crash/read-back cases in queue/write_test.go; local/remote checkpoint identity, expired-replay recovery, and handle repair in queue_write_claim_test.go. go test -race -count=3 passed for changed CLI, queue, and queueui tests. mise run lint, format-check, test, typecheck, hooks all passed; docs/queue.md and docs/work-queue-tui-proposal.md updated.

Implementation commit: 309dcd8 (local task-132-4-recovery branch; not pushed). Review outcome: one general pass identified and resolved exact-checkpoint identity, failed read-back held-claim reporting, and Recovery view name collision; focused checks repeated after fixes. No remaining blocker.

Integrated the completed task branch into main at 7f863c3 after preserving the earlier primary-checkout plan at 23baa11. Focused race tests (count=3) and lint, format-check, test, typecheck, and staged hooks passed against the merged tree. No push requested.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added claim-gated provider mutation previews and durable cross-source Recovery UI/JSON with safe read-back, evidence-only reconciliation, and exact local/remote checkpoint recovery. Verified with focused race tests and all five project quality gates.
<!-- SECTION:FINAL_SUMMARY:END -->
