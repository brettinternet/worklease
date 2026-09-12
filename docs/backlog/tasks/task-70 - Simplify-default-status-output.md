---
id: TASK-70
title: Simplify default status output
status: Done
assignee:
  - '@pi-01a0936b'
created_date: '2026-09-12 02:10'
updated_date: '2026-09-12 02:39'
labels:
  - cli
  - ux
dependencies: []
references:
  - src/worklease/cli.py
  - tests/test_cli.py
  - docs/cli-reference.md
  - CHANGELOG.md
modified_files:
  - src/worklease/cli.py
  - src/worklease/cli_dispatch.py
  - src/worklease/projections.py
  - tests/test_cli.py
  - docs/cli-reference.md
  - CHANGELOG.md
priority: high
type: enhancement
ordinal: 77000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The default human-readable `status` response currently repeats the resource and prints claim, session, and owner identifiers that are mainly useful for diagnostics. Operators usually need to know whether the resource is claimed, who or what owns the work, and how long the lease remains. Make the default view a concise operational summary while preserving the full diagnostic projection.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Default text output for a claimed singleton shows one concise resource label plus state, agent, work key, lease time remaining or elapsed, and revision; it does not show claim ID, session ID, owner ID, absolute timestamps, guarantee, or a repeated resource.
- [x] #2 Default text output for an unclaimed singleton remains immediately understandable and does not print an empty claim block.
- [x] #3 `status --verbose` preserves the complete current redacted diagnostic projection, including full resource values, lifecycle identifiers, timestamps, unknown operations, release data, and guidance.
- [x] #4 JSON output remains schema-compatible and complete in both default and verbose invocations.
- [x] #5 Resource and lease summaries reuse the list-output conventions established by TASK-68 rather than defining conflicting formatting.
- [x] #6 CLI contract tests use a fixed clock to cover active, expired, and unclaimed states plus singleton and bundle default, verbose, and JSON output; command help and human-readable output documentation are updated, and CHANGELOG `Unreleased` records the changed text output.
- [x] #7 Bundle status commands use the same summary vocabulary, preserve ordered bundle identity without dumping lifecycle IDs, and retain their complete current projection in verbose and JSON modes.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Reuse the compact list resource and lease-time rendering helpers for singleton and bundle status summaries.
2. Preserve the existing full redacted status projection behind --verbose and all JSON output.
3. Add fixed-clock CLI coverage for active, expired, free, singleton, bundle, verbose, and JSON modes; update help, CLI docs, and CHANGELOG.
4. Run focused tests and all repository quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented compact singleton and ordered-bundle status summaries using the existing list resource and relative lease helpers. Added bundle --verbose diagnostics and preserved read-only behavior, including rejecting a non-file state database after independent review found that edge case.

Validation: mise run lint, mise run format-check, mise run typecheck, and mise run test all passed (306 core tests and 19 SDK tests).
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Simplified default singleton and bundle status text while retaining complete redacted verbose and JSON diagnostics. Added fixed-clock rendering, CLI, schema-compatibility, bundle identity, read-only, and storage-failure coverage; all repository quality gates pass and the independent review finding was fixed.
<!-- SECTION:FINAL_SUMMARY:END -->
