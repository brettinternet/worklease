---
id: TASK-139.4
title: Sync and reconcile Jira Cloud queue index
status: todo
type: feature
parent: TASK-139
blocked_by: [TASK-139.3]
rank: a9c4b73dfa9c4b6b4
created: 2026-09-27T00:02:00Z
updated: 2026-10-22T01:23:00Z
---

Jira search does not guarantee exact totals or deletion evidence; minute-granularity updates and linked issues need safe overlaps and reconciliation rather than naive watermark paging.

## Acceptance criteria

- [ ] Incremental index uses per-quota scheduler and complete reconciliation of issues and links; partial scans do not advance watermarks or prove deletion
- [ ] Tests cover interrupted and reordered pages, missing/deleted/inaccessible items and rate-limit retry time

## Comments

### 2026-10-22T01:23:00Z unknown

Superseded on 2026-10-21: the work queue leaves Worklease (TASK-156). Jira Cloud support moves to bl as BL-40 (probe, owner-gated) and BL-41 (adapter) in github.com/brettinternet/backlog.
