---
id: TASK-129.7
title: Load-test one remote authority with 25 clients and 500 claims
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
  - authority
milestone: m-1
dependencies:
  - TASK-129.5
references:
  - cmd/worklease-remote-smoke
  - internal/server/server.go
  - internal/watch
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-129
priority: high
type: task
ordinal: 27000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Each namespace watch polls the authority store every 50-500 ms (250 ms by default), so every watching queue client adds a polling loop to the single-writer authority (plan sections 3 and 15). D21 targets 25 concurrent clients and 500 active claims per authority. Plan section 17 asks whether namespace watch polling holds at that scale or needs server-side coalescing, and S4 renewal safety depends on the answer.

Benchmark the authority separately from source throughput. Use a real `worklease serve` instance on the reference machine.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A reproducible harness runs 25 clients, each with one namespace watch and an overlay, against one remote authority holding 500 active claims with a recorded 10-minute TTL, renewing before half the TTL
- [ ] #2 Renewal p50, p95, and p99 latency and the remaining TTL margin are recorded. The budget is met when p99 renewal leaves at least 50% of the TTL margin
- [ ] #3 Burst load (many acquisitions at once) and shorter-TTL saturation are also measured, and the plan states that the claim count alone is not a capacity guarantee
- [ ] #4 Authority CPU, store polling rate, write latency, and WAL growth are recorded
- [ ] #5 The plan section 17 watch-polling question is answered in the plan. If polling saturates the store, a follow-up task for server-side watch coalescing is created and linked, and the S4 dependency on it is recorded
- [ ] #6 The harness is committed with instructions for running it, and no production code path changes
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
