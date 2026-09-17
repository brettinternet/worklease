---
id: TASK-120
title: Add safe contextual handle inspection and archival
status: In Progress
assignee:
  - '@pi'
created_date: '2026-09-16 23:57'
updated_date: '2026-09-17 04:37'
labels:
  - cli
  - handles
  - recovery
dependencies:
  - TASK-119
references:
  - internal/handle/handle.go
  - docs/claim-model.md
  - internal/handle/handle_test.go
priority: medium
type: feature
ordinal: 162000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Operators can encounter a stale or foreign contextual handle but have no supported way to identify or set it aside. Manual filesystem deletion risks losing credentials or pending recovery state, while selecting another session only bypasses the blocked slot and leaves its condition unexplained.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 A CLI command inspects the selected or explicitly named handle and reports only redacted authority, claim, selector, lifecycle state, resources, and recovery-relevant metadata
- [ ] #2 A CLI command archives a handle into owner-private recoverable storage without contacting, releasing, revoking, or otherwise mutating any authority
- [ ] #3 Archival clearly warns that an active claim may remain and requires explicit acknowledgement before preserving aside pending or recovery state
- [ ] #4 Inspection and archival reject unsafe paths and concurrent changes, preserve owner-only permissions, and never expose credentials or private evidence
- [ ] #5 Help, documentation, and tests cover ready, pending, recovery, authority-mismatched, missing, malformed, and unsafe handles
- [ ] #6 Inspection is offline and non-mutating: it neither contacts an authority nor creates a missing handle/store. Recorded expiry is not reported as proof that the claim is inactive; selector provenance is unknown when it cannot be established.
- [ ] #7 Archive storage is durable and no-overwrite before the original is removed; any failure preserves at least one complete recoverable copy with exact credentials, pending request bytes, and recovery state. Successful output gives a usable explicit-handle recovery path without exposing file contents.
- [ ] #8 Pending/recovery archival requires an explicit noninteractive acknowledgement; refusal leaves the source unchanged. Malformed and unsafe inputs fail closed, and an archive is never automatically selected as a fresh contextual handle.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add offline handle inspect and archive commands that reuse authority-aware contextual/explicit handle selection.
2. Introduce redacted metadata projection plus owner-private, pinned, durable no-overwrite archival with explicit acknowledgement for pending/recovery state.
3. Cover ready, pending, recovery, foreign, missing, malformed, unsafe, collision, and race cases; update help and handle documentation.
4. Run focused and repository quality gates, review the diff, then finalize and integrate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Validation: internal/handle/handle.go provides private validated reads, pinned locks, writes, and removal but no supported archive operation; the CLI has no handle inspection/archive surface. Handle.State is pending or ready; RecoveryRequest is separate, and ExpiresAt alone cannot prove authority-side expiry. Contextual filenames are hashes and the handle does not store the checkout/selector, so inspection cannot reconstruct selector provenance from an arbitrary file. TASK-119 is a real prerequisite because contextual selection and legacy lookup must agree.

Implementation complete in task-120-handle-archive: added offline handle inspect/archive commands, strict redacted projections, snapshot-bound no-overwrite archival, contextual-destination rejection, recovery metadata validation, and race/collision/unsafe-input coverage. Independent security review found and drove fixes for pre-copy replacement consent bypass, contextual-slot archive injection, malformed recovery metadata projection, and shell-unsafe recovery commands. Validation passed: mise run lint, format-check, test, and typecheck.
<!-- SECTION:NOTES:END -->
