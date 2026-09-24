---
id: TASK-128.9
title: Enforce read-only slice guarantees with acceptance tests
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 23:56'
labels:
  - work-queue
  - reviewed
milestone: m-1
dependencies:
  - TASK-128.4
  - TASK-128.5
  - TASK-128.6
  - TASK-128.7
  - TASK-128.8
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-128
priority: high
type: task
ordinal: 19000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The S2 exit criteria in plan section 16 are guarantees, not features: no provider writes, no claim ownership, no unexpected network access, the D8 import boundary, and TUI/JSON parity. Enforce each with tests that fail on regression, so later slices cannot silently erode them. Use an end-to-end fixture that combines a scratch Backlog.md project with the fake GitHub server.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 An import-boundary test fails if any package under internal/authority, internal/lease, internal/mcp, internal/server, or internal/store imports internal/queue or a TUI library
- [x] #2 A test proves that queue code has no path to provider mutation commands or to authority acquire, heartbeat, checkpoint, transfer, release, or guarded-operation calls, for example with interface fakes that fail on any mutating call
- [x] #3 A network-isolation test (for example, a dialer hook that fails on unexpected dials) proves that a view with only local sources and the local authority makes no network connection, and that a remote-authority view dials only the selected authority endpoint and the explicitly configured provider endpoints
- [x] #4 A parity test runs one fixture through both `queue query --json` and the TUI model and asserts the same items, readiness, coverage, freshness, authority, and claim observations
- [x] #5 End-to-end tests cover an incomplete dependency graph, an unreadable source, a stale source, and local versus remote authority scope, each visible in both surfaces
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Enforce queue-only read I/O seams and static import/mutation guards. 2. Exercise scratch Backlog.md plus fake GitHub snapshots, JSON/TUI parity, stale/unreadable cases, and local/remote authority and subprocess network isolation. 3. Run focused/full gates, one defect review, commit/merge, and finalize with receipts.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation: narrowed queue authority dependency to Status, enforced read-only Backlog command and GitHub GraphQL seams with negative controls, and added AST import/mutation boundary tests. Acceptance fixtures cover combined scratch Backlog/GitHub, JSON/TUI parity, incomplete and unreadable/stale sources, local/remote scope with live status, and subprocess network sandbox denial. One independent review found six item-scoped gaps; all fixed and affected tests rerun. `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass. Implementation commit aac5f43cdc5b48dd89b2b741d094e61c072fc01c fast-forward merged to main; no D1-D27 or section 16 refinement required.

Review: TUI total is unknown when a source is unresolved; incomplete-graph fixture now carries a dependency edge; combined Backlog+GitHub [me] and held/expired claim-filter TUI/JSON parity tests added (67b1f0e, a6991f2). Merged to main 013a058.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Read-only queue safety and JSON/TUI parity enforced for Backlog.md and GitHub fixtures, stale/unreadable sources, and local/remote authority. Merged aac5f43; full quality gates and hooks passed; review findings fixed.
<!-- SECTION:FINAL_SUMMARY:END -->
