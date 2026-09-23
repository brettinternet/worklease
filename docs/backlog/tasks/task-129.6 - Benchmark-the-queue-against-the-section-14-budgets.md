---
id: TASK-129.6
title: Benchmark the queue against the section 14 budgets
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 23:33'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-129.1
  - TASK-129.3
  - TASK-129.4
  - TASK-129.5
references:
  - mise.toml
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-129
priority: high
type: task
ordinal: 26000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Plan section 14 sets acceptance budgets as prototype exit criteria: validate them or revise them with recorded measurements before scope expands, and never weaken safety to meet them. Measure on the D22 reference machine (Apple M1 Max, 32 GiB) with fixed fixtures and injected network latency. CI tracks relative regressions only, never absolute numbers.

Reuse the TASK-126.6 Backlog.md fixture generator, and add a GitHub fixture served by the fake GitHub with configurable latency.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Reproducible benchmark fixtures exist for 10,000-summary Backlog.md and GitHub sources, a 50,000-summary multi-source view, and a 100,000-summary stress view, with injected network latency
- [ ] #2 p50, p95, and p99 are recorded, together with process count, API request count, bytes transferred, RSS, disk and index size, and quota cost, for every row of the section 14 budget table except the authority load row (TASK-129.7)
- [ ] #3 Each budget is marked met, or revised in plan section 14 with the measurement and rationale. The 100,000-summary failure boundary is recorded
- [ ] #4 A combined scenario (rate limiting, a hung adapter, a large graph, and a full refresh at once) shows that input never freezes
- [ ] #5 A CI benchmark job or test tracks relative regressions without absolute thresholds, and a `mise` task runs the full benchmark locally
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reuse deterministic Backlog and fake GitHub fixtures to build reproducible 10k/50k/100k queue benchmark harness with controlled latency and counters. 2. Measure section 14 rows on the D22 machine, including combined fault/refresh scenario and failure boundary; capture p50/p95/p99 and resource/quota evidence. 3. Add local mise runner and relative-only CI regression coverage. 4. Update section 14 with results and budget decisions; run all quality gates, commit, merge and verify.
<!-- SECTION:PLAN:END -->
