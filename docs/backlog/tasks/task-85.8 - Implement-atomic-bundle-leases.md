---
id: TASK-85.8
title: Enable claims over many resources
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 06:27'
labels:
  - go-rewrite
milestone: m-0
dependencies:
  - TASK-85.7
references:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - src/worklease/acquisition.py
  - src/worklease/models.py
  - tests/test_store.py
parent_task_id: TASK-85
priority: high
type: feature
ordinal: 100000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Extend the one-claim service from one resource to 1–32 without a separate bundle API or implementation. Read contract sections 7.2, 7.5 and 7.9–7.12. Own the minimal lease/acquire/status extensions and their tests.

Acquire all resources or none, including replacement of entire expired predecessor claims, retain input order for reporting, and act on the whole claim for lifecycle operations. Resource-scoped status can query several unrelated claims without accidentally selecting one. Preserve every unresolved predecessor operation across partial overlap so recovery does not disappear when only one old member is acquired.

Evidence and patterns (the amended contract is normative): `src/worklease/acquisition.py` acquire_bundle (preflight, expired-member replacement, rollback) and `models.py` require_bundle_resources. Tests in `tests/test_store.py`: test_bundle_validation_rejects_invalid_shapes_and_bounds, test_bundle_acquire_is_atomic_and_single_member_mutations_are_rejected, test_bundle_retry_and_expiry_reclaim_are_idempotent_and_versioned, test_exact_expired_bundle_replacement_records_each_member_once, test_overlapping_expired_bundle_reclaim_removes_all_old_claim_rows, test_bundle_partial_expiry_replacement_and_release_record_each_member, test_overlapping_bundles_have_one_winner_without_partial_claims, test_overlapping_bundles_across_processes_have_one_winner, test_bundle_acquire_rolls_back_after_partial_member_failure, test_bundle_checkpoint_renews_every_member_and_replays.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Validation accepts 1–32 unique valid resources and rejects duplicates, blanks and excess members before writes.
- [ ] #2 Contention or an injected partial-insert failure leaves all requested resources unchanged; first contended resource metadata follows caller order.
- [ ] #3 Replacing multiple expired claims finalizes each epoch once; replacing one member of an expired many-resource claim removes the entire old projection and reports retained predecessor unknowns. Partial acquisition intersecting a started predecessor is rejected with its complete required resource union; one covering successor can reconcile.
- [ ] #4 Repeated overlapping subprocess acquisitions have at most one winner; all lifecycle mutations cover the whole claim and member status resolves consistently, with no individual-member release/transfer.
- [ ] #5 Status for resources belonging to different claims/free resources reports each mapping truthfully, and text/JSON preserve resource order and redaction; mise run ci-go passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
