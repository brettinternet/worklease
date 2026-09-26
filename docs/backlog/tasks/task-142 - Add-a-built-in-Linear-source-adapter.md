---
id: TASK-142
title: Add a built-in Linear source adapter
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-25 16:30'
updated_date: '2026-09-26 00:26'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-142.1
  - TASK-142.2
  - TASK-142.3
  - TASK-142.4
  - TASK-142.5
  - TASK-142.6
  - TASK-142.7
documentation:
  - docs/work-queue-tui-proposal.md
  - skills/worklease-workflow/references/source-providers/linear.md
priority: medium
type: feature
ordinal: 64000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The user requested Linear support on 2026-09-25 for a frequently used environment. Worklease queue currently cannot show its issues, dependency readiness, or claims. D29 accepts a built-in Go adapter with a user-configured credential helper and stable organization/issue identity. This is an integration checklist; implement through ordered subtasks after probing a dedicated test team. Do not infer unprobed API behavior or enable unsafe writes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The Linear API probe is recorded in plan §3 and any contradicted declarations are revised
- [x] #2 Credential helper and Linear identity vectors pin safe principal and claim-key behavior
- [x] #3 Read, sync, and dependency behavior pass the shared conformance suite with complete-graph and partial-scan safeguards
- [x] #4 Claim for me, D11, the identity gate, queue next --claim, and MCP queue_next work on Linear sources
- [x] #5 Focused writes follow §8 recovery/read-back, and Linear configuration and limits are documented
- [x] #6 All Linear child tasks are Done and acceptance is reverified on main
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reverify the seven completed child outcomes against the Linear probe, plan §3, shared conformance and queue write/claim tests on main. 2. Run required repository quality gates in an isolated worktree; correct only item-scoped regressions. 3. Record criterion-specific evidence and completion on main, commit the parent task record on the worktree, merge into main and remove the owned worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Main re-verification 2026-09-26: §3 TEST/PDEV probe documents UUID stability, 250-page limit, moving-cursor skip, asymmetric relation updatedAt, archived/trash visibility, single assignee, comment marker and quota headers; D29/§7 revised and two synthetic issues deleted (TASK-142.1). Generic helper and principal/redaction tests, v1 organization/UUID key fixtures, adapter conformance/complete relation safeguards, index interrupted/reordered/relation tests, CLI/MCP known-item claims, and §8 write recovery are delivered in completed TASK-142.2–142.7. On main: mise exec -- go test -race -count=3 -run "TestLinear|TestAdapterConformance/linear$" ./internal/queue ./internal/queueindex ./internal/cli and TestVersionedKeyVectors|TestStaticPolicyGoldenDerivations ./internal/resource passed. Isolated worktree: focused Linear race tests and shared Linear adapter conformance passed; mise run lint, format-check, typecheck, test all passed; no code changes needed. One general integration review of child evidence, plan and test outcomes found no concrete item-scoped defect. Linear setup, scope and recovery limits documented in docs/queue.md. No live production writes attempted; permission loss and cross-client quota behavior remain explicitly unprobed.
<!-- SECTION:NOTES:END -->
