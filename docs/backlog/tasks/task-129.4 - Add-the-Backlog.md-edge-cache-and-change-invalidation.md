---
id: TASK-129.4
title: Add the Backlog.md edge cache and change invalidation
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
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
- [ ] #1 Edge observations are partitioned by checkout and configuration generation. Metadata tuples (task ID, updatedAt, path, mtime, size) only trigger invalidation and never serve as proof of freshness; prerequisite state is observed separately from the dependent's file
- [ ] #2 Watch loss, a branch or HEAD change, or uncertain invalidation forces re-reads of affected observations. A test changes a prerequisite without touching the dependent's file and shows the dependent is re-evaluated
- [ ] #3 An explicit action (claim, next, or provider write) refreshes the relevant closure from the provider before it proceeds, regardless of cache state
- [ ] #4 Background edge hydration runs through the scheduler in priority order: selected closure, visible rows, then background
- [ ] #5 The view reports edge coverage (for example, `edges 812/10000`). Evidenced blockers are preserved, candidates with incomplete closures show unknown, and next-ready stays unavailable until the current required graph is complete
- [ ] #6 The invalidation mechanism is chosen from measurements at 10,000 tasks and recorded in plan section 14. It recovers from overflow or stream failure by re-listing, and it runs periodic reconciliation
- [ ] #7 If `task list --json` includes dependencies (see TASK-127), the adapter uses them and skips per-task views, with tests for both paths
- [ ] #8 Tests cover invalidation on a same-minute file change, a changed prerequisite with an unchanged dependent file, branch switch, stream or watch failure recovery, and partial coverage with and without a known blocker
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
