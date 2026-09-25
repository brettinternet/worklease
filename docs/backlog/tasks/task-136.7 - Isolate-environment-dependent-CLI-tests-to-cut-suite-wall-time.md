---
id: TASK-136.7
title: Isolate environment-dependent CLI tests to cut suite wall time
status: To Do
assignee: []
created_date: '2026-09-25 10:49'
labels: []
dependencies:
  - TASK-136.4
parent_task_id: TASK-136
priority: medium
type: task
ordinal: 58000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
On macOS the TASK-136.4 same-machine baseline was 234.23 seconds and parallelizing 30 safe tests reduced it only to 224.39 seconds. A conservative direct/helper audit flags 103 environment-dependent CLI tests consuming about 209 seconds: Go schedules parallel tests only after serial tests finish. Investigate safe test isolation that preserves realistic CLI behavior and does not mutate shared process state; choose the smallest maintainable solution after profiling the current suite. The earlier 40% reduction goal belongs here, not in the safe-parallelism task.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A same-machine go test -count=1 ./internal/cli wall-time run falls by at least 40% against a fresh pre-change baseline, with both measurements in task notes.
- [ ] #2 Environment-mutating tests never run concurrently in one process; isolated test state and process cleanup are verified with go test -race -count=3 ./internal/cli ./internal/mcp.
- [ ] #3 The full lint, format-check, test, and typecheck gates pass.
<!-- AC:END -->
