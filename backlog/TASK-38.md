---
id: TASK-38
title: Preserve repository path in compact list resources
status: done
type: enhancement
priority: medium
assignees: ['@codex-loop-fresh-20260715-worklease-main']
labels: [cli, ux]
rank: a452a37b0f452a33e
refs: [src/worklease/cli.py, tests/test_cli.py, README.md]
created: 2026-07-16T16:17:00Z
updated: 2026-07-16T17:06:00Z
---

Make default human-readable worklease list output retain a useful repository or directory path component when compacting long opaque resources. Keep the resource policy provider-neutral and bounded, preserve full and JSON output, and leave location metadata out of scope.

## Acceptance criteria

- [x] Default text list remains bounded and deterministic while retaining a useful path component for long path-based resources.
- [x] The renderer remains lexical and provider-agnostic; arbitrary opaque resources retain the generic fallback.
- [x] worklease list --full and explicit JSON output remain complete and unchanged.
- [x] CLI tests cover multiple repository/path depths and existing compact/full/JSON behavior.

## Plan

1. Replace the current resource boundary shortening with a lexical path-aware renderer that retains a useful repository or directory component within the existing width. 2. Add deterministic CLI coverage for multiple path depths, opaque fallback behavior, and unchanged full/JSON values. 3. Update the human-readable grammar and run the required project checks.

## Notes

Implemented lexical path-anchor shortening with longest fitting suffix, retained bounded fallback for oversized components, added POSIX and Windows path regressions plus worklease list rendering coverage, and updated README grammar. Focused CLI contract tests pass.

Objective validation: focused CLI formatter tests passed; full mise run test passed (193 core and 19 SDK tests); mise run lint passed; mise run format-check passed; mise run typecheck passed; staged mise run hooks passed Ruff format, Ruff check, and both test suites; commit 619eb14551b5cecabe69e1faa405da1190d92052 pushed as origin/fix/compact-resource-path. Independent verifier job was unavailable and cancelled after no report; acceptance evidence is from direct targeted tests and required project gates.

## Summary

Implemented lexical compact RESOURCE rendering that preserves a useful path anchor when the existing suffix would otherwise hide repository context, keeps the longest fitting source/item suffix, remains bounded for oversized path components, and supports POSIX and Windows separators without provider-specific parsing. Added helper and worklease list regressions, preserved full/JSON values, and documented the grammar. Verified with focused CLI tests, full core/SDK suites, lint, format-check, typecheck, hooks, commit 619eb14551b5cecabe69e1faa405da1190d92052, and push to origin/fix/compact-resource-path.
