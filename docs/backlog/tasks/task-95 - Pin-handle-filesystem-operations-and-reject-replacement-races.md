---
id: TASK-95
title: Pin handle filesystem operations and reject replacement races
status: Done
assignee:
  - '@pi'
created_date: '2026-09-12 22:42'
updated_date: '2026-09-12 23:34'
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
- [x] #1 Handle reads, writes, removals, and lock acquisition bind to a validated pinned directory and fail closed if the pathname identity changes
- [x] #2 All handle and lock opens use no-follow semantics and validate the opened descriptor type, owner, mode, and link count
- [x] #3 Deterministic tests cover ancestor substitution and parent or lock replacement between validation, lock acquisition, and handle persistence
- [x] #4 CLI and MCP handle isolation and transfer lock-order tests pass on Linux and macOS
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Replace pathname-based handle filesystem access with a descriptor-relative pinned-directory abstraction that walks ancestors without following symlinks, validates the private parent and opened leaf descriptors, and detects parent/leaf identity replacement.
2. Bind CLI and MCP lifecycle reads, writes, and removals to the pinned directory and lock inode held by each acquired Lock, including deterministic multi-handle lock lookup for transfer. Preserve safe one-operation wrappers for metadata and tests.
3. Add deterministic race hooks and regression tests for ancestor/parent substitution, lock replacement, and replacement during handle persistence while preserving atomic write, fsync, and stable lock-file behavior.
4. Run focused handle/CLI/MCP tests, Linux and macOS cross-build checks, all repository quality gates and hooks, then independently review the filesystem race boundary before finalizing.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Review reproduction passed with a temporary Go overlay: hold the original exclusive lock, rename its parent, recreate the parent, acquire a second exclusive lock at the identical pathname, then successfully write/read the replacement handle. Durable reproduction steps and review scope are in docs/reviews/go-rewrite-boundaries-follow-up.md.

Implemented descriptor-relative handle and lock I/O with component-wise no-follow parent traversal, opened-descriptor validation, pinned lock-bound lifecycle operations, atomic *at persistence, and parent/lock identity rechecks. Updated CLI and MCP lifecycle/recovery/transfer callers. Added deterministic regressions for ancestor symlinks, parent substitution during lock acquisition and before persistence, lock-leaf replacement, sibling-lock misuse, and Darwin alias canonicalization. Validation so far: focused and race tests pass for internal/handle, internal/cli, and internal/mcp; Linux amd64 and Darwin arm64 CGO-disabled builds pass; mise lint, format-check, test, and typecheck pass.

Final verification: implementation commit fedce72. mise run lint, format-check, test, and typecheck passed on macOS; go test -race passed for internal/handle, internal/cli, and internal/mcp; the focused handle plus CLI/MCP handle-isolation and transfer suite passed in golang:1.27-bookworm on Linux; Linux amd64 and Darwin arm64 CGO-disabled builds passed. Pre-commit hooks passed. Independent filesystem-race review found three defects (Darwin alias ordering, post-flock parent identity, and sibling lock binding); all were fixed and the follow-up review found only a test self-skip, also fixed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Pinned handle and lock operations to validated no-follow directory descriptors, bound CLI/MCP lifecycle persistence to the held sibling lock, and added deterministic replacement-race and cross-platform lock-order coverage. Verified by macOS full/race suites, Linux focused handle/isolation/transfer tests, cross-builds, repository quality gates, hooks, and independent review. Implementation commit: fedce72.
<!-- SECTION:FINAL_SUMMARY:END -->
