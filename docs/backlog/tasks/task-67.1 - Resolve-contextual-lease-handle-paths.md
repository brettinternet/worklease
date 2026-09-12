---
id: TASK-67.1
title: Resolve contextual lease handle paths
status: To Do
assignee: []
created_date: '2026-09-12 02:04'
labels:
  - cli
  - security
dependencies: []
references:
  - src/worklease/execution_context.py
  - src/worklease/sqlite.py
  - src/worklease/lease_file.py
  - tests/test_execution.py
parent_task_id: TASK-67
priority: high
type: enhancement
ordinal: 74000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-67 makes a context-scoped lease handle the CLI default. This slice adds the pure resolution logic as its own module so the CLI wiring in TASK-67.2 builds on tested behavior and the identity rules are reviewable on their own. The location and identity rules are settled in the TASK-67 description; read it first and do not redesign them.

Reuse `execution_context._git_output` (or extract a small shared helper) for the git probe and `sqlite.secure_directory` for the 0700 directory rather than duplicating them. `tests/test_execution.py` already shows how to build temporary repositories and linked worktrees with `git init` and `git worktree add`. This subtask changes no CLI behavior.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A new module (suggested `src/worklease/lease_context.py`) exposes a function that returns the canonical context root for a caller directory: the resolved Git top level with `GIT_*` routing variables ignored, or the resolved caller directory when git is missing, the directory is not inside a work tree, or the probe fails.
- [ ] #2 A function maps a home override and caller directory to the handle path `<lease_home(home)>/context-leases/<sha256 hex of the root path in UTF-8>.lease`, and the module docstring documents the digest rule.
- [ ] #3 The handle path is identical for the worktree root, a nested subdirectory, and a symlinked spelling of the same worktree; it differs between a main worktree and a linked worktree, and between two distinct non-Git directories.
- [ ] #4 A separate prepare-for-write function creates `context-leases/` with mode 0700 through `secure_directory` and rejects a symlink, non-directory, or foreign-owned path with a non-secret `LeaseError` (exit code 64); resolving a path for reading creates nothing on disk.
- [ ] #5 `tests/test_lease_context.py` covers the criteria above with temporary repositories from `git init` and `git worktree add`, skips Git cases when `git` is unavailable, and `mise run lint`, `format-check`, `test`, and `typecheck` pass.
<!-- AC:END -->
