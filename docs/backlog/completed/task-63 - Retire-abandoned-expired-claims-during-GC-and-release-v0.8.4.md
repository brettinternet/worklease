---
id: TASK-63
title: Retire abandoned expired claims during GC and release v0.8.4
status: Done
assignee:
  - '@brett'
created_date: '2026-09-10 21:11'
updated_date: '2026-09-10 21:55'
labels: []
dependencies: []
references:
  - docs/cli-reference.md
  - docs/claim-model.md
modified_files:
  - src/worklease/garbage_collection.py
  - src/worklease/cli.py
  - src/worklease/schemas/v1/commands.json
  - tests/test_gc.py
  - tests/test_cli.py
  - docs/cli-reference.md
  - docs/claim-model.md
  - CHANGELOG.md
  - pyproject.toml
  - packages/worklease-source-sdk/pyproject.toml
  - packages/worklease-source-sdk/src/worklease_source_sdk/__init__.py
  - uv.lock
  - .github/workflows/release.yml
priority: high
type: enhancement
ordinal: 67000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make garbage collection match operator expectations by retiring abandoned expired claim projections after the retention window, while preserving ownership safety, unresolved-operation evidence, revision continuity, and truthful history. Include the concise dry-run apply hint already requested and publish the result as v0.8.4.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Text-mode GC dry runs with eligible records print a copyable apply hint using the captured cutoff; empty dry runs, applied runs, and JSON output do not add prose
- [x] #2 GC inventories expired singleton and bundle claims older than the strict retention cutoff and explains expired claims protected by unresolved operations
- [x] #3 GC apply atomically retires eligible expired claims and bundles so they disappear from list, while active claims and expired claims inside the retention window remain unchanged
- [x] #4 Retirement records truthful expired terminations, preserves monotonic resource revisions, and intentionally forfeits checkpoint recovery only after the retention boundary
- [x] #5 Unknown operations, concurrency, interruption, bundle atomicity, redaction, dry-run parity, schemas, and operator documentation have regression coverage
- [x] #6 Version 0.8.4 is committed, pushed, tagged, and published with passing local and remote release checks
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend the atomic GC inventory with strict-cutoff expired singleton and bundle candidates plus unresolved-operation-protected summaries, keeping dry-run/apply inventory parity.
2. On apply, truthfully record expired terminations at collection time and atomically remove eligible current singleton or whole-bundle projections before deleting the already-captured historical candidates; preserve revisions and defer new termination/history collection to a later retention window.
3. Update text/JSON contracts, documentation, and focused regressions for boundaries, protected unknown outcomes, checkpoint/history semantics, bundle atomicity, concurrency, rollback, redaction, and hints.
4. Prepare version 0.8.4 metadata/changelog, run full local gates and independent review, commit and push main, validate CI/release workflow, tag v0.8.4, then verify the published assets.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented strict-cutoff inventories for expired singleton and bundle claims, unresolved-operation protected summaries, atomic termination/removal on apply, copyable dry-run hints, and v0.8.4 metadata/docs. Focused GC/CLI/schema/package/release tests pass (54 tests). Added boundary, checkpoint forfeiture, history completeness, revision, bundle, unknown-operation, rollback, and concurrent acquire coverage.

Independent review found legacy revision regression/orphan-termination risk and pre-transaction termination timestamps. Fixed by preserving resource revision tombstones, avoiding fabricated orphan terminations when legacy claims lack epochs, and capturing termination time after acquiring the immediate transaction; added legacy reuse and lock-contention timestamp regressions.

Final local verification: mise lint and format-check pass; 282 core and 19 SDK tests pass; core/SDK Pyright report zero errors; root and SDK 0.8.4 wheel/sdist builds pass; staged Lefthook Ruff and full tests pass. Independent reviewer PASS after two findings were fixed, with no remaining validated findings.

Committed and pushed c889edf to main. Main CI 34533981529 and non-publishing release validation 34533999733 passed. Tagged v0.8.4 at c889edf; tagged CI 34534243946 and Release 34534243887 passed. GitHub release is public with nine assets, and all downloaded assets passed checksum validation.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
GC now retires abandoned singleton and bundle claims after the retention cutoff, reports unresolved-operation protections, preserves truthful history and monotonic revisions, and gives text users a copyable apply hint. Published v0.8.4 from c889edf after full local gates, independent review/fixes, green main and tagged workflows, and checksum verification of all nine release assets.
<!-- SECTION:FINAL_SUMMARY:END -->
