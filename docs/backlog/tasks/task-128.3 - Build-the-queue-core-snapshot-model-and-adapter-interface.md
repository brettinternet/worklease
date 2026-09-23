---
id: TASK-128.3
title: Build the queue core snapshot model and adapter interface
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 17:02'
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
- [x] #1 An internal Go adapter interface covers resolve, capabilities, list (with cursor, coverage, and total exactness), batched readItems with per-item outcomes, and readDependencies with completeness, using the TASK-126.2 capability value
- [x] #2 The item model keeps raw provider status beside the normalized category. It also carries readiness (ready, blocked, or unknown, with reasons), provider-reported readiness as its own field, assignment, claim observation, native claim state, freshness, and coverage
- [x] #3 Readiness follows the TASK-126.7 contract extension and D23: a known unsatisfied hard prerequisite means blocked even while other edges are loading; otherwise incomplete or stale evidence means unknown; ready requires a complete, satisfied closure and no provider blocker. Missing prerequisites and cycles prevent readiness proof. Provider-reported readiness is never used as proof
- [x] #4 Relationships keep their type, direction, source-qualified endpoints, provenance, completion condition, and raw outcome. Hierarchy and related work never block, and a test with the TASK-126.7 fixture file passes every case
- [x] #5 A change to an edge, a prerequisite's state, a completion condition, or permissions (including a reopen) recomputes every affected dependent. The transitive graph is never cached solely under the selected item's version
- [x] #6 View evaluation applies filters, preserves configured source order and per-source order with deterministic tie-breakers, and deduplicates items by canonical identity
- [x] #7 Snapshots are immutable and delivered by subscription. A hung, failing, or partially paged source never blocks other sources' snapshots, as shown by tests that use the fake adapter
- [x] #8 A fake adapter in a test package supports pagination, errors, delays, capability denials, and dependency edges
- [x] #9 No package under internal/queue/ imports Bubble Tea, Lip Gloss, or any other TUI library
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Define queue source-qualified model and read-only adapter interface under internal/queue with explicit capability, coverage, freshness and per-item outcomes. 2. Implement dependency closure evaluation, view ordering/deduplication and immutable subscription snapshots with independently refreshed sources. 3. Add fake adapter and fixture-driven/readiness, invalidation, ordering, isolation and latency tests. 4. Run focused and repository gates, review, integrate and record evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Queue core and fixture tests implemented in task-128-3-queue-core; focused race test, repository test and vet passed. One independent review found concrete readiness, snapshot and source refresh defects; correcting and extending regression tests before final gates.

Verification: fixture-driven eligibility cases and regression tests for reopen, changed permissions, stale/incomplete dependencies, owner maintenance, pagination, principal isolation, independent refresh and snapshot immutability pass (`go test -race -count=1 ./internal/queue/...`). On integrated main: mise run lint, format-check, test, typecheck, hooks pass; no TUI imports. One general review found 13 concrete defects, all corrected; independent verifier found missing permission observation, corrected with ReadPermission and regression test. No D1-D27 or plan refinement required. Source commit bf096ac, integrated by merge d67ff61. Next: commit final task receipt, clean owned worktree, release claim.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Built source-qualified read-only queue core, typed dependency readiness, deterministic views, immutable subscribed snapshots and fake adapter. Fixture and regression tests, race tests, repository gates and hooks passed; merged as d67ff61.
<!-- SECTION:FINAL_SUMMARY:END -->
