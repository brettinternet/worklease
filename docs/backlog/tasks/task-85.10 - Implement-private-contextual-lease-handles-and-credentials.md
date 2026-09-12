---
id: TASK-85.10
title: Implement private contextual lease handles and credentials
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.7
references:
  - src/worklease/lease_file.py
  - src/worklease/credentials.py
  - src/worklease/execution_context.py
  - TASK-67
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 102000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make the safe path the default: normal lifecycle commands should recover private claim state from a context-scoped handle rather than exposing credentials or requiring identity flags. Redesign the proof-of-concept work from TASK-67 around the new config and store instead of preserving its Python option precedence. Explicit handles and noninteractive credential inputs remain available for concurrent and automated workflows.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Acquire writes an owner-only opaque contextual handle under the configured authority by default, and ordinary status, heartbeat, checkpoint, guarded execution, transfer, verification, and release require no copied token, claim ID, revision, or path.
- [ ] #2 Git worktrees, linked worktrees, subdirectories, and non-Git directories have documented deterministic context identities; explicit handle selection supports concurrent leases in one context.
- [ ] #3 Handle and credential reads reject symlinks, unsafe ownership or permissions, malformed or oversized content, and ambiguous credential sources without exposing secrets.
- [ ] #4 Successful mutations update or clear the handle atomically, and failures after authority commit provide an actionable recovery path without silently losing control of the claim.
- [ ] #5 Tests cover context resolution, collisions, explicit/stateless modes, permissions, atomic writes, stale handles, redaction, transfer, and release cleanup.
<!-- AC:END -->
