---
id: TASK-143.3
title: Add a write-capable reference adapter covering receipts and recovery
status: Done
assignee: []
created_date: '2026-09-25 16:38'
updated_date: '2026-09-25 20:53'
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
- [x] #1 A reference adapter, runnable without network access against a local fixture store, implements a configured state transition, an appended progress comment carrying the D7 operation marker, and assign-to-me, each returning a provider receipt with honest `conditionalWrite` and `fencingEvidence` values.
- [x] #2 `readReceipt` recovers after a lost response using the journaled intent and marker, returns `unknown` when it cannot distinguish its own write from another actor's, and never redispatches.
- [x] #3 The reference adapter passes `worklease queue adapter check` including mutation checks, and host tests drive it through claim, write, lost-response, and recovery flows.
- [x] #4 The authoring guide walks through the write methods and states which guarantees the example does not provide.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Inspect protocol, sample adapter, conformance runner and host integration tests.
2. Implement a local-fixture write adapter with truthful receipts and journal-backed readReceipt; add focused host tests.
3. Document write/recovery walkthrough, run conformance and project gates, review once; commit, merge and clean up.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented fixture-backed reference adapter with writeState, recordProgress and assign; host pipeline test verifies applied-but-lost response recovers from journal with one dispatch. Independent review identified four concrete fixture correctness issues (custom completion, rejected rebind, symlink store, post-write small-budget error); corrected all and reran focused race tests. Full lint/format/test/typecheck passed. Built shipped binaries and queue adapter check --disposable-target reference-1 returned pass including mutation-receipt, lost-response and unknown-outcome.

Evidence: TestReferenceAdapterWritesReceiptsAndRecoversMarkedAppend and TestReferenceAdapterThroughHostWritesConfiguredActions cover all write receipts and marker; TestReferenceAdapterReceiptUnknownWhenMarkerAttributionIsAmbiguous and TestReferenceAdapterLostResponseRecoversWithClaimAndNoRedispatch prove unknown and exactly-once recovery; built CLI adapter check verdict pass with disposable reference-1, including mutation-receipt/lost-response/unknown-outcome. Focused race count=3 passed on merged main. Authoring guide documents all methods and explicit lack of fencing/conditional provider writes. Commit 7d7ab8e1dbe086c5cd24f3382f606b6423755867 merged to main as 91378f1. Review: four item-scoped findings fixed and tests added; no remaining task blockers. Next: TASK-143.4 is ready; TASK-143 parent still depends on remaining subtasks.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Delivered fixture-backed writable external reference adapter and recovery guide; conformance mutation probes, host claim/recovery tests, focused race checks and repository gates passed. Merged to main (91378f1).
<!-- SECTION:FINAL_SUMMARY:END -->
