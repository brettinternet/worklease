---
id: TASK-107.3
title: Implement the hosted single-writer lock
status: To Do
assignee: []
created_date: '2026-09-14 00:37'
updated_date: '2026-09-14 00:37'
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
A hosted SQLite authority must never be served by two live processes: a rolling deploy, blue-green cutover, or hostname pointing at two machines would create two writable databases with one authorityId. SQLite transaction serialization does not enforce one serving process, and a startup-only expiring lease would let a paused server resume after another adopted the database. The design mandates an exclusive process-lifetime `flock` on a stable lock file in the hosted home, verified after locking with the pinned device/inode technique already used in `internal/handle`, held by `serve` for its lifetime and by every offline hosted-writer command (init, restore, bootstrap reissue, retirement).

Ordinary local CLI homes keep their existing concurrency model. Do not add a blanket process-lifetime lock, a generic lock registry, or any change to concurrent local CLI and watch behavior. The lock is a local safety check for one volume; it does not protect against independent writable clones, and SQLite WAL requires a single-host filesystem.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A hosted-writer lock helper acquires an exclusive advisory lock on a stable file in the home, verifies the opened descriptor device and inode against the directory entry after locking, never unlinks or replaces the lock file while held, and reports a distinct reason when the lock is already held.
- [ ] #2 A second `serve` against a locked home and an alternate hosted-writer command both fail before opening the database for writes; after the holder exits, takeover succeeds and tests verify stable inode identity across the handoff.
- [ ] #3 A holder paused with SIGSTOP retains the lock, a competitor is refused during the pause, and the holder resumes safely; tests cover pause, resume, and takeover after exit.
- [ ] #4 Local CLI commands on an ordinary home never take this lock; the existing concurrent local CLI and watch tests are unchanged.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci` passes on the final commit
<!-- DOD:END -->
