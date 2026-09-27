---
id: TASK-139.3
title: Read Jira Cloud scope and dependencies
status: To Do
assignee: []
created_date: '2026-09-27 00:02'
labels: []
dependencies:
  - TASK-139.1
  - TASK-139.2
parent_task_id: TASK-139
type: feature
ordinal: 86000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Jira sites configure workflows, link names and visibility differently; use observed probe semantics and the shipped bounded credential helper to add a built-in read source with explicit site, JQL, account and hard-link mapping.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Read adapter covers the configured scope with estimated or unknown totals, hydrating issue and link detail while preserving raw status/category/resolution and distinguishing non-success done resolutions
- [ ] #2 Principal matches configured account on credential change; token is not persisted or emitted in logs or errors, and hard links require configured types
- [ ] #3 Shared adapter conformance and focused tests cover interrupted pagination, permission loss, project moves, redaction and non-success resolution
<!-- AC:END -->
