---
id: TASK-139.2
title: Pin Jira Cloud source and issue claim identity
status: todo
type: task
parent: TASK-139
blocked_by: [TASK-139.1]
rank: a98b3a62ce98b39dc
created: 2026-09-27T00:02:00Z
updated: 2026-10-22T01:23:00Z
---

Project and issue keys can change, while sites and JQL locators have separate semantics. Select claim identity from the completed live probe, not from mutable display keys; keep existing static generic coordination policy.

## Acceptance criteria

- [ ] Site-qualified source and stable issue identity are recorded as versioned golden vectors; queue and CLI keys agree byte-for-byte and remote admission accepts coordination keys
- [ ] Project moves, key changes and site mismatches preserve correct claims or require an explicit guarded rebind, with skill jira.md updated

## Comments

### 2026-10-22T01:23:00Z unknown

Superseded on 2026-10-21: the work queue leaves Worklease (TASK-156). Jira Cloud support moves to bl as BL-40 (probe, owner-gated) and BL-41 (adapter) in github.com/brettinternet/backlog.
