---
id: TASK-85.13
title: Add cursor-based lifecycle watches
status: In Progress
assignee:
  - '@pi-01a09633'
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 15:22'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.9
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - tests/test_history.py
  - ../hum/internal/cli/wait_test.go
  - TASK-81
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 105000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement bounded read-only lifecycle watches using the event and state contracts in section 11. Own internal/watch and watch CLI. An expired lease becomes acquirable without any new durable event; waiting for a release event alone is incorrect.

Capture initial state/cursor in one snapshot, scan matching events in order, recheck time-based transitions on every poll, and return only a position actually inspected at timeout. Bind continuations to the authority/feed/filter and expose gaps and unresolved predecessor metadata. Do not introduce notification sockets or filesystem watchers.

Evidence and patterns (the amended contract is normative): TASK-81 (closed as superseded) records the watch intent; `tests/test_history.py` test_events_cursor_preserves_ties_and_concurrent_insert_semantics shows the concurrent-insert cursor expectations. hum patterns for bounded waits and cancellation: `internal/cli/wait_test.go` and `internal/daemon`.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A subprocess event wakes matching waiters within the polling bound; unrelated events are skipped without skipping a later matching event.
- [ ] #2 Until-free waits for all resources to be absent/expired, wakes on expiry with no writes, keeps waiting through transfer/reacquire, and separately reports unresolved predecessors; until-change observes lazy expiry too.
- [ ] #3 Tests cover no missed release between initial snapshot/polling, no skipped event arriving at timeout, and cursor binding to authority/feed/filter including GC gaps and malformed inputs.
- [ ] #4 Many in-process/subprocess waiters do not hold transactions between polls, honor cancellation/deadlines and leak no goroutines; clock regression never reports false freeness.
- [ ] #5 Default/maximum timeout and empty-feed durable cursors are tested in text/JSON; watches create no state and expose no private payloads; mise run ci-go passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add internal/watch with authority-bound event continuations, atomic initial state/cursor snapshots, short read transactions, adaptive bounded polling, expiry-aware free/change evaluation, gap handling, and unresolved-predecessor metadata.
2. Wire worklease watch CLI validation and text/JSON output for cursor and resource until modes, enforcing the 30s default and 1h maximum without opening or creating state for malformed input.
3. Add domain and CLI/subprocess tests for matching/unrelated events, lazy expiry, transfer/reacquire, cursor binding/gaps, timeout scanned positions, concurrency/cancellation, clock regression, empty authorities, redaction, and no-write behavior.
4. Run focused Go tests and the full repository quality gates, review the diff, and record criterion-specific evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed with Worklease resource backlog-md:/Users/brett/dev/me/worklease/.git:docs/backlog:TASK-85.13 (claim 5a5a7d0b482f4da2f416ecf00d4326e0; local coordination only, provider writes are not fenced).
<!-- SECTION:NOTES:END -->
