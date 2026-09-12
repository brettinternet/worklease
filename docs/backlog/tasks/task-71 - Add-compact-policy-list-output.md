---
id: TASK-71
title: Add compact policy list output
status: To Do
assignee: []
created_date: '2026-09-12 02:10'
updated_date: '2026-09-12 02:17'
labels:
  - cli
  - ux
dependencies: []
references:
  - src/worklease/cli.py
  - tests/test_cli.py
  - docs/cli-reference.md
  - scripts/release_docs.py
  - CHANGELOG.md
priority: medium
type: enhancement
ordinal: 78000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Human-readable `policy list` currently emits nine tab-separated columns, forcing wide terminals and making the policy names difficult to compare. Most operators need policy identity and behavior; package provenance and contract versions are diagnostic details. Introduce a compact default without reducing the machine-readable contract.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Default text output uses an aligned table containing policy name, scope, capability, execution guarantee, and whether provider fencing is supported.
- [ ] #2 Default headings and boolean values use concise human-readable wording and the table remains legible on an 80-column terminal for every built-in policy.
- [ ] #3 A documented `--full` text mode preserves origin, origin version, contract version, key policy version, and every field currently shown.
- [ ] #4 Explicit JSON output remains schema-compatible and complete regardless of `--full`.
- [ ] #5 Empty policy discovery emits the correct compact or full header without placeholder rows.
- [ ] #6 CLI contract tests cover compact, full, empty, external-policy, and JSON behavior; command help and human-readable output documentation are updated, generated release documentation renders successfully, and CHANGELOG `Unreleased` records the changed text output.
<!-- AC:END -->
