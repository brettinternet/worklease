---
id: TASK-139.5
title: Claim and select Jira Cloud work through queue and MCP
status: To Do
assignee: []
created_date: '2026-09-27 00:02'
updated_date: '2026-10-22 01:23'
labels: []
dependencies:
  - TASK-139.4
parent_task_id: TASK-139
type: feature
ordinal: 88000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Read-only Jira visibility is insufficient for agent selection; use pinned generic keys and the existing D11 admission and identity gates without interpreting Jira assignment as an exclusion claim.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Claim for me, D11, identity drift gating, queue next --claim and MCP queue_next work on Jira sources with stable remote-admitted coordination keys
- [ ] #2 Focused CLI and MCP tests cover claim selection and source drift or changed credentials
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-10-22 01:23
---
Superseded on 2026-10-21: the work queue leaves Worklease (TASK-156). Jira Cloud support moves to bl as BL-40 (probe, owner-gated) and BL-41 (adapter) in github.com/brettinternet/backlog.
---
<!-- COMMENTS:END -->
