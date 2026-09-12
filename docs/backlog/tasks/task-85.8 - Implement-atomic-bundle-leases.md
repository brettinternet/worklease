---
id: TASK-85.8
title: Enable claims over many resources
status: To Do
assignee: []
created_date: '2026-09-12 03:23'
updated_date: '2026-09-12 04:06'
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
Workflows sometimes need all-or-nothing ownership of several resources, such as a task plus the files it touches. The one-claim model already stores an ordered resource set per claim, and TASK-85.7 rejected more than one resource. This task lifts that limit to 32 and proves the atomicity, contention, and reporting rules of contract 7.9 and 7.10 for many resources without adding any separate code path.

Read first: contract sections 2 (D4), 4 (acquire and status rows), 7.9, 7.10, 7.12, 8. Python evidence: `src/worklease/acquisition.py` (acquire_bundle: preflight, expired-member replacement, rollback), `models.py` require_bundle_resources; `tests/test_store.py`: test_bundle_validation_rejects_invalid_shapes_and_bounds, test_bundle_acquire_is_atomic_and_single_member_mutations_are_rejected, test_bundle_retry_and_expiry_reclaim_are_idempotent_and_versioned, test_exact_expired_bundle_replacement_records_each_member_once, test_overlapping_expired_bundle_reclaim_removes_all_old_claim_rows, test_bundle_partial_expiry_replacement_and_release_record_each_member, test_overlapping_bundles_have_one_winner_without_partial_claims, test_overlapping_bundles_across_processes_have_one_winner, test_bundle_acquire_rolls_back_after_partial_member_failure, test_bundle_checkpoint_renews_every_member_and_replays.

Deliver: remove the single-resource guard in `internal/lease`; validate 1 to 32 unique resources (invalid-resource for duplicates, blanks, or more than 32); acquire all or none in one transaction including replacement of several expired holders (one `expired-replaced` event and one epoch end per replaced claim); report the first contended resource in caller order with its holder; `Status` by a subset of resources returns the whole claim with the full ordered list and marks which requested resources matched; every mutation from TASK-85.7 keeps acting on the whole claim; events carry the full ordered resource list. CLI: repeated `-r` on `acquire` and `status`; text status shows the ordered resources compactly.

Owned paths: `internal/lease` (extension), acquire and status tests in `internal/cli`. Out of scope: per-resource release or transfer (unsupported by design), new commands.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Validation tests prove 1 to 32 unique resources are accepted and duplicates, blank entries, and 33 resources fail invalid-resource before any write.
- [ ] #2 Atomicity tests prove an acquire over {A,B,C} where B is held by an active claim fails already-claimed naming B and its holder and leaves A and C free, and an injected storage failure after inserting two of three claim_resources rows rolls back everything.
- [ ] #3 Expiry tests prove an acquire over {A,B} where A and B are held by two different expired claims replaces both in one transaction with two expired-replaced events and two finalized epochs, and an acquire over {A} where A belongs to an expired claim {A,B} replaces the whole old claim and leaves B free.
- [ ] #4 Concurrency tests prove two subprocesses acquiring overlapping sets {A,B} and {B,C} repeatedly yield at most one winner per round with no partial claims, and heartbeat, checkpoint, release, and transfer on a three-resource claim advance one revision and cover every member (status by each member returns the same claim id and revision).
- [ ] #5 CLI tests prove `acquire -r A -r B --json` lists resources in caller order, `status -r B` returns the whole claim, text output is compact and deterministic, and `mise run ci-go` passes.
<!-- AC:END -->

## Definition of Done
<!-- DOD:BEGIN -->
- [ ] #1 `mise run ci-go` passes on the final commit
- [ ] #2 Final summary names the Go test functions or commands that prove each acceptance criterion
<!-- DOD:END -->
