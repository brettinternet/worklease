---
id: TASK-77
title: Make the release reason optional
status: To Do
assignee: []
created_date: '2026-09-12 02:18'
labels:
  - cli
  - ux
dependencies: []
references:
  - src/worklease/cli.py
  - src/worklease/lifecycle.py
  - tests/test_cli.py
  - README.md
  - docs/cli-reference.md
  - skills/worklease-workflow/SKILL.md
  - CHANGELOG.md
priority: medium
type: enhancement
ordinal: 84000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`worklease release` refuses to run without `--reason`, yet the workflow skill states the reason is audit metadata and not checkpoint proof. A required audit note turns the one command that must work in shell traps and interrupted agent sessions into a two-argument command that fails on omission.

## Decision

`-m/--reason` on `release` and `release-bundle` defaults to `released`. The store keeps requiring non-empty text, so an explicit empty reason still fails with `release-reason` as today. The MCP `release` tool schema keeps `reason` required and is unchanged.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease release` and `worklease release-bundle` succeed without `--reason` (given a lease handle or complete credentials) and record the reason `released` in the release receipt and the retained termination.
- [ ] #2 An explicit `--reason` is recorded verbatim, and an explicit empty reason still fails with the existing `release-reason` error.
- [ ] #3 Help text shows the default; README, `docs/cli-reference.md`, and the skill examples drop `--reason` where it is only boilerplate while keeping one example that records a meaningful audit note; CHANGELOG `Unreleased` is updated; the MCP `release` tool is untouched.
- [ ] #4 Tests cover the default, an explicit reason, and the empty-reason failure for both commands, and `mise run lint`, `format-check`, `test`, and `typecheck` pass.
<!-- AC:END -->
