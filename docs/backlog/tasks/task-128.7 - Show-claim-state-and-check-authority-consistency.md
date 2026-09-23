---
id: TASK-128.7
title: Show claim state and check authority consistency
status: Done
assignee:
  - '@brett'
created_date: '2026-09-23 04:29'
updated_date: '2026-09-23 23:56'
labels:
  - work-queue
  - authority
  - reviewed
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
- [x] #1 Authority access goes through the existing authority client construction (internal/cli authority context and internal/authority). Queue code never opens authority SQLite tables directly
- [x] #2 Batched Status requests are sized for both the 1 MiB request cap and the 4 MiB response cap. On `response-too-large` they split and retry, as tested with bundles of 32 resources of nearly 1 KiB each
- [x] #3 Each item's claim observation includes authorityId, a state of free, held, expired, or unknown, holder agent and session, expiresAt in authority time, and observedAt. An unreachable authority yields unknown with a stale badge, never free
- [x] #4 A view whose remote profile fails never falls back to the local authority, and a test covers this
- [x] #5 For backlog-md sources, the checkout's profile is resolved with the CLI's precedence through config.SelectProfile, ignoring the queue's own flags. When the authority IDs differ, claim and launch are unavailable with reason `authority-mismatch`, as tested with checkout bindings
- [x] #6 Items whose resource prefix the remote authority does not admit show claim unavailable with reason `resource-not-admitted`, and the key policy never changes to pass admission
- [x] #7 Native claim state shows `not-exposed` for both initial adapters
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [x] #1 `mise run lint`, `mise run format-check`, `mise run test`, `mise run typecheck`, and `mise run hooks` pass
- [x] #2 Any decision (D1-D27) or plan section this work contradicts or refines is updated in docs/work-queue-tui-proposal.md in the same commit
<!-- DOD:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add authority-context-backed batched Status overlay and stable claim observations to queue snapshots. 2. Validate checkout profile/admission against view authority and expose action reasons. 3. Test remote failures, batching, profile mismatch and run all gates; review, commit, merge and checkpoint.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented read-only batched Status overlay, authority profile selection, claim states/admission/D11 checks; focused tests and lint, format-check, test, typecheck pass. Reviewing before commit.

Evidence: TestClaimOverlayBatchesAndSplitsWithoutList, TestLargeResourceStatusSplitsOnResponseLimit (32 near-1KiB resources), TestClaimOverlayStatesAndOutage, TestQueueUnavailableRemoteNeverFallsBackToLocal, TestCheckoutBindingMismatchWithPortableKey, TestClaimOverlayAdmissionAndCheckoutAuthority, TestConfiguredSourcesPreserveKeyInputsAndNativeClaim; all repository gates and staged hooks pass. Review: corrected stale observedAt, checkout authority domain and GitHub enterprise key input; no unresolved item defects. Merged 584a571 and 8cbf9b8 into main as 25f398c and db751b4. No D1-D27 or plan text contradicted.

Review: cached snapshots now get claim overlays; local checkout authority compared with worker's default local store; authority-mismatch denials no longer suppress holder observations; TUI shows stale claim state (3ff7075). Merged with TASK-129.5 overlay; late admission metadata now re-overlays under current authority (00a6b2e). Merged to main 013a058.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added read-only batched Status claim observations, view authority selection, D11/admission checks and native claim state. All gates and focused tests pass; merged as 25f398c and db751b4.
<!-- SECTION:FINAL_SUMMARY:END -->
