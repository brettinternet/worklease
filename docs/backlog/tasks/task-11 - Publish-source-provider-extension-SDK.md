---
id: TASK-11
title: Publish source provider extension SDK
status: To Do
assignee: []
created_date: '2026-07-14 02:33'
labels:
  - providers
  - sdk
  - plugins
dependencies:
  - TASK-10
references:
  - skills/worklease-source-workflow/references/provider-contract.md
priority: medium
type: enhancement
ordinal: 11000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Turn the documented source-provider capability contract into a stable typed SDK and conformance kit for external provider packages. Provider SDKs, credentials, network calls, scheduling, and authoritative mutations remain outside the lease core.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The public SDK defines typed source, source-qualified work reference, work item, provider receipt, capability result, and source-provider protocol contracts matching the documented provider boundary.
- [ ] #2 The contract covers source resolution, complete discovery, authoritative reads, resource-policy selection, authorized state/progress writes, review boundaries, archive behavior, provider versions, and durable receipts without defining a scheduler or claim lifecycle.
- [ ] #3 A reusable conformance kit verifies source qualification, dependency closure, unsupported-capability behavior, stale provider-version rejection, ambiguous outcomes, receipt durability, token redaction, and truthful provider-fencing declarations.
- [ ] #4 An example external provider package composes the SDK with a TASK-10 resource policy without importing provider dependencies into worklease core modules.
- [ ] #5 Versioning and compatibility documentation defines how third-party providers declare supported SDK and resource-policy contract versions.
<!-- AC:END -->
