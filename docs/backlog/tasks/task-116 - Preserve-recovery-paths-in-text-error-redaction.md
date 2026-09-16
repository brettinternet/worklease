---
id: TASK-116
title: Preserve recovery paths in text error redaction
status: To Do
assignee: []
created_date: '2026-09-16 17:51'
labels: []
dependencies: []
references:
  - internal/output/output.go
  - internal/output/output_test.go
priority: medium
type: bug
ordinal: 158000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Text error rendering applies value-only token redaction to safe detail fields, so a contextual handle path containing its expected 64-hex context digest is printed as ctx-[REDACTED].json. The structured redactor already preserves SHA-256 values under path keys, but writeTextError discards that key context. This breaks the recovery pointer promised by the output contract while providing no additional credential protection.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Text errors preserve the complete pendingPath and other explicitly safe path-valued recovery details, including contextual handle names containing a 64-hex digest.
- [ ] #2 JSON and text rendering use the same key-aware redaction policy for safe detail values.
- [ ] #3 Bearer credentials and unclassified bare 64-hex values remain redacted in messages, arbitrary fields, and nested output.
- [ ] #4 Regression tests reproduce the contextual pendingPath case in rendered text and verify both path preservation and credential redaction.
<!-- AC:END -->
