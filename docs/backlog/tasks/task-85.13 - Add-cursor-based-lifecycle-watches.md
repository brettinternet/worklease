---
id: TASK-85.13
title: Add cursor-based lifecycle watches
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 04:06'
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
Agents and supervisors currently poll status or retry acquisition to learn that a resource freed up or that the authority changed. Contract section 11 defines `watch`: wait from a cursor, or on a resource until it is free or changes, with a bounded timeout, built on the events seq, honest about gaps, and cheap for many local waiters. TASK-81 captured the need; this task implements it in Go.

Read first: contract sections 4 (watch row), 7.12, 11 (watch and events), 12 (the watch tool limits, wired later), 18 (watch sketch). Evidence: the TASK-81 acceptance criteria; `tests/test_history.py` test_events_cursor_preserves_ties_and_concurrent_insert_semantics. Patterns: `../hum/internal/cli/wait_test.go` and `../hum/internal/daemon` for bounded waits and cancellation.

Deliver in `internal/watch`: `Wait(ctx, store, Request{Cursor, Resources, Until, Timeout})` implementing an immediate gap result when the cursor is below pruned_through_seq; an immediate free result for `--until free` when the resource is free; the matching rules (any event when no resources are given, intersection when they are, and for `--until free` the epoch-ending kinds followed by a freeness re-check); adaptive polling of `MAX(seq)` from 50 ms to 500 ms with no transaction held between polls; a timeout result carrying the latest cursor; context cancellation; no goroutine leaks (verified in tests). Deliver in `internal/cli`: `watch` with `--cursor` or `-r ... --until free|change` and `--timeout` (default 30s, maximum 1h), text and JSON, exit 0 for a match, exit 0 with timedOut true on timeout (documented in help), exit 64 for invalid combinations.

Owned paths: `internal/watch`, `internal/cli/watch.go` and tests. Out of scope: MCP tool wiring (TASK-85.15), notification through the filesystem or sockets.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Wake-up tests prove a waiter on --cursor returns the first matching event within 600 ms of a subprocess appending it, resource filtering ignores events for other resources, and the returned cursor equals that event's seq.
- [ ] #2 Free tests prove `-r R --until free` returns immediately when R is free, wakes when the holder releases, keeps waiting through a transfer (still held), keeps waiting when an expired holder is replaced by a new acquire (still held), and wakes when gc retires the expired holder; `--until change` returns on any event touching R including renewals.
- [ ] #3 Gap and error tests prove a cursor below pruned_through_seq returns the gap result immediately without waiting, a malformed cursor fails cursor-invalid, and --cursor combined with --until fails invalid-argument.
- [ ] #4 Concurrency tests prove ten waiters in one process and three waiter subprocesses all wake on one event, no transaction is open between polls (a concurrent BEGIN IMMEDIATE writer never observes busy), context cancellation returns within 600 ms, and a goroutine count check shows no leaks after 100 cancelled waits.
- [ ] #5 Timeout tests prove the default and maximum bounds and that the timeout result includes the latest cursor; redaction assertions show watch output contains no tokens, hashes, checkpoints, or evidence; `mise run ci-go` passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
