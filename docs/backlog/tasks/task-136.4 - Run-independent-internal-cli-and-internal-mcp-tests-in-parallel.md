---
id: TASK-136.4
title: Run independent internal/cli and internal/mcp tests in parallel
status: To Do
assignee: []
created_date: '2026-09-24 15:05'
labels: []
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
- [ ] #1 Every test that calls `t.Setenv`, `os.Setenv`, or `os.Chdir`, or changes a package-level variable, stays serial
- [ ] #2 `go test -race -count=3 ./internal/cli ./internal/mcp` passes
- [ ] #3 `go test -count=1 ./internal/cli` wall time drops by at least 40% against a baseline measured on the same machine, and the task notes record both numbers
<!-- AC:END -->
