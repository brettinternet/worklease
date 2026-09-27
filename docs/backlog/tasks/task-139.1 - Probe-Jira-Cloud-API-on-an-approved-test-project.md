---
id: TASK-139.1
title: Probe Jira Cloud API on an approved test project
status: To Do
assignee: []
created_date: '2026-09-27 00:01'
labels: []
dependencies: []
parent_task_id: TASK-139
type: spike
ordinal: 84000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Jira Cloud behavior and limits for TASK-139 are not yet verified; the parent prohibits finalizing identity and write decisions from documentation alone. Requires a user-approved disposable test project and API-token helper; use only synthetic issues and remove them afterward. Record observed behavior and uncertainty in plan §3 and the Jira declaration.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Synthetic issues in an explicitly approved test project probe search pagination/ordering, links and updated timestamps, project moves, deletion and permission loss, transition requirements, rate-limit headers, ADF marker round-trip, and native-claim semantics; all created issues are removed
- [ ] #2 D30, §3 and §7 distinguish observations from unknowns, and identify safe stable issue and source identities before implementation
<!-- AC:END -->
