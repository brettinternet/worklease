---
id: TASK-153.3
title: Resolve one claim authority per source
status: To Do
assignee: []
created_date: '2026-09-29 02:25'
updated_date: '2026-09-29 02:25'
labels:
  - work-queue
  - authority
milestone: m-1
dependencies:
  - TASK-153.1
  - TASK-153.2
references:
  - internal/config/queue.go
  - internal/cli/authority_context.go
  - internal/cli/queue_init.go
documentation:
  - docs/queue.md
  - docs/remote-claim-authority.md
parent_task_id: TASK-153
priority: medium
type: feature
ordinal: 96000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The claim authority is configured per view, but it is a property of the claim domain: every claimant of a source must use the same authority or their claims do not contend. Per-view authority causes three problems:

- One source can be listed under two views with different authorities, splitting its claims.
- `queue init --authority` refuses a source whose authority differs from the existing default views (`view authority differs from --authority`), so personal projects on the local authority and team projects on a remote one cannot share the default views.
- The view authority and the checkout binding in `bindings.yaml` select an authority for the same checkout by different mechanisms. Launch (`run --expect-authority`) and MCP (startup profile check) detect a mismatch; a plain `worklease acquire` in the checkout never consults the view.

Authority is already a claim input, so changing it goes through identity confirmation today.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Each source resolves exactly one claim authority; configuration that places one source under two authorities is rejected with an error naming the source.
- [ ] #2 For a checkout-backed source, queue claim actions and plain `acquire`/`verify` in that checkout or its linked worktrees resolve the same authority, or queue claim actions are refused with a named mismatch reason.
- [ ] #3 Projects on different authorities share one set of filter views; a cross-project scope spanning authorities shows each source's claim state from that source's authority and routes its claims there.
- [ ] #4 Changing a source's authority still requires identity confirmation before claim actions are available.
- [ ] #5 Existing `queue.yaml` files with per-view authority keep loading or fail with a documented migration step; docs/queue.md, docs/config-schemas/queue.schema.json, and CHANGELOG.md are updated.
<!-- AC:END -->
