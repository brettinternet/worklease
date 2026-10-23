---
id: TASK-139.5
title: Claim and select Jira Cloud work through queue and MCP
status: todo
type: feature
parent: TASK-139
blocked_by: [TASK-139.4]
rank: a9e175ab909e17520
created: 2026-09-27T00:02:00Z
updated: 2026-10-22T01:23:00Z
---

Read-only Jira visibility is insufficient for agent selection; use pinned generic keys and the existing D11 admission and identity gates without interpreting Jira assignment as an exclusion claim.

## Acceptance criteria

- [ ] Claim for me, D11, identity drift gating, queue next --claim and MCP queue_next work on Jira sources with stable remote-admitted coordination keys
- [ ] Focused CLI and MCP tests cover claim selection and source drift or changed credentials

## Comments

### 2026-10-22T01:23:00Z unknown

Superseded on 2026-10-21: the work queue leaves Worklease (TASK-156). Jira Cloud support moves to bl as BL-40 (probe, owner-gated) and BL-41 (adapter) in github.com/brettinternet/backlog.
