---
id: TASK-139.6
title: Add recoverable Jira Cloud writes and Start work
status: To Do
assignee: []
created_date: '2026-09-27 00:02'
labels: []
dependencies:
  - TASK-139.5
parent_task_id: TASK-139
type: feature
ordinal: 89000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Jira Cloud writes are unconditional and workflow transitions are issue-specific. Only enable narrow guarded transitions, marked ADF comments and single-assignee replacement after read/sync/claim behavior is established by predecessor subtasks.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Before every write, revalidate accountId and discovered issue-specific transition and apply §8 recovery/read-back; Start work uses a configured transition
- [ ] #2 ADF comment markers round-trip and assignments require confirmation when replacing another account; tests cover lost responses, receipts, identity drift and secret redaction
- [ ] #3 Queue documentation covers Jira Cloud site, JQL, API-token scopes, limits and Data Center exclusion
<!-- AC:END -->
