---
id: TASK-70
title: Simplify default status output
status: To Do
assignee: []
created_date: '2026-09-12 02:10'
updated_date: '2026-09-12 02:12'
labels:
  - cli
  - ux
dependencies: []
references:
  - src/worklease/cli.py
  - tests/test_cli.py
  - docs/cli-reference.md
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
- [ ] #1 Default text output for a claimed singleton shows one concise resource label plus state, agent, work key, lease time remaining or elapsed, and revision; it does not show claim ID, session ID, owner ID, absolute timestamps, guarantee, or a repeated resource.
- [ ] #2 Default text output for an unclaimed singleton remains immediately understandable and does not print an empty claim block.
- [ ] #3 `status --verbose` preserves the complete current redacted diagnostic projection, including full resource values, lifecycle identifiers, timestamps, unknown operations, release data, and guidance.
- [ ] #4 JSON output remains schema-compatible and complete in both default and verbose invocations.
- [ ] #5 Resource and lease summaries reuse the list-output conventions established by TASK-68 rather than defining conflicting formatting.
- [ ] #6 CLI contract tests cover active, expired, and unclaimed states plus default, verbose, and JSON output; human-readable output documentation is updated.
- [ ] #7 Bundle status commands use the same summary vocabulary, preserve ordered bundle identity without dumping lifecycle IDs, and retain their complete current projection in verbose and JSON modes.
<!-- AC:END -->
