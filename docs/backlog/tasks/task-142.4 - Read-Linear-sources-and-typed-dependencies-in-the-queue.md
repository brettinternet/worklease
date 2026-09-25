---
id: TASK-142.4
title: Read Linear sources and typed dependencies in the queue
status: Done
assignee: []
created_date: '2026-09-25 16:30'
updated_date: '2026-09-25 18:34'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-142.1
  - TASK-142.2
  - TASK-142.3
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-142
priority: medium
type: feature
ordinal: 68000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Add a built-in Linear Go adapter using GraphQL HTTPS, adapting §7 after probe evidence rather than assuming provider semantics. A source names organization, team, optional project filter, and account.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 resolve, capabilities, list, readItems, and readDependencies use bounded pagination and report exact coverage and principal identity
- [x] #2 Blocking relationships become hard edges with typed provenance; related, duplicate, similar, and parent relationships remain informational unless the probe supports another decision
- [x] #3 Raw workflow state remains beside normalized state; unknown capability never permits action; shared adapter conformance passes
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reuse queue adapter contracts and probed Linear API semantics for read-only resolve, capabilities, bounded issue/dependency pagination and stable identity. 2. Add focused GraphQL fixture tests and shared conformance coverage for coverage, hard/informational edges, raw state and failure paths. 3. Run focused race and repository gates, review once, commit and merge to main, then finalize and clean up.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Merged read-only Linear adapter on main (code 6ab9089, merge 6425a1e; relation fixture a0931d6, merge d6dd042). Config pins organization/team/account UUIDs and optional project; resolver verifies viewer and scope through bounded credential helper. List exposes partial visibility until TASK-142.5 reconciles moving pages/permissions; neither disappearance nor incomplete cross-scope relation proves deletion or readiness. Fixture tests cover pagination limit, stable UUID, principal mismatch and stale binding, raw/normalized state, both relation directions, blocking versus related/duplicate/similar/parent, scope and selected-row hydration, quota headers. Read-only Linear branch of shared adapter conformance passes; mutation/receipt checks belong to TASK-142.7. One independent review found six concrete risks; fixed dependency closure, re-resolution, selected hydration, me filter, project parent and two-quota handling; reran affected race and full gates. No live production Linear data was read in this implementation.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Shipped read-only Linear queue source and typed dependency reads with fail-closed partial coverage; shared conformance, focused race tests, lint, format, tests, typecheck, and staged hooks passed. Merged as 6425a1e and d6dd042; sync, claims and writes remain ordered follow-up subtasks.
<!-- SECTION:FINAL_SUMMARY:END -->
