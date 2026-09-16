---
id: DRAFT-4
title: Add recovery import and durable completed-history journals
status: Draft
assignee: []
created_date: '2026-09-16 06:03'
labels:
  - remote-authority
  - recovery
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Restore recovery can remain blocked when independent installation, pending-set, lost-tail, or completed-operation evidence cannot be reconstructed. Design auditable recovery import and a durable client-side completed-history journal for representing known lost-tail work and coverage without treating journals as proof of executor or provider cessation. Trigger implementation when recovery drills cannot meet the target with manual evidence.
<!-- SECTION:DESCRIPTION:END -->
