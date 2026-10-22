---
id: TASK-139.2
title: Pin Jira Cloud source and issue claim identity
status: To Do
assignee: []
created_date: '2026-09-27 00:02'
updated_date: '2026-10-22 01:23'
labels: []
dependencies:
  - TASK-139.1
parent_task_id: TASK-139
type: task
ordinal: 85000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Project and issue keys can change, while sites and JQL locators have separate semantics. Select claim identity from the completed live probe, not from mutable display keys; keep existing static generic coordination policy.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Site-qualified source and stable issue identity are recorded as versioned golden vectors; queue and CLI keys agree byte-for-byte and remote admission accepts coordination keys
- [ ] #2 Project moves, key changes and site mismatches preserve correct claims or require an explicit guarded rebind, with skill jira.md updated
<!-- AC:END -->

## Comments

<!-- COMMENTS:BEGIN -->
created: 2026-10-22 01:23
---
Superseded on 2026-10-21: the work queue leaves Worklease (TASK-156). Jira Cloud support moves to bl as BL-40 (probe, owner-gated) and BL-41 (adapter) in github.com/brettinternet/backlog.
---
<!-- COMMENTS:END -->
