---
id: TASK-50
title: >-
  Fix bundle projections in status --verbose and clean up overlapping bundle
  reclaim
status: Done
assignee:
  - '@codex-task-50'
created_date: '2026-09-07 03:28'
updated_date: '2026-09-07 06:10'
labels:
  - bundles
dependencies: []
references:
  - src/worklease/store.py
modified_files:
  - README.md
  - src/worklease/cli.py
  - src/worklease/store.py
  - src/worklease/schemas/v1/common.json
  - tests/test_cli.py
  - tests/test_schemas.py
  - tests/test_store.py
priority: medium
type: bug
ordinal: 51000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`status --verbose` (`LeaseStore.status_verbose`) reads only the singleton claim row and filters operations by the plain resource key, so a bundle member renders as a singleton claim with no bundle marker, and bundle operations stored under the JSON-array key from `_bundle_operation_resource` never appear in `unknownOperations`. The one command documented for surfacing unknown outcomes cannot surface bundle ones.

Separately, reclaiming an expired bundle that overlaps a new bundle (acquire [R2,R3] over expired [R1,R2]) deletes bundle_members for the old claim across all members, leaving R1 with an expired claims row whose claim_id belongs to a bundle epoch with no bundle. Not a safety violation today, but it breaks the invariant that GC and diagnostics rely on.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 status --resource R --verbose for a bundle member reports the bundle resources and lists started exec-bundle operations in unknownOperations
- [x] #2 status --verbose text and JSON output for bundle members is documented in README and covered by tests
- [x] #3 Overlapping bundle reclaim leaves no claims row referencing a bundle claim_id without a bundles row; a regression test covers acquire [R2,R3] over expired [R1,R2]
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend read-only verbose status projection to detect bundle membership, emit ordered bundle resources, and find current or historical bundle operations through bundle epoch membership.
2. Delete all claim projections for an expired overlapping bundle before removing its membership and bundle rows.
3. Extend the v1 schema, add store and CLI regressions for bundle diagnostics and overlapping reclaim, and document bundle-member verbose output.
4. Run focused tests and all repository quality gates; review the diff and finalize the task with verification evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented bundle-aware verbose diagnostics using bundle epoch membership, including historical unknown operations without canonical-key collisions. Extended the v1 verbose claim schema and text renderer for ordered resources. Expired overlapping bundle reclaim now removes all old claim projections before deleting bundle metadata.

Review fixes: added schema validation, historical bundle-operation lookup, canonical-key collision coverage, case-insensitive reconciliation fingerprint matching, and type narrowing in CLI tests.

Validation passed: mise run lint; mise run format-check; mise run test (216 core tests and 19 SDK tests); mise run typecheck (0 errors).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Fixed bundle-member verbose status in JSON/text, retained unknown bundle-operation diagnostics across overlapping reclaims, and removed orphaned claim projections during expired bundle reclaim. Verified with schema, CLI, store, collision, historical-operation, reconciliation, and full repository gate coverage.
<!-- SECTION:FINAL_SUMMARY:END -->
