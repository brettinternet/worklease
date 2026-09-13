---
id: TASK-100
title: Align history shortcut contracts and help
status: To Do
assignee: []
created_date: '2026-09-13 03:36'
labels: []
dependencies: []
references:
  - e3408d5
documentation:
  - docs/backlog/docs/go-rewrite/doc-2 - Go-Product-Contract.md
  - docs/cli-reference.md
modified_files:
  - internal/cli/commands.go
  - internal/cli/root_test.go
priority: low
type: docs
ordinal: 125000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Commit e3408d5 made `worklease history` without a resource an alias for the bounded global event feed while preserving `history --resource RESOURCE` as the retained epoch projection. The normative Go product contract still describes a required resource, command help shows only the no-argument form, and the JSON alias behavior should be explicit so future changes do not accidentally fork the event contract.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 The normative Go product contract documents both no-argument global events and resource-scoped epoch history, including bounded limit/cursor behavior.
- [ ] #2 `worklease history --help` presents both supported forms and clearly distinguishes their projections.
- [ ] #3 User documentation states that no-argument `history --json` returns the canonical events envelope with `operation` equal to `events`.
- [ ] #4 Automated tests protect the documented help and JSON alias behavior.
<!-- AC:END -->
