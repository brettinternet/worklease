---
id: TASK-30
title: Add aggregate CLI help for agent discovery
status: To Do
assignee: []
created_date: '2026-07-15 19:07'
updated_date: '2026-07-15 19:08'
labels: []
dependencies: []
ordinal: 31000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Provide an explicit read-only aggregate help mode for one-shot CLI onboarding while keeping normal help concise. Recurse through the parser tree so canonical top-level and nested commands are covered, including policy list and policy describe, and represent aliases without duplicate sections.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 An explicit aggregate-help invocation lists every canonical top-level and nested command, including policy list and policy describe.
- [ ] #2 Aggregate output includes usage, options, aliases, and any existing or relevant examples, with clear command-path section separators.
- [ ] #3 The output is deterministic, read-only, generated from the same parser tree, and does not change existing targeted --help behavior.
- [ ] #4 Subprocess coverage verifies every emitted canonical help invocation exits successfully and aliases are not emitted as duplicate sections.
<!-- AC:END -->
