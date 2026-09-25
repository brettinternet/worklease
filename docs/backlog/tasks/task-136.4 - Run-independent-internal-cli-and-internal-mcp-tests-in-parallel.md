---
id: TASK-136.4
title: Run independent internal/cli and internal/mcp tests in parallel
status: Done
assignee:
  - '@brett'
created_date: '2026-09-24 15:05'
updated_date: '2026-09-25 23:07'
labels:
  - reviewed
dependencies:
  - TASK-136.1
parent_task_id: TASK-136
priority: medium
type: task
ordinal: 55000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`internal/cli` sets the pace for `go test ./...`: its 240 top-level tests take 74 s in total and 74 s of wall time, so they run strictly one after another. The whole repo has only 18 `t.Parallel()` calls, and in cli and mcp the only one is in `internal/cli/selection_test.go`. The lefthook pre-commit hook waits on this package for every commit, and so does CI (25–41 s on linux-x64). `internal/mcp` looks the same: 26 tests, 29 s.

What we already know:
- Each package `TestMain` calls `testkit.IsolateProcessEnvironment` once. It replaces HOME and XDG_* and unsets WORKLEASE_* and GIT_*, so the process environment the tests share never changes after startup.
- 23 test files call `t.Setenv` (150 calls; `hosted_commands_test.go` has 61 and `doctor_commands_test.go` has 20), and a few call `os.Setenv` or `os.Chdir`. Go does not allow `t.Setenv` in a parallel test. Top-level parallel tests start only after every serial top-level test has finished, so tests that change the environment can stay serial without racing the parallel ones.
- The real risk is package-level mutable state: overridable package variables, cobra globals, and shared fixtures.
- `testkit.Home` returns a per-test environment map without touching the process environment. New parallel-safe tests should follow that pattern.

Direction: call `t.Parallel()` only in tests that touch no process-global state. This task does not refactor production code to inject the environment. If the tests that must stay serial still dominate the run time, record the numbers in the notes and propose a follow-up task.

This depends on TASK-136.1 so that any failure parallelism exposes is not confused with the known flakes.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every test that calls `t.Setenv`, `os.Setenv`, or `os.Chdir`, or changes a package-level variable, stays serial
- [x] #2 `go test -race -count=3 ./internal/cli ./internal/mcp` passes
- [x] #3 Record same-machine baseline and post-change CLI test wall times; safe parallelization improves wall time, and track the remaining serial-test bottleneck in a separate task.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Measure serial baseline and audit cli/mcp tests for process-global mutations and isolated fixtures.
2. Enable parallelism only for safe top-level tests; validate environment/global safety under race.
3. Measure wall-time improvement on same machine; run required project gates, commit, merge, and record evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Baseline on this machine (worktree, go test -count=1 ./internal/cli): real 234.23 s, package 228.549 s. Claim held for TASK-136.4.

Parallelized 30 isolated/pure CLI and MCP tests. Same-machine CLI post-change wall time: 224.39 s (package 221.874 s) versus baseline 234.23 s (package 228.549 s), a 4.2% wall-time improvement. Conservative direct/helper audit flagged 103 CLI tests accounting for about 209 s of the measured test duration as environment-mutating and necessarily serial. User approved replacing the unattainable 40% criterion and tracking isolation separately. Race count=3 passed with timeout 30m (CLI 872.605 s, MCP 26.927 s); default 10m Go test timeout was insufficient. lint, format-check, test, typecheck and staged hooks passed.

Source review found no process-global environment, directory, package hook, or shared fixture mutation in the 30 newly parallel tests; global-mutating tests remain serial. Commit 554b79d, merged to main as 87df6ec. Serial-test isolation is tracked by TASK-136.7. A first pre-commit run encountered an unrelated queue test provider-read timeout under load; its targeted race count=3 and subsequent complete hooks and commit hooks passed.

Post-completion review: the 30 parallel CLI/MCP tests touch no process environment, directory, or package-level state (only remote_test.go uses t.Setenv and stays serial). No follow-up.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Parallelized 30 independently isolated CLI and MCP tests; race count=3 and all project checks passed. CLI wall time improved 234.23 to 224.39 s; TASK-136.7 tracks the remaining serial bottleneck. Committed 554b79d and merged 87df6ec.
<!-- SECTION:FINAL_SUMMARY:END -->
