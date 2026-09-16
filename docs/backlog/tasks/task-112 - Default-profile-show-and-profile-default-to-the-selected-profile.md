---
id: TASK-112
title: Default profile show and profile default to the selected profile
status: To Do
assignee: []
created_date: '2026-09-16 04:35'
labels:
  - ergonomics
  - remote-authority
dependencies: []
priority: medium
type: enhancement
ordinal: 154000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
`worklease profile show` and `worklease profile default` with no argument fail with 'profile name is required', and their USAGE lines omit the NAME argument entirely. After enrollment there is usually exactly one profile and it is already selected, so the bare command has an unambiguous meaning: show or report the profile that normal precedence (`--profile`, `WORKLEASE_PROFILE`, worktree binding, default) would select. Requiring the name adds a lookup step for the most common inspection.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [ ] #1 `worklease profile show` with no NAME shows the profile that current selection precedence would use and states which rule selected it; with no selectable profile it fails naming `profile list` and `enroll` as next steps.
- [ ] #2 `worklease profile default` with no NAME prints the current default profile (or 'no default profile' with exit 0) instead of erroring; `profile default NAME` still sets it.
- [ ] #3 USAGE and help text for `profile show`, `profile default`, `profile remove`, `profile bind`, and `profile unbind` document the NAME argument and whether it is optional; tests cover both bare forms and the JSON output.
<!-- AC:END -->
