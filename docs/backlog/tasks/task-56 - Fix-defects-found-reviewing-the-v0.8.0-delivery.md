---
id: TASK-56
title: Fix defects found reviewing the v0.8.0 delivery
status: Done
assignee:
  - '@claude'
created_date: '2026-09-07 14:09'
updated_date: '2026-09-07 14:13'
labels: []
dependencies: []
ordinal: 57000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Adversarial review of every backlog item completed the week of 2026-09-01 (TASK-43 through TASK-54, shipped as v0.8.0) found ten defects across the delivered work, three of them affecting lease safety or credential handling. Fix them, cover each with a regression test that fails without the fix, and restore the reference documentation that the README rewrite removed without relocating.

Scope: the clock-regression predicate from TASK-53 that expired live leases; the transfer replay token leak and the lost in-lock bundle-membership check from the TASK-47/49 store split; three lease-handle defects from TASK-44; the maxDuration idempotency fingerprint from TASK-52; the grouped-short-option argv boundary from TASK-45; the display-width model from TASK-48; a non-total credential comparison from TASK-51; and the documentation dropped by TASK-54.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Each fixed defect has a regression test that fails against the unfixed code and passes after the fix
- [x] #2 A live lease holder is never dispossessed by a backward wall-clock step, and an abandoned lease with an implausible expiry is reclaimable within one TTL
- [x] #3 No command can return a bearer token belonging to a claim other than the one the caller is replaying
- [x] #4 Exit codes, short option namespace, state selection, API surface, garbage-collection semantics, and text grammar are documented somewhere the README links to
- [x] #5 mise run lint, format-check, test, and typecheck all pass
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Adversarially review each backlog item completed the week of 2026-09-01 against its acceptance criteria and the current code, one reviewer per change area.
2. Confirm the lease-semantics regression independently and take a second opinion on the replacement predicate before changing mutual-exclusion behavior.
3. Fix each validated defect with the smallest correction, adding a regression test that fails against the unfixed code first.
4. Restore the reference documentation the README rewrite dropped, into a linked docs page rather than back into the README.
5. Run every quality gate, record evidence per acceptance criterion, and commit.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Defects fixed, each with a regression test verified to fail against the unfixed code (checked by stashing the source fix and re-running):

Lease safety
- models.py lease_is_active (TASK-53): reduced algebraically to 'now >= heartbeat_at', so a 50ms backward step expired a 900s lease and let a second agent acquire the same resource. Replaced with plain expiry plus clock_regression() and a _repair_clock_regression() pre-pass at the three contention sites, committed in its own transaction so it survives the contender's already-claimed. Oracle consulted before changing mutual-exclusion semantics; an independent reviewer re-validated the replacement.
- lifecycle.py transfer replay (TASK-49): re-injected whatever token currently held the resource, handing a former owner control of an unrelated live claim. Now returns the recorded successor's token only while that successor is still current.
- store.py _acquire_transaction (TASK-49): the bundle-membership check ran before the resource lock and the in-lock re-check was lost in the split, letting a singleton claim overwrite a bundle member and wedge the resource beyond acquire and gc. Re-check restored inside the lock and transaction.

Lease handle (TASK-44)
- A handle write failure after the mutation committed discarded the payload holding the claim's only bearer token. Destination is now validated pre-commit; a late failure emits the claim with its token plus leaseFileError and exit 75.
- acquire now refuses to clobber a handle still holding a live claim (lease-file-in-use).
- An idempotent replay no longer writes back the older recorded revision and bricks the handle.

Other
- operations.py (TASK-52): maxDuration had joined the idempotency fingerprint, so operation rows written before the bound existed replayed as operation-id-request-mismatch. Excluded from the fingerprint, kept in the receipt.
- cli.py (TASK-45): grouped short options were modelled as one option plus an attached value, so 'worklease -jH DIR exec ... /bin/echo --json' was rejected while its child flags were consumed. Tokens are now expanded character by character against the parser; an unrecognized option no longer truncates the caller's output options away.
- cli.py (TASK-48): combining marks and format characters counted as 1-2 columns instead of 0, misaligning decomposed text; the existing test measured alignment with the function under test, so it passed against the pre-fix model. Replaced with an independent oracle plus concrete width assertions.
- credentials.py (TASK-51): a non-UTF-8-encodable token raised out of the ownership guard; comparison is now total.
- reconciliation.py: removed an unreachable branch carried through the split.
- cli.py: removed a dead _RETRYABLE_ACQUIRE_ERRORS copy and a duplicate _DEFAULT_POLL_INTERVAL that let the documented default drift from behavior.

Documentation and CI
- docs/cli-reference.md restores the exit-code table, short option namespace (including -M, which TASK-52 added and no listing covered), state selection, supported API surface, garbage-collection safety semantics, and the text output grammar. TASK-54 deleted all of it four minutes before TASK-48 shipped, leaving TASK-48's own 'documented widths' unreferenced anywhere. README links it.
- The README lifecycle used 'set -euo pipefail' with a guarded step that fails under a bare 'python', so a copy-pasted run aborted before release and leaked the claim for its full TTL. Verified by extracting the block and running it: it now completes and 'worklease list' is empty afterwards.
- CHANGELOG 0.8.0 documented 2 of the 12 items that shipped in it. Filled in the lease handle, generated identifiers, --max-duration, and the five bug fixes; the post-release corrections are under Unreleased.

Verification: mise run lint, format-check, typecheck (0 errors), and test (253 core + 19 SDK) all pass.

Concurrency note: TASK-55 was committed independently in this working tree during this review (6bbb3b2, 0c43205), raising the exec timeout budgets to 0.2s. That left test_exec_timeout_kills_inherited_pipe_descendant_and_is_inspectable at 0.1s, one of the four tests that actually failed on macos-15-intel. This change supersedes it: budgets derive from a single documented GUARD_BUDGET, children outlive it by 30x, and the kill is proven by reaping the descendant's pid rather than by waiting out a sleep. Verified with 3 consecutive clean runs of tests.test_execution under 24 CPU hogs.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Reviewed every backlog item completed the week of 2026-09-01 (TASK-43 through TASK-54, shipped as v0.8.0) and fixed ten validated defects, each with a regression test confirmed to fail against the unfixed code. The three most serious: TASK-53's clock-regression predicate expired live leases on a 50ms backward step and let a second agent acquire the same resource, inverting the mutual-exclusion guarantee it was meant to protect; the TASK-47/49 store split let a transfer replay return an unrelated live claim's bearer token and dropped acquire's in-lock bundle-membership check, which could permanently wedge a resource; and a TASK-44 lease-handle write failure after a committed mutation discarded the claim's only copy of its token. Also restored the reference documentation TASK-54 removed without relocating (docs/cli-reference.md), made the README lifecycle survive a copy-pasted run, completed the CHANGELOG for what 0.8.0 actually shipped, and made the four exec-timeout tests that were failing CI on macos-15-intel deterministic. Verified with mise run lint, format-check, typecheck, and test (253 core + 19 SDK), plus targeted end-to-end CLI runs and 3 clean stress runs of the execution suite under 24 CPU hogs.
<!-- SECTION:FINAL_SUMMARY:END -->
