---
id: TASK-85.9
title: 'Implement operation inspection, reconciliation, and lifecycle events'
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.8
references:
  - src/worklease/operations.py
  - src/worklease/reconciliation.py
  - src/worklease/projections.py
  - TASK-69
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 101000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Guarded work can fail after intent is durable but before its result is known. Provide one coherent operation ledger and append-oriented lifecycle event model for singleton and bundle activity. This replaces the proof of concept’s separate projection paths and absorbs the useful cross-resource event-feed behavior from TASK-69.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every durable lifecycle transition records a non-secret event with a stable ordering key and enough identity to inspect the affected singleton or bundle epoch.
- [ ] #2 Unknown operations can be inspected and reconciled exactly once against caller-supplied evidence, with request-hash verification and deterministic replay.
- [ ] #3 A paginated global event feed and resource-scoped history use cursor pagination without duplicates or skips among retained rows and disclose retention gaps honestly.
- [ ] #4 Event, history, and reconciliation output excludes credentials, token hashes, checkpoint bodies where not explicitly requested, command output, and provider evidence that is unsafe to list.
- [ ] #5 Tests cover concurrent events, equal timestamps, pagination, retention gaps, singleton and bundle operations, ambiguity, reconciliation outcomes, and deterministic output.
<!-- AC:END -->
