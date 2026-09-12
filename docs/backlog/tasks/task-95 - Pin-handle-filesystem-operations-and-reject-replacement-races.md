---
id: TASK-95
title: Pin handle filesystem operations and reject replacement races
status: To Do
assignee: []
created_date: '2026-09-12 22:42'
updated_date: '2026-09-12 22:45'
labels:
  - go-rewrite
dependencies: []
references:
  - internal/handle/handle.go
priority: high
type: bug
ordinal: 120000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Contract section 8 requires handle and lock files to use pinned directory descriptors with no-follow semantics. internal/handle currently validates only the immediate parent through trustedParent, then re-resolves pathname strings in Read, Write, Remove, and both lock openers. AcquireLock also omits O_NOFOLLOW. A renamed/replaced parent can leave a caller holding a lock on the old inode while subsequent handle operations access the replacement directory; a symlink above the immediate parent is accepted. This is a local filesystem integrity boundary, not protection from a malicious process with arbitrary access as the same UID. Implement the promised boundary across the handle lifecycle rather than claiming path checks close the race.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Handle reads, writes, removals, and lock acquisition bind to a validated pinned directory and fail closed if the pathname identity changes
- [ ] #2 All handle and lock opens use no-follow semantics and validate the opened descriptor type, owner, mode, and link count
- [ ] #3 Deterministic tests cover ancestor substitution and parent or lock replacement between validation, lock acquisition, and handle persistence
- [ ] #4 CLI and MCP handle isolation and transfer lock-order tests pass on Linux and macOS
<!-- AC:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Review reproduction passed with a temporary Go overlay: hold the original exclusive lock, rename its parent, recreate the parent, acquire a second exclusive lock at the identical pathname, then successfully write/read the replacement handle. Durable reproduction steps and review scope are in docs/reviews/go-rewrite-boundaries-follow-up.md.
<!-- SECTION:NOTES:END -->
