---
id: TASK-81
title: Add cursor-based lifecycle watches
status: Done
assignee: []
created_date: '2026-09-12 03:06'
updated_date: '2026-09-12 04:45'
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Closed 2026-09-12 as superseded before starting the unattended go-rewrite loop, so readiness-based selection cannot pick Python-era work. Acceptance criteria intentionally left unchecked: they were not delivered here. Delivery is owned by TASK-85.13.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Superseded by TASK-85.13 in the Go rewrite (TASK-85). Not delivered in Python. Go Product Contract (docs/backlog/docs/go-rewrite/doc-2) section 11 (watch by cursor or resource with bounded timeout and honest gaps) fixes the behavior.
<!-- SECTION:FINAL_SUMMARY:END -->
