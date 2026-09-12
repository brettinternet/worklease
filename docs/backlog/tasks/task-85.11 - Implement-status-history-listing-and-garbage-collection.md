---
id: TASK-85.11
title: 'Implement status, history, listing, and garbage collection'
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.9
references:
  - src/worklease/projections.py
  - src/worklease/garbage_collection.py
  - docs/cli-reference.md
  - TASK-68
  - TASK-69
  - TASK-70
  - TASK-71
  - TASK-72
  - TASK-73
  - TASK-74
parent_task_id: TASK-85
priority: medium
type: feature
ordinal: 103000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Expose useful read models and bounded maintenance over the new event-oriented authority. Favor concise human summaries with explicit full-detail and machine modes instead of reproducing Python field dumps. Incorporate the completed UX lessons and remaining intent from TASK-68 through TASK-74 while keeping provider state authoritative.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Status, list, policy list, history, and event views default to concise deterministic human output and provide documented full-detail and stable machine-readable modes.
- [ ] #2 History clearly distinguishes complete, open, expired, retention-bounded, and otherwise incomplete epochs without claiming provider audit completeness.
- [ ] #3 Garbage collection previews exact eligible categories and cutoffs, protects active claims and unresolved outcomes, and applies all removals and retirement events atomically.
- [ ] #4 Retention preserves required revision or cursor continuity metadata and reports gaps rather than fabricating complete history.
- [ ] #5 Fixed-clock tests cover empty and populated views, singleton and bundle activity, Unicode and terminal widths, dry-run/apply parity, boundaries, protected records, rollback, and redaction.
<!-- AC:END -->
