---
id: TASK-30
title: Add aggregate CLI help for agent discovery
status: In Progress
assignee:
  - '@brett'
created_date: '2026-07-15 19:07'
updated_date: '2026-07-15 20:40'
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
- [x] #1 An explicit aggregate-help invocation lists every canonical top-level and nested command, including policy list and policy describe.
- [x] #2 Aggregate output includes usage, options, aliases, and any existing or relevant examples, with clear command-path section separators.
- [x] #3 The output is deterministic, read-only, generated from the same parser tree, and does not change existing targeted --help behavior.
- [x] #4 Subprocess coverage verifies every emitted canonical help invocation exits successfully and aliases are not emitted as duplicate sections.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Add an explicit opt-in aggregate-help flag that traverses the existing parser tree and renders canonical command sections with aliases and examples. 2. Preserve normal and targeted help paths while covering policy list and policy describe. 3. Add subprocess coverage for deterministic aggregate output, canonical invocations, and alias de-duplication.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented in src/worklease/cli.py and tests/test_cli.py. Added explicit --help-all traversal over the existing argparse tree, canonical command-path sections, nested policy commands, alias summaries, usage/options/epilog examples, deterministic output, and read-only behavior. Verification: aggregate subprocess coverage passed; every emitted canonical help invocation exited 0; aliases were not emitted as duplicate sections; full tests (40 CLI tests plus SDK suite) passed; mise run lint, mise run format-check, and mise run typecheck passed.
<!-- SECTION:NOTES:END -->
