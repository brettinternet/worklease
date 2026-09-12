---
id: TASK-85.4
title: Select and prove the SQLite driver
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
updated_date: '2026-09-12 06:27'
labels:
  - go-rewrite
milestone: m-0
dependencies:
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
Prove a SQLite driver against the actual authority boundary before selecting it. Read D2/D3/D8 and contract sections 8, 14 and 20. Prefer modernc.org/sqlite; evaluate alternatives only if executable evidence requires it. Own internal/store/driver.go and driver tests plus driver dependency/amendment.

Prove WAL/FULL, writer serialization, rollback, cancellation, crash durability, AUTOINCREMENT, safe actual driver opens of main/WAL/SHM, and read-only observation without creating files or ignoring committed WAL. An lstat check followed by an ordinary path-based driver open is not equivalent to no-follow safety. Record capability limits and amend unsafe assumptions before later tasks rely on them.

Evidence and patterns (the amended contract is normative): `src/worklease/sqlite.py` shows the pragmas the proof of concept relied on (WAL, synchronous FULL, busy_timeout, BEGIN IMMEDIATE) and `connect_readonly` opening `?mode=ro` over a WAL database, which is the read-only behavior section 8 requires the spike to prove. Tests to reproduce in shape: `tests/test_store.py` test_concurrent_acquire_has_one_winner_and_independent_resources_proceed and test_epoch_schema_migration_rolls_back_atomically, `tests/test_gc.py` test_apply_rolls_back_on_injected_sqlite_interruption. Candidate drivers in order: `modernc.org/sqlite` (preferred, pure Go), `github.com/ncruces/go-sqlite3` (pure Go), `github.com/mattn/go-sqlite3` (CGO fallback requiring a D3 amendment).
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Driver tests read back every required pragma and prove cross-process BEGIN IMMEDIATE serialization, rollback, bounded cancellation and AUTOINCREMENT non-reuse.
- [ ] #2 Crash tests distinguish killed uncommitted writes from durable commits; ambiguous commit errors are not assumed to mean rollback.
- [ ] #3 Tests exercise main/WAL/SHM symlink and hard-link rejection, unsafe-path races, private modes, and read-only opens that see committed WAL without creating state; any required design deviation is explicitly amended.
- [ ] #4 All four target builds succeed with CGO_ENABLED=0, or an evidence-backed amendment defines CGO/native builds; the selected dependency is pinned and license/vulnerability checks recorded.
- [ ] #5 The contract amendment and TASK-85 comment record the driver, settings, evidence and limitations; OpenDriver is available to 85.6 and mise run ci-go passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
