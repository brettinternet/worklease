---
id: TASK-81
title: Add cursor-based lifecycle watches
status: To Do
assignee: []
created_date: '2026-09-12 03:06'
labels: []
dependencies:
  - TASK-69
references:
  - 'https://github.com/DmarshalTU/coord'
priority: medium
type: feature
ordinal: 88000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Supervisors and coding agents currently poll status, retry acquisition, or repeatedly inspect history to learn that a resource or the local authority changed. A bounded cursor-based watch should let callers sleep until relevant lifecycle activity occurs, reducing process churn and model/tool polling while building on the cross-resource event feed.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Callers can wait from a supplied event cursor for a bounded duration and receive the first matching lifecycle change or an explicit timeout result.
- [ ] #2 Resource-scoped waiting supports the practical condition of a claimed resource becoming available without requiring the caller to repeatedly invoke status.
- [ ] #3 The watch contract handles release, transfer, replacement after expiry, new acquisition, and retained-history cursor gaps without silently reporting a false match.
- [ ] #4 CLI and MCP surfaces return consistent schema-versioned, non-secret results and enforce finite timeout limits appropriate to their transports.
- [ ] #5 Concurrent tests demonstrate prompt wake-up, timeout behavior, multiple waiters, cursor resumption, and token/checkpoint redaction.
- [ ] #6 Documentation distinguishes lifecycle notification from task scheduling, durable provider progress, and provider-side fencing.
<!-- AC:END -->
