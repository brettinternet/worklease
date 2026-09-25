---
id: TASK-142.4
title: Read Linear sources and typed dependencies in the queue
status: To Do
assignee: []
created_date: '2026-09-25 16:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-142.1
  - TASK-142.2
  - TASK-142.3
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-142
priority: medium
type: feature
ordinal: 68000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Add a built-in Linear Go adapter using GraphQL HTTPS, adapting §7 after probe evidence rather than assuming provider semantics. A source names organization, team, optional project filter, and account.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 resolve, capabilities, list, readItems, and readDependencies use bounded pagination and report exact coverage and principal identity
- [ ] #2 Blocking relationships become hard edges with typed provenance; related, duplicate, similar, and parent relationships remain informational unless the probe supports another decision
- [ ] #3 Raw workflow state remains beside normalized state; unknown capability never permits action; shared adapter conformance passes
<!-- AC:END -->
