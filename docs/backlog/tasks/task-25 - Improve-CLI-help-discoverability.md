---
id: TASK-25
title: Improve CLI help discoverability
status: In Progress
assignee:
  - '@codex-loop-fresh-20260714-worklease-pass'
created_date: '2026-07-15 04:10'
updated_date: '2026-07-15 04:19'
labels:
  - cli
  - ux
dependencies: []
priority: medium
type: enhancement
ordinal: 26000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Make the worklease help menus easier to scan and more useful for common workflows by organizing commands and showing concise examples.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 Top-level help groups related commands, including singleton, bundle, inspection/reconciliation, and maintenance commands.
- [ ] #2 Top-level help includes one or two safe, copyable common-usage examples.
- [ ] #3 The acquire, exec, release, and replace-file help menus each include a relevant example.
- [ ] #4 Subprocess tests verify each enriched help command exits successfully and prints its examples to stdout.
- [ ] #5 Existing command parsing and output behavior remain unchanged.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Rework argparse help rendering into stable command groups while preserving aliases and parsing behavior. 2. Add concise safe examples to top-level help and acquire, exec, release, and replace-file help. 3. Add subprocess coverage for enriched help output and run the repository quality gates. 4. Verify acceptance criteria, commit the feature, and integrate the branch into local main.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented grouped top-level argparse help with Singleton, Bundles, Inspection and reconciliation, and Maintenance sections. Added concise top-level examples plus acquire, exec, release, and replace-file epilogs. Added subprocess coverage for grouped help and each command example. Smoke-tested all enriched help commands; mise run lint, format-check, typecheck, and test pass.
<!-- SECTION:NOTES:END -->
