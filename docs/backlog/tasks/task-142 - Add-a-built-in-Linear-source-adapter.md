---
id: TASK-142
title: Add a built-in Linear source adapter
status: To Do
assignee: []
created_date: '2026-09-25 16:30'
updated_date: '2026-09-25 16:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-142.1
  - TASK-142.2
  - TASK-142.3
  - TASK-142.4
  - TASK-142.5
  - TASK-142.6
  - TASK-142.7
documentation:
  - docs/work-queue-tui-proposal.md
  - skills/worklease-workflow/references/source-providers/linear.md
priority: medium
type: feature
ordinal: 64000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The user requested Linear support on 2026-09-25 for a frequently used environment. Worklease queue currently cannot show its issues, dependency readiness, or claims. D29 accepts a built-in Go adapter with a user-configured credential helper and stable organization/issue identity. This is an integration checklist; implement through ordered subtasks after probing a dedicated test team. Do not infer unprobed API behavior or enable unsafe writes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The Linear API probe is recorded in plan §3 and any contradicted declarations are revised
- [ ] #2 Credential helper and Linear identity vectors pin safe principal and claim-key behavior
- [ ] #3 Read, sync, and dependency behavior pass the shared conformance suite with complete-graph and partial-scan safeguards
- [ ] #4 Claim for me, D11, the identity gate, queue next --claim, and MCP queue_next work on Linear sources
- [ ] #5 Focused writes follow §8 recovery/read-back, and Linear configuration and limits are documented
- [ ] #6 All Linear child tasks are Done and acceptance is reverified on main
<!-- AC:END -->
