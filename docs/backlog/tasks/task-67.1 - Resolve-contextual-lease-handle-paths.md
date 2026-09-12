---
id: TASK-67.1
title: Resolve contextual lease handle paths
status: Done
assignee:
  - '@pi-01a09365'
created_date: '2026-09-12 02:04'
updated_date: '2026-09-12 03:06'
labels:
  - cli
  - security
dependencies: []
references:
  - src/worklease/execution_context.py
  - src/worklease/sqlite.py
  - src/worklease/lease_file.py
  - tests/test_execution.py
modified_files:
  - src/worklease/lease_context.py
  - src/worklease/sqlite.py
  - tests/test_lease_context.py
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
- [x] #1 A new module (suggested `src/worklease/lease_context.py`) exposes a function that returns the canonical context root for a caller directory: the resolved Git top level with `GIT_*` routing variables ignored, or the resolved caller directory when git is missing, the directory is not inside a work tree, or the probe fails.
- [x] #2 A function maps a home override and caller directory to the handle path `<lease_home(home)>/context-leases/<sha256 hex of the root path in UTF-8>.lease`, and the module docstring documents the digest rule.
- [x] #3 The handle path is identical for the worktree root, a nested subdirectory, and a symlinked spelling of the same worktree; it differs between a main worktree and a linked worktree, and between two distinct non-Git directories.
- [x] #4 A separate prepare-for-write function creates `context-leases/` with mode 0700 through `secure_directory` and rejects a symlink, non-directory, or foreign-owned path with a non-secret `LeaseError` (exit code 64); resolving a path for reading creates nothing on disk.
- [x] #5 `tests/test_lease_context.py` covers the criteria above with temporary repositories from `git init` and `git worktree add`, skips Git cases when `git` is unavailable, and `mise run lint`, `format-check`, `test`, and `typecheck` pass.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add pure context-root and contextual lease-path resolution using the existing Git probe and state-home rules.
2. Add a prepare-for-write boundary that creates and validates the private context-leases directory through secure_directory.
3. Add focused non-Git, Git worktree, symlink, environment, and unsafe-directory tests, then run repository quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented lease_context resolution and private directory preparation in the isolated worktree. Added focused tests for non-Git directories, nested/symlinked Git paths, linked worktrees, stripped Git routing variables, failed probes, mode 0700, and unsafe directories. Verification: mise run lint, format-check, test (312 core + 19 SDK), and typecheck all pass.

Independent review identified a directory check/use race; secure_directory now opens the directory without following symlinks and validates ownership through the descriptor. Post-merge verification on main: lint, format-check, test (316 core + 19 SDK), and typecheck all pass. Commit: a86c067.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added Git-aware contextual lease handle resolution, private context directory preparation, and descriptor-based secure directory hardening. Verified all path identity and unsafe-directory cases in tests/test_lease_context.py and passed every repository quality gate on main.
<!-- SECTION:FINAL_SUMMARY:END -->
