---
id: TASK-142.3
title: Pin Linear organization and issue claim identity vectors
status: Done
assignee: []
created_date: '2026-09-25 16:30'
updated_date: '2026-09-26 04:41'
labels:
  - work-queue
  - reviewed
milestone: m-1
dependencies:
  - TASK-142.1
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-142
priority: medium
type: task
ordinal: 67000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Linear claim keys must not change with the human-readable issue identifier or team move. Use the existing linear static resource policy; do not change policy bytes or version.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Versioned vectors pin organization ID as source and issue UUID as item; identifier/team moves preserve the key or disable claims when identity cannot be established
- [x] #2 Queue- and CLI-derived resources are byte-equal; remote coordination admission accepts the keys
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend shared v1 resource fixtures with probe-backed Linear organization/issue UUID vectors, stable across identifier and team moves and distinct across organizations/issues. 2. Verify resource, CLI-derived JSON, and remote-admission conformance against the same vectors; add focused fail-closed identity coverage where the queue lacks an adapter. 3. Run focused race tests and repository gates, review once, commit, merge, and finalize on main.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Commit fb3ac0b merged to main as fa74c6d. Probe-backed organization/UUID v1 vectors pin TEST-1→PDEV-5 stability and organization/issue isolation; empty identities fail resource derivation. Queue overlay/pre-acquisition and CLI JSON match the shared key; remote coordination admission accepts it and unknown admission or identity drift blocks action. Focused race -count=3 passed in resource, cli, queue and lease both before/after merge; lint, format-check, test, typecheck, staged hooks passed. One diff review found the lease fixture expectation for linear needed updating; corrected and reran checks. Next TASK-142.4 should supply verified UUID/organization inputs from the real adapter, not aliases.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
author: @pi
created: 2026-09-26 04:41
---
Post-completion review confirmed the Linear organization/issue UUID vectors, queue/CLI resource derivation and remote admission agree; no additional item-scoped defect or follow-up identified.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Pinned Linear organization/issue UUID claim vectors and queue/CLI/remote conformance without changing the v1 policy; merged fb3ac0b as fa74c6d. Four focused race tests and all project gates passed.
<!-- SECTION:FINAL_SUMMARY:END -->
