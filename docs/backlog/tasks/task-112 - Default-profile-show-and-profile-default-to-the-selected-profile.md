---
id: TASK-112
title: Default profile show and profile default to the selected profile
status: Done
assignee: []
created_date: '2026-09-16 04:35'
updated_date: '2026-09-16 04:48'
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
- [x] #1 `worklease profile show` with no NAME shows the profile that current selection precedence would use and states which rule selected it; with no selectable profile it fails naming `profile list` and `enroll` as next steps.
- [x] #2 `worklease profile default` with no NAME prints the current default profile (or 'no default profile' with exit 0) instead of erroring; `profile default NAME` still sets it.
- [x] #3 USAGE and help text for `profile show`, `profile default`, `profile remove`, `profile bind`, and `profile unbind` document the NAME argument and whether it is optional; tests cover both bare forms and the JSON output.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Give each profile subcommand an explicit positional-argument usage/help contract.
2. Resolve bare profile show through existing profile precedence and expose the selection source in text and JSON; make bare profile default inspect without mutation.
3. Add focused command/help tests, run repository quality gates, review the diff, and integrate it.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented in d0f6073. Focused profile command tests cover bare show selection/source, missing-profile guidance, bare default with and without a configured default, explicit default mutation, JSON envelopes, and positional-argument help. Self-review found no remaining item-scoped defects. Validation passed: go test ./internal/cli; mise run lint; mise run format-check; mise run test; mise run typecheck; mise run hooks.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Implemented optional profile inspection forms and complete positional help. Bare profile show now follows normal precedence and reports its selection source; bare profile default reports the current default without mutation, including JSON and the no-default case. Commit d0f6073 was merged to main after all repository quality gates and pre-commit hooks passed.
<!-- SECTION:FINAL_SUMMARY:END -->
