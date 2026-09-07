---
id: TASK-47
title: Split store.py along singleton and bundle seams
status: To Do
assignee: []
created_date: '2026-09-07 03:27'
labels:
  - maintainability
dependencies: []
references:
  - src/worklease/store.py
  - src/worklease/cli.py
priority: medium
type: chore
ordinal: 48000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`store.py` is ~3000 lines and every singleton method has a near-verbatim bundle twin (`_cached_operation`/`_bundle_cached_operation`, `_require_owner`/`_require_bundle_owner`, `begin_operation`/`begin_bundle_operation`, `complete_operation`/`complete_bundle_operation`, `reconcile_operation`/`reconcile_bundle_operation`). The twins have drifted: the bundle GC predicate lacked the claim_id join the singleton one had (fixed 2026-09-06), `status_verbose` diverged from `status`, and `_advance_claim` stores receipts with the token while `complete_operation` deliberately does not. The UPDATE claims / UPSERT resources / re-read / build receipt block is hand-rolled three times.

Refactor without behavior change: parameterize each singleton/bundle pair by the operation-resource function; extract the operation ledger (`_operation_row`, `_cached_operation`, `begin_*`, `complete_*`) into operations.py; extract reconciliation, GC, and the read projections (`status`, `status_verbose`, `list_claims`) into their own modules. Similarly split the `_dispatch` chain in cli.py into stateless and store-backed halves to remove the 19 `assert store is not None` checks.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 No public API or JSON output changes; the full test suite passes unchanged
- [ ] #2 Each singleton/bundle behavior pair is implemented once; no module exceeds ~800 lines
- [ ] #3 Operation receipts for internal exec renewals no longer include the bearer token
<!-- AC:END -->
