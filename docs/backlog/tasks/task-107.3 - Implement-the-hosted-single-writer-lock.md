---
id: TASK-107.3
title: Implement the hosted single-writer lock
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 01:03'
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
- [ ] #1 A durable hosted-home marker is recognized before any database writer or migration opens. Direct local authority mutations refuse with remote-profile guidance before opening a marked home even when the lock is free. Only `serve` and explicit offline hosted administration or maintenance entry points may write or migrate it, and each acquires the hosted lock first.
- [ ] #2 The lock helper takes an exclusive advisory lock on a stable file, verifies the opened descriptor device and inode against the directory entry, never replaces the file while held, and returns a distinct lock-held reason.
- [ ] #3 A second server and every unapproved hosted writer fail before opening the database while the lock is held; approved takeover succeeds after exit and retains stable inode identity, while direct local mutations remain refused after the lock becomes free.
- [ ] #4 A SIGSTOP-paused holder retains ownership, a competitor is refused during the pause, and resume plus exit handoff is safe.
- [ ] #5 Ordinary local homes do not acquire the hosted process-lifetime lock, and existing concurrent local CLI, migration, and watch behavior remains unchanged.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
