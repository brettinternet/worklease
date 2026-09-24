---
id: TASK-130.1
title: Claim for me from the queue with a private session
status: Done
assignee:
  - '@pi'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-24 05:58'
labels:
  - work-queue
  - authority
milestone: m-1
dependencies:
  - TASK-130.3
references:
  - internal/handle/handle.go
  - internal/authority/authority.go
  - internal/lease/service.go
  - internal/cli/lease_commands.go
  - docs/claim-model.md
documentation:
  - docs/work-queue-tui-proposal.md
parent_task_id: TASK-130
priority: high
type: feature
ordinal: 29000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Plan section 9 ("Who renews a claim?") says an explicit human Claim for me is owned by the queue under its own persisted full-UUID session, separate from any worker's session. Claim never assigns the item or changes provider state (D5). Every mutation opens a preview first (plan section 13), showing authority and scope, resources, session and TTL, provider effect ("unchanged"), and limits.

Reuse the existing handle and pending-request machinery (internal/handle and the authority client) so the queue gains no ownership path weaker than `worklease acquire`. This task covers acquiring and displaying the claim, and it must call the TASK-130.3 identity gate immediately before every acquisition. Renewal, exit, release, and cancellation are TASK-130.2.

Remote authority metadata exposes admitted prefixes but not maxTTL or maxHold (internal/authority/http.go), so the preview cannot always know effective limits before acquiring.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `c` in the TUI opens a claim preview that shows authority (profile, ID, and local or remote scope), exact resources, session, requested TTL, "Provider: unchanged", and the coordination limits. Nothing is sent until the user confirms
- [x] #2 Acquisition goes through the existing authority client and handle machinery with a queue-owned full-UUID session. The private handle is persisted with the same owner-only protections as CLI handles
- [x] #3 The preview shows the requested TTL and hold plus any limits the authority is known to enforce. An admission rejection (for example, TTL above the server maximum) is reported with no claim held, and after acquisition the detail view shows the TTL and expiry the grant actually returned, as tested against a server whose maximum is below the request
- [x] #4 Immediately before acquiring, the queue refreshes the item's prerequisite closure and action eligibility from the provider and runs the TASK-130.3 identity gate. If either fails, nothing is acquired, as tested by reopening a prerequisite after the snapshot
- [x] #5 Claim is unavailable, with the TASK-128.7 reasons, on authority mismatch (D11), unadmitted resource prefixes, an unavailable authority, or stale claim observations. It is never offered for an item the overlay has not verified as free
- [x] #6 Contention (already claimed, lost race) shows the holder and expiry and never retries in a tight loop. An uncertain acquire is surfaced as uncertain and recovered through the existing pending-request rules
- [x] #7 Claim never changes provider assignment or state, as tested against both adapters
- [x] #8 An end-to-end test acquires from the queue and then fails `worklease acquire` for the same item, and the reverse, using resources from the TASK-126.4 vectors, for both the local and remote authorities
- [x] #9 With an active queue-owned handle, a remote authority restore-ID or authority-ID change stops claim-dependent actions; the queue follows profile identity and recovery procedures before resubscribing, never silently repins IDs, and never treats snapshot rebuilding as handle recovery (D20, staged from TASK-129.5)
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add a TUI claim preview and a queue-owned private-session acquisition path using the existing authority/handle lifecycle. 2. Re-read provider closure, identity, and live claim status immediately before dispatch; fail closed on mismatch/contention/uncertainty. 3. Exercise local and remote acquisition, resource contention, provider nonmutation, and restore drift with focused tests. 4. Run repository gates, review, commit, integrate, record evidence, and release.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
User approved staging TASK-129.5 active queue-owned handle restore regression here, where the queue-owned handle is first implemented. Test the fail-closed gate and recovery with the handle still active; no S3/S4 dependency cycle.

Implemented on task-130-1-queue-claim (47f1061), fast-forward merged to main. TUI preview/confirmation and private queue session with owner-only handle; provider closure, identity and authority rechecked before acquisition. Local/remote contention, lower remote TTL rejection/grant, reopened prerequisite, authority identity drift, migration and delayed UI response exercised in tests. Existing provider read-only boundary tests and queue AST mutation guard cover both adapters. Reviewer found four concrete defects (navigation result loss, stale overlay, binding migration, truncated preview); all fixed and regression-tested. mise run lint, format-check, test, typecheck and hooks pass; no plan/decision changed.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Claim for me now previews and acquires queue-owned coordination leases without provider writes; validated by local/remote contention and fail-closed regressions, full Go gates, and pre-commit hooks. Integrated commit 47f1061 into main.
<!-- SECTION:FINAL_SUMMARY:END -->
