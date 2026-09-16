---
id: TASK-114
title: Reacquire expired remote contextual claims with a fresh epoch
status: To Do
assignee: []
created_date: '2026-09-16 17:50'
labels: []
dependencies: []
references:
  - internal/cli/remote_lifecycle.go
  - internal/cli/lease_commands.go
  - docs/claim-model.md
priority: high
type: bug
ordinal: 156000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
With the remote profile selected, an expired contextual handle is reused by acquire instead of being replaced with a new ownership epoch. The reproduced default-handle flow listed coordination:demo as expired, then reused claim ef6473a06db7d017b9ee21230030faeb and its completed acquire operation rather than generating a fresh claim. Users should be able to reacquire an expired resource in the same checkout without inventing a new session or understanding handle internals. The local acquire path already distinguishes active, expired, and pending handles; the remote path must provide equivalent safe lifecycle behavior while preserving uncertain-request recovery.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Acquiring through an expired ready remote contextual handle creates a fresh claim ID and credential, replaces the handle only after the new acquire is confirmed, and succeeds without --session or --handle.
- [ ] #2 An active remote contextual handle remains protected from replacement, while a genuinely pending or uncertain request is preserved and must be recovered rather than silently discarded.
- [ ] #3 The fresh acquire uses the caller’s current resource set and acquisition options rather than stale metadata from the expired epoch.
- [ ] #4 Regression tests cover expired singleton and multi-resource handles, active-handle refusal, pending-request preservation, and default versus explicit session selection against the remote adapter.
- [ ] #5 The documented contextual-handle behavior clearly distinguishes generated claim session metadata from the optional session selector used to isolate concurrent loops.
<!-- AC:END -->
