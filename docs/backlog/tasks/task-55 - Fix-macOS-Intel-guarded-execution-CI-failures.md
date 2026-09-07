---
id: TASK-55
title: Fix macOS Intel guarded execution CI failures
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-07 13:21'
updated_date: '2026-09-07 13:53'
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
- [ ] #1 Guarded execution timeout and watchdog behavior passes on macOS Intel without weakening assertions
- [ ] #2 Lint, format-check, tests, and typecheck remain independently runnable and pass
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
<!-- SECTION:NOTES:END -->
