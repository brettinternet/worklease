---
id: TASK-50
title: >-
  Fix bundle projections in status --verbose and clean up overlapping bundle
  reclaim
status: In Progress
assignee:
  - '@codex-task-50'
created_date: '2026-09-07 03:28'
updated_date: '2026-09-07 05:47'
labels:
  - bundles
dependencies: []
references:
  - src/worklease/store.py
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
- [ ] #1 status --resource R --verbose for a bundle member reports the bundle resources and lists started exec-bundle operations in unknownOperations
- [ ] #2 status --verbose text and JSON output for bundle members is documented in README and covered by tests
- [ ] #3 Overlapping bundle reclaim leaves no claims row referencing a bundle claim_id without a bundles row; a regression test covers acquire [R2,R3] over expired [R1,R2]
<!-- AC:END -->
