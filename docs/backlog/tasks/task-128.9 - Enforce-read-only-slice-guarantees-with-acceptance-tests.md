---
id: TASK-128.9
title: Enforce read-only slice guarantees with acceptance tests
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
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
- [ ] #1 An import-boundary test fails if any package under internal/authority, internal/lease, internal/mcp, internal/server, or internal/store imports internal/queue or a TUI library
- [ ] #2 A test proves that queue code has no path to provider mutation commands or to authority acquire, heartbeat, checkpoint, transfer, release, or guarded-operation calls, for example with interface fakes that fail on any mutating call
- [ ] #3 A network-isolation test (for example, a dialer hook that fails on unexpected dials) proves that a view with only local sources and the local authority makes no network connection, and that a remote-authority view dials only the selected authority endpoint and the explicitly configured provider endpoints
- [ ] #4 A parity test runs one fixture through both `queue query --json` and the TUI model and asserts the same items, readiness, coverage, freshness, authority, and claim observations
- [ ] #5 End-to-end tests cover an incomplete dependency graph, an unreadable source, a stale source, and local versus remote authority scope, each visible in both surfaces
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
