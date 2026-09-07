---
id: TASK-47
title: Split store.py along singleton and bundle seams
status: Done
assignee:
  - '@codex-task-47'
created_date: '2026-09-07 03:27'
updated_date: '2026-09-07 06:28'
labels:
  - maintainability
dependencies: []
references:
  - src/worklease/store.py
  - src/worklease/cli.py
modified_files:
  - src/worklease/store.py
  - src/worklease/operations.py
  - src/worklease/reconciliation.py
  - src/worklease/projections.py
  - src/worklease/garbage_collection.py
  - src/worklease/claims.py
  - src/worklease/acquisition.py
  - src/worklease/lifecycle.py
  - src/worklease/cli.py
  - src/worklease/cli_dispatch.py
  - src/worklease/execution.py
  - tests/test_execution.py
  - tests/test_store.py
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
- [x] #1 No public API or JSON output changes; the full test suite passes unchanged
- [x] #2 Each singleton/bundle behavior pair is implemented once; no module exceeds ~800 lines
- [x] #3 Operation receipts for internal exec renewals no longer include the bearer token
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extract store operation-ledger behavior into focused modules while preserving LeaseStore's public methods, and parameterize singleton/bundle paths by operation resource.
2. Extract reconciliation, read projections, and garbage collection into focused modules, consolidating duplicated singleton/bundle behavior and keeping each extracted module near or below 800 lines.
3. Split CLI dispatch into stateless and store-backed paths, and add regression coverage proving internal exec-renewal receipts omit bearer tokens without changing public JSON.
4. Run focused tests, full project quality gates, adversarial review, and acceptance verification; fix item-scoped findings before finalizing.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented commit de85a26: reduced store.py from 3,044 to 224 lines by extracting acquisition, claims, lifecycle, operations, reconciliation, projections, and garbage collection mixins; extracted store-backed/stateless dispatch to cli_dispatch.py. Shared operation-resource behavior now handles singleton and bundle operation pairs once. Added explicit internal exec renewal handling so durable renewal receipts redact bearer tokens while public heartbeat and custom bundle completion behavior remains unchanged.

Review found and fixed singleton reconciliation resource omission, duplicate singleton expiry validation, opaque operation-ID token behavior, and custom bundle completion token drift. The remaining child-argv output scan finding is pre-existing TASK-45 scope and was not changed here.

Validation: mise run lint; mise run format-check; mise run typecheck; mise run test (216 core + 19 SDK); mise run hooks; independent verifier PASS for all acceptance criteria.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Split the 3,044-line lease store into focused modules with shared singleton/bundle operation behavior, separated CLI dispatch, and redacted internal exec-renewal receipts. Verified with all project quality gates, 235 tests, pre-commit hooks, adversarial review, and independent acceptance verification.
<!-- SECTION:FINAL_SUMMARY:END -->
