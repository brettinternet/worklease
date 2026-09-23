---
id: TASK-129.3
title: Add incremental GitHub sync and reconciliation
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
  - github
milestone: m-1
dependencies:
  - TASK-129.1
  - TASK-129.2
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-129
priority: high
type: feature
ordinal: 23000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-128.5 enumerates a whole repository on every refresh. Plan section 14 ("GitHub loading") replaces that with incremental sync into the TASK-129.1 index, and GitHub's own guidance makes it subtle. Updated-order pages move records between pages. A 304 on the newest issue proves nothing about older issues, edges, or permissions. A 404 can mean lost permission rather than deletion. The design: `since` filtering with an overlap window and a fixed scan-start watermark; each page persisted atomically with its resume cursor; the committed watermark advanced only after the whole window is scanned; the conditional GET used only as an optional hint; periodic bounded reconciliation for membership and visibility.

Use the TASK-126.5 results. If dependency edits do not change updatedAt, edge freshness relies on reconciliation and on refreshing the closure before any action, and the view must label edges accordingly.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Incremental sync uses the since filter with an overlap window and a fixed scan-start watermark, deduplicates by node ID, and persists each page with its resume cursor in one index transaction. The committed watermark advances only after the complete window is scanned, so a crash or partial newest-first page never skips older changes
- [ ] #2 A conditional GET is keyed to its exact origin, principal, query, page, and representation. A 304 is only a hint and never suppresses scheduled incremental sync or reconciliation, as tested by changing an older issue while the newest page returns 304
- [ ] #3 Reconciliation runs as bounded, resumable scan generations. A completed generation may retire rows from the accessible projection but never records deletion. Absent items are classified as deleted, moved, inaccessible, or unknown only as the evidence allows, stale content is withheld, and identity and recovery records are kept. No partial scan retires rows
- [ ] #4 Visible rows are hydrated through batched `nodes(ids:)` queries sized by the TASK-126.5 limit, and comments load lazily
- [ ] #5 Edge freshness is labeled according to the TASK-126.5 finding on whether dependency edits change updatedAt
- [ ] #6 Tests against the fake GitHub cover interrupted multi-page sync, an issue edited between pages, overlap deduplication, a newest-page 304 with older changes, permission loss returning 404 for an existing issue, transfers, and expired cursors
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
