---
id: TASK-12
title: Stabilize the public API and JSON schemas
status: To Do
assignee: []
created_date: '2026-07-14 02:33'
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
