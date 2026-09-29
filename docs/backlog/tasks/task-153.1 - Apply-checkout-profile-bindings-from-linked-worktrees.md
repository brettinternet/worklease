---
id: TASK-153.1
title: Apply checkout profile bindings from linked worktrees
status: Done
assignee:
  - '@pi'
created_date: '2026-09-29 02:25'
updated_date: '2026-09-29 04:38'
labels:
  - work-queue
  - authority
milestone: m-1
dependencies: []
references:
  - internal/config/profile.go
  - internal/handle/handle.go
  - internal/cli/authority_context.go
  - internal/cli/profile_commands.go
documentation:
  - docs/remote-claim-authority.md
parent_task_id: TASK-153
priority: high
type: bug
ordinal: 94000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`bindings.yaml` maps a checkout root to an authority profile, and the root comes from `handle.ContextRoot` (`git rev-parse --show-toplevel`). A linked worktree has its own top level, so the main checkout's binding does not apply there. Probe on 2026-09-29: after `profile bind local` in the main checkout, `profile show` reported `local (selected by binding)` there and `local (selected by local)` in a linked worktree of the same repository.

Agents normally work in Worktrunk worktrees under `.worktrees/<name>`. With a remote team authority bound to the checkout and no default profile, every such agent acquires against the local authority, and its claims never contend with teammates'. Nothing reports the difference.

Contextual handles are intentionally keyed per worktree root (docs/cli-reference.md) and must stay that way; only the authority binding is repository-scoped. Project scoping and contract loading in TASK-153 also need the same worktree-to-repository resolution.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 In a linked worktree, profile selection uses the main checkout's binding: `profile show` reports it as selected by binding, and `acquire`, `verify`, `heartbeat`, `release`, `run`, and queue claim actions use that authority.
- [x] #2 `profile bind` and `profile unbind` run in a linked worktree change the binding of the repository's main checkout.
- [x] #3 An existing `bindings.yaml` entry for a linked worktree path that names a different profile than its main checkout fails closed with a named error instead of silently choosing one.
- [x] #4 Contextual handles remain per worktree root; tests cover binding selection and handle scope from both a main checkout and a linked worktree.
- [x] #5 docs/remote-claim-authority.md states that a binding applies to every worktree of its repository; CHANGELOG.md Unreleased has a Fixed entry.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Resolve repository binding roots independently of per-worktree contextual handle roots, preserving non-Git behavior and sanitized Git discovery.
2. Apply repository binding resolution to profile selection and bind/unbind; fail closed on conflicting legacy linked-worktree bindings without changing explicit authority precedence.
3. Add focused main/linked-worktree binding, conflict, and handle-isolation coverage; update authority documentation and Unreleased Fixed.
4. Run focused repeated race tests and required multi-package gates, obtain one independent authority-routing review, fix concrete scoped findings, and commit on main.

Validation scope approved by user: repair the unrelated TestBacklogInvalidatedInFlightListCannotRestoreStaleEdges FIFO release handshake rather than ignore its full-suite timeout. Use a persistent FIFO descriptor and an explicit newline signal instead of transient EOF.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implementation worker timed out during full-suite validation; parent took over. Focused changed tests pass with -race -count=3; lint, format-check, and vet pass. First full-suite run exposed a FIFO-release timeout in the pre-existing queue invalidation test; user approved a narrow deterministic synchronization fix. Parent performed the single scoped routing review after the worker timeout prevented the planned reviewer stage.

Committed implementation on main as 3456bb9 (Apply profile bindings across linked worktrees).
AC1/2: TestProfileBindingSelectionAndMutationsSpanLinkedWorktrees verifies linked profile show, shared remote authority construction, and bind/unbind against the main checkout entry. TestLinkedCheckoutAuthorityUsesRepositoryBindingWithoutChangingViewAgreement verifies queue agreement uses the same repository binding and rejects mismatch. Existing command lifecycle suites passed.
AC3: TestRepositoryBindingSelectionRejectsConflictingLegacyWorktreeBindings covers differing legacy bindings, missing main binding, matching bindings, explicit precedence, and actionable profile-binding-conflict; CLI test verifies named error propagation.
AC4: TestContextRootResolvesSymlinksGitSubdirectoriesAndLinkedWorktrees verifies main/linked binding roots, hostile Git environment isolation, and distinct contextual handles; non-Git behavior is covered in TestContextRootAndContextualPathAreStableSessionAndAuthorityScoped.
AC5: Updated remote authority documentation and Unreleased Fixed; mise run doc-test passed.
All changed task tests passed with -race -count=3. User-approved FIFO synchronization fix passed -race -count=30. Final gates passed: mise run lint, format-check, typecheck, test (real providers enabled), race (both partitions, exit 0), doc-test, and staged hooks plus commit hooks. Foreground race commands exceeded their command windows; a logged background run completed successfully in 1019.75s.
Review: parent reviewed executor changes for authority routing, legacy conflict handling, bind/unbind semantics, and handle isolation; no remaining scoped defects. No remaining blocker. Next dependency-ready subtask is TASK-153.2; not started.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Repository bindings now apply to linked worktrees while handles remain worktree-scoped. Conflicting legacy bindings fail closed with profile-binding-conflict. Implemented and committed on main in 3456bb9, including the user-approved FIFO test fix. Full test/race suites, static checks, documentation examples, focused repeated race tests, and hooks passed.
<!-- SECTION:FINAL_SUMMARY:END -->
