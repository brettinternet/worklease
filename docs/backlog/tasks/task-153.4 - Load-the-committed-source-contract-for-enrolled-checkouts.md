---
id: TASK-153.4
title: Load the committed source contract for enrolled checkouts
status: To Do
assignee: []
created_date: '2026-09-29 02:25'
updated_date: '2026-09-29 02:25'
labels:
  - work-queue
milestone: m-1
dependencies:
  - TASK-153.1
  - TASK-153.2
references:
  - internal/cli/queue_init.go
  - internal/config/queue.go
  - internal/config/queue_identity.go
documentation:
  - docs/queue.md
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-153
priority: medium
type: feature
ordinal: 97000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
TASK-151 lets a repository commit `.config/worklease/queue-sources.yaml`, but only `queue init` reads it, and adoption copies the sources into each user's owner-private `queue.yaml` with no link back. That is safe for a single user and wrong for a team: the claim domain is the one value every collaborator must share, and when the committed domain changes, each user who does not re-run init keeps claiming under the old domain. Their claims silently stop contending with teammates', and no gate fires because their private configuration did not change.

Load the contract at runtime instead, while keeping the D10 trust boundary. The file stays limited to the TASK-151 allowlist (IDs, adapter, workflow, claims), so it never supplies checkouts, credentials, executables, launch argv, network consent, or authority. Enrollment in owner-private configuration is the user's trust decision; an unenrolled checkout is never read. Read the file from the enrolled checkout, not from the current linked worktree, so branches checked out in different worktrees cannot disagree on the claim domain.

This revises D10 ("v1 reads no repository-provided queue configuration") and TASK-151 AC #2 ("no command other than queue init reads the file"). A gitignored in-repo override was considered and rejected: any process in the worktree can write it, and it does not follow into new worktrees.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 For a project the user enrolled in owner-private configuration, the TUI, `queue query`, `queue next`, and MCP `queue_next` read `.config/worklease/queue-sources.yaml` from the enrolled checkout (not the current linked worktree) and apply its source IDs, adapters, workflows, and claim domains at runtime; an unenrolled checkout's file is never read.
- [ ] #2 The TASK-151 allowlist and bounds are unchanged, and every privileged key is still rejected by name.
- [ ] #3 A contract change to any claim input disables claim actions for the affected source until `queue identity confirm`, while reads continue; no path lets a user keep claiming under a superseded claim domain.
- [ ] #4 Owner-private configuration cannot override the contract's claim domain; any contract field it may override is documented with its precedence.
- [ ] #5 A missing or malformed contract marks only that project's sources incomplete with a named diagnostic; other projects are unaffected.
- [ ] #6 Sources adopted by copy under TASK-151 keep working, and a documented, previewed step converts them to contract-backed sources.
- [ ] #7 docs/work-queue-tui-proposal.md records the revised D10, including the rejected gitignored override and why; docs/queue.md and CHANGELOG.md are updated.
<!-- AC:END -->
