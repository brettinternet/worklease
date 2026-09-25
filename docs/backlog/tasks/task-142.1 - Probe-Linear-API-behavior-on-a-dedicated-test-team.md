---
id: TASK-142.1
title: Probe Linear API behavior on a dedicated test team
status: To Do
assignee: []
created_date: '2026-09-25 16:30'
labels:
  - work-queue
milestone: m-1
dependencies: []
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-142
priority: medium
type: spike
ordinal: 65000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
D29 and the Linear declarations are hypotheses. Use only the user-approved dedicated Linear test team, create synthetic issues for mutations, and clean them up. The user offered an API key for this probe; obtain it securely when beginning, never place it in the task or logs.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Record viewer, organization and team IDs; issue UUID versus identifier across team moves; Relay page limits, order, and updatedAt filtering in §3
- [ ] #2 Measure whether relation add/remove changes updatedAt on either endpoint; record archive, trash, and permission-loss visibility
- [ ] #3 Establish workflow state types, single-assignee semantics, request and complexity limits/headers, Markdown operation-marker round-trip, and native claim absence or capabilities against §9
- [ ] #4 Clean up every synthetic issue and revise D29 and §7 for any contradicted assumptions; no existing issue is mutated
<!-- AC:END -->
