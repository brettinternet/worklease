---
id: TASK-163
title: Add recovery import and durable completed-history journals
status: draft
labels: [remote-authority, recovery]
rank: afa9c4b73dfa9c3da
created: 2026-09-16T06:03:00Z
updated: 2026-09-16T06:03:00Z
---

Restore recovery can remain blocked when independent installation, pending-set, lost-tail, or completed-operation evidence cannot be reconstructed. Design auditable recovery import and a durable client-side completed-history journal for representing known lost-tail work and coverage without treating journals as proof of executor or provider cessation. Trigger implementation when recovery drills cannot meet the target with manual evidence.
