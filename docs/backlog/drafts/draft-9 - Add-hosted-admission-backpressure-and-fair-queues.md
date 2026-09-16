---
id: DRAFT-9
title: Add hosted admission backpressure and fair queues
status: Draft
assignee: []
created_date: '2026-09-16 06:03'
labels:
  - remote-authority
dependencies: []
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Hosted acquisition has no server-side queue, admission backpressure, or FIFO fairness. Add bounded, observable admission controls when measured disk, WAL, pinned-history, request-size, or contention pressure threatens lifecycle capacity, while preserving cancellation, expiry, replay, and resource-bundle atomicity.
<!-- SECTION:DESCRIPTION:END -->
