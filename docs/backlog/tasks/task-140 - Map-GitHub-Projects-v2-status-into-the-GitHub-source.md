---
id: TASK-140
title: Map GitHub Projects v2 status into the GitHub source
status: To Do
assignee: []
created_date: '2026-09-25 16:26'
labels:
  - work-queue
milestone: m-1
dependencies: []
documentation:
  - docs/work-queue-tui-proposal.md
  - skills/worklease-workflow/references/source-providers/github-issues.md
priority: medium
type: feature
ordinal: 62000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
GitHub Issues has only open/closed, so the GitHub adapter cannot represent In Progress, Blocked, or In Review, and Start work on a GitHub source can only claim (plan §8). Teams that run their workflow on a GitHub Projects v2 board keep that state in a single-select Status field (or similar) on the project item, which the queue ignores today. Plan §17 deferred this for S6 because it needs the `project` token scope and per-project field discovery. The user asked for it on 2026-09-25 during TASK-134 (S8 intake).

This extends the existing built-in GitHub adapter; it is not a new provider and does not change GitHub claim keys (D24). An issue can belong to several projects and a project can hold items from many repositories, draft issues, and pull requests, so the binding must name exactly one project and field. Projects are owned by a user or organization, independently of the repository.

Reversing the §17 deferral means updating §17, D2/D14 as needed, and the §7 GitHub column in the same commit as the decision. GHES support for Projects v2 is unprobed.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 docs/work-queue-tui-proposal.md records the decision in §2, §16, and §17 (replacing the S6 deferral), and a §3 evidence subsection from a live probe on a user-approved synthetic project: field and option discovery, item pagination, whether field changes advance issue or item `updatedAt`, an issue in multiple projects, draft and pull-request items, required token scopes, and rate-limit cost.
- [ ] #2 A GitHub source can bind to exactly one Projects v2 project (owner plus project number, with the node ID recorded) and one single-select status field in queue.yaml; the binding is validated by discovery and a changed or missing project, field, or option disables the mapping with a structured diagnostic rather than inferring state.
- [ ] #3 Configured field options map to queue workflow states (for example In Progress, Blocked, In Review, Done); unmapped options stay raw, and the raw option name is shown beside the normalized state.
- [ ] #4 Issues outside the bound project, draft issues, and pull-request items are handled explicitly: drafts and pull requests never become claimable issues, and an issue without a project item reports its state as unmapped, not free or ready.
- [ ] #5 Readiness and completion still follow D27: a project Done option does not override issue state, and conflicts between issue state and project status are shown rather than silently resolved.
- [ ] #6 Setup requests read-only project scope first; status writes require explicitly enabled write scope, and capabilities are rediscovered after scope changes.
- [ ] #7 Start work and focused state edits can set the mapped project field through the §8 recovery pipeline with read-back of the exact item and field value; the transition is unconditional and declared as such.
- [ ] #8 GitHub claim keys and existing sources without a project binding behave exactly as before, verified by the existing identity vectors and tests.
- [ ] #9 Tests cover option rename or deletion, an issue in multiple projects, project permission loss, pagination interruption, and lost write responses.
- [ ] #10 User-facing queue docs describe the binding, required scopes, and limits.
<!-- AC:END -->
