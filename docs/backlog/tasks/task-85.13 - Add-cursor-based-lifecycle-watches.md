---
id: TASK-85.13
title: Add cursor-based lifecycle watches
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 05:51'
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
