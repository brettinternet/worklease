---
id: TASK-136.3
title: Remove wall-clock waits and binary builds from the slowest tests
status: Done
assignee:
  - '@brett'
created_date: '2026-09-24 15:05'
updated_date: '2026-09-25 23:07'
labels:
  - reviewed
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
- [x] #1 No `*_test.go` file runs `go build`
- [x] #2 TestReferencesCrossServerPendingRecoveryAndRestartHold, or its moved equivalent, keeps every assertion, including that the CLI verify of an MCP handle does not leak token fields, and runs in under 2 s
- [x] #3 TestBusyOpenDoesNotRebuildLiveIndex still proves that a second Open fails while another connection holds the write lock, runs in under 2 s, and the production busy timeout stays 10 s
- [x] #4 The listed TTL and timeout sleeps are replaced by controlled clocks or shorter durations; where an enforced minimum prevents this, the task notes say which one and why
- [x] #5 The rewritten mcp and queueindex tests still fail when the behavior they guard is deliberately broken; the task notes record this one-time check
- [x] #6 The task notes record per-package times from `go test -count=1 -json ./...` before and after, and the mcp, queueindex, and watch packages are each at least 50% faster
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Measure baseline per-package times and inspect slow tests. 2. Replace build/sleeps with in-process CLI, controlled clocks, and bounded lock/process checks without weakening assertions. 3. Run focused, mutation, race and full gates; compare times; commit, merge, and finalize.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Per-package seconds from go test -count=1 -json ./... before -> after (2026-09-25, same worktree/host): cmd/worklease .528 -> .208; cmd/worklease-release .525 -> .206; cmd/worklease-remote-smoke 1.567 -> 1.426; internal/authority 1.324 -> 1.633; internal/cli 230.904 -> 229.842; internal/config .496 -> .463; internal/doctor .478 -> .529; internal/gc .921 -> .963; internal/guard 6.514 -> 7.340; internal/handle 1.072 -> 1.357; internal/instructions .227 -> .139; internal/lease 1.757 -> 2.142; internal/ledger .757 -> .758; internal/mcp 23.367 -> 8.716 (62.7% faster); internal/output .167 -> .116; internal/queue 43.115 -> 29.164; internal/queueindex 12.787 -> 1.232 (90.4% faster); internal/queueui .217 -> .296; internal/reason .105 -> .182; internal/release .212 -> .206; internal/resource 1.393 -> 1.322; internal/sampleadapter .329 -> .308; internal/server .461 -> .429; internal/setup .215 -> .202; internal/store 1.202 -> 1.246; internal/testkit 2.315 -> 2.349; internal/watch 9.677 -> 1.197 (87.6% faster). No-test packages omitted.

One-time deliberate break checks: made queueindex.open return success despite migration error; TestBusyOpenDoesNotRebuildLiveIndex failed with second Open unexpectedly succeeded under write lock. Made MCP status reject lease references; moved TestReferencesCrossServerPendingRecoveryAndRestartHold failed on second-server reference. Both mutations reverted, focused tests and full suite passed afterward. CLI/MCP handle test 0.10s, queueindex busy Open 0.12s. Local MCP and ledger expiry tests set stored expires_at into the past instead of sleeping. Remote MCP expiry still waits ~1.1s: MCP acquire enforces minimum 1s TTL (internal/mcp/mcp.go); remote hosted authority owns its clock, so a client test cannot advance it. Watch tests use 100-180ms timeouts and a 140ms late event; production MinPoll remains 50ms. Queueindex production busy_timeout remains 10000ms; only the locked test uses 100ms.

Review: one bounded item-scoped pass over the diff found no remaining concrete defects. Gates passed on code commit 35523e6: mise run lint, format-check, test, typecheck, hooks; go test -race -count=3 for changed CLI, MCP expiry, queueindex and watch cases; fresh go test -count=1 -json ./... before/after. Committed 35523e6 and fast-forward merged into main; Worktrunk worktree and branch removed after verifying receipt and same-commit integration. No push.

Post-completion review (b33ff23): TestWaitTimeoutContinuationDoesNotSkipLateEvent left only 40 ms between the last scan and the injected event, so a delayed Wait start could observe it early; now PollInterval exceeds Timeout (one scan) with a 250 ms write. TestLockIsSingleFlightAcrossProcesses busy-spun with a 2 s helper-start deadline and leaked blocked helpers on failure; now polls every 10 ms, allows 20 s, and kills helpers in cleanup. Race count=3 and all gates passed. No follow-up.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Moved MCP/CLI and launcher process tests to in-process/re-executed test-binary fixtures, bounded queue index lock tests, and shortened timeout/expiry tests. Verified all six criteria with fresh package timings (mcp 23.367→8.716s, queueindex 12.787→1.232s, watch 9.677→1.197s), race/focused tests, deliberate break checks, and all quality gates. Commit 35523e6 integrated into main; worktree cleaned.
<!-- SECTION:FINAL_SUMMARY:END -->
