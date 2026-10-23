---
id: TASK-139.3
title: Read Jira Cloud scope and dependencies
status: todo
type: feature
parent: TASK-139
blocked_by: [TASK-139.1, TASK-139.2]
rank: a9a7f8d0649a7f848
created: 2026-09-27T00:02:00Z
updated: 2026-10-22T01:23:00Z
---

Jira sites configure workflows, link names and visibility differently; use observed probe semantics and the shipped bounded credential helper to add a built-in read source with explicit site, JQL, account and hard-link mapping.

## Acceptance criteria

- [ ] Read adapter covers the configured scope with estimated or unknown totals, hydrating issue and link detail while preserving raw status/category/resolution and distinguishing non-success done resolutions
- [ ] Principal matches configured account on credential change; token is not persisted or emitted in logs or errors, and hard links require configured types
- [ ] Shared adapter conformance and focused tests cover interrupted pagination, permission loss, project moves, redaction and non-success resolution

## Comments

### 2026-10-22T01:23:00Z unknown

Superseded on 2026-10-21: the work queue leaves Worklease (TASK-156). Jira Cloud support moves to bl as BL-40 (probe, owner-gated) and BL-41 (adapter) in github.com/brettinternet/backlog.
