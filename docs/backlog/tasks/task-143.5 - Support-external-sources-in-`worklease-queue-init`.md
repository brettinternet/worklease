---
id: TASK-143.5
title: Support external sources in `worklease queue init`
status: Done
assignee: []
created_date: '2026-09-25 16:38'
updated_date: '2026-09-26 00:34'
labels:
  - work-queue
  - external-adapter
milestone: m-1
dependencies:
  - TASK-143.4
documentation:
  - docs/queue.md
  - docs/external-adapter-protocol.md
parent_task_id: TASK-143
priority: medium
type: feature
ordinal: 77000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`worklease queue init` (TASK-137) generates queue.yaml only for Backlog.md and GitHub. Configuring an external source means hand-writing the executable, expected identity and version, config, and claim binding, then discovering the separate approval step. Reading these values from the adapter's manifest removes that manual work and its typos.

The user wants the CLI approached with API-driven design: every command added here is a stable, scriptable interface first, with `--json` output in the existing CLI envelope (`schemaVersion`, `ok`, `operation`, and `error.reason`/`exitCode`/`details`), stable diagnostic reasons, and documented exit codes, and the human output is a rendering of that same result.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 `worklease queue init --adapter external --executable PATH` starts the adapter only to read its manifest, then proposes a source with the executable path, expected adapter ID and version from the manifest, and config validated against the manifest's config schema.
- [x] #2 Claims stay read-only unless `--portable-claims SOURCE` is given; init never enables claims, writes, or credential use implicitly.
- [x] #3 Init never approves the adapter; its `--json` result includes the exact next command (`queue adapter approve --source ID`) and the executable digest to review.
- [x] #4 Manifest failures, schema-invalid config, and existing sources produce stable diagnostics in the JSON envelope, and queue.yaml is left unchanged on any failure.
- [x] #5 docs/queue.md documents the flow.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Extend queue init external adapter discovery and schema-checked source proposal using existing manifest and config mechanisms. 2. Add stable JSON diagnostics, approval guidance and focused in-process tests. 3. Document flow, run quality gates, commit and merge, then finalize task.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented external manifest-only init with schema validation, explicit portable claim binding, SHA-256 approval guidance and stable JSON errors. Focused in-process tests pass (race count=3); lint, format-check, typecheck and full mise test passed. Independent reviewer unavailable due authentication error; performed one item-scoped self-review and corrected human digest rendering.

Verified TestQueueInitExternalManifestApprovalAndReadOnlyDefaults and TestQueueInitExternalFailureAndPortableClaims with go test -race -count=3; manifest-only test helper refuses all non-initialize calls, checks schema rejection, exact SHA-256 and approval command, default read-only source, explicit portable claims, JSON reason/exitCode, and unchanged YAML on duplicate/failure. docs/queue.md covers CLI, approval, exit codes. All mise quality gates and staged hooks passed; code commit 6be11e0 merged to main by 9bf26ea; post-main-merge focused race, lint, format-check and typecheck passed. Review: no remaining item-scoped defects; external reviewer failed due invalid API key.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added manifest-driven external queue init with schema validation and safe read-only defaults; documented approval and stable diagnostics. Verified focused race tests, full suite, quality gates and hooks; merged to main (9bf26ea).
<!-- SECTION:FINAL_SUMMARY:END -->
