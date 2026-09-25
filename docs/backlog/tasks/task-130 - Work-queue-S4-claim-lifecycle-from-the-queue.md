---
id: TASK-130
title: 'Work queue S4: claim lifecycle from the queue'
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-25 14:53'
labels:
  - work-queue
  - reviewed
milestone: m-1
dependencies:
  - TASK-130.1
  - TASK-130.2
  - TASK-130.3
  - TASK-130.4
  - TASK-130.5
  - TASK-130.6
documentation:
  - docs/work-queue-tui-proposal.md
priority: high
type: feature
ordinal: 28000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Humans need to claim work directly from the queue, and agents need a deterministic next-ready selector that does not screen-scrape. The queue then becomes a Worklease client that owns claims. It must use the same handle, session, heartbeat, admission, and recovery rules as the CLI, with no weaker path. See plan sections 5, 6, 9 ("Who renews a claim?"), 13, and 16 (S4), and D5, D11, D12, D23, D24, and D25.

This parent is an integration checklist, not an implementation lane. It depends on every child, so it becomes ready only after they are Done. Complete it by re-verifying the criteria below on main and recording the evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every S4 child task is Done
- [x] #2 A remote authority outage never changes the authority a view uses
- [x] #3 The queue and the CLI contend on byte-identical resources, as shown by an end-to-end contention test in both directions
- [x] #4 The renewal margin holds under the S3 load scenarios
- [x] #5 Native occupancy is never rendered as global exclusion
- [x] #6 `queue next` explains the dependency reasons behind every candidate and every no-work result
- [x] #7 Concurrent agent loops using `queue next --claim` through the CLI or MCP claim distinct items with no wasted selection, as shown by the TASK-130.5 and TASK-130.6 concurrency tests on main
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reverify every S4 integration criterion and child status against main. 2. Run the required repository gates in an isolated worktree. 3. Record per-criterion evidence, finalize, and integrate the provider-state commit.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
On main 8afe10f, all six TASK-130 children are Done. Worktree task-130-s4-integration: focused go test ./internal/cli ./internal/queue ./internal/queueui (selected S4 tests, including local/remote queue-vs-CLI contention in both directions, remote outage fail-closed, native claim not-exposed, no-work dependency reasons, 8-worker local/remote CLI and mixed MCP contention) passed. QUEUE_LIFECYCLE_COMBINED=1 measured prior-lease margin 19.226606s >= 7.5s with 50k dependency graph and 10k indexed rows; stalled-verification renewal test passed. mise run lint, format-check, test, typecheck all passed.

Integration commit on main: 1df4d82. Single general review of the integration checklist and test coverage found no item-scoped defects; no code changes needed. Next resumable step: select a ready S5 or S6 child task (TASK-131.1 or TASK-132.1).

Post-completion review: re-ran S4 CLI/MCP queue next and queue start tests on main; child review fixes merged in 71f5f54. No follow-up needed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Closed S4 integration checklist after re-verifying all seven criteria on main; focused contention/renewal tests and all repository code gates passed.
<!-- SECTION:FINAL_SUMMARY:END -->
