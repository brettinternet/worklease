---
id: TASK-18
title: Make guarded execution worktree-safe
status: Done
assignee: []
created_date: '2026-07-14 04:41'
updated_date: '2026-07-14 21:20'
labels:
  - git
  - worktrees
  - exec
  - safety
dependencies: []
references:
  - src/worklease/adapters/protocol.py
  - skills/worklease-source-workflow/references/provider-contract.md
  - skills/worklease-workflow/references/contract.md
modified_files:
  - src/worklease/cli.py
  - src/worklease/execution.py
  - src/worklease/sqlite.py
  - tests/test_execution.py
  - tests/test_cli.py
  - tests/test_adapters.py
  - README.md
priority: high
type: enhancement
ordinal: 19000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Ensure guarded provider mutations invoked from linked Git worktrees execute against one validated canonical provider checkout and share the same lease authority. Preserve the current working directory for generic exec commands; provider execution explicitly opts into a canonical provider directory, with a Git-primary convenience mode. Keep Git checkout metadata diagnostic-only and do not strengthen the reported provider-fencing guarantee. Coordinate request and receipt compatibility changes with TASK-12.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Both exec and exec-bundle accept an explicit provider working directory plus a Git-primary derivation mode, while generic execution without provider mode continues in the caller current directory.
- [x] #2 Git-primary resolution identifies the registered primary/control worktree without assuming the Git common directory is a .git child, and rejects missing, ambiguous, bare, unregistered, cross-repository, or otherwise unsafe provider directories before starting an operation.
- [x] #3 The resolved execution directory and resolution mode are included in the operation request identity and receipt; retrying an operation ID with a different directory fails as an idempotency conflict.
- [x] #4 Provider child processes cannot be redirected away from the validated checkout by inherited repository-routing Git environment variables, while unrelated Git identity, configuration, and credential variables remain available.
- [x] #5 Repository-relative WORKLEASE_HOME usage is either canonicalized to shared control-checkout state or replaced in documentation with a worktree-stable location, so linked worktrees cannot silently acquire the same resource in separate stores.
- [x] #6 Tests cover main and linked worktrees, symlinked paths, separate Git directories, nested repositories or submodules, prunable worktrees, inherited Git routing variables, non-Git fallback behavior, and single and bundle replay conflicts.
- [x] #7 CLI help, Python API documentation, examples, and versioned request/receipt schemas describe execution-directory semantics and continue to report local coordination rather than provider-side fencing.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Resolve a canonical provider execution directory from an explicit path or registered Git primary, rejecting unsafe, ambiguous, bare, prunable, cross-repository, and unregistered candidates. 2. Include normalized execution mode and path in request and receipt identity, preserve generic caller-directory execution, and scrub only Git repository-routing variables from provider children. 3. Cover single and bundle CLI/API behavior, linked/symlinked/separate/nested/prunable worktrees, replay conflicts, environment isolation, state-home guidance, schemas, and documentation.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Canonical main integration completed. Merged guarded execution directory and Git-primary worktree support, including CLI and bundle wiring, request/receipt identity, provider environment routing isolation, replay conflicts, linked/symlinked/separate/nested/prunable worktree handling, and worktree-stable WORKLEASE_HOME guidance. Verification: mise run lint passed; mise run format-check passed; mise run test passed for the full discovered suite; mise run typecheck passed with 0 errors; mise run hooks passed; package smoke tests passed during merge conflict resolution.

AC2 evidence detail: the full suite covers registered-primary resolution across linked, symlinked, separate-Git-dir, nested-repository, and prunable-worktree cases. Source-level validation paths reject non-Git/missing, bare, ambiguous, unregistered, cross-repository, and unsafe directories before execution; no claim is made that every negative branch has a dedicated automated test.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
TASK-18 is implemented and integrated into main. Guarded provider execution now supports explicit provider directories and Git-primary derivation while generic exec remains caller-directory based; receipts and idempotency include execution-directory identity; provider Git routing variables are isolated; linked-worktree safety, replay conflicts, CLI/schema semantics, and operational guidance are covered. Verified with the full repository quality gates and targeted package smoke tests.
<!-- SECTION:FINAL_SUMMARY:END -->
