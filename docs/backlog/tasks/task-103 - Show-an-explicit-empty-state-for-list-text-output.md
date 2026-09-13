---
id: TASK-103
title: Show an explicit empty state for list text output
status: Done
assignee:
  - '@brett'
created_date: '2026-09-13 04:08'
updated_date: '2026-09-13 04:30'
labels:
  - cli
  - ux
dependencies: []
references:
  - internal/cli/text.go
  - internal/cli/text_test.go
  - internal/cli/resource_commands_test.go
documentation:
  - docs/cli-reference.md
priority: low
type: enhancement
ordinal: 128000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
When no claims match, human-readable list output currently prints only the table header. That is ambiguous at a glance and looks like truncated output. Empty text views should state the successful empty result directly while preserving the existing table and JSON contracts for populated results.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Default text list output says that there are no current claims when the authority contains no matching claims.
- [x] #2 Full text list output uses the same explicit empty-state semantics.
- [x] #3 Populated compact and full tables and JSON envelopes remain unchanged.
- [x] #4 Tests cover a missing authority, an empty existing authority, and a resource filter with no matches.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. writeListTextAt prints 'no current claims' instead of a bare header when there are no rows (compact and full).
2. Keep populated tables and JSON envelopes unchanged.
3. CLI-level tests for a missing authority home, an empty existing authority, and a resource filter with no matches; document in cli-reference and CHANGELOG.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
writeListTextAt prints 'no current claims' for zero rows in both compact and --full text; populated tables and the JSON envelope are untouched. Found and fixed an adjacent bug: list read the --resource slice flag with cmd.String, so the filter was always empty; it now takes exactly one resource and rejects more. Verified with TestListTextExplicitEmptyState, TestListTextEmptyStatesAcrossAuthorityShapes (missing home, empty authority, unmatched filter, populated table, JSON envelope, multi-filter rejection), TestListTextCompactAndFull, mise run test/lint/typecheck/format-check, smoke, doc-test, e2e.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
List text now states 'no current claims' instead of a bare header, and the list --resource filter actually filters. Verified by unit and CLI-level tests across missing, empty, and filtered authorities plus the full quality gates.
<!-- SECTION:FINAL_SUMMARY:END -->
