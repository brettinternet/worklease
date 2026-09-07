---
id: TASK-59
title: Add retained local claim lifecycle history
status: In Progress
assignee:
  - '@brett'
created_date: '2026-09-07 14:59'
updated_date: '2026-09-07 18:36'
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
- [ ] #1 Every ownership transition recorded after the schema migration persists a token-free terminal snapshot per affected resource; expired-but-unreclaimed and legacy-incomplete epochs are represented without inventing an end record.
- [ ] #2 A read-only local CLI command projects retained singleton and bundle-member history for one exact resource using only explicitly safe fields, deterministic ordering, and schema-versioned JSON.
- [ ] #3 Documentation defines local and provider-authority boundaries, post-migration completeness, legacy and prior-GC gaps, record-level retention, sanitized export, and complete database archival.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Verify the integrated TASK-59.1 through TASK-59.3 implementation against the parent acceptance criteria.
2. Run focused history checks and all repository quality gates in the isolated worktree.
3. Record criterion-level evidence, finalize TASK-59, commit the backlog state, merge to main, validate, and clean the worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Selected as the only open dependency-ready backlog item after all three subtasks completed; working in HWT workspace w5C on branch task-59-retained-history.
<!-- SECTION:NOTES:END -->
