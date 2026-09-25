---
id: TASK-142.6
title: Claim and select Linear work through queue and MCP
status: To Do
assignee: []
created_date: '2026-09-25 16:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-142.3
  - TASK-142.4
  - TASK-142.5
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-142
priority: medium
type: feature
ordinal: 70000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Once the Linear read/identity and sync path proves eligibility, expose existing Worklease claim lifecycle on exactly the same resources used by the CLI. Provider assignment or status is advisory, not a lease.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Claim for me, D11 authority consistency, the identity gate, and remote admission under coordination: work for Linear sources
- [ ] #2 queue next --claim and MCP queue_next revalidate complete readiness and acquire only the stable Linear organization/issue resource; uncertain outcomes fail closed
<!-- AC:END -->
