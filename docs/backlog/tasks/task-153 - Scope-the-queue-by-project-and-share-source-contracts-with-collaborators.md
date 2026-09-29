---
id: TASK-153
title: Scope the queue by project and share source contracts with collaborators
status: To Do
assignee: []
created_date: '2026-09-29 02:24'
updated_date: '2026-09-29 02:25'
labels:
  - work-queue
milestone: m-1
dependencies: []
references:
  - internal/cli/queue_init.go
  - internal/cli/queue_command.go
  - internal/config/queue.go
  - internal/config/profile.go
  - internal/handle/handle.go
documentation:
  - docs/work-queue-tui-proposal.md
  - docs/queue.md
  - docs/remote-claim-authority.md
priority: high
type: feature
ordinal: 93000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
## Why

A user working across 5-10 projects, each with its own Backlog.md backlog until it moves to GitHub Issues or Linear, has no good way to segment the queue by project. A view bundles three unrelated things: a source set, a filter, and a claim authority. Segmenting by view needs one view per project per filter (20-40 views). Worse, `queue init` appends every newly configured checkout's source to the shared default Ready/Mine/Claimed/All views (`queueInitAddViews`), the TUI opens on `Views[0]` whatever the working directory, and the agent-loop guidance is `queue next --view Ready --claim`. An agent working in repo A can therefore claim repo B's task.

Collaboration adds three gaps:

- **Claim domain drift.** TASK-151 lets a repository propose sources, but adoption copies them into each user's owner-private `queue.yaml`. When the team changes the committed claim domain, everyone who does not re-run init keeps claiming under the old one, and their claims silently stop contending with teammates'.
- **Authority per view.** One source can sit under views with different authorities; `queue init --authority` refuses a source whose authority differs from the existing default views, so personal (local) and team (remote) projects cannot share views; and view authority duplicates the checkout-to-profile selection in `bindings.yaml`.
- **Worktrees.** `bindings.yaml` is keyed by `git rev-parse --show-toplevel`, so a binding on the main checkout does not apply in a linked worktree. Probe on 2026-09-29: `profile show` reported `local (selected by binding)` in the main checkout and `local (selected by local)` in a linked worktree. With a remote team authority bound to the checkout and no default profile, an agent in a Worktrunk worktree claims against the local authority and never contends with teammates.

## Direction

- Derive scope from the checkout (owner-private resolution, following the `bindings.yaml` pattern) instead of encoding it in views. Views become filters within that scope, and cross-project selection is explicit.
- Split configuration by who must agree on it. The committed `.config/worklease/queue-sources.yaml` becomes the shared source contract (IDs, adapter, workflow, claim domain), loaded at runtime only for checkouts the user enrolled in owner-private config. Enrollment, `me`, accounts, credentials, network consent, launch, executables, authority profiles, and views stay owner-private. This deliberately revises D10 and TASK-151 AC #2.
- Rejected: a gitignored in-repo override file. Any agent or process in the worktree can write it, so it cannot hold launch argv, executables, network consent, or authority (the reason for D10), and it does not follow into new worktrees. Owner-private configuration keyed by checkout is the local override layer.

Subtasks are ordered by risk: worktree bindings and project scoping fix silent misrouting of claims; per-source authority and the loaded contract follow.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 With several configured projects across local and remote authorities, one set of filter views serves all of them, and each checkout (including its linked worktrees) sees only its own work by default.
- [ ] #2 An agent loop in one project never selects or claims another project's item without explicit cross-project opt-in.
- [ ] #3 Collaborators who pull a changed shared source contract cannot keep claiming under the superseded claim domain; claim actions fail closed until identity is confirmed.
- [ ] #4 Executable, credential-bearing, network-consent, and authority configuration remains owner-private; no repository file, committed or gitignored, can supply it.
<!-- AC:END -->
