---
id: TASK-85.4
title: Select and prove the SQLite implementation
status: To Do
assignee: []
created_date: '2026-09-12 03:22'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.2
  - TASK-85.3
references:
  - src/worklease/sqlite.py
  - src/worklease/store.py
  - .github/workflows/release.yml
parent_task_id: TASK-85
priority: high
type: spike
ordinal: 96000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Worklease needs transactional local persistence and should avoid CGO when that does not weaken correctness or releases. Evaluate a pure-Go SQLite driver first against the actual durability, concurrency, cancellation, migration, and cross-build needs of the new design. This is a Go implementation choice, not a requirement to preserve the Python database.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A bounded executable comparison evaluates the preferred pure-Go driver and, if necessary, a CGO alternative for WAL, synchronous durability, busy handling, immediate writes, rollback, read-only access, context cancellation, and concurrent processes.
- [ ] #2 The selected driver supports Linux amd64/arm64 and macOS amd64/arm64 builds and tests with a documented version pin and license.
- [ ] #3 A fresh versioned schema and migration policy are documented without requiring import or upgrade of the Python database.
- [ ] #4 Crash, lock-contention, and multi-process tests demonstrate that committed lease transitions are durable and partial transitions are not observable.
- [ ] #5 The decision and evidence are recorded so later agents use one driver and do not reopen the choice without contrary test results.
<!-- AC:END -->
