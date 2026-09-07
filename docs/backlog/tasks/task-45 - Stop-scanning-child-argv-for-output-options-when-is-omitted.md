---
id: TASK-45
title: Stop scanning child argv for output options when -- is omitted
status: To Do
assignee: []
created_date: '2026-09-07 03:27'
labels:
  - cli
dependencies: []
references:
  - src/worklease/cli.py
priority: medium
type: bug
ordinal: 46000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`_visible_output_options` truncates argv only at a bare `--`, but `exec`/`exec-bundle` accept child argv without it. Any exact `--json`, `-j`, `--format`, `-f`, `--format=...`, or `-f...` token in the child command then either rejects the whole command as conflicting-output-format (`worklease --json exec ... git log --format=oneline` exits 64) or flips the error output mode. Either require `--` for exec commands with an actionable hint, or truncate the pre-parse scan at the first positional after the exec subcommand, and prefer deriving the json/format conflict from parsed args rather than argv scanning where possible.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 worklease --json exec <claim flags> git log --format=oneline runs the child and returns JSON
- [ ] #2 A parser error for exec is rendered in the format chosen by worklease options, unaffected by tokens in child argv
- [ ] #3 Behavior with and without -- is documented in exec help and README; tests cover both
<!-- AC:END -->
