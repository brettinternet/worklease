---
id: TASK-46
title: Make CLI short flags and text labels consistent across commands
status: Done
assignee:
  - '@codex-task-46'
created_date: '2026-09-07 03:27'
updated_date: '2026-09-07 05:11'
labels:
  - cli
  - devex
dependencies: []
references:
  - src/worklease/cli.py
priority: medium
type: enhancement
ordinal: 47000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Short flags mean different things per subcommand: `-r`/`-c`/`-a` are --resource/--claim-id/--agent-id everywhere except `gc` (--retention-days/--cutoff/--apply) and the top level (`-a` = --help-all); `-F` is --token-file except on `list` (--full); `-C` is --coordination-only on acquire but --successor-claim-id on transfer and --content-file on replace-file; `-W`/`-O` differ between acquire, transfer, and reconcile. Scripts that reuse a flag across commands silently change meaning. Also: `status --verbose` prints raw camelCase claim keys while every other renderer uses `_text_label`, and `--json --help`/`--help-all` print text.

Decide on one flag namespace (likely: reserve -r/-c/-a/-s/-o/-w/-t/-F/-D/-R/-T for the shared claim vocabulary and give command-specific options distinct letters or long-only forms), treat removals as a breaking CLI change with a documented migration, and normalize verbose labels.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Every short flag has one meaning across all subcommands, documented in a table in README
- [x] #2 status --verbose labels use the same upper-snake convention as other renderers; tests updated
- [x] #3 Removed or changed short flags are listed in release notes; tests assert no letter is bound to two meanings
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Remove colliding short aliases while retaining the shared lifecycle vocabulary and unique command aliases; add a parser-tree invariant test that every short letter resolves to one long-option meaning.
2. Render verbose status claim and release labels through the existing upper-snake label helper and strengthen text-output coverage.
3. Document the complete short-flag namespace in README and record every removed alias with long-form migrations in Unreleased release notes.
4. Run focused CLI tests, then all repository quality gates; review the final diff and record objective acceptance evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented one-to-one short-option bindings, a recursive parser-tree collision/documentation test, upper-snake verbose status labels, the README namespace table, and Unreleased migration notes. Four focused CLI tests pass.

Validation: recursive parser-tree test proves every short option maps to one long-option meaning and every mapping appears in README; verbose status text assertions prove upper-snake labels and redaction; CHANGELOG.md lists all 13 removed aliases and migrations. mise run lint, format-check, test (232 tests total), typecheck, and hooks passed. Final diff review found no remaining item-scoped defects.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Made CLI short options globally unambiguous by removing colliding aliases while preserving long options, normalized verbose status labels, and documented the complete namespace and breaking migrations. Verified with parser-tree/documentation invariants, verbose text coverage, all 232 tests, lint, formatting, type checks, and pre-commit hooks.
<!-- SECTION:FINAL_SUMMARY:END -->
