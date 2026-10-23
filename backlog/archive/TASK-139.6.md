---
id: TASK-139.6
title: Add recoverable Jira Cloud writes and Start work
status: todo
type: feature
parent: TASK-139
blocked_by: [TASK-139.5]
rank: a9fe3419269fe338c
created: 2026-09-27T00:02:00Z
updated: 2026-10-22T01:23:00Z
---

Jira Cloud writes are unconditional and workflow transitions are issue-specific. Only enable narrow guarded transitions, marked ADF comments and single-assignee replacement after read/sync/claim behavior is established by predecessor subtasks.

## Acceptance criteria

- [ ] Before every write, revalidate accountId and discovered issue-specific transition and apply §8 recovery/read-back; Start work uses a configured transition
- [ ] ADF comment markers round-trip and assignments require confirmation when replacing another account; tests cover lost responses, receipts, identity drift and secret redaction
- [ ] Queue documentation covers Jira Cloud site, JQL, API-token scopes, limits and Data Center exclusion

## Comments

### 2026-10-22T01:23:00Z unknown

Superseded on 2026-10-21: the work queue leaves Worklease (TASK-156). Jira Cloud support moves to bl as BL-40 (probe, owner-gated) and BL-41 (adapter) in github.com/brettinternet/backlog.
