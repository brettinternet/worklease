---
id: TASK-17
title: Complete explicit human-readable CLI output
status: To Do
assignee: []
created_date: '2026-07-14 02:46'
labels:
  - cli
  - ux
dependencies: []
references:
  - src/worklease/cli.py
  - README.md
priority: medium
type: enhancement
ordinal: 18000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Keep schema-versioned JSON as the default for agents and integrations. Make --format text a consistently human-readable opt-in mode across every CLI command instead of falling back to compact JSON for most operations.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 JSON remains the default for every command and its released schema and exit-code behavior remain compatible.
- [ ] #2 Every command has a documented, deterministic --format text representation that does not emit compact JSON as a fallback.
- [ ] #3 Text status and list output never expose bearer tokens; mutation output exposes only the minimum owner data needed for continued operation.
- [ ] #4 Tests cover successful and failed text output for every command, including child-process failures and parser errors.
<!-- AC:END -->
