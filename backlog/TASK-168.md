---
id: TASK-168
title: Add hosted admission backpressure and fair queues
status: draft
labels: [remote-authority]
rank: aff1a0c934ff19fe8
created: 2026-09-16T06:03:00Z
updated: 2026-09-16T06:03:00Z
---

Hosted acquisition has no server-side queue, admission backpressure, or FIFO fairness. Add bounded, observable admission controls when measured disk, WAL, pinned-history, request-size, or contention pressure threatens lifecycle capacity, while preserving cancellation, expiry, replay, and resource-bundle atomicity.
