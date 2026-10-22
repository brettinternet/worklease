---
id: TASK-139.4
title: Sync and reconcile Jira Cloud queue index
status: To Do
assignee: []
created_date: '2026-09-27 00:02'
updated_date: '2026-10-22 01:23'
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

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-10-22 01:23
---
Superseded on 2026-10-21: the work queue leaves Worklease (TASK-156). Jira Cloud support moves to bl as BL-40 (probe, owner-gated) and BL-41 (adapter) in github.com/brettinternet/backlog.
---
<!-- COMMENTS:END -->
