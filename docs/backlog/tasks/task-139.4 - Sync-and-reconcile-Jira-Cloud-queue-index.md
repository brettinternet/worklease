---
id: TASK-139.4
title: Sync and reconcile Jira Cloud queue index
status: To Do
assignee: []
created_date: '2026-09-27 00:02'
labels: []
dependencies:
  - TASK-139.3
parent_task_id: TASK-139
type: feature
ordinal: 87000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Jira search does not guarantee exact totals or deletion evidence; minute-granularity updates and linked issues need safe overlaps and reconciliation rather than naive watermark paging.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Incremental index uses per-quota scheduler and complete reconciliation of issues and links; partial scans do not advance watermarks or prove deletion
- [ ] #2 Tests cover interrupted and reordered pages, missing/deleted/inaccessible items and rate-limit retry time
<!-- AC:END -->
