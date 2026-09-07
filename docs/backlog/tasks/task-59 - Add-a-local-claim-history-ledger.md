---
id: TASK-59
title: Add retained local claim lifecycle history
status: Done
assignee:
  - '@brett'
created_date: '2026-09-07 14:59'
updated_date: '2026-09-07 19:34'
labels:
  - storage
  - cli
  - docs
dependencies: []
references:
  - docs/distributed-cloudflare-claim-authority.md
  - docs/claim-model.md
  - src/worklease/sqlite.py
  - src/worklease/garbage_collection.py
modified_files:
  - src/worklease/projections.py
  - tests/test_history.py
priority: medium
type: feature
ordinal: 60000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Add retained local diagnostics that answer which agent held a resource, in what recoverable order, which explicit lifecycle operations Worklease recorded, and how each ownership epoch closed or was superseded.

The store persists acquisition identity in `epochs` and `bundle_epochs`, current mutable state in `claims` and `bundles`, explicit mutation receipts in `operations`, singleton release replay state in `releases`, and reconciliations. Acquire is not an operation row, singleton release is not an operation row, and bundle release is recorded only as a bundle operation. Expiry replacement and transfer do not persist the complete final state of the predecessor.

Two gaps remain:

1. Release, transfer, and replacement of expired ownership need a uniform token-free terminal snapshot. Expiry itself is lazy: an expired but unreclaimed current row cannot gain a durable end record without a later write.
2. No read-only command projects the retained rows as safe resource history. `list` shows current rows, `inspect-operation` requires an operation ID, and `gc` only inventories or explicitly removes retained state.

This history is local, retention-bounded diagnostic state. It is not authoritative provider progress, an attestation, tamper evidence, cross-host history, or part of the future remote authority contract. The first subtask records truthful epoch boundaries for new transitions; the second adds the minimal local read surface. Legacy overwrites and records already removed by `gc --apply` cannot be reconstructed.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every ownership transition recorded after the schema migration persists a token-free terminal snapshot per affected resource; expired-but-unreclaimed and legacy-incomplete epochs are represented without inventing an end record.
- [x] #2 A read-only local CLI command projects retained singleton and bundle-member history for one exact resource using only explicitly safe fields, deterministic ordering, and schema-versioned JSON.
- [x] #3 Documentation defines local and provider-authority boundaries, post-migration completeness, legacy and prior-GC gaps, record-level retention, sanitized export, and complete database archival.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Verify the integrated TASK-59.1 through TASK-59.3 implementation against the parent acceptance criteria.
2. Run focused history checks and all repository quality gates in the isolated worktree.
3. Record criterion-level evidence, finalize TASK-59, commit the backlog state, merge to main, validate, and clean the worktree.

4. Fix the verified pre-v3 read-only history failure by projecting missing acquisition revisions as legacy-incomplete without migrating the database, and add singleton and bundle regression coverage.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Selected as the only open dependency-ready backlog item after all three subtasks completed; working in HWT workspace w5C on branch task-59-retained-history.

Independent verification found that history on a valid unmigrated v2 database failed on missing acquisition_revision columns. Added a read-only schema-aware fallback and regression coverage that preserves schema v2 while projecting singleton and bundle epochs as legacy-incomplete.

Verification passed: focused history/store/GC/schema tests (194), full suite (276 core + 19 SDK), lint, format-check, typecheck, hooks, and git diff --check. Independent verifier passed all three parent criteria after confirming the pre-v3 fallback through the public CLI and database non-mutation.
<!-- SECTION:NOTES:END -->

## Comments

<!-- COMMENTS:BEGIN -->
author: @brett
created: 2026-09-07 19:34
---
Post-completion review of TASK-59 and subtasks 59.1-59.3. No correctness defects found: every claims delete/replace site (acquisition.py:120, lifecycle.py:106/303/507) records a termination; singleton acquire over a bundle member is rejected with bundle-operation-required so partial bundle retirement cannot bypass it; claim-id-reused blocks epoch_terminations PK reuse; EXPLAIN QUERY PLAN confirms epochs_by_resource_revision; a v2 database projects legacy-incomplete without mutation (schema_meta stays 2); JSON is byte-identical across runs. Fixed in e2cbbc8: removed write-only private keys (_resource_key, reconciliation _target_claim_id/_target_operation_id/_kind/_resource) and the unused bundle_key helper plus the operation_bundle_key alias from projections.history; added history to the claim-model required-values table; added COVERAGE to the cli-reference history output grammar row. Verified: mise run lint, format-check, typecheck, test (276 core + 19 SDK), hooks.
---
<!-- COMMENTS:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Completed retained local claim lifecycle history across TASK-59.1 through TASK-59.3. Added the final pre-v3 read-only fallback so unmigrated singleton and bundle epochs project as legacy-incomplete without schema mutation. Verified terminal snapshots, exact safe deterministic history, schema-v1 JSON, documentation boundaries, and all quality gates; independent verification passed.
<!-- SECTION:FINAL_SUMMARY:END -->
