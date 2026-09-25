---
id: TASK-142.3
title: Pin Linear organization and issue claim identity vectors
status: To Do
assignee: []
created_date: '2026-09-25 16:30'
updated_date: '2026-09-25 16:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-142.1
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-142
priority: medium
type: task
ordinal: 67000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Linear claim keys must not change with the human-readable issue identifier or team move. Use the existing linear static resource policy; do not change policy bytes or version.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Versioned vectors pin organization ID as source and issue UUID as item; identifier/team moves preserve the key or disable claims when identity cannot be established
- [ ] #2 Queue- and CLI-derived resources are byte-equal; remote coordination admission accepts the keys
<!-- AC:END -->
