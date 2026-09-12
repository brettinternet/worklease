---
id: TASK-85.13
title: Add cursor-based lifecycle watches
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.9
references:
  - TASK-81
  - src/worklease/projections.py
parent_task_id: TASK-85
priority: medium
type: feature
ordinal: 105000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Agents and supervisors should be able to wait for authority changes without repeated status polling. Build the TASK-81 capability directly on the new event cursor model with finite, cancellable waits suitable for both one-shot CLI use and long-lived MCP sessions.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A caller can wait from a supplied cursor for the first matching lifecycle event or an explicit bounded timeout result.
- [ ] #2 Resource-scoped waiting supports a claimed resource becoming available and correctly handles release, transfer, expiry replacement, and reacquisition.
- [ ] #3 Stale or collected cursors return an explicit gap result and never silently skip into a false match.
- [ ] #4 Multiple local waiters wake promptly without busy polling, leaking goroutines, or holding a database transaction for the wait duration.
- [ ] #5 Deterministic concurrent tests cover wake-up, timeout, cancellation, multiple waiters, cursor resumption, retention gaps, and secret redaction.
<!-- AC:END -->
