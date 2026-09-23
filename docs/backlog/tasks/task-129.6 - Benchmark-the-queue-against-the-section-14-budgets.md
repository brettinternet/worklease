---
id: TASK-129.6
title: Benchmark the queue against the section 14 budgets
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 23:52'
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

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Initial D22 benchmark harness on task-129-6-benchmarks: fixed 10k/50k/100k in-process TUI fixtures, 10k GitHub GraphQL fake with 5 ms/page and 100 requests, 10k SQLite FTS search. 20-sample first run exposed p95 147 ms navigation at 10k and 4,958 ms at 100k; implemented cached sorted TUI projection. Follow-up warm benchmark p95: 0.419 ms (10k), 1.321 ms (50k), 2.333 ms (100k), 60.29 ms warm first view; GitHub 619.46 ms / 1,252,057 bytes / 100 requests; index search 54.10 ms (3 samples). These are Go Update+View, not terminal paint. Continue with end-to-end, combined faults, per-row resource accounting, and section 14 decision before completion.

Checkpoint 2026-09-23: branch task-129-6-benchmarks in .worktrees/task-129-6-benchmarks, commit b3d355a (bench fixtures, p50/p95/p99 runner, relative-only PR comparison, cached TUI rows). General self-review: confirmed old TUI projection incurred repeat full sort; fixed and reran focused checks; no second general review. Verified mise run lint, format-check, test, typecheck, hooks; no acceptance criterion checked because full end-to-end budget and concurrent fault evidence outstanding. Next: benchmark cold remote page-to-render and real terminal paint, Backlog summary/edge process and bytes, measure combined rate-limit/hung adapter/large graph/full-refresh input and renewal; quantify per-row RSS/quota then decide section 14 revisions and failure boundary. Do not merge or clean worktree until complete.
<!-- SECTION:NOTES:END -->
