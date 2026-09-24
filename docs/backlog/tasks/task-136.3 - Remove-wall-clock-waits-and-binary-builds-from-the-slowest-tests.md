---
id: TASK-136.3
title: Remove wall-clock waits and binary builds from the slowest tests
status: To Do
assignee: []
created_date: '2026-09-24 15:05'
labels: []
dependencies: []
parent_task_id: TASK-136
priority: medium
type: task
ordinal: 54000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Measured on 2026-09-24 on an M1 Max with `go test -count=1 -json ./...` (81 s wall): a few tests spend most of their time sleeping or compiling rather than checking anything. Every pre-commit run pays for them, because lefthook runs the full suite, and so does CI.

| Test | Time | Cause |
|---|---|---|
| `internal/mcp` TestReferencesCrossServerPendingRecoveryAndRestartHold | 18.2 s | runs `go build ../../cmd/worklease` inside the test so it can call `worklease verify` |
| `internal/queueindex` TestBusyOpenDoesNotRebuildLiveIndex | 10.2 s | waits out the hard-coded SQLite `busy_timeout(10000)` in `internal/queueindex/index.go:88` |
| `internal/watch` TestWaitTimeoutContinuationDoesNotSkipLateEvent, TestWaitTimeoutCursorOnlyAdvancesThroughScannedRows, TestEmptyFeedReturnsDurableBoundCursor, TestWaitUsesPersistedObservationTimeAfterClockRollback | 2–3 s each | real timeouts and poll intervals, for example the 2750 ms sleep at `watch_test.go:294` |
| TTL-expiry waits | 1.1–1.2 s each | `internal/mcp/remote_test.go:158`, `internal/mcp/review_regression_test.go:106`, and `internal/cli/ledger_commands_test.go:208` sleep until a 1 s TTL lapses |
| `internal/queueindex/index_test.go:583` | 1.5 s | a helper process sleeps after writing its marker |

Context: `internal/cli` imports `internal/mcp`. A test that needs both MCP servers and the CLI can therefore live in `internal/cli` and call the CLI in-process (`Run`, or `testkit.RunCLI`) instead of building a binary. Controlled time already exists: `testkit.Clock`, and constructors such as `lease.New(st, clock, ...)` accept a clock. Keep the tested behavior exactly as it is; the goal is to stop spending wall time, not to drop assertions. Leave the eight-worker contention tests (`TestQueueNextEight*` and `TestMCPQueueNextEightMixedContenders`) alone, because they deliberately exercise real concurrency.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 No `*_test.go` file runs `go build`
- [ ] #2 TestReferencesCrossServerPendingRecoveryAndRestartHold, or its moved equivalent, keeps every assertion, including that the CLI verify of an MCP handle does not leak token fields, and runs in under 2 s
- [ ] #3 TestBusyOpenDoesNotRebuildLiveIndex still proves that a second Open fails while another connection holds the write lock, runs in under 2 s, and the production busy timeout stays 10 s
- [ ] #4 The listed TTL and timeout sleeps are replaced by controlled clocks or shorter durations; where an enforced minimum prevents this, the task notes say which one and why
- [ ] #5 The rewritten mcp and queueindex tests still fail when the behavior they guard is deliberately broken; the task notes record this one-time check
- [ ] #6 The task notes record per-package times from `go test -count=1 -json ./...` before and after, and the mcp, queueindex, and watch packages are each at least 50% faster
<!-- AC:END -->
