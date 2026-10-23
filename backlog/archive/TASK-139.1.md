---
id: TASK-139.1
title: Probe Jira Cloud API on an approved test project
status: todo
type: spike
parent: TASK-139
rank: a96e7bf53896e7b70
created: 2026-09-27T00:01:00Z
updated: 2026-10-22T01:23:00Z
---

Jira Cloud behavior and limits for TASK-139 are not yet verified; the parent prohibits finalizing identity and write decisions from documentation alone. Requires a user-approved disposable test project and API-token helper; use only synthetic issues and remove them afterward. Record observed behavior and uncertainty in plan §3 and the Jira declaration.

## Acceptance criteria

- [ ] Synthetic issues in an explicitly approved test project probe search pagination/ordering, links and updated timestamps, project moves, deletion and permission loss, transition requirements, rate-limit headers, ADF marker round-trip, and native-claim semantics; all created issues are removed
- [ ] D30, §3 and §7 distinguish observations from unknowns, and identify safe stable issue and source identities before implementation

## Comments

### 2026-10-22T01:23:00Z unknown

Superseded on 2026-10-21: the work queue leaves Worklease (TASK-156). Jira Cloud support moves to bl as BL-40 (probe, owner-gated) and BL-41 (adapter) in github.com/brettinternet/backlog.
