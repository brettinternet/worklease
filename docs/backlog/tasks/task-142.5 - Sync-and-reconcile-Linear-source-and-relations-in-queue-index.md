---
id: TASK-142.5
title: Sync and reconcile Linear source and relations in queue index
status: To Do
assignee: []
created_date: '2026-09-25 16:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-142.4
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-142
priority: medium
type: feature
ordinal: 69000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Integrate Linear source refresh with the existing per-quota scheduler and disposable index. Relation invalidation and pagination semantics come from the recorded probe, not assumptions about updatedAt.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Incremental pages and relation reconciliation preserve complete graph evidence and respect per-organization/account request and complexity limits
- [ ] #2 Interrupted or partial scans never advance a watermark or prove deletion or permission loss; tests cover changed page ordering and relation removal
- [ ] #3 D21 scale fixture benchmark records request counts and latency, and §14 assumptions are updated if measured results differ
<!-- AC:END -->
