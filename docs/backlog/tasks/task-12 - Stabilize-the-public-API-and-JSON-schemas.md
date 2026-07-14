---
id: TASK-12
title: Stabilize the public API and JSON schemas
status: To Do
assignee:
  - '@codex-main'
created_date: '2026-07-14 02:33'
updated_date: '2026-07-14 03:33'
labels:
  - api
  - schema
  - compatibility
dependencies: []
references:
  - src/worklease/__init__.py
  - src/worklease/cli.py
priority: medium
type: enhancement
ordinal: 12000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Give library and CLI integrators an explicit, versioned compatibility surface instead of requiring imports from incidental module layout or inference from prose examples.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A documented public Python facade exports the supported request models, errors, lease store operations, guarded-operation interfaces, and resource-policy result types while internal persistence and locking modules remain explicitly private.
- [ ] #2 Machine-readable JSON Schemas cover the common success and error envelopes, claims, operation receipts, key results, and every released CLI command response.
- [ ] #3 Automated contract tests validate representative success and failure output against the published schemas and ensure read-only schemas cannot contain bearer tokens.
- [ ] #4 Compatibility documentation defines additive changes, unknown-field handling, stable errors and exit codes, deprecation expectations, and the conditions requiring a new schema or API contract version.
- [ ] #5 Wheel, sdist, editable, and supported standalone builds include the public type information and schema artifacts they claim to expose.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. **[T1] Supported typed Python facade (AC1, AC5).** Define the public exports in `worklease.__init__` for lease requests/results/errors/store operations, guarded execution/replacement interfaces, and resource-policy result types; keep SQLite, locking, migrations, and helpers private. Add `py.typed` and import/type contract tests across editable, wheel, and sdist installs.
2. **[T2] Packaged JSON Schema v1 (AC2, AC3, AC5).** Add `src/worklease/schemas/v1` with shared envelope/claim/receipt/resource-policy definitions and a schema for every released CLI success and error response. Package the artifacts and validate representative success/failure output, including token-redacted read-only commands, against them.
3. **[T3] Compatibility policy (AC3, AC4).** Document Python/API and CLI/schema compatibility: additive optional fields within schema v1, unknown-field tolerance, stable error reasons/exit codes, deprecation procedure, and major/minor/patch bump conditions. Add release artifact smoke checks for schema/type data in wheel, sdist, editable, and standalone builds.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
### Refinement snapshot

**Execution status:** available now. Resource-policy result schemas use the current stable `ResourceKey` fields and must remain compatible with TASK-10's extension work; neither item requires an unfinished artifact from the other to begin.

**Goal and target area:** Edit `src/worklease/__init__.py` and public model modules; create `src/worklease/schemas/v1/` and `src/worklease/py.typed`; extend packaging, contract tests, README compatibility guidance, and release artifact checks. New schema/type paths are item-local creations, not missing prerequisites.

**Resolved decisions:** Public Python exports include LeaseStore, LeaseError/ClaimError, request/claim/result models, guarded execute/replace interfaces, and resource-policy result protocols; SQLite connection/migration, file-lock, path, and serialization helpers remain private. JSON Schema draft 2020-12 artifacts are packaged by schema version 1 with shared envelope/claim/receipt/resource-policy definitions and per-command success/error schemas. Additive optional fields remain schema v1 and consumers ignore unknown fields; removals, semantic changes, or newly required fields require a schema-version bump. Python API follows semantic versioning with documented deprecation before removal. Stable error reasons and exit codes are compatibility surface.

**Non-goals:** freezing internal storage schema, promising private imports, generating language clients, or redesigning CLI output.

**Evidence and assumptions:** The package currently has an incidental facade and one integer CLI envelope version; wheel/sdist/PyInstaller pipelines already exist and can assert packaged artifacts.

**Task/acceptance map:** T1→AC1/5; T2→AC2/3/5; T3→AC3/4/5.

**Pending verification:** static type/import tests, JSON Schema validation for every command success/failure, redaction checks, wheel/sdist/editable/frozen artifact inspection, full quality gates.

**Next action:** define the public export manifest and schema inventory before moving implementations.

**Refinement checkpoint:** refined: TASK-12 specification complete; provider=backlog-md; providerVersion=1.48.0; claimId=f514a99c-b3a0-4de5-b3d8-b533db33dc75; claimRevision=3; refinement: complete.
<!-- SECTION:NOTES:END -->
