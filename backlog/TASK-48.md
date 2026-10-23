---
id: TASK-48
title: Measure display width for wide characters in list output
status: done
type: enhancement
priority: low
assignees: ['@pi-codex']
labels: [cli]
rank: a57213c2eb5721376
refs: [src/worklease/cli.py]
files: [src/worklease/cli.py, tests/test_cli.py]
created: 2026-09-07T03:27:00Z
updated: 2026-09-07T14:13:00Z
---

`_render_table`, `_shorten_text`, and `_shorten_resource` measure and pad by code point. East-Asian-wide characters in a resource or claim ID render at roughly twice the width, misaligning every following column and exceeding the documented 52-character RESOURCE bound. Use `unicodedata.east_asian_width` (W/F count as 2) for measurement and truncation.

## Acceptance criteria

- [x] A list row containing CJK characters keeps column starts aligned with ASCII rows and stays within documented widths
- [x] Tests cover wide-character resources and claim IDs in compact and --full output

## Plan

1. Add display-width helpers using unicodedata.east_asian_width, counting W/F characters as two columns.
2. Apply display-width measurement to table padding and prefix/suffix/resource truncation while preserving existing resource anchors.
3. Add compact and --full list-rendering tests with CJK resources and claim IDs, then run focused and repository quality gates.

## Notes

Claimed resource backlog-md:/Users/brett/dev/me/worklease/.git:.:TASK-48 for local coordination; provider mutations are not fenced.

Implemented display-column measurement and width-aware prefix/suffix truncation. CJK compact/full rendering coverage verifies aligned column starts and 52/18-column resource/claim bounds. Review found no actionable defects. Validation on integrated main: mise run lint, format-check, typecheck, test (227 core + 19 SDK tests), and staged pre-commit hooks passed. Implementation commit: 9f82e86.

Post-delivery review (TASK-56) found and fixed defects in this work; see TASK-56 for the specific defect, the fix, and its regression test.

## Summary

List rendering now measures East Asian wide/fullwidth characters as two terminal columns for padding and truncation. Added compact and --full CJK resource/claim-ID coverage; all repository quality gates and independent review passed. Implemented in 9f82e86.
