---
id: TASK-128.3
title: Build the queue core snapshot model and adapter interface
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-126
references:
  - skills/worklease-workflow/references/contract.md
  - skills/worklease-workflow/references/source-provider-contract.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-128
priority: high
type: feature
ordinal: 13000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The TUI and the JSON path must be thin clients of one queue core (plan sections 4 and 13), so neither can be safer or know more than the other. The core keeps readiness, assignment, claim availability, freshness, and coverage as separate observations and never collapses them into one "ready" flag. It publishes immutable snapshots, so the render loop never performs I/O (D9).

This task owns the internal adapter interface (the plan section 7 operations, using the capability semantics defined by TASK-126.2), a registry of built-in adapters keyed by adapter name, the item and snapshot model, view evaluation, readiness computation, and a fake adapter for tests. Real adapters (TASK-128.4, TASK-128.5), claim observation (TASK-128.7), the persistent index (TASK-129.1), and the TUI (TASK-128.8) are separate tasks. Put the packages under internal/queue/, and never import TUI libraries there.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 An internal Go adapter interface covers resolve, capabilities, list (with cursor, coverage, and total exactness), batched readItems with per-item outcomes, and readDependencies with completeness, using the TASK-126.2 capability value
- [ ] #2 The item model keeps raw provider status beside the normalized category. It also carries readiness (ready, blocked, or unknown, with reasons), provider-reported readiness as its own field, assignment, claim observation, native claim state, freshness, and coverage
- [ ] #3 Readiness follows the TASK-126.7 contract extension and D23: a known unsatisfied hard prerequisite means blocked even while other edges are loading; otherwise incomplete or stale evidence means unknown; ready requires a complete, satisfied closure and no provider blocker. Missing prerequisites and cycles prevent readiness proof. Provider-reported readiness is never used as proof
- [ ] #4 Relationships keep their type, direction, source-qualified endpoints, provenance, completion condition, and raw outcome. Hierarchy and related work never block, and a test with the TASK-126.7 fixture file passes every case
- [ ] #5 A change to an edge, a prerequisite's state, a completion condition, or permissions (including a reopen) recomputes every affected dependent. The transitive graph is never cached solely under the selected item's version
- [ ] #6 View evaluation applies filters, preserves configured source order and per-source order with deterministic tie-breakers, and deduplicates items by canonical identity
- [ ] #7 Snapshots are immutable and delivered by subscription. A hung, failing, or partially paged source never blocks other sources' snapshots, as shown by tests that use the fake adapter
- [ ] #8 A fake adapter in a test package supports pagination, errors, delays, capability denials, and dependency edges
- [ ] #9 No package under internal/queue/ imports Bubble Tea, Lip Gloss, or any other TUI library
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
