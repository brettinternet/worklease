---
id: TASK-103
title: Show an explicit empty state for list text output
status: done
type: enhancement
priority: low
assignees: ['@brett']
labels: [cli, ux]
rank: ad0649a7f8d0648f0
refs: [internal/cli/text.go, internal/cli/text_test.go, internal/cli/resource_commands_test.go, docs/cli-reference.md]
created: 2026-09-13T04:08:00Z
updated: 2026-09-13T04:30:00Z
---

When no claims match, human-readable list output currently prints only the table header. That is ambiguous at a glance and looks like truncated output. Empty text views should state the successful empty result directly while preserving the existing table and JSON contracts for populated results.

## Acceptance criteria

- [x] Default text list output says that there are no current claims when the authority contains no matching claims.
- [x] Full text list output uses the same explicit empty-state semantics.
- [x] Populated compact and full tables and JSON envelopes remain unchanged.
- [x] Tests cover a missing authority, an empty existing authority, and a resource filter with no matches.

## Plan

1. writeListTextAt prints 'no current claims' instead of a bare header when there are no rows (compact and full).
2. Keep populated tables and JSON envelopes unchanged.
3. CLI-level tests for a missing authority home, an empty existing authority, and a resource filter with no matches; document in cli-reference and CHANGELOG.

## Notes

writeListTextAt prints 'no current claims' for zero rows in both compact and --full text; populated tables and the JSON envelope are untouched. Found and fixed an adjacent bug: list read the --resource slice flag with cmd.String, so the filter was always empty; it now takes exactly one resource and rejects more. Verified with TestListTextExplicitEmptyState, TestListTextEmptyStatesAcrossAuthorityShapes (missing home, empty authority, unmatched filter, populated table, JSON envelope, multi-filter rejection), TestListTextCompactAndFull, mise run test/lint/typecheck/format-check, smoke, doc-test, e2e.

## Summary

List text now states 'no current claims' instead of a bare header, and the list --resource filter actually filters. Verified by unit and CLI-level tests across missing, empty, and filtered authorities plus the full quality gates.
