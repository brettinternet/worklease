---
id: TASK-99
title: Make human-readable CLI output consistent
status: Done
assignee:
  - '@brett'
created_date: '2026-09-13 02:41'
updated_date: '2026-09-13 03:35'
labels:
  - cli
  - ux
dependencies: []
references:
  - internal/cli/text.go
  - internal/cli/lease_commands.go
  - internal/cli/ledger_commands.go
  - internal/cli/resource_commands.go
  - internal/cli/watch_commands.go
  - internal/cli/gc_commands.go
  - internal/output/output.go
  - docs/cli-reference.md
priority: medium
type: enhancement
ordinal: 124000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
An audit after compacting `worklease list` found that several text commands still echo their command name as a standalone first line, some documented `--full` flags do not change output, and related views use inconsistent labels, timestamp styles, and state coloring. Human-readable output should be concise and conventional while JSON retains stable operation metadata and complete machine-oriented fields.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Text output for `status`, `history`, `events`, `gc`, `key`, `policy describe`, and `watch` does not echo the command name as a standalone banner; useful outcome sentences and table headers remain.
- [x] #2 `status --full`, `events --full`, and `policy describe --full` each have a documented, test-covered distinction from their default text view, or the unsupported flag is removed from that command.
- [x] #3 Human-readable field labels follow one documented vocabulary and casing convention; JSON field names and envelopes remain unchanged.
- [x] #4 Default `status`, `history`, and `events` views use concise relative timing where timing aids scanning, while full views retain absolute RFC3339 timestamps.
- [x] #5 History and event state or kind coloring uses the shared TTY-aware semantic palette without coloring identifiers or timestamps; redirected output, `NO_COLOR`, `TERM=dumb`, and JSON contain no ANSI sequences.
- [x] #6 Affected commands use command-specific text renderers rather than the generic operation-banner writer, and tests cover deterministic ordering, redaction, control-character safety, and the absence of Go map or struct dumps.
- [x] #7 CLI help and `docs/cli-reference.md` describe compact versus full output and color behavior for the affected commands.
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Adopt the existing TASK-99 diff into an isolated HWT worktree while preserving the primary checkout.
2. Audit affected commands, renderers, tests, help, and docs against all acceptance criteria; finish missing behavior with focused tests.
3. Run focused tests and all repository quality gates, then obtain independent acceptance review.
4. Commit the completed change, verify the primary diff is represented by the worktree commit, clear only that duplicate diff, merge to main, finalize TASK-99, and remove the managed worktree/branch.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented command-specific text renderers and outcome titles for the affected commands, compact relative timing with RFC3339 full views, lowerCamelCase labels, semantic TTY-aware coloring, stable JSON projections, control-safe deterministic rendering, and expanded help/docs.
Validation: mise run lint, format-check, test, typecheck, and hooks passed after syncing main. Independent verifier passed all seven criteria, including manual TTY/NO_COLOR/TERM=dumb/redirection/JSON checks.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Completed the human-readable CLI consistency pass: concise command-specific output, meaningful compact/full timing, consistent labels, safe semantic color, unchanged JSON envelopes, and updated help/reference documentation. Verified by all repository gates and an independent criterion-by-criterion review.
<!-- SECTION:FINAL_SUMMARY:END -->
