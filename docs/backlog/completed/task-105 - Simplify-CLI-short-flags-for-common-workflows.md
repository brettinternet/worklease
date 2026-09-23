---
id: TASK-105
title: Simplify CLI short flags for common workflows
status: Done
assignee:
  - '@brett'
created_date: '2026-09-13 05:33'
updated_date: '2026-09-13 05:48'
labels: []
dependencies: []
type: enhancement
ordinal: 130000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
The CLI's short options prioritize historical and advanced inputs, producing uppercase-heavy common commands while frequently used session and wait selectors have no intuitive lowercase aliases. Backward compatibility is not required. Reserve short options for routine human workflows and leave uncommon provider metadata, explicit credentials, replay controls, and guarded-operation tuning clear in their long forms.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 The supported short option mapping is exactly `-j/--json`, `-H/--home`, `-h/--help`, `-v/--version`, `-r/--resource`, `-s/--session`, `-t/--ttl`, `-w/--wait`, `-a/--agent`, `-f/--full`, and `-m/--reason` wherever their long option is supported.
- [x] #2 `--source` and `--work-key` remain available as long options but no longer use `-s` or `-w`; provider triples, handles/leases, explicit credentials, replay controls, polling controls, coordination-only mode, and guarded-operation tuning have no short aliases.
- [x] #3 Parser and help tests prove each retained alias behaves identically to its long form, removed aliases are rejected, and no short option has different meanings across commands.
- [x] #4 README, CLI reference, generated help/man-page expectations, examples, and product-contract short-option documentation reflect the new mapping without claiming backward compatibility.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Replace the CLI alias declarations with the exact supported namespace, keeping uncommon and guarded-operation controls long-only.
2. Strengthen command-tree and parser tests to prove retained aliases equal their long forms, removed aliases fail, and every short name has one global meaning.
3. Update README, CLI reference, generated help/man expectations, examples, product contract, and release notes to document only the new breaking mapping.
4. Run focused CLI tests and all repository quality gates, independently review the diff, then merge the implementation and finalize the authoritative task record.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented the exact 11-alias namespace in the isolated worktree, added parser/help rejection and equivalence coverage, updated generated manual expectations, and refreshed README, CLI reference, product contract, examples, and Unreleased breaking notes. Focused internal/cli tests pass.

Validation: mise run lint, format-check, test, typecheck, and staged mise run hooks passed. Independent verifier passed all acceptance criteria, including go test -race ./internal/cli and mandoc lint, with no defects. Post-merge focused alias/parser/help/man tests passed. Implementation commit 1156a75; merge commit 00ad183.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Simplified the CLI to the exact common-workflow short-option namespace, removed advanced and historical aliases, and updated parser/help/man coverage plus README, CLI reference, product contract, examples, and breaking release notes. Verified with all repository gates, pre-commit hooks, independent race/man checks, and post-merge focused tests.
<!-- SECTION:FINAL_SUMMARY:END -->
