---
id: TASK-163
title: Add recovery import and durable completed-history journals
status: draft
type: feature
priority: low
labels: [remote-authority, recovery]
related: [TASK-160]
rank: afa9c4b73dfa9c3da
refs: [docs/remote-claim-authority.md, docs/go-rewrite-contract.md]
created: 2026-09-16T06:03:00Z
updated: 2026-10-24T04:09:37Z
---

Restore recovery can remain blocked when independent installation, pending-set, lost-tail, or completed-operation evidence cannot be reconstructed. Design auditable recovery import and a durable client-side completed-history journal for representing known lost-tail work and coverage without treating journals as proof of executor or provider cessation. Trigger implementation when recovery drills cannot meet the target with manual evidence.

## Acceptance criteria

- [ ] Before design, a recovery drill record shows manual evidence cannot represent known lost-tail work or establish coverage

- [ ] Recovery import accepts only structured, auditable evidence and never redispatches an old effect

- [ ] Docs state the journal helps enumerate work and never proves executor or provider cessation
