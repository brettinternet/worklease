---
id: TASK-129.4
title: Add the Backlog.md edge cache and change invalidation
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 21:33'
labels:
  - work-queue
  - backlog-md
milestone: m-1
dependencies:
  - TASK-129.1
  - TASK-129.2
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-129
priority: high
type: feature
ordinal: 24000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Next-ready over a source needs its complete edge set (D23), but Backlog.md exposes edges only through one `task view --json` per task (plan section 3). Plan section 14 ("Backlog.md loading") specifies an observation cache partitioned by checkout and configuration generation. Task ID, updatedAt (minute resolution), path, mtime, and size are invalidation hints, not content versions: an unchanged task file does not mean its prerequisites are unchanged. Prerequisite observations are tracked separately, and watch loss, branch or HEAD changes, or uncertain invalidation force provider re-reads. Background hydration goes through the TASK-129.2 scheduler, with the selected item's closure first, then visible rows, then everything else.

Invalidation is a choice made from measurements: the long-lived `task list --json --watch` stream, or a debounced filesystem watch followed by a re-list. Use the TASK-126.6 results and decide at 10,000 tasks. If the upstream bulk-dependency field has shipped, list output becomes the primary edge source and this cache the fallback for older versions.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Edge observations are partitioned by checkout and configuration generation. Metadata tuples (task ID, updatedAt, path, mtime, size) only trigger invalidation and never serve as proof of freshness; prerequisite state is observed separately from the dependent's file
- [x] #2 Watch loss, a branch or HEAD change, or uncertain invalidation forces re-reads of affected observations. A test changes a prerequisite without touching the dependent's file and shows the dependent is re-evaluated
- [x] #3 An explicit action (claim, next, or provider write) refreshes the relevant closure from the provider before it proceeds, regardless of cache state
- [x] #4 Background edge hydration runs through the scheduler in priority order: selected closure, visible rows, then background
- [x] #5 The view reports edge coverage (for example, `edges 812/10000`). Evidenced blockers are preserved, candidates with incomplete closures show unknown, and next-ready stays unavailable until the current required graph is complete
- [x] #6 The invalidation mechanism is chosen from measurements at 10,000 tasks and recorded in plan section 14. It recovers from overflow or stream failure by re-listing, and it runs periodic reconciliation
- [x] #7 If `task list --json` includes dependencies (see TASK-127), the adapter uses them and skips per-task views, with tests for both paths
- [x] #8 Tests cover invalidation on a same-minute file change, a changed prerequisite with an unchanged dependent file, branch switch, stream or watch failure recovery, and partial coverage with and without a known blocker
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend the Backlog adapter with partitioned edge observations and bulk-list dependency support; keep prerequisite state independent and invalidate on metadata, branch/HEAD, and lost observation. 2. Integrate prioritized bounded hydration and edge coverage in queue snapshots, with authoritative closure refresh for future actions. 3. Add watch/reconciliation policy from 10k measurements, tests for same-minute/prerequisite/branch/failure/partial graph, and update proposal section 14. 4. Run focused and repository quality gates, review once, commit and integrate when main is safe, then finalize.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented partitioned Backlog edge observations and bulk-list dependency path, authoritative non-coalesced action closure reads, priority hydration, filesystem invalidation with one-minute reconciliation, and coverage in snapshots/TUI. Tests: TestBacklogEdgeInvalidationAndIndependentPrerequisite, TestInFlightBacklogViewCannotRestoreInvalidatedEdges, TestBacklogActionClosureRereadsEveryPrerequisite, TestEdgeHydrationOrdersSelectedClosureBeforeVisibleAndBackground, TestSelectedClosureTraversesKnownEdgeToUnknownPrerequisite, TestBacklogBulkEdgesAndPartialCoverage, TestBacklogFilesystemWatchInvalidatesSameMinuteEdit, TestBacklogPeriodicReconciliationRelistsWithoutEvents, plus queue UI and full suites. One independent review found six item-scoped defects; all fixed and affected checks rerun. Worktree commits 46a32e0 and 920a5ad fast-forwarded to main; lint, format-check, test, typecheck, staged hooks passed. Default-parallel full test sporadically failed pre-existing queueindex cross-process single-flight helper under load; focused isolation passed and full suite passed with GOMAXPROCS=2, with no disabled tests.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added Backlog edge cache, invalidation/reconciliation, bulk-edge fallback, and prioritized hydration; verified targeted coverage, race checks and full quality gates. Integrated 46a32e0 and 920a5ad into main.
<!-- SECTION:FINAL_SUMMARY:END -->
