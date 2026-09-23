---
id: TASK-131
title: 'Work queue S5: launch actions'
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-131.1
  - TASK-131.2
documentation:
  - docs/work-queue-tui-proposal.md
priority: high
type: feature
ordinal: 33000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Users want to start an agent or script on a selected item from the queue. D18 and plan section 12 define launch actions: user-configured argv templates with item references, run in an allowlisted environment that never carries credentials or item content. The launched worker acquires its own claim. The queue neither supervises nor retries it. See plan sections 10, 12, 15, and 16 (S5).

This parent is an integration checklist, not an implementation lane. It depends on every child, so it becomes ready only after they are Done. Complete it by re-verifying the criteria below on main and recording the evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every S5 child task is Done
- [ ] #2 With secret variables set in the queue's environment, none reach the child unless named in `passEnv`
- [ ] #3 Hostile titles and option-like IDs cannot inject arguments
- [ ] #4 Claim then Launch, and launch with an unresolved cwd, are refused with explanations
- [ ] #5 A tested launcher consumes the handoff, and the worker verifies the same authority and exact resources, including for portable Backlog.md bindings
<!-- AC:END -->
