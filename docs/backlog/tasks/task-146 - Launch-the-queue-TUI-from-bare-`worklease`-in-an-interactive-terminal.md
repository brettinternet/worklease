---
id: TASK-146
title: Launch the queue TUI from bare `worklease` in an interactive terminal
status: Done
assignee: []
created_date: '2026-09-26 06:09'
updated_date: '2026-09-26 08:32'
labels: []
dependencies:
  - TASK-145
references:
  - internal/cli/commands.go
  - internal/cli/queue_command.go
  - cmd/worklease-doc-test
  - cmd/worklease-man
priority: medium
type: feature
ordinal: 80000
---

## Description

<!-- SECTION:DESCRIPTION:BEGIN -->
Bare `worklease` currently prints help and exits 0. Once the queue TUI has a Claims tab (TASK-145), a person typing `worklease` at a terminal is better served by the live TUI than by help text, and users with no task provider still get a useful view of claims.

Agents, scripts, and CI are primary callers of this CLI and rely on bare invocation being non-interactive, so the TUI must launch only for a human at a terminal.

The starting tab is chosen from configuration, not from provider reachability. Falling back to Claims when a configured provider is down would hide the outage and look like missing config; the queue tab already shows degraded sources, and Claims is one keystroke away.

Bare invocation is also the current onboarding path: root help says to start with `worklease acquire --path README.md`. That guidance has to survive in the TUI empty state.
<!-- SECTION:DESCRIPTION:END -->

## Acceptance Criteria
<!-- AC:BEGIN -->
- [x] #1 Bare `worklease` with no arguments launches the queue TUI only when both stdin and stdout are terminals
- [x] #2 Bare `worklease` with a non-terminal stdin or stdout prints the current root help and exits 0; `worklease help` and `-h` print help in every case
- [x] #3 With a valid `queue.yaml`, the TUI opens on the first configured queue view even when sources fail, showing the existing source-error state
- [x] #4 Without `queue.yaml`, the TUI opens on the Claims tab with a hint to run `worklease queue init`
- [x] #5 A malformed `queue.yaml` fails with the same error as `worklease queue`
- [x] #6 Launching the TUI from bare `worklease` creates no authority, store, or config state on a fresh machine
- [x] #7 The empty Claims tab shows onboarding hints including `worklease acquire --path README.md`, `?` for help, and `q` to quit
- [x] #8 Root help text, the man page, and doc tests describe the interactive and non-interactive behavior of bare `worklease`
- [x] #9 Tests cover terminal detection, starting-tab selection for each config state, and no state creation at the lowest layer that reproduces each
<!-- AC:END -->

## Implementation Plan

<!-- SECTION:PLAN:BEGIN -->
1. Route bare root action through the existing queue entry point only for real stdin/stdout TTYs; retain explicit help and nonterminal behavior.
2. Reuse queue config fallback/Claims model, add missing-config and empty-claims onboarding copy without creating state.
3. Update root help/man/doc checks and focused tests for TTY routing, config selection/errors, and fresh-machine read-only behavior.
4. Run focused/race tests and repository gates, review once, commit and integrate, then finalize task evidence.
<!-- SECTION:PLAN:END -->

## Implementation Notes

<!-- SECTION:NOTES:BEGIN -->
Implemented in 0cd42a4 (fast-forwarded to main). PTY tests exercised bare launch with missing queue config (Claims, onboarding, clean home/config/state), valid config with unavailable source (first view and source error), and malformed config parity with queue. Terminal matrix and nonterminal/explicit help tests passed; generated man-page assertion and shipped-binary doc-test passed. Validation: mise run lint, format-check, test, typecheck, doc-test, hooks; focused go test -race -count=3. One item-scoped general review found no remaining defects. No blocker or follow-up.
<!-- SECTION:NOTES:END -->

## Final Summary

<!-- SECTION:FINAL_SUMMARY:BEGIN -->
Bare interactive worklease opens the queue TUI, defaulting to Claims without config; redirected and explicit-help invocations remain noninteractive. PTY, doc/man, race, full suite, and hook checks passed; integrated as 0cd42a4.
<!-- SECTION:FINAL_SUMMARY:END -->
