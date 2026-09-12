---
id: TASK-85.7
title: Implement singleton lease lifecycle and ownership epochs
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.5
  - TASK-85.6
references:
  - src/worklease/acquisition.py
  - src/worklease/claims.py
  - src/worklease/lifecycle.py
  - src/worklease/models.py
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 99000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement the central same-host coordination lifecycle on the new Go authority. Retain expiring ownership epochs, heartbeats, checkpoints, transfer, stale-owner rejection, wait-on-contention, clock-regression handling, and truthful local-only guarantees, but expose a simpler internal API designed for Go callers rather than reproducing Python classes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Callers can acquire, inspect, renew, checkpoint, release, and transfer one resource using an opaque private lease reference and bounded TTL.
- [ ] #2 Stale identities, credentials, revisions, expired claims, and conflicting holders are rejected without mutating current ownership; retryable contention supports a bounded cancellable wait.
- [ ] #3 Mutation operation IDs provide deterministic replay, request mismatch detection, and explicit unknown-outcome state without automatically repeating external effects.
- [ ] #4 Forward and backward wall-clock changes follow documented conservative expiry behavior while waits and deadlines use monotonic time.
- [ ] #5 Deterministic-clock, lifecycle, idempotency, contention, interruption, redaction, and concurrent-process tests pass.
<!-- AC:END -->
