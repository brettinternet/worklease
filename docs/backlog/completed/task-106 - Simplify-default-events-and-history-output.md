---
id: TASK-106
title: Remove synthetic row numbers and simplify lifecycle timelines
status: Done
assignee:
  - '@brett'
created_date: '2026-09-13 06:51'
updated_date: '2026-09-13 07:19'
labels: []
dependencies: []
modified_files:
  - internal/cli/text.go
  - internal/cli/text_test.go
  - docs/cli-reference.md
  - CHANGELOG.md
priority: medium
type: enhancement
ordinal: 131000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Human-readable collection output still reads like serialized records rather than a compact view. Synthetic row numbers are not referenced elsewhere, collection headers already establish row context, and identifiers and sequence metadata make routine timeline scanning noisy. Remove synthetic numbering wherever it adds no stable identity, and keep complete lifecycle diagnostics available through `--full` while making default events and history output comparable in density to `worklease list`.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Default `worklease events` text rows omit synthetic row numbers, sequence values, claim IDs, and redundant field labels while retaining event kind, summarized resources when present, and relative time.
- [x] #2 Default resource-specific `worklease history` text rows omit synthetic row numbers, claim IDs, and redundant field labels while retaining agent, status, acquisition and end timing, end reason, and the compact operation summary.
- [x] #3 `--full` retains complete non-secret event and history metadata, including event sequence, complete claim identifiers, operation metadata, and absolute timestamps, without adding synthetic row numbers.
- [x] #4 Other human-readable collection views, including full resource status and acquire recovery details, omit synthetic row numbers when rows have no stable user-facing identity.
- [x] #5 Gap and pruning notices remain visible in compact text because they affect interpretation of the timeline.
- [x] #6 JSON output and cursor behavior remain unchanged.
- [x] #7 Tests cover compact and full events and history output, resource-less authority events, and every other collection view changed by the numbering rule.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Simplify compact event/history rows to unlabeled timeline values while preserving gap and pruning notices.
2. Remove synthetic numbering from full event/history, full resource status, and acquire recovery rows while retaining complete metadata.
3. Update fixed-time renderer and CLI tests to cover compact/full output, authority-wide events, changed collection views, and unchanged JSON cursors.
4. Update user-facing output documentation and changelog, run focused tests and all repository gates, independently review, then finalize and integrate.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Simplified compact event and history rows, removed synthetic numbering from full timelines/status/recovery details, retained full diagnostics with work keys and operation timestamps, and updated fixed-time tests plus CLI docs/changelog. Focused internal/cli tests pass.

Verification: go test ./internal/cli; mise run lint; mise run format-check; mise run test; mise run typecheck; LSP diagnostics clean. Independent reviewer reported no validated findings. Fixed-time renderer tests cover compact/full event and history rows, resource-less authority events, gap/pruning notices, full resource status, and acquire recovery details; CLI tests preserve JSON event/history envelopes and cursors.

Implementation commit: f9eff3a (Simplify lifecycle timeline output).

Rebased implementation commit for main integration: bb9e664.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Removed unstable row numbering and serialized-record noise from human lifecycle timelines while retaining complete non-secret diagnostics under --full. Compact events/history now prioritize kind, resources, actor/status, timing, end reason, and operation activity; JSON and cursor contracts remain unchanged. Verified by focused and full Go tests, lint/format/typecheck gates, clean diagnostics, and independent review.
<!-- SECTION:FINAL_SUMMARY:END -->
