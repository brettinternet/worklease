---
id: TASK-133
title: 'Work queue S7: external adapter protocol'
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-133.1
  - TASK-133.2
  - TASK-133.3
documentation:
  - docs/work-queue-tui-proposal.md
priority: medium
type: feature
ordinal: 42000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Adding sources beyond the two built-ins should not require changing Worklease. D16 defers an external adapter protocol until both built-ins have exercised the model. That avoids repeating the Python era's mistake of shipping a source SDK before any real adapter existed (plan section 3, "Prior art"). The protocol is JSON-RPC 2.0 over stdio with newline-delimited messages and a Worklease-specific method set that mirrors the section 7 operations. Resource policies stay static built-ins; an external adapter selects an existing policy. See plan sections 11 and 16 (S7).

This parent is an integration checklist, not an implementation lane. It depends on every child, so it becomes ready only after they are Done. Complete it by re-verifying the criteria below on main and recording the evidence.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every S7 child task is Done
- [ ] #2 Both built-in adapters pass the shared conformance suite
- [ ] #3 Tests cover adapter crashes, malformed output, cancellation, and secret redaction
- [ ] #4 Installing an external adapter requires explicit user approval
<!-- AC:END -->
