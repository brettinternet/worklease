---
id: TASK-72
title: Make garbage-collection output easier to scan
status: To Do
assignee: []
created_date: '2026-09-12 02:11'
labels:
  - cli
  - ux
dependencies: []
references:
  - src/worklease/cli.py
  - tests/test_cli.py
  - tests/test_gc.py
  - docs/cli-reference.md
priority: medium
type: enhancement
ordinal: 79000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Human-readable `gc` output exposes storage field names such as `bundleEpochs`, several absolute timestamps, and null placeholders. The result is technically complete but makes it difficult to answer the operator questions: what is eligible, how old is it, what remains protected, and what command is safe to run next.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Dry-run text output clearly identifies that no records were changed, states the retention window and cutoff in concise human terms, and shows the total number of eligible records.
- [ ] #2 Eligible record groups use readable labels, counts, and compact oldest/newest ages; zero-count groups and missing ranges do not emit raw `null` values.
- [ ] #3 Protected unresolved-operation groups remain visible with enough information to explain why collection cannot remove them.
- [ ] #4 The apply hint remains copy-pasteable, preserves the exact cutoff used by the dry run, and is omitted when there is nothing eligible to collect.
- [ ] #5 `gc --apply` clearly reports what was collected and distinguishes a successful no-op; JSON output remains schema-compatible and complete.
- [ ] #6 CLI contract tests use a fixed clock to cover eligible, empty, protected, dry-run, apply, and JSON cases; human-readable output documentation is updated.
<!-- AC:END -->
