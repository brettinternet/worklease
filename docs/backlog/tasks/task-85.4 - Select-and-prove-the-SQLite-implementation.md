---
id: TASK-85.4
title: Select and prove the SQLite driver
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 04:06'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.2
  - TASK-85.3
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/sqlite.py
  - tests/test_store.py
  - tests/test_gc.py
  - 'https://pkg.go.dev/modernc.org/sqlite'
  - 'https://pkg.go.dev/github.com/ncruces/go-sqlite3'
  - 'https://pkg.go.dev/github.com/mattn/go-sqlite3'
parent_task_id: TASK-85
priority: high
type: spike
ordinal: 96000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The contract prefers a pure-Go SQLite driver (`modernc.org/sqlite`) so releases stay CGO-free, but that preference is only safe if the driver satisfies the authority's real needs: WAL with synchronous=FULL, busy_timeout behavior across processes, BEGIN IMMEDIATE, rollback on error, read-only opens, context cancellation, AUTOINCREMENT, and cross-compilation to the four targets. This spike proves or refutes that with executable evidence and records the decision as a contract amendment so no later task reopens it.

Read first: contract sections 2 (D2, D3, D8), 8, 14, 15. Python evidence: `src/worklease/sqlite.py` (pragmas the proof of concept relied on: WAL, synchronous FULL, busy_timeout, BEGIN IMMEDIATE), `tests/test_store.py` test_concurrent_acquire_has_one_winner_and_independent_resources_proceed and test_epoch_schema_migration_rolls_back_atomically, `tests/test_gc.py` test_apply_rolls_back_on_injected_sqlite_interruption.

Method:

1. Add the candidate driver to `go.mod`. Write `internal/store/driver.go` exposing `OpenDriver(path string, readOnly bool) (*sql.DB, error)` that applies the D8 pragmas, and `internal/store/driver_test.go` that: verifies each pragma reads back; runs two `testkit.Helper` subprocesses performing 200 `BEGIN IMMEDIATE` transactions each on one table and asserts no lost updates and no surfaced lock errors with busy_timeout 10000; asserts a transaction returning an error is fully rolled back; asserts a read-only open cannot write; asserts a long-running statement is aborted within 500 ms when its context is cancelled; asserts AUTOINCREMENT never reuses a deleted id; records a simple write-transaction latency as information.
2. Cross-compile with `CGO_ENABLED=0` for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64 and record the result.
3. Crash tests: a helper subprocess commits a row and is SIGKILLed immediately after commit, and the parent reopens and asserts the row exists; another helper is SIGKILLed mid-transaction and the parent asserts no partial rows.
4. If `modernc.org/sqlite` fails any check, repeat with `github.com/ncruces/go-sqlite3` (pure Go), then `github.com/mattn/go-sqlite3` (CGO), and record which passed.
5. Record the decision as a contract section 17 amendment (procedure in section 15): driver import path and pinned version, license, DSN and pragma configuration, and evidence per check. If CGO is required, the amendment must also restate D3 and the section 14 build flags. Add a TASK-85 comment pointing to the amendment.

Owned paths: `internal/store/driver.go`, `internal/store/driver_test.go`, the driver entry in `go.mod`/`go.sum`, contract section 17 (via `backlog doc update`). Out of scope: the schema, lease logic, migration of Python data.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 internal/store driver tests pass under `go test` and `go test -race` and cover pragma read-back (journal_mode wal, synchronous full, busy_timeout 10000, foreign_keys on), BEGIN IMMEDIATE serialization across two subprocesses with no lost updates and no surfaced lock errors, rollback on error, read-only open rejecting writes, context cancellation aborting a running statement within 500 ms, and AUTOINCREMENT non-reuse.
- [ ] #2 Crash tests prove a row committed immediately before SIGKILL survives reopen and a writer killed mid-transaction leaves no partial rows.
- [ ] #3 `CGO_ENABLED=0 go build ./...` succeeds for linux/amd64, linux/arm64, darwin/amd64, and darwin/arm64 with the selected driver, or the contract amendment records why CGO is required and the builds succeed on native runners.
- [ ] #4 The selected driver is pinned in go.mod, its license is named in the task notes, and `mise run go-vuln` reports no known vulnerabilities for it.
- [ ] #5 Contract section 17 contains the amendment recording the driver decision, version, DSN and pragma configuration, and evidence; TASK-85 has a comment pointing to it; internal/store/driver.go exposes OpenDriver for TASK-85.6 to build on.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
