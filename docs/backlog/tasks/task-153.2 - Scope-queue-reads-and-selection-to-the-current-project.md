---
id: TASK-153.2
title: Scope queue reads and selection to the current project
status: To Do
assignee: []
created_date: '2026-09-29 02:25'
updated_date: '2026-09-29 02:25'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-153.1
references:
  - internal/cli/queue_command.go
  - internal/cli/queue_init.go
  - internal/mcp/mcp.go
  - internal/queueui/model.go
documentation:
  - docs/queue.md
  - docs/queue-tui.md
parent_task_id: TASK-153
priority: high
type: feature
ordinal: 95000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Views are the only way to segment the queue, and a view bundles a source set with a filter and an authority. `queue init` appends each newly configured checkout's source to the shared default views, the TUI opens on `Views[0]` whatever the working directory, and neither `queue next` nor MCP `queue_next` has any notion of the current project. With several projects configured, the documented agent loop (`queue next --view Ready --claim`) run in repo A can claim and start repo B's item, and the only workaround is one view per project per filter.

Sources already carry what is needed to match a checkout: Backlog.md and Beads sources have `checkout`, and GitHub sources have `host`/`repository`, which can be matched against the checkout's `origin` (as `queue init` already does). Linear and external sources have no checkout link today.

Resolve the project from any linked worktree using the repository resolution from the worktree-bindings subtask, so an agent in `.worktrees/<name>` gets its own project's scope.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 From a configured project's main checkout or any of its linked worktrees, the TUI, `queue query`, and `queue next` (with and without `--claim`) default to that project's sources: Backlog.md and Beads sources whose checkout is that repository, and GitHub sources matching its `origin`.
- [ ] #2 Cross-project selection requires explicit opt-in through a CLI flag and a TUI toggle; outside any configured project the default is all sources; human and JSON output name the effective scope.
- [ ] #3 MCP `queue_next` applies the same default scope for the checkout the server was started in, and cross-project selection requires an explicit argument.
- [ ] #4 Linear, external, and other sources without a checkout can be associated with a project in owner-private configuration; unassociated sources appear only in the cross-project scope.
- [ ] #5 Views filter within the effective scope, so one set of Ready/Mine/Claimed/All views serves every project and `queue init` in a new project needs no view edits; existing `queue.yaml` files load unchanged, and a view that lists sources still restricts to them.
- [ ] #6 A regression test shows `queue next --view Ready --claim` run from checkout A never selects or claims checkout B's item when both sources are in the view.
- [ ] #7 docs/queue.md, docs/queue-tui.md, docs/mcp.md, docs/cli-reference.md, and skills/worklease-workflow describe project scope; CHANGELOG.md Unreleased has an entry.
<!-- AC:END -->
