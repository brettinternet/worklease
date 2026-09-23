---
id: TASK-128.7
title: Show claim state and check authority consistency
status: To Do
assignee: []
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 04:30'
labels:
  - work-queue
  - authority
milestone: m-1
dependencies:
  - TASK-128.2
  - TASK-128.3
references:
  - internal/authority/authority.go
  - internal/cli/authority_context.go
  - internal/config/profile.go
  - internal/lease/service.go
  - internal/lease/remote.go
  - internal/server/server.go
  - internal/resource/resource.go
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-128
priority: high
type: feature
ordinal: 17000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Browsing must show who holds what in the view's selected authority, and in this slice the queue never owns or renews a claim. Plan section 3 shows that remote Status accepts many resources but caps requests at 1 MiB and responses at 4 MiB, and that List is unpaginated. Overlays therefore batch Status and never use List. The live watch-based overlay comes later, in TASK-129.5.

D11: a worker in a checkout resolves its authority from --profile, WORKLEASE_PROFILE, the checkout binding, then the default. If that differs from the view's authority, queue and worker claims land in different exclusion domains. D25: an unavailable remote authority never falls back to local. The queue must also respect remote admission (default prefix `coordination:`) and never switch key policies to pass it. Claim resources must come from internal/resource so that they match the TASK-126.4 vectors.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Authority access goes through the existing authority client construction (internal/cli authority context and internal/authority). Queue code never opens authority SQLite tables directly
- [ ] #2 Batched Status requests are sized for both the 1 MiB request cap and the 4 MiB response cap. On `response-too-large` they split and retry, as tested with bundles of 32 resources of nearly 1 KiB each
- [ ] #3 Each item's claim observation includes authorityId, a state of free, held, expired, or unknown, holder agent and session, expiresAt in authority time, and observedAt. An unreachable authority yields unknown with a stale badge, never free
- [ ] #4 A view whose remote profile fails never falls back to the local authority, and a test covers this
- [ ] #5 For backlog-md sources, the checkout's profile is resolved with the CLI's precedence through config.SelectProfile, ignoring the queue's own flags. When the authority IDs differ, claim and launch are unavailable with reason `authority-mismatch`, as tested with checkout bindings
- [ ] #6 Items whose resource prefix the remote authority does not admit show claim unavailable with reason `resource-not-admitted`, and the key policy never changes to pass admission
- [ ] #7 Native claim state shows `not-exposed` for both initial adapters
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [ ] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->
