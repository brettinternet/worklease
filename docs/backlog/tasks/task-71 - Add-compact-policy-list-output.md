---
id: TASK-71
title: Add compact policy list output
status: Done
assignee:
  - '@pi-01a09381'
created_date: '2026-09-12 02:10'
updated_date: '2026-09-12 02:56'
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
modified_files:
  - src/worklease/cli.py
  - tests/test_cli.py
  - docs/cli-reference.md
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
- [x] #1 Default text output uses an aligned table containing policy name, scope, capability, execution guarantee, and whether provider fencing is supported.
- [x] #2 Default headings and boolean values use concise human-readable wording and the table remains legible on an 80-column terminal for every built-in policy.
- [x] #3 A documented `--full` text mode preserves origin, origin version, contract version, key policy version, and every field currently shown.
- [x] #4 Explicit JSON output remains schema-compatible and complete regardless of `--full`.
- [x] #5 Empty policy discovery emits the correct compact or full header without placeholder rows.
- [x] #6 CLI contract tests cover compact, full, empty, external-policy, and JSON behavior; command help and human-readable output documentation are updated, generated release documentation renders successfully, and CHANGELOG `Unreleased` records the changed text output.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add `--full` to `policy list` and route the render-mode flag without changing the JSON payload.
2. Render a five-column aligned compact table by default and retain all nine policy fields in full text mode.
3. Add compact, full, empty, external-policy, JSON, help, documentation, changelog, and release-doc coverage.
4. Run focused tests, then all required quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented compact and full policy-list text modes, added parser/help wiring, preserved identical JSON payloads, and added coverage for built-in, empty, external, full, and JSON cases. Focused CLI/release tests and the full test suite pass.

Validation: mise run lint, format-check, test (307 core + 19 SDK), and typecheck passed; staged-file hooks passed; independent verifier returned PASS for all six criteria; merged implementation commit d324a95 to main by fast-forward.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added an aligned five-column default `policy list` table and a `--full` text mode retaining all policy metadata while leaving JSON unchanged. Added compact/full/empty/external/JSON tests and updated help, CLI documentation, and changelog. Verified with all required quality gates and independent acceptance review; merged as d324a95.
<!-- SECTION:FINAL_SUMMARY:END -->
