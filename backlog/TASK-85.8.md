---
id: TASK-85.8
title: Enable claims over many resources
status: done
type: feature
priority: high
assignees: ['@pi-01a09528']
labels: [go-rewrite]
milestone: m-0
parent: TASK-85
blocked_by: [TASK-85.7]
rank: ab48c20563b48c166
refs: [docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md, src/worklease/acquisition.py, src/worklease/models.py, tests/test_store.py]
files: [CHANGELOG.md, internal/lease/helpers.go, internal/lease/service.go, internal/lease/bundle_test.go, internal/cli/lease_commands.go, internal/cli/status_test.go]
created: 2026-09-12T03:23:00Z
updated: 2026-09-12T10:38:00Z
---

Extend the one-claim service from one resource to 1–32 without a separate bundle API or implementation. Read contract sections 7.2, 7.5 and 7.9–7.12. Own the minimal lease/acquire/status extensions and their tests.

Acquire all resources or none, including replacement of entire expired predecessor claims, retain input order for reporting, and act on the whole claim for lifecycle operations. Resource-scoped status can query several unrelated claims without accidentally selecting one. Preserve every unresolved predecessor operation across partial overlap so recovery does not disappear when only one old member is acquired.

Evidence and patterns (the amended contract is normative): `src/worklease/acquisition.py` acquire_bundle (preflight, expired-member replacement, rollback) and `models.py` require_bundle_resources. Tests in `tests/test_store.py`: test_bundle_validation_rejects_invalid_shapes_and_bounds, test_bundle_acquire_is_atomic_and_single_member_mutations_are_rejected, test_bundle_retry_and_expiry_reclaim_are_idempotent_and_versioned, test_exact_expired_bundle_replacement_records_each_member_once, test_overlapping_expired_bundle_reclaim_removes_all_old_claim_rows, test_bundle_partial_expiry_replacement_and_release_record_each_member, test_overlapping_bundles_have_one_winner_without_partial_claims, test_overlapping_bundles_across_processes_have_one_winner, test_bundle_acquire_rolls_back_after_partial_member_failure, test_bundle_checkpoint_renews_every_member_and_replays.

## Acceptance criteria

- [x] Validation accepts 1–32 unique valid resources and rejects duplicates, blanks and excess members before writes.
- [x] Contention or an injected partial-insert failure leaves all requested resources unchanged; first contended resource metadata follows caller order.
- [x] Replacing multiple expired claims finalizes each epoch once; replacing one member of an expired many-resource claim removes the entire old projection and reports retained predecessor unknowns. Partial acquisition intersecting a started predecessor is rejected with its complete required resource union; one covering successor can reconcile.
- [x] Repeated overlapping subprocess acquisitions have at most one winner; all lifecycle mutations cover the whole claim and member status resolves consistently, with no individual-member release/transfer.
- [x] Status for resources belonging to different claims/free resources reports each mapping truthfully, and text/JSON preserve resource order and redaction; mise run ci-go passes.

## Definition of done

- [x] `mise run ci-go` passes on the final commit
- [x] Final summary names the Go test functions or commands that prove each acceptance criterion

## Plan

1. Extend service validation and acquisition to accept 1–32 ordered unique resources, preflight active contention and unresolved predecessor coverage, then retire each expired predecessor exactly once in one transaction.
2. Add ordered per-resource status projections so mixed claimed/free queries remain unambiguous while retaining whole-claim lifecycle behavior.
3. Add focused validation, rollback, expiry/recovery, lifecycle, status, and cross-process overlap tests.
4. Run focused Go tests and mise run ci-go; review the diff and record acceptance evidence.

## Notes

Implemented ordered 1–32-resource claims with transactional preflight, whole expired-claim retirement, unresolved-operation coverage closure, and ordered mixed-resource status projections. Added focused service, subprocess contention, rollback, lifecycle, and CLI status tests. Validation so far: focused Go tests, 20 repeated bundle test runs, mise run ci-go, mise run lint, mise run format-check, mise run test, and mise run typecheck all pass (typecheck after mise run sync).

Final Go validation passed: mise run ci-go completed gofmt check, vet/staticcheck, unit tests, race tests, vulnerability scan, and static build.

## Summary

Implemented atomic ordered claims over 1–32 resources, whole-predecessor expiry replacement, unresolved-operation recovery coverage, and truthful ordered mixed-resource status output. Evidence: AC1 TestBundleValidationAcceptsBoundsAndRejectsInvalidShapesBeforeWrites; AC2 TestBundleAcquireContentionUsesCallerOrderAndRollsBack and TestBundleAcquireRollsBackAfterPartialMemberFailure; AC3 TestBundleExpiredReplacementFinalizesClaimsOnceAndRemovesWholeProjection and TestBundlePendingPredecessorRequiresCompleteResourceCoverage; AC4 TestOverlappingBundlesAcrossProcessesHaveOneWinner and TestBundleLifecycleAndStatusOperateOnWholeClaim; AC5 TestStatusMixedResourcesPreservesOrderInJSONAndText and mise run ci-go. Repository gates mise run lint, mise run format-check, mise run test, and mise run typecheck also passed.
