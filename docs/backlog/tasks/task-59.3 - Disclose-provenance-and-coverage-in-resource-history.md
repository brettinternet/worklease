---
id: TASK-59.3
title: Disclose provenance and coverage in resource history
status: To Do
assignee: []
created_date: '2026-09-07 17:17'
updated_date: '2026-09-07 17:28'
labels:
  - cli
  - storage
  - docs
dependencies:
  - TASK-59.2
references:
  - src/worklease/projections.py
  - src/worklease/sqlite.py
  - src/worklease/schemas/v1/history.json
  - docs/cli-reference.md
  - >-
    https://github.com/jpicklyk/task-orchestrator/blob/main/current/src/main/resources/db/migration/V16__Resource_Lease_History.sql
  - >-
    https://github.com/GoogleCloudPlatform/khi/blob/main/docs/en/guide/03-timeline-view.md
  - tests/test_gc.py
  - tests/test_schemas.py
  - tests/test_store.py
  - docs/claim-model.md
parent_task_id: TASK-59
priority: medium
type: enhancement
ordinal: 64000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Extend the TASK-59.2 retained projection with record-category provenance, an explicit coverage summary, a documented projected-held interval, and an indexed singleton lookup. These adapt provenance and partial-window ideas from the references; they do not provide append-only or audit parity. The trust boundary is unchanged: this is local, retention-bounded diagnostic state, not an audit log, attestation, or provider record.

Scope:

1. Provenance. Put `source` on each source-backed nested object to identify its retained record category, using exactly `epoch` for the acquisition object (including bundle epochs), `operation`, `reconciliation`, `termination`, or `current-claim` for the current singleton or bundle claim snapshot. Existing stable IDs identify the particular record where available. Keep facts from different retained record kinds in separate objects; in particular, keep reconciliation-derived outcomes in the reconciliation object instead of folding them into an operation. `completeness` and `coverage` are derived projection metadata, not events, and do not receive a fabricated source.
2. Completeness. Formalize the TASK-59.2 epoch distinction with one deterministic value, derived in precedence order under the storage invariant that a terminated epoch is not current: `legacy-incomplete` when `acquisitionRevision` is null or when both a termination and a matching current claim snapshot are absent; otherwise `open` when the matching current snapshot is present and termination is null; otherwise `complete` when termination is present. `open` describes retained epoch closure, not clock-derived activity; a stored expiry in the past does not change it.
3. Coverage. Add `coverage.earliestRetainedAcquisitionRevision` (nullable), `coverage.resourceRevisionWatermark` (nullable), and `coverage.legacyIncompleteCount`. The first value is the minimum non-null acquisition revision among retained singleton and bundle-member epochs for the requested resource. The watermark is the monotonic value in `resources`, including its retained tombstone, and the count covers retained epochs only. An absent or greater-than-one earliest acquisition revision can reveal that revision-stamped acquisition history starts after prior resource activity, but cannot identify exact missing events: GC and pre-migration history are indistinguishable, and non-acquisition mutations also consume revisions. When all epochs have been collected the earliest value is null but the watermark remains; a never-seen resource has both revisions null and a zero count.
4. Held-at semantics. Document the projected half-open interval only where an end bound is stored: `acquiredAt <= T < termination.effectiveAt` for a terminated epoch, and `acquiredAt <= T < currentClaim.expiresAt` for an open epoch. A legacy-incomplete epoch without either end source has an unknown upper bound. This is a consumer rule over retained local rows, not an audit fact. Do not add `--at` or use the current clock.
5. Index. Add `epochs_by_resource_revision` on `epochs(resource, acquisition_revision)`, register it as required schema state so existing v3 databases self-heal without data loss, and verify the singleton `WHERE resource = ?` history lookup uses it. Bundle epochs continue to use `json_each` over stored members.

Non-goals: `--at`, `--since`, identity filters, all-resource output, JSON Lines export, append-only triggers, hash chaining, signing, never-prune retention, force-release reasons, a GC provenance watermark, and remote or cross-host semantics.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Every acquisition, operation, reconciliation, termination, and current-claim object in history --json has the matching source value from the fixed enum; facts from different source kinds remain in separate nested objects
- [ ] #2 Each epoch reports exactly one of complete, open, or legacy-incomplete using the stated stored-field precedence, with no clock input; open is not treated as active, and repeated runs on an unchanged store are byte-identical
- [ ] #3 Each resource reports nullable earliestRetainedAcquisitionRevision, nullable resourceRevisionWatermark, and retained legacyIncompleteCount; tests show never-seen, pre-GC, partial-GC, and all-epochs-collected values without synthesizing missing events
- [ ] #4 The v1 history schema documents every source value, the limits of revision-based coverage, the distinction between open and active, and the held-at rules including an unknown legacy upper bound; schema tests validate the fields
- [ ] #5 The epochs_by_resource_revision index is required schema state, is added to pre-existing v3 databases without data loss, and EXPLAIN QUERY PLAN for the singleton resource history lookup reports that index
- [ ] #6 Text output shows source, completeness, and coverage using the documented grammar; docs/cli-reference.md and docs/claim-model.md describe all completeness states, nullable coverage, and the migration/GC caveat
- [ ] #7 Tests cover all five source values, all three completeness states, singleton and bundle-member epochs, current singleton and bundle claims, a migrated current epoch with a null acquisition revision, a legacy epoch with no end bound, retained legacy plus known epochs, partial and complete epoch GC, deterministic output, index migration and query planning, and schema validation
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend the TASK-59.2 projection and v1 schema with separate sourced record objects, mutually exclusive completeness derivation, and nullable coverage metadata.
2. Add and register the singleton epoch lookup index while retaining the existing bundle-member scan.
3. Update deterministic text rendering and the CLI and claim-model documentation, including held-at and coverage caveats.
4. Add projection, GC, migration, query-plan, schema, and text tests for singleton, bundle, legacy, partial-retention, and no-retained-epoch cases.
5. Run focused tests and all repository quality gates, then finalize TASK-59.3 with criterion-level evidence.
<!-- SECTION:PLAN:END -->
