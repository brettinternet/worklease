---
id: TASK-107.3
title: Implement the hosted single-writer lock
status: Done
assignee:
  - '@brett'
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 03:03'
labels:
  - remote-authority
dependencies: []
references:
  - internal/handle/handle.go
  - internal/store/driver_identity_linux.go
  - internal/store/driver_identity_darwin.go
documentation:
  - docs/remote-claim-authority.md
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
parent_task_id: TASK-107
priority: high
type: feature
ordinal: 135000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Implement a hosted-home marker and an exclusive process-lifetime OS lock for every approved hosted writer. Hosted-home recognition occurs before any writer or migration opens the database. Direct local authority mutations against a marked hosted home refuse with remote-profile guidance even when no server holds the lock, because local policy would bypass remote admission and recovery rules. Only `serve` and explicit offline hosted administration or maintenance entry points may open a marked home for writes, and they acquire the hosted lock first. Migrations run only through those approved entry points.

Use the stable lock-file and pinned device/inode technique already used in `internal/handle`. Never unlink or replace the lock file while held. Ordinary local homes keep their current concurrency model.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A durable hosted-home marker is recognized before any database writer or migration opens. Direct local authority mutations refuse with remote-profile guidance before opening a marked home even when the lock is free. Only `serve` and explicit offline hosted administration or maintenance entry points may write or migrate it, and each acquires the hosted lock first.
- [x] #2 The lock helper takes an exclusive advisory lock on a stable file, verifies the opened descriptor device and inode against the directory entry, never replaces the file while held, and returns a distinct lock-held reason.
- [x] #3 A second server and every unapproved hosted writer fail before opening the database while the lock is held; approved takeover succeeds after exit and retains stable inode identity, while direct local mutations remain refused after the lock becomes free.
- [x] #4 A SIGSTOP-paused holder retains ownership, a competitor is refused during the pause, and resume plus exit handoff is safe.
- [x] #5 Ordinary local homes do not acquire the hosted process-lifetime lock, and existing concurrent local CLI, migration, and watch behavior remains unchanged.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add secure hosted-home marker creation/recognition and a store-local nonblocking exclusive lock with pinned directory, device, and inode identity.
2. Extend store opening with an explicit approved hosted-writer mode that acquires and retains the lock before handles, SQLite, bootstrap, or migration; reject ordinary local writes with remote-profile guidance.
3. Add same-process and subprocess coverage for refusal, contention, stable-inode handoff, pause/resume ownership, migrations, and unchanged ordinary local concurrency/read behavior.
4. Run focused tests and full quality gates, independently review acceptance risks, fix findings, finalize the backlog task, and commit.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented durable owner-private hosted markers, pre-database local-write refusal with remote-profile guidance, and an exclusive nonblocking process-lifetime flock retained by approved hosted Store opens. Lock acquisition pins and verifies the stable lock inode and the home directory identity; Close releases only after SQLite closes and never unlinks the lock. Added deterministic lock/home replacement tests, v1 migration gating, stable-inode takeover, same-process contention, subprocess SIGSTOP/resume/exit handoff, empty-home marking, and ordinary-home concurrency/no-lock coverage. Independent verification found a parent-directory replacement gap; added immediate post-lock and pre-schema pinned-home identity checks plus regression coverage. Validation: focused store tests, go test -race ./internal/store, mise run lint, mise run typecheck, and full mise run ci passed. Final independent verification passed all five acceptance criteria.

Delivery commit: 24eb317 (Add hosted authority writer lock).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added durable hosted-home recognition and stable-inode exclusive writer locking at the store boundary. Marked homes now reject direct local mutations before SQLite opens, approved hosted writers hold the lock through Store.Close, and migrations run only under that lock. Verified contention, replacement races, stable takeover, SIGSTOP ownership, migration gating, and unchanged ordinary local concurrency with focused/race tests and a passing full CI run.
<!-- SECTION:FINAL_SUMMARY:END -->
