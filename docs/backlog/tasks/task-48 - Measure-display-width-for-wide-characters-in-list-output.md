---
id: TASK-48
title: Measure display width for wide characters in list output
status: To Do
assignee: []
created_date: '2026-09-07 03:27'
labels:
  - cli
dependencies: []
references:
  - src/worklease/cli.py
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
- [ ] #1 A list row containing CJK characters keeps column starts aligned with ASCII rows and stays within documented widths
- [ ] #2 Tests cover wide-character resources and claim IDs in compact and --full output
<!-- AC:END -->
