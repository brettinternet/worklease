---
id: TASK-55
title: Fix macOS Intel guarded execution CI failures
status: Done
assignee:
  - '@brett'
created_date: '2026-09-07 13:21'
updated_date: '2026-09-07 14:13'
labels: []
dependencies: []
ordinal: 56000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The v0.8.0 CI run fails guarded execution timeout/watchdog tests on macOS Intel. Fix the root platform-specific process/output issue while keeping behavior and assertions intact.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Guarded execution timeout and watchdog behavior passes on macOS Intel without weakening assertions
- [x] #2 Lint, format-check, tests, and typecheck remain independently runnable and pass
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reproduce and analyze the macOS Intel timeout/output race in guarded execution.
2. Apply the smallest process lifecycle fix and add or adjust focused regression coverage without weakening assertions.
3. Run focused stress tests plus lint, format-check, full tests, typecheck, and hooks.
4. Finalize TASK-55, commit, push, and verify remote CI.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
CI run 34125927025 failed three guarded-execution tests only on macOS Intel. The timeout fixtures allowed 50-80ms for nested interpreter startup and watchdog scheduling, so the child could miss its output/termination checkpoints before the deadline. The implementation also published deadline_reached before process termination completed and cancelled timers without joining an already-running callback. The fix publishes expiry after termination, joins cancelled watchdogs in singleton and bundle execution, and gives the same strict test assertions realistic scheduling margins. Validation: the three affected tests passed 10 consecutive runs; mise run lint, format-check, test (239 core + 19 SDK), typecheck, and hooks all passed.

Remote CI run 34130578357 passed all six jobs, including Quality (macos-15-intel) with its full tests, typecheck, lint, formatting, builds, and package smoke tests.

Extended by TASK-56. The committed fix raised three budgets to 0.2s but left test_exec_timeout_kills_inherited_pipe_descendant_and_is_inspectable at maximum_duration=0.1, which was one of the four tests failing on macos-15-intel in CI run 34125927025, and 0.2s still has to absorb two nested CPython interpreter startups on that runner. TASK-56 derives all four from one documented GUARD_BUDGET, has children outlive it by 30x, and proves the kill by reaping the descendant's pid instead of waiting out a sleep.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Fixed the macOS Intel guarded-execution CI race by completing watchdog termination before publishing expiry, joining watchdog callbacks before teardown, and widening timing margins while preserving timeout, output, and termination assertions. Verified with 10 focused stress runs, all local quality gates, independent review, and green remote CI run 34130578357.
<!-- SECTION:FINAL_SUMMARY:END -->
