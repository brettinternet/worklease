---
id: TASK-85.8
title: Implement atomic bundle leases
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.7
references:
  - src/worklease/acquisition.py
  - src/worklease/lifecycle.py
  - src/worklease/claims.py
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 100000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Support workflows that must own several resources as one unit. Bundles should use the same ownership and operation concepts as singleton leases without maintaining parallel implementations that can drift. Preserve caller order where it is meaningful while acquiring internal locks deterministically.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 One operation atomically acquires 1 to the documented maximum number of unique ordered resources or acquires none.
- [ ] #2 Status, heartbeat, checkpoint where supported, release, execution authorization, inspection, and reconciliation operate on the exact bundle ownership epoch.
- [ ] #3 Singleton operations cannot silently mutate bundle members, overlapping bundles contend correctly, and lock ordering cannot deadlock.
- [ ] #4 Bundle operation replay, expiry, transfer or its documented exclusion, and unknown outcomes follow the shared lifecycle model rather than divergent special cases.
- [ ] #5 Concurrent, rollback, ordering, overlap, expiry, redaction, and maximum-size tests pass under normal and race-enabled runs.
<!-- AC:END -->
