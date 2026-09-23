---
id: TASK-126
title: 'Work queue S1: contracts, identity vectors, and provider probes'
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-126.1
  - TASK-126.2
  - TASK-126.3
  - TASK-126.4
  - TASK-126.5
  - TASK-126.6
  - TASK-126.7
documentation:
  - docs/work-queue-tui-proposal.md
priority: high
type: task
ordinal: 1000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Every later work-queue slice builds on three foundations that are not settled yet. The first is contract semantics for capabilities, coverage, and freshness. The second is claim resources that are byte-identical between queue and CLI callers. The third is provider behavior the plan could not verify offline: GitHub sync behavior, Backlog.md cost at 10,000 tasks, and Git side effects. A mistake in any of them would be built into every adapter. See plan sections 2, 3, 6, 7, and 16 (S1).

The upstream Backlog.md bulk-dependency request (TASK-127) runs in parallel and does not gate S1.

This parent is an integration checklist, not an implementation lane. It depends on every child, so it becomes ready only after they are Done. Complete it by re-verifying the criteria below on main and recording the evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every S1 child task is Done, with its evidence recorded in the child task
- [ ] #2 Dependency fixtures distinguish hard edges from hierarchy, cross-source references, cycles, partial graphs with known blockers, and unsupported completion conditions
- [ ] #3 Plan section 3 contains the S1 probe results. Any decision they change is updated in section 2 and in the dependent plan sections
- [ ] #4 contract.md, source-provider-contract.md, and the Backlog doc `doc-1 - Worklease-Workflow` agree with the plan and with each other
- [ ] #5 The TASK-126.4 identity vectors run in `mise run test`
<!-- AC:END -->
