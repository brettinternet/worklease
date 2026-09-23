---
id: TASK-129
title: 'Work queue S3: source index, sync, and live claim overlay'
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
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
- [ ] #1 Every S3 child task is Done
- [ ] #2 Every plan section 14 acceptance budget is met, or revised in the plan with recorded measurements
- [ ] #3 Pagination and cursor tests cover interrupted multi-page sync, changing sort order, a newest-page 304 while older issues changed, and permission loss. A partial scan never advances the committed watermark or proves deletion
- [ ] #4 Injected-race tests show the claim overlay never misses a change
- [ ] #5 The 25-client, 500-claim authority test has recorded results, and the plan section 17 question about namespace watch polling is answered
<!-- AC:END -->
