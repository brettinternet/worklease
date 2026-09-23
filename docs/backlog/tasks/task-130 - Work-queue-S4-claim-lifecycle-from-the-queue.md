---
id: TASK-130
title: 'Work queue S4: claim lifecycle from the queue'
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-130.1
  - TASK-130.2
  - TASK-130.3
  - TASK-130.4
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
- [ ] #1 Every S4 child task is Done
- [ ] #2 A remote authority outage never changes the authority a view uses
- [ ] #3 The queue and the CLI contend on byte-identical resources, as shown by an end-to-end contention test in both directions
- [ ] #4 The renewal margin holds under the S3 load scenarios
- [ ] #5 Native occupancy is never rendered as global exclusion
- [ ] #6 `queue next` explains the dependency reasons behind every candidate and every no-work result
<!-- AC:END -->
