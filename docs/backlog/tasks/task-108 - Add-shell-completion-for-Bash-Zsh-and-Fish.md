---
id: TASK-108
title: 'Add shell completion for Bash, Zsh, and Fish'
status: Done
assignee:
  - '@brett'
created_date: '2026-09-14 02:13'
updated_date: '2026-09-15 00:22'
labels:
  - cli
  - ux
dependencies: []
references:
  - internal/cli/root.go
  - internal/cli/commands.go
documentation:
  - README.md
  - docs/cli-reference.md
priority: medium
type: enhancement
ordinal: 146000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Worklease currently rejects the completion-generation flag exposed by its CLI framework and ships no completion command, scripts, or installation guidance. Users must remember a broad nested command and option surface manually. Add static, side-effect-free completion for the common supported shells without reading claim state or exposing credentials.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 A documented Worklease command emits shell completion for Bash, Zsh, and Fish, rejects unsupported shells clearly, and does not create or mutate Worklease state.
- [x] #2 Completion covers visible root and nested commands, command aliases, global options, and command-local options from the registered CLI tree; hidden/internal completion plumbing is not suggested.
- [x] #3 Generated completion is deterministic and ANSI-free, and automated tests exercise representative root, nested-command, alias, global-option, and local-option completions for each supported shell.
- [x] #4 README installation guidance shows how to enable completion in Bash, Zsh, and Fish, and the CLI reference documents the completion command.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Inspect the CLI framework and existing command/test patterns.
2. Add a side-effect-free completion command for Bash, Zsh, and Fish using the registered CLI tree.
3. Add deterministic completion tests covering commands, aliases, and options.
4. Document shell setup and the command in README and CLI reference.
5. Run focused and repository quality gates, review the diff, commit, merge to main, and clean up the worktree.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented the Bash, Zsh, and Fish completion command with read-only tree-derived suggestions, alias/global/local option coverage, unsupported-shell validation, tests, and installation/reference documentation.

Review found and fixed three completion safety/coverage defects: completion after exec's -- separator could dispatch the guarded child; the upstream Bash script used eval and depended on bash-completion helpers; and explicit help/h aliases were omitted. Added valid-claim no-dispatch/revision regression and clean-Bash command-substitution smoke coverage.

Implementation commit: 4a9a855. Final focused/full checks passed before integration; independent reviewer re-review: PASS.

Final verification on integrated main: mise run lint, format-check, test, and typecheck all passed. mise run man and doc-test passed before integration. Completion tests cover all three shells, unsupported-shell errors, deterministic ANSI-free scripts, visible tree/alias/global/local suggestions, clean Bash without bash-completion, literal command-substitution arguments, and completion after exec -- without child execution or revision change. Independent reviewer re-review passed. Integrated commit: 2a7f342.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Added documented Bash, Zsh, and Fish completion derived from the registered CLI tree. Completion is deterministic, ANSI-free, state-independent, includes aliases and options, avoids Bash eval/dependency hazards, and cannot dispatch guarded commands after --. Verified with the full repository gates, generated manual/docs checks, focused safety regressions, and independent review; merged to main as 2a7f342.
<!-- SECTION:FINAL_SUMMARY:END -->
