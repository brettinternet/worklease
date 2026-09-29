---
id: TASK-153.3
title: Resolve one claim authority per source
status: Done
assignee: []
created_date: '2026-09-29 02:25'
updated_date: '2026-09-29 08:50'
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
- [x] #1 Each source resolves exactly one claim authority; configuration that places one source under two authorities is rejected with an error naming the source.
- [x] #2 For a checkout-backed source, queue claim actions and plain `acquire`/`verify` in that checkout or its linked worktrees resolve the same authority, or queue claim actions are refused with a named mismatch reason.
- [x] #3 Projects on different authorities share one set of filter views; a cross-project scope spanning authorities shows each source's claim state from that source's authority and routes its claims there.
- [x] #4 Changing a source's authority still requires identity confirmation before claim actions are available.
- [x] #5 Existing `queue.yaml` files with per-view authority keep loading or fail with a documented migration step; docs/queue.md, docs/config-schemas/queue.schema.json, and CHANGELOG.md are updated.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Resolve authority once per source, preserving unambiguous legacy per-view configurations and rejecting conflicting source authority assignments.
2. Route CLI, TUI and MCP reads/claims and identity confirmation through source authority; enforce consistency with checkout/worktree profile selection. Allow mixed-authority projects to share filter views and queue init defaults.
3. Add focused authority-routing, mismatch, migration and identity regression coverage; update queue documentation, schema and changelog.
4. Run required validation and one independent scoped review; fix concrete findings, commit on main, record evidence and release the claim.

5. Add optional per-source authority with conflict-checked legacy per-view inference; route each source through its configured trusted authority across CLI/TUI/MCP, bind MCP lease lifecycle to the authority/profile selected at acquisition, and enforce source-checkout binding consistency.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented source-owned authorities with conflict-checked legacy view inference, mixed-authority CLI/TUI/MCP routing, source-checkout/worktree authority guards, queue init migration, and identity-confirmation preservation. Updated docs/queue.md, queue JSON schema and CHANGELOG.
Verification: full mise lint, format-check, typecheck, test, race and doc-test passed after corrections. Parent independently reran lint/format-check/typecheck and nine routing, mismatch, identity, restart and live-overlay tests with go test -race -count=3 across config/cli/queue/mcp; all passed.
One independent review found two concrete P2 defects: MCP profile binding lost on restart with profile aliases, and out-of-order mixed-authority live publications. Both fixed with persisted private profile binding and serialized publications; restart/pending-recovery and controlled-interleaving regression tests pass. No second general review performed.
Compatibility limits: older remote MCP handles without profile metadata fail closed if multiple profiles match; authority migration still requires the documented operator checks for claims/workers on the former authority. No remaining implementation blocker. Delivery pending main commit and final task checkpoint.

Acceptance evidence:
AC1/AC5: TestQueueSourceAuthoritiesResolveLegacyAndSourceBindings verifies legacy inference, source-level selection and source-named conflicts; required schema/docs/changelog updated and doc-test passed.
AC2: TestLinkedCheckoutAuthorityUsesRepositoryBindingWithoutChangingViewAgreement and TestCheckoutMismatchKeepsStatusObservationAndActionDenial verify shared main/worktree selection and named fail-closed mismatches.
AC3: TestQueueAuthoritySetRoutesMixedSourceBindings, TestOverlayClaimsByAuthorityRoutesEachSource, TestRunClaimOverlayByAuthorityRoutesAndJoinsWorkers, TestRunClaimOverlayByAuthorityPublishesAggregatesInOrder and TestMCPLeaseKeepsAcquisitionAuthorityProfileForLifecycle cover mixed routing, live display, lifecycle/restart and pending recovery.
AC4: TestQueueIdentityConfirmsAuthorityChange verifies authority drift requires confirmation.
All listed tests independently passed three race-enabled runs. Full quality gates passed; installed hook and staged mise run hooks passed, then commit hook passed again. Delivered on main in c5a72d1. No remaining blocker or resumable step for this item; TASK-153.4 is the remaining sibling and was not started.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Completed per-source authority resolution and mixed-authority queue routing in c5a72d1 on main. Preserved legacy migration, checkout/worktree mismatch protection and identity confirmation. One independent review completed; both concrete findings fixed and regression-tested. Full lint, formatting, vet, tests, race, doc tests and commit hooks passed.
<!-- SECTION:FINAL_SUMMARY:END -->
