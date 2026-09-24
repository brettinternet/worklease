---
id: TASK-129
title: 'Work queue S3: source index, sync, and live claim overlay'
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-24 04:14'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-129.1
  - TASK-129.2
  - TASK-129.3
  - TASK-129.4
  - TASK-129.5
  - TASK-129.6
  - TASK-129.7
documentation:
  - docs/work-queue-tui-proposal.md
priority: high
type: feature
ordinal: 20000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
S2 enumerates sources into memory on every run and shows claim state from one-shot status reads. That breaks down at team scale (D21: 10,000 items per source, 50,000 per view, 25 clients). It also breaks down when agents call `queue query` in a loop, which plan section 15 names the dominant traffic multiplier. S3 adds the disposable per-user index with single-flight refresh (D19), a quota-aware scheduler, incremental provider sync, the live claim overlay (D20), and the measurements that validate or revise the section 14 budgets. See plan sections 14, 15, and 16 (S3).

This parent is an integration checklist, not an implementation lane. It depends on every child, so it becomes ready only after they are Done. Complete it by re-verifying the criteria below on main and recording the evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every S3 child task is Done
- [x] #2 Every plan section 14 acceptance budget is met, or revised in the plan with recorded measurements
- [x] #3 Pagination and cursor tests cover interrupted multi-page sync, changing sort order, a newest-page 304 while older issues changed, and permission loss. A partial scan never advances the committed watermark or proves deletion
- [x] #4 Injected-race tests show the claim overlay never misses a change
- [x] #5 The 25-client, 500-claim authority test has recorded results, and the plan section 17 question about namespace watch polling is answered
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Recheck every child Done and each parent criterion against main. 2. Run focused pagination, overlay, and benchmark checks; review §14 and §17 evidence. 3. Mark verified criteria and close parent through Backlog CLI, commit provider state, merge to main and clean owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Final integration on main at fafc904: TASK-129.1–129.7 all Done (Backlog list reread). Section 14 workload accounting and explicit met/revised budgets in docs/work-queue-tui-proposal.md and docs/queue-benchmark-results.md; 100k refresh failure boundary and measured scope stated. Targeted tests passed for interrupted/resumed GitHub incremental and reconciliation pages, moving cursor order, newest-page 304 with older change, permission loss/restoration, incomplete scan/no premature watermark or retirement, and local/remote claim overlay snapshot-watch injected races and restore/gap fail-closed. Section 14 records D22 25-client/500-claim authority benchmark: p99 renewal 2617 ms, minimum previous lease margin 512s; section 17 answers watch polling did not saturate at measured load. Full lint, format-check, test, typecheck, doc-test passed on parent worktree; task checklist has no new source implementation. No further general review required for provider-only closeout.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
S3 integrated on main: all seven children Done; section 14 budgets measured or revised, sync and overlay race tests reverified, and 25-client authority watch polling assessed. No new source changes for this integration checklist.
<!-- SECTION:FINAL_SUMMARY:END -->
