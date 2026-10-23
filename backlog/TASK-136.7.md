---
id: TASK-136.7
title: Isolate environment-dependent CLI tests to cut suite wall time
status: done
type: task
priority: medium
assignees: ['@brett']
labels: [reviewed]
parent: TASK-136
blocked_by: [TASK-136.4]
rank: a68324d3fc6832478
created: 2026-09-25T10:49:00Z
updated: 2026-09-26T03:40:00Z
---

On macOS the TASK-136.4 same-machine baseline was 234.23 seconds and parallelizing 30 safe tests reduced it only to 224.39 seconds. A conservative direct/helper audit flags 103 environment-dependent CLI tests consuming about 209 seconds: Go schedules parallel tests only after serial tests finish. Investigate safe test isolation that preserves realistic CLI behavior and does not mutate shared process state; choose the smallest maintainable solution after profiling the current suite. The earlier 40% reduction goal belongs here, not in the safe-parallelism task.

## Acceptance criteria

- [x] A same-machine go test -count=1 ./internal/cli wall-time run falls by at least 40% against a fresh pre-change baseline, with both measurements in task notes.
- [x] Environment-mutating tests never run concurrently in one process; isolated test state and process cleanup are verified with go test -race -count=3 ./internal/cli ./internal/mcp.
- [x] The full lint, format-check, test, and typecheck gates pass.

## Plan

1. Profile the fresh CLI baseline and identify the serial environment-mutating tests that dominate wall time. 2. Re-execute selected slow tests in bounded, isolated test-binary processes so the parent may schedule them in parallel without shared process mutation; validate process cleanup and failure propagation. 3. Compare same-machine wall times, run race and project quality gates, review, commit, merge to main, and record verification.

## Notes

Fresh pre-change worktree baseline: /usr/bin/time -p go test -count=1 ./internal/cli: real 250.08 s, package 246.493 s (macOS, 2026-09-25). Profiling next.

Profiling (top-level test durations) found TestQueueNextStartOutcomes 49.34 s, queue write preview 21.76 s, queue start composition 19.31 s, and related queue start/next tests dominating. Isolated 15 slow environment-mutating top-level tests with a bounded test-binary re-exec per test; parent tests schedule in parallel, child TestMain isolates HOME/XDG and exits after the body, process-group cleanup reaps descendants. Same-machine post-change go test -count=1 ./internal/cli: real 107.64 s, package 104.962 s versus fresh baseline real 250.08 s (57.0% less wall time).

Post-review validation: go test -race -count=3 -timeout 30m ./internal/cli ./internal/mcp passed (CLI 510.800 s, MCP 26.995 s). Focused testkit race count=3 passed, including parent deadline/process-group cleanup, exact subtest selection and child skip/failure propagation; CLI subtest selection and missing-backlog SKIP confirmed. mise run lint, format-check, test and typecheck all passed. Single read-only review found three concrete reexec issues (parent deadline, subtest selector, skip propagation); fixed all and reran affected checks.

Code commit e2a3dbd1e23d4f96b8716791c315cfd9e7f72083 merged to main as 65b041a. Staged pre-commit hooks passed (gofmt and full test); no residual item-scoped review findings after corrections. Next resumable step: none; criteria verified and delivery integrated.

Retrospective review: isolated-test runner, selected CLI test callsites, bounded cleanup, selection and skip propagation checks remain correct; testkit race count=3 and focused CLI race checks passed. No necessary follow-up.

## Summary

Isolated 15 slow environment-mutating CLI tests in bounded child processes, reducing same-machine CLI wall time from 250.08 s to 107.64 s (57.0%). Race count=3, all project gates, staged hooks, and targeted process/selection/skip tests passed. Committed e2a3dbd; merged as 65b041a.
