---
id: TASK-77
title: Make the release reason optional
status: To Do
assignee: []
created_date: '2026-09-12 02:18'
updated_date: '2026-09-12 02:24'
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
  - scripts/release_docs.py
priority: medium
type: enhancement
ordinal: 84000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`worklease release` refuses to run without `--reason`, yet the workflow skill treats the caller-supplied reason as audit metadata rather than checkpoint proof. A required audit note turns the command that must remain usable in shell traps and interrupted agent sessions into a two-argument command that fails on omission.

## Decision

At the CLI parser boundary only, `-m/--reason` on `release`, `release-bundle`, and their aliases defaults to `released`. The lifecycle/store API and MCP `release` schema continue to require a nonblank caller-supplied reason. Therefore an explicit empty CLI value is preserved and continues to fail with `invalid-release-reason`; it is never replaced by the default. The chosen audit reason remains in the release receipt and idempotency request. Epoch termination keeps its separate semantic reason `released`, including when a meaningful audit reason was supplied.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease release` and `worklease release-bundle` succeed without `--reason`, given a lease handle or complete credentials, and persist `released` as the audit reason in the receipt and idempotency record; the retained epoch termination reason remains `released`.
- [ ] #2 An explicit `--reason` is recorded verbatim in the receipt and idempotency record, while the epoch termination reason remains `released`; an explicit empty value fails with the existing `invalid-release-reason` error.
- [ ] #3 Canonical commands and the `bundle-release` alias expose the same default, and help text shows it. README, `docs/cli-reference.md`, the workflow skill, and generated manual examples drop `--reason` where it is boilerplate while retaining one meaningful audit-note example; CHANGELOG `Unreleased` is updated; generated release documentation renders successfully.
- [ ] #4 The public lifecycle/store methods and MCP `release` tool keep requiring a nonblank reason and remain schema-compatible.
- [ ] #5 Tests cover omitted, explicit, and explicitly empty reasons for singleton and bundle CLI commands, the bundle alias, receipt/idempotency persistence, fixed termination semantics, and unchanged direct API and MCP validation; `mise run lint`, `format-check`, `test`, and `typecheck` pass.
<!-- AC:END -->
