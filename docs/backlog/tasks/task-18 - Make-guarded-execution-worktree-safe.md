---
id: TASK-18
title: Make guarded execution worktree-safe
status: To Do
assignee: []
created_date: '2026-07-14 04:41'
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
- [ ] #1 Both exec and exec-bundle accept an explicit provider working directory plus a Git-primary derivation mode, while generic execution without provider mode continues in the caller current directory.
- [ ] #2 Git-primary resolution identifies the registered primary/control worktree without assuming the Git common directory is a .git child, and rejects missing, ambiguous, bare, unregistered, cross-repository, or otherwise unsafe provider directories before starting an operation.
- [ ] #3 The resolved execution directory and resolution mode are included in the operation request identity and receipt; retrying an operation ID with a different directory fails as an idempotency conflict.
- [ ] #4 Provider child processes cannot be redirected away from the validated checkout by inherited repository-routing Git environment variables, while unrelated Git identity, configuration, and credential variables remain available.
- [ ] #5 Repository-relative WORKLEASE_HOME usage is either canonicalized to shared control-checkout state or replaced in documentation with a worktree-stable location, so linked worktrees cannot silently acquire the same resource in separate stores.
- [ ] #6 Tests cover main and linked worktrees, symlinked paths, separate Git directories, nested repositories or submodules, prunable worktrees, inherited Git routing variables, non-Git fallback behavior, and single and bundle replay conflicts.
- [ ] #7 CLI help, Python API documentation, examples, and versioned request/receipt schemas describe execution-directory semantics and continue to report local coordination rather than provider-side fencing.
<!-- AC:END -->
