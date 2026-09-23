---
id: TASK-129.7
title: Load-test one remote authority with 25 clients and 500 claims
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 23:42'
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
- [x] #1 A reproducible harness runs 25 clients, each with one namespace watch and an overlay, against one remote authority holding 500 active claims with a recorded 10-minute TTL, renewing before half the TTL
- [x] #2 Renewal p50, p95, and p99 latency and the remaining TTL margin are recorded. The budget is met when p99 renewal leaves at least 50% of the TTL margin
- [x] #3 Burst load (many acquisitions at once) and shorter-TTL saturation are also measured, and the plan states that the claim count alone is not a capacity guarantee
- [x] #4 Authority CPU, store polling rate, write latency, and WAL growth are recorded
- [x] #5 The plan section 17 watch-polling question is answered in the plan. If polling saturates the store, a follow-up task for server-side watch coalescing is created and linked, and the S4 dependency on it is recorded
- [x] #6 The harness is committed with instructions for running it, and no production code path changes
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Build a reproducible remote-authority load harness with 25 watch/overlay clients and 500 renewing claims, plus burst and short-TTL cases. 2. Measure latency, TTL margin, CPU, poll rate, writes and WAL on D22; document results and section 17 decision. 3. Run full gates and review, commit, integrate, finalize and release.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented isolated real-TLS remote authority benchmark (585a604; integrated into main at 66319e8). One review identified misleading watch overlap and metadata 429 evidence; fixed by separate renewal pool, recorded 25 active watches, prior-lease completion margins, higher disposable metadata quota, and server PID cleanup. D22 M1 Max/32 GiB full mise run authority-benchmark evidence /private/tmp/worklease-authority-1790206628-45533/report.json: 25 clients, 500 ten-minute claims; acquire p50/p95/p99 433/627/722 ms; concurrent renewal 979/2022/2617 ms; p99 previous-lease margin 512 s (>300 s); 1,869 watches, 8,604 wall-time-inferred polls (~98/s); CPU 1.35 to 14.03 s, WAL 4,202,432 to 4,350,752 bytes; two rounds of 500 30-second renewals succeeded with minimum previous-lease margin 12.9 s. Polling is estimated, not direct SQLite tracing, and two short-TTL rounds do not prove sustained capacity. Plan section 14/17 updated, no production code changed. mise run lint, format-check, test, typecheck, hooks and focused smoke passed; no follow-up coalescing needed at measured scale. Provider state finalized after integration; unrelated TASK-135 work preserved.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Benchmarked 25 remote clients and 500 claims; renewal p99 2.617 s with 512 s previous-lease margin, no watch saturation. Harness and measurements committed to main; all project gates pass.
<!-- SECTION:FINAL_SUMMARY:END -->
