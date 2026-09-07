---
id: TASK-48
title: Measure display width for wide characters in list output
status: Done
assignee:
  - '@pi-codex'
created_date: '2026-09-07 03:27'
updated_date: '2026-09-07 08:23'
labels:
  - cli
dependencies: []
references:
  - src/worklease/cli.py
modified_files:
  - src/worklease/cli.py
  - tests/test_cli.py
priority: low
type: enhancement
ordinal: 49000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`_render_table`, `_shorten_text`, and `_shorten_resource` measure and pad by code point. East-Asian-wide characters in a resource or claim ID render at roughly twice the width, misaligning every following column and exceeding the documented 52-character RESOURCE bound. Use `unicodedata.east_asian_width` (W/F count as 2) for measurement and truncation.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A list row containing CJK characters keeps column starts aligned with ASCII rows and stays within documented widths
- [x] #2 Tests cover wide-character resources and claim IDs in compact and --full output
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add display-width helpers using unicodedata.east_asian_width, counting W/F characters as two columns.
2. Apply display-width measurement to table padding and prefix/suffix/resource truncation while preserving existing resource anchors.
3. Add compact and --full list-rendering tests with CJK resources and claim IDs, then run focused and repository quality gates.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Claimed resource backlog-md:/Users/brett/dev/me/worklease/.git:.:TASK-48 for local coordination; provider mutations are not fenced.

Implemented display-column measurement and width-aware prefix/suffix truncation. CJK compact/full rendering coverage verifies aligned column starts and 52/18-column resource/claim bounds. Review found no actionable defects. Validation on integrated main: mise run lint, format-check, typecheck, test (227 core + 19 SDK tests), and staged pre-commit hooks passed. Implementation commit: 9f82e86.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
List rendering now measures East Asian wide/fullwidth characters as two terminal columns for padding and truncation. Added compact and --full CJK resource/claim-ID coverage; all repository quality gates and independent review passed. Implemented in 9f82e86.
<!-- SECTION:FINAL_SUMMARY:END -->
