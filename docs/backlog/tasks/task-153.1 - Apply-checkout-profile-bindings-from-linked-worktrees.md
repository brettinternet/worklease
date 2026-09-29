---
id: TASK-153.1
title: Apply checkout profile bindings from linked worktrees
status: To Do
assignee: []
created_date: '2026-09-29 02:25'
updated_date: '2026-09-29 02:25'
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
- [ ] #1 In a linked worktree, profile selection uses the main checkout's binding: `profile show` reports it as selected by binding, and `acquire`, `verify`, `heartbeat`, `release`, `run`, and queue claim actions use that authority.
- [ ] #2 `profile bind` and `profile unbind` run in a linked worktree change the binding of the repository's main checkout.
- [ ] #3 An existing `bindings.yaml` entry for a linked worktree path that names a different profile than its main checkout fails closed with a named error instead of silently choosing one.
- [ ] #4 Contextual handles remain per worktree root; tests cover binding selection and handle scope from both a main checkout and a linked worktree.
- [ ] #5 docs/remote-claim-authority.md states that a binding applies to every worktree of its repository; CHANGELOG.md Unreleased has a Fixed entry.
<!-- AC:END -->
